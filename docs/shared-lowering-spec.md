# Shared Lowering Spec

This document is the **shared lowering contract** between the two gusty
backends:

- the **AST interpreter** (`lang.EvalExpr`, `pkg/lang/jit.go`), and
- the **LLVM AOT compiler** (`lang.Compile`, `pkg/lang/codegen.go`).

Both backends consume the same parsed AST and lower it independently. Because
they are two separate implementations of the same semantics, they can **drift**:
a construct can be correct in one backend and wrong, silently ignored, or
unsupported in the other. The conformance matrix (see
`integration/conformance_test.go` and `integration/conformance-matrix.json`)
is the mechanical check that catches that drift.

## The contract

For every **shared-surface** whole-program case, evaluating the source with the
interpreter must produce **byte-identical stdout** to compiling the source with
the AOT compiler, writing the IR, running `llc` and `cc`, and executing the
resulting native binary.

```
EvalExpr(src).stdout  ==  AOT(src).stdout
```

The matrix asserts this equality for every case in `integration/conformance_cases.go`
and records the observed outputs so a failure is visible, not silently accepted.

## Value model

Both backends share a small dynamic value model. A value is one of:

| kind      | interpreter            | AOT (codegen)              |
|-----------|------------------------|----------------------------|
| int       | `int64`                | `i32` (signed)             |
| float     | `float64`              | `double`                   |
| bool      | `0`/`1`                | `i1`/`i32 0|1`             |
| none      | `None`                 | sentinel `0`               |
| string    | `*str` (heap)          | `%str` global + i8*        |
| list/dict/set | heap object handle | `%obj`-tagged runtime heap |

Numeric promotion follows Python-style gradual rules: mixing `int` and `float`
in a binary op promotes to `float` (`sitofp`), and `int(f)`/`float(i)` are the
explicit conversions. Integer division is floor (`//` -> `sdiv`), modulo is the
remainder (`%` -> `srem`), `**` is exact binary exponentiation (folded for
literals, `@llvm.pow.f64` for runtime values).

## Evaluation semantics

Both backends evaluate a statement sequence in order and write `print` output
to stdout with the same formatting. The `print` builtin is the observable
contract point: each argument is rendered with the same `Repr` formatting on
both backends, and each argument is followed by a newline.

| construct        | interpreter                                | AOT                                      |
|------------------|--------------------------------------------|------------------------------------------|
| `if`/`elif`/`else` | branch on truthiness                       | `br` on `icmp`                           |
| `while` + `else`   | run `else` iff loop exits normally         | same control flow                        |
| `for x in range(n)`| eager `range` list, step/neg handled       | `rt_range`/unroll                        |
| `break`/`continue` | exit/skip the innermost loop               | `br` to exit/continue blocks             |
| `def` + call       | closure over env                           | function `alloca` + env closure          |
| default/keyword args | bound at call                            | same                                    |
| generators/`yield`  | interpreter only (see divergence)         | not yet lowered (see divergence)        |

## Divergence (documented, non-shared surface)

The matrix marks every case `shared: true` today — every registered whole-program
case passes parity. Constructs that are **not** yet shared surface are recorded
as backend-specific and **excluded** from the matrix rather than silently
asserted:

- generators / `yield` / `yield from` — interpreter-only today.
- arbitrary (non-identity) decorators — interpreter-only today.
- multi-file build semantics (`import`/duplicate detection) — AOT build layer.

## Machine-readable matrix

`lang.ConformanceMatrix` (`pkg/lang/conformance.go`) is the JSON schema for the
emitted artifact. A row records, for one case, the interpreter stdout, the AOT
stdout, whether each backend ran clean, and the `parity` flag. The artifact is
deterministic for a given registry + toolchain, so a script can diff two runs to
detect a **new** drift. `lang.InterpreterRun` is the in-process interpreter half
of a case; the AOT half requires the `llc`/`cc` toolchain.

## Keeping the matrix green

When adding a language construct:

1. Add a whole-program case (single-file or merged) to
   `integration/conformance_cases.go` covering it.
2. Implement it on **both** backends so `EvalExpr` and `Compile` agree.
3. Run `go test ./integration/ -run TestConformanceMatrix` — the matrix must
   stay 19/19 green, and the artifact must record the new case as parity.
4. If the construct is intentionally backend-only, leave `shared` false and
   document the divergence here.
