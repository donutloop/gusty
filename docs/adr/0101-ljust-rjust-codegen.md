# ADR 0101: codegen `ljust`/`rjust` folding

## Decision

Add the string methods `"s".ljust(w)` and `"s".rjust(w)` to the AOT codegen
**string-method dispatch** (not the builtin Call switch), matching how the
parser represents method calls: the receiver is resolved as a constant
string and `c.Args[0]` is the constant width.

- `ljust` pads the receiver on the **right** with spaces to width `w`.
- `rjust` pads on the **left** with spaces to width `w`.
- Both are no-ops when `len(s) >= w`.

The result is a padded string global via `strConst`, mirroring the
interpreter.

## Motivation

`ljust`/`rjust` were interpreter-only string methods; the codegen method
dispatcher fell through to "unsupported string method". Both are pure
string-width padding folds over constant receiver + constant width.

## Tests

- `TestIRLjustRjustFolds` (ircheck): llc-verifies `"ab".ljust(5)` folds to a
  `c"ab   \00"` string global, `"ab".rjust(5)` to `c"   ab\00"`, and the
  `len(s) >= w` no-op cases fold to the unchanged receiver.
- `TestStdlibLjustRjust` (stdlib): interpreter behavior checks via
  method-call syntax.

## Alternatives rejected

- Adding to the builtin Call switch as `ljust(s, w)`: rejected — the parser
  represents string methods as method-call nodes; the Call-switch path is
  unreachable for the real syntax (CLI confirmed "unsupported string method").
