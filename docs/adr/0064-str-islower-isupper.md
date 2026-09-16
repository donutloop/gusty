# ADR 0064: String `.islower()` / `.isupper()` in interpreter + AOT codegen

## Decision

Add `str.islower()` and `str.isupper()` — 1 if there is at least one cased
rune and all cased runes are lowercase / uppercase, else 0 — in **both**
paths, mirroring Python's `str.islower` / `str.isupper`.

## Details

- **Interpreter**: the `callStrMethod` `islower`/`isupper` cases scan cased
  runes (`unicode.IsLower`/`IsUpper`), track `hasCased` and
  `allLower`/`allUpper`, and return 1 only when both hold.
- **Codegen**: the attr `call` dispatch folds the receiver `v` the same way,
  returning `fmt.Sprintf("%d", res)` (`print("abc".islower())` emits
  `i32 1`).

## Scope

- Constant receivers fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
