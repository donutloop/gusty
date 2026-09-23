# 0145 — Language Server (LSP) over stdio

- Status: accepted
- Date: current
- Deciders: agent loop
- Ticket: roadmap Phase 8 — Language server / LSP

## Context

Editors need live diagnostics, hover, and completion for gusty source. The
roadmap's Phase 8 item asks for a Language Server. The interpreter already has
a parser (`Parse`) and a semantic analyzer (`Analyze`) that attaches inferred
types to AST nodes, so an LSP can reuse them directly.

## Decision

Implement a stdio LSP server (`gustyc --lsp`) speaking JSON-RPC 2.0 with
Content-Length framing, in `pkg/lang/lsp.go`. It:

- parses + semantically analyzes each open buffer into a `Document` and
  pushes `textDocument/publishDiagnostics` on open/change;
- builds a symbol index (`docIndex`) by walking the AST: definitions
  (functions, classes, variables, parameters, loop variables) and references
  (Name nodes + attribute names);
- serves `textDocument/hover` (inferred type + docstring) and
  `textDocument/completion` (module-level + scope-local names + builtins),
  using the language's own 1-based rune spans converted to 0-based UTF-16
  LSP positions.

The server is synchronous and single-threaded — adequate for a small language
and simple to reason about. The CLI wires `--lsp` to `lang.RunLSP(os.Stdin,
os.Stdout)`.

## Consequences

- Editors get diagnostics, hover, and completion without a separate daemon.
- The symbol index is rebuilt on every change (full sync, `textDocumentSync=1`),
  which is fine for small buffers.
- Positions are converted rune→UTF-16 so non-ASCII identifiers remain correct.
- Follow-on: incremental sync, textDocument/definition, and references.
