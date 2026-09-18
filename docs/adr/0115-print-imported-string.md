# ADR 0115: print of imported string module globals in AOT

## Context
ADR 0114 added string module globals and `+` concatenation to AOT data
imports. `print(mod.str)` of a folded string module global compiled
incorrectly: the print arg-type detection (`stringVal`) only recognized
`StrLit`, string variables, and concat expressions — not `Attr` (module
global) — so the string fell into the integer `%d` path, emitting
`printf("%d", i32 gep)` (invalid IR).

## Decision
Extend `stringVal` with an `*Attr` case: if the attribute expression is
`mod.str` where `mod` is an imported module whose folded global `str` is a
`StrLit`, return that string. The print loop's `%s` path then emits
`printf("%s", i8* gep)`.

## Consequences
- `print(mod.str)` of imported string globals produces correct output.
- Folded string constants from module attrs are recognized as strings in
  all `stringVal` consumers (print, string comparisons, etc.).
