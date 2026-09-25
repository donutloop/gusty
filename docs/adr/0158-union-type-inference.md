# ADR 0158 — Union-type inference in the gradual checker (L6.3)

## Status
Accepted

## Context
Union annotations (`int | str`, `int | float`) parse and participate in gradual
assignability (L5.5, committed earlier). But inference never *produced* a union
value: a conditional expression whose branches carried different concrete types
inferred to the dynamic type (or mismatched), and arithmetic on a union-typed
value treated the union as opaque. So `x: int | str = (1 if c else "hi")` could
not be accepted, and `x: int | float` arithmetic could not be proven numeric.

## Decision
Extend inference so unions flow through expressions:

- **Conditional (ternary) widening** — `inferExprTy` on a `CondExpr` widens the
  branch types to their normalized union when they differ: `int` vs `str`
  infers `int | str`. Identical branch types normalize back to the single
  member type.
- **Union-aware arithmetic** — `inferBinOp` flattens each operand's union
  members (`unionMembers`) and checks membership:
  - all members numeric => arithmetic is allowed with no warning;
  - a mixed union (`int | str`) still warns `arithmetic on non-numeric`;
  - `+` over a string-only union infers `str` (concatenation).
- New helpers: `normalizeUnion` (flattens nested unions, dedupes, collapses a
  single member back to the plain type), `unionMembers`, `unionAllNumeric`,
  `unionAllString`, `anyFloat`.

The union type was initially a semantic/checker concept; AOT codegen saw
dynamic values (tagged-union lowering was a separate follow-on, L6.3 runtime).

## Follow-on (L6.3 AOT tagged-union lowering) — DONE
Union-annotated scalar variables (`int | float`, `int | str`) now get a tagged
`%unionbox` slot in the AOT backend: assignment stores the runtime member tag
(0=int, 1=float, 2=string) alongside the payload, and `print` on the variable
dispatches on the live tag to emit `%d`/`%f`/`%s`. Cross-member reassignment
under branches/loops therefore prints the currently-stored member correctly.
Covered by `integration/union_aot_test.go` (linear, control-flow, and
cross-member reassign parity).

## Consequences
- Ternary widening makes a mixed-type conditional assignable to a union
  annotation but not to either single member (`expected int, got int | str`).
- Numeric-union arithmetic no longer spurious-warns; mixed-union arithmetic
  still warns.
- AOT union variables are tagged slots that dispatch on the runtime member for
  `print`, matching the interpreter's dynamic behavior.
- Unit tests added: `TestUnionInferTernary`, `TestUnionArithmetic`, and
  interpreter `EvalExpr` union-member tests; AOT parity in `union_aot_test.go`.
- Inference itself is checker-only; the runtime/codegen behavior is the
  tagged-union lowering above.
