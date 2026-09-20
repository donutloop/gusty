# ADR 0134: Escape-analysis heap elision (dead list-literal allocations)

## Context
AOT codegen (`GenerateIR`) emits a runtime heap allocation (`@rt_alloc`) for
every list literal assigned to a top-level variable, even when that variable
is never read afterwards. Such an allocation is a **dead object**: it is
created, never observed, and (in the static-dispatch AOT model) can never
escape to a function body, because function bodies use their own shadowed
locals and cannot read top-level globals. Eliminating the allocation is a
pure win — no observable behavior changes.

## Decision
Add an always-on source-level escape analysis (`pkg/lang/escape.go`,
`deadListAssignments`) that runs before codegen and proves, for each top-level
variable, whether every assignment to it is a list literal and whether it is
ever read at top level:

- A variable is a **dead-list candidate** iff every top-level assignment to it
  is a list literal **and** it is never read at top level.
- Reads are collected by walking expressions (Names, list/dict/set literals,
  BinOp/UnOp/CondExpr, Call fn+args, Attr obj, Index obj+idx, Comp elems/keys/
  vals, Generator elems+iter) and compound bodies (if/while/for/match/try).
- Function bodies are **not** descended into: in this codegen functions
  cannot read top-level globals (they own their locals), so top-level deadness
  is decided purely from top-level reads.
- In `stmt`, a `Name = ListLit` assignment is emitted **without** `rt_alloc`
  when the variable is in the dead set; the `inFunc` guard ensures the
  optimization never fires inside a function body.

This is a dead-object elimination delivered at the source/codegen boundary,
narrower than the IR-level liveness+escape pass the roadmap envisions, but it
proves the allocation never escapes (no `%obj` handle, closure env, or method
table can observe it).

## Alternatives rejected
- **IR-level liveness pass in `opt.go`**: larger surface, needs to prove
  liveness across emitted IR and interact with dead-global elimination; the
  source-level proof is simpler, verifiable, and always-on.
- **Skipping only when `--opt-level=1`**: dead-object elimination is always
  correct, so it is gated on nothing.

## Consequences
- Programs that assign a never-read list literal emit zero `rt_alloc` calls
  for it (verified in `TestAOTEscapeDeadList`).
- Whole-program output is unchanged (guarded by `TestEscapeDeadListRun`).
- Conservative: a variable with any non-list assignment or any top-level read
  still allocates, so no path can observe a freed slot.
