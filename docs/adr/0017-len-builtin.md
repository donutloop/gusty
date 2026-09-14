# ADR 0017: `len(list)` builtin

## Decision
`len(list)` returns the element count of a list object, complementing the
generator/list feature. The semantic analyzer types `len` as returning an int.

## Codegen/IR implications
- Evaluator: `len` builtin returns `len(o.elems)` for a `list` heap object.
- Semantic analyzer: `len` is a builtin name typed `TInt()`.

## Alternatives rejected
- None — this is a minimal companion builtin to the list feature.
