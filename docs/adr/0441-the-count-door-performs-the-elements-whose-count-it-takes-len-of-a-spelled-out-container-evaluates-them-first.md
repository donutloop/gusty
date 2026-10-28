# ADR 0441 — the count door PERFORMS the elements whose count it takes: `len` of a spelled-out container evaluates its elements before it states the number

- **Status**: Accepted
- **Date**: 2026-10-28
- **Cycle**: the one after **ADR 0440** (which closed **Gap R.227** and filed **Gap R.494**, **Gap R.495**)
- **Roadmap**: **Gap R.494** closed; **Gap R.496**, **Gap R.497**, **Gap R.498**, **Gap R.499** filed
- **Builds measured**: parent `94a1458` (**ADR 0440**) and this commit, every number from a build made from the tree
  under test; the parent re-measured through a detached worktree, as **ADR 0440**'s lesson 3 demands.

## Context

`len([1, 2, 3])` is a number the spelling states. The length door answers it from the element list and never builds
the container, and nothing in the language lets a program notice the difference — until an element is not a value
but an operation. An element can call a function that prints, read a slot whose accident the subscript door raises
(**Gap R.227**, closed one commit ago), divide by zero, or run a comprehension that loops. The reference evaluates
every element expression, builds the container, and only then asks the object how many slots it holds.

Measured off `94a1458`, and every one of them at the exit code of SUCCESS:

| program | this backend | CPython |
|---|---|---|
| `print(len([[1, 2][0][0], 1]))` | `2` at exit 0 | `TypeError: 'int' object is not subscriptable` |
| `def f():` / `    print("side")` / `    return 1` / `print(len([f()]))` | `1` at exit 0 | `side` then `1` |
| `print(len([1 // 0]))` | `1` at exit 0 | `ZeroDivisionError: integer division or modulo by zero` |
| `print(len([[1, 2][5]]))` | `1` at exit 0 | `IndexError: list index out of range` |
| `print(len([1, [f(), 2], 3]))` | `3` at exit 0 | `side` then `3` |

The subscript door beside them raises the second row's `TypeError` for the same element written anywhere else —
`print([[1, 2][0][0], 1])` has raised catchably since **ADR 0440** — so one program told two stories depending on
whether a `len` happened to be reading the container. That asymmetry is the bug, and an exit-0 wrong answer is the
class this house files loudest (**ADR 0166** reserves exit 2 for the compiler's own failure and exit 0 for an answer
the program is entitled to).

## Decision

**The count door performs its elements.** `(*irGen).lowerLiteralElementsForEffect`
(`pkg/lang/length_element_effects.go`) is called from `builtinLen`'s `*ListLit` road and from the two roads that
answer a count off an element list a CALL produced (`dictMethodElems` — `"a b".split()` — and `listCallElems` — a
list-taking builtin such as `sorted([...])`), in `pkg/lang/codegen_builtin_seq.go`. Three rules, each measured
before it was written.

1. **A pure gate decides what performs.** `literalElementAsksToBeRun` answers *is this element an operation, or a
   spelling?*. Inert — evaluated to itself, no output, no raise — is a number, a text, a verdict, the void, a name,
   or a container literal all of whose parts are inert in turn. Everything else is an operation: a call prints and
   raises, a read raises, arithmetic raises, a comprehension loops. A `Name` is inert because an undefined name is a
   front-end error in this language, so a slot read cannot raise the reference's `NameError` from here. The gate is
   asked of the whole element list **before** the door touches a builder, which is what keeps the row's own negative
   control honest: `len([1, 2, 3])` still emits no instruction, no allocation and no builder.

2. **A container element is walked to its parts, not handed to `value`.** The first attempt called `g.value` on
   every acting element, and it is wrong for a reason worth keeping: the element road a container takes is chosen
   from what the container HOLDS (a static `{i32, [N x i32]}` global when every element reads as an int, a heap
   object otherwise), and a door that measures the container's SHAPE must not depend on that choice. `f()`'s answer
   reads as an int, so `[f(), 2]` takes the static element road, which refuses anything but an `IntLit` outright —
   the door's own "keep the count today" fallback swallowed the refusal and `print(len([1, [f(), 2], 3]))` printed
   `3` with no `side`. Going through the container to its PARTS and performing those builds nothing, which is also
   why a nested literal costs nothing: the object the reference allocates for it has no answer any program can read
   once the container holding it is itself thrown away for a count.

3. **Scratch, then commit.** Elements are emitted into a buffer and committed only when every one of them lowered —
   the discipline `chain_operand.go` and `answer_kind.go` exist for. An element whose read raises emits a trap that
   ENDS a block, and the rest of the program (the print of the count) still has to be emitted; the trap roads here
   open a fresh label as they raise (`constantTrap`, `emitBadNegation`), so the discarded count is harmless rather
   than an instruction after a terminator. The module the compiler hands `llc` is the one place **ADR 0166** refuses
   to spend exit 2, and this row is about a program that previously never reached `llc` at all.

An element with no lowering at all leaves the count the door answers TODAY, abandoning the whole attempt rather than
committing half of it: `len` of a dict literal whose value is a call has always stopped the build in the dict road's
own words, and it still does, unchanged. Refusing a program that has always answered `3` would trade a pinned, owned
divergence for a new unowned one; the shape that cannot be lowered stays the OTHER road's refusal, in its own words.

## Agentic rationale

Nothing about the interface moves — no flag, no schema field, no exit code. What changes is what an agent can trust:
a compiled `len` of a literal is now a value AND the effects the program wrote inside its argument, so a script that
generates `print(len([...]))` around calls gets the prints, and a generated program that expects a raise inside a
literal gets exit **3** with a catchable `TypeError`/`IndexError`/`ZeroDivisionError` instead of exit **0** with a
number. `--emit-llvm` is where the row's own negative control is read: `print(len([1, 2, 3]))` carries no
`rt_list_new`/`rt_dict_new`/`rt_set_new` call, and `print(len([f()]))` carries `call i32 @gy_f` — asserted at both
altitudes rather than claimed in prose. The trap is checked on the JIT's SPLIT streams (`JITResult.Output` is
exactly `side`, the raise on `Stderr`), which is the only way to assert an ORDER rather than a concatenation.

## Codegen / IR implications

* `pkg/lang/codegen_builtin_seq.go`: `lowerLiteralElementsForEffect` is called from the `*ListLit` road and from the
  two element-list roads of the `*Call` argument; the count returned is unchanged — the constant the spelling states.
* `pkg/lang/length_element_effects.go` (new): the gate, the whole-container question, and the element walk. It builds
  into a `strings.Builder` and commits with one `WriteString`.
* No new runtime helper, no new global, no new mark: the container the count is taken of is never built, so the marks
  the discarded object would have set are subtracted by never being set — **ADR 0192**'s `literalNeedsTags` subtracts
  one road's marks from another's; this door has no marks to leave.
* The dict and set roads are untouched: their heap roads already performed keys, values and members (that is why
  `len({"a": f()})` printed `side` before this commit), and their static roads accept only constant integers, so no
  acting element can reach them.
* `sum`/`min`/`max` are separate doors with their own static-array fold (**ADR 0239**) and are not claimed here.

## What this commit does NOT reach — filed, with today's bytes pinned

* **Gap R.496** — `len` of a comprehension the RUN TIME builds: the `*Comp` road loads the count field out of the
  fold's static global for a comprehension the fold never answered, and the module names a function-local register
  as a global (`invalid use of function-local name`) — **exit 2**. Its sibling doors of the same comprehension answer
  wrongly (`[g(x) for x in [1, 2, 3]][1]` traps `IndexError`, the loop over it prints `0`). Root: `g.compLen`/
  `g.compEls` asked where `g.rtComps` should be. Same root as **Gap R.490** at the loop/condition/truth doors.
* **Gap R.497** — a `def` inside a `try:` block has its whole `define` emitted into the enclosing block: `llc` says
  `expected instruction opcode`, **exit 2**, for a program with no `len` in it at all. Found when this cycle's rows
  wanted a helper bound *inside* the `try:` they catch around; the rows now bind before the block, and the shape is a
  row on the definition road.
* **Gap R.498** — the same statement in an `if`/`for`/`while`: `print(f())` after `if 1:` / `    def f(): …` refuses
  with `"f" is called but is not a function this backend can build a call to`, where the reference prints `1`.
* **Gap R.499** — an element that reads a verdict, a double or the void out of a container the fold answered is
  claimed by `numericElemUse` and answers `1`, so there is no raise for the count door to perform:
  `print(len([[True][0][0]]))` prints `1` at exit 0, and `x = [[True][0][0]]` / `print("ok")` prints `ok` too, which
  is what puts the defect on the subscript door and not here.

## Alternatives rejected

* **Build the container and call `rt_list_len`.** Honest about evaluation order and about the object, and it would
  put a builder, an allocation and a GC root into every module whose only job is to state a number — `len([1, 2, 3])`
  would stop being constant. Rejected: the count is a fact of the spelling; only the elements' EFFECTS are missing.
* **Lower every element through `value`.** Written, measured, rejected on `[f(), 2]` (Decision 2): the element road a
  container takes is a decision about its CONTENT, and a shape door that inherits it loses the effects of the very
  elements it exists to perform.
* **Evaluate elements and discard them with the container built into a scratch heap.** Rejected: the marks a builder
  sets are exactly what the row's `Next concrete action` warned about, and there is no answer the program can read
  out of the object.
* **Land the comprehension `len` fix (Gap R.496) in this commit.** Rejected: one commit per feature (**AGENTS.md**),
  and its fix is `g.rtComps` at a door that decides a container's KIND, which is the **Gap R.490** family — a
  different rule from "perform the operands you discard".
* **Land the def-in-a-block fixes (Gap R.497/R.498) in this commit.** Rejected on the same rule, and on measurement:
  both reproduce identically on `94a1458`, so they are rows, not damage from this one.
* **Delete the two **Gap R.494** rows from `folded_subscript_trap_test.go` and its CLI twin.** Rejected; they MOVED —
  to `pkg/lang/length_element_effects_test.go` and `integration/length_element_effects_test.go`, where they pin the
  reference's answer instead of today's. **ADR 0440**'s row-hygiene rule cuts both ways: a row that starts answering
  must move, and a filed table that empties itself proves nothing — so the filed table beside THIS door now holds the
  residue (**Gap R.496**, and the two neighbouring shapes measured by the same sweep), and the emptied tables carry a
  comment naming where each row went rather than an empty loop.

## Evidence

* `pkg/lang/length_element_effects_test.go` — `TestTheCountDoorPerformsTheElementsItCounts` (11 element shapes: the
  call first, last, middle, nested, a dict value, a set member, a `sorted` element, a tuple element, two calls in one
  literal, an acting element beside a nested `len`, a literal holding a run-time-built comprehension), each on the
  compiled leg (`compiledOut`) and the record leg (`runGoldenStdout`);
  `TestTheCountDoorsElementsRaiseWhatTheReferenceRaises` (5 traps, `jitTrap` + `recordTrapOf`, exit 2 forbidden by the
  helper); `TestTheCountDoorsElementsPrintBeforeTheyRaise` (the JIT's split streams: exit 3, stdout EXACTLY `side`,
  the raise on the other channel); `TestTheCountDoorsElementRaisesReachTheProgramsOwnHandler` (every raise caught by
  the program's own `except … as e:`, sentence compared);
  `TestTheCountDoorStillAnswersAContainerOfSpellingsWithoutBuildingIt` (7 controls + 5 module rows: no builder for a
  container of spellings, `call i32 @gy_f` present for one that acts);
  `TestTheElementAsksToBeRunPredicateIsAskedDirectly` (21 asks — the gate widened or narrowed by accident fails a
  decision row); `TestWhatTheCountDoorStillOwesIsFiledNotFixed` (the residue, with today's bytes).
* `integration/length_element_effects_test.go` — 12 answers, 6 raises (stdout checked for the SILENCE that means the
  count was never printed), 5 catches, 9 controls, an `--emit-llvm` builder row and the filed table, all beside a live
  `python3`.
* `tools/recmerge` wrote **30 entries (10005 → 10035), none refused** — including the two programs **ADR 0440**'s run
  refused to record, which record now because the legs agree.
* `integration/programs/length_element_effects.gy` joined the conformance corpus as a line-for-line `match` row.
