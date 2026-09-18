# ADR 0120: indexing imported list module globals in AOT

## Context
ADR 0119 folded list module globals (`l = [1, 2, 3]`), but indexing
`cfg.l[i]` still fell through the `Index` path (it only handled literal
`ListLit`/string/dict), so folded list elements were unavailable.

## Decision
In the AOT `Index` path, resolve an `*Attr` argument against the imports
registry: if `cfg.l` folds to a `ListLit`, return `g.value(b, Elems[i])`
for a valid `i`, else a clear error. Mirrors the interpreter's list index.

## Consequences
- `cfg.l[i]` of imported list globals folds to the element value.
- The imports registry is reused consistently (stringVal, len, ord,
  reversed, Index).
