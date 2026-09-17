# ADR 0110: Scalar `min`/`max` arguments in the AOT codegen

## Decision

The AOT codegen (`irGen`) now accepts a single scalar `*IntLit` argument to
`min`/`max`, treating it as a one-element collection and emitting the scalar
value itself: `min(5)` emits 5, `max(7)` emits 7. This matches the
interpreter, which already treated scalar args as single-element collections.

## Motivation

The language spec (`docs/language.md`) states "`min`/`max` accept a list or
set (or a single value)". The interpreter implemented this, but the codegen
rejected scalar args with "min requires an inline list/set/dict literal" — a
backend sync gap (AGENTS.md requires both backends to stay in sync).

## Implementation

In the `min`/`max` case of the codegen call handler, after the
`len(c.Args) != 1` guard, a type switch on `c.Args[0]` checks for `*IntLit` and
returns `fmt.Sprintf("%d", il.Value)`. List/comp/dict/call element
computation is unchanged for non-scalar args.

## Tests

`cmd/gustyc/main_test.go`: `TestCLIEmitLLVMScalarMinMax` checks the emitted IR
contains `i32 5` for `min(5)` and `i32 7` for `max(7)`.
