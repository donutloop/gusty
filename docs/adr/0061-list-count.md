# ADR 0061: List `.count(value)` in interpreter

## Decision

Add list `.count(value)` — the number of occurrences of `value` in the list —
mirroring Python's `list.count`. The interpreter evaluates the value arg and
scans the list's `elems`, comparing each element by string content via
`dictKeyEq` (so string elements match by content, not boxed id).

## Details

- **Interpreter**: the `callListMethod` `count` case requires exactly 1
  argument, evaluates it, scans `o.elems`, increments a counter for each
  `dictKeyEq` match, and returns the count.
- Elements may be strings or ints: `dictKeyEq` compares string keys by
  content and int values directly.

## Scope

- Interpreter path only: codegen list method support is limited (append is
  interpreter-only), so `.count` is not folded in codegen yet.
