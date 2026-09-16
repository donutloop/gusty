# ADR 0036: len() on string variables

## Decision

The AOT codegen previously rejected `len(s)` where `s` is a string variable
("len requires an inline list/dict/set literal"). Track concrete string
constant values assigned to variables (`strVals map[string]string`), resolved
in `AssignStmt`, and emit `len(s)` as an i32 constant at codegen time — no
strlen runtime call is needed since strings are compile-time constants.

## Codegen/IR implications

- `AssignStmt` records `strVals[target]` when the RHS is a StrLit, a string
  variable, or a `+` concatenation of two string constants.
- `len(Name)` returns `len(strVals[name])` directly.
- The AOT path stays i32-only and allocation-free.
