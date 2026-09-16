# ADR 0083: `enumerate` builtin

## Decision

Add an `enumerate(x)` builtin that returns a list of `[index, value]` pairs
for each element of a list: `enumerate([10, 20, 30])` -> `[[0, 10], [1, 20],
[2, 30]]`.

## Details

- **Interpreter**: `evalCall`'s builtins switch adds `case "enumerate":`. It
  evaluates the argument; for a `list` object it allocates a new list and
  appends a nested 2-element list `[i, elem]` per element (`i` is the raw
  int64 index). Anything else is an `*EvalError{"enumerate expects a list"}`.
- Example: `enumerate([10, 20, 30])` evaluates to `[[0, 10], [1, 20], [2, 30]]`.

## Scope

- Interpreter only: the LLVM AOT codegen's builtins folding handles literal
  list/string args for `sum`/`min`/`max`/`reversed`, but constructing nested
  list literals for `enumerate` is not yet lowered (documented limitation). A
  future ADR will cover codegen `enumerate` lowering.

## Alternatives Rejected

- **Python-style lazy `enumerate` iterator** (yields pairs on demand): rejected
  for simplicity — `enumerate(x)` evaluates eagerly to a list of pairs.
