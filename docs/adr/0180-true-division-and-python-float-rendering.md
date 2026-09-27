# ADR 0180: `/` is true division, and floats render like Python's

## Status

Accepted.

## Context

Two Python-compatibility defects, both invisible to the parity harness because
the two backends agreed on the wrong answer:

1. **`/` truncated.** `7 / 2` evaluated to `3`, `1 / 4` to `0`, `a / b` for
   integers was documented as identical to `//`. That is C's operator, not
   Python's: since PEP 238, `/` is true division and `//` is floor division.
   Every program that divided silently lost its fraction — and `x /= 2` in the
   docs was literally "integer divide".
2. **Floats printed as integers.** `print(2.0)` printed `2`, `print(1.0 + 2.0)`
   printed `3`, `print(x * 2.0)` printed `2`. A float result was
   indistinguishable from an integer one. Worse, the two formatting tools each
   failed differently: `%g` truncated significant digits (`0.123456789` →
   `0.123457`) and `%.17g` invented them (`0.1` → `0.10000000000000001`).

The Python oracle added over the last cycles made both obvious: with the corpus
compared against CPython rather than against the other backend, three programs
flipped to matching as soon as the operators and the rendering were fixed.

## Decision

**Match Python on both, in both backends.**

- `a / b` yields a **float** whatever the operands are. The interpreter allocates
  a float box; the AOT marks the node float (`isFloat` returns true for `/`
  unconditionally), which routes it through the existing double machinery —
  `sitofp` + `fdiv` — and everything downstream (assignment, printing, further
  arithmetic) follows the float path it already had.
- `a // b` keeps floor semantics, including **flooring rather than truncating**
  for negatives: `-7 // 2` is `-4`, not `-3` (Go's `/` on ints truncates, so the
  interpreter now divides in floating point and floors).
- `float(x)`, `str(f)`, `print(f)` and f-strings share one renderer. Go-side it is
  `pyFloatRepr`: positional notation for `1e-4 ≤ |f| < 1e16` (Python's range, not
  Go's, which disagreed at 1e15), shortest round-tripping digits, `.0` when
  integral, `inf`/`-inf`/`nan` spellings, and the sign preserved for `-0.0`.
- At run time in the compiled backend, `@rt_fmt_double` does what printf cannot:
  it tries the shortest precision (15 → 17) whose text round-trips through
  `strtod`, then appends `.0` when the value is finite, integral, and small
  enough to be rendered positionally. Text-scanning for the `.0` decision was
  tried first and got it wrong (`3.5` came back `3.5.0`); deciding from the
  *value* is both smaller and correct.
- Two instruction-level lessons, recorded because they cost time: an LLVM
  **instruction** `getelementptr` takes no parentheses (the parenthesised form is
  for constant expressions), and a runtime helper must live in a block that is
  actually emitted — `rt_fmt_double` first landed in `heapRuntimeIR`, so a
  float-only program referenced an undefined function. It now has its own
  `floatRuntimeIR`, pulled in by a `floatFmtUsed` flag.

## Consequences

- The language gained its first real PEP-238 semantics: `print(7 / 2)` → `3.5`
  and `print(84 / 2)` → `42.0` in both backends, cross-checked against CPython by
  `integration/division_test.go` (every expectation is asserted against what
  CPython prints, not remembered).
- Many golden expectations changed — `4` → `4.0`, `2` → `2.0`, `sqrt(9)` → `3.0`.
  They were updated *after* checking CPython for each, not to make tests pass.
- `x //= 2` had to be separated from `x /= 2` in the parser: they were collapsed
  to one operator while `/` truncated. `//=` keeps floor semantics, `/=` is true
  division.
- The corpus's Python-oracle match count rose (32 of the 35 Python-valid programs
  now match byte-for-byte), and the remaining divergences are each individually
  documented rather than merely tolerated.
- **Remaining AOT gaps (Gap P.1)**, pinned in `TestKnownAOTDivisionGaps` with
  both the correct answer *and* the compiled backend's current wrong one, so a
  fix registers as a failing test rather than a surprise: AOT `//` on negative
  integers truncates; `x /= 2` keeps the integer representation; a float passed
  to an untyped parameter truncates to int. Also open: `floor`/`ceil` return a
  float where Python's `math.floor` returns an int (Gap P.2), and float `%` uses
  truncated rather than floored semantics.

## Alternatives rejected

- **Keep `/` truncating and add a separate `truediv` operator** — rejected: the
  language's premise is Python-shape ergonomics; a new spelling for the common
  case is the wrong way round.
- **Print floats with `%g` plus a ".0" patch in the caller** — rejected: `%g`
  loses digits (0.123456789 → 0.123457) and `%.17g` fabricates them (0.1 →
  0.10000000000000001); neither is a rendering of the number.
- **Decide the trailing `.0` by scanning the formatted text** — rejected after it
  mis-fired (`3.5` → `3.5.0`): the scan is fragile in IR, and the condition is a
  property of the value (finite, integral, positionally rendered).
- **Fix only the interpreter** — rejected: AOT is a first-class backend, and a
  formatting split would break the parity harness in the direction that hides
  bugs.
- **Let the failing tests encode the old behaviour** — rejected: the expectations
  in `lang_test.go` and the `features.txt`/`data.txt` goldens recorded `%g`
  output, not Python's; each was re-derived from CPython before updating.

## References

- `pkg/lang/floatrepr.go` — `pyFloatRepr`; `pkg/lang/floatrepr_test.go`
- `pkg/lang/jit.go` — true division and floored `//` in the interpreter
- `pkg/lang/codegen.go` — `isFloat` for `/`, `floatRuntimeIR` / `rt_fmt_double`,
  `floatFmtUsed`
- `pkg/lang/parser.go` — `/=` and `//=` are distinct operators
- `integration/division_test.go` — Python-cross-checked cases plus
  `TestKnownAOTDivisionGaps`
- ADR 0178 (string literals are text), ADR 0179 (the CLI reports its backend) —
  the oracle and the explicit `--aot` flag are what surfaced this
- ADR 0022 (`//` floor division in the AOT codegen) — the decision this replaces
