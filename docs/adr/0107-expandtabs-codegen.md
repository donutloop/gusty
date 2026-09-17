# ADR 0107: codegen `expandtabs` folding

## Decision

Add the string method `"s".expandtabs(w)` to the AOT codegen **string-method
dispatch** (method-call syntax, consistent with ADR 0101-0103/0105): the
receiver is resolved as a constant string `v` and `c.Args[0]` is the constant
width.

Each tab is replaced with the spaces to the next tab stop at width `w`,
tracking the running column — mirroring the interpreter's tab-stop algorithm.
Source string literals have **no escape sequences**, so literal receivers
contain no tabs (the fold is a no-op for them); the interpreter can build
tabs via `chr(9)`.

## Motivation

`expandtabs` was an interpreter-only string method; the codegen method
dispatcher fell through to "unsupported string method". It is a pure
tab-to-space fold over constant receiver + constant width.

## Tests

- `TestIRExpandtabsFolds` (ircheck): llc-verifies `"abc".expandtabs(4)` folds
  to the unchanged `abc` (tab-free literal receiver) and verifies cleanly.
- `TestStdlibExpandtabs` (stdlib): interpreter behavior via `chr(9)`
  construction — `(chr(9) + "b").expandtabs(4)` expands to `"    b"`.

## Alternatives rejected

- Rejecting `expandtabs` because tabs can't appear in source literals:
  rejected — the fold mirrors the interpreter exactly, closes a real gap, and
  the interpreter's chr(9) path demonstrates the algorithm.
