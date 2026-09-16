# ADR 0072: String `.rsplit(sep)` in interpreter

## Decision

Add `str.rsplit(sep)` — split the string on `sep` from the right —
mirroring Python's `str.rsplit`. With no `maxsplit`, all separators are
used, so the parts are the same as `split(sep)`.

## Details

- **Interpreter**: the `callStrMethod` `rsplit` case takes 1 or 2
  arguments, evaluates the separator (a string), splits via
  `strings.Split`, and returns a new list of string elements (mirroring
  `split`'s list construction).

## Scope

- Interpreter path only: codegen list construction for method results is not
  folded yet (matching split's list-return limitation).
