# ADR 0081: Generator expressions

## Decision

Add generator expressions `(elem for var in iter [if cond])` — a
Python-style generator producing `elem` for each element of `iter` bound
to `var`, optionally filtered by `cond`.

## Details

- **Parser**: `parseAtom`'s `(` case detects a `for` keyword after the
  parenthesized element and calls `parseGeneratorTail`, which parses
  `for var in iter [if cond]` and builds a `Generator{Elems, ForVar,
  Iter, Cond}`. The closing `)` is consumed after the tail.
- **Semantics**: `semantic.go` already types `Generator` as an iterable
  (`TIter(TDyn())`), so no analyzer change was needed.
- **Interpreter**: `evalGen` evaluates `iter` (a list/str/dict/set),
  binds `ForVar` to each element in `Vars`, evaluates `Elems` (each
  filtered by `Cond`), and appends the results to a new `list` object.
  It evaluates eagerly to a list of yielded values, consumable by
  `for x in gen:` or `list(gen)`.
- Example: `(x * 2 for x in [1, 2, 3] if x > 1)` → `[4, 6]`.

## Scope

- Interpreter only: the LLVM AOT codegen has no generator lowering yet
  (documented limitation). A future ADR will cover codegen generators.

## Alternatives Rejected

- **Lazy generator objects** (a `gen` object consumed on demand): rejected
  for simplicity — generator expressions evaluate eagerly to a list. The
  AST already carries `Generator`, so a lazy `gen` object can be added later
  without parser changes.
