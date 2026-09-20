# ADR 0137: Sequence slicing `s[a:b]`, `s[::step]`, negative indices

## Status
Accepted.

## Context
The roadmap lists sequence slicing (`s[a:b]`, `s[::step]`, negative indices)
as the next Phase-6 feature. Python slicing has rich semantics: default bounds,
negative-index normalization, explicit positive/negative steps, and reverse
iteration. Both the interpreter (jit) and the AOT (LLVM IR) backend must behave
identically.

## Decision
- **AST**: a new `Slice` node (`Obj`, optional `Low`/`High`/`Step`). Absent
  bounds are nil (`s[:b]`, `s[a:]`, `s[:]`, `s[::step]`).
- **Parser**: `parsePostfix` distinguishes `s[i]` (index) from `s[a:b:c]`
  (slice) by a `:` token after the optional low bound.
- **Interpreter**: `pySliceIndices` implements CPython `PySlice_GetIndicesEx`
  normalization; string slices copy bytes, list slices copy element handles.
  Zero step is an error; dict/other kinds are rejected with a clear message.
- **AOT**: new runtime helpers `rt_max`, `rt_min`, `rt_slice` in the heap IR.
  `rt_slice` reads the source object's kind, normalizes bounds in IR, and
  copies elements into a fresh heap object, so strings and lists share one
  runtime function. The `value` emitter uses a dedicated `%sl%d` register
  prefix to avoid colliding with `%g%d` (the general index path increments
  `heapSeq` before use, while most other paths use-then-increment).
- **Print**: the `print` builtin recognizes a `Slice` whose object is a list
  variable so sliced lists print as `[a, b, c]` rather than an opaque handle.

## Consequences
- List slicing is fully supported in both backends and covered by the
  `TestSlice` integration test.
- String slicing is supported in the interpreter. The AOT backend has a
  pre-existing limitation: string *variables* are not supported (only string
  literals inline), so native string slicing is limited to that surface.
  This is tracked as a follow-up rather than a blocker for the slicing feature.
- The `%sl%d` register prefix keeps slice results unambiguous even though the
  codebase has two `heapSeq` increment conventions.
