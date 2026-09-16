# ADR 0078: String `.rsplit([sep[, maxsplit]])` optional maxsplit

## Decision

Extend `str.rsplit` to accept an optional `maxsplit` argument — split at
most `maxsplit` separators from the right — mirroring Python's
`str.rsplit(sep, maxsplit)`.

## Details

- **Interpreter**: the `callStrMethod` `rsplit` case takes 1 or 2 arguments
  (`sep`, and optionally `maxsplit` as an int). With `maxsplit >= 0` and
  `maxsplit < len(parts)-1`, the left-most `len(parts)-maxsplit` parts are
  joined with `sep` as the first element and the remaining last parts are
  appended individually; otherwise all parts are returned.
- Example: `"a-b-c-d".rsplit("-", 1)[0]` is "a-b-c".

## Scope

- Interpreter path only: codegen list construction for method results is not
  folded yet.
