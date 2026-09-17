# ADR 0091: list-destructuring match patterns

## Decision

Extend `match` with **list-destructuring patterns**: a case pattern that is a
list literal (e.g. `case [a, b]:`) matches the subject **element-wise**:

- The subject must be a heap list with the same arity as the pattern.
- A `Name` pattern element binds that name to the subject's corresponding
  element (stored in `e.Vars`, since match bodies run at global scope).
- A non-Name element is evaluated and compared element-wise against the
  subject's element.
- On arity/kind mismatch the case does not match (falls through).

Previously, match patterns were expressions compared for structural identity
(`pv == sub`); a `case [a, b]:` would only match if the literal `[a,b]` was
already bound to the same heap id as the subject — not a destructuring bind.

## Motivation

The mission's "pattern matching" item benefits from real structural patterns:
binding pattern variables to the elements of a matched container is the core
of destructuring. This makes `match` genuinely useful for unpacking lists
(e.g. `match x: case [a, b]: a + b`).

## Agentic rationale

A machine-readable, predictable pattern-matching surface: `case [a, b]:`
unpacks deterministically, and the bound names are observable via `e.Vars`.

## Tests

`pkg/lang/match_test.go`:

- `match [10,20]: case [a,b]: a+b` -> 30 (binds a=10, b=20, sums).
- A 3-element subject vs a 2-element pattern falls through to a wildcard
  `case _:` -> 99.

## Alternatives rejected

- Nested destructuring (`case [[a, b], c]:`): deferred — the top-level
  element-wise bind covers the common case; nested lists would need recursive
  pattern compilation.
