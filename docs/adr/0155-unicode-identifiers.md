# ADR 0155 — Unicode identifiers (XID_Start/XID_Continue)

## Status
Accepted (implemented, roadmap L4.3).

## Context
The lexer originally scanned identifiers byte-by-byte with an ASCII-only
`isIdentStart`/`isIdentChar`, so any non-ASCII identifier (e.g. `café`,
`你好`) was rejected. Unicode TR31 defines `XID_Start`/`XID_Continue` as the
identifier classes. Go's `unicode` package does not export the XID tables, so
we approximate at the category level. Identifiers also need NFC
canonicalization so decomposed and precomposed spellings are one symbol, and
a diagnostic for homoglyph (Greek/Cyrillic lookalike) identifiers.

## Decision
- **Scanning**: decode UTF-8 runes; an identifier starts on `isXIDStart(r)`
  (letters + `Nl` + Other_ID_Start additions: CJK/Kangxi radicals) and
  continues on `isXIDContinue(r)` (start + marks + digits + connector
  punctuation + Other_ID_Continue: middle dots, Hebrew points, Tibetan/Lao
  marks). ASCII-start identifiers may continue through multi-byte runes.
- **NFC**: identifier text is normalized to NFC via
  `golang.org/x/text/unicode/norm` (pinned at v0.3.0 for Go 1.20), so
  `e`+combining-acute and precomposed `é` are the same symbol.
- **Confusables**: a focused homoglyph table maps Greek/Cyrillic lookalikes
  (e.g. `Ο` U+039F → `O`) to their ASCII lookalike. A new `TokWarning` token
  carries the message; the parser drops it from the token stream but records
  it as a `LevelWarning` diagnostic in `prog.Diags`, feeding the CLI/LSP
  warning path. Confusable identifiers remain valid; only a warning is emitted.
- **Keywords**: keyword checks run on the NFC-normalized word (keywords are
  ASCII, so normalization is a no-op for them).

## Consequences
- Non-ASCII identifiers lex and parse; NFC makes spellings canonical.
- Homoglyph attacks surface as warnings, not errors.
- Adds `golang.org/x/text` (v0.3.0) as a dependency and a `TokWarning` kind.

## Alternatives rejected
- Implementing NFC by hand (decomposition/ordering/composition tables) — large
  and error-prone; x/text's `norm` is the standard, small dependency.
- Making confusables a hard error — legitimate Greek/Cyrillic identifiers
  would be rejected; Python precedent is a SyntaxWarning.
