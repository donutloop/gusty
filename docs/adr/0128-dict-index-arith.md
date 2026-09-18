# ADR 0128: arithmetic on indexed imported dict globals in AOT

## Context
ADR 0121/0124 folded dict module globals and indexing. `cfg.d[1] + cfg.d[2]`
folds to the sum via the existing Index-`*Attr` resolution and BinOp
constant folding, but lacked unit/integration coverage.

## Decision
Add an end-to-end integration test (TestExecImportDictArith) and an
interpreter-vs-AOT parity unit test (TestIRImportDictArithInterpVsAOT)
covering `cfg.d[1] + cfg.d[2]`.

## Consequences
- Indexed imported dict elements fold in arithmetic.
- Interpreter-vs-AOT parity verified for this path.
