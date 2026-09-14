# ADR 0012: Loop `else` clauses (runs on normal completion, skipped by `break`)

## Decision
`while`/`for` loops accept an optional `else:` block that runs only when the
loop terminates normally (condition false / range exhausted) and is skipped when
the loop exits via `break`.

## Agentic rationale
Loop-`else` is a distinctive Python ergonomic that lets agents express
"search succeeded/failed" control flow without a separate flag variable. The
semantic contract (else runs iff no break) is simple to state and test.

## Codegen/IR implications
- AST: `WhileStmt.Else` and `ForStmt.Else` (`[]Stmt`).
- Interpreter: a `completed` flag is set false on break; else body runs when
  `completed` remains true.
- LLVM: the loop's normal-exit branch (cond-false / `i >= stop`) targets an
  `else` block (when present) which falls through to the join label; `break`
  branches directly to the join label, skipping else. IR stays `llc
  -opaque-pointers` compilable.

## Alternatives rejected
- A `break`-flag variable checked at loop exit (needs extra state and a join
  merge point).
- Treating else as a plain `if` after the loop (cannot distinguish break).
