# ADR 0087: machine-readable JSON schema for AST/IR dumps

## Decision

Publish a single JSON Schema (draft-07) document describing gusty's two
structured outputs — the JSON AST dump (`--emit-ast`) and the LLVM IR text
dump (`--emit-llvm`) — exposed via `pkg/lang.ASTIRSchema` and a new `--schema`
CLI flag.

The schema is a self-describing contract for agents: `gustyc --schema` prints
the document, so a downstream agent can validate a `--emit-ast` dump against
it (`required: ["stmts"]`, per-node required fields) before planning a
compilation, without reading the compiler source.

## Agentic rationale

The mission's "agentic/JSON/schema paths" need a deterministic, machine-
readable interface. The AST dump is already JSON; a schema makes it
*self-describing* and *validatable*. The IR dump is LLVM IR text (validated
by `llc`/`opt`), so it is documented inside the same document as
`definitions.irDump` with `contentMediaType: text/plain` — one artifact
covers both outputs.

## Structure

- `pkg/lang/schema.go` defines `ASTIRSchema`, a draft-07 document with
  `definitions.stmt` (16 statement kinds) and `definitions.expr` (17
  expression kinds), each listing its required fields per Go's default struct
  JSON tags. Node kinds are identified by required-field signatures (there is
  no explicit `kind` discriminator), so the schema uses `oneOf`.
- `cmd/gustyc/main.go` adds `--schema`, printing `lang.ASTIRSchema` to stdout
  before source-dependent dispatch.

## Alternatives rejected

- Emitting a `kind` discriminator per node: rejected — it would change the
  existing `--emit-ast` JSON shape; the schema describes the current dump.
- A separate IR schema: rejected — the IR is text, not JSON; documenting it as
  a media type inside the same document is simpler and keeps one `--schema`
  artifact.

## Tests

- `pkg/lang/schema_test.go`: schema is valid JSON; a real parsed program's
  AST dump carries the required fields named in the schema; `irDump` is
  `text/plain`.
- `cmd/gustyc/main_test.go` (`TestCLISchema`): `--schema` prints valid
  draft-07 JSON with `definitions`.
