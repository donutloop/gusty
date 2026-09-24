# Round L4.7 — Token-stream cursor API

- Added a shared, bounds-safe `Cursor` over `[]Token` in `pkg/lang/token.go`:
  `peek(n)` lookahead (returns `TokEOF` past the end, never panics), `next()`,
  `mark()`/`reset()` backtracking, `position()`, and the
  `atEOF`/`atNewline`/`atDedent`/`atIndent`/`skipNewlines` predicates.
- The parser now walks the shared cursor instead of its own `toks []Token` +
  `pos int` — the token stream and spans are a single source of truth shared
  with the formatter and LSP (roadmap L4.7). All lookahead that previously
  indexed `p.toks[p.pos+n]` now goes through `peek(n)`.
- Fixed two latent shadowing bugs introduced when rewiring lookahead: the
  assignment-detection and keyword-arg-detection probes now use a distinct
  `tk` so the target/keyword name `t` is not replaced by the `=` token.
- Unit tests: `pkg/lang/cursor_test.go` (lookahead stability, mark/reset
  backtracking, EOF safety, newline predicates).
- `go test ./...` green across `pkg/lang`, `integration`, and `cmd/gustyc`.

