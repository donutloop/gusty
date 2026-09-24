# ADR 0152: Standalone type-check mode (`gusty check`)

## Status
Implemented (Round 13).

## Context
The roadmap item "Standalone type-check mode (`gusty check`)" asks for a
`mypy`-style checker that runs the semantic pass on annotated code **without
executing it**. The CLI already had `--verify` (parse + analyze a single
source string), but it was not a project-level, agent-friendly checker with a
deterministic exit contract, and the semantic pass did not yet check argument
types against parameter annotations or return statements against the
`->` annotation.

## Decision
- **Semantic checks**: add two mypy-style checks to `pkg/lang/semantic.go`:
  1. At every call site (`inferUserCall`), compare each statically-typed
     argument (positional, keyword, or default) against the parameter's static
     annotation and emit `argument "x": expected T, got U` on mismatch.
  2. In `analyzeStmt`'s `ReturnStmt` case, compare the inferred return type
     against the enclosing function's `ReturnAnno` and emit
     `return type mismatch: expected T, got U` on mismatch.
  Dynamic (unannotated) values are accepted (gradual typing) to avoid false
  positives.
- **`Check` API** (`pkg/lang/check.go`): `CheckSource`, `CheckFile`,
  `CheckFiles` parse + run `Analyze` and return a `CheckResult`
  (`{files, diagnostics, ok, exit}`). Exit codes are deterministic:
  `0` clean, `1` type errors, `2` parse/usage error.
- **CLI** (`cmd/gustyc/main.go`): a `--check <src>` flag and a
  `gusty check <file1> <file2> ...` subcommand. With `--json` the CLI emits the
  `CheckResult` as JSON for agent consumption. The check path is dispatched
  before the REPL branch and excluded from the REPL trigger condition so the
  flag reliably selects check mode.

## Consequences
- Type errors are now caught at check/compile time for annotated calls and
  returns; the runtime still guards unannotated/dynamic mismatches.
- `gusty check` gives agents a clean, machine-readable type-checking
  interface without running code.
