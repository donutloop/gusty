# ADR 0038: set and dict comprehensions in the AOT codegen

## Decision

Extend `comp()` to lower **set comprehensions** (`{x * x} for x in [1, 2, 3]`)
and **dict comprehensions** (`{x: x * 10} for x in [1, 2]`) to dedicated
`@.setN` / `@.dictN` globals, matching the interpreter's semantics.

## Details

- Set comprehensions unroll the iteration, fold `Elems[0]` per item, and
  deduplicate via a `seen` map; the result is emitted as
  `@.setN = private global {i32, [n x i32]} { i32 n, [n x i32] [...] }`.
- Dict comprehensions fold `Keys[0]`/`Vals[0]` per item and emit
  `@.dictN = private global {i32, [n x i32], [n x i32]} { i32 n, [...keys], [...vals] }`.
- Both record `compLen[c]`/`compEls[c]` so `len(...)` works exactly like the
  list-comprehension path.
- Globals are written to `g.globals` (module level), never `b` (function body).

## Alternatives rejected

- Keeping set/dict comprehensions interpreter-only (they were already
  evaluated by the interpreter but produced an unsupported-kind error in the
  AOT codegen).
