# ADR 0035: `len` and list-index over runtime-variable-element lists

## Decision

The interpreter accepts `len([a, b])` (returns the count) and `[a, b][0]`
(indexing with a runtime-element list). The LLVM AOT codegen built a
constant-initialized list global via `emitList`, which required every element
to be an `*IntLit` and rejected runtime Names with `list literal elements must
be integers`. `len([a, b])` needs only the element count, so return it
directly as a constant (no `emitList` global, no load). `[a, b][i]` needs the
indexed element, so evaluate it directly via `g.value(b, ln.Elems[i])`.
`emitList` and its other callers (`for`-over-list, comprehension globals) are
unchanged.

## Codegen/IR implications

- `len(ListLit)` returns `fmt.Sprintf("%d", len(ln.Elems))` directly — an i32
  constant; no global declaration, no count-field load.
- `Index(ListLit, IntLit)` returns `g.value(b, ln.Elems[i])` directly — the
  indexed element is lowered inline.
- The AOT path stays i32-only and allocation-free.

## Alternatives rejected

- Extending `emitList` to emit runtime stores — rejected: that touches the
  global-declaration builder and all 5 callers; direct `g.value` lowering is
  per-case, allocation-free, and needs no global plumbing.
- Keeping the IntLit-only restriction (documenting it as an AOT limitation) —
  rejected: rejecting `len([a, b])`/`[a, b][0]` that the interpreter accepts
  and runs is a silent divergence, not a documented limitation.
