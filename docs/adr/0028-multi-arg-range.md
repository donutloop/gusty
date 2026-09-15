# ADR 0028: Multi-argument range iterables in comprehensions

## Decision

Support `range(start, stop)` and `range(start, stop, step)` as comprehension
iterables in both the interpreter (`pkg/lang/jit.go`) and the LLVM AOT codegen
(`pkg/lang/codegen.go`).

## Codegen / IR implications

- The AOT `comp()` iterable handling now accepts 1-, 2-, or 3-argument `range`
  calls. It folds the constant start/stop/step, rejects a zero step, and
  unrolls the comprehension body over the stepped range at compile time — so
  `sum([x for x in range(1, 5, 2)])` folds to `1 + 3 = 4` with no runtime loop.
- The interpreter's `rangeBounds` now treats 2- or 3-argument range as
  `(start, stop)`; the comprehension eval computes the step from the third
  argument and iterates with it (positive or negative). The `range` builtin
  accepts 1-3 arguments so `eval(range(...))` no longer rejects multi-arg calls.

## Alternatives rejected

- Emitting a runtime stepped loop in the AOT path — rejected because the
  comprehension body is already constant-folded and unrolled at compile time;
  folding keeps the emitted module allocation-free and optimal.
