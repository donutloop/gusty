# ADR 0096: static gradual typing — annotation/inferred-type mismatch check

## Decision

Extend the semantic analyzer (`Analyze`) to check **assignments whose target
has a type annotation** against the inferred type of the assigned value:

- `x: int = "hello"` now reports a static `type mismatch: expected int, got
  str` diagnostic from `--verify`.
- `x: any = [1, 2]` reports nothing (the dynamic `any` annotation accepts any
  inferred type).
- A concrete annotation differing from the inferred type reports a
  `LevelError` diagnostic.

`EvalExpr` now returns a Go error for the first fatal diagnostic (instead of a
bare `nil` third return), so runtime callers observe the mismatch early.

## Motivation

The mission's "gradual typing" item had only runtime annotation enforcement
(`checkAnnot`). The static analyzer tracked types in scopes but never compared
annotations against inferred types, so `--verify` returned "ok" for
`x: int = "hello"`. This makes gradual typing statically observable.

## Tests

`pkg/lang/semantic_test.go` (`TestStaticAnnotMismatch`): `x: int = "hello"`
produces a diagnostic; `x: any = [1,2]` produces none.

## Alternatives rejected

- Runtime-only enforcement: rejected — `--verify` is the agentic static
  surface and should catch annotation mismatches without evaluation.
