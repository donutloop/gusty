# ADR 0026: Ternary conditional expressions

## Decision

Implement Python-style ternary conditional expressions (`then if cond else
otherwise`) in both the interpreter (`pkg/lang/jit.go`) and the LLVM AOT codegen
(`pkg/lang/codegen.go`).

## Grammar / parsing

- A ternary is parsed at the lowest precedence, between `parseExpr` and
  `parseOr`: `parseExpr` calls `parseTernary`, which parses an or-level `then`,
  then `if cond` (also or-level), then `else` and a full expression (so the
  else branch is right-associative — `1 if 0 else 2 if 1 else 3` is
  `1 if 0 else (2 if 1 else 3)`).
- The comprehension iterable is parsed as an or-level expression (`parseOr`),
  not a ternary: otherwise `parseTernary` greedily consumes the comprehension's
  own `if` filter (`[y*y for y in range(4) if y > 1]`) and demands an `else`.

## Lowering

- Interpreter: evaluate `cond`; if nonzero evaluate `then`, else `otherwise`.
- AOT codegen: a constant condition folds to the taken branch; a runtime
  condition (comparison, or `and`/`or`) lowers to a single LLVM
  `select i1 cond, i32 then, i32 else` — allocation-free and block-free,
  matching the codegen's bare-constant convention where LLVM infers the
  condition type from the `select` context.
- Semantic inference: the result type is the branch type when the `then` and
  `else` branch types agree (via a new structural `Type.Same`), else dynamic.

## Alternatives rejected

- Lowering to `br` labels and a phi merge like `IfStmt` — rejected because the
  ternary is an expression yielding a value; `select` is simpler and needs no
  new blocks or phi nodes.
