# 0225 — A subscript of a string is a one-character string

- Status: Accepted
- Date: cycle 173 (roadmap Gap R.45; ADR 0210 owns negative positions, ADR 0224 owns the string representation, ADR 0166 owns refusal-vs-invalid-module)
- Affects: `pkg/lang/jit.go` (string subscript, string slice, `len`, `ord`), `pkg/lang/codegen.go` (`value(*Index)` string fold, `exprIsString`, `printsAsInternedStr`, list-literal element tagging, `truthyValue`, the `len`/`ord` folds), `docs/language.md`

## The measurement

Sixteen shapes through both engines, expectations from CPython. Before: **0 of 16** matched on both
legs, and the interpreter — the REPL path a human actually uses — was wrong on all sixteen:

```
s = "abc"
s[1]                 -> 98          (CPython: b)
s[-1]                -> 99          (CPython: c)
s[0] + s[2]          -> 196         ! arithmetic on characters
s[1] == "b"          -> false       ! compiled too: the wrong value, not a refusal
s[1] == 98           -> true        ! Python says false
len(s[1])            -> trap
s[1].upper()         -> trap
ord(s[1])            -> trap
```

One missing type produced all of it. Both backends implemented "the character at position i" as "the
byte at position i", returned as an `int64`/`i32`.

## The decision

**A string is a sequence of code points, and a subscript of it is a one-character string.**

- Interpreter: `s[i]` allocates a one-character string (`allocStr(string(runes[i]))`), with the
  index normalised against the *rune* count — so `s[-1]` is the last character (ADR 0210's rule,
  which had been applied to containers and left pointing at byte indices for strings).
- Codegen: a subscript whose base the compiler can name folds to `rt_str_intern2` of that one
  character — an `@str_tab` index (ADR 0224), not `%d` of a byte. Printing it reads the text back
  through `rt_str_ptr`.
- `len(s)`, `s[a:b]`, `ord(s)` and the subscript count the same unit. The interpreter's string slice
  walked bytes and could cut a multi-byte character in half; `len("café")` answered 5. All four now
  count code points, and `ord` takes the first *rune*.
- Container facts follow the value, as they had to for ADR 0224: `xs = [s[1]]` records that the list
  holds strings (both `heapElemKind`, which reads the emitted shape, and the static `exprIsString`,
  which is the only thing that can see a folded subscript), so `print(xs[0])` prints `b`.

## The thing this uncovered, which is worse than the bug we came for

`truthyValue` — how a condition is lowered — had this in it:

```go
v, err := g.truthOperandErr(b, e)
if err != nil {
    // Mirrors valueText: an un-lowerable condition is reported by the enclosing
    // statement path, which still has the error.
    return g.asI1(b, "0")
}
```

The comment's premise is false for the ternary: `value(*CondExpr)` has no error channel for its
condition. So a condition that could not be lowered became a *false branch*, and this went out the
other end:

```gusty
print(1 if s[1] == "b" else 0)     # emitted `icmp ne i32 0, 0` and printed 0
```

The program was never answered; it was told the answer was `0`. A refusal would have been honest and
an invalid module would have been loud; a plausible wrong value is neither, and a green suite did not
notice because nothing had ever asked that question of that emitter.

The rule, asserted in `TestUnlowerableConditionIsAnErrorNotAFalseBranch`: **a part of a program that
cannot be lowered is a compile error, and never a default value.** All four `truthyValue` call sites
now propagate.

## Alternatives rejected

- **Return an interned index in codegen but keep the interpreter's int**, or vice versa. The two
  backends agreeing with each other is exactly what hid this: only the oracle leg can see that
  `98` is not `b`, so a fix in one leg would have left a permanent backend divergence in the matrix.
- **Represent a character as its code point and make `== "b"` a special case.** That keeps the type
  error and pushes it into every operator; the character *is* a string in this language, and CPython's
  `s[1] == 98` being false is the evidence.
- **Byte indexing with a rune-count special case for non-ASCII.** Two units in one language, chosen
  by whether anyone is looking; `len("café")` would still disagree with `s[3]`.
- **Keep the compile-time refusal for an out-of-range constant subscript as the whole answer.** It is
  the honest thing to do today, and it is recorded as Gap R.37 (a trap the program can name should
  trap at runtime, with CPython's class), not treated as correct.

## Follow-ups recorded

- **Gap R.47** — a compiled string exists only as a compile-time value. `s[i]` with a variable index,
  `len(s[1])`, `s[1].upper()`, `ord(s[1])`, and `for c in s` over a variable string all refuse (each
  asserted to refuse *with a message*, exit 1, never 2). The interpreter answers all of them; the
  compiled leg needs a runtime string object.
- **Gap R.37** (already open) — the out-of-range constant subscript refuses instead of trapping, so
  the compiled exit class is 1 where the interpreter traps with 3 and CPython exits 1 with
  `IndexError`; pinned by class in `TestUncaughtTrapClassesOnAnOutOfRangeCharSubscript`.

## Checks

- `pkg/lang/string_subscript_test.go` — the character's type in the interpreter (comparison against
  text and against a code point, concatenation, `len`, a method on it, `ord`, code points), the
  compiled fold interning rather than formatting, a typed `IndexError`, and the swallowed-condition
  regression.
- `integration/string_subscript_test.go` — ten shapes against CPython on **both** engines; seven
  more on the interpreter alone, so the compiled hole stays visible rather than averaged away; the
  holes asserted to refuse with a message; the exit classes pinned per `docs/operations.md`.
- `integration/programs/string_subscript.gy` — a parity program, all three legs identical.
