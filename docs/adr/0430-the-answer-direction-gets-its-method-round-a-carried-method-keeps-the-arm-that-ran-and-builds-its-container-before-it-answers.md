# ADR 0430 — the answer direction gets its METHOD round: a carried method keeps the arm that ran in a word of its own, and a body answering a container builds it before it answers

## Status

Accepted, 2026-10-28. Closes **roadmap Gap R.453**, the row **ADR 0429** filed against itself when it gave
the answer direction its VOID round for a body in a class and left the CARRIED round standing. Pays half of
**Gap R.324** (the module `def` twin of the same comparison). Files **Gap R.458**, **Gap R.459**, **Gap R.460** and **Gap R.461** for the
shapes this round declines — and pins the one shape that moved AWAY from a right-by-accident answer. Depends on **ADR 0368** (the carried answer word), **ADR 0429**/**ADR 0427**
(the one body reading, `answerBodyPlan`, and the class-qualified method resolution `methodBodyOf`),
**ADR 0333**/**Gap R.67** (the container answer built by the road that knows whether the literal needs the
heap). Supersedes nothing; widens **ADR 0189**/**ADR 0188**'s "no global in a word slot" to a class's return.

## Context

**ADR 0368** taught the compiler to keep, in a word of its own, the kind a `def`'s answer denotes: a body whose
arms answer two kinds cannot be answered by the source, so `answerReturn` stores `(payload, kind)` beside the
`ret` and the reading roads ask that word. **ADR 0427** then folded "is this answer the void?" onto the same
plan, and **ADR 0429** noticed the plan's reader stopped at the module's top level — it walked `prog.Stmts`
and asked `answerBodyPlan` of each `def`, never of a `ClassDef`. ADR 0429 repaired the VOID half of that
(as `methodAnswersOnlyTheVoid`) and filed the CARRIED half as Gap R.453.

What the missing half cost, measured on the parent build:

```python
class C():
    def m(self, x):
        if x:
            return 7
        return None

c = C()
print(c.m(1), c.m(0), c.m(0) is None)   # the reference: 7 None True   this backend: 7 0 False   — exit 0
```

`0` is **ADR 0187**'s legal payload for the void: the void's word is the same word an `int` slot carries, and
a fold that reads the payload instead of the kind prints a digit where the reference prints `None` and answers
`False` for `c.m(0) is None`. The same defect on the module side — `def f(x):` / `    if x:` /
`        return None` / `    return 1` / `print(f(True) == 0)` — printed `True` beside CPython's `False`,
because the equality against a NUMBER was folded from the source exactly as the equality against `None` had
been. One question, two spellings, one answer (**ADR 0232**).

The second symptom was louder. A method whose body answers a container:

```python
class C():
    def m(self):
        return [1, 2]

c = C()
print(c.m())   # the reference: [1, 2]   this backend: exit 2, llc: "expected instruction opcode"
```

A module `def` that answers a container is not carried at all: it is registered in `containerRet`, the call
site is told the answer is a container, and the ordinary call road lowers the handle (**Gap R.67**,
**ADR 0333** — which is the road that took `ret i32 @.lst1` off a `def`'s return). A method has no such table:
`registerMethodFunc` is dead code, `methodContainerAnswerKind` is read nowhere, and the method's return value
came from `g.value(arm)` — the folded GLOBAL. That global in an `i32` return slot is the shape **ADR 0188**
and **ADR 0189** forbid and `llc` rejects: a compiler bug, spent at **ADR 0166**'s exit 2, filed by ADR 0429
in `TestWhatTheVoidAnsweringMethodFiledNextDoorDiesInTheAssembler`.

The three behaviours the row had to keep were the reason this is a decision and not a patch. A method that
answers ONE immediate kind (`return 7`) is answered by the source perfectly well, and the whole suite of
existing method roads — `strFuncs`, the float table, the verdict census — keys off its return convention;
giving it a word it never reads would tax every one of them. A method that answers nothing but the void is
**ADR 0429**'s static fold, which is a better answer than a run-time word: carrying it would store a
write-only kind. And an answer compared against ANOTHER answer is a pair-vs-pair equality that
`comparePair` declines on purpose (`Gap R.324`'s standing half), because comparing two payloads whose tags
nobody has read is answered by coincidence.

## Decision

**1. The plan grows a method round, keyed by the emitted symbol.** `answerKindPlanOf` walks a `ClassDef`'s
`FuncDef`s beside the module's `def`s. The key is `gy_C_m` — the same `irSymbol(fmt.Sprintf("%s_%s", cd.Name,
mname))` that `emitClassMethod` and `resolveMethod` already mint — because the word is a property of the
emitted `define`, and a name-keyed table would give two classes' different bodies one answer (**ADR 0299**'s
reason for rejecting a name-keyed table, applied to a class's method name). Resolution walks bases in the
order the method table walks them, so an inherited or shadowed method reads the body the dispatch would call.

**2. One body reading, asked twice.** `answerBodyPlan` is split into `answerBodyArms` (the walk that stops at
a nested `def`, the arm reader `answerArmOf`, the varied/un-nameable verdicts) and the two callers that ask it
of a module `def` and of a method. There is no second implementation of "which kinds does this body answer?"
(**ADR 0232**), and the method round inherits every exclusion the module round has: a generator, a decorated
body, an `async`, a body with any un-nameable arm.

**3. A method is carried when a `def` would be, plus containers.** `answerBodyPlan` carries a `def` only when
its arms VARY, because a one-kind `def` with an immediate answer can be folded from the source; a
container-answering `def` does not need to be carried at all, because `containerRet` carries it instead. A
method has no `containerRet`, so `methodAnswerBodyPlan` carries a container-answering body too: the pair is the
only road by which a class's container answer can reach a door that reads containers. A void-only body is
excluded — ADR 0429 answers it statically and a stored word would be write-only — and a one-kind immediate body
is excluded, so `print(c.m())` of `return 7` still emits `ret i32 7` with no word declared (asserted in
`TestWhatTheMethodAnswerEmitsIsTheTagWordAndNotAGlobal`).

**4. `stmtReturn`'s carried arm is the method's return road.** `answerReturn` is the one function that lowers
`return <expr>` under the carried convention; a method now enters it. For a container arm it takes
`containerHandleOf`, falling back to `containerOperand`, so the `ret` carries a HANDLE made by the road that
knows whether the literal needs the heap — never `ret i32 @.lst1`. The method epilogue and the unwind target
store the void's word before their `ret i32 0`, so "fell off the end" and "unwound" cannot be read as a number.

**5. The word is `@gy_C_m.anst` — the module round's own convention, not a new cell.** `storePairTag` and
`pairCallPair` write and read `pairTagSlot(fn)` = `irSymbol(fn) + ".anst"`, which is how **ADR 0368** stores a
`def`'s answer kind. Choosing a runtime cell (`rt_pair_tag_set`/`_get`) instead would put a method's kind in a
different artifact from every other answer kind, and the JIT's per-cell dylibs would give the writer and the
reader different copies of it.

**6. The reading roads ask the plan, not a name.** A container answer is read by doors that key their
notebooks by a NAME (`containerRet[name]`), which is the one thing an object built inside a callee's frame is
not. Each of those doors grew the method arm beside its name arm, declining exactly where the name arm
declines: `pairCallPair` (the `(payload, tag)` every tagged road reads), `containerHandleOf` (`len`,
membership), `containerOperand` and `isContainerExpr` (a container equality, an index), `containerOperandRead`
(print, `str`/`repr`, an f-string, a condition, a loop, a comprehension), the binding road's
`recordContainerAnswer` (so `ys = c.m()` records the kind and the tag-carrying slots the same three facts
**Gap R.67** records for `ys = build()`), and `carriedCallIsPairElem` (a literal element that is a method
answer).

**7. The comparison asks the word for the void AND for a scalar.** `voidAgainstACarriedAnswer` covers
`==`, `!=`, `is`, `is not` where one side is a carried answer and the other is `None` or a written scalar
literal whose pair `taggedOperand` states (an int, a verdict, a text). It builds into a scratch buffer and
commits only when both sides answered — a read that emitted a bounds check and then changed its mind leaves
instructions after a terminator, which is **ADR 0166**'s compiler bug reported as the program's fault. A float
literal is deliberately excluded: the float table owns that answer word elsewhere (**Gap R.324**'s float
half). Two carried answers facing each other are DECLINED by this door and left to `comparePair`, which answers
`f(1) == g(1)` across two bodies with the reference's bytes (`True True False`) and is not this round to claim.
The shape it gets wrong is the one this round's own word-sharing exposes — TWO calls of the SAME carried body
in one expression, where one kind word says whichever ran last (`f(1) == f(0)` prints `True` where the
reference prints `False`) — and that is **Gap R.460**, pinned in both suites rather than smoothed over.

**8. What the round refuses, in words.** `sum`/`max`/`min` over a container the answer hands back
(`len` asks the object; the number roads never learned to) — and the module `def` twin refuses the same
sentence, so this is one door and one row: **Gap R.458**. A BOUND method answer measured or subscripted
(`ys = c.m()` / `print(len(ys))`) — where `ys = build()` measures: **Gap R.459**. Both are exit 1 with the
machine-readable capability document (**ADR 0006**), never exit 2.

## Evidence

Measured beside a `git worktree` build of the parent (`d493ce8`, **ADR 0429**) and a live `python3`:

* a **31**-program battery (the `pkg/lang/method_answer_test.go` answered family): all **31** print the
  reference's bytes today, and beside the parent **26 moved** to them while **5 held** byte-for-byte — the doors the
  payload already answered right (`bool()`'s truth, a condition head, the four-way `== "s"`/`== 1` quad on a
  text-and-number body, and the two one-kind controls). Across both suites' **42**
  deduplicated sources — the answered families AND both owed tables — the move is **34** with **6** held and
  the **6** owed rows wrong or refused identically on both builds; NOTHING moved away. The parent refused
  `c.m()[0]` with `index requires an inline list/dict/set literal`, refused `c.m()["k"]` with `index must be a
  constant`, printed a bare handle (`1`) for a container answer, and died in `llc` on `print(c.m())` of
  `return [1, 2]`.
* **40** sources newly recorded from the reference with the compiled leg cross-checked
  (`9706 → 9747`), `tools/recmerge` refusing none of them — so every answered row is agreed on by CPython and
  this backend before it entered the record (**ADR 0302**).
* `pkg/lang/method_answer_test.go` (NEW): **31** answered programs on the compiled leg AND the record leg
  (a MISSING record fails the case), **9** rows asking `answerKindPlanOf`'s method round directly and in both
  directions (with the un-carried rows asked of **ADR 0429**'s void predicate too, so "not carried" cannot mean
  "not read"), **5** IR rows, and **6** owed rows — three refusals (**Gap R.458**, **Gap R.459**, **Gap R.461**)
  and three wrong answers (**Gap R.460**), each pinned with the reference's bytes and the parent's beside it.
* `integration/method_answer_test.go` (NEW): **30** CLI programs each re-derived from a live CPython inside
  the case, **1** machine-readable JSON refusal, **6** owed rows pinned beside the reference.
* Deleted rather than edited, and moved into the answered families: two pins in
  `pkg/lang/void_method_test.go::TestWhatTheVoidAnsweringMethodStillOwesIsFiledNotFixed`, the **Gap R.324**
  pins in `pkg/lang/void_answer_test.go` and `pkg/lang/void_forward_test.go`,
  `integration/void_method_test.go`'s owed row and the container half of
  `TestWhatTheVoidAnsweringMethodFiledNextDoorDiesInTheAssembler` (whose nested-`def` twin is **Gap R.457**
  and still dies), and `integration/mixed_answer_kind_test.go`'s "a method's mixed arms" row.

## Consequences

* A method's answer is now a fact with two words, and the doors that read a container read it: `print`,
  `str`/`repr`, an f-string, `bool`, a condition, a binding, a list/dict literal element, `len`, an index, `in`,
  a `for` iterable, a container equality. None of them is a new door; each grew the arm its name-callee twin has.
* The emitted module carries `@gy_C_m.anst` ONLY for a carried method. A one-kind method's `define` and a
  void-only method's are byte-for-byte what they were, which is what lets this land without touching the
  `strFuncs`/`floatFuncs`/verdict-census conventions at all.
* A carried method is no longer registered as a text-returning method (`answerOwnsMethod`): a body whose arms
  vary is not "a method that returns a string", and `strMethodFuncs` claiming it is how an arm's `@str_tab`
  index reached a printer unchanged.
* `docs/language.md`'s void/method sections now describe a method's KIND as a run-time word rather than
  owing one; the two comparison rows that named **Gap R.453** as standing are re-pointed at what is still owed.
* The three refusals are pinned at exit 1 and the three wrong answers at exit 0 in both suites, with the
  reference's bytes — and the parent's — beside them, so closing **Gap R.458**, **Gap R.459**, **Gap R.460** or
  **Gap R.461** must break a pin.
* One item moved away from a right answer, and it is named rather than hidden: `print(bool(c.m(1)),
  bool(c.m(0)))` of a body answering `"s"` and `None`. The parent printed `True True` (the reference: `True
  False`); this build prints `False False`. Neither half is right now; the parent's `True` was the
  text-method table (**ADR 0368**'s one guess, **Gap R.219**'s class) claiming a carried body, which
  `answerOwnsMethod` correctly refuses, and the two arms then share one word. Its owner is **Gap R.460** —
  a per-CALL word, not a per-body one — and its program is pinned in both suites.

## Alternatives rejected

* **Register the method in `containerRet` under the bare method name.** It is the smallest diff — every
  container door starts working at once — and it is unsound: `containerRet` is keyed by the bare name, so
  `class C(): def m(self): return [1,2]` and `def m(): return 3` would share an answer, and the roads that
  read the table lower the call through a name-keyed notebook that the class's body never wrote. Two classes
  with one method name is the first control in `TestTheAnswerPlanCarriesTheMethodRound` for this reason.
* **A runtime cell for the tag (`rt_pair_tag_set`/`rt_pair_tag_get`).** Rejected on the same grounds
  **ADR 0368** used: a word declared beside the callee's `define` is one word, read by the caller that just
  called it; the JIT path compiles each REPL cell separately, and a global in the runtime would be shared
  across unrelated methods of the same name while a global beside the `define` is not.
* **Carry every method body, for uniformity.** Taxed the common case (`return 7`, `return "s"`) for a word no
  road reads, and inverted **ADR 0429**'s static fold into a run-time question that answers the same thing —
  the two-control case in the plan test and the IR test both exist to fail this.
* **Carry a void-only body and let the fold read the word.** Then `print(c.m())` of `return None` asks a tag
  the fold already knows, and the fold's own call-preservation rule (**ADR 0172**) has to be re-proved against
  a road that never needed it.
* **Set `pairRetDone` for a carried method, to make every position ask the pair at once.** `pairRetDone` is
  the pair road's PERMISSION for the positions it serves and the gate that REFUSES every other one; seeding it
  from the plan turned `while c.m():`, `c.m() + 1` and `len(c.m())` into compile errors at the exit code of
  success on programs the reference runs. The answer road is opt-in, position by position (**ADR 0273**).
* **Fold the mixed-arm comparison from the source with a smarter guess.** The payload is `0` for the void and
  a legal `int` besides (**ADR 0187**), which is exactly why `c.m(1) == 0` was answered `True`; no amount of
  source-side cleverness distinguishes them, because which arm ran is not in the source.
* **Leave the container method refused instead of carried.** It is the one shape whose module `llc` refuses —
  a refusal the toolchain does not get to choose (**ADR 0166** reserves exit 2 for the compiler's own
  mistakes), and the reference prints `[1, 2]`.
