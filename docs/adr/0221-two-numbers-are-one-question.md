# Two numbers are one question

## Status

Accepted (cycle 169, Gap R.29) — numeric equality in the interpreter. The literal-container defect it
exposed is recorded separately as Gap R.40.

## Context

```gusty
print(1 == 1.0)     # interpreter: 0    compiled: 1    CPython: True
print(1.0 == 1)     # interpreter: 1    compiled: 1    CPython: True
```

The interpreter compared the raw word of an integer against a float object's handle, so an int was
never equal to the float with the same value. `!=` was wrong in the matching way. The compiled backend
answered both correctly, so this was not one implementation being wrong twice — it was two wrongs that
agreed in the direction anyone thought to test, and disagreed in the direction nobody did. The roadmap
entry made exactly that point: parity between the backends is a weak contract, because the two can
agree on a wrong answer.

The measured grid, before the fix: five integers × five floats × six operators, both operand orders —
300 comparisons. The compiled leg was right on all 300. The interpreter was wrong on exactly 8: every
one of them an `==`/`!=` with the **integer on the left and a float of equal value on the right**. All
the ordering operators were fine in both, and the float-on-left direction was fine, which is why this
had survived: a test written from the working direction never sees the broken one.

## Decision

**Equality between two numbers asks one question about their values, in either order.** In
`Evaluator.eqVal`, the float-on-right case now coerces a plain-integer left operand instead of
returning `false`, and refuses to coerce anything that is a handle:

```go
if rf, ok := e.floatOf(r); ok {
    if e.isHandle(l) { return false }   // a container or a string is a different type
    return rf == float64(l)
}
```

Three rules hold this in place rather than four:

- **`isHandle` is the only type test** (ADR 0215). A float is a heap object, an int is an immediate, so
  "is the other side a number?" is "is the other side not a handle?" — the same predicate operators use.
- **`is` does not come through here** (ADR 0217's sibling rule about identity). `is` asks whether two
  names bind one thing; `1 is 1.0` stays 0, and a test pins it, because a coercion added to the wrong
  predicate would look like a passing equality suite.
- **Container equality inherits rather than duplicating**: `[1] == [1.0]` and `{"a": 1} == {"a": 1.0}`
  became right by falling through to element-wise comparison under this same predicate — no second rule
  about elements was written, and the roadmap's stale claim that containers compare by handle was
  corrected by measurement, not by reading.

The compiled path needed no change: it was already right on the grid, in both orders, for literals and
for variables. That asymmetry is recorded here so that nobody "fixes" the compiled side to match the
old interpreter.

## Agentic rationale

`--interp` and `--aot` disagreeing on `1 == 1.0` is the worst kind of answer for an agent: both exits
are 0, both print a single token, and only the token differs. The fix ships with the three-leg test —
the same file run through the interpreter, the compiled binary, and `python3`, with the expectation
computed from the Python rules in the test (`pyNumCompare`) and then **checked against CPython before
it is allowed to judge anybody** — so the numbers cannot be quietly renegotiated from an emission. The
ledger gains `programs/numeric_equality.gy` as a standalone parity program (30 lines of comparisons
printed as `1`/`0`, byte-identical to CPython on both legs), which takes the matrix to 90 cases, 65
parity rows and 46 oracle matches.

## Codegen / IR implications

None — and that is the finding. But writing the tests found two neighbours that are codegen defects,
pinned as **Gap R.40** rather than quietly fixed in this commit:

- `print(1 if [1] == [1.0] else 0)` emits `@.lst2 = private global {i32, [1 x i32]} { i32 1, [1 x i32] [@env_store = internal global ... —
  the literal-list emitter writes a `@` with no name into an int-typed element slot, `llc` says
  *expected type*, and the user gets a toolchain rejection (exit 2) for a program whose answer is 1.
- `print(1 if 1.0 == "a" else 0)` emits `%t222 = sitofp i32 @.str13 to double` — a string global fed to
  a float conversion — same exit 2.

Both belong to the Gap K.10 / ADR 0166 class: the front end should refuse with a diagnostic, not emit a
module `llc` rejects. The same comparisons through variables compile and print correctly, which is what
makes them emitter bugs rather than semantic holes.

## Alternatives rejected

- **Coerce in the parser by making `1.0` an int when it has no fraction.** Rejected: it changes the
  value's type to win a comparison, and `type(1.0)` / `1.0 + 0.1` would then disagree with Python.
- **Box every integer so both sides are objects.** Rejected: it would make the common case (two ints)
  pay a heap indirection to fix a rare one, and this runtime already decided numbers stay words where
  they can (ADR 0160).
- **Teach the interpreter's `==` to try `floatOf` on both sides without the handle test.** Rejected:
  `1 == [1]` and `1.0 == "a"` would compare a number to a pointer-cast-to-double and could answer True,
  which is the same bug wearing a different hat.
- **Fix the container cases in the same commit.** Rejected per the one-commit-per-feature rule: those
  are compiled-emitter defects with their own measurements (Gap R.40), and bundling them would have
  made the numeric change unreadable.
