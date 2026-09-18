# ADR 0116: len of imported string module globals in AOT

## Context
ADR 0114/0115 made imported string module globals fold to constants and
print correctly. `len(mod.str)` of an imported string module global still
fell through the `len` builtin (it only handled `StrLit`, string variables,
and lists), so the folded length was unavailable in compiled programs.

## Decision
In the AOT `len` builtin, resolve an `*Attr` argument against the imports
registry: if `mod.str` folds to a `StrLit`, return its length
(`len(str.Value)`). This mirrors the interpreter's `len(string)`.

## Consequences
- `len(msg.msg)` of imported string globals returns the folded length.
- The imports registry is reused consistently (stringVal, len) for
  module-attr resolution.
