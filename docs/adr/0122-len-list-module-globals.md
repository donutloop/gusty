# ADR 0122: len of imported list module globals in AOT

## Context
ADR 0119/0120 folded list module globals and indexing. `len(cfg.l)` of an
imported list module global still fell through the `len` builtin (its
`*Attr` branch handled only folded `StrLit`), so the folded length was
unavailable.

## Decision
In the AOT `len` builtin's `*Attr` resolution branch, also accept a folded
`ListLit`: return `len(lst.Elems)`. Mirrors the interpreter's `len(list)`.

## Consequences
- `len(cfg.l)` of imported list globals returns the folded length.
- The imports registry is reused consistently (stringVal, len, ord,
  reversed, Index).
