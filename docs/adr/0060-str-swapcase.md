# ADR 0060: String `.swapcase()` in interpreter + AOT codegen

## Decision

Add `str.swapcase()` — swap the case of each rune — in **both** paths,
mirroring Python's `str.swapcase`. A shared package-level `swapcase(s string)`
helper uses `strings.Map` with `unicode.IsUpper`.

## Details

- **Interpreter**: the `callStrMethod` `swapcase` case returns
  `allocStr(swapcase(s))`.
- **Codegen**: `swapcase` folds in `stringConst` and `stringVal`
  (`return swapcase(v), true`) and in the attr `call` dispatch
  (`v = swapcase(v)`), so `print(len("HeLLo".swapcase()))` emits `i32 5`.

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
