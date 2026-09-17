# ADR 0098: sorted(iter, reverse=True)

## Decision

Extend the `sorted` builtin to accept an optional second argument controlling
order:

- `sorted(iter)` — ascending (existing).
- `sorted(iter, reverse=True)` — descending. The second argument may be a
  keyword argument (`reverse=True`) or a positional truthy value.

The evaluator's builtin dispatch previously rejected keyword arguments
("unsupported expression"); the sorted case now recognizes a `*KeywordArg`
and evaluates its `.Value`.

## Motivation

`sorted` lacked the common Python `reverse` flag, so descending sort required
a manual `reversed(...)` step. This is a small, well-understood stdlib
completion.

## Tests

`pkg/lang/stdlib_test.go` (`TestSortedReverse`): `sorted([3,1,2],
reverse=True)` produces a nonzero descending result.

## Alternatives rejected

- General builtin keyword-arg support: rejected — scoped to `sorted` to keep
  the change minimal; the KeywordArg AST node is already used for user
  function calls.
