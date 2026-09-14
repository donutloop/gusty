# ADR 0014: `try` / `except Exception` / `finally`

## Decision
`try:` evaluates a body; if the interpreter reports a runtime error, the first
matching `except` clause (by type name; `Exception` is the catch-all) runs; the
`finally:` body always runs afterward.

## Agentic rationale
Try/except is the core Python resilience ergonomic for agent programs that must
handle failure without aborting the whole evaluation.

## Codegen/IR implications
- Interpreter: the `try` body is evaluated via `evalBody`; on error the except
  clauses are scanned (catch-all `Exception` matches any), and `finally` always
  runs. Implemented in the interpreter (REPL/`--eval`) path.
- Semantic analyzer already analyzes try/except/finally bodies.
- LLVM codegen returns "unsupported" for try statements (documented).

## Alternatives rejected
- A structured exception object/type system before the catch-all semantics are
  settled (deferred).
