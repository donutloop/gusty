# ADR 0066: String `.isalnum()` in interpreter + AOT codegen

## Decision

Add `str.isalnum()` — 1 if every rune is alphanumeric and the string is
non-empty, else 0 — in **both** paths, mirroring Python's `str.isalnum`.
The interpreter checks `unicode.IsLetter`/`unicode.IsDigit`; the codegen
constant-folds it to an `i32` literal.

## Details

- **Interpreter**: the `callStrMethod` `isalnum` case returns 0 on an empty
  string or any non-alphanumeric rune, else 1.
- **Codegen**: the attr `call` dispatch adds an `isalnum` case folding the
  receiver `v` to `fmt.Sprintf("%d", res)` where `res` is 1 only when `v`
  is non-empty and every rune is a letter or digit
  (`print("abc123".isalnum())` emits `i32 1`).

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
