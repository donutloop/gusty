# ADR 0073: String `.removeprefix(prefix)` / `.removesuffix(suffix)` in interpreter

## Decision

Add `str.removeprefix(prefix)` / `str.removesuffix(suffix)` — the string
without the prefix / suffix when it is present, else unchanged — mirroring
Python 3.9's `str.removeprefix` / `str.removesuffix`. The interpreter
applies `strings.TrimPrefix` / `strings.TrimSuffix`.

## Details

- **Interpreter**: the `callStrMethod` `removeprefix`/`removesuffix` cases
  require exactly 1 argument, evaluate it to a `str`, and return
  `allocStr(strings.TrimPrefix(s, prefix.sval))` /
  `allocStr(strings.TrimSuffix(s, suffix.sval))`.

## Scope

- Interpreter path only: codegen string methods with string args are not
  folded for these new methods yet.
