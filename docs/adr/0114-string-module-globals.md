# ADR 0114: string module globals & concatenation in AOT data imports

## Context
ADR 0112/0113 introduced data imports in AOT, folding module top-level
globals to compile-time constants. `foldConst`/`foldBin` only folded
numeric globals; string globals and `"a" + "b"` concatenation were rejected
as non-constant, so layered string constants could not be imported.

## Decision
Extend the AOT data-import constant folder to handle `StrLit` globals and
`+` string concatenation in module global expressions:
`foldBin("+", StrLit, StrLit)` returns the concatenated `StrLit`. This
mirrors the interpreter's string `+` semantics.

## Consequences
- Layered string constants (`greet = "hello"`, `msg = greet + "!"`) fold to
  constants in compiled programs.
- Numeric folding is unchanged; string concat requires both operands to be
  `StrLit` (mixed concat stays deferred).
