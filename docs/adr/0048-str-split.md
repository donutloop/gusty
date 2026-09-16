# ADR 0048: String `.split()` folds to substring count

## Decision

Fold `.split()` on constant string literals in the AOT codegen so
`len("a b c".split())` -> 3. The separator defaults to a space (matching the
interpreter's `callStrMethod` default), splitting via `strings.Split`.

## Details

- `dictMethodElems` gains a `split` case: constant-folds `attr.Obj` via
  `stringVal`, takes an optional separator arg (also `stringVal`-folded,
  default `" "`), runs `strings.Split`, and returns the parts as `*StrLit`
  elements. `len`/`sum`/`min`/`max` then fold over them.

## Scope

- Only the element count is foldable in the codegen (the parts themselves are
  strings; emitting a list of string globals at runtime is deferred).
