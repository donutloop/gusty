# ADR 0153 — Numeric-literal modernization

## Status
Accepted (implemented in this cycle).

## Context
The lexer only accepted decimal integer/float literals (`123`, `1.5`). The
roadmap (L4.4) calls for hex/binary/octal literals (`0xFF`, `0b101`, `0o17`)
and `_` digit separators (`1_000`, `0x_FF`), mirroring Python's syntax.

## Decision
Extend the lexer's number scanner (`lexNumber`) to:

- Detect base prefixes `0x`/`0X` (16), `0b`/`0B` (2), `0o`/`0O` (8).
- Scan digits per base via `scanDigits`, permitting a single `_` separator
  between digits, or once right after a non-decimal base prefix.
- Compute values exactly at lex time with `strconv.ParseInt`/`ParseFloat`,
  returning lexical errors on overflow or misplaced separators.
- Preserve the original literal spelling on the token `Text` (and on the
  `IntLit`/`FloatLit` AST nodes via a new `text` field), so the canonical
  formatter round-trips `0xFF`, `0b101`, `1_000`, etc. verbatim.
- Add `text` to the AST JSON (`-emit-ast`) for machine consumers.

## Consequences
- Both the interpreter and the AOT backend agree on modern-literal values.
- Formatter output no longer normalizes away base prefixes or separators.
- Large literals remain limited by the language's i32 AOT integer model;
  hex/binary/octal values above the int32 range truncate in AOT (a
  pre-existing limitation shared by decimal literals).

## Alternatives
- Parsing in the parser rather than the lexer — rejected: values flow from
  tokens through parser/jit/codegen unchanged, so lex-time computation is
  the single point of truth.
- Dropping the `text` field and re-printing `0xFF` as `255` — rejected: it
  breaks formatter round-trip fidelity.
