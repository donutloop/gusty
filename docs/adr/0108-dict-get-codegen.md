# ADR 0108: codegen `get` folding

## Decision

Add the dict method `{k: v}.get(key, default)` to the AOT codegen's
dict-method dispatch in `call` (alongside keys/values): a constant `DictLit`
receiver with a constant int/string key looks up the key/value pairs.

- Found: returns the matching value (via `g.value`).
- Not-found with a default arg: returns the default.
- Not-found without a default: error (mirrors the interpreter's missing-key
  behavior under the backend's no-error-channel convention).

## Motivation

`get` was an interpreter-only dict method; the codegen dict-method dispatch
fell through to "unsupported dict method". It is a pure constant-key lookup
fold over a constant dict.

## Tests

- `TestIRDictGetFolds` (ircheck): llc-verifies `print({1: 42}.get(1))` folds
  to `42` and the not-found-with-default case `print({1: 42}.get(2, 7))`
  folds to `7`.

## Alternatives rejected

- Adding to `dictMethodElems` (Expr-slice path): rejected — `get` returns a
  single value, not an element slice; it belongs in the value-returning
  dict-method dispatch.
