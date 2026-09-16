# ADR 0040: Comprehension indexing in the AOT LLVM codegen

## Decision

Resolve indexing into lowered **dict** and **set comprehensions** at codegen
time, mirroring the interpreter's `Index` semantics. The previous `Index`
`*Comp` case treated every comprehension as a list-shaped struct and emitted a
positional `GEP {i32, [n x i32]}` load, which was semantically wrong for
sets/dicts.

## Details

- `comp()` now records the folded keys of a dict comprehension in a new
  `compKeys map[*Comp][]int64` (alongside the existing `compEls`/`compLen`).
- `Index` on a `*Comp` branches on `obj.Kind`:
  - `CompList`: unchanged positional GEP+load into `{i32, [n x i32]}`.
  - `CompSet`: membership test — call `comp()` (populates `compEls`, emits the
    set global), return the element as a constant if present, else error
    `not in set` (interpreter semantics).
  - `CompDict`: constant key lookup — call `comp()` (populates `compKeys`/
    `compEls`, emits the dict global), return the mapped value if the key
    matches, else error `key not found`.
- Because the mapped values are folded constants, dict/set comprehension
  indexing emits no runtime lookup.

## Alternatives rejected

- Treating all comprehensions as lists (the bug this ADR fixes).
- Requiring the runtime to do dict/set lookups (unnecessary; values fold).
