# 0269. `and` and `or` hand back the operand the test chose

## Status

Accepted. Ships with both operators lowered as an **operand choice** on both backends: the interpreter's
`evalBin` (`pkg/lang/jit.go`) asks which operand its test chose and returns that value unconverted; the
compiled backend answers the same question in one file, `pkg/lang/logic_value.go`, behind three doors —
`logicValue` (the i32 door), `logicDouble` (the double door, the `select i1 …, double …` ADR 0262 wrote for a
ternary's arms) and `logicPrintPair` (the print door, which selects a `(payload, tag)` pair and hands it to the
module's one tag-reading printer `rt_print_mixed_value`) — plus `logicCondition` for the heads of `if`, `elif`,
`while` and a ternary's test, where only a verdict is asked. `constantLogicArm` in `pkg/lang/boolvalue.go` is
the one question "which operand does this test choose", consulted by the renderer (`IsBoolExpr`), both
lowerings, the fold paths (`logicFoldConst`, in both folders in `pkg/lang/codegen.go`) and the interpreter.

Closes roadmap **Gap R.147** ("`and`/`or` answer the verdict instead of the operand, on both engines":
`print(2 and 3)` printed `1` where CPython prints `3`, and `print("" or "d")` printed `1` where the reference
prints `d` — exit 0, digits wrong, no refusal, on an operator every Python program uses). The row was filed by
ADR 0268 while it was writing its probe, not by a failing test.

Files **Gap R.149** (the operand the test did *not* choose is still evaluated, on both engines: `x and boom()`
runs `boom`, `x and (1 // 0)` raises, where the reference does neither) and keeps **Gap R.146** open for the
positions that keep one word for a whole value — `len`'s argument, a binding, a container element, an
arithmetic operand — which the print door's pair road does not reach.

Promotes `programs/and_or_answer_like_python` to standalone parity (34 lines, three engines, the same bytes)
and registers `programs/probe_and_or_shapes_the_word_carry` and `programs/probe_and_or_the_test_skips` as the
two debt rows. The `and` line of `programs/probe_pair_bound_name_takes_a_value` stopped being a pin: `n and 3`
prints CPython's `3` on all three legs.

## Context

The compiled lowering was four instructions, and all four were about a verdict:

```llvm
%t3 = icmp ne i32 %l, 0          ; truth of the left operand
%t4 = icmp ne i32 %r, 0          ; truth of the right operand
%t5 = and i1 %t3, %t4            ; the conjunction
%t6 = zext i1 %t5 to i32         ; and the answer is 1 or 0
```

`print(2 and 3)` printed `1`; `print("" or "d")` printed `1`; `print([1] and [2])` printed `1`. The interpreter
had its own copy of the same mistake (`if e.truthy(l) && e.truthy(r) { return 1 }`), so the two engines agreed,
and nothing in the corpus noticed: the three places the corpus used `and`/`or` at all were `if`/`while` heads
(where a verdict *is* the right answer) and one line, `print(ys[0] and ys[1])` over `[True, 1]`, whose `1` is
CPython's answer for the wrong reason. That line is ADR 0261's "the row that has not moved", and it is the
reason this cycle had to be about *which operand*, not about making every chosen operand print like a verdict.

Three facts decided the shape of the fix.

**A condition and a value ask different questions.** `truth(a and b)` is `truth(a) and truth(b)`, so an `if`
head never needs the operands to agree on anything — it can branch on an `int or text` as happily as on two
numbers. A *value* position needs the answer to live in a word. Mixing the two, as the old lowering did, is
why a condition that works today would have broken if the value door had been the only door.

**The rendering of a chosen operand is a fact about the operand.** ADR 0261 learned this from `min`/`max` and
shipped `constantTestArm` for a ternary: when the source wrote the test, the arm that runs *is* the answer and
the other is not in the program. `and`/`or` have the same test, so `constantLogicArm` is the same function with
the operator's polarity in it. `True or 1` is `True` and `1 or True` is `1`: no predicate over the *operator*
can produce that pair, which is also what makes ADR 0261's untouchable row fall out for free instead of being
specially preserved.

**The module already has a printer that takes a kind.** Every wrong answer in this family is "a value reached a
context that could not ask what it was". The tagged value word (L11.1) is the standing answer, and it is not
here yet. But `rt_print_mixed_value(i32 %v, i32 %t, i32 %quote)` exists, is what a container slot and a tagged
loop variable already print through (ADR 0187, ADR 0232, ADR 0233, ADR 0259), and takes its tag as an ordinary
i32 — which means a `select` can produce one. So the print door could render a *run-time* choice exactly as
honestly as it renders a slot: payload and tag selected together, one printer, no invented formatting.

## Decision

1. **One operator, one question, asked in one place.** `constantLogicArm(n)` (in `boolvalue.go`, beside
   `constantTestArm`) answers "which operand does this test choose, when the source wrote the test". The
   interpreter, both compiled doors, the print door, the renderer and both constant folders all call it. It is
   the only place the polarity of `and` versus `or` is written; a rule asked twice is a rule that drifts, and
   this one has already drifted once (the two engines carried two different copies of the verdict bug).
2. **The interpreter returns the operand.** `evalBin`'s `and`/`or` arm evaluates both operands, then hands back
   the one the test chose, unconverted — no `boolVal`, no truncation, no verdict. A test the source wrote is
   answered by the chosen operand alone, so the operand the reference never evaluates is not evaluated here
   either.
3. **The verdict-ness of the answer is asked of the chosen operand.** `IsBoolExpr`'s `and`/`or` arm consults
   `constantLogicArm` and asks only that operand; where the test is a run-time fact the conservative rule
   stands (both operands must be verdicts), exactly as ADR 0257 wrote it for a ternary. On the interpreted leg
   the chosen verdict additionally *enters through the box a container slot gives it* (`logicChosen`, ADR 0259's
   `allocBool`), because the interpreter's print door asks the AST and the AST cannot know which operand won;
   `x = True` / `print(x or 2)` is `True` on both engines now, and `print(1 or True)` stays `1`.
4. **The compiled backend chooses with three doors, and refuses with the fourth.**
   - *constant test* → the chosen operand is the expression; every downstream door renders and stores it as it
     would render that operand alone (print rewrites the argument, the lowerings call `value`/`floatValue` on
     it). Nothing is emitted for the operand the test rejected.
   - *both operands in one word* → one `select`: `select i1 %c, i32 …` in the i32 door (`logicValue`),
     `select i1 %c, double …` in the double door (`logicDouble`, which is ADR 0262's instruction reused, and
     converts a non-double arm because the caller asked for a double).
   - *the print door* → two `select`s over one test — the payload *and* the tag — into
     `rt_print_mixed_value`, so `print(x or "d")` prints the word and not the index, `print(x or 2.5)` prints
     `2.5`, `print(xs or "empty")` prints `[1, 2]`. Tags are the module's own vocabulary (`value.go`'s
     `ValueTag`), so a pair built here and a pair a container slot carries cannot disagree.
   - *everything else* → `logicWordErr`, naming both operands, the run-time fact and the missing word, at the
     capability exit class. Not the verdict, not the payload, not a `sitofp` guess.
5. **A word is earned.** `logicOperandKind` names the word an operand answers with, and its `int` answer is
   restricted to operands the pass can actually see: a literal, a name none of the kind-tables claims, an
   element read the literal describes, an arithmetic expression of number-word operands, a call that is not a
   class construction or a container builder. An operand it cannot name — an attribute like `math.PI`, an
   instance, a container the run time built, a slot the object describes — is refused, not read as an int.
   (`print(x or math.PI)` printed the truncated `3` from precisely this default in the door's first draft, and
   the shape is refused instead). The numeric family is int *and* bool, per ADR 0259: `(3 < 4) + 1` is a
   number, and a `select` between a verdict and an int needs no tag to be legal IR.
6. **The condition keeps its own door.** `logicCondition` composes two predicates for `if`/`elif`/`while`/a
   ternary's test, and `truthOperandErr` asks it before anything else touches the expression. Two operands that
   share no word still branch, because a condition never asks what the answer *is*. This is the old lowering,
   moved out of `value()` — where it had no business being — and it is why `if x or "d":` did not become a
   refusal on the way.
7. **The folders answer the operand too.** Both constant folders (`value()`'s literal fold and
   `foldConstInt`) used to fold `and`/`or` to 1/0, which is worse than the door's wrong answer because it
   disappears into an index, a repeat count or a constant argument where no later pass can see the operator
   that invented it. Both now call `logicFoldConst`.
8. **Both engines evaluate both operands when the test is a run-time fact — and neither does when the test is
   a value the source wrote.** The constant-test road is fully lazy on both legs: `print(0 and boom())` prints
   `0` and the call is not in the module at all (the compiled leg had been running `boom` **twice**, one call
   for the truth test and one for the value), and `print(0 and (1 // 0))` prints `0` where both engines used to
   raise. What stays owed is the run-time test: `x = 0` / `print(x and boom())` prints `boom` on both legs where
   the reference is silent, and `print(x and (1 // 0))` raises where it prints `0`. Keeping the two legs
   identical is ADR 0262's rule for a ternary's arms, and the half that is owed to CPython is owed in one row
   (Gap R.149), not in two divergences.

## Measurement

Reference leg CPython 3.12.3; compiled leg Ubuntu LLVM 20.1.2 (`llc-20`, `opt-20`). The *before* column was
re-measured for this ADR on a binary built from the pre-cycle commit (`be1ea45`, in a `git worktree`), not
remembered from the cycle — every row printed the same bytes on both engines, which is the whole point of the
row. Exit 2 fails a row by itself (ADR 0166) and no row reached it; the refusals are exit 1.

| Expression | Before (both engines) | After — interpreter | After — compiled | CPython |
|---|---|---|---|---|
| `print(2 and 3)` | `1` | `3` | `3` | `3` |
| `print(0 or 5)` | `1` | `5` | `5` | `5` |
| `print("" or "d")` | `1` | `d` | `d` | `d` |
| `print([1] and [2])` | `1` | `[2]` | `[2]` | `[2]` |
| `print(True or 1)` / `print(1 or True)` | `1` / `1` | `True` / `1` | `True` / `1` | `True` / `1` |
| `x = True` / `print(x or 2)` | `1` | `True` | `True` | `True` |
| `y = 1` / `print(y and True)` | `1` | `True` | `True` | `True` |
| `print((2 and 3) * (0 or 4))` | `1` | `12` | `12` | `12` |
| `xs = [10, 20, 30]` / `print(xs[1 and 2])` | `20` | `30` | `30` | `30` |
| `x = 0` / `print(x or 2.5)` | `1` | `2.5` | `2.5` | `2.5` |
| `x = 0` / `print(x or "d")` | `1` | `d` | `d` | `d` |
| `x = ""` / `print([x or "b"])` | `[1]` | `['b']` | refusal, both operands named | `['b']` |
| `import math` / `print(0 or math.PI)` | `1` | `3.141592653589793` | refusal, both operands named | `3.141592653589793` |
| `xs = []` / `xs.append([7, 8])` / `n = xs[0][0] * 2` / `print(n and 3)` | `1` / exit 1 refusal | `3` | `3` | `3` |
| `x = 0` / `if x or "d":` — a **condition** | `7` | `7` (unchanged) | `7` (unchanged) | `7` |
| `print(0 and boom())` — a **constant** test, `boom` printing and returning `5` | interp `boom`,`0`; compiled `boom`,`boom`,`0` (the call ran **twice**) | `0` | `0` | `0` |
| `print(0 and (1 // 0))` — a constant test over an operand that traps | `ZeroDivisionError`, both engines | `0` | `0` | `0` |
| `x = 0` / `print(x and boom())` — a **run-time** test | `boom`, `0` | `boom`, `0` | `boom`, `0` | `0` (Gap R.149) |
| `x = 0` / `print(x and (1 // 0))` — a run-time test over a trapping operand | `ZeroDivisionError` | `ZeroDivisionError` | `ZeroDivisionError` | `0` (Gap R.149) |

Counts, from one green run of `go test -tags=llvm20 ./...`: `pkg/lang/logic_value_test.go` 8 tests / **75**
sub-rows — parity, the rendering of a chosen operand, the condition door, the two folders, the refusal family,
the IR-shape rows and the constant-test drop; `integration/logic_value_test.go` 4 tests / **29** sub-rows —
three-engine parity, the compiled refusals with their exit class pinned, the parity program's bytes and the
two owed halves. Corrected rather than added, because a row that stops refusing has to move: `TestExecAndOrBool`
(`print(1 and 2)` asked for `1`), `integration/truthiness_test.go`'s `and_value`/`or_value` (the table is named
`MatchesPython`), `TestEvalAndOrFloorDiv` (`1 and 2` evaluated to `1`), and
`TestThePairRoadStillRefusesThePositionsThatTakeAValue`, which lost its `print(n and 3)` row to the parity
tables. Corpus: `programs/and_or_answer_like_python` registered as a `match` row, two new debt rows
(`probe_and_or_shapes_the_word_carry`, `probe_and_or_the_test_skips`), and `probe_pair_bound_name_takes_a_value`
lost its `and` line — 149 programs and 140 rows became 152 and 143, `match` 88 → 89 with one paid debt.

## Consequences

Fixed, measured before and after against `python3`: the 34 lines of
`programs/and_or_answer_like_python.gy` print CPython's answer on both backends, so the program joins the
conformance corpus as a `match` row. The table that had pinned the wrong answers is corrected rather than
extended: `integration/truthiness_test.go`'s `and_value`/`or_value` rows asked for `1` (they are named
`MatchesPython`), `TestExecAndOrBool` asked `print(1 and 2)` to print `1`, `TestEvalAndOrFloorDiv` asserted
`1 and 2` evaluates to `1`, and `TestThePairRoadStillRefusesThePositionsThatTakeAValue` pinned `print(n and 3)`
as a refusal — the last of those moved up into the parity table, because a row that stops refusing has to move,
not disappear.

Two refusals were bought by removing an unearned default. The shipped baseline printed the verdict (`1`) for
every shape in this family; the *first draft* of the value door, which selected whatever `value()` could
lower, then printed `3` for `print(x or math.PI)` — the truncated double — and `[0]` for `[x or "b"]`, an
interned index where the text should be. Both are the exit-0 wrong answer the contract forbids, so the
default was taken away and the two shapes now refuse with both operands named. A refusal in the capability
class is the direction AGENTS and ADR 0166 choose; `docs/language.md` says so where the operators are
documented.

Two shapes are filed rather than fixed, with per-leg pins (ADR 0166's discipline — a probe that cannot match is
recorded, not skipped):

* **Gap R.149** — `probe_and_or_the_test_skips.gy`: both engines evaluate the operand the test rejected. The
  two backends agree with each other line for line, which is what makes it one row.
* **Gap R.146/R.147's owed half** — `probe_and_or_shapes_the_word_carry.gy`: `len(x or "abc")`, a text bound to
  a name, a text as a container element, a float among integers, a container bound to a name. CPython and the
  interpreted leg answer all five; the compiled leg declines on the first, and the tag is what it is waiting
  for.

`--json` is the machine path, and it followed without a schema change: `--json --eval 'True or 1'` reports
`"type": "bool"` and `"result": "True"` while `--json --eval '1 or True'` reports `"type": "int"` and `"1"`,
because the renderer, the checker's type and the JSON report all ask the one predicate. `--verify` got honest
too — `x: bool = 2 and 3` is now a type mismatch (`expected bool, got int`), where the checker had typed every
`and`/`or` as `bool` and was checking an `int` assignment against a type the operator never returns.

## Agentic rationale

An agent writes `name = maybe or "default"`, `if ready and count:`, `print(rows or "no rows")` thousands of
times. The toolchain's old answer — exit 0, `1`, no diagnostic — is the worst failure an agent can be given,
because it is indistinguishable from success: the program is not wrong, the exit code is not wrong, only the
*value* is. After this cycle an agent gets one of three things, all of them machine- legible:

* CPython's bytes, at exit 0, on either leg;
* a refusal in the capability class that quotes both operands and names the missing word
  (`roadmap L11.1 … Gap R.147`), so `--json` and the exit code together say "this shape is the tagged value
  word's, go read the row";
* exit 2 never at all — `TestTheCompiledLegRefusesWhatItCannotState` and both probe rows fail if a module
  reaches for the compiler-is-broken class, which is what `[1] and [2]` did before it learned to select
  (`ret`/`printf` through the elements-array global).

The tripwires are the part that keeps that true in two years. `TestTheModuleSelectsTheOperandAndItsKind` fails
if the print door stops selecting a tag beside the payload, if the value door loses its `select`, or if a
*constant* test starts paying for the tag printer; `TestAConditionAsksOnlyWhetherTheAnswerIsTrue` fails if the
value door's refusal leaks into an `if` head; `TestTheFoldAnswersTheOperandAndNotTheVerdict` fails if a folder
goes back to the verdict; and `TestTheConstantTestDropsTheOperandTheReferenceDrops` fails if one engine starts
skipping the operand the other still runs, which is how a two-backend language grows its first output
divergence.

## Alternatives rejected

* **Return the verdict, and fix the four examples.** The rows in the roadmap row are the four CPython examples,
  and four `if n.Op == "and"` special cases would have printed them. The rule is not four examples: the operand
  keeps its own representation, in a container, in a binding, in an f-string, as a verdict, as a float. A
  special case per example is how the corpus ended up with `print(ys[0] and ys[1])` as its only `and` in a
  value position and no coverage of the rest.
* **Promote every chosen operand to a verdict.** ADR 0261 rejected this and the reason has not changed: `1 or
  True` is the number `1`. Any rule that reads the operator instead of the chosen operand breaks that row.
* **Make the value door `select` whatever `value()` can lower.** Cheaper, and it is how the door's first draft
  came to print `3` for `print(x or math.PI)` and `[0]` for `[x or "b"]`: an i32 word is not one word for every
  kind, and an unearned default turns a refusal into a wrong answer at exit 0. The refused shapes are named,
  in the class the contract reserves for them, with the row that owns them.
* **Short-circuit in the interpreter alone** (which is what a first draft did — the code said "the operand the
  reference never evaluates is not evaluated here either"). One engine dropping the effects of the operand the
  other keeps is a *split*, and AGENTS' two-backends rule is what this cycle has to obey, not CPython's
  shortcut. Both engines now evaluate both operands and both owe the same row.
* **Short-circuit in codegen with blocks and a `phi` today.** Correct, and bigger than this row: the merge
  needs every predecessor to have stored, the exception paths need the raise roads, and the print/value doors
  are mid-architecture. It is Gap R.149 with a plan attached, not a detail of this commit.
* **Give the print door a bool/None/text guess instead of a tag.** That is ADR 0185's mistake one operator
  later: a value reaching a context that guesses at its kind. The tag is one extra `select`, and the printer
  already exists.
* **Leave the fold paths answering the verdict.** A fold's wrong answer is invisible to every later pass; it
  becomes the program's index.
