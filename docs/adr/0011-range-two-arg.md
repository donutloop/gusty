# ADR 0011: Two-argument `range(a, b)` in `for` loops

## Decision
`for i in range(a, b)` iterates `i` from `a` to `b-1`. The `range` call is
recognized structurally (a `Call` whose `Fn` is a `Name` `"range"` with exactly
two args) and lowered to `i = a; while i < b: ...; i++` in both the interpreter
and the LLVM codegen path.

## Agentic rationale
`range(a,b)` is the most common Python loop shape. A structural check (rather
than a packed return value) keeps the interpreter simple and lets codegen
compute `start` and `stop` as independent IR operands without a synthetic
multi-value type.

## Codegen/IR implications
- Interpreter: `rangeBounds(iter)` returns `(start, stop)`; the `for` loop runs
  `i` from `start` to `stop`.
- LLVM: `rangeBounds` emits `start`/`stop` operands; the for-loop init block
  stores `start` and the cond block compares `i < stop` (signed). IR remains
  `llc -opaque-pointers` compilable.

## Alternatives rejected
- A packed `start*N+stop` integer (fragile, overflow-prone).
- A runtime `range` object/value type (no boxed range type exists yet).
