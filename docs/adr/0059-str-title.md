# ADR 0059: String `.title()` in interpreter + AOT codegen

## Decision

Add `str.title()` — capitalize the first rune of each whitespace-separated
word, lowercase the rest — in **both** paths, mirroring Python's
`str.title`. A shared package-level `title(s string)` helper uses
`strings.Map` tracking the previous rune (a word boundary is a whitespace).

## Details

- **Interpreter**: the `callStrMethod` `title` case returns
  `allocStr(title(s))`.
- **Codegen**: `title` folds in `stringConst` and `stringVal`
  (`return title(v), true`) and in the attr `call` dispatch
  (`v = title(v)`), so `print(len("hello world".title()))` emits
  `i32 11`.

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
