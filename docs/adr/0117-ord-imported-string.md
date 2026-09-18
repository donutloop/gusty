# ADR 0117: ord of imported string module globals in AOT

## Context
ADR 0114/0115/0116 made imported string module globals fold to constants,
print, and len correctly. `ord(mod.str)` of an imported string module
global still fell through the `ord` builtin (it only handled `stringConst`
literals/variables), so the first-byte value was unavailable.

## Decision
In the AOT `ord` builtin, resolve an `*Attr` argument against the imports
registry: if `mod.str` folds to a `StrLit`, return its first byte value
(`str.Value[0]`, error on empty string). Mirrors the interpreter's `ord`.

## Consequences
- `ord(msg.msg)` of imported string globals returns the first byte value.
- The imports registry is reused consistently (stringVal, len, ord).
