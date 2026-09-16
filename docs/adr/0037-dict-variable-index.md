# ADR 0037: dict-variable indexing

## Decision

The AOT codegen previously rejected `d[key]` where `d` is a dict variable
("index requires an inline list/dict/set literal"). Track concrete DictLit
values assigned to variables (`dictVals map[string]*DictLit`), resolved in
`AssignStmt`, and emit `d[key]` as a compile-time i32 constant via
`dictIndex` — a constant-key lookup over the recorded dict literal.

## Codegen/IR implications

- `AssignStmt` records `dictVals[target]` when the RHS is a DictLit.
- `Index(Name)` looks up `dictVals[name]` and emits `vals[i]` as a constant.
- The AOT path stays i32-only and allocation-free.
