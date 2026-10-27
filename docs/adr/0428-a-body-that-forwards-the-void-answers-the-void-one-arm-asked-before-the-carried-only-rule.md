# A body that FORWARDS the void answers the void — one arm, asked before the carried-only rule, so `def g(): return f()` inherits `f`'s verdict

## Status

Accepted — 2026-10-28. Closes **roadmap Gap R.451** (filed 2026-10-28 by **ADR 0427**, from the probe that
closed **Gap R.388**). The sibling **Gap R.452** (a METHOD whose body answers the void) stays open, and so do
the two rows this one's probes re-confirmed as standing: a forwarded call whose answer is ONE ARM of a mixed
body (**Gap R.324**) and a forwarded call that hands back a PARAMETER (**Gap R.417** / **Gap R.171**).

```gusty
def f():
    return None
def g():
    return f()

print(g() is None)    #  False before, True after    — the reference prints True
print(g() == None)    #  False before, True after    — the same fold
print(g() == 0)       #  True  before, False after   — the reference prints False
```

**ADR 0427** had connected the comparison fold to **ADR 0368**'s answer plan, and the verdict reached exactly
as far as the callee's own name. One frame later the plan had nothing to say, the fold fell back to the
payload — the word `0` — and answered the void's question with `False` while calling the void's value a
number. The third line is the one that makes this a defect rather than a cosmetic gap: the same program
printed `None` for `print(g())` and `True` for `g() == 0`, which is two doors giving one value two answers
(**ADR 0232**).

## Context

`answerKindPlanOf` runs rounds, and a `return other(...)` arm is answerable only once `other` is **carried** —
**ADR 0368**'s fixed-point rule, so an inner frame writes its kind word before an outer caller reads it. The
arm is implemented in `answerArmOf`:

```go
case *Call:
    nm, isName := v.Fn.(*Name)
    if !isName || (fd != nil && nm.Value == fd.Name) {
        return answerArm{}, false           // a body answering itself: the inner frame, the outer caller
    }
    if !p.carries(nm.Value) {
        return answerArm{}, false           // ← a void-answering callee lands here
    }
    return answerArm{word: answerWordVaried, dyn: nm.Value}, true
```

A void-only callee is precisely a callee that rule skips. `def f():` / `    return None` is a one-arm body
whose arm is a literal, and the PAIR road serves such a body — **ADR 0427** had to grow `answerKindPlanOf`
just to record its VOID verdict, because `if p.carry[name] || pairReturns[name] { continue }` skipped the body
whole. The carried-only check above never got that treatment: `f` is not carried, so `g`'s arm returned
`ok=false`, and `answerBodyPlan` declines a body with ANY un-nameable arm. So `g` kept the roads
**Gap R.388** was filed for, one call deeper, and the control that localises it — `f() is None` printing
`True` in the same module — was already in the suite.

The same rule, met from the other side, had been hiding two programs behind a refusal. `f = lambda: None`
above `def g(): return f()` died with `"g" answers f$lambda$…(), whose own answer carries a kind word this
door declined: the answer direction cannot store a kind it was promised`, and the mutual pair
`def a(): return b()` / `def b(): return None` died the same way. Both are the reference's `True`.

## Decision

**Ask the void the arm, before the rule that would decline it.** In `answerArmOf`'s `*Call` arm:

```go
if p.answersOnlyTheVoid(nm.Value) {
    return answerArm{word: answerWordNone}, true    // a CONSTANT, not a forwarded word
}
if !p.carries(nm.Value) {
    return answerArm{}, false
}
return answerArm{word: answerWordVaried, dyn: nm.Value}, true
```

1. **A void-only callee has no word for the outer frame to wait on.** The carried-only rule protects the
   ordering in which kind WORDS get written; a body whose every road answers `None` has one answer and it is
   a constant. The forwarding arm therefore answers `answerWordNone` rather than `answerWordVaried`, and the
   fixed point still settles monotonically — the rounds **ADR 0368** already run are where a chain
   (`h → g → f`) and a mutual pair (`a → b → None`) reach their verdict, and nothing is removed between rounds.
2. **It is asked BEFORE the carried-only check**, because the callee may be carried (`def f():` /
   `    return 1`… no — a body the plan carries) or pair-served (`return None`), and the forwarding body's
   answer is the same either way. Ordering it after that check is the bug: the pair-served case, which is the
   only case this row has, never reaches it.
3. **The arm is a kind, not a substitution.** The forwarding body still emits its call — `@gy_g`'s body
   carries exactly one `call i32 @gy_f()` — so a callee that prints, prints, and `def g():` / `    print("side")`
   / `    return f()` still prints `side` under a folded comparison (**ADR 0172**, **ADR 0288**).
4. **The self-call exclusion stands** (**ADR 0273**): `def f():` / `    return f()` is not a void answer, it is
   an infinite regress wearing the row's word, and a direct-AST row fails if it starts being read as one.

The three doors that read the verdict (`isNoneExpr` for print/`str()`/the REPL, the None-equality fold for
`is`/`==`, and the truth roads) were NOT touched: this row's whole change is in the arm that feeds the plan.
The fold's IR is asserted unchanged — no `@rt_payload_eq`, no `@rt_pair_truth`, no `@rt_icmp_values` for a
forwarded void — because the temptation after **ADR 0427**'s rejected alternative is to route these through a
run-time door, and a run-time door here would read a tag slot the forwarding body only just learned to store.

## Evidence

- `pkg/lang/void_forward_test.go` (NEW): **24** answered programs judged on the compiled leg AND the record leg
  (`compileAndRun` + `HasGoldenAnswer` + `runGoldenStdout`, a MISSING record failing the case, **ADR 0302**) —
  the four spellings of the question, the void written on either side, the print/`str`/`repr`/list/dict/`bool`/
  membership/bound positions, the `0`/`False`/`1` collisions, one frame deeper and two, a forwarding body with a
  parameter and one that also falls off its end, two side-effect survivors, the `lambda`-bound callee and the
  mutual pair the parent REFUSED, and four controls (a forwarded number, text, verdict, container); **10**
  position rows with their own controls; **8** rows asking `answerKindPlanOf` DIRECTLY over a parsed program in
  both directions (forwarder carried+void, callee carried+void, a two-frame chain, a mutual pair, three controls
  that must not be void, the pair-served-callee case, the self-call refusal) plus the invariant that a void-only
  body is never left un-carried; **3** IR rows — `@gy_g` calls `@gy_f` exactly once and stores
  `store i32 3, i32* @gy_g.anst`, `@main` calls `@gy_g` exactly once under a folded comparison and reaches no
  comparison door, and a body forwarding a NUMBER stores no answer word at all; **4** filed-not-fixed pins.
- `integration/void_forward_test.go` (NEW): **34** programs of the family through the CLI with every expectation
  read off a live `python3` in the same case, the record-leg audit beside it, a **6**-row exit-code table
  (including the module-container REFUSAL, exit 1, exit 2 forbidden, no `*lang.`/`%!s(`/`__cans`/`0x` leak), one
  machine-readable refusal row that asserts the door's own sentence and the row it names, and **3** owed rows
  pinned beside the reference's bytes.
- Measurement: a **32-program battery** beside a `git worktree` build of the parent (`93f3404`, **ADR 0427**) and
  the pinned CPython — **15 moved to the reference, 13 held byte-for-byte, none moved away from it**, the 4 that
  still differ being the pinned rows and byte-identical on the parent. A **9618**-source sweep of the record
  corpus through both builds reports **0 changed answers** — the corpus has no pre-existing case that forwards a
  void answer, which is why the coverage is the new records and the sweep is only the safety claim (**ADR 0302**).
  Of the **34** sources this row newly recorded, the parent printed the reference's bytes for 15, answered
  wrongly for 17 and REFUSED 2 outright.
- `tools/recmerge` wrote **34** entries from the reference with the compiled leg cross-checked (**9618 → 9652**)
  and refused none — every shape whose compiled leg disagrees is a pinned row, not a record.

## Consequences

- A void answer now travels through as many frames as the program writes, and the fold that decides
  `is`/`==` is still the only place the question is answered. The IR of a forwarding body says so: the call is
  there, the word is stored, and no door is reached.
- `answerArmOf`'s carried-only rule is no longer the last word for a call arm. Anything that later reads
  `p.void` may assume the invariant these rows pin: a void-only body is a carried body, and a body that
  forwards a void-only callee joins it.
- Three shapes stay owed and are pinned with bytes, so the next cycle fails a test rather than a hope: the
  mixed-arm callee (**Gap R.324**), the forwarded parameter (**Gap R.417**), the method body
  (**Gap R.452** — this row's sibling, one table away).
- **Gap R.228**'s Definition of done ("`print(f())` writes `None` on both legs **and `f() is None` is `True`**")
  is now true of a body that FORWARDS that answer too, not just of the body that answers it.

## Alternatives rejected

- **Widen the fold to look through one call** (teach `isNoneExpr`'s Call arm to recurse into the callee's
  body). Rejected: it is a third reader of a body's returns, in the road **ADR 0383** and **Gap R.383** spent
  two cycles making readers stop inventing. The plan is the one reader; the arm is the one place a forwarding
  body may speak.
- **Set `void` from the pair road for the forwarding body by name** (a special case in `answerKindPlanOf` for a
  body whose arms are all calls). Rejected: it is the same verdict recorded in two places, and the second copy
  would not know which arms it had spoken for — `answerBodyPlan` already declines those bodies, and a plan that
  claims a verdict its own arm-walker refuses is the two-walkers mistake in a new file.
- **Set `carry` without the arms** (mark the forwarding body carried so `carries` answers, and let the doors
  guess). Rejected: `carries` is the gate that refuses to let a call's answer be read by a one-word position
  (**Gap R.146**, **Gap R.324**), and turning it on without the arms would widen five doors to answer a
  question this row is about.
- **Record the four still-wrong shapes on the record leg.** Rejected by rule (**ADR 0302**): `tools/recmerge`
  refuses an entry the compiled leg will not agree to, and the four are the pinned rows above.
