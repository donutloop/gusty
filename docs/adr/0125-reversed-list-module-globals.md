# ADR 0125: reversed of imported list module globals in AOT

## Context
ADR 0119/0120 folded list module globals. `reversed(cfg.l)` of an imported
list module global still fell through the `reversed` builtin (its `*Attr`
branch handled only folded `StrLit`).

## Decision
In the AOT `reversed` builtin's `*Attr` resolution branch, also accept a
folded `ListLit`: reverse it via `reversedExprs` and emit via `g.emitList`.

## Consequences
- `reversed(cfg.l)` of imported list globals folds to the reversed list.
- The imports registry is reused consistently.
