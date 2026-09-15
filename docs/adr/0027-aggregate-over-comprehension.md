# ADR 0027: Aggregate builtins over lowered comprehensions

## Decision

Extend the LLVM AOT codegen's aggregate builtins `len`, `sum`, `min`, `max` to
accept a lowered comprehension result (`*Comp`) in addition to an inline list
literal (`*ListLit`).

## Codegen / IR implications

- A comprehension over a constant iterable already unrolls to a
  `{i32 count, [n x i32]}` global struct with the same shape as a list literal,
  so the aggregates reuse the existing lowering.
- `comp()` now records the folded element values (`compEls map[*Comp][]int64`)
  alongside the count (`compLen`) and global name (`compNames`).
- `len(comp)` emits a `getelementptr` + `load` of the stored count field,
  mirroring the `*ListLit` case exactly.
- `sum`/`min`/`max` over a comprehension fold the folded elements (which are
  all constants, since the iterable and body are constant-folded) to a single
  constant at codegen time — no runtime loop. `sum([x for x in range(5)])`
  folds to `10`.

## Alternatives rejected

- Emitting runtime add/select instructions over the comprehension's global
  struct — rejected because the elements are already constants, so folding to a
  single constant is simpler and produces optimal IR with no runtime loop.
