# ADR 0255 — a comprehension loop's counter takes its back edge from a latch block, not from a guess

Date: 2026-10-02 · Status: Accepted · Roadmap: L11.1 (the runtime comprehension loop), Gap R.100 (closed), Gap R.105 / R.106 (filed), ADR 0253 (the division's guard, and a guard that reports the block it ends in), ADR 0251 (a slot read that branches on its tag), ADR 0224 (a `phi` names the predecessor the edge comes from), ADR 0138, ADR 0166 (a broken module is our bug), ADR 0187, ADR 0238

## Context

Gap R.99 was filed last cycle as `[xs[0] / 2]` printing `[2]`. Its neighbour, filed beside it, was worse and quieter: the same arithmetic one spelling over —

```
xs = []
xs.append(6)
print([v / 2 for v in xs])   # CPython [3.0] · --interp [3.0] · --aot: exit 2
```

— and the failure was `llc` refusing to assemble the module at all:

```
PHI node entries do not match predecessors!
  %t3 = phi i32 [ 0, %comp.pre1 ], [ %cc2, %comp.body3 ]
label %comp.body3
```

The runtime comprehension — the one that walks a container the program built, where the compiler knows neither the length nor the elements — writes an ordinary counted loop. Its induction `phi` names `comp.body` as the block the counter arrives from. That was true the day it was written, because the body block was the last thing emitted before the increment. It stopped being true the day the body grew a branch of its own: ADR 0253's `/` emits a zero guard (`fcmp oeq` + `br i1` + a raise arm), and a tagged slot read branches on the tag (ADR 0251), so the instructions that add one to the counter and jump back to the header now live in `fdiv.ok6`, two blocks down.

Three things made this worth an ADR rather than a two-line patch.

It is **exit 2 on the ordinary case**: `[v / 2 for v in xs]` is not an exotic program, and ADR 0166 assigns a module that `llc` rejects to us, not to the person who wrote the program.

The **filtered path had the right shape all along**. With a filter the emitter already creates a merge block — both arms of the condition branch to `comp.skip`, so `comp.skip` really is the loop's only back edge, and naming it in the `phi` is honest. The unfiltered path created no merge and guessed. That is why `[v / 0 for v in xs if v > 1]` trapped correctly while `[v / 0 for v in xs]` did not compile: two spellings of one loop, one of them right by luck.

And the **`for` statement never showed it**, because its body is unrolled — no `phi`, no back edge to misname. The bug needed the one loop in this backend that is a real loop.

## Decision

**The increment runs in a latch block of its own, and the induction `phi` names that.** The unfiltered comprehension now does what the filtered one does: every path the element can end on — the fall-through, a guard's `ok` arm, a tag arm's merge — ends in a `br label %comp.merge`, and `comp.merge` is the single block that increments the counter and branches to the header. The `phi` reads `[ 0, %comp.pre ], [ %cc, %comp.merge ]`, and the entry list finally says what the control flow does.

**An emitter that hand-builds a loop asks its body where it finished; it does not assume.** The assumption was not merely that the body ends in the body block — it was that the *emitter* could name that block without asking. ADR 0253 made a guard report the block it ends in (`branchRaise` returning its continuation) for a different reason, and this change is the second consumer of that: the latch is derivable, and a block a body can jump into is not something the enclosing construct may claim to know. The alternative — threading a mutable "current block" through every emission function — is the same correctness bought with a field every emitter must remember to maintain; the latch needs no memory, because a `br` to a fixed label is valid from any block.

**The row that pinned the rejection is gone, and a row that can fail replaced it.** `TestTrueDivisionInsideAComprehensionIsFiledNotFixed` had a row whose expectation was `llvm-as` rejecting the module, printed with the instruction "this row is the pin". It now prints the oracle's answer, so the row is deleted and the shape lives in three new tables: nine element shapes across both engines, four raises that must **raise** with the pair's own `ZeroDivisionError` wording (including one behind a filter, and one caught by a `for` body's own `except`), and an IR row that parses the emitted module's own terminators and fails with the loop's name if a `phi` entry does not come from a block that jumps back to the header. That last row was checked by deleting the latch and watching it fail — a pin that cannot fail is a comment.

## Consequences

- Gap R.100 closes: `[v / 2 for v in xs]` prints `[3.0]`, `[v / 0 for v in xs]` traps with exit 3 and `division by zero`, `[v / 0.0 for v in xs]` with `float division by zero`, `[6 / v for v in xs]` traps when the slot is zero, `{v / 2 for v in xs}` prints `{3.0}`, a `for` body that divides and a `for` that appends the quotient work, and the filter keeps and drops items — all three engines, no exit 2 in any row.
- The `phi`-against-terminators row is reusable machinery for the next hand-built loop in this backend: it turns "the verifier was unhappy" into "the induction phi of `comp.cond2` names `%comp.body3`, whose terminator is a conditional branch — not the unconditional back edge the entry list claims".
- Two shapes came out of the sweep and are **filed rather than absorbed**, both silently-wrong answers with exit 0, so both are in the open queue's priority-1 class: **Gap R.105** — `print({v: v / 2 for v in xs})` prints `{6: 3}` where the interpreter and CPython print `{6: 3.0}`; the dict entry's value is stored without the tag its list-side twin already carries. **Gap R.106** — `xs.append("a")` then `print([v / 2 for v in xs])` prints `[0.0]` where every honest engine raises `TypeError: unsupported operand type(s) for /: 'str' and 'int'`; a loop variable bound from a container the compiler cannot see is read by the static arm, and the `for`-statement spelling is wrong identically, which localises the defect to the loop-variable binding rather than to comprehensions.
- Agentic path: no new flag, no new code, no schema change — this class of failure previously surfaced as exit 2 with `llc`'s prose on stderr, which no script can classify except by string-matching a toolchain. Those programs now exit 0 with the answer, or 3 with a traceback whose class and sentence are the language's; `--json` reports `{"backend": …, "exit": …}` for both, and the exit-code contract in `docs/operations.md` covers them unchanged.

## Alternatives rejected

- **Thread a current-block field through the generator and have every emitter maintain it.** Correct, and it is what a production compiler does; here it is a field that any new emitter can forget, and forgetting it re-creates this bug as an `llc` rejection. The latch block is unconditionally valid, needs no state, and reuses the shape the filter already had — the cheapest thing that is also right.
- **Name the guard's `ok` block in the `phi` by asking the element emitter what it emitted.** This is the "assume you can observe the body" position, and it breaks the moment an element emits two branches (a tag dispatch, a `match`-shaped element, a boolean short-circuit): there is no single block to name, and there *is* always a single latch to write.
- **Emit the append, then the guard.** It would put the loop back the way the `phi` expected by hoisting the zero check out of the element — and it would be wrong: which `ZeroDivisionError` wording the pair earns is a fact about both operand kinds and about the *slot's* contents (ADR 0253), so the guard has to sit where the operand is known. Reordering the IR to satisfy a `phi` is the `phi` guessing the program.
- **Refuse a trapping element in a runtime comprehension.** Honest, cheap, and it takes away `[v / 2 for v in xs]`, `[v / 2 for v in xs if v > 1]` and the raises that now behave — nine shapes that answer, four that trap with CPython's sentence. A refusal here would be the compiler declining a program the oracle runs, for a reason that turned out to be two lines of block layout.
- **Leave the filed-not-fixed row in place, re-pinned to the answer.** The row's own text said it must be deleted when the answer arrives: a table whose rows describe what the compiler *gets wrong* is a queue, and a row that quietly stops being a defect while staying in the table is how a fixed bug becomes invisible to the next reader.
