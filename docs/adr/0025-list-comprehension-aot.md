# ADR 0025: List comprehensions in the LLVM AOT codegen

## Decision

The interpreter already supports list comprehensions at runtime. Mirror them in
the LLVM AOT codegen (`pkg/lang/codegen.go`) for the compile-time-known cases so
list comprehensions ship in **both** execution backends.

## Codegen/IR implications

- A list comprehension over a constant iterable — an inline list literal or
  `range(n)` — is unrolled at compile time into a dedicated global struct with
  the same shape as a list literal: `{i32 count, [n x i32] elems}`. The
  `comp()` lowering folds each element expression under a compile-time binding
  (`constBindings`) for the comprehension variable, and deduplicates lowered
  globals by AST node (`compNames`) with their folded element count recorded in
  `compLen`.
- A constant `if` condition filters elements at compile time: filtered-out
  iterations simply do not contribute to the emitted initializer list.
- The folded result can be indexed inline exactly like a list literal: an
  `Index` whose object is a `*Comp` bounds-checks the constant key against
  `compLen` and emits a `getelementptr` + `load` at that key, mirroring the
  `*ListLit` case.
- Like list/dict/set literals, the comprehension must be used inline (no
  assignment-to-variable indirection) in the AOT path: indexing a `*Name` still
  requires a runtime boxed list, which the allocation-free i32-only codegen does
  not support.

## Alternatives rejected

- Emitting a runtime loop over `range` to build a heap list — rejected because
  the AOT codegen is i32-only and cannot heap-allocate; unrolling at compile time
  keeps the emitted module allocation-free and fully verifiable.
- Lowering only the iterable to a global and re-running the body at each index —
  rejected as needlessly complex; constant-folding the body once into a static
  struct matches the existing list-literal lowering and keeps indexing a single
  GEP+load.
