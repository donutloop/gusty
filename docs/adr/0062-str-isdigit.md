# ADR 0062: String `.isdigit()` in interpreter + AOT codegen

## Decision

Add `str.isdigit()` — 1 if every rune is a digit and the string is
non-empty, else 0 — in **both** paths, mirroring Python's `str.isdigit`.
The interpreter checks `unicode.IsDigit`; the codegen constant-folds it to
an `i32` literal.

## Details

- **Interpreter**: the `callStrMethod` `isdigit` case returns 0 on an empty
  string or any non-digit rune, else 1.
- **Codegen**: the attr `call` dispatch adds an `isdigit` case folding the
  receiver `v` to `fmt.Sprintf("%d", res)` where `res` is 1 only when `v`
  is non-empty and every rune is a digit (`print("123".isdigit())` emits
  `i32 1`).

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
