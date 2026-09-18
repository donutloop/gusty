# ADR 0126: arithmetic on indexed imported list globals in AOT

## Context
ADR 0119/0120 folded list module globals and indexing. `cfg.l[0] + cfg.l[1]`
folds to the sum via the existing Index-`*Attr` resolution and BinOp
constant folding, but lacked unit/integration coverage.

## Decision
Add an end-to-end integration test (TestExecImportListArith) and an
interpreter-vs-AOT parity unit test (TestIRImportListArithInterpVsAOT)
covering `cfg.l[0] + cfg.l[1]`.

## Consequences
- Indexed imported list elements fold in arithmetic.
- Interpreter-vs-AOT parity verified for this path.
