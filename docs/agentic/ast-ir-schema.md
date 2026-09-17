# gusty AST/IR JSON schema (agentic contract)

gusty exposes a machine-readable, self-describing JSON Schema (draft-07) for
its structured outputs. Agents consuming gusty's AST dump can validate and
plan against it without reading the compiler source.

## Retrieve

    gustyc --schema            # prints the JSON Schema document to stdout

## The AST dump (`--emit-ast`)

`--emit-ast` prints the parsed `Program` as JSON: `{"stmts": [...]}`. Each
statement is one of the `stmt` kinds in the schema (`assignStmt`, `ifStmt`,
`call` via `exprStmt`, `funcDef`, `classDef`, `matchStmt`, `tryStmt`,
`importStmt`, `yieldStmt`, `breakStmt`, `passStmt`, `continueStmt`, ...).
Expressions nest recursively via `expr` definitions.

Node kinds are identified by required field names (Go's default struct JSON
tags), not an explicit `kind` discriminator; the schema's `oneOf` lists the
required-field signatures.

## The IR dump (`--emit-llvm`)

`--emit-llvm` prints LLVM IR **text**, not JSON. The schema documents it as
`definitions.irDump` with `contentMediaType: text/plain`; it is validated by
`llc`/`opt`, not by this JSON Schema.

## Validation example

    python3 -c '
      import json, sys
      d = json.load(open("dump.json"))     # --emit-ast output
      assert "stmts" in d                  # schema root: required ["stmts"]
      print(len(d["stmts"]), "statements")
    '
