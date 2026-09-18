# ADR 0119: list module globals in AOT data imports

## Context
ADR 0112-0118 made AOT data imports fold numeric, string, and concat module
globals. List globals (`l = [1, 2, 3]`) were rejected as non-constant, so
imported constant lists were unavailable.

## Decision
Extend `foldConst` with a `*ListLit` case: fold each element recursively
(against the module's own globals and the shared imports registry) and return
a `ListLit` of folded elements. `mod.list` reads then emit the folded list.

## Consequences
- Imported constant lists compile to folded lists.
- Numeric/string folding is unchanged.
