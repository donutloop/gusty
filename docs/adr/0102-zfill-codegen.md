# ADR 0102: codegen `zfill` folding

## Decision

Add the string method `"s".zfill(w)` to the AOT codegen **string-method
dispatch** (method-call syntax, like ljust/rjust in ADR 0101): the receiver is
resolved as a constant string and `c.Args[0]` is the constant width.

- `zfill` pads the receiver on the **left** with `0` to width `w`.
- No-op when `len(s) >= w` (mirrors the interpreter).

The result is a zero-padded string global via `strConst`.

## Motivation

`zfill` was an interpreter-only string method; the codegen method dispatcher
fell through to "unsupported string method". It is a pure string-width
padding fold over constant receiver + constant width.

## Tests

- `TestIRZfillFolds` (ircheck): llc-verifies `"ab".zfill(5)` folds to a
  `000ab` string global and the `len(s) >= w` no-op folds to the unchanged
  receiver.
- `TestStdlibZfill` (stdlib): interpreter behavior checks via method-call
  syntax.

## Alternatives rejected

- Adding to the builtin Call switch: rejected — the parser represents string
  methods as method-call nodes; the Call-switch path is unreachable for the
  real `"s".zfill(w)` syntax (same rationale as ADR 0101).
