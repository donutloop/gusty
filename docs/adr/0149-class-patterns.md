# ADR 0149 — Class patterns in `match`

## Status

Accepted (interpreter-side; AOT/codegen limitation documented).

## Context

`match` supports literal, capture (`_`/name), guard, or-pattern, and dict
patterns. Class patterns (`case Point(x, y):`) — matching an instance of a
class and binding its attributes — were the remaining Phase 6 follow-up.

## Decision

Implement class patterns in the **parser** and **interpreter**:

- `parsePatternAtom` detects `Name(...)` after an ident and builds a `Call`
  AST node whose arguments are attribute-name `Name` nodes. This is
  unambiguous because call-expressions were previously the only fallback and
  match patterns never evaluated calls.
- `matchPattern` (jit.go) adds a `*Call` case: resolve the callee to a class
  id via `resolveClassID` (definition name in `classIDs`, or a class value in
  a variable); require the subject to be an `instance` whose class chain (via
  the `classIDs` base walk) contains the pattern class; then for each argument
  name look up the instance attribute and bind a same-named capture variable.
  A missing attribute or a non-matching class fails the pattern.
- Non-class `Call` patterns retain the prior expression-equality semantics.

## Alternatives

- Positional-order binding (Python's `case Point(x, y)` binds the class's
  positional attrs in order): rejected because the interpreter does not track
  attribute order; same-name binding is simpler and covers the common case.
- Keyword attrs (`case Point(x=0)`): rejected for now; positional
  same-name binding is the minimal, testable surface.

## Consequences

- Class patterns are an interpreter feature. The AOT/codegen backend lowers
  `match` to expression-equality only (no instance/class runtime checks), so
  class patterns are documented as interpreter-only in `docs/language.md`.
