# ADR 0105: codegen `index` folding

## Decision

Add the string method `"s".index(sub)` to the AOT codegen **string-method
dispatch**: the receiver is resolved as a constant string `v` and
`c.Args[0]` is the constant substring (via `g.stringVal`).

- Returns the byte index of `sub` in the receiver (`strings.Index`).
- The interpreter raises on not-found; the AOT codegen has **no error
  channel**, so it folds to `-1` on not-found (mirroring `find`'s sentinel).

The result is an i32 constant.

## Motivation

`index` was an interpreter-only string method; the codegen method dispatcher
fell through to "unsupported string method". The found case is a pure
strings.Index fold over constant receiver + constant substring.

## Tests

- `TestIRIndexFolds` (ircheck): llc-verifies `print("abcabc".index("bc"))`
  folds to `1` and the not-found case folds to `-1`.
- `TestStdlibIndex` (stdlib): interpreter behavior check (found case).

## Alternatives rejected

- Folding the not-found case to an error/raise: rejected — the AOT backend
  has no error channel, so `-1` mirrors `find`'s sentinel and keeps the IR
  verifiable.
