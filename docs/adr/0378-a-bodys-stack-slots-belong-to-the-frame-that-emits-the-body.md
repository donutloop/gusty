# A body's stack slots belong to the frame that emits the body

**Roadmap:** Gap R.342 (closed here) · Gap R.345 and Gap R.346 (filed here, measured by this cycle's probe) ·
ADR 0351 (one name, one slot, *per scope* — the half this keeps and the half that was missing) · ADR 0228 (a
possibly-unwritten local gets a flag, an alloca and a trap) · ADR 0181 (a slot's address is what identifies a
variable to the collector) · ADR 0375 (the float table is a frame fact: the same rule, one table earlier) ·
ADR 0166 (exit 2 is a compiler bug) · ADR 0211 (a compiler fault is never reported as a defect in the reader's
program) · ADR 0302 (one backend, so a rule that exists only on one road has no second engine to catch it)

## Status

Accepted. Landed 2026-10-19.

## Context

`g.allocd` answers one question, and a great many roads ask it: 「does this name already have a stack slot in
the frame being emitted」. The assignment road consults it before writing an `alloca`, `emitFreeOld` consults
it before releasing an old binding («only if this frame ever owned a slot for the name»), and
`copyInReboundParams` consults it so a rebound parameter owns its slot whichever road allocated it first.
`g.freshSlots` says which of those slots the current *statement* created, and `g.gcRootSeen` is the same
frame's root bookkeeping (ADR 0181 — it is the slot's ADDRESS that identifies a variable to the collector, so
the bookkeeping is per-frame by construction).

For the whole life of this backend those three maps were allocated once per **module**, and exactly one road
reset them: `beginScope`, taken by the `def` road and by the module's top-level code. The two roads that emit
a body without passing through `funcDef` never did:

- `emitClosureDef` (`pkg/lang/closure.go`) — the closure lift, which lowers a nested `def` to a top-level
  `define …_env(i32 %env)`, **emitted in the middle of the enclosing body's lowering**;
- `emitClassMethod` (`pkg/lang/codegen_class.go`) — the method emitter, in the same position relative to
  whatever body declared the class.

Neither had an opinion about the slot table, so both read the enclosing frame's. The measured shape is three
lines of ordinary source that the reference runs:

```
def outer():
    def inner():
        w = 3
        return w
    w = 1.5
    return inner()

print(outer())          # CPython 3 · this toolchain: exit 2, `use of undefined value '%_w'`
```

`inner` was emitted inside `outer`'s scope. When `inner` bound `w` it asked `outer`'s table, was told
「already allocated」 — an answer about a slot that did not exist *yet* and belonged to a different frame — so
the closure's `define` got no `alloca`. Then `outer`'s own `w = 1.5` asked the same table, got the same answer,
and emitted `store i32 %t7, i32* %_w` in a `define` that had never allocated `%_w`. `llc-20` rejected the
module and the program left at **exit 2**, the code ADR 0166 reserves for a compiler bug. Renaming either
binding made it compile — the signature of a name-keyed table that one road resets and another does not.

The written-flag table (ADR 0228) had the same hole one level down. `emitBoundAllocas` writes the entry-block
allocas of whichever body's flag table is currently installed, and both roads asked it *before* installing
their own:

| shape | what `llc` said | what the reference does |
|---|---|---|
| `def outer(): def inner(): w = 3; return w` over `w = 1.5` | `use of undefined value '%_w'` | prints `3` |
| `def outer(): def inner(c): if c: z = 1; return z` | `use of undefined value '%bnd_z'` | raises `UnboundLocalError` |
| `class C: def m(self, c): if c: z = 1; return z` | `use of undefined value '%bnd_z'` | raises `UnboundLocalError` |
| a nested `def` binding the container its enclosing body binds | `use of undefined value '%_xs'` | prints `11` |
| a nested `def` binding the text its enclosing body binds | `use of undefined value '%_s'` | prints `7` |

Five shapes, all exit 2, all found by writing 20 probe programs around one axis — *which frame owns the name*
— and asking each on both legs. Two of the five were container/text collisions rather than float ones, which
is how we know the fault was the frame entry and not the float convention ADR 0375 had just installed.

## Decision

**One rule, in one file: a body owns its own `allocd`, `gcRootSeen` and `freshSlots`.**
`pkg/lang/frame_slots.go` states it and hands out the entry:

```go
func (g *irGen) enterFrameSlots() func()   // installs NEW maps, returns the undo
```

It installs new maps rather than clearing the frame's own, because a nested body is emitted *inside* its
enclosing body's lowering and that frame still owns its locals afterwards. This is the same shape ADR 0375
chose for the float table, for the same reason, and it is now taken by `enterBody` — the one helper the
method road and the closure-lift road share with `beginScope`, which itself delegates to `enterFrameSlots`
so there is no second copy of 「which slots does this frame own」 to forget. Both roads take it **before** the
entry-block allocas, which is the half that was backwards: asking `emitBoundAllocas` while the enclosing
body's flag table was installed allocated the *enclosing* body's locals inside this `define`, marked them
allocated in this frame's table, and left this body's own possibly-unwritten name with no flag to set.

**And the compiler audits the module it produced, in the class ADR 0166 reserves for that event.**
`pkg/lang/frame_audit.go` walks each emitted `define` and asks, of every slot-shaped pointer operand
(`%_…`, `%_…_tag`, `%_ctr…`, `%bnd_…`), whether that name was allocated by an `alloca` **in the same
function**. It is asked of the shipped text — after `attachFrameStack`, which may add instructions, and before
`hoistAllocas`, which only moves an allocation within its own function — and only when nothing else has
already refused, so a program the compiler declined is reported as the refusal it got. Its failure is a new
error type, `*lang.IRInvariantError`, which the CLI classifies exactly like a toolchain rejection: exit **2**,
and under `--json` a payload with `phase: "codegen"`.

Why a new type rather than reusing `ToolchainRejectionError` (ADR 0211): that type's whole meaning is 「the
external tool said no」, and a module that never left the compiler was never refused by anyone. Why the audit
at all, when `llc` already catches this: because `llc` catches it at exit 2 with a temp path and someone
else's stack trace, and says nothing about *which function* or *which slot*; and because a rule that lives
only in prose can rot back on the next road that emits a body, which is precisely what happened to ADR 0351's
per-scope rule.

## What this deliberately does not decide

| shape | today | owner |
|---|---|---|
| a lifted `def` whose body answers a double (`def inner(): w = 1.5; return w`) | compiles, prints `0` at exit 0 — the lift opens `define internal i32 @…_env` whatever the body computes and reads its own `double` slot back with `ret i32 %_w.ld1` | **Gap R.345** (ADR 0377's fold, unwritten on the lift road) |
| two nested `def`s of the *same spelling* in two different frames | `@inner_slot = internal global` twice → `llc` refuses → exit 2 (verified identical on the pre-ADR build) | **Gap R.346** (ADR 0228/R.274's mint-name ownership) |
| a `def` nested inside a **method** | refused: 「not a function this backend can build a call to」 | Gap R.332 / Gap R.337 |
| a nested body reading an enclosing frame's *captured* name through a tagged word | capture-kind rows stand | Gap R.332 and its capture neighbours |

The frame entry is not a capture mechanism: a nested body reads the names the enclosing frame owns through the
environment (`%env`), never through the enclosing frame's stack slots, which are dead by the time the closure
runs. That is why `enterFrameSlots` installs empty maps rather than inheriting anything.

## Machine path

- `*lang.IRInvariantError` (new, `pkg/lang/frame_audit.go`) — `Error()` names the function, the slot, the
  offending instruction verbatim, the audit that spoke, and the roadmap/ADR pair. `cmd/gustyc` classifies it
  beside `*lang.ToolchainRejectionError` in both `toolchainExit` and `reportCompileErr`, so the exit code is
  2 and the `--json` payload is `{"ok": false, "phase": "codegen", "exit": 2, …}` whichever door the module
  left by (`--emit-llvm`, `--verify`, `--build`, `--file`, `--eval`).
- The record leg gained 15 entries through `tools/recmerge` — every source in both new test tables, so a
  source this rule decided cannot leave the corpus unrecorded (ADR 0302's rule, applied forwards). The
  conformance corpus gained `integration/programs/nested_def_shares_a_slot.gy`, registered
  `OracleMatch`: CPython runs all eight lines and the matrix asserts the compiled bytes against them, which is
  the leg the two compiled legs cannot certify between themselves.

## Alternatives rejected

- **Seed the nested body's table from the enclosing frame's.** It would have made the first probe print `3`,
  and made `outer`'s store legal by writing `inner`'s alloca — a shared slot for two frames, which dies the
  moment the closure outlives the call, and which contradicts ADR 0351's 「one name, one slot, per scope」 by
  deleting the 「per scope」. Rejected.
- **Rename the closure's locals** (`w` → `w$1`) so the tables stop colliding. Fixes the symptom by manufacturing
  unique spellings, leaves every other road (methods, the module frame, any future body emitter) un-entered,
  and makes the IR unreadable. Rejected.
- **Clear the maps instead of installing new ones** (`g.allocd = map[…]{}` with no undo). A nested body is
  emitted in the middle of its frame's lowering; clearing destroys the enclosing frame's record of its own
  slots, and `emitFreeOld`/`copyInReboundParams` then refuse to touch slots that exist. The save/restore form
  is what `enterBodyFloatTable` already does and why both are one helper. Rejected.
- **Move the entry into `emitFuncBody` alone.** The lifted `define` header, its entry-block allocas and its GC
  frame are written by the *caller*, before `emitFuncBody` runs — which is exactly where `%bnd_z` went wrong.
  An entry that arrives after the frame's own allocas has been written is not an entry. Rejected.
- **Leave the audit to `llc`.** Keeps exit 2 as the only witness, blames a temp file, and leaves the invariant
  unenforced on any path that does not reach `llc`. Rejected.
- **Report the audit's failure as an ordinary codegen error (exit 1).** ADR 0211's exact confusion: a compiler
  bug described to the reader as a defect in their program. Rejected.

## Tests

`pkg/lang/frame_slots_test.go` — 10 answer rows on the compiled leg against CPython's bytes, each cross-checked
against the record where the record covers it (15 of the file's sources are on the record); 2 written-flag trap
rows against the reference's `UnboundLocalError`; 3 catchability rows (the arm runs, the program carries on,
exit 0); 3 filed-not-fixed rows for the neighbours this rule does not answer; 5 IR rows asserting
that the `define` which *stores* through a slot is the `define` that *allocated* it (`gy_outer`'s `%_w`, the
lifted closure's `%bnd_z`/`%_z`, the method's `%bnd_z`/`%_ys`); the audit asked directly, with the forged
modules it must refuse and the legal ones it must not (a slot read through its load register, a runtime helper
whose allocas are hand-written); the wiring rows (the audit is asked of the shipped text, after
`attachFrameStack` and before the hoist; both roads take the frame entry *before* `emitBoundAllocas`); and
`TestTheFrameEntryIsAnEntryAndNotARename`, which asks the Go value — an entered frame starts with no slots, its
writes stay in its own maps, and the enclosing frame's bookkeeping comes back whole.
`integration/frame_slots_test.go` — 9 programs through `--jit` and `--aot` against CPython, with **exit 2 in
any row a failure of the file**; the trap rows through the CLI at exit 3; and the audit's machine path at the
CLI. `integration/programs/nested_def_shares_a_slot.gy` is the asserted conformance row.
