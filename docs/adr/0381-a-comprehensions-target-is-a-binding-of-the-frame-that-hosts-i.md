# A comprehension's target is a binding of the frame that hosts it, so a body written inside it can capture it

**Roadmap:** Gap R.348 (closed here) · L11.7 (the row the applied `lambda` belongs to) · ADR 0380 (the cycle that
measured this shape and wrote the table this ADR wires) · ADR 0370 (the capture parameter an application fills, and
the gate that refuses a capture whose kind the value tables cannot state) · ADR 0378 / ADR 0379 (a frame's own tables:
stack slots, then a slice's targets — the two roads this one copies) · ADR 0375 (the same per-frame rule for the float
table) · ADR 0351 (one name, one slot, per frame) · ADR 0245 (the nested-`def` lift, which carries a name down one
frame at a time) · ADR 0273 (a door that cannot answer says so; a wrong value at exit 0 is the defect this repo files
rather than ships) · ADR 0166 (exit 2 is a compiler bug, so a parameter minted for a register nobody allocated is
failed for explicitly) · ADR 0302 (one backend, so a rule missing on one road has no second engine to catch it) ·
Gap R.35 (the module-global rule this row deliberately does NOT touch) · Gap R.332 (the sibling it inherits: a capture
whose value is not a number is refused)

## Status

Accepted. Landed 2026-10-20.

## Context

Four lines of ordinary source. The reference answers the first three; the fourth has answered here for a year.

```python
print([(lambda z: z + x)(1) for x in [10, 20]])                     # the reference: [11, 21]   here: undefined name "x"
print([(lambda y: y + x)(2) for x in [10, 20]])                     # the reference: [12, 22]   here: undefined name "x"
print([(lambda y: (lambda z: z + x + y)(1))(2) for x in [10, 20]])  # the reference: [13, 23]   here: undefined name "x"
print([(lambda fn: fn(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]]) # the reference: [4, 6, 8]  here: [4, 6, 8] ✓
```

The fourth line is the control, and it is what makes the diagnosis short: the same loop variable, reached as an
**argument**, answers. What differs is only *where the name is read*. An argument is lowered by the comprehension road
itself, which has the target on its tables; a body reads it from inside the `define` the closure road mints for that
body, and a `define` reaches neither another `define`'s registers nor the module's storage for a name the module never
bound. `lambdaFrameCaptures` reported `x` correctly as a name the body is free in. The application that had to fill it
had nothing to read, and the compile died at the read:

```console
gustyc: jit: codegen: undefined name "x" (no binding for it; assign it before use) — the reference reports the same error
```

"Assign it before use" is a sentence about a program that never assigned `x`, and the program is not broken: the loop
assigns it, once per item, through machinery the source cannot see.

There was a second face, and it is the reason this is a row rather than a footnote. Where the module happened to spell
the same name, the generated body read the **module's** value and printed the wrong list at the exit code of success:

```python
x = 5
print([(lambda z: z + x)(1) for x in [10, 20]])    # the reference: [11, 21]   here: [6, 6]   exit 0
```

A refusal that hides a wrong answer is the worst artifact this repo knows how to make, and a `recmerge` run proved the
change on that very source: the recorder asks CPython first and refuses to write an entry the compiled leg agrees with
nothing about, so before this change the source could not be recorded at all — the compiled leg's `[6, 6]` was turned
away by the tool whose whole job is to refuse a self-certifying answer.

ADR 0380 had already measured this shape, found it identical on the pre-ADR build, filed it as Gap R.348 — and had
written the missing rule down in a file with no callers. `pkg/lang/frame_targets.go` arrived in that cycle holding the
four pieces the fix needs (`noteFrameTarget`, `forgetFrameTarget`, `frameOwnsTarget`, `beginFrameTargets`), the
`irGen.frameTargets` field, and the per-frame entry beside the slot table and the float table in `beginScope` and
`enterBody`. Nothing called the writer and nothing called the reader. The table was a law with no police: a
comprehension allocated its target's slot, and no road that asks a frame what that frame owns could hear about it.

## Decision

**Install the target where the capture road can read it, and ask it there — nearer than the module.**

1. **The comprehension road installs its target.** `runtimeCompLoop` and `runtimeCompList` already allocated `%_x` and
   stored each item into it; they now call `g.noteFrameTarget(lv)` beside the allocation and release it with
   `defer g.forgetFrameTarget(lv)`, so the name is owned for exactly the span the element and the filter are lowered.
   That span is the scoping rule: a binding that outlives the construct that wrote it is a lie the next expression
   would believe.
2. **The capture road asks the frame before it asks the module.** `lambdaFrameCaptures` no longer returns `nil` at the
   module's top level — the module's top level is a frame too, and it owns the targets of the comprehensions written in
   it — and the filter order is now: a name the frame owns as its own target is a capture, *whatever the module also
   binds*; otherwise a name the module binds is not a capture (that half is Gap R.35's, unchanged); otherwise the
   frame has the name and the application fills it. The ordering is the whole of the `[6, 6]` fix. The binding nearer
   a read is the one that read sees, which is the rule ADR 0220 wrote for module reads and ADR 0172 for slots.
3. **The table is a frame fact.** `beginFrameTargets` installs a fresh map per body and hands the host's back on the
   way out, from inside the same two helpers that own the slot table (ADR 0378) and the float table (ADR 0375): a body
   owns only the targets of the comprehensions written *inside* it, and what the host frame bound reaches it as a
   parameter. A host table left standing would mint a capture parameter for a register no `define` owns — `llc`
   rejects that, and ADR 0166 calls it our bug, so `TestTheFrameOwnsOnlyTheTargetsWrittenInsideIt` pins the nesting,
   the counts and the undo directly.
4. **A frame-owned target still has to cross as one word.** Nothing was relaxed in ADR 0370's gate: `emitLambda` asks
   `nameCarriesOneWord` of the supplying frame, and a target bound to a container, a text, a bool, the void or a float
   is refused there in that gate's own words. `TestWhatAComprehensionTargetRefusesWhenTheWordCannotCarryIt` pins three
   of those at the unit altitude and `TestWhatTheComprehensionTargetDoorRefusesExitsOneAndNamesTheRow` at the CLI, all
   of them exit 1 and never exit 2.

What the emitted module looks like after the change, for the first program:

```llvm
define i32 @gy_lambda_0(i32 %p0, i32 %p1)   ; %p0 is `z` (spelled), %p1 is the target `x` (forwarded)
  …
  store i32 10, i32* %_x                    ; the host frame's slot, one store per item
  %_x.ld1 = load i32, i32* %_x
  %t6 = call i32 @gy_lambda_0(i32 1, i32 %_x.ld1)
```

which is the shape ADR 0380 emits for a `def` local and a `for` target, with the comprehension's binding as the third
kind of name a frame can own. `TestTheComprehensionTargetEntersTheBodyAsACaptureParameter` asserts exactly that: two
parameter words on the `define` for one spelled, and one application per item carrying `%_x.ld`.

## What this row deliberately does not decide

- **A `for` target at the module level stays module state.** The reference leaks it, so the `for` road stores through
  `storeModuleScalar`, and a body reading one meets Gap R.35's module-global refusal — a different sentence, on
  purpose, and pinned beside the new rows in the same test file so the two roads cannot quietly merge.
  `frame_targets.go` is where that difference is written down.
- **A comprehension's target still leaks nowhere.** `print([x for x in [1, 2]])` followed by `print(x)` is a
  `NameError` in the reference and a front-end refusal here; `TestThatAComprehensionTargetLeaksNothingIntoTheModule`
  asks the shared checker and the run, because the answer the row installed is scoped to the element and the filter
  and to nothing else.
- **A target read by an entry lambda handed to a higher-order `def`** is still Gap R.336's (an entry is a bare symbol,
  ADR 0345); the row's fix makes the *read* visible to the capture road, not the entry word portable.
- Five neighbours measured on the way through and filed rather than absorbed, each with its own row and its own
  witness: **Gap R.349** (a capture of a name the module also spells answers the module — `1005` where the reference
  answers `6`, at exit 0), **Gap R.350** (a comprehension inside a comprehension: `[1, 2]` at exit 0 for one face and
  `llc` rejecting `rt_append_tagged(i32 %h1, i32 @.lst1, i32 0)` — exit 2 — for another), **Gap R.351** (a subscript
  on a comprehension the run time builds traps with `IndexError` at exit 3), **Gap R.352** (a comprehension's second
  `for` clause is a parse error), **Gap R.353** (a set or dict comprehension whose element or value is a call refuses
  `comprehension element must be constant`).

## Consequences

- Three programs the row names answer at exit 0 on all three witnesses, and `integration/programs/lambda_capture_chain.gy`
  now carries the chain inside a comprehension beside the loop it already walked — the Definition of done, met with the
  row's own refusals deleted rather than kept beside the answers.
- Eleven sources went to `tools/recmerge` and are on the record with the reference's answer, including the module-shadow
  case whose compiled leg was `[6, 6]` when the recorder was last asked and which it therefore refused to write.
- The frame table now has one writer pair and one reader, each called from exactly the roads the file describes. A
  fourth per-frame table added the same way costs one line in each of `beginScope` and `enterBody`, which is the
  pattern ADR 0375 and ADR 0378 established and this row joins.
- The machine path is unchanged and stays honest: `--callables` still says the defunctionalisation pass declines this
  program (the ordinary lambda road is what answers), and `--json` reports the answer at `"exit": 0`.

## Alternatives rejected

- **Install the target in module storage, the way the `for` road does.** It is the same one call, and it is wrong: the
  reference leaks a comprehension's target nowhere, so `print([x for x in [1, 2]])` then `print(x)` would answer `2`
  where the reference raises `NameError` — trading one wrong answer for another and breaking a refusal the record
  already holds. The two roads have mirror-image shapes because their scoping is mirror-image.
- **Let the module read win when both spell the name.** That is the `[6, 6]` answer, kept on the record leg as a
  counter-example precisely so nobody reinstates it: `x = 5` / `print([(lambda z: z + x)(1) for x in [10, 20]])`.
- **Keep the recording inside the comprehension road's element lowering, as the argument position does.** That is where
  the hole came from — a name recorded for the positions one road happens to lower, and invisible to every other road.
  The row is the installation, not another position-specific read.
- **Widen the capture road to module-bound names.** Gap R.35 owns compiled module globals; taking them as parameters
  would change what a program that compiles today passes across the boundary, which ADR 0273 forbids a widening to do
  silently.
- **Refuse the shape instead.** It was already refused, in the worst available wording — `undefined name "x"`, blamed
  on the program — while the same program, with one character changed (`z + x` to the argument position), answered. A
  refusal is only honest when the door genuinely cannot answer, and this door had the slot all along.
