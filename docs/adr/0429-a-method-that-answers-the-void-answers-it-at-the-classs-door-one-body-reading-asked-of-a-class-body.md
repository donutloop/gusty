# A METHOD that answers the void answers it at the class's door — one body reading, `answerBodyPlan`, asked of a body that lives in a class

## Status

Accepted — 2026-10-28. Closes **roadmap Gap R.452** (filed 2026-10-28 by **ADR 0427**, from the probe that
closed **Gap R.388**; pinned with bytes on both witness legs by **ADR 0428**).

Filed from this cycle's battery, each with today's bytes beside the reference's: **Gap R.453** (the answer
direction has no method round at all, so a method whose two arms answer two kinds carries no kind word — and
the arm that answers a container reaches `llc` at **exit 2**), **Gap R.454** (a method forwarding a call to
another METHOD of its class is an arm `answerArmOf` cannot name), **Gap R.455** (a generator METHOD is refused
as `yield outside a generator function`), **Gap R.456** (a method appending to its own FIELD container is
refused by the string-method door), **Gap R.457** (a nested `def` inside a method re-emits the GC frame
prologue and `llc` rejects the module).

```gusty
class C():
    def m(self):
        return None

c = C()
print(c.m() is None)   #  False before, True  after   — the reference prints True
print(c.m())           #  0     before, None  after   — the reference prints None
print(c.m() == 0)      #  True  before, False after   — the reference prints False
```

**ADR 0427** connected the comparison fold to **ADR 0368**'s answer plan, and **ADR 0428** carried that verdict
through a forwarding body. Both reached exactly as far as a body the MODULE holds by name. A method's body is in
no such table — it lives in the class's — so the same question about it was answered by nobody, and both doors
that read the answer fell to the payload: the print door wrote the digit `0` (**ADR 0187**: the void's payload is
a word an int slot legally carries) and the fold compared the same word and called it a number.

## Context

`isNoneExpr` is the one predicate that owns "is this expression the void" — `print`, `str()`/`repr()`, the REPL's
echo, the binding road, the container-element door and (since **ADR 0427**) the comparison fold all ask it. Its
`*Call` arm asks three things, and every one of them is keyed by a MODULE-level `def` name:

```go
if nm, ok := v.Fn.(*Name); ok {                                    //  ← a method call has an *Attr callee
        if g.answer.carries(nm.Value) && !g.answer.answersOnlyTheVoid(nm.Value) { return false }
        if g.answer.answersOnlyTheVoid(nm.Value)                   { return true }
        if fd, ok2 := g.fds[nm.Value]; ok2 && !fdReturnsValue(fd)   { return true }
        ...
}
```

A method call is not even the same SHAPE: `c.m()` is a `*Call` whose `Fn` is an `*Attr`, so it fell past the
`*Name` block entirely, past the `d.get(...)` fold (whose arm requires a `DictLit` receiver), and returned
`false`. The class's own tables — `methodCensusT`, `strMethodFuncs`/`methodReturnsStr` — were built for exactly
this question and were never asked it. That split is **ADR 0366**'s recorded shape for the text direction
("one question, two tables"), and this row is the void direction meeting the same split one door later.

The measurement that localises it is one line: `print(f())` had always written `None` for a module `def` whose
body answers nothing, and `print(c.m())` wrote `0` for the same body in a class — and in the same program,
`print(c.m(), c.m() == None, c.m() is None)` wrote `0 False False`, which is one value with no fewer than three
doors reading the same word three ways (**ADR 0232**).

## Decision

**One body reading, asked of a class's body too.** `pkg/lang/answer_kind.go` grows the method round the file's own
header named as its missing sibling:

1. `methodAnswersOnlyTheVoid(fd)` asks the SAME `answerBodyPlan` the module road asks — the walk
   (`answerBodyReturns`) that stops at a nested `def` and the arm reader (`answerArmOf`) that refuses a body with
   ANY arm it cannot name — and additionally claims the no-`return`-at-all body (`def m(self): pass`,
   `def reset(self): self.x = 0`), which the module road answers with `fdReturnsValue` and which this reading
   answers better, because the reflection walk reads a nested `def`'s `return 7` as the method's own
   (**Gap R.392**'s two roads over one AST).
2. `methodBodyOf(recv, name)` resolves the callee to the FuncDef the DISPATCH runs — keyed by the
   **receiver's class and the name**, not by the name alone. The verdict census drops an ambiguous name, which is
   right when one class's body would otherwise stand in for another's (**ADR 0299**) and wrong here: the
   receiver's class is already known, so `class A:` and `class B:` each with its own `__init__` are two bodies the
   compiler can each read, and a program with two constructors is not an ambiguous program. The base walk mirrors
   `resolveMethod` — own declaration first, then bases in order — because the compiled backend already walks every
   base (**Gap R.66**), so declining an inherited method would be this door's conservatism, not the dispatch's.
3. `isNoneExpr`'s `*Call` arm asks (1) through (2), beside the mutation arm, the `*Name` arms and the `d.get(...)`
   fold it already had. Nothing else changes: the print road, the binding road, the container-element door and the
   fold all consult the one predicate, which is why one arm is enough.

What the verdict will NOT claim, in the same direction as every other census in the package: an `async` body, a
decorated body, a generator body, a body with any arm the plan cannot name (`return self.x`, `return self.other()`
— **Gap R.454**), a receiver whose class the compiler cannot place, and a duplicated class name (two classes, one
name is no class). A verdict that guesses prints `None` for a value.

## Evidence

* `pkg/lang/void_method_test.go` NEW — **33** answered programs on the compiled leg AND the record leg, **13**
  position rows (`if`/`while`/ternary/`and`/`or`/`not`/loop element) with their own controls, **20** direct
  predicate rows asking `methodCallAnswersOnlyTheVoid` and `isNoneExpr` over a parsed program in both directions
  plus three sub-runs (a `self` receiver inside the class being lowered, an unknown class/unknown method/a
  non-attribute call, a duplicated class name), **4** IR rows, **7** filed-not-fixed pins.
* `integration/void_method_test.go` NEW — **47** CLI programs read off a live `python3` in the case, a **13**-row
  exit-code table, **1** machine-readable refusal, **2** owed pins, **2** pins that pin the module `llc` refuses
  as the compiler bug it is (**ADR 0166**).
* **54** new records, `9652 → 9706`, written from the reference by `tools/recmerge` with the compiled leg
  cross-checked; none refused. Asked of the parent build, those 54 printed the reference's bytes for **15** (the
  controls, and the truth positions took by accident), answered wrongly for **38**, and REFUSED **1** outright
  (`print(str(c.m()), repr(c.m()))` died on the str/repr door's sentence and now prints `None None`).
* A **50-program battery** beside a `git worktree` build of the parent (`66c20b2`, **ADR 0428**) and the pinned
  CPython: **28 moved to the reference, 22 held byte-for-byte, none moved away**.
* A **9652**-source sweep of the record corpus through both builds: **0 changed answers**.
* Four pins MOVED rather than vanished: both `Gap R.452` rows in `pkg/lang/void_answer_test.go` and
  `pkg/lang/void_forward_test.go`'s owed tables, and their twins in `integration/void_answer_test.go` and
  `integration/void_forward_test.go`, are DELETED and stand in the answered families of the two new files.

## Consequences

* The answer direction now answers for two kinds of body, and the file's header says which claim it still
  withholds: a method's body gets the VOID verdict, not a carried kind word. That is the line
  **Gap R.453** is filed on, and its worst face is the **exit 2** of a method whose body answers a container —
  the shape `answerReturn` builds through `containerHandleOf` and `emitClassMethod` has no equivalent of.
* `methodBodyOf` is the third door to key a method question by the receiver's class (beside
  `methodReturnsStr`'s table and the verdict census), which makes **Gap R.66**'s base walk a shared shape rather
  than a per-door accident. A future MRO change has three call sites to keep in step, and
  `TestTheVoidAnsweringMethodIsTheBodyAndNotTheName` is the guard that says when they part.
* The print door's void arm now runs for methods, so `print(c.m())` evaluates the call for its effects and then
  writes `None` — asserted from `@main`, which calls `@gy_C_m` exactly once and carries the `rt_print_none` call
  beside it. The fold's half is asserted the same way: `@rt_payload_eq`, `@rt_pair_truth` and `@rt_icmp_values`
  are ABSENT for a question the source can name, and PRESENT-never is the claim a fold makes about its own call.
* A method answering a NUMBER stores no answer kind — asserted from `@gy_C_m`, not from the print, so a future
  widening that prints the right bytes for the wrong reason still fails.

## Alternatives rejected

* **A name-keyed method table, mirroring `textMethods`.** Written first; it declined `two classes, each with its
  own constructor body`, declined the inherited method, and needed a new ambiguity rule to stay honest about the
  program it cannot answer. Keying by the class needed no such rule, because the receiver already names the class.
* **Routing the method answer through the pair door** (`answerCallPair`/`pairCallPair`), which is what the
  module-level family was first widened with and reverted (**ADR 0427**). It answers `False` again — from a tag
  slot no road in that program ever writes — and it leaves two sources of truth for one value.
* **Carrying method bodies in the answer plan**, arms and all. `answerReturn` stores its constant kind word with
  `storePairTag` on the return road the module's `def`s take; a method's body is emitted by `emitClassMethod`,
  which does not share it. Doing this properly is **Gap R.453**'s whole row, not this one's half-line, and doing
  it halfway would have made a mixed-arm method print `None` for the arm that answered `7`.
* **Widening `fdReturnsValue`'s reflection walk to class bodies.** It is the oldest half of the module answer and
  the least able to help: it asks "does a `return` with an expression appear anywhere below here", which reads a
  nested `def`'s `return 7` as the method's own and misses `return None` as not-a-value — two errors in one
  predicate (**Gap R.392**, a row that exists because the toolchain already had two such walks).
