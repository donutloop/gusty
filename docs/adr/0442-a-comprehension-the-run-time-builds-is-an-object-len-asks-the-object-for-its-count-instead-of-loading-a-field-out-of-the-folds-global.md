# ADR 0442 — a comprehension the run time BUILDS is an OBJECT: `len` asks the object for its count instead of loading a field out of the fold's global

- **Status**: Accepted
- **Date**: 2026-10-28
- **Cycle**: the one after **ADR 0441** (which closed **Gap R.494** and filed **Gap R.496**–**Gap R.499**)
- **Roadmap**: **Gap R.496** closed; **Gap R.500**, **Gap R.501** filed
- **Builds measured**: parent `3b13229` (**ADR 0441**) and this commit, every number from a build made from the tree
  under test; the parent re-measured through a detached worktree, as **ADR 0440**'s lesson 3 demands.

## Context

`print(len([x for x in [1, 2, 3]]))` prints `3` and always did. `print(len([g(x) for x in [1, 2, 3]]))` never became
a program at all:

```
$ gustyc --eval 'def g(v):
    return v * 2
print(len([g(x) for x in [1, 2, 3]]))'
    llc-20: error: invalid use of function-local name
      %t15 = load i32, i32* getelementptr({i32, [0 x i32]}, {i32, [0 x i32]}* %h1, i32 0, i32 0)   → exit 2
$ python3   (same source)                             reference: 3
```

There are two comprehensions in this backend, and `builtinLen`'s `*Comp` road knew only one of them. **ADR 0192**'s
fold writes an unrolled `{i32, [N x i32]}` global and records `N` in `g.compLen`; the four runtime builders —
`runtimeCompList`, `runtimeCompSetDict`, `runtimeCompLoop` (`pkg/lang/codegen_comp*.go`) and `nestedCompRuntimeList`
(`pkg/lang/codegen_comp_nested.go`) — call `rt_list_new`/`rt_dict_new`/`rt_set_new` and hand back a heap object in a
register, recorded in `g.rtComps`. The length door asked the FOLD's table for both, so for a built comprehension it
emitted the fold's load with the fold's type — `[%d x i32]` where the table was empty, so `[0 x i32]` — against the
object's register. `llc` rejects that module: **exit 2**, the code **ADR 0166** reserves for the toolchain's own
failure, spent by a program CPython answers with a digit. Fourteen programs of the family were measured against a
build of the parent commit: **thirteen** of them moved from exit 2 to the reference's bytes at exit 0, and two —
`xs = [g(x) for x in [1, 2, 3]]` / `print(len(xs))` and the count taken of a function's answer — already answered,
because the assignment and the container-answer roads had each been taught this question (**ADR 0333**, **ADR 0439**)
while the door beside them was not.

## Decision

**The count door asks who built the comprehension, and asks the right party.** In `builtinLen`'s `*Comp` road
(`pkg/lang/codegen_builtin_seq.go`), the handle comes from `g.comp` as it always did, and then:

* `g.rtComps[lit]` — the run time built it — the count is a word inside the object, so the question goes to the
  object through `heapLenOf`, the same helper a container VARIABLE's length asks (`call i32 @rt_heap_len`);
* otherwise the fold answered it — the compile-time load of the stored count field runs exactly as **ADR 0192**
  wrote it, with no builder, no allocation and no runtime call in the module.

Three rules, each measured before it was written.

1. **The predicate is the builders' own record, not a new guess.** `g.rtComps` is set by the four runtime builders
   and by nothing else, so the door cannot claim a comprehension is an object when the fold wrote a global, nor the
   other way round. A door that re-derived the answer would be a second opinion; the ADR 0438 lesson — one container
   door asked the tag road rather than keeping its own list — is the same rule one construct over.
2. **`rt_heap_len`, not `rt_list_len`.** The count is the same word for a list, a dict and a set, and a built
   comprehension may be any of the three; `heapLenOf` asks the object's own kind word. A per-kind call would have
   needed the door to know the kind, which is exactly the claim (`g.compLen`'s companion `g.compEls`) this commit
   refuses to make about a built comprehension.
3. **The fold keeps priority.** **ADR 0192**'s rule — a comprehension that folds IS the literal it folds to — is
   what makes `len`, `in` and `d[k]` of a folded comprehension answer statically, and `min`/`max`/`sum` still read
   those folded elements. This commit narrows the door that read a table the fold never filled; it does not widen
   the runtime road and it does not make a folded comprehension allocate. The two module rows in
   `TestTheCountDoorChoosesTheObjectOrTheGlobalByWhoBuiltTheComprehension` assert both halves: the built one carries
   `call i32 @rt_heap_len(` and NOT the `[0 x i32]` load, the folded one carries the `[3 x i32]` load and NO
   `rt_heap_len`.

The two rows that prove the number came from the OBJECT and not from the iterable are the collapsing ones:
`print(len({g(x) for x in [1, 1, 2]}))` answers `2` and `print(len({g(x) // 4: 1 for x in [1, 2, 3]}))` answers `2`,
where a count read off the iterable — which is what the fold's table would have said — is `3`.

## Agentic rationale

The visible change for a script or an agent is an exit code: fourteen generated programs that died with **2**
("a compiler bug", per **ADR 0166**, and the code that makes an agent retry a program that never had a chance)
now answer at **0**, and `--emit-llvm` shows why. The machine door for this row is that flag: the built
comprehension's module contains `call i32 @rt_heap_len(`, the folded one does not, and neither carries the
instruction shape `llc` rejected — asserted at both altitudes rather than claimed in prose, because an agent that
has to grep for `rt_heap_len` itself is an agent guessing. No flag, no schema field and no exit code moves: exit 2
stays reserved for the toolchain, which is the whole point of the row.

## Codegen / IR implications

* `pkg/lang/codegen_builtin_seq.go`: `builtinLen`'s `*Comp` road returns `g.heapLenOf(b, name)` when `g.rtComps[lit]`
  is set, and the fold's `load i32, i32* getelementptr({i32, [N x i32]}, …)` otherwise. Nothing else in the file
  changes.
* No new runtime helper and no new global: `@rt_heap_len` is the one a container variable's length already calls, and
  `heapLenOf` sets `g.heapUsed`, so the heap runtime arrives with it.
* `g.rtComps` gains its FIRST reader outside the slot doors. It was written by the four builders and read, until
  now, only where **ADR 0439** materialises an element's handle — which is why the fold's tables could be consulted
  for a built container for as long as the runtime road existed without anything noticing.
* The instruction shape **ADR 0188** forbids — a container global in an operand position — is what this door emitted
  in REVERSE: a container REGISTER in a global position. Both are now unreachable for this door, and the row's own
  module assertion forbids the second.
* The three sibling doors of the same object (the loop head, the condition, the truth test) and the subscript door
  are untouched — see below. `min`/`max`/`sum` keep their folded-element fold (**ADR 0239**) and still refuse a built
  comprehension in their own words.

## What this commit does NOT reach — filed, with today's bytes pinned

* **Gap R.500** — the loop head, the truth test and `bool()` standing over a comprehension the run time BUILDS
  answer the COUNTER and the handle instead of walking the object: `for v in [g(x) for x in [1, 2, 3]]:` /
  `    print(v)` prints `0` where the reference prints `2`, `4`, `6`; `if [g(x) for x in [1, 2, 3]]:` prints nothing
  where it prints `yes`; `print(bool([g(x) for x in [1, 2, 3]]))` prints `False` where it prints `True` — all three at
  **exit 0**, the class **ADR 0166** files loudest. **Gap R.490** is the same three doors over a FOLDED comprehension,
  where the module is rejected at **exit 2** instead; a built comprehension is the other road of the same missing
  question, measured identically on `3b13229` and on this commit.
* **Gap R.501** — a subscript of a comprehension the run time builds reads a slot the builder never filled:
  `print([g(x) for x in [1, 2, 3]][1])` traps `IndexError: list index out of range` at **exit 3** where the reference
  prints `4`. The fold cannot answer that base, `containerHandleOf`'s `*Comp` arm knows the handle, and the pair door
  `slotReadUnderTag` is opened only by a door that already holds the pair — which is **Gap R.495**'s missing hand-off
  arriving at the built road instead of the folded one. Also unchanged between the two builds.
* **Gap R.495**, **Gap R.497**, **Gap R.498**, **Gap R.499** — filed by **ADR 0440**/**ADR 0441**, untouched here.

## Alternatives rejected

* **`containerHandleOf(b, lit)` instead of `g.comp` + `g.rtComps`.** The handle would be right, but the door would
  then have to re-decide the kind it is not entitled to know, and `containerHandleOf`'s `*Comp` arm answers `""` for
  the kind and lets the helper ask the object — for `len` that is the same call, so the shorter road is the one that
  states the predicate the builders already record. Rejected as a wider door than the row needs.
* **`rt_list_len` alone, with a per-kind switch written from the comprehension's brace.** Rejected: a second source
  of the element's kind, which is the defect **ADR 0438** removed one door earlier, and a dict or set comprehension
  would take the list's helper.
* **Materialising every built comprehension through `containerOperand` and re-reading the fold's tables.** Rejected:
  the fold's tables are the thing that was empty.
* **Land the loop/truth/bool doors (Gap R.500) in this commit.** Rejected: one commit per feature (**AGENTS.md**), and
  that fix is a deciding door (`isContainerExpr`/`staticContainerKind`) acquiring a handle to hand — **Gap R.490**'s
  measured shape, where giving the arm without the handle leaves all three modules rejected. This row is a measuring
  door.
* **Land the subscript door (Gap R.501) in this commit.** Rejected on the same rule and on measurement: it is
  **Gap R.495**'s hand-off at a second base, and both reproduce identically on `3b13229`, so they are rows and not
  damage from this one.
* **Delete the **Gap R.496** row from the filed tables.** Rejected; it MOVED — to
  `countDoorBuiltComprehensions` and its CLI twin, where it pins the reference's answer instead of today's, and to the
  record (`tools/recmerge`) now that both legs agree. The filed tables beside it now hold the residue this commit
  measured and did not reach (**Gap R.500**, **Gap R.501**) so that a filed table never empties itself into
  proving nothing (**ADR 0440**'s row-hygiene rule).

## Evidence

* `pkg/lang/length_element_effects_test.go` —
  `TestTheCountDoorAsksTheObjectForAComprehensionTheRunTimeBuilds` (15 programs: the row as filed, an element that
  prints, a filter the folder cannot see, a dict and a set comprehension, the two COLLAPSING ones, a nested
  comprehension, the runtime loop over a container variable, `range()`, the count used in arithmetic, the
  comprehension bound first, the count taken inside a function body, a method-call element), each on the compiled leg
  (`compiledOut`, which runs `llc`/`lli` and so cannot pass a rejected module) and on the record leg
  (`runGoldenStdout`, asked with NO tolerance for a missing record — these sources are on the record now);
  `TestTheCountDoorChoosesTheObjectOrTheGlobalByWhoBuiltTheComprehension` (3 module rows: the built comprehension
  carries `rt_heap_len` and not the `[0 x i32]` load, the folded one carries the `[3 x i32]` load and no
  `rt_heap_len`, the runtime loop's road carries `rt_heap_len`);
  `TestWhatTheCountDoorStillOwesIsFiledNotFixed` (the residue, including the three **Gap R.500** shapes and the
  **Gap R.501** trap, and the rows **ADR 0441** filed).
* `integration/length_element_effects_test.go` —
  `TestTheCountDoorAsksTheObjectForAComprehensionTheRunTimeBuildsThroughTheCLI` (the same 15 programs through
  `--eval` beside a live `python3`, exit 2 forbidden on every row, plus the `--emit-llvm` instruction-shape row) and
  `TestWhatTheCountDoorStillOwesThroughTheCLI` (the residue, with a new `trap3` kind: a program that RUNS and raises
  what the reference does not raise is neither an exit-0 answer nor a build that never finished).
* `tools/recmerge` wrote **16 entries (10035 → 10051), none refused** — fifteen of them programs the compiled leg
  rejected at exit 2 on the parent build, which the record can hold now because both legs answer it, and the sixteenth the
  corpus program itself, whose record is what `TestGCCorpusCollectsAndAgrees` reads back.
* `integration/programs/built_comp_len.gy` joined the conformance corpus as a line-for-line `match` row, its three
  comment lines naming the row and the two COLLAPSE lines naming the claim.
