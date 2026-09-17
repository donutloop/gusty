# ADR 0094: structured typed `--eval --json` result

## Decision

The `--eval --json` output now carries a dynamic `type` field:

    {"result": "...", "type": "int", "exit": 0}

The type comes from the evaluator's dynamic type inference (`typeOfVal`), via
a new public `Evaluator.TypeOf(v)` wrapper, and is rendered by the Type's
`Name()` (e.g. `int`, `float`, `str`, `list`, `dict`, `set`, `closure`).

## Agentic rationale

The mission's agentic/JSON path needs a structured, machine-readable result:
agents get the value's *dynamic type* alongside its repr, without re-deriving
it from the repr string. This makes the JSON output self-describing.

## Tests

`cmd/gustyc/main_test.go` (`TestCLIJSONType`): `--json --eval="x = 42\nx"`
parses as JSON with `type == "int"` and `exit == 0`.

## Alternatives rejected

- Exposing `typeOfVal` directly (private): rejected — a public `TypeOf`
  wrapper keeps the CLI on the public API.
