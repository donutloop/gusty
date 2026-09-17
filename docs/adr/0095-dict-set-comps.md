# ADR 0095: dict/set comprehensions with `for` inside braces

## Decision

Extend `parseDictOrSet` to accept **`{... for x in iter}`** — a dict/set
comprehension whose `for` appears *inside* the braces, matching Python's
syntax and the list-comprehension style (`[x for x in iter]`).

Previously only `{...} for x in iter` (for after the closing brace) parsed;
`{x: x*2 for x in [1,2,3]}` errored with "expected }". Now a `for` token
before the closing `}` triggers the comprehension build (same lowering as the
after-brace form), then the `}` is consumed.

## Motivation

The mission's "comprehensions" item needs Python-shaped dict/set
comprehensions. List comprehensions already accepted `for` inside brackets;
dict/set comprehensions did not, so the feature was unusable in the common
Python idiom.

## Tests

`pkg/lang/stdlib_test.go` (`TestDictSetComprehensions`): `{x: x*2 for x in
[1,2,3]}` and `{x for x in [1,2]}` now parse and evaluate to nonzero results.

## Alternatives rejected

- Keeping only the after-brace `{...} for ...` form: rejected — Python
  programmers write `for` inside braces.
