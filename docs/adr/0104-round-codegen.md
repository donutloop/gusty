# ADR 0104: codegen `round` folding

## Decision

Add the scalar builtin `round(x)` to the AOT codegen builtin Call switch:
a constant integer literal argument folds to itself (returns the value
unchanged), mirroring the interpreter's `round` int case.

The AOT backend has no float representation, so `round` folds only integer
literals (an int rounded is the int itself). The result is an i32 constant.

## Motivation

`round` was interpreter-only; the codegen fell through to "unsupported
call". The int-literal case is a pure identity fold.

## Tests

- `TestIRRoundFolds` (ircheck): llc-verifies `print(round(42))` folds to
  `42` and `print(round(-7))` folds to `-7`.
- Existing `TestStdlibRound` already covers interpreter behavior.

## Alternatives rejected

- Folding float args at codegen: rejected — the AOT backend has no float
  representation (float is interpreter-only).
