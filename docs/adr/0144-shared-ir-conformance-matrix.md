# 0144 — Shared IR / conformance matrix

- Status: accepted
- Date: 2026
- Deciders: agent loop (Round 16)

## Context and problem statement

gusty has **two independent lowering backends** that consume the same AST:

- the AST **interpreter** (`EvalExpr`, `pkg/lang/jit.go`), and
- the **LLVM AOT compiler** (`Compile`, `pkg/lang/codegen.go`).

They were built separately, so there is **no shared IR** between them and no
single place where their semantics are pinned down. The result is semantics
drift: a construct can be correct in one backend and wrong, silently ignored,
or unsupported in the other (e.g. dispatch is statement-level in AOT only).
Drift was previously caught only ad hoc, by a handful of inline parity tests
that covered a subset of cases and produced no stable, machine-readable record.

## Decision

We add a **shared lowering spec** (`docs/shared-lowering-spec.md`) that pins the
semantic contract both backends must meet, and a **conformance matrix** that runs
**every** whole-program integration case through **both** backends and diffs the
stdout.

The matrix:

1. has a **canonical case registry** (`integration/conformance_cases.go`):
   every standalone `programs/*.gy` plus every multi-file merge (ctrl, data,
   features, whole, math), all marked shared surface;
2. runs each case through **both** backends — `lang.InterpreterRun` (interpreter)
   and `lang.Compile` → `llc` → `cc` → run (AOT) — and diffs stdout;
3. records the outcome in a **machine-readable JSON matrix**
   (`lang.ConformanceMatrix`, schema v1.0, emitted to
   `integration/conformance-matrix.json`);
4. **asserts parity**: every shared case must produce byte-identical stdout on
   both backends; a failing row fails the test.

The matrix is the mechanical enforcement of the shared lowering spec. The JSON
artifact is deterministic for a given registry + toolchain, so a script can diff
two runs to detect a *new* drift as a parity row flipping from pass to fail.

## Consequences

- **Positive**: every integration case is now a parity check, so a backend
  change that drifts semantics fails `TestConformanceMatrix`. The contract is
  documented, and the machine-readable artifact gives agents/scripts a stable
  schema (`pkg/lang/conformance.go`) to consume.
- **Positive**: the registry is the single source of truth for whole-program
  cases; new constructs are added as a case and must stay green on both backends.
- **Negative**: running both backends on every case adds a small CI cost (each
  AOT half shells out to `llc`/`cc`). The interpreter half runs in-process.
- **Trade-off**: non-shared surface (generators/yield, arbitrary decorators,
  multi-file build semantics) is documented as divergence and **excluded** from
  the matrix rather than asserted. Today every registered case is shared and
  passes parity (19/19).
- **Risk**: `lang.InterpreterRun` redirects process-wide `os.Stdout`, so the
  matrix is not safe for concurrent use; it runs sequentially.

## Alternatives considered

- **A shared IR layer** (both backends lower to one common IR, then interpret
  and codegen it): higher cost — would require rewriting both backends, and
  changes the interpreter's semantics rather than verifying them. Rejected for
  this round; the matrix *detects* drift without forcing one IR.
- **Golden stdout files** per case compared against one backend only: would not
  verify the two backends agree with each other, only that one backend matches a
  frozen file. Rejected — the contract is *backend-to-backend* equality.
