# ADR 0043: Dict `.keys()` / `.values()` in the AOT codegen

## Decision

Constant-fold `.keys()` and `.values()` on constant dict literals in the AOT
LLVM codegen, matching interpreter semantics: `{1: 2, 3: 4}.keys()` lowers to
a list `[1, 3]` and `.values()` to `[2, 4]`, so `len`/`sum` work on them.

## Details

- The codegen's `Attr` callee branch (string methods upper/lower/strip) now
  also handles a `*DictLit` receiver: `keys` emits a `*ListLit{Elems: Keys}`
  and `values` a `*ListLit{Elems: Vals}` via `g.value`, returning the list
  reference.
- A new `g.dictMethodElems(e) ([]Expr, bool)` helper resolves a dict-method
  Call to its keys/vals, used by the `len` builtin (a `case *Call:` that
  returns the element count) and the `sum` builtin (initializes `elems` from
  the folded keys/vals so the existing fold loop runs).

## Scope / limitation

- `.items()` (key/value pairs) and non-constant receivers remain
  interpreter-only, as does list `.append()` and string `.split()`.
