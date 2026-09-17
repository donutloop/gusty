# ADR 0099: codegen `sorted` folding

## Decision

Add `sorted` to the AOT codegen builtin dispatch. `sorted(list)` folds an
**inline list literal of integer literals** to a sorted list global:

- ascending by value by default (mirrors the interpreter's `lessVal`
  int-ascending order);
- descending when a second `reverse=True` keyword arg (or a truthy positional
  second arg) is present; `reverse=False` keeps ascending.

A new `constIntVal` helper resolves a literal (`IntLit`/`BoolLit`/`FloatLit`
truncation/`NoneLit`) to a compile-time int for the reverse flag truthiness.
The folded result is a new `ListLit` emitted via `emitList`.

## Motivation

`sorted` was interpreter-only; the codegen fell through to "unsupported
call". This closes the gap for constant list literals, which is the only
list representation the AOT backend supports (integer-element globals).

## Tests

- `TestIRSortedFolds` (ircheck): llc-verifies bare `sorted([3, 1, 2])`
  expressions and asserts the folded global IR is ascending `[i32 1, i32 2,
  i32 3]`, descending `[i32 3, i32 2, i32 1]` for `reverse=True` / truthy
  positional, and ascending again for `reverse=False`.
- `TestGenSorted` (gen): extended with reverse keyword, positional truthy,
  and `reverse=False` behavior checks via the interpreter.

## Alternatives rejected

- Sorting non-literal or string-element lists at codegen: rejected — the AOT
  backend folds only constant int-element list globals.
