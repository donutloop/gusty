# ADR 0090: standard-library builtins any/all/chr/ord/round

## Decision

Add five standard-library builtins to the evaluator's builtin dispatch:

- `any(iter)` — 1 if any element is truthy, else 0.
- `all(iter)` — 1 if all elements are truthy, else 0.
- `chr(n)` — the single-character string for codepoint `n` (via `allocStr`).
- `ord(s)` — the codepoint of the first character of `s` (reads `o.sval[0]`).
- `round(x)` — identity for ints; truncates floats toward zero.

These complete common Python-programmer-facing scalar/boolean builtins that
were missing from the standard library (the evaluator already had
`len/print/range/min/max/zip/int/float/str/sum/abs/sorted/reversed/enumerate`).

## Agentic rationale

The mission's "standard library" item benefits from covering the most-used
builtins programmers reach for. `any`/`all` are the boolean quantifiers over a
list; `chr`/`ord` give scalar↔codepoint conversion; `round` rounds floats.
Each is small, well-understood, and testable.

## Implementation

All follow the existing builtin pattern: `arg, err := e.eval(n.Args[0])`
(err handled), read the list/string/float from `e.heap`, compute, and return.
`any`/`all` iterate a list's `elems` (`v != 0` truthiness). `chr` builds a
1-char string via `allocStr`. `ord` returns `int64(o.sval[0])`. `round`
returns the float's `fval` truncated for float heap objects, else the raw int
unchanged.

## Tests

`pkg/lang/stdlib_test.go` (`TestStdlibAnyAll`, `TestStdlibChrOrd`,
`TestStdlibRound`): truthiness quantifiers over `[0,0,1]`/`[0,0,0]`/
`[1,1,1]`/`[1,0,1]`; `ord(chr(65)) == 65` round-trip; `round(7) == 7`.

## Alternatives rejected

- `enumerate` was already present; the new builtins avoid overlap.
- `input`/`open` (I/O builtins): deferred — they require a stream/IO model
  beyond the pure evaluator (see ADR 0001 for the portable, no-external-IO
  design).
