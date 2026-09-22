# ADR 0140: `with` context managers and `yield from`

Status: accepted

## Context

Phase 6 (language-surface parity) calls for `with` / context managers and
`yield from`. Both build on the already-shipped `try`/`except` and `yield`
features. `with` drives the `__enter__`/`__exit__` context-manager protocol;
`yield from` delegates a generator's yields to a sub-iterable.

## Decision

**Interpreter** (`jit.go`): fully implement both.

- `with expr [as name]: body`:
  - evaluate `expr` to the manager; call `manager.__enter__()` and bind its
    result to `name` when an `as` clause is present;
  - run `body`; on exception call `manager.__exit__(exc_type, exc_val, 0)`
    and, if it returns truthy, suppress the exception; on normal completion
    call `manager.__exit__(0, 0, 0)`.
- `yield from expr`:
  - append every element of the sub-iterable (a generator-call list handle, a
    list literal, or a `range(...)` expansion) to the current generator's
    yield list.

**AOT codegen** (`codegen.go`): emit structurally-correct LLVM IR for both.

- `yield from` emits a runtime loop: `rt_list_len` bounds, `rt_get_elem` per
  element, `rt_append` into the generator list.
- `with` emits `__enter__` / `__exit__` dispatch calls, binds the `as` name,
  and runs the body.

**Known codegen limitations** (documented, not silently dropped): the AOT
backend's class-method and generator-function paths currently emit duplicate
function definitions (`@main` emitted twice) for programs that use classes or
generators — a pre-existing defect independent of this feature. Because both
`with` (which needs a manager class) and `yield from` (which needs a generator)
sit on those paths, the codegen IR for them is structurally valid but the
runtime execution path is blocked by the underlying duplicate-function bug.
The interpreter is the authoritative, fully-tested implementation for both
features.

## Consequences

- `with` and `yield from` are fully testable through `EvalExpr`/JIT interpreter.
- The codegen IR for `yield from` and `with` is emitted and parseable; fixing
  the pre-existing duplicate-function defect will unblock runtime execution.
- Parser/semantics: `with`, `from`, `as` are now reserved keywords; `WithStmt`
  and `YieldFromStmt` AST nodes carry spans and are analyzed for scoping.
