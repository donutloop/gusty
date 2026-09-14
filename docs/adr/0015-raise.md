# ADR 0015: `raise` statement and exception type names

## Decision
`raise Exception` (optionally with an expression) signals a runtime error. A
`try:` body that encounters a `raise` (or builtin error) is handled by the first
matching `except` clause (`Exception` is the catch-all); `finally:` always runs.

## Agentic rationale
`raise` turns try/except into a real control-flow feature: agents can signal and
handle their own failures instead of only catching builtin errors. `Exception`
is registered as a builtin exception type name so `raise Exception` type-checks.

## Codegen/IR implications
- Interpreter: `RaiseStmt` returns an `EvalError{Msg:"raised"}`; `TryStmt` scans
  except clauses on error (catch-all `Exception` matches any).
- Semantic analyzer: `exceptions` map registers `Exception` as a valid name.
- LLVM codegen returns "unsupported" for `raise`/`try` (documented).

## Alternatives rejected
- A full exception type hierarchy (ValueError, TypeError, ...) before catch-all
  semantics are settled (deferred).
