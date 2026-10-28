# ADR 0443 — a comprehension standing where the LOOP, the CONDITION or `bool()` asks for a container is walked, tested and measured as the OBJECT

- **Status**: Accepted
- **Date**: 2026-10-28
- **Cycle**: the one after **ADR 0442** (which closed **Gap R.496** and filed **Gap R.500**, **Gap R.501**)
- **Roadmap**: **Gap R.490** and **Gap R.500** closed together; **Gap R.502** filed; **Gap R.501** untouched
- **Builds measured**: parent `290aa59` (**ADR 0442**) and this commit, every number from a build made from
  the tree under test; the parent re-measured through a detached worktree, as **ADR 0440**'s lesson 3 demands.

## Context

Three programs, one missing question.

```
$ gustyc --eval 'for v in [x for x in [1]]:
    print(v)'
    llc-20: error: global variable reference must have pointer type
      %t3 = icmp slt i32 %_ctr1.ld1, @.lst2                                            → exit 2
$ python3 (same)                                                  reference: 1

$ gustyc --eval 'def g(v):
    return v * 2
if [g(x) for x in [1, 2, 3]]:
    print("yes")'
    the same rejection, at the condition                                            → exit 2
$ python3 (same)                                                  reference: yes

$ gustyc --eval 'def g(v):
    return v * 2
for v in [g(x) for x in [1, 2, 3]]:
    print(v)'
    0                                                            at exit 0
$ python3 (same)                                                  reference: 2, 4, 6
```

**Gap R.490** filed the first two; **Gap R.500** the third. They are one defect wearing two faces, and the
reason for the difference is what the loop road already had: `case *ForStmt:` asks its iterable a chain of
questions and one of them is `containerHandleOf`, which has had a `*Comp` arm since **ADR 0439** — it hands
back the object (the heap register a runtime builder returned, or the fold's global materialised into the heap
by `containerOperand`, **ADR 0188**'s rule). What that arm answers is `""` for the KIND, correctly: for a slot
read (`xs[0]`, `d["a"]`) only the tag says what the payload is. A comprehension is not a slot read. The program
wrote `[`, `{a}` or `{a: b}`, and the kind is a fact of that spelling — the same question **ADR 0439** asks of
an element's tag and **ADR 0442** asks of the count.

Asked nothing, the arm took the handle, set `iterKind` to `""`, and the walk below was skipped for want of a
kind: the loop fell through to the numeric counter road, which compared the counter against the container's
GLOBAL for a folded comprehension (`llc` refuses — **ADR 0166**'s exit **2**) and, for a built one, found a
number to compare, succeeded, and bound the loop variable to the counter — `0` printed once, at the exit code
that means the program ran. The condition door and `bool()` had no arm at all: they probed the word the
comprehension arrived in, which is `@.lstN` (exit 2) or an allocation index (every comprehension "true", the
empty one included).

There was a third fault under those two, found only after the doors were fixed: with the kind asked and the walk in
place, the built loop printed **`4`, `6`, `4`, `6`** for CPython's `2`, `4`, `6` — four lines, at the exit code of
success. The comprehension's object had been allocated, parked in a register, and left unrooted while its elements
called into a function whose own body collects. That is **ADR 0181**'s rule rather than the loop road's, and it is
the second pillar of this decision.

## Decision

**One predicate, asked once, shared by the three doors.** `compContainerKind` (`pkg/lang/for_iterable.go`)
answers a comprehension's own "what kind of container are you" off the brace the program wrote — `CompList` →
`list`, `CompSet` → `set`, `CompDict` → `dict`, anything else → not a comprehension at all, the caller's own
question standing. It is a pure function of the AST node, not of a table, so it cannot be stale and cannot
disagree with the builder that wrote the object.

* **The loop** (`pkg/lang/codegen_for.go`): the `containerHandleOf` arm, when the handle arrives with no kind,
  asks the brace and then walks the object with `rt_list_len` / `rt_set_len` / `rt_dict_len` and
  `rt_get_elem` + `rt_tag_of` — the road a container variable's loop already uses, one road for a list whichever
  road built it (**ADR 0333**'s shape, **ADR 0339**'s rule that a loop asks its iterable what it is). A dict
  comprehension walks its KEYS at the stride the dict's entries live at, as a dict variable's loop does.
* **The condition** (`pkg/lang/codegen_truth.go`): a `*Comp` test materialises through the same container door
  and asks the object its length through `heapLenOf` — the `rt_heap_len` **ADR 0442**'s count door asks — and
  tests it against zero. Truth is a question about the object, not about the word under it: a handle is non-zero
  for the empty comprehension too.
* **`bool()`** (`pkg/lang/codegen_builtin_numeric.go`): the same question, the same helper, the verdict word the
  constructor's own select already emits (**ADR 0287**).
* **The object is rooted for the duration of its own build** (`gcRootDuringBuild` / `gcEndBuildRoot` in
  `pkg/lang/codegen_gc.go`, called by all four builders). This half was not in the first draft, and the loop kept
  printing `4`, `6`, `4`, `6` for `2`, `4`, `6` until it was added. A comprehension the run time builds is
  `rt_alloc`'d, its handle lives in a bare register, and the road that fills it is free to CALL — and a call's body
  collects (**ADR 0347**, **ADR 0317**). A live object reachable from no root is **ADR 0181**'s forbidden shape;
  **ADR 0347** rooted the container a LOOP walks, and this is the same rule one construct earlier, at the BUILD.
  The window closes when the last slot is written, so a comprehension nothing consumed is still collectable.

Four rules, each measured before it was written.

1. **The kind is asked from the spelling, never invented.** `containerHandleOf`'s `""` is preserved for the slot
   reads that need it; only a comprehension — the one shape whose kind the source states — is answered. Widening
   the door's own `kind` answer was the first draft and it is rejected: **ADR 0339** measured that teaching
   `isContainerExpr`/`staticContainerKind` a container without teaching the road beside it turns programs that
   answer into refusals.
2. **The handle comes from the door that knows how to materialise it.** The loop, the condition and `bool()` all
   take `containerHandleOf`'s answer, so a folded comprehension is put into the heap by the same
   `containerOperand` that **ADR 0188** taught that trick for; no door re-implements it, and no module states a
   container global as an operand.
3. **The element's tag is a run-time fact and stays owed.** Walking the object fixes the COUNT, the KEYS and the
   ORDER; it does not give the element a kind word. A comprehension the run time builds over an iterable of more
   than one kind therefore prints payloads where the reference prints `1`, `a`, `2.5`, `None` — measured
   byte-identical on the parent build and on this one, filed as **Gap R.502**, and it is why every answered loop
   row here walks an iterable of one kind. A loop item handed to a call is now refused in **Gap R.146**'s words
   (a (payload, tag) pair meeting a one-word position) where it used to reach `llc` at exit **2**: a refusal that
   names the missing word beats a module LLVM rejected.

## Agentic rationale

Three programs that died with **exit 2** — the code **ADR 0166** reserves for a compiler bug, and the one an
agent has no way to recover from — now answer at **0**; one that answered `0` at the code of success now walks.
`--emit-llvm` is the machine door for the claim and is asserted, not described: the loop's module carries
`call i32 @rt_list_len(` / `rt_set_len(` / `rt_dict_len(` per brace, the condition's carries
`call i32 @rt_heap_len(`, and BOTH legs forbid the instruction the rows were filed for — a line matching
`icmp slt i32 %_ctr` whose other operand is a `@.` global. A script that generates a loop over a comprehension
can now read the loop's answer; one that generates `if [...]:` gets the branch the program wrote instead of a
crash that looks like the toolchain's fault.

## Codegen / IR implications

* `pkg/lang/for_iterable.go` (new predicate `compContainerKind`) — the brace is the only source of the kind.
* `pkg/lang/codegen_for.go`: the `containerHandleOf` arm fills an empty kind from that predicate. No new helper,
  no new label shape: the walk is the one a container variable takes, GC rooting (`gcRootIterable` on the handle
  in the preheader, `gcReg` on the loop variable's slot) included (**ADR 0181**, **Gap Q.2**).
* `pkg/lang/codegen_truth.go`: a `*Comp` test emits `rt_heap_len` + `icmp ne … 0`; `and`/`or` reach it through
  the same door, so `if [x for x in [1]] and [x for x in []]:` takes the else arm as the reference does.
* `pkg/lang/codegen_builtin_numeric.go`: `bool` of a `*Comp` emits `rt_heap_len` + the verdict `zext`.
* No new runtime helper. `@rt_heap_len`, `@rt_list_len`, `@rt_set_len`, `@rt_dict_len`, `@rt_get_elem` and
  `@rt_tag_of` are the ones the container roads already call. The build root uses the two the variable roads
  already call too — `@rt_root_put` beside the allocation and `@rt_root_clear` after the last element — so the
  module grows two calls per comprehension and no new block shape.
* The numeric counter road is unchanged and still reached by every iterable that is not a container — the guard
  above it (**ADR 0339**) keeps refusing a container it cannot walk, and no comprehension reaches it now.

## What this commit does NOT reach — filed, with today's bytes pinned

* **Gap R.502** — the LOOP ITEM of a comprehension the run time builds carries no kind word of its own:
  `def g(v):` / `    return v` / `for v in [g(x) for x in [1, "a", 2.5, None]]:` / `    print(v)` writes `1`, `0`,
  `1`, `0` where the reference writes `1`, `a`, `2.5`, `None`, and `print([g(x) for x in [1, "a", 2.5, None]])`
  writes `[1, 0, 1, 0]` — the same bytes on `290aa59` and on this commit, which is what makes it a row and not
  damage. A built dict comprehension's KEY prints the `@str_tab` index for the same reason. The count, the keys
  and the order are the object's now; the element's tag is the L11.1 word still.
* **Gap R.501** — a subscript of a comprehension the run time builds (**ADR 0442**'s row), unchanged here.
* `bool` of a container VARIABLE or a spelled-out literal still refuses in **Gap R.131**'s words; a comprehension
  is answered because its object is the one this backend built and can hand to `rt_heap_len`. Pinned beside this
  row's residue so the difference between the two is a measured line, not a guess.

## Alternatives rejected

* **Root the comprehension at its CONSUMER instead of at its builder.** The consumer half is real (the loop road
  already parks the handle it walks, `gcRootIterable`) and it is not enough: the collection that recycles the object
  happens DURING the build, from inside the element's call, before any consumer exists. Rooting at the consumer is
  what left the first draft printing `4`, `6`, `4`, `6`.
* **Keep the build root open for the whole frame.** One line shorter, and it would have shipped — but a program
  that mints a comprehension per iteration would then hold every one of them. The measured difference is small here
  (`total_freed=2999` either way, because the build's slot is one slot per instruction site and gets reused) and the
  rule is not about this program: the window is the build, so the window closes with the build (**ADR 0181**'s
  clearing rule, applied to an allocation the program never named).
* **Give `containerHandleOf`'s `*Comp` arm the kind.** One line, and it would have shipped — but the kind a door
  answers is a permission some other door reads, and **ADR 0339** measured exactly that: teaching the deciding
  predicates about a container without teaching the road that walks it turned `print(1 in sorted(xs))` from an
  answer into a refusal. The kind now lives where only the brace can be read, and the door that reads it is the
  loop.
* **`iterKind = "tagged"` for a comprehension** (ask the object its kind per element through
  `rt_iter_len`/`rt_iter_elem`/`rt_iter_tag`). Rejected: that road exists for a slot the compiler cannot see, and
  a comprehension's kind IS visible; the tagged road would also throw away the dict's key stride the compiler can
  state, and would not carry the element tag either (**Gap R.502** would have been silently absorbed into a
  road that cannot fix it).
* **Fold the comprehension and unroll the loop.** The fold already exists for what it can write down, and the
  loop's literal road walks it; these rows are precisely the comprehensions the fold declines, and a fold that
  accepted `[g(x) for x in xs]` would be a call evaluated at compile time.
* **Land the element's tag (**Gap R.502**) in this commit.** Rejected on measurement and on the rule: it is the
  element's kind word — the builders' `runtimeCompNeedsSelfDescribingSlots` question — not the loop's, and it
  reproduces identically on the parent, so it is a row. One commit per feature (**AGENTS.md**).
* **Land the subscript door (**Gap R.501**) here.** Rejected: it is **Gap R.495**'s pair hand-off at a second
  base, a deciding door again rather than a measuring one.
* **Delete the **Gap R.500** rows from the filed tables.** Rejected; they MOVED — to `compIterableLoops` /
  `compIterableTruths` and their CLI twin, where they pin the reference's answer instead of today's — and the
  emptied slot carries a comment naming where each row went (**ADR 0440**'s row-hygiene rule, and **ADR 0441**'s
  and **ADR 0442**'s practice). The filed tables beside them now hold **Gap R.502** and the **Gap R.131**
  neighbour, so a ledger still has something to hold.

## Evidence

* `pkg/lang/comp_loop_truth_test.go` — `TestTheLoopWalksAComprehensionStandingWhereItsIterableIs` (15 loops: the
  filed folded row, the filed built row, a runtime loop over a container variable, a filter, `range()`, a set
  comprehension, a dict comprehension's keys, a nested comprehension, `break`, `continue`, `else` on a walked
  loop and on an empty one, a comprehension loop followed by a plain one), each on the compiled leg
  (`compiledOut`, which runs `llc`/`lli` and so cannot pass a rejected module) AND the record leg
  (`runGoldenStdout`, asked with NO tolerance for a missing entry);
  `TestTheConditionAndBoolAskAComprehensionWhetherItIsEmpty` (13 rows: both filed conditions, both filed `bool`s,
  the empty comprehension false, a collapsing set comprehension, an empty dict comprehension, `not`, `and`, `or`);
  `TestNoModuleComparesALoopCounterWithAComprehensionGlobal` (four module rows naming the length helper per brace,
  plus the forbidden `icmp slt i32 %_ctr … @.` instruction, plus the condition's `rt_heap_len`);
  `TestWhatTheComprehensionLoopStillOwesIsFiledNotFixed` (**Gap R.502**'s three shapes and the one-word-position
  refusal); `TestTheComprehensionBraceAnswersTheKindQuestionDirectly` (6 asks — the gate widened or narrowed by
  accident fails a decision row).
* `integration/comp_loop_truth_test.go` — `TestTheLoopWalksAComprehensionThroughTheCLI` (14) and
  `TestTheConditionAndBoolAskAComprehensionThroughTheCLI` (18 rows: the answered conditions and `bool`s beside two
  **Gap R.131** controls and the `--emit-llvm` instruction-shape row), every one beside a live `python3` with exit
  **2** forbidden;
  `TestWhatTheComprehensionLoopStillOwesThroughTheCLI` (the residue, with the reference re-derived inside the case).
* `pkg/lang/length_element_effects_test.go` and `integration/length_element_effects_test.go` — the three
  **Gap R.500** rows left the filed tables for the answering tables above (they fail on the fix), and the emptied
  slot names where they went.
* `TestABuiltComprehensionSurvivesTheCollectionItsOwnElementsTrigger` (pkg/lang) and
  `TestABuiltComprehensionSurvivesItsBuildThroughTheCLI` (integration) — the rooting half: five programs whose
  elements CALL (the filed loop, a loop whose element collects nine times per item, the same build asked three ways
  by `bool()`/`len`/the loop, a collapsing set comprehension, a dict comprehension whose values collect), each on the
  compiled leg, the record leg and beside a live `python3`, plus the two module rows — `call void @rt_root_put(i32*
  %_cb` sits BETWEEN `= call i32 @rt_alloc(` and `call i32 @gy_g(`, and `call void @rt_root_clear(i32* %_cb` is there
  at all. Without the root the same two loops print `4`, `6`, `4`, `6` and `10`, `15`, `6` (control build, exit 0).
  The reclamation control: 3000 comprehensions minted by one loop run at `total_freed=2999`, `capacity=1024`,
  `grown=0`, `exhausted=0`.
* `tools/recmerge` wrote **35 entries (10051 → 10086), none refused** — the answered programs of both pillars, the
  corpus program in both of its shapes.
* `integration/programs/comp_loop_truth.gy` joined the conformance corpus as a line-for-line `match` row
  (33 lines of output, byte-identical to `python3`), the last four of them the rooting programs above.
