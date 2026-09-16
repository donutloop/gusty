# ADR 0047: String indexing in interpreter + AOT codegen

## Decision

Ship string indexing `"abc"[1]` in **both** paths, returning the ASCII char
code at the index (`"abc"[1]` -> 98 for 'b'). Previously `*StrLit` index was
unimplemented in both the interpreter and the codegen.

## Details

- **Interpreter**: the `*Index` eval case gains a `str` kind: bounds-checks
  against `len(o.sval)` and returns `int64(o.sval[idx])`.
- **Codegen**: the `*Index` value case gains a `*StrLit` branch that
  constant-folds via `g.stringVal(obj)` and returns `str[key]` as an i32.

## Scope

- Negative / out-of-range indexes error (`string index out of range`).
- Non-constant string receivers are not yet foldable in the codegen.
