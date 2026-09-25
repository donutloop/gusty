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

The union type remains a semantic/checker concept; AOT codegen still sees
dynamic values (tagged-union lowering is a separate follow-on, L6.3 runtime).

## Consequences
- Ternary widening makes a mixed-type conditional assignable to a union
  annotation but not to either single member (`expected int, got int | str`).
- Numeric-union arithmetic no longer spurious-warns; mixed-union arithmetic
  still warns.
- Unit tests added: `TestUnionInferTernary`, `TestUnionArithmetic`.
- No runtime/codegen behavior change in this ADR; inference is a checker-only
  improvement.
