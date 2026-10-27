# ADR 0431 — a method that FORWARDS a call to another METHOD answers the callee's own word, read out of the two tables the method round keeps and never out of the callee's body

## Status

Accepted, 2026-10-28. Closes **roadmap Gap R.454**, the row **ADR 0429** filed when it gave the answer
direction its VOID round for a body in a class, and which **ADR 0430** left standing one shape further in.
Files **Gap R.462** (a call forwarded to a method answering ONE text), **Gap R.463** (a module `def`
forwarding a call through a PARAMETER), **Gap R.464** (a forwarded container answer subscripted or read by
key), **Gap R.465** (a cyclic base chain overflowing the compiler's own stack) and **Gap R.466** (a method
answering ONE float printing the double's low word — found by trying to pin the forwarded spelling and discovering
the un-forwarded one was already wrong, on this build and on its parent). Depends on **ADR 0428**
(the `return other(...)` arm for a module `def`), **ADR 0429**/**ADR 0430** (the method round's two tables and
the one body reading), **ADR 0273** (the self-call exclusion) and **ADR 0299** (a duplicated name is no
name). Supersedes nothing.

## Context

**ADR 0428** taught `answerArmOf` that `return other(...)` is a nameable arm: the arm's kind is the callee's,
so the plan records the callee's name in `answerArm.dyn` and the reading roads go and ask. It read
`v.Fn.(*Name)` and nothing else. **ADR 0430** then gave the plan a METHOD round — `methodCarry`, keyed by the
emitted symbol `gy_C_m` — and **ADR 0429** gave it a method VOID verdict, `methodAnswersOnlyTheVoid`. What
nobody gave the plan was the attribute road. So this program:

```python
class C():
    def m(self):
        return None
    def n(self):
        return self.m()

c = C()
print(c.n() is None, c.n())   # the reference: True None   this backend: False 0   — exit 0
```

was one un-nameable road, and **one un-nameable arm retires a whole body**: `answerBodyArms` returns `ok=false`
and `methodAnswerBodyPlan` declines the body, so `c.n()` was neither carried nor folded and its `int` slot
carried the void's payload — which is the word `0` (**ADR 0187**), which the number printer legally prints. A
program the reference answers `True None` printed `False 0` **at exit 0**, the exit code of success: the class
of bug a suite only catches if it asks.

The shape is not exotic. It is how a class reuses its own methods — a `reset` that calls `__init__`, a `show`
that calls `render`, a `describe` that calls `summary` — and every one of them was, at the print door and at
the `is` door, answered by a payload wearing another kind's bits.

## Decision

### The arm, and where it sits

`answerArmOf`'s `*Call` case asks `answerForwardedMethodArm(fd, v, p)` **before** its existing `*Name` road.
The arm claims an `*Attr` callee whose receiver is the literal name `self`, and answers the CALLEE's kind:

```go
if p.methodVoid[key] {                 // forwarding the void answers the void — the CONSTANT word
    return answerArm{word: answerWordNone}, true
}
if p.methodCarry[key] {                // forwarding a carried answer forwards its run-time word
    arm := answerArm{word: answerWordVaried, dyn: key}
    if k, isContainer := p.methodContainerKind(key); isContainer {
        arm.fwdKind = k
    }
    return arm, true
}
return answerArm{}, false
```

Both answers come out of the **tables**, never out of the callee's body. That is the load-bearing choice, and
it is the difference between this round and **ADR 0428**'s: the name road resolves a module `def` by looking
it up and reading it, because a module's `def`s form a DAG the fixed point walks; two methods that forward to
each other (`def n(self): return self.m()` above `def m(self): return self.n()`) are a reading that never
ends, and a plan that recursed would either hang or guess. A round reads a table. `answerKindPlanOf` already
walks every `ClassDef` and asks each body once, so by the time any arm is asked the callee's verdict — void or
carried — is already in the plan.

The void case takes the **constant** word rather than a forwarded one, for the reason **ADR 0428** wrote for
the module twin: a void-only callee has no word of its own for an outer frame to wait on, and the forwarding
body still EVALUATES the call, so a `print` inside the callee still prints. The answered family pins both:
`the callee's side effect still runs` prints `side` then `None`, and asking twice prints `side` twice.

### The class the body belongs to

An attribute callee is answerable only if the reading knows whose `self` it is, and `answerArmOf` is handed a
`*FuncDef` and a `*Call` — the class is nowhere in that. So the plan records it while it walks:

- `bodyClass map[*FuncDef]string` — the class a method body was written in, keyed by the body itself.
- `classTable map[string]*declaredClass` — each class's own declared methods and its bases, in the order the
  source names them, read off the AST because the answer direction exists before any IR.

`methodAnswerBodyAt` then walks the class's own declarations and every base in order, and returns both the
body and **the class that declared it**, because that class is the other half of the symbol the answer word is
stored under (**Gap R.453**'s key: `gy_B_m`, not `gy_C_m`, for a method `C` inherits from `B`). It is the walk
`resolveMethod` walks at the call site and `methodCensusT.declaredMethodAt` walks for the verdict census
(**Gap R.66**: the compiled backend walks every base, so an inherited method is not an ambiguous one), and it
carries the two guards the other two walks learned: a per-lookup visit set, and a depth bound. A base chain
that names itself is a program the reference rejects at the `class` statement, and a plan that followed it
would have met the same fate `resolveMethod` meets today — see **Gap R.465**.

### One question, one reading

`methodAnswersOnlyTheVoid` (**ADR 0429**) and the new arm ask **the same function**, `bodyAnswersTheVoid`,
which is what `answersOnlyTheVoid` already answered a module `def` by: the async/decorated/generator gate,
then `answerBodyPlan`'s `voidOnly`, then the no-`return`-at-all body that `answerBodyReturns` answers by the
walk that STOPS at a nested `def` (**Gap R.392**'s two roads over one AST). Three doors, one walk
(**ADR 0232**), so a method cannot be the void at the print and a value at the `is`.

Splitting that predicate also exposed a hole in the round itself. `answerKindPlanOf`'s fixed point skipped a
key already in `methodCarry` and asked `methodAnswerBodyPlan` of the rest — so a body that is VOID-only was
never recorded at all, since a void body is never carried (**ADR 0429**'s static fold answers it without a
word). A forwarding arm reading only `methodCarry` would therefore have named a call to a carried callee and
declined the identical call over a void callee, stranding exactly the program **Gap R.452** was paid for. The
plan now keeps `methodVoid` beside `methodCarry` and the fixed point records both verdicts, so the arm asks one
table for each answer and the round stays a table-reading round.

### The container kind travels with the forwarded answer

`answerBodyArms` reports one container kind when every road of a body answers the same kind, and the doors that
WALK an answer — `len`, an index, `in`, a loop — choose their road from that KIND. A forwarding body states no
kind of its own (`return self.m()` spells nothing), so the arm carries the callee's in `answerArm.fwdKind` and
`methodAnswerBodyPlan` adopts it, with the same rule it already applies to two literal arms: two arms that
forward two DIFFERENT containers are not one kind and the door declines the body. Without this, the four
walking doors refused the answer of a method that answers a container while the direct call to the same
container answered — one question, two doors, two answers, which is **ADR 0232**'s failure mode and is
measured in the family as `a forwarded list, measured`, `a forwarded list, walked` and
`a forwarded list, membership`.

### Three declines, each pinned

- **A receiver the body's own spelling cannot name.** `methodCensusT.receiverClass` knows a module variable
  bound once to one class; leaning on it would hand one class's answer word to a call the dispatch may run
  against another — **ADR 0299**'s ambiguity wearing a name. `self` is the one receiver whose class is written
  in the body that wrote the arm. A module `def`'s `return o.m()` is declined (roadmap **Gap R.463**).
- **A call forwarded to ITSELF.** The name arm has the same exclusion (**ADR 0273**): the inner frame writes
  the kind word after the outer caller has read it. A body whose only road is `return self.m()` keeps the roads
  it always had. Note the boundary the family pins: recursion that BOTTOMS OUT (`return self.m(0)` under
  `if x:` / `return 7`) is a body with another arm, and the other arm is what the plan reads.
- **A class the plan never registered, or one declared twice.** The first is a class the module has no symbol
  for; the second is **ADR 0299**'s rule, applied by the walk that fills the table rather than beside it — a
  class name declared twice is deleted from `classTable`, and one method name declared twice in ONE class
  records `nil` and resolves to no body. The dispatch answers neither, so neither may the plan.

## What moved

Measured beside a `git worktree` build of the parent (`68a327d`, **ADR 0430**), across **68** deduplicated
sources from both suites — the answered families AND both owed tables:

- **30 moved to the reference**, **38 held** (36 byte-for-byte, 2 differing only in the temp path `llc` prints
  inside a refusal that fires on both builds), and **nothing moved away**.
- The row's own program moved `False 0` → `True None`, and with it the equality twin, the negated identity,
  the truth door, `str`/`repr`, the binding road, the list-literal element, the f-string field and the
  condition head — one arm, every door, because every one of those roads consults the same plan.
- Inherited forwarding (`class B` with the callee, `class C(B)` with the forwarder) and a SHADOWED base method
  (the derived body is the one forwarded) both answer the reference; two classes sharing one method name stay
  keyed apart, one answering the void and one carried.
- A forwarded container is an object: `print(c.n())` prints `[1, 2]`, `{'a': 1}` and `{1, 2}`,
  `len(c.n())` prints `2`, `for v in c.n():` walks, `1 in c.n()` answers, and `two arms forwarding two
  containers` prints both. The IR row asserts the forwarding body declares a word of its own, loads the
  callee's, stores its own three times (the forwarded one and the void's twice, epilogue and unwind target),
  and never `ret i32 @`.

`tools/recmerge` wrote **39** entries from the reference with the compiled leg cross-checked
(**9747 → 9786**), none refused.

## Alternatives rejected

- **Recuse into the callee's body from the arm.** Rejected for the cycle above: `m` forwards `n` forwards `m`
  is a legal program (the reference runs it until it traps on depth) and a plan that recursed either hung or
  answered from whichever table was filled first. Reading the tables the round already filled is the same
  answer with no recursion, and it is what makes the fixed point the place both verdicts live.
- **Reuse `methodCensusT`'s receiver-class analysis for the receiver.** It answers "which class is this name
  ever bound to", and where that is one class it is right — but it is a census over the whole module, its
  answer for `def f(o): return o.m()` is one class in the module that happens to bind `o`, and the dispatch has
  no idea. Wrong in the direction that prints a payload with another class's word. Filed as **Gap R.463** and
  pinned with the reference's bytes beside this backend's.
- **Give `answerArmOf` a class argument and pass `g.classInfos`.** `answerArmOf` is called from the plan
  (`answerKindPlanOf`), which runs before any `irGen` exists; the generator-side copy would be a second
  reading of the same question, and a second reading is how two doors get two answers. Keying the plan's own
  walk by the body gives the arm the class without a second pass.
- **Let the arm answer the forwarded CONTAINER answers too by re-reading the callee.** The kind already travels
  — through `fwdKind`, which is the same table-reading. The slot-reading doors (an index, a key) still resolve
  through the notebooks a literal fills; a forwarded answer fills none, and inventing a notebook per call
  would be **Gap R.67**'s record asked of a class again. Filed as **Gap R.464**.
- **Silently widen the one-kind exemption.** A callee that answers ONE text keeps its answer out of the round
  (**ADR 0430**'s decline: no word is needed when the spelling says the kind), so forwarding it prints the
  payload — `print(c.greet())` of `return self.hi()` writes `0` where the reference writes `hi`, while the direct
  `print(c.hi())` writes `hi`. Answering it would need the one-kind method to enter the round, which is a wider
  change than an arm, and is the reason the row is filed (**Gap R.462**) and pinned with the reference's bytes
  rather than quietly folded. The FLOAT spelling turned out not to be this shape at all: `print(c.half())` prints
  `1` on the direct call too, which makes it a door the class never had rather than an arm it never grew, and it is
  **Gap R.466** — the measurement, not the wording, is what split the two rows.

## Agentic interface

No new flag, no new schema field: the arm changes which programs compile and what they print, and every
refusal it leaves standing is still a machine-readable document — capability code, phase, suggestion and the
roadmap rows that own it — through the `--json` channel (**ADR 0006**, **ADR 0346**). The reference leg pins
that: `TestTheForwardingMethodRefusalIsMachineReadable` asks for `"ok":false`, `"exit":1`, `"phase":"codegen"`,
`"capability":`, `"code":"lower.unsupported.`, `"suggestion":` and `"rows":` in one document, so a script never
scrapes prose to learn that a forwarded container cannot be subscripted. No refused program reaches
`llc` at **exit 2** (**ADR 0166**); the one compiler-side crash found while measuring this row is a base chain
that overflows the Go stack, and it is **Gap R.465**.

## Tests

- `pkg/lang/method_forward_test.go` NEW — **39** answered programs on the compiled leg AND the record leg (a
  MISSING record fails the case, **ADR 0302**), **20** rows asking `answerForwardedMethodArm` DIRECTLY with
  their own controls (the arm answers out of exactly one table; a declined arm never claims a kind it was
  given; a declined-but-described callee is a hole and fails), **4** IR rows, **7** owed rows pinned with the
  reference's bytes and the gap that owns them.
- `integration/method_forward_test.go` NEW — **39** CLI programs, each re-derived from a live CPython inside
  the case (a reference that stopped printing what the row pins fails the test too), on both CLI engines;
  **1** record-audit row; **1** machine-readable JSON refusal; **7** owed rows pinned with the reference's
  bytes beside this backend's.
- `pkg/lang/void_method_test.go` — `readsASelfCall` moves from `false` to `true` (forwarding a void method IS
  the void now), and the owed pin that held **Gap R.454** is DELETED rather than left.
- `integration/void_method_test.go` — the four R.454 programs move from the owed CLI table into the answered
  family, which is the only place a green answer can be certified.
