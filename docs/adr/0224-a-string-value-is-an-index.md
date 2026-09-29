# 0224 — A string value is an index, not the address of a literal

- Status: Accepted
- Date: cycle 172 (roadmap Gap R.42, closes L11.8; ADR 0163 introduced `@str_tab`, ADR 0154 owns textual IR)
- Affects: `pkg/lang/codegen.go` (`value`, `internStr`, `exprIsString`, `callReturnsStr`, `printsAsInternedStr`, the Assign element-kind facts, the f-string print path, the static-dispatch call emission, the comprehension loop header), `docs/language.md`

## The measurement that produced this

Twenty-two string shapes through both engines, expectations taken from CPython, not from the
emission. Before the change, five of them never reached the question they were asking:

```
x = "hi";  x == "hi"                 -> exit 2  icmp eq i32 @.str1, %t1
Dog().sound()   (-> str: return "x") -> exit 2  ret  i32 @.str1
class C: self.w = "hi"; print(C().w) -> silent   prints `0`
"x" in ["a","b"]                     -> exit 2  icmp eq i32 %getresult2, @.str2
[n for n in names if n == "a"]       -> exit 2  PHI node entries do not match predecessors!
```

`llc` was rejecting an `i32` that held the address of a `private global [N x i8]`. The programs are
ordinary; the exit code blamed the compiler, and no diagnostic could make that fair.

## The decision

**A string *value* is an index into the runtime `@str_tab`.** One representation, everywhere a value
is what is asked for:

- `value()` of a string literal interns it (`call i32 @rt_str_intern2(i8* getelementptr(...@.strN), i8* null)`) and yields the index.
- A variable, an instance attribute, a comprehension element, a container element, a function or
  method result are all indices, so comparing, testing membership, or returning one is an
  operation between two `i32`s.
- The address of a global survives only where bytes are the question: `printf`'s format strings,
  the arguments to the runtime's `rt_str_*` helpers, and the compile-time folds that ask for a Go
  string.
- A fold that *produces* a string (concatenation of constants, a string-method fold, a folded
  slice) now interns its result, because the caller is holding a value, not a literal.
- The runtime interns the bytes of anything it made up itself — a `+` concatenation or a slice
  computed at runtime — so an index always means an entry, including for text no literal wrote.

Two consequences had to be settled at the same moment, because both are the same representation
change seen from the other end:

- **Printing** an index must read the text back (`call i8* @rt_str_ptr(i32)`), so `print(f"hi {n}")`
  interpolates through `%s`/`rt_str_ptr`. Printing an index with `%d` is a silent wrong answer
  (`hi 0` for `hi world`), and printing an index is exactly what a method's string result used to do.
- **Element-kind facts** must follow the value. A list of strings stores indices; printing one of
  them must ask the facts (`listElemStr`/`setElemStr`/`dictValStr`, and the static
  `exprIsString` for a comprehension element), not the runtime flag that only covers printing the
  whole container.

## What the change pulled up underneath it

- **The static dispatch wrote arguments into the middle of a call.** The emitter began
  `  %t = call i32 @gy_G_greet(i32 %self` and then evaluated each argument — which is fine while an
  argument is a register, and broken the moment interning needs an instruction of its own. Arguments
  are now evaluated into registers first. The same bug in the `super()` path was fixed the same way.
- **The filtered comprehension loop named the wrong `phi` predecessor.** With a filter, the
  increment lives in the block the skips fall through to, but the loop header declared the body as
  its back edge. `llc` said `PHI node entries do not match predecessors`. This was the *second*
  bug under the L11.8 refusal: the refusal had been covering an invalid CFG as well as the
  comparison, and only removing the refusal showed both.
- **A bare string statement no longer keeps a literal alive.** A discarded `"hi"` is a no-op, so
  `--opt-level 3` can still collect a dead `@.strN` — the optimisation-level test that asserts the
  module shrinks stayed meaningful only once value interning skipped statements that produce
  nothing.
- **Classes must learn that an attribute holds text.** `self.w = "hi"` in `__init__` registers
  `C.w` as string-valued, so `print(C().w)` outside the class reads the text. The scan walks nested
  statements, because an attribute assigned inside an `if` is still an attribute of the class.
- **Methods returning strings were never registered.** Module functions had
  `strReturningFuncs`; a method's return type is discovered from its own `return` statements and
  recorded under the mangled symbol, which is what tells the printer and the comparison that the
  `i32` they hold is text.

## Alternatives rejected

- **Keep both representations and coerce at each operator** (what an earlier repair attempt tried,
  reverted in ADR 0163). It needs a `select` on every string operand and it makes `x == "hi"` a
  two-instruction question with two meanings. It was reverted once already; re-adding it was not
  arguable.
- **`ptrtoint` the global into an `i32`** and treat the address as an index-like value. That is
  pointer arithmetic posing as a value model: two equal literals interned twice would compare
  unequal, and any `icmp` against an interned index would be nonsense.
- **Leave the L11.8 refusal on the comprehension filter** now that the comparison works. A refusal
  that hides a `phi` bug is the same mistake as an invalid module with better manners (ADR 0166):
  fix the CFG and assert the shape positively.

## Follow-ups recorded, not smoothed over

- **Gap R.46** — `print(out[0])` for a freshly built comprehension list prints its index. The
  element-kind fact exists once the list has been walked; the same program one statement earlier
  prints `0` where CPython prints `a`.
- **Gap R.45** — the *interpreter* answers `s[1]` with `98`: a string subscript yields the character
  code where CPython yields `"b"`. Wrong on the human path, in a shape the compiled path refuses.
- **Gap R.22** — `str(x) == "5"`, `sorted(["b","a"])[0]` and other compiled string holes still
  refuse (asserted to refuse *with a message*, never silently).

## Checks

- `pkg/lang/string_value_test.go` — artifact assertions: an interned literal in value context, no
  `icmp eq i32 … @.str` anywhere in a module, the f-string reading text back through `rt_str_ptr`, a
  method returning `%t` rather than `@.str`, and no intern call torn into an operand list.
- `pkg/lang/comprehension_test.go` — the filtered comprehension compiles, verifies, and its
  induction `phi` names the block that actually branches to it.
- `integration/string_value_test.go` — the shapes against CPython on both engines, with the two
  divergences above pinned at what the compilers do, and the remaining holes asserted to refuse
  with a message (exit 1, not exit 2, per `docs/operations.md`).
- `integration/programs/string_values.gy`, `str_loop_eq.gy`, `comp_str_filter.gy` — parity
  programs; the first two are promoted probes whose debt the matrix no longer carries.
