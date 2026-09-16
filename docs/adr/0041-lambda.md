# ADR 0041: Lambda (anonymous functions) in interpreter + AOT codegen

## Decision

Ship `lambda params: expr` as a first-class anonymous function in **both** the
interpreter (`jit.go`) and the AOT LLVM codegen (`codegen.go`), reusing the
existing `def` closure machinery so both paths stay in sync.

## Details

- Parser already produced a `*Lambda` AST node; it was unimplemented in both
  paths (eval rejected it, codegen rejected it).
- **Interpreter**: eval `*Lambda` builds a synthetic `FuncDef` (params, body =
  `ReturnStmt{lambda.Body}`) and calls `allocClosure(fd, e.Vars)`, returning a
  closure handle. evalCall's existing closure-callee path invokes it for
  inline `(lambda ...)(args)`; named `f = lambda ...; f(3)` resolves the Name
  to the closure in the environment.
- **Codegen**: `emitLambda` builds a synthetic `FuncDef` named `lambda_N`,
  registers it in `g.funcs`/`g.fds`, and emits its `define` into the module
  **globals** builder (`&g.globals`) — not the current statement builder, so
  the define is not nested inside `main`. The value switch lowers a Lambda to
  a closure reference; `Call` handles an inline Lambda callee and resolves
  bound names via a new `g.lambdas map[string]string` (var -> FuncDef name)
  populated by `Assign` with a Lambda RHS.

## Alternatives rejected

- Implementing lambda only in the interpreter (would desync the AOT codegen).
- Emitting the lambda `define` into the current statement builder (nested the
  function inside `main`, producing invalid IR).
