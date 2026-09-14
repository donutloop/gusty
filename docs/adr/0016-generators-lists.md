# ADR 0016: Generators (yield) and list literals

## Decision
`yield` inside a function makes it a generator: calling it evaluates the body
and returns a list of all yielded values. List literals `[...]` produce list
objects. `for` loops can iterate a generator result or a list literal.

## Agentic rationale
Generators and lists are core Python ergonomics for data pipelines. The
interpreter implements generators eagerly (collects all yields into a list),
which is a faithful subset for pure generator functions.

## Codegen/IR implications
- Evaluator: `yieldList` accumulator collects YieldStmt values; a FuncDef whose
  body contains YieldStmt returns a `list` heap object.
- `list` heap kind holds `[]int64` elements; `ListLit` expressions build it.
- `for` iterates `range(...)` via bounds and list objects via elements.
- Semantic analyzer binds the loop variable even when the iterable type is
  unknown, using a dynamic type fallback.

## Alternatives rejected
- Lazy/resumable generators (suspend/replay) — deferred until an iterator
  protocol value type exists.
