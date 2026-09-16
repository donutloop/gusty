# ADR 0068: `sorted(list)` builtin in interpreter

## Decision

Add the `sorted(list)` builtin — a copy of the list with elements sorted —
mirroring Python's `sorted`. The interpreter copies the list's `elems`,
sorts them with `sort.Slice` using a `lessVal` comparator (ints by value,
strings by content), and returns a new list.

## Details

- **Interpreter**: the builtin `sorted` case requires exactly 1 argument,
  evaluates it to a `list`, copies `lo.elems`, sorts via `sort.Slice` with
  `e.lessVal`, creates a new list object, and returns it.
- `lessVal(a, b)` compares boxed values: for string heap objects by `sval`,
  otherwise by raw value.

## Scope

- Interpreter path only: codegen list construction for builtin results is
  not folded yet.
