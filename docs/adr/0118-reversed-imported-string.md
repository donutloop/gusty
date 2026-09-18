# ADR 0118: reversed of imported string module globals in AOT

## Context
ADR 0114-0117 made imported string module globals fold to constants, print,
len, and ord correctly. `reversed(mod.str)` of an imported string module
global still fell through the `reversed` builtin (it only handled literal
`StrLit`/lists), so the reversed value was unavailable.

## Decision
In the AOT `reversed` builtin, resolve an `*Attr` argument against the
imports registry: if `mod.str` folds to a `StrLit`, return the reversed
string via the existing `strConst(reverseStr(...))` path. Mirrors the
interpreter's `reversed(string)`.

## Consequences
- `reversed(msg.msg)` of imported string globals folds to the reversed
  string.
- The imports registry is reused consistently (stringVal, len, ord,
  reversed).
