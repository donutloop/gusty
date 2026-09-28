# ADR 0181: The collector knows its roots — precise stack roots and real safe points

## Status

Accepted. The interpreter half is implemented; the compiled backend's root stack is
the next commit of this same roadmap item (L7.2), and the AOT-specific sections
below record the design it implements.

## Context

The interpreter had a collector that nothing called. `Evaluator.Collect` existed
with a mark-and-sweep implementation, a nursery, and tests — and the only callers
were tests. A REPL session therefore kept every list, dict, string and instance a
session had ever created, and a program that looped a million times allocated a
million objects with no reclamation at any point in between.

Collecting during a program is not free in a tree-walking interpreter. The values
that are live at an arbitrary moment are not only in variables and containers: they
are in the Go locals of the `eval`/`evalCall`/`evalBin` frames currently on the
stack — the left operand of `a + b` while `b` is being evaluated, the argument list
of a call whose callee is about to run, the iterable a `for` statement is stepping
through. A collector that cannot see the host stack must be told what is live, and
the only honest options are: never collect (what we had), collect conservatively by
guessing at machine words (impossible from Go), or define safe points at which the
interpreter can *prove* its live set.

Meanwhile the compiled backend had the harder version of the same problem, and it
showed up as a wrong answer rather than as growth. Root slots were allocated once
per (scope, name) and written into a global `@gc.roots` array at the assignment that
first made a variable a container. Recursion reuses one static frame, so the inner
call's assignment overwrote the outer call's root entry, and a collection inside the
inner call swept the outer frame's list while the outer frame was about to read it
back:

```
def walk(depth):
    keep = [depth, depth * 2, depth * 3]   # rooted in a *static* slot
    if depth > 0:
        filler = walk(depth - 1)           # the inner call re-registers the same slot
    churn(200)                             # collections happen here
    return keep[0] + keep[1] + keep[2]     # keep has been swept: prints 0
```

`integration/programs/gc_precise.gy` is that program, pinned as a conformance case.

## Decision

**One root discipline in both backends: the collector is told exactly which slots
hold handles, and it only runs where that set is provably complete.**

Interpreter (`pkg/lang/gc.go`):

- The root set is `Vars` ∪ **active call frames** ∪ **declared root groups** ∪
  permanent roots (the `None` singleton, the generator accumulator, the `super()`
  receiver, every class object). A frame's local scope is pushed by `callFunc` (and
  by `swapScope`, which also roots the *caller's* scope — while a callee runs, the
  caller's bindings are not reachable through `e.Vars`) and popped when the call
  returns, so a dead frame keeps nothing alive and a live frame keeps everything.
- **The watermark is the soundness floor.** Every object minted after the last safe
  point is unconditionally live. That is what makes collecting with expressions
  half-evaluated sound: their temporaries are, by construction, younger than the
  floor.
- **A safe point is a statement boundary with an empty expression stack.** The
  watermark advances only when `exprDepth` is at its base *and* the construct
  owning the boundary has declared its root groups (`runBodyRooted` for loops and
  branches, which is why a `for` loop hands over its iterable instead of leaving it
  in a Go local). Anywhere else the collector still runs — it just cannot reclaim
  what is above the floor.
- **A call that *is* the statement is a safe point of its own.** `work()` or
  `total = helper(x)` has no half-evaluated enclosing expression: its arguments have
  been copied into the callee's rooted frame and its result does not exist yet, so
  the callee's body may advance the watermark and reclaim as it goes (`runFuncBody`).
  A call nested inside a larger expression is not — that is the documented limit,
  and the honest reason a value the caller holds in a register cannot be told apart
  from garbage until the value model gives the interpreter a real value stack
  (roadmap Phase 11).
- Collection is triggered by an allocation *threshold*, not a clock or a heuristic:
  the same program collects the same number of times on every run, which is what
  keeps the parity harness able to assert anything.
- `GUSTY_GC_STRESS=1` (and `Evaluator.SetGCStress`) force a collection at every
  statement boundary, and `integration/gc_stress_test.go` runs the whole conformance
  corpus that way, comparing output against the unstressed run. A missing root
  becomes a failing test instead of a heisenbug.
- The collector reports itself: `GCStats` (collections, roots traced, roots skipped,
  marked, freed, total freed, live, frames, protected, generational, backend) is
  reachable from Go (`Evaluator.GCStats`, `InterpreterRunOpts`) and from the CLI
  (`gustyc --gc-stats` → one `key=value` line on stderr, or a `gc` member of the
  `--json` payload; `definitions.gcStats` in `--schema`).

Compiled backend (same roadmap item, next commit): the root array becomes a
*stack*. A function prologue opens a frame (`@gc_roots_used` saved into a frame
slot), each handle-assigning store pushes `(slot, kind=1)` — deduplicated per frame,
so a loop that reassigns a list a thousand times still contributes one entry — a
scalar store marks that slot's entry `kind=0`, and every return path closes the
frame, restoring the top. `rt_gc` traces only entries tagged as handles and counts
what it skipped, so neither a dead frame's stack nor an `int` ever gets guessed at.
Counters land in globals and `rt_gc_report` prints the same `key=value` line the
interpreter prints.

## Consequences

- The interpreter reclaims while programs run: a 400-iteration loop that binds a
  fresh list per iteration no longer leaves 400 lists behind, and a 2000-input REPL
  session stays bounded instead of growing without limit (`pkg/lang/gc_test.go`
  measures both).
- Frame rooting is load-bearing and *tested* as such: with `pushFrame` stubbed out,
  `TestGCFramesKeepRecursionLive` fails with `cannot index null` — the recursion's
  lists are swept under it. The test asserts the behavioural symptom, not the
  internals.
- Conservatism is explicit rather than hopeful: allocations inside a call nested in
  an expression are not reclaimed until the enclosing statement completes. That is
  stated in `docs/language.md` instead of being left for someone to discover as an
  OOM.
- The GC's behaviour is observable without source edits, which is what made the
  soundness harness possible: `--gc-stats` and the corpus-wide stress run say
  whether the collector ran, how many roots it traced, and how many it proved were
  not handles.
- Both backends keep their existing safe-point *policy* (the AOT collects at every
  statement in a body), so parity is preserved by construction rather than by
  luck — and the corpus runs under collection stress to keep it that way.

## Alternatives rejected

- **Leave collection where it was** (explicit `Collect()` calls from tests only) —
  rejected: a language whose REPL grows without bound is not finished, and a
  collector nobody calls is not a memory model.
- **Conservative scanning of the host stack / all words** — not available: Go gives
  no way to walk the interpreter's own frame, and scanning `[]int64` arenas
  opportunistically is exactly the guessing this ADR removes.
- **An allocation-count *timer* (collect every N bytes of allocation, wall-clock
  based)** — rejected: collection timing must be reproducible or cross-backend
  parity assertions are noise.
- **Root every live Go local by hand-listing them at every `eval` site** — rejected:
  it is unreviewable and unfinishable. The watermark gives the same guarantee as a
  birth-time anchor for the whole young region, with no per-site bookkeeping.
- **Advance the watermark inside every callee** — rejected as unsound: the enclosing
  expression holds results in registers; only statement-root calls have a rooted
  caller side.
- **Keep the AOT's static root slots and patch recursion specially** — rejected: the
  bug is the model (one slot per name cannot express two live frames), not an edge
  case; a root stack is both the fix and the precision win.
- **Make `--gc-stats` stdout-only** — rejected: the report describes the tool, so it
  goes to stderr, and `--json` carries it as data (ADR 0169's rule about diagnostics).

## Compiled backend (same round, second commit)

The compiled backend had a *table*: `xs = [...]` recorded the address of `%_xs` at
index `f(scope,name)`, allocated once per function body. Three properties followed from
that single design, all wrong:

- a recursive call re-registered the same index, so the inner frame's list **replaced**
  the outer frame's root — a collection inside the callee swept a list the outer frame
  was still about to read (`integration/programs/gc_precise.gy`: interpreter 130, AOT 346);
- every index ever registered was scanned for the whole run (`store i32 1024,
  @gc_roots_used`), so precision was a matter of luck;
- a slot could hold an `int`, and avoiding marking a random object at that integer's
  index relied on small ints falling outside `@heap` — the same guess this ADR removes
  in the interpreter.

**Decision: the array became a stack, with the discipline in the target's runtime.**
`@gc.roots` holds slot addresses, `@gc.kinds` says whether an entry names a heap
handle, and four primitives do the work: `rt_root_put` (append, or retag an existing
entry for that slot — deduped by scanning down from the top, so a loop that re-executes
its assignments cannot grow the stack), `rt_root_clear` (tag dead), `rt_frame_open`
(record the base), `rt_frame_close` (pop back to it). Every function-emitting path in
codegen opens a frame in its prologue and pops at every return and unwind edge:
`funcDef`, the closure `_env` helper, the decorated `_impl`, and `emitClassMethod` —
which was the last one found, and the one whose absence turned a 50 000-instance loop
into a trap.

**Two consequences that were not obvious, both measured:**

1. *Every handle store must re-push.* Once clearing is real, a variable rebound from a
   scalar to a container (`s = {1, 2}` after `s = 5`) would otherwise leave the new
   object tagged dead: it got recycled while in use, and the symptom was a set whose
   insert loop never terminated. Hence `gcStoreHandle`, which pairs the store with a
   push. Dedup makes the repeated push cost a scan of a handful of entries.
2. *A slot must have one address per call.* Codegen emitted `%_p = alloca i32` where the
   variable was first assigned — inside a loop body — and at llc's default `-O0` nothing
   hoists it, so each iteration bumped a fresh frame slot: the machine stack grew per
   iteration, and the root entry never matched (`top` reached **2002** for a 2000-iteration
   loop), every stale slot kept its object live, the 1024-slot heap filled, and the
   program died. `hoistAllocas` moves every allocation to the top of its own function
   (`top` after the fix: **3**), which is also what a local variable means in C.

**Capacity is explicit and fails loudly.** 4096 entries is (container variables in
scope) x (call depth). Exhausting it means a handle that cannot be rooted, so the target
writes a diagnostic to fd 2 and executes `llvm.trap()` rather than continue with a
collector that cannot see one of the program's objects. `top=` in the report shows how
much headroom a workload actually uses.

**Self-report.** The counters live in the program's globals, so the target prints the
line (`rt_gc_report`, on fd 2 like every other tool-level line — ADR 0179); `--gc-stats`
turns it on, the JIT captures fd 2 so the CLI can forward it unchanged, and `--json`
carries the same fields as a `gc` member (`ParseGCStatsLine` is the single parser, and
round-trips against the interpreter's `GCStats.String`).

**Tests are calibrated against stubs, not against hopes.** `pkg/lang/gc_roots_ir_test.go`
checks the structural invariants over a corpus that exercises every function-emitting
path (a rooting function must open a frame and pop before each `ret`; no allocation may
sit below the first instruction). `integration/gc_stress_test.go` checks behaviour
through the runtime. Each was verified to fail when the mechanism was removed:
stubbing `rt_frame_close` gives "19 objects still live at the end"; disabling
`hoistAllocas` gives "root stack grew to 1210 entries". The first version of the runtime
assertion (`top < 64` only) passed with frames never popping at all — an assertion tuned
until a stub fails it is the only kind worth having.

## Alternatives rejected (compiled backend)

- **Snapshot the value at push time instead of the slot address** — rejected: the root
  would go stale the moment the variable is reassigned, which is the common case in loops.
- **Stable compile-time slot ids (per-function indexes)** — rejected: correct only if each
  variable has one address per call, which is exactly what the alloca-in-loop discovery
  says codegen was not providing; rooting by id also loses the ability to see the current
  value of a slot that a callee mutated.
- **Close a frame per loop iteration** — rejected as unsound: a container bound in a loop
  body is routinely read after the loop ends (`print(p.x)`), so its root must outlive the
  iteration.
- **Leave `store i32 1024, @gc_roots_used` (scan everything registered) as the safe option**
  — rejected: it was the source of both the clobbering bug and the guessing; the
  4096-entry stack with an explicit overflow trap is both safer and cheaper.
- **Emit the root runtime in the module preamble** — rejected after the fact: whether a
  module touches the root stack is only known once the body has been generated, and
  emitting the data half early produced modules whose `@gc.kinds` was undefined. Both
  halves are emitted at assembly time, gated on `rooted || heapUsed || report`.

## References

- `pkg/lang/gc.go` — root set, watermark, safe points, `GCStats`
- `pkg/lang/jit.go` — `swapScope`, `runFuncBody`, `exprDepth`/`exprBase`/`stmtRoot`, safepoint in `runStatements`; `pkg/lang/gc.go` — `runBodyRooted`
- `pkg/lang/gc_test.go` — behavioural tests (and the frame-root test that fails when frames are not rooted)
- `integration/gc_stress_test.go` — corpus-wide collection stress + heap bound
- `integration/programs/gc_precise.gy` — the conformance program
- `cmd/gustyc/main.go` — `--gc-stats`; `pkg/lang/schema.go` — `definitions.gcStats`
- `pkg/lang/codegen.go` — `rootRuntimeIR`/`rootGlobalsIR`, `rt_root_put`/`rt_root_clear`/
  `rt_frame_open`/`rt_frame_close`/`rt_gc_report`, `gcStoreHandle`, `gcOpenFrame`,
  `hoistAllocas`, `isScalarConst`; `pkg/lang/closure.go`, `emitClassMethod` — frame edges
- `pkg/lang/gc_roots_ir_test.go` — the structural invariants (stub-verified)
- ADR 0151 (Gap A: AOT dynamic dispatch and GC roots), ADR 0163 (assigned containers are
  heap handles), ADR 0164 (`verifyModule`-driven pipeline — the earlier case of "turning a
  real check on exposed three bugs nobody could see")
