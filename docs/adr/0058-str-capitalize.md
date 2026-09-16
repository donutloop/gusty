# ADR 0058: String `.capitalize()` in interpreter + AOT codegen

## Decision

Add `str.capitalize()` — uppercase the first rune, lowercase the rest — in
**both** paths, mirroring Python's `str.capitalize`. A shared package-level
`capitalize(s string)` helper (runes: `ToUpper(r[0])` + `ToLower(rest)`) is
used by both the interpreter and the codegen.

## Details

- **Interpreter**: the `callStrMethod` `capitalize` case returns
  `allocStr(capitalize(s))`.
- **Codegen**: `capitalize` folds in `stringConst` and `stringVal`
  (`return capitalize(v), true`) and in the attr `call` dispatch
  (`v = capitalize(v)`), so `print(len("hello".capitalize()))` emits
  `i32 5`.

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.

## Alternatives rejected

- Using deprecated `strings.Title`/`strings.ToTitle`: rejected — neither
  matches Python's capitalize (first rune upper, rest lower), so a custom
  helper is used.
