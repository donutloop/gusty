# ADR 0034: `min`/`max`/`sum` over lists with runtime-variable elements

## Decision

The interpreter folds `min`/`max`/`sum` over any list, including one whose
elements are runtime variables (`min([a, b])`). The LLVM AOT codegen built a
constant-initialized list global via `emitList`, which required every element
to be an `*IntLit` and rejected runtime Names with `list literal elements must
be integers`. Lower each element directly via `g.value(b, el)` in the
`min`/`max`/`sum` cases — an `icmp`+`select` chain for `min`/`max`, an `add`
chain for `sum` — so runtime-variable elements behave exactly like integer
literals. `emitList` and its other callers are unchanged.

## Codegen/IR implications

- `min([a, b])`: `best = g.value(b, a)`; for each later element `el =
  g.value(b, el)`, emit `icmp slt`/`sgt` and `select`, keeping the running
  best.
- `sum([a, b])`: `acc = g.value(b, a)`; for each later element `el =
  g.value(b, el)`, emit `add`.
- The `emitList` constant-global path is untouched (still used by
  `for`-over-list, `len`, `index`), so no global emission or caller changes.
- The AOT path stays i32-only and allocation-free.

## Alternatives rejected

- Extending `emitList` to emit runtime stores — rejected: that touches the
  global-declaration builder and all 5 callers; folding elements directly via
  `g.value` is a 2-line-per-case lowering, allocation-free, and needs no
  global plumbing.
- Keeping the IntLit-only restriction (documenting it as an AOT limitation) —
  rejected: rejecting `min([a, b])` that the interpreter accepts and runs is
  a silent divergence, not a documented limitation.
