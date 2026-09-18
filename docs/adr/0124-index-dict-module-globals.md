# ADR 0124: indexing imported dict module globals in AOT

## Context
ADR 0121 folded dict module globals (`d = {1: 10}`). `cfg.d[k]` still
fell through the `Index` path (its `*Attr` branch handled only folded
`ListLit`), so folded dict values were unavailable.

## Decision
In the AOT `Index` path's `*Attr` resolution branch, also accept a folded
`DictLit`: compare integer keys (`dictLiteralKeys`/`dictLiteralVals`) and
return the matching value. Mirrors the interpreter's integer-keyed dict
index.

## Consequences
- `cfg.d[k]` of imported dict globals folds to the value.
- The imports registry is reused consistently (stringVal, len, ord,
  reversed, Index).
