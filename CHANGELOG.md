# CHANGELOG

Single clean list of features, newest first.

## Current

- `feat(interp+llvm)`: **`pass` (no-op statement)** — `pass` parses as a
  dedicated `PassStmt` (previously fell through to an undefined `Name`), is a
  pure no-op in both the interpreter and the LLVM AOT codegen, and is a valid
  placeholder in function/loop/branch bodies. Adds unit + integration tests.

- `feat(interp+llvm)`: **`elif` chains** — the parser already built
  `IfStmt.Elifs`, but neither the interpreter nor the LLVM codegen executed
  them, and the semantic analyzer skipped elif bodies entirely (and scoped
  if/else bodies to child scopes, hiding assignments from the enclosing
  scope). Both backends now evaluate elif branches; the analyzer analyzes
  elif bodies and analyzes if/while/for bodies in the enclosing scope so
  assignments flow outward (matching the runtime's shared-vars model).

- `feat(llvm)`: **inline list literals with constant indexing + len**
  in the AOT codegen — `[1,2,3][1]` and `len([1,2,3])` lower to a
  dedicated global struct with constant-GEP loads.

- `feat(interp)`: **modules / imports** — `import mod` loads `mod.gy`,
  evaluates it, and binds `mod` as a module namespace; top-level variables and
  functions are accessed via `mod.name` and `mod.fn(args)`. A module can
  import other modules. Parser/AST already had `ImportStmt`; the evaluator now
  runs it.
- `feat(interp)`: **class inheritance** — `class Child(Base):` inherits
  `Base`'s methods and `__init__`; instance/class method and attribute lookup
  walks the whole base chain (multi-level). An overridden method can delegate
  to the base implementation with `super()` (resolves methods on the base
  class of the currently-executing class, bound to the current instance).
- `feat(interp)`: **decorators** — `@dec` lines before a `def` apply at
  definition time (`@dec def f` -> `f = dec(f)`), bottom-up for multiple
  decorators. Parser accepts `@`, analysis infers decorator expressions, and
  the evaluator applies them to the function value.
- `feat(interp)`: **closures** — a `def` nested in a function body captures the
  enclosing scope, can be returned/stored/called (`m = add(1); m(2)`).
  Semantic analysis treats calls to function-typed variables with unknown
  return type as dynamic so assigned names aren't flagged undefined.
- `feat(interp)`: `match` `case _:` wildcard always matches (interpreter).
- `feat(lang)`: `for i in range(a, b)` iterates `a`..`b-1` end-to-end (interpreter + LLVM codegen, llc-clean).
- `feat(semantic)`: reject `break`/`continue` outside a loop with a diagnostic;
  track loop depth through `while`/`for` bodies.
- `feat(lang)`: add `break` and `continue` loop control in the parser,
  interpreter, and IR emitter (loop-label tracking; llc-clean). Loop bodies now
  go through the full statement dispatcher, fixing nested `if`/control flow
  inside `while`/`for` bodies.
- `feat(codegen)`: emit `match` as a chain of integer comparisons with
  terminators on every basic block (llc-clean IR).
- `feat(interp)`: evaluate `match` statements with literal patterns in the
  interpreter.
- `feat(interp)`: evaluate `if`/`elif`/`else`, `while`, and `for ... in
  range(n)` in the interpreter.
- `feat(interp)`: register and call user-defined functions in the
  interpreter; bind params in a fresh scope and return via `evalBody`.
- `feat(semantic)`: infer user function signatures by binding call argument
  types to parameters and analyzing the body; check argument counts.
- `feat(semantic)`: indentation-aware lexer producing `INDENT`/`DEDENT`.
- `feat(codegen)`: deterministic textual LLVM IR emitter (opaque pointers)
  replacing the crashing go-llvm `CreateCall` path; verified by `llc
  -opaque-pointers`.
- `feat(lang)`: Python-like indentation-based language with functions, control
  flow, match, print, and range.
