# The module's literal container is handed to the reading frame as a binding, not to a global as a handle

**Roadmap:** **Gap R.35** (the container half, closed here — a function cannot read a module-level name) ·
**Gap R.398** (the write-through half this row deliberately did NOT open) · **Gap R.399** (the one shape the
frame-slot audit still kills at exit 2) · **Gap R.400** (a slot whose element kind the answer direction cannot
carry) · **Gap R.401** (the closure and `lambda` roads take no prologue) · **Gap R.402** (the doors that ask the
module's records rather than the frame's) · **L11.1** (the tagged value word, which is what would make a copy
unnecessary) · **ADR 0227** (the two module tables this adds a third to, and the three rules about a value,
state and a local that still decide all of them) · **ADR 0376** (`module_float.go`, the sibling this file is
modelled on) · **ADR 0172** / **ADR 0267** / **ADR 0367** (the records a container's name carries are written by
the BINDING road, and by nothing else) · **ADR 0341** (the fold and the emission settle one element kind at a
binding) · **ADR 0220** (a body that binds a name anywhere owns it, and a module lookup happens at call time) ·
**ADR 0343** (a fact two frames cannot share out of one map) · **ADR 0232** (one question, one asking place) ·
**ADR 0249** (a refusal names what is missing, never the compiler's machinery) · **ADR 0166** (exit 2 is a
compiler bug; a refusal at exit 1 beats a wrong answer at exit 0) · **ADR 0302** / **ADR 0309** (the newly
answering programs are recorded from the reference, never self-certified)

## Status

Accepted. Landed 2026-10-23. Closes the **container half of Gap R.35** (the scalar half is **ADR 0227**, the
`double` half **ADR 0376**), and files **Gap R.398** (mutation and identity), **Gap R.399** (`xs + [3]` at
exit 2), **Gap R.400** (a returned slot that is not an integer), **Gap R.401** (closures and `lambda`s) and
**Gap R.402** (the doors that ask the module's records).

## Context

For five weeks the compiled backend answered this program with a refusal at exit 1 while CPython answered it
with `3`:

```gusty
xs = [1, 2, 3]

def n() -> int:
    return len(xs)

print(n())
```

The sentence was honest — `"xs" is bound at module level, and a compiled function body cannot read module-level
state that changes … (roadmap Gap R.35)` — and honest refusals are this project's preferred answer where the
machinery is genuinely missing. But the machinery was not missing here. **ADR 0227** had built one mechanism for
module state and **ADR 0376** had rebuilt it for a second word type, and a container had been left out on purpose
both times, for two reasons that are worth restating because this ADR does not repeal either:

1. **A container's word is a heap handle.** It has to be rooted (`@rt_root_put`) or the collector frees the list
   under the frame that is reading it — the failure mode this repo already names ("the list reads back empty").
2. **Every door that reads a container asks a name-keyed notebook.** `print` loads `%_xs` and asks which printer
   renders it; `len` asks `listVars`; a subscript asks `containerLits`; a loop asks `listElemStr`; equality asks
   the mixed bit. Those records — some twenty-odd of them — are written by the binding road
   (`stmtAssign` → the literal builders of **ADR 0189**, the element-kind settlement of **ADR 0341**) and by
   nothing else.

Handing a body the module's handle would therefore have meant either loading a word whose notebooks nobody owns
— which the frame-slot audit already catches as `%h1 = load i32, i32* %_xs` in a `define` that never allocated
`%_xs`, i.e. **exit 2**, the code **ADR 0166** reserves for a compiler bug — or re-implementing the binding road
beside the module door, which is **ADR 0232**'s forbidden move: two doors holding one opinion.

## Decision

**1. A third module table, built by one wiring function.** `pkg/lang/module_container.go` adds
`g.moduleContainers map[string]containerAnswer` beside `g.moduleSlots` (**ADR 0227**'s `i32` globals) and
`g.moduleFloatSlots` (**ADR 0376**'s doubles). All three are built by one call — `moduleTablesFor` — which hands
each later door the names the earlier one already owns, so one spelling never gets two owners. The generator's
old three-line sequence is gone; a test that asks the wiring asks the same function the emitter calls
(**ADR 0232**), which is how `TestTheModuleContainerGateDecidesEachNameItsOwnWay` can check the table name by
name instead of asking a program-level "did it work".

**2. A name earns the road only through a gate, and the gate is the whole feature.** A name is handed to a body
only when:

- its **one** module binding is a plain `AssignStmt` of a **self-describing** container literal — every slot
  itself a literal, no name and no call inside (`literalContainerIsSelfDescribing`). A slot that reads a name
  reads it where the copy is BUILT, which is the reading body, where a local of that spelling answers instead
  (**ADR 0220** read sideways); a slot that calls allocates and can answer a container the program writes through;
- no other binder takes the name anywhere at module level — a `for` target, a `with … as`, a `match` capture, an
  unpacking target, an `import`, a `global`-style binding form, a second assignment (**ADR 0220**'s rule about a
  body's locals is applied to the module as a frame);
- it is neither constant-folded (**ADR 0227**'s first rule: a folded value needs no slot) nor already owned by
  the `i32` or `double` table;
- **nothing anywhere in the program writes through it**, under that name or under any name the handle reached:
  `xs.append(1)`, `xs[0] = 1`, `xs += [1]`, `for row in table: row.append(1)`, `ys = xs` followed by any of those,
  a method not on the read list, a user function handed the handle (**`moduleContainersAreReadOnly`**);
- it is not asked an **identity** question — `xs is ys`, `xs is not xs`, a dict keyed by the name — because those
  compare objects, and a copy is the right value in the wrong body;
- the body does not **take the name for itself**: a body that binds the spelling anywhere in itself reads its own
  binding, and the prologue asks that before minting anything, in both the gate and the emitter
  (`moduleBodyClaimsName`), because two slots under one name in one frame is the load-nobody-owns shape again.

**3. The copy is bound by the binding road, in the reading frame.** `bindModuleContainerReads` hands `stmtAssign`
an `AssignStmt` whose value is a **copy of the module's literal AST** (`copyASTExpr`) and lets it do everything it
already does at module level: allocate `%_xs`, zero it, root it, mint `%h1 = call i32 @rt_alloc`, write each slot
with `@rt_set_elem` / `@rt_dict_put`, and record the kind claim, the element notebooks, the mixed bit and the fold
record. Nothing in this ADR writes one of those records itself. The prologue runs **before** the body, so the read
is never the first touch (`TestTheCopyIsBuiltBeforeTheBodyReadsIt` pins the order, which a `Contains` IR check
cannot), and it is emitted in a stable (sorted) name order because the prologue is IR and two compiles of one
program must emit one module.

**4. Two roads take the hook; a third does not.** `funcDef` (**pkg/lang/codegen_func.go**) and `emitClassMethod`
(**pkg/lang/codegen_class.go**) call it — the method road is emitted by its own function and shares none of the
`def` road's prologue, so a method reading the module's list is answered only because the hook is taken twice.
`emitClosureDef` and the `lambda` road do **not** take it: a nested `def` or a `lambda` that reads the module's
container keeps the refusal, which is **Gap R.401**, asserted as a refusal rather than quietly left out of the
tables.

**5. The escape question is asked of the literal, not of the node's shape.** A body that hands the object out
declines the name — `return xs`, `return xs + [1]`, `return pack(xs)`. A slot of it, `return xs[0]`, used to
decline it too, which declined the name for the WHOLE program: one body writing `return xs[0]` left every other
body reading `len(xs)` refused. The question is therefore asked of what the module actually bound
(`literalSlotAnswersInts`, handed the candidate's literal by the gate):

| module binding | body | answer | why |
|---|---|---|---|
| `xs = [1, 2, 3]` | `return xs[0]` | **answered** `1` | the slot is a number; a caller cannot write through a number |
| `xs = [1, 2, 3]` | `return xs[-1]` | **answered** `3` | the reference's last element, spelled from the other end |
| `d = {"a": 1}` | `return d["a"]` | **answered** `1` | the entry's value is a number |
| `xs = [1, 2]` | `return [xs[1], d["a"]]` | **answered** `[2, 7]` | a container the body BUILDS is not the module's object, provided every element is a value |
| `table = [[1], [2]]` | `return table[0]` | **refused** | the slot IS the module's row — the object wearing a subscript |
| `xs = ["b","a"]` | `return xs[1]` | **refused** | see below |
| `xs = [1.5, 2.5]` | `return xs[1]` | **refused** | see below |
| `xs = [True, False]` | `return xs[1]` | **refused** | see below |
| `xs = [1, 2]` | `return xs[9]`, `return xs[i]` | **refused** | a position the compiler cannot read, or one the literal does not hold |

**Integers and nothing else**, and that bound is a measurement rather than a taste. Run beside a build of the
parent commit, a body that returns a slot of its OWN list — no module anywhere in the program — answers the
reference's bytes for `7` and the wrong word for every other element kind: `["b","a"]` prints `1` (the text's
`@str_tab` index), `[1.5, 2.5]` prints `2`, `[True, False]` prints `0`, all at **exit 0**. That is the answer
direction's road (**Gap R.324**, **Gap R.219**, **Gap R.331**), not this door's; a name whose slots hold those
kinds therefore keeps the honest refusal at exit 1 rather than trading it for one of those numbers at exit 0
(**ADR 0166**'s ordering). Those two wrong-answer shapes are pinned in
`pkg/lang/module_container_test.go::TestWhatTheModuleContainerDoorsAnswerWrong` and
`integration/module_container_test.go::TestWhatTheModuleContainerRoadStillOwesAtTheCLI` as **Gap R.400**, and
each fails the day the tagged return road carries the kind.

**6. The emitter asks the write question again, and refuses when the two doors disagree.** `bindModuleContainerReads`
re-asks `moduleBodyClaimsName` and `moduleBodyWritesThrough` for the body it is about to bind. If the gate said
"read-only" and the body writes through the object anyway, that is a gate bug — and the response is the family's
own module-state refusal, never a copy. Two doors reading one rule from opposite ends is how a stale container at
the exit code of success survives a review.

**7. A copy is honest only where nothing writes, so the write half is a row, not a hole.** Every mutation,
augmentation, alias-write, identity and multiple-binding program in the family keeps the refusal at exit 1 while
CPython runs it, and `pkg/lang/module_container_test.go::TestWhatTheModuleContainerGateWillNotBuy` and the CLI
table beside it assert the sentence for each. That is **Gap R.398**, whose answer is the tagged value word
(**L11.1**) carrying a rooted container as module state — not a wider gate here.

## Machine path

Nothing new is advertised, and that is the point: no new flag, no new capability code, no new schema field. The
refusal sentences are the ones the module family has always used, so a refusal remains
`--json` `ok:false` / `exit:1` / `phase:"codegen"` with the sentence inside `"error"` and no Go type name in it —
asserted for two shapes in `TestTheModuleContainerRefusalIsMachineReadable`. The answer paths are the existing
ones (`--eval`, `--file`, `--repl`, `--emit-llvm`, `--verify`, `--build`, `--oracle`).
`g.moduleContainerReads` records what THIS frame was handed, so a diagnostic or an IR row can ask the emitter what
it bound (`moduleContainerBound`) without reaching into the slot tables, and it is per-frame by **ADR 0343**'s
rule about a fact two compiles — here two frames — cannot share.

## IR implications

For `xs = [1,2,3]` / `def n(): return len(xs)` the body of `@gy_n` is

```
%_xs = alloca i32
store i32 0, i32* %_xs
call void @rt_root_put(i32* %_xs)
%h1 = call i32 @rt_alloc(i32 1)
…  call void @rt_set_elem(i32 %h1, i32 0, i32 1) …
store i32 %h1, i32* %_xs
…  %t2 = load i32, i32* %_xs
%t3 = call i32 @rt_list_len(i32 %t2)
```

and the two module doors' spellings are **denied** in it: no `@gy_mod_xs`, no `global i32` for the name. A
container read by a body is a binding a frame owns, not module state, and the copy is built by the only road that
has ever built one — which is also what keeps `@main`'s own binding intact (`the module still builds its own
literal in main` is a row of the IR table, because a copy that replaced the module's binding would make the
module's own `print(xs)` read a different object than the one the program wrote).

## Alternatives rejected

- **A third `@gy_mod_<name>` global holding the handle.** Rejected twice over. It loads a word whose notebooks
  nobody owns (the frame-slot audit's exit-2 shape, and the pre-ADR 0223 class of bug that ADR 0225, ADR 0227 and
  ADR 0366 each had to chase); and it makes the IDENTITY questions answer wrongly rather than refused — with one
  shared handle, `f() is xs` answers whichever opinion the emitter holds instead of refusing the question a copy
  cannot answer.
- **Closure conversion / a module-scope slot table** (lift every body and pass the environment): the honest long
  answer, and the one **Gap R.35**'s remaining half and **L11.1** point at. It re-plumbs every body in the emitter
  — the method road, the closure lift, the defunctionalised clones of **ADR 0345** — for a class of programs
  (mutating a module container from a body) it would still not answer, because the missing thing there is the
  value word that carries its own kind, not the plumbing that delivers a handle.
- **Answering the read from the module's fold record** (which is what a declined name falls back to today): cheap,
  and measurably order-dependent — `print(f"{xs}")` alone in a body refuses while `print(xs)` written ABOVE it
  answers, because the interpolation field asks the record the print road wrote rather than the one the binding
  road owns. That is **Gap R.402**, filed with both spellings pinned rather than shipped as a coincidence.
- **Trusting the gate alone** (no emitter-side second opinion): rejected with the exit-2 class — the audit is
  process-wide, and a frame that mints a second slot under a name the body already owns is a program the compiler
  blames itself for.
- **Widening the returned-slot rule past integers**: would ship `2` for `2.5`, `0` for `False` and `1` for `"a"`
  at exit 0 — the exact class **ADR 0166** and **ADR 0273** put above any refusal.

## Measurements (taken, not remembered)

- **The probe census.** 178 programs — the four containers × the doors that read one, plus the mutation, identity,
  shadowing, method, recursion, ordering and unnameable-slot shapes — run through the CLI on a build of the parent
  commit (`0c54a58`) and a build of this one, with `python3` asked beside both: **22 agreed with the reference
  before, 111 after; 89 newly answering; 0 regressed.** The 61 that still disagree are refusals at exit 1, each
  pinned with today's sentence and the reference's bytes. Two shapes reach the frame-slot audit at **exit 2**
  (`return xs + [3]` and its unindented twin) and four print an untagged word (`[1.5, 2.5]` → `2`,
  `[True, False]` → `0`) — all six byte-for-byte what the PARENT build does for the same source, so none of them
  is this door's; they are Gap **R.399** and the answer-kind family (Gap **R.324** · Gap **R.219** · Gap **R.331**),
  and each is pinned as an owed row rather than left as a footnote.
- **The record leg.** Every answered source was written by `tools/recmerge` from CPython and cross-checked against
  the compiled leg (which cannot write an answer, **ADR 0309**): **60 sources added, the record moving 8791 → 8851**, none refused. The whole-record comparison the suite runs reported **no new divergence** anywhere in the corpus, and one
  row that had been pinned as compiled-vs-record debt became paid: `xs = [1, 2, 3]` / `def n() -> int: return
  len(xs)` / `print(n())`, held on the ledger as "the compiled backend cannot build a program the engine ran", now
  answers the record's and CPython's `3`. The ledger was rewritten by the artifact-writing mode alone (serially, per
  the gate contract): **319 → 318**.
- **The reference leg.** `integration/programs/module_container.gy` is a new asserted matrix row: 23 lines that
  are line-for-line CPython (`oracle: match`, `conformant: true`), and the matrix moved 232 → **233 rows**,
  203 → **204 parity-asserted**, 0 failures, 0 oracle drift.
- **The negative controls.** Deleting the prologue fails the answer tables; the gate's clauses are asserted
  per-name by `TestTheModuleContainerGateDecidesEachNameItsOwnWay` (a gate that claimed every name in a program
  because one name moved — the broom **Gap Q.1** refused — fails there), and the IR table denies both module doors'
  spellings in every body it checks.
