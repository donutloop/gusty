# ADR 0077: String `.split([sep[, maxsplit]])` optional maxsplit

## Decision

Extend `str.split` to accept an optional `maxsplit` argument — split at most
`maxsplit` separators — mirroring Python's `str.split(sep, maxsplit)`. The
interpreter uses `strings.SplitN(s, sep, maxsplit+1)` when `maxsplit` is
given, else `strings.Split`.

## Details

- **Interpreter**: the `callStrMethod` `split` case takes 1 or 2 arguments
  (`sep`, and optionally `maxsplit` as an int). With `maxsplit >= 0` it
  splits via `strings.SplitN(s, sep, maxsplit+1)`; otherwise all splits.
- The result is a new list of string elements (existing list construction).

## Scope

- Interpreter path only: codegen list construction for method results is not
  folded yet.
