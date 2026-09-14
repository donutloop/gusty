# ADR 0018: Closures (nested defs capturing the enclosing scope)

## Decision
A `def` nested inside a function body is a closure: it captures the enclosing
scope at definition time and can be returned, stored in a variable, and called
later (`m = add(1); m(2)`). Closures are implemented in the interpreter
(REPL / `--eval` path).

## Agentic rationale
Nested functions that can read and return enclosing locals are a core Python
ergonomic. Without closures, factory-style code (`def add(x): def mid(y): ...;
return mid`) is impossible, and agents generating such programs would silently
fail semantic analysis. The feature must be consumable as ordinary source that
the `--eval` / REPL path runs and the JSON AST dump shows (the AST already
represents nested `FuncDef` nodes; no schema change).

## Codegen/IR implications
- Evaluator: a `FuncDef` statement evaluated while `inCall` is true binds a
  `closure` heap object whose `env` is a copy of the current scope (`Vars`).
- Calling a closure (`evalCall` on a `Name` whose value is a closure) seeds the
  callee scope from the captured `env`, then binds params/defaults.
- Semantic analysis: a call to a function-typed variable whose return type is
  unknown returns `Dyn` (not `nil`), so a name assigned from a closure call
  (`m2 = m(2)`) is not reported as undefined.
- Not lowered by the AOT LLVM backend yet (same status as classes/generators).

## Alternatives rejected
- Lexical scoping via environment chains with shared mutable cells — deferred
  until the runtime gets reference semantics beyond plain `map[string]int64`.
- Treating unannotated closure returns as `nil` in analysis — rejected: `nil`
  means "undefined name" in scope lookup, so it broke `m2 = m(2)`.
