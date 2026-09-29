# 0208. Anything stored in a slot stores the interned index, not the global

## Status

Accepted. Implemented this cycle in `pkg/lang/codegen.go` (the unrolled `for … in [ … ]` path); pinned
by `pkg/lang/for_string_ir_test.go`, `integration/for_string_test.go` and
`integration/programs/for_string_chars.gy`. Roadmap Gap R.15.

## Context

```gusty
for c in "ab":
    print(c)
```

The interpreter prints `a` and `b`. The compiled backend did not compile at all:

```
llc-20: error: … global variable reference must have pointer type
  store i32 @.str1, i32* %_c
```

`for x in <literal>` is compiled by unrolling one copy of the body per element, and the element was
lowered with the **scalar** value path (`g.value`), which for a `StrLit` hands back the raw
`@.strN` global. Loop variables live in `i32` slots, so the module asked LLVM to put a global in an
`i32`, which the verifier refuses — and under the exit-code contract (ADR 0006) an LLVM rejection is
**the tool's fault**, not the source's.

The rule that prevents this already existed, in one place. Gap I.2 established that a *container slot*
stores the index into `@str_tab`, not the pointer, because "a stored string" and "a printed string" are
different things in this backend: printing passes `i8* @.strN` to printf directly, storing must pass an
`i32` index, and `g.internedVars` is what tells the printer to render an index as text. `heapElemKind`
is the single helper that does that lowering — interning the text and its Python repr together, and
refusing a string the backend cannot resolve to text with the actionable diagnostic rather than emitting
broken IR.

The loop had simply not been invited to that rule. `for x in ["a","b"]` and `for c in "ab"` (which the
same code turns into a literal list of one-rune strings) were the two remaining places that wrote
element values through the scalar path.

## Decision

**Anything written into an `i32` slot goes through the heap-element lowering; the scalar path is only
for scalars.**

1. The unrolled loop takes each element through `g.heapElemKind(b, el)`, so a string element becomes its
   `@str_tab` index exactly as it would inside a list, and the loop variable is marked interned for the
   duration — the same mechanism that already keeps `print(x)` rendering text rather than an index when
   iterating a list variable (Gap I.2).
2. **Internedness is per element copy, not per loop.** The unroller emits a body copy per element, so the
   flag is set or cleared before each copy and restored after the loop. That is what makes the mixed case
   correct rather than accidentally tolerable:
   ```gusty
   for m in [1, "a", 2]:
       print(m)          # 1 a 2 — on the interpreter, in the binary, and in CPython
   ```
   A single decision taken once per loop would print either an index or a number for half the elements.
3. **Numeric loops stay numeric**, asserted rather than assumed: a literal of ints must not gain an
   `@rt_str_intern2` call, and must still store its constants plainly (`store i32 1, i32* %_x`). The
   guard matters because the new path is *shared* with strings — a rule that "fixes" strings by making
   every element a heap value has simply moved the bug.
4. **Tests read the artifact, not just stdout.** For this class the output test is not enough: before the
   fix the *same* output came from the interpreter while the compiler died, so the assertions check the
   emitted module — no `store i32 @.str`, an `@rt_str_intern2(` call where the text is bound, and the
   definition present — and run LLVM's verifier over the module (ADR 0177's gate, exercised in a test
   rather than only in the CLI).

## Consequences

- `for c in "ab"`, `for w in ["x","y"]`, mixed literals, and `print("<", c, ">")` inside a text loop all
  produce `a b …` identically on the interpreter, in the compiled binary, and in CPython;
  `programs/for_string_chars.gy` joins the ledger as a three-engine parity program
  (`a b c x y 1 a 2 < h > < i > ab cd`).
- **The same investigation localised the sibling defect**, and this is the part worth recording:
  `for c in txt()` where `def txt(): return "hi"` fails with
  *`use of undefined value '@rt_str_intern2'`* — the call is emitted by the "function returns a string"
  path, which returns the interned index (**correct**, ADR 0174) but never sets `g.heapUsed`, so the
  block that *defines* the helper is not emitted. That is the roadmap's R.2 signature
  (`await` + `while True: return "ok"`), reproduced here in a deterministic await-free shape: the bug is
  not the async path's intern accounting, it is that whether a runtime block travels with the module
  depends on each of several call sites remembering a flag. R.2 is re-scoped accordingly, and the fix
  there must be the derived rule (emit the block when the module *references* it) rather than another
  flag set in one more place.
- One consequence of the shared path: `for x in [1, [2]]` — a nested container in an unrolled loop — now
  fails with the actionable "nested container" diagnostic from `heapElemKind` instead of emitting
  something. That is the same ADR 0166 stance as everywhere else, and the L11.1 item stays the place
  where nesting is actually implemented.

## Alternatives considered

- **Refuse string iteration in codegen, with a message** (the cheap option, and what the roadmap
  suggested). Rejected on measurement: the interpreter, CPython, *and* this backend's own list-variable
  path all do the right thing — `xs = ["a","b"]; for x in xs:` compiles and prints `a b`. What was
  missing was not a capability but the one line that applies the existing rule; refusing would have
  removed a working feature to hide a bug in another path.
- **Special-case strings in the loop** (`if element is StrLit then intern`). Rejected: it is the second
  copy of a rule that already has an owner, and the second copy is where the mixed-element case goes
  wrong — the flag would be set once per loop instead of per element.
- **Store the string's bytes into the loop slot** (an inline `[1 x i8]` per character). Rejected: the
  backend has no runtime string values (that is L11.x), so a slot that is not a `@str_tab` index cannot
  be printed, compared, or concatenated consistently with every other string in the language.
- **Fix R.2 and R.15 in one commit.** Rejected as bundling, but the split is recorded here in both
  directions: this commit closes the unrolled-loop store, and the emission rule that closes R.2 follows
  on its own merits with its own tests.
