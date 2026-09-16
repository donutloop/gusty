# ADR 0067: String `.isspace()` in interpreter + AOT codegen

## Decision

Add `str.isspace()` — 1 if every rune is whitespace and the string is
non-empty, else 0 — in **both** paths, mirroring Python's `str.isspace`.
The interpreter checks `unicode.IsSpace`; the codegen constant-folds it to
an `i32` literal.

## Details

- **Interpreter**: the `callStrMethod` `isspace` case returns 0 on an empty
  string or any non-whitespace rune, else 1.
- **Codegen**: the attr `call` dispatch adds an `isspace` case folding the
  receiver `v` to `fmt.Sprintf("%d", res)` where `res` is 1 only when `v`
  is non-empty and every rune is whitespace (`print("   ".isspace())`
  emits `i32 1`).

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
