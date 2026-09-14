# ADR 0019: Decorators

## Decision
Decorator lines (`@dec`, one per line) may precede a `def` statement:
`@dec def f: ...` is equivalent to `f = dec(f)` at definition time. Multiple
decorators apply bottom-up (`@dec1 @dec2 def f` -> `f = dec2(dec1(f))`).
Decorators are implemented in the interpreter (REPL / `--eval` path).

## Agentic rationale
Decorators are a common Python idiom agents emit to wrap functions (logging,
counting, transform wrappers). Without support, such programs fail to parse
(`@` is not a token) or silently bind the wrong function. The AST already has
`FuncDef.Decorators []Expr`; this wires parsing, analysis, and evaluation.

## Codegen/IR implications
- Lexer: `@` added to the operator set.
- Parser: `parseStmt` detects `@` and parses decorator lines (each ends on a
  NEWLINE), then requires `def` and attaches `Decorators` to the `FuncDef`.
- Semantic analysis: each decorator expression is inferred for validity in the
  enclosing scope.
- Evaluator (`FuncDef` case): the base function value is built as a closure,
  then each decorator value is applied in order (`f = dec(f)`), binding the
  final value to the function name. A bare decorator `Name` may resolve a
  top-level `FuncDef` (a first-class value not representable as an int64);
  `evalDecorator` and `callDecValue` dispatch on `*FuncDef` vs closure ids.
- Not lowered by the AOT LLVM backend yet (same status as closures/classes).

## Alternatives rejected
- Requiring decorators to be pre-bound in `e.Vars` only: rejected, since
  top-level functions live in `e.funcs` and are not int64 values.
- Treating decorators as purely syntactic (no transform): rejected — the
  mission is the general Python `dec(f)` semantics.
