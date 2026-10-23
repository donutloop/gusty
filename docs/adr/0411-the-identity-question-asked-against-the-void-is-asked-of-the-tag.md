# The identity question asked against the void is asked of the tag

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.412** (the popped slot compared with `None` answers `False`).
Files four rows it measured on the way and did not buy: **Gap R.415** (the same list under a second name),
**Gap R.416** (a loop item over a literal list), **Gap R.417** (a callee's own parameter), **Gap R.418**
(identity against a container literal, which dies in the assembler at **exit 2**). The call's half of the same
door stays **Gap R.388**'s. One commit, one feature: `pkg/lang/codegen_arith.go`, `pkg/lang/codegen_binop.go`,
`pkg/lang/codegen_truth.go`, `pkg/lang/the_void_question_test.go`, `integration/the_void_question_test.go`,
plus the two pins this row deletes from `pkg/lang/pop_tag_test.go` and `integration/pop_tag_test.go`.

## Context

**ADR 0410** landed a week-old promise kept: a list slot is a payload and the tag that says what the payload
means, and the door that removes the slot moves both. It also left, in its own honest table, a program that
answered the identity question differently from every other reader of the same value:

```
xs = [None, 1]
print(xs.pop(0))            #  None     — the print door asks the pair
print(xs.pop(0) == None)    #  True     — the equality door asks the pair
print(xs.pop(0) is None)    #  False    #  the reference: True   — at exit 0
```

One value, one question, three readers, two answers. The printer and `==` had been given pair arms —
`answerCallPair` and `comparePair`, both widened by the previous cycle — and both reach
`@rt_payload_eq`, the one answer to *do these two payloads denote the same value*. `is` was never let through
that door: `mixedTaggedCompare` began with `if n.Op != "==" && n.Op != "!=" { return not-handled }`, and the
question fell past it to the **None-equality fold**, whose own comment states its method — *"decides the result
statically, from what the source says each side is"*.

That fold is right exactly as often as the source states the kinds. `print(1 is None)` is `False` and must cost
no call; `x = None` / `print(x is None)` is `True` and is a fact about the source text. It is wrong for an
operand whose kind lives in a container the program built at run time, and wrong for the specific reason this
house keeps having to relearn: **the payload a void leaves behind is `0`**, and `0` is also the word an int's
slot carries and the intern table's first entry and the arena's first slot. Testing the payload against zero is
not testing for the void; it is testing which number happens to be in the box (**ADR 0232**'s collision, wearing
the identity road's clothes).

The same defect, one door over, was already on the tracker: **Gap R.388** — `def f(): return None` above
`print(f() is None)` answers `False` too. A call's answer and a slot's answer are both a payload whose kind lives
somewhere else; the fold cannot see into either.

And there was a second half nobody had asked about, because a condition is lowered by a different road than a
value is. `print(x is None)` and `if x is None:` are the same question spelled in two positions, and
**ADR 0232** had long ago learned to hang the *tagged equality* in both places precisely so they could not drift.
The identity road had been widened in one place only, and a suite that pins only the value spelling cannot tell
which of the two the door answered.

## Decision

**One: `is` and `is not` join `==` and `!=` at the pair door — for one shape.** `mixedTaggedCompare` admits the
identity operators when one side is statically the void (`isNoneExpr`). The void is a **singleton**, so for it
identity and equality coincide, and `@rt_payload_eq(payload, tag, 0, TagNone)` is exactly the question *does the
object's own tag say the void* — the same helper, the same door, the same `(payload, tag)` pair the printer and
`==` already use. Nothing about the answer is invented here: the void's pair is `(0, TagNone)` and the tag came
out of `@heap_tags` with the payload.

**Two: only the void.** This is the decision the row could have gotten wrong. `xs.pop(0) is [1]` is a container
IDENTITY question, and `@rt_payload_eq` answers containers **by content** (**ADR 0189**, because two list
literals holding the same elements are equal). Identity is not content: the reference answers `False` for two
containers that hold the same elements, and `True` only when they are the same object. Routing the container
case through the content door would have replaced an honest crash with a wrong answer at the exit code of
success — the trade this repository refuses (**ADR 0166**'s direction, **ADR 0232**'s one-question rule). So the
door asks the void and declines the rest, and the shape that remains is filed as **Gap R.418** with the assembler's
own error line beside it: `print([1] is [1])` and `print(xs.pop(0) is [1])` both die on
`icmp eq i32 …, @.lst1` — a module-level global standing in a word position — at **exit 2**, the
**Gap R.393**/**Gap R.253** family's asking-door half.

**Three: both roads, or the door answers only half the language.** The call is hung in the value road
(`codegen_binop.go`, beside the tagged equality **ADR 0232** already hangs) and in the condition road
(`codegen_truth.go`, where `if`/`while`/`and`/`or` lower their test). The condition half is not decoration: the
measured family contains `if xs.pop(0) is None:`, which was already answering `True` before this row for a
different reason, and `while xs.pop(0) is None:`, which was not. One door, both hooks, one IR claim each.

**Four: the fold still folds, and that is asserted, not hoped.** The IR guard runs in both directions: the
popped slot's `@main` contains exactly one

```
%t14 = call i32 @rt_payload_eq(i32 %t11, i32 %t12, i32 0, i32 3)
```

— the tag a **register** the removal produced (`%t12`, loaded through `rt_pop`'s out-parameter), the void's pair
a constant `(0, TagNone)` — and the four controls the source can settle (`print(1 is None)`,
`x = None` / `print(x is None)`, `print(None is None)`, `print("a" is None)`, plus an untagged single-kind slot
read) contain **no pair call at all**. A suite that checked only the first half would pass contentedly after
someone routed the easy case through the runtime (**ADR 0385**'s rule, restated for this door).

**Five: what the row declined to answer, it filed with the bytes it saw.** Four shapes measured beside the
family, each identical on the parent build, each pinned in `theVoidOwed()` with this backend's answer beside the
reference's: the same list under a second name (**Gap R.415**, where `print(ys.pop(0))` prints `0` for `None`
because the alias carries no kind claim to the door), a loop item over a literal list (**Gap R.416**, whose `==`
half was already wrong, which is why they are one row), a callee's own parameter (**Gap R.417**, three shapes —
and note that `pairCallScan` OPENS the pair for a popped argument, so the tag crosses the frame and the body
throws it away), and the container identity that reaches the assembler (**Gap R.418**). Plus the standing
**Gap R.388**, which this door will swallow the moment a callee's answer is read as a pair.

## Machine path

Nothing new is discoverable that was not discoverable before, which is the point — the row changes an *answer*,
not the interface. What an agent can still ask, and what this row keeps honest:

* `gustyc --eval 'xs = [None, 1]
print(xs.pop(0) is None)'` prints `True` at exit 0, and the same source is on the record leg with those bytes.
* `--emit-llvm` for that program shows one `call i32 @rt_payload_eq` in `@main` taking the popped payload and the
  popped **tag**; `--emit-llvm` for `print(1 is None)` shows none. `TestTheVoidQuestionIsTheTagAndNotWhatTheSourceWrote`
  asserts both, so a future fold-into-runtime or fold-over-into-a-slot is a failing test rather than a silent drift.
* the refusals the row leaves standing keep their capability codes — `lower.unsupported.tuple-is-immutable` for a
  tuple's `pop`, `lower.unsupported.container-method-over-a-name` for a dict's — with `ok:false`, `exit:1`,
  `phase:"codegen"`, a `suggestion` and `rows` naming the roadmap row (**ADR 0006**, **ADR 0346**).
* the traps in the family (a list popped past its end, a bad index) exit **3** with CPython's class and sentence,
  and the suite fails them at **1** (a refusal) or **2** (a broken module, **ADR 0166**); `except IndexError:`
  catches them, which is the difference between a trap the runtime raises and a diagnostic the compiler prints.

## IR implications

* No signature changed, no helper added, no new global. `@rt_payload_eq` already existed for `==`; the change is
  which operators are allowed to reach it and from which roads.
* The identity result is an `i32` 0/1 through `asBoolI32`, exactly as the tagged equality answers it, so a
  condition built from the same call site reuses the verdict rather than re-asking the door — the door emits
  **one** `rt_payload_eq` per question, which the IR test counts.
* Because the read inside `comparePair` can emit a bounds check whose `IndexError` branch terminates a block, the
  door keeps building its whole answer in a scratch buffer and commits only once both sides answer (**ADR 0166**:
  a door that changes its mind halfway leaves instructions after a terminator, and `llc` reports the compiler's
  mistake as the program's). Admitting two new operators to that door inherits the discipline; it does not get to
  skip it.
* A void on one side means no container is ever materialised for this question, which is what keeps the row off
  the **Gap R.418** path: `containerOperand` is never reached from the identity road, and the file says so rather
  than leaving it to be rediscovered.

## Alternatives rejected

* **Widen the pair door to every `is`, containers included.** Cheapest possible diff, and it answers `[1] is [1]`
  `True` where the reference answers `False`, because `@rt_payload_eq` compares containers by content (**ADR 0189**).
  Rejected: a wrong answer at exit 0 is worse than the exit-2 it replaces, because the exit-2 one is impossible to
  miss. Filed as **Gap R.418** with the assembler's line quoted.
* **Fix the fold instead of the door** — teach `isNoneExpr` that a popped slot can hold the void. That predicate
  is a *compile-time* question by contract, and the fact here is a run-time one; a table of "which shapes could be
  the void" is exactly how `isNoneExpr` grew the arm-for-arm accretion it has. The tag already exists; ask it.
* **Answer the void by comparing the payload against `0`.** The word a void leaves behind is also the word an int
  slot carries; that is the collision **ADR 0172** was written to stop, and the fold's comment says so in those
  words. The tag is the discriminator; the payload is not.
* **Give the condition road its own identity door.** **ADR 0232**'s whole point is that a fact asked twice answers
  yes once; two doors over one `BinOp` is how `print` and `if` came to disagree about `xs.pop(0)` in the first place.
* **Leave the literal-loop and parameter shapes answered-by-fold and unfiled.** They are pre-existing, they are
  invisible to any suite that only tests the printer, and two of them (the `==` half of R.416, the `print(v)` half
  of R.417) were already wrong before this row touched anything. Filing them with this backend's bytes beside the
  reference's, and with **ADR 0388**/**ADR 0393** named as the precedent for the door that was built and not joined,
  is what makes the next cycle's first commit a lookup rather than an excavation.

## Measurements (taken, not remembered)

Over the **34** programs the two new files carry, a build of the parent commit (`af8b847`) beside a build of this
one, each asked of the pinned CPython:

* **17 moved to the reference's answer** — the popped slot's identity and its negation, the same slot asked twice,
  the text/container/dict/number kinds, the default and negative indices, the drain, `str()`, the `and`
  short-circuit, a dict value, a method's return, a `while` head.
* **4 of those 17 were REFUSALS on the parent build** — a slot read of a mixed list, a tuple's slot, a loop
  element over a named list, a name the pair road bound. Each of those positions wanted one word and the read
  carries two; asking the tag is what turned a wall into an answer, and it is the difference the row would most
  easily have gone uncredited for.
* **11 controls held** — the written void, a void bound to a name, identity between numbers, a single-kind list's
  pop, the append neighbour, the literal-fold family, and the equality twins that were already answering.
* **6 still differ**, and every one is a row: **Gap R.388** (a call's void answer, standing), **Gap R.416** (the
  literal loop item, both spellings), **Gap R.417** (the callee's own parameter, three shapes), **Gap R.415** (the
  alias), **Gap R.418** (the container identity, at **exit 2**). All of them byte-identical on the parent build —
  which is the sentence that separates a filed debt from a regression, and the reason both columns are measured.

`tools/recmerge` wrote **34** entries from the reference for this family (the record to **9025**) and refused
none of them: the compiled leg agreed with the reference on every source this row claims to have paid (**ADR 0309**'s
rule that the compiled leg never writes an answer, checked by the recorder rather than by this paragraph). The two
pins **Gap R.412** left in `TestWhatThePopRowLeavesStandingIsFiledNotFixed` and
`TestWhatThePopRowAnswersWrongAtTheCLI` are **deleted, not edited**, which is the only way a paid debt is
distinguishable from a rewritten one.

`make testparallel` green; every emitted module verifier-clean; exit 2 forbidden in both new files and present in
exactly one pinned owed shape, which is **Gap R.418** and says so.
