# ADR 0121: dict module globals in AOT data imports

## Context
ADR 0119/0120 folded list module globals and indexing. Dict globals
(`d = {1: 10}`) were rejected as non-constant, so imported constant dicts
were unavailable.

## Decision
Extend `foldConst` with a `*DictLit` case: fold each key and value
recursively (against the module's own globals and the shared imports
registry) and return a `DictLit` of folded entries. `mod.d` reads then emit
the folded dict. (String keys are deferred — the AOT dict representation
requires integer keys.)

## Consequences
- Imported constant dicts (integer-keyed) compile to folded dicts.
- Numeric/string/list folding is unchanged.
