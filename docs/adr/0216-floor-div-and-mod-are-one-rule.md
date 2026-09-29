# ADR 0216 — `//` and `%` are one rule

Status: accepted (closes roadmap Gaps R.28 and R.30)

## Context

`print(-7 // 2)` printed `-3` in the compiled backend and `-4` in the interpreter; `print(-7 % 2)`
printed `-1` on **both**. CPython: `-4` and `1`.

The operator-kinds work (ADR 0215) found this by accident: it needed a matrix of operand shapes run
against CPython, and arithmetic on negative operands fell out of the same sweep. The measurement, on
704 cases:

| engine | integer `//` | integer `%` | float `//` | float `%` |
|--------|--------------|-------------|------------|-----------|
| interpreter | correct | **44 wrong** | correct | **14 wrong** |
| compiled | **44 wrong** | **44 wrong** | correct | **14 wrong** |

Two details made this worth an ADR rather than a two-line patch.

**The pair is self-consistent when wrong.** Go's `/` and `%` truncate, and truncation satisfies
`a == (a // b) * b + (a % b)` just as flooring does. So a test suite that checks each operator
against its own table can certify a *consistently wrong pair* — which is what had happened:
`integration/lang_test.go` asserted `-3.5 % 2.0` was `-1.5` and `-5.0 % 2.0` was `-1.0`, with
comments naming `frem` as the reason. Those tests had been written from the emitted IR, not from the
language, and they were the reason nobody noticed.

**`frem` is not Python's `%`.** LLVM's `frem` and libm's `fmod` (Go's `math.Mod`) are the *truncated*
remainder. Flooring the floats needed the same correction as the ints, plus the IEEE detail that an
exact remainder carries the divisor's sign — `7.5 % -0.5` is `-0.0`, and printing `0.0` for it is
printing a different number.

## Decision

**One definition of flooring, used by every consumer.** In `pkg/lang/jit.go`:

```go
func floorDiv(a, b int64) int64  // quotient steps down when there is a remainder and signs differ
func floorMod(a, b int64) int64  // remainder carries the divisor's sign
func floorModFloat(a, b float64) float64  // same, with copysign(0, b) for an exact remainder
```

- The interpreter's integer `/`-family and float `%` paths call them.
- The codegen **constant folder** calls them, so `print(-7 // 2)` folded at compile time cannot be a
  third opinion disagreeing with both backends.
- The emitted module says the same thing in IR: `sdiv`/`srem` followed by `icmp`/`xor`/`and` and a
  `select` that steps the quotient down or adds the divisor back; for doubles, `frem` then
  `@llvm.copysign.f64` for the exact case and `fadd` for the correction. The guards from ADR 0212
  (`guardNonZeroInt`) are emitted inside these paths too — an early `return` that skipped them was
  caught by the existing division-trap tests, which is the value of having them.
- `/` is untouched: it is true division (ADR 0212's neighbourhood) and always a float. Restoring its
  `sdiv` case in the i32-only path keeps `1 / 0` trapping there without pretending that path is
  where true division belongs.

**Tests assert the invariant, not just the tables.** `TestFloorIdentityHoldsForEverySign` checks
`a == (a // b) * b + (a % b)` through the language over the sign grid, precisely because a truncating
pair satisfies per-operator tables and this one. The grids are also run through CPython on both
backends (`integration/floor_division_test.go`), and the module is checked *structurally* for the
corrections — a future "simplification" back to a bare `srem` fails even if behaviour tests were again
written from the emission.

**The two wrong pins stay as evidence, corrected.** The failing assertions were not deleted: the
comments now say what they pinned, why it was wrong, and that the values came from `python3`. A test
that agrees with the implementation is the most expensive kind of test, because it launders the bug
into a requirement — and this is the second one this loop has found in three cycles (the property
generator's `9 + [1, 7, 6]` was the first).

## Alternatives rejected

- **Fix only the interpreter and record the compiled half.** Rejected: unlike the operand-kind gap,
  the compiled fix is a dozen lines of `select`, and a numeric rule that differs per backend is the
  single worst kind of divergence to leave open — both engines are internally consistent, so nothing
  downstream complains.
- **Emit a runtime call (`gy_floordiv`, `gy_floormod`) instead of inline corrections.** Rejected for
  now: the inline form keeps the optimiser able to fold sign-known cases, and the codegen already
  prefers straight-line arithmetic for `//`/`%` in hot loops. If a shared helper appears later it
  should host the rule for both backends, and these tests move with it.
- **Truncate and document it as a C-like dialect choice.** Rejected: the language's promise is
  Python-shaped ergonomics, and `//` exists *only* to mean floor division — `-7 // 2 == -3` makes
  the operator a synonym for `/`, so the divergence would be the whole reason the operator exists.
- **Test the operators separately, as before.** Rejected, on the evidence: separate tables certified
  a wrong pair for the entire life of the feature, and two tests had been written from the IR.
