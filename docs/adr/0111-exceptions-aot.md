# ADR 0111: try/except/finally/raise in the AOT/LLVM backend

## Context
The interpreter supports `try`/`except`/`finally`/`raise` via Go error
propagation. The AOT/LLVM IR backend rejected these statements as
"unsupported". This ADR describes how exceptions were added to the codegen.

## Decision
Use two internal globals to signal an in-flight exception:
- `@exn_flag` (i32, 0/1): raised flag.
- `@exn_code` (i32): exception class code (0 = Exception/generic, 1..7 for
  ValueError/TypeError/KeyError/IndexError/RuntimeError/StopIteration/
  ZeroDivisionError).

- `raise <Class>("msg")` stores 1 into `@exn_flag`, the class code into
  `@exn_code`, then branches to the nearest enclosing except-handler label
  (tracked on `irGen.handlerStack`) or to the current function's raise-exit
  label if none is in scope.
- `try:` compiles its body, then checks `@exn_flag` and branches to the except
  handler or `finally`; `finally` runs on both the normal and exception paths.
  `except` matches bare/`Exception` (any) or a specific class by `@exn_code`.
- Every user-function call site emits a post-call flag check (`checkExn`) so a
  raise inside a callee transfers to the caller's nearest handler, or re-propagates
  to the caller's raise-exit if uncaught.
- Each function (and `main`) gets a raise-exit label emitted before its final
  `ret`, giving uncaught raises a reachable propagation point. `funcDef`
  saves/restores the enclosing raise-exit/handler stack so nested functions
  compile correctly.

## Status
Accepted.

## Consequences
- try/except/finally/raise programs now compile and run identically in the
  interpreter and the AOT/LLVM backend.
- Added `TestExecTryExcept` integration coverage.
- Uncaught raises in `main` return a nonzero status rather than printing an
  interpreter-style error (the caught case is the supported path).
