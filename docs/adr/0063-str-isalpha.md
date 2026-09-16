# ADR 0063: String `.isalpha()` in interpreter + AOT codegen

## Decision

Add `str.isalpha()` — 1 if every rune is alphabetic and the string is
non-empty, else 0 — in **both** paths, mirroring Python's `str.isalpha`.
The interpreter checks `unicode.IsLetter`; the codegen constant-folds it to
an `i32` literal.

## Details

- **Interpreter**: the `callStrMethod` `isalpha` case returns 0 on an empty
  string or any non-letter rune, else 1.
- **Codegen**: the attr `call` dispatch adds an `isalpha` case folding the
  receiver `v` to `fmt.Sprintf("%d", res)` where `res` is 1 only when `v`
  is non-empty and every rune is a letter (`print("abc".isalpha())` emits
  `i32 1`).

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
