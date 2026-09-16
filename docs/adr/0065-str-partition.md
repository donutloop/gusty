# ADR 0065: String `.partition(sep)` in interpreter

## Decision

Add `str.partition(sep)` — a list `[head, sep, tail]` split at the first
occurrence of `sep` (or `[s, "", ""]` when absent) — mirroring Python's
`str.partition`. The language has no tuples, so the result is a 3-element
list.

## Details

- **Interpreter**: the `callStrMethod` `partition` case requires exactly 1
  argument, evaluates it to a `str`, finds the first occurrence via
  `strings.Index`, creates a list object (`allocObj("list")`), and appends
  `[head, sep, tail]` (or `[s, "", ""]` when absent). Returns the list id.

## Scope

- Interpreter path only: codegen list construction for method results is not
  folded yet (matching split's list-return limitation).
