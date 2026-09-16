# ADR 0054: String `.lstrip()` / `.rstrip()` in interpreter + AOT codegen

## Decision

Add `str.lstrip()` and `str.rstrip()` — remove leading / trailing whitespace
(any `unicode.IsSpace` rune) — in **both** paths, mirroring Python's
`str.lstrip()` / `str.rstrip()`.

## Details

- **Interpreter**: the `callStrMethod` `lstrip`/`rstrip` case applies
  `strings.TrimLeftFunc(s, unicode.IsSpace)` /
  `strings.TrimRightFunc(s, unicode.IsSpace)` and returns an `allocStr`.
- **Codegen**: `lstrip`/`rstrip` are folded in three places — `stringConst`,
  `stringVal`, and the attr `call` string-method dispatch — applying the same
  `TrimLeftFunc`/`TrimRightFunc`, so `print(len("  hi  ".lstrip()))` emits
  `i32 4` (the trimmed result "hi  " is 4 chars). This required adding the
  `unicode` import to both `jit.go` and `codegen.go`.

## Scope

- Constant-folding in the codegen (matching the existing string-method
  pattern); interpreter path supports runtime string receivers.

## Alternatives rejected

- Trimming only `" "` via `strings.TrimLeft(v, " ")`: rejected — Python strips
  all whitespace (tabs/newlines), so `unicode.IsSpace` is faithful.
