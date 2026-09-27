# ADR 0176: incremental parsing reuses both sides of an edit, and says what it did

## Status

Accepted.

## Context

L5.8 asked for "a stable parse tree keyed by spans so the LSP and REPL can
re-parse only edited ranges". `ParseCache` existed and preserved node identity,
but only for the statements *before* the edit: `Update` re-parsed everything
from the first affected statement to the end of the file. Editing the first
line of a 21-statement file reused **0** statements and re-parsed 21 — measured,
not guessed:

```
PROBE reused=0 stmts=21 identity_kept=false
```

For an editor that is the worst case, because it is the common case: the cursor
sits somewhere in the document and everything below it gets re-analysed on every
keystroke. The Phase 9 plan (L9.1 incremental JIT REPL, L9.4 richer LSP) both sit
on top of this, so a prefix-only cache would cap them too.

Two more defects were sitting in the same path:

- `didChange` ignored `cache.Update`'s error and fell back to
  `d.cache.SetText(last.Text)`. For an *incremental* change, `last.Text` is only
  the edited region — the fallback replaced the whole buffer with a fragment.
- Nothing told the client whether the server had re-parsed one statement or the
  whole file, which is exactly the thing an editor (or an agent watching the
  server) wants to know.

## Decision

**Reuse the tail as well as the prefix, and only under conditions that keep the
reused nodes' spans truthful.**

`Update` now splices three regions: preserved prefix statements, a re-parsed
middle, and preserved tail statements. Tail reuse requires all of:

- exactly one edit;
- the edit lies within one line and inserts no newline — because a reused node
  keeps its `Span`s, and inserting a line would leave every statement below it
  reporting the old line numbers. Stale spans in hover and diagnostics are worse
  than re-parsing;
- the bytes after the edit are **identical text**, verified by comparing
  `src[old:]` with `newSrc[new:]`, not assumed from a length difference.

When those hold, the tail statements are the *same AST nodes* as before, shifted
by a constant byte delta, and only the statements overlapping the edit go through
the parser (`parseTopLevelRange` parses a token window rather than the rest of
the file). The same edit now reuses 20 of 21 statements and keeps node identity.

**A change that cannot be applied keeps the buffer and says so.** `Update`
failure sets `Document.staleChange`; the server retains the previous text and
publishes a warning ("could not apply the last incremental change; resend the
full document"). It never replaces the document with `last.Text`, because that is
a fragment when the change is incremental.

**The parser reports its own work.** `publishDiagnostics` gained a
`parseCache` object — `{statements, reusedStatements, incremental}` — so the
self-report travels on the notification that is already going out. Reuse is
verified by comparing the incremental program against a full parse of the same
text, in every reuse test.

## Consequences

- **The editor loop gets cheaper in proportion to document size**, which is what
  L9.1 (incremental JIT) and L9.4 (richer LSP) need before they can be honest
  about "only the edited ranges".
- **`Reused()` means something new**: statements preserved on *either* side of
  the edit. Two tests asserted the old prefix-only number and were updated to
  the new one (`1 → 3`, `1 → 2`) alongside identity assertions, so the change is
  pinned rather than silently loosened.
- **Reuse is conservative by design**: multi-line edits, pasted blocks, and
  multiple edits in one notification all take the full re-parse path. The
  fallback is the behaviour we already had, so the new code can only help.
- **Stale spans are treated as a correctness bug, not a perf win** — the guard
  is a line-count condition, and there is a test that fails if a node survives a
  line insertion.

## Alternatives rejected

- **Prefix-only reuse (status quo)** — rejected: re-parses the whole tail on the
  most common edit, and leaves L9.1/L9.4 built on a cache that does not actually
  bound its work.
- **Reuse the tail for any edit, adjusting spans afterwards** — rejected: a
  reused node's whole subtree would need a span rewrite (every token offset in
  every nested expression), which costs about as much as re-parsing and risks
  diagnostics pointing at the wrong line — the failure mode users notice.
- **Content-hash the statement text and match statements by hash** — rejected:
  hash matching cannot prove a statement boundary is where the old one was once
  the token stream shifts, and a wrong boundary silently merges or splits
  statements. Comparing the tail's bytes and requiring a token boundary is
  stricter.
- **Skip the error when the update fails** — rejected: it left the buffer
  holding a fragment, which is a data-loss bug in an editor.

## References

- `pkg/lang/incremental.go` — `ParseCache.Update`, `tailReuse`,
  `parseTopLevelRange`, `tokenIndexAtByte`
- `pkg/lang/incremental_test.go` — `TestParseCacheReusesTailAfterLineEdit`,
  `TestParseCacheTailReuseStopsWhenLinesMove`,
  `TestParseCacheBoundariesShiftWithTheEdit`, `TestParseCacheTailReuseOnDeletion`,
  `TestParseCacheMultiLineEditReParsesTheTail`
- `pkg/lang/lsp.go` — `parseCacheStats`, `Document.staleChange`
- `pkg/lang/lsp_test.go` — `TestLSPPublishesParseCacheStats`,
  `TestLSPFailedIncrementKeepsBuffer`
- `docs/operations.md` — the `parseCache` notification field
- ADR 0172 (L.1 first-class `None`) — earlier cycle in this loop
