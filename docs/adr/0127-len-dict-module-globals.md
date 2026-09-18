# ADR 0127: len of imported dict module globals in AOT

## Context
ADR 0121/0124 folded dict module globals and indexing. `len(cfg.d)` of an
imported dict module global still fell through the `len` builtin (its
`*Attr` branch handled folded `StrLit`/`ListLit`).

## Decision
In the AOT `len` builtin's `*Attr` resolution branch, also accept a folded
`DictLit`: return `len(dct.Keys)` (entry count). Mirrors the interpreter's
`len(dict)`.

## Consequences
- `len(cfg.d)` of imported dict globals returns the entry count.
- The imports registry is reused consistently.
