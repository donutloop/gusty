# ADR 0109: Element access into list-producing call expressions in the AOT codegen

## Decision

The AOT codegen (`irGen`) now folds constant-index element access into
list-producing call expressions that are representable in the integer-element
list model:

- dict methods `keys()` and `values()`
- builtins `sorted(list)` (including `reverse=True`) and `reversed(list)`
- string method `split(sep)`

`{d}.keys()[k]`, `{d}.values()[k]`, `sorted(...)[k]`, `reversed(...)[k]` and
`"s".split(sep)[k]` emit the exact element constant selected by the literal
index `k`, matching the interpreter. Out-of-bounds indices are a compile-time
error ("list index out of range").

## Motivation

The interpreter already supported these indexings, but the codegen rejected
them with "index requires an inline list/dict/set literal" — a backend sync
gap (AGENTS.md requires both backends to stay in sync). `len()` of these calls
was already folded; element access was missing.

## Non-goals / exclusions

- `partition(sep)` is excluded: `dictMethodElems` returns only dummy length
  elems for it, so indexing would silently give wrong results. It stays a
  compile-time error rather than a wrong constant.
- `items()` is excluded: it returns list-of-pair nested lists, which the
  integer-element list model cannot represent.

## Implementation

A helper `indexListElems(c *Call) ([]Expr, bool)`:

1. method calls (`keys`, `values`, `split`) -> `dictMethodElems(c)`, excluding
   `partition`
2. builtin calls (`sorted`, `reversed`) -> read the `*ListLit` arg, sort /
   reverse the `int64` values (honoring `reverse=True`), return `IntLit`
   elements

The Index handler's `case *Call:` calls this helper, bounds-checks the literal
index, and emits `elems[key]`; otherwise it returns the original
"index requires an inline list/dict/set literal" error.

## Tests

`cmd/gustyc/main_test.go`:
`TestCLIEmitLLVMIndexCallElems` checks the emitted IR contains the exact
selected constant for keys/values/sorted/reversed/split indexing.
