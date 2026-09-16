# ADR 0044: List `.append()` on constant literals in the AOT codegen

## Decision

Constant-fold `.append(x)` on constant list literals in the AOT LLVM codegen,
matching interpreter semantics for the foldable case: `[1, 2, 3].append(4)`
lowers to the list `[1, 2, 3, 4]`, so `len`/`sum` work on the result.

## Details

- The codegen's `Attr` callee branch (string methods, dict keys/values) now
  handles a `*ListLit` receiver with method `append`: it builds a new
  `*ListLit{Elems: append(receiver.Elems, arg)}` and emits it via `g.value`,
  returning the list reference.
- `dictMethodElems` is extended to fold an append Call to its resulting elems,
  so the `len` and `sum` builtins fold `.append(...)` args.

## Scope / limitation

- Mutating a list bound to a variable (`xs.append(x)` then `xs[0]`) stays
  interpreter-only: the codegen folds only append on constant literals.
- `.items()` (dict pairs), non-constant dict receivers, and string `.split()`
  remain interpreter-only.
