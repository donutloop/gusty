# A body that answers the void answers it at every door — one verdict, `answersOnlyTheVoid`, asked by the fold that decides `is`/`==`

## Status

Accepted — 2026-10-28. Closes **roadmap Gap R.388** (filed 2026-10-23 by **ADR 0402**, as the control beside that
row's own new shapes) and finally pays **Gap R.228**'s Definition of done, written 2026-10-19 and false for one of
its two spellings ever since:

```gusty
def f():
    return None
print(f())            #  None  on both builds        — the print road, and the fall-off-the-end twin
print(f() is None)    #  False before, True after    — the reference prints True
print(f() == None)    #  False before, True after    — the same fold; the reference prints True
```

One question — *does this callee's body answer the void, and nothing else?* — had two readers and only one of them
had ever been told the answer. `print(f())` wrote `None`, correctly, for both spellings of the body.
`print(f() is None)` wrote `False`, at the **exit code of success**, for the spelling that writes `return None`,
while the spelling that falls off its end wrote `True`. The two shapes of the same value, decided by whether a
statement exists in a body.

Two rows filed from the same probe, neither bought: **Gap R.451** (a body that FORWARDS a void answer —
`def g(): return f()` — is not itself read as answering the void) and **Gap R.452** (a METHOD whose body returns
`None`). A third shape, the arm of a body whose other arm answers a number, stays **Gap R.324**'s standing row, and
a container `pop` over a name stays **Gap R.174**'s refusal — both pinned as what they are.

## Context

A value in this language is a `(payload, tag)` pair (**ADR 0187**), and the void's payload is the word `0` — the
same word an integer's slot legally carries. Every road that decides a question about a value therefore has to
consult a kind, and this backend has exactly two places a kind can come from before run time: the source text, and
**ADR 0368**'s `answerKindPlan`.

`pkg/lang/codegen_facts.go`'s `isNoneExpr` is the predicate the print door, `str()`/`repr()` and the REPL consult:
is this expression the void? Its Call arm had two answers — an in-place mutation (`xs.append(2)`), and a callee
whose body `fdReturnsValue` says never returns a value. That second answer is why `def f(): pass` printed `None`
AND answered `f() is None` with `True`: both roads, print and fold, agree when the body has no `return` at all.

The comparison roads do not use a kind. `codegen_arith.go`'s `mixedTaggedCompare` opens the pair door only for the
shape **ADR 0411** filed — an operand whose kind lives in an OBJECT — and everything else falls to the
None-equality fold, which decides "**from what the SOURCE says each side is**" (**docs/language.md**'s own words for
it). For a call, "what the source says" was `fdReturnsValue`: this body has a `return`, therefore it is not the
void. A body that writes `return None` has a `return`. So the fold compared the payload — `0` — against the void's
own `0`, in a road that had convinced itself the two could not meet.

`answerKindPlanOf` did have the right answer, and skipped it for exactly these functions:

```go
for _, name := range order {
    if p.carry[name] || pairReturns[name] {
        continue            // ← the pair road serves `def f(): return None`; the VOID verdict was skipped with it
    }
    arms, voidOnly, ok := answerBodyPlan(fd, p)
```

The skip is deliberate and correct for the ARMS — a pair-served body's tag is a run-time word and beats the plan's
constant one (**ADR 0368**). The bug was that it skipped the verdict too, and `answersOnlyTheVoid` is consulted by
name (`codegen_facts.go`, `order_kind.go`), by roads that never look at arms at all.

## Decision

**One verdict, recorded once, asked by every road that needs it.**

1. `answerKindPlanOf` records the void verdict for a pair-served body, and still takes no arms from it:

   ```go
   if pairReturns[name] {
       if voidOnly { p.void[name] = true }
       continue
   }
   ```

   The fixed-point rounds stay monotonic — every round only adds entries, which is what makes the loop's
   termination argument (**ADR 0273**'s, borrowed by **ADR 0368**) hold.

2. `isNoneExpr`'s Call arm asks that verdict before it asks the older one:

   ```go
   if g.answer.answersOnlyTheVoid(nm.Value) { return true }
   if fd, ok := g.fds[nm.Value]; ok && !fdReturnsValue(fd) { return true }
   ```

   with the existing guard in front of it intact: a body the plan CARRIES but does not call void-only still answers
   `false`, because which arm ran is a run-time word and a print road that claimed the void would print `None` for
   the arm that answered `7` (**Gap R.219**, **Gap R.228**).

No comparison road was widened, and no pair door was written for this. I tried that first — an
`isTaggedCompareSide`/`taggedOperand` arm routing the void call through `answerCallPair` — and deleted it: the
gate admitted the operand, `comparePair` had already emitted the call for its effects, and the pair door then
answered `rt_payload_eq(0, 0, 0, 3)` — `False` again, because the callee's tag slot is written by a road that this
program never reached. Two doors giving one value two answers is the defect class, not the fix (**ADR 0232**), and
the honest place for a source-decidable question is the fold that can decide it. The IR rows in the suite are
pinned against that temptation: no `@rt_payload_eq`, no `@rt_pair_truth`, no `@rt_icmp_values` for these answers.

The call is still EMITTED. `r = f() == None` folds to a constant, and a fold that dropped the call would keep the
answer and lose the program's effects — `def f(): print("side")` / `return None` prints `side`, and the IR row
counts `call i32 @gy_f()` exactly once in `@main` (**ADR 0172**'s "both operands are still evaluated", **ADR
0288**'s one evaluation).

## Evidence

- `pkg/lang/void_answer_test.go` (NEW): **23** answered programs judged on the compiled leg AND the record leg
  (`compileAndRun` + `HasGoldenAnswer` + `runGoldenStdout`, a MISSING record failing the case, **ADR 0302**); **13**
  rows in the positions a comparison stands — **9** reading the answer (`if`, a negated `if`, `while`, both
  short-circuits, a ternary, a loop element, a folded answer whose call prints, the answer used twice) and
  **4** controls that must NOT be read as the void (a two-arm body, a number body, a text body, and the same
  number body asked its own question); **4** print-and-compare parity rows;
  **7** rows asking `isNoneExpr` and the plan DIRECTLY over a parsed program, in both directions — plan-void ⇒
  predicate-void, and predicate-void ⇒ plan-void OR no `return` at all; **3** IR rows; **4** filed-not-fixed pins.
- `integration/void_answer_test.go` (NEW): **35** programs of the family through the CLI with every expectation
  read off the live `python3` in the same case (a reference that stopped printing what the row pins fails too),
  the record-leg audit beside it (**ADR 0302**), a **7**-row exit-code table (no answer spends exit 2, no refusal
  is silently answered, no sentence leaks `*lang.`/`%!s(`/`__cans`/`0x`), one machine-readable refusal row for
  the container `pop` (**ADR 0006**, **ADR 0346**), and **3** owed rows pinned beside the reference's bytes.
- Pins MOVED rather than deleted: the `Gap R.388` rows in `pkg/lang/the_void_question_test.go` and
  `integration/the_void_question_test.go` leave the owed tables for `theVoidAnswered()` /
  `voidQuestionFamily()`, with the applied-lambda twin beside them, and the same happened to the applied-`lambda`
  pair in `pkg/lang/lambda_answer_test.go::lambdaAnswerOwed` and `integration/lambda_answer_kind_test.go::lambdaAnswerKindPinned`
  — `print((lambda: None)() is None)`, pinned at `False`, is the same bug wearing the hoist's hat, and it stands
  in the answered family with `True`. Both `the_void_question` files' headers now say the row is paid, in the
  same words **ADR 0412** and **ADR 0426** used for theirs, and `docs/language.md` stopped describing the fold
  as the answer for a void-answering call.
- Measurement: **40 void-answer programs** beside a `git worktree` build of the parent (`324f5c3`, **ADR 0426**)
  and the pinned CPython — **17 moved to the reference, 19 held byte-for-byte, none moved away from it**; the 4
  that still differ are the pinned rows below, byte-identical on the parent. A **9618**-source sweep of the
  record corpus through both builds changes **nothing among the 9574 sources that were already on the record**;
  the **22** sources whose answer does move are every one of them sources this row newly recorded, which is the
  sweep reporting a blast radius of exactly one shape (the corpus had no pre-existing case for it).
- `tools/recmerge` wrote **44** entries from the reference with the compiled leg cross-checked (**9574 → 9618**) and
  REFUSED four whose compiled leg disagrees; those four are the pinned owed rows, which is the only place a
  disagreement may live (**ADR 0302**).

## Consequences

- The fold is sound where it was unsound, and it is still a fold: a source-decidable question costs no run-time
  door. Anything that later wants to route these through the tagged value word (**L11.1**) has one predicate to
  delete, not two roads to reconcile.
- `answerKindPlan` is now consulted by roads that take no arms from it. Anything that reads `p.void` may no longer
  assume "void ⇒ carried"; the two rows that read it (`isNoneExpr`, `order_kind.go`) were both read before this
  landed, and the direct-AST rows pin the pairing.
- Three shapes stay owed and are now pinned with bytes, so the next cycle fails a test rather than a hope: a
  forwarded void answer (**Gap R.451**), a method's void body (**Gap R.452**), and the arm of a body that also
  answers a number (**Gap R.324**).
- A DoD sentence in `roadmap.md` is now backed by the tests it describes. **Gap R.228**'s cell keeps its wording; it
  simply became true.

## Alternatives rejected

- **Widen the pair door instead** (`isTaggedCompareSide` + `taggedOperand` arms routing the void call through
  `answerCallPair`/`pairCallPair`). Tried and deleted: the tag word it would have read is stored by a road this
  program never reaches, so the door answered `False` a second time, from a different place. It also left the print
  road and the fold answering the same call from two sources of truth — **ADR 0232**'s defect, transplanted rather
  than cured.
- **Teach the fold about `return None` directly** (a `*NoneLit` arm inside the comparison road). Rejected: it puts a
  third copy of "is this the void?" in the compiler, in the one road that had already proved unable to answer it,
  and it would have to be re-written for every new position the question is asked at. The predicate that
  `print`/`str()`/the REPL share is the one place this question may live.
- **Set `carry` as well as `void` for a pair-served void body.** Rejected: `carries` is the gate that refuses to let
  a call's answer be read by a one-word position (**Gap R.146**/**Gap R.324**), and turning it on for these bodies
  changes five other doors to answer a question this row was about.
- **Record the four still-wrong shapes on the record leg.** Rejected by rule (**ADR 0302**): `tools/recmerge` refuses
  an entry the compiled leg will not agree to, and a record the backend cannot reproduce is a future failure with
  the wrong name.
