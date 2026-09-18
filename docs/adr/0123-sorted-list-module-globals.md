# ADR 0123: sorted of imported list module globals in AOT

## Context
ADR 0119/0120 folded list module globals. `sorted(cfg.l)` of an imported
list module global still fell through the `sorted` builtin (it only
handled literal `ListLit`), so the sorted value was unavailable.

## Decision
In the AOT `sorted` builtin, resolve an `*Attr` argument against the
imports registry: if `cfg.l` folds to a `ListLit`, sort it via the
existing literal-list path.

## Consequences
- `sorted(cfg.l)` of imported list globals folds to the sorted list.
- The imports registry is reused consistently.
