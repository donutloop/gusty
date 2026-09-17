# ADR 0100: codegen `chr`/`ord` folding

## Decision

Add `chr` and `ord` to the AOT codegen builtin dispatch, folding constant
literal arguments:

- `chr(n)`: a constant integer codepoint folds to a single-character string
  global via `string(rune(n))`, mirroring the interpreter.
- `ord(s)`: a constant string folds to the codepoint of its **first byte**
  (`int64(s[0])`), mirroring the interpreter's `sval[0]` (not a rune).

## Motivation

`chr`/`ord` were interpreter-only; the codegen fell through to "unsupported
call". Both are pure scalar conversions over literals, so they fold cleanly
at codegen time.

## Tests

- `TestIRChrOrdFolds` (ircheck): llc-verifies `chr(65)` emits a `c"A\00"`
  string global, `chr(97)` emits `c"a\00"`, and `print(ord("A"))` /
  `print(ord("hello"))` fold to `65` / `104`.
- Existing `TestStdlibChrOrd` already covers interpreter behavior.

## Alternatives rejected

- Folding `ord` to the first rune rather than first byte: rejected — must
  mirror the interpreter exactly (`sval[0]`).
