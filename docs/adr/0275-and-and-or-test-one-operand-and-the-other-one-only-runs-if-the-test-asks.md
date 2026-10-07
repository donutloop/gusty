# 0275. `and` and `or` test one operand, and the other one only runs if the test asks

## Status

Accepted. Ships as four rewritten doors in `pkg/lang/logic_value.go` (`logicCondition`, `logicValue`,
`logicDouble`, `logicPrintPair` — all four now emitting one shared skeleton, `logicSkeleton`), one hoisted
dispatch in `pkg/lang/codegen.go`'s `value`, three lines in `pkg/lang/jit.go`'s `evalBin`, and one runtime
helper, `@rt_pair_truth`, in the tagged-arithmetic block of the module. The `select`-based pair selector
(`logicSelectPair`) is deleted: with the branch there is nothing left for it to select.

Closes roadmap **Gap R.149**, the row ADR 0269 filed for exactly this: both engines evaluated the operand
the test did not choose, so

```gusty
def boom():
    print("boom")
x = 0
y = 1
print(x and boom())        # CPython: 0, silent            · both engines: boom, then 0
print(y or boom())         # CPython: 1, silent            · both engines: boom, then 1
print(x and (1 // 0))      # CPython: 0                    · both engines: ZeroDivisionError, exit 3
if x and boom():           # CPython: nothing              · both engines: boom
    print("then")
print(boom() and 2)        # CPython: boom, 2 (one call)   · --aot: boom, boom, 2 (two calls)
```

The program that pinned the row, `integration/programs/probe_and_or_the_test_skips.gy`, grows from six
statements to nineteen and moves from the debt ledger to `conformanceStandalone()`: it is parity surface
now, `match` on all three legs.

## Context

ADR 0269 made `and`/`or` answer an *operand*. It did that with `select`:

```llvm
  %t = select i1 %c, i32 %r, i32 %l      ; `a and b`, i32 door
```

which is a correct answer to *which value* and an impossible one to *which value, computed when?*. An
instruction cannot decline to run: `%r` has to exist before the `select`, so the right operand's call, its
print and its division all happen on every path. The interpreter did the same thing in a different way — it
evaluated both operands and then asked which one it wanted — so the two backends agreed line for line,
which is what made the divergence one row rather than two.

Three things the row's own text had already predicted turned out to matter more than the row said:

**The compiled leg evaluated the *tested* operand twice.** `print(boom() and 2)` printed `boom` twice. Two
independent causes, found one after the other, and the second one is why this ADR is not just four function
bodies:

- each road asked for the left operand's truth with `truthyValue(b, n.L)` and for its value with
  `value(b, n.L)` — two full lowerings of the same expression;
- `value()`'s `case *BinOp:` reached the `and`/`or` dispatch only **after** the generic operand lowering had
  already emitted `g.value(b, n.L)` and `g.value(b, n.R)` for the comparison and arithmetic doors above it.

The second is the shape of bug this file keeps meeting: a door placed after the code that assumes the door
was already asked. `case *BinOp:` already knows this rule — the membership and tagged-equality doors sit at
its top with comments saying "checked before the operands are lowered" — and `and`/`or` were not in that
list. The dispatch is now the first thing in the case.

**A skipped operand is not only a wasted call.** `print(x and (1 // 0))` died with `ZeroDivisionError` where
CPython prints `0`, and the same trap in a *condition* — `if x and 1 // 0 == 0:` — killed a program whose
test had already failed. The guard idiom every Python program writes (`if xs and xs[0] > 0:`, `if d and d["k"]:`)
is the ordinary use case of the operator, and this backend turned it into a crash.

**The condition door had the same defect with none of the pair machinery.** `logicCondition` composed
`truthyValue(L)` and `truthyValue(R)` with an `and i1`, so `if x and boom():` called `boom` with `x` false.
That road needs no payload and no tag at all, which is why it is the cheapest of the four to convert and the
one whose fix is a `phi i1`.

## Decision

**One skeleton, four doors, and the truth read from the register the evaluation already produced.**

```
<the block the expression starts in>   …the left operand, once…   %c = <its truth>
                                       br i1 %c, label %logic.rhs, label %logic.lhs
logic.rhs                          …the right operand, emitted only here…
                                       br label %logic.rhsfwd
logic.lhs                                br label %logic.merge
logic.rhsfwd                             br label %logic.merge
logic.merge                        %ans = phi <ty> [ %l, %logic.lhs ], [ %r, %logic.rhsfwd ]
```

Four rules, each of which is a thing that could have been got wrong:

1. **The left operand is evaluated once and its truth is read off the word that evaluation produced.**
   `logicValue` tests its i32 register with `icmp ne …, 0`; `logicDouble` tests its double with `fcmp one
   …, 0.0` (the same predicate the float condition door already used); `logicCondition` needs only the i1.
   Nothing asks `truthyValue` for an operand that has already been lowered. This is what kills the
   double-`boom`, and it is a cheaper module than the one it replaces.
2. **An operand whose kind only the object carries asks the object for its truth.** `logicPrintPair`'s left
   arm is a `(payload, tag)` pair, and whether that pair is true — a non-zero number in either family, a
   non-empty text, a non-empty container, not-None, any object — is the run time's fact. `@rt_pair_truth`
   answers it from the same `ValueTag` vocabulary `rt_print_mixed_value` and `rt_payload_eq` read, and it
   reuses `rt_lift_num` for the numeric arms rather than inventing a second numeric reading (ADR 0265's one
   door, one rule). A float slot is therefore unboxed before the test instead of being read as a handle,
   which is the difference between `ys = [0.0, 1]` / `print(ys[0] or "d")` answering `d` and answering
   whatever the box index happened to be.
3. **The merge is a `phi` whose predecessors are blocks this generator named.** No block-tracking pass
   exists in `irGen`, and writing `br i1 …, label %rhs, label %lhs` from an anonymous middle block leaves the
   merge with two predecessors nobody can name. Both one-instruction forwarding blocks therefore exist for
   the phi's benefit: a phi's incoming *block* must be a real predecessor, while its incoming *value* only
   has to dominate that block, which the left operand's register does (it sits two instructions earlier on
   that very path). `simplifycfg` deletes both blocks; they cost the program nothing and the generator no
   new state.
4. **Every road builds into a scratch builder and commits only when both arms answered.** `logicDouble`
   and `logicPrintPair` answer "I cannot serve this" (`""` / `handled=false`) and their callers fall through
   to another door. Emitting the branch first and declining afterwards would leave instructions after a
   terminator in the caller's block — `llc`'s error, exit 2, ADR 0166's compiler-is-broken class spent on the
   program's mistake. This is `mixedTaggedCompare`'s existing buffer rule, applied to a door that emits
   control flow for the first time.

The interpreter takes the three-line version of the same decision (`jit.go`, `evalBin`): evaluate the left
operand, ask `truthy`, evaluate the right one only on the path that needs it, and hand the chosen expression
to `logicChosen` exactly as before — so ADR 0261's pair (`print(True or 1)` is `True`, `print(1 or True)` is
`1`) and ADR 0269's rendering rule are untouched.

## Alternatives rejected

- **Keep the `select` and merely skip the right operand when its truth is statically known.** Fixes nothing
  that a program can observe: the interesting operand is the one whose truth is not known, which is the case
  the row is about.
- **`select` for the value plus a branch only for side effects.** An operand cannot be both evaluated and not
  evaluated. A `call` to a function that prints is not idempotent and cannot be hoisted out of a path.
- **A `phi` naming the block the expression started in.** `irGen` emits one linear text and knows no block
  names; the alternative is to add block tracking to every emitting path in a 16k-line generator, for the
  benefit of two blocks `simplifycfg` removes anyway. Dominance is what makes the forwarding blocks legal,
  and the verifier agrees (every module in the corpus still passes `opt -passes=verify`).
- **A new tag-only truth path in the print door, skipping `@rt_pair_truth` for constant tags.** Two doors
  answering one question — the empty-text case and the empty-container case would each need their own
  reading of the object, and the failure mode of "the compile-time arm and the runtime arm disagree" is
  exactly the class ADR 0236 named (two renderers, one rule). One call, one table, whatever the tag is.
- **Refuse the shapes whose truth the module cannot state.** They are not unanswerable — the object knows —
  and refusing a program the reference runs is the honesty this project already priced (ADR 0166): a
  capability refusal is only honest when there is nothing to compute.
- **Only fix the interpreter, or only the codegen.** AGENTS' two-paths rule, and here it is not procedure
  for its own sake: an interpreter that skips while the compiled leg runs keeps a program working in one
  mode and crashing in the other, which is the split the whole corpus exists to catch.

## Consequences

- `docs/language.md` now states short-circuiting as a **promise** ("a program may rely on this"), with the
  guard idiom's four shapes in the sample, and `docs/operations.md` describes the lowering as a branch with
  the `select` it replaced named — so the next reader does not re-add one.
- The `select`-based roads are gone, so `TestTheSkippedOperandIsNotInTheCompiledModule` can assert their
  absence: the function that holds an `and`/`or` contains no `select i1` at all, and a call that the test
  excluded sits after the `logic.rhs` label that guards it. Presence tests prove a door opens; the absence
  rows are what prove it closes (the lesson ADR 0273 learned from `function_calls`).
- `@rt_pair_truth` is emitted only when a module asks a tagged operand's truth (the block's reference scan
  does the bookkeeping, as everywhere else), so an ordinary program carries no new runtime code.
- A `try`/`except` around a skipped trap now finds nothing to catch — which is the correct behaviour and is
  asserted (`TestTheTrapTheTestSkippedIsNotRaised`), because a branch that swallowed a raise *and* answered
  the right number would look like success.
- Nothing in the exception machinery had to change: the raise inside the right operand's block is emitted by
  the same store-and-branch as anywhere else, its `br` to the handler is a terminator inside a block this
  commit created, and the merge is simply not on that path — which is why the two `ZeroDivisionError` wordings
  and `except ZeroDivisionError` keep working unchanged (ADR 0228, ADR 0253).
- **No new debt filed.** This row removed a divergence rather than displacing one: the shapes that were
  refusals before are the same refusals now (`programs/probe_and_or_shapes_the_word_carry.gy` still exits 1
  compiled, and its ledger row is untouched), because the skeleton declines exactly where the doors declined.
  The two rows beside it — **Gap R.146** (a pair-bound name in a position that keeps one word) and **Gap
  R.117**'s residuals — are unaffected, and the `probe_and_or_the_test_skips` ledger row is deleted rather
  than amended.
- One shape this record deliberately does not chase: `print(x and boom(), x and boom())` runs each operand
  per argument position, which is what the source wrote. Two evaluations of two *different* expressions is
  not the same defect as two evaluations of one, and the IR row asserts the latter.

## Verification

- `pkg/lang/logic_value_test.go` — four tables, both engines: `TestTheOperandTheTestDidNotChooseIsNeverRunOnTheCompiledBackend`
  (24 rows: a skipped call, a skipped trap, a skipped condition test, a skipped `while` head, chains, texts,
  containers, `None`, tagged-slot tests that must ask the runtime door);
  `TestTheSkippedOperandIsNotInTheCompiledModule` (IR shape: call counts, the call behind the branch, `phi
  i32`/`phi i1`, `@rt_pair_truth`, and no `select i1` in the operator's function);
  `TestTheOperandTheTestReachedStillTrapsOnTheCompiledBackend` (three traps that must still raise, with the
  reference's message, on both legs); `TestTheTrapTheTestSkippedIsNotRaised` (the `except` arm that must not
  run).
- `integration/logic_value_test.go` — `TestTheReferenceShortCircuitsAndSoDoesTheCompiledBackend` (20 rows × CPython +
  `--interp` + `--aot`, exit 2 forbidden by every row), `TestTheShortCircuitProgramPrintsTheSameOnEveryLeg`
  (the promoted program, plus the count of `boom` lines the reference never prints), and
  `TestTheOwedHalvesArePinnedAsTheyMeasure` reduced to the refusal probe the row still owes.
- `integration/programs/probe_and_or_the_test_skips.gy` — nineteen statements, three engines, one verdict:
  `oracle: match` in `integration/conformance-matrix.json`, and its `conformance_cases.go` debt row deleted
  (a paid debt that left its row behind would fail the build).
- Full suite green: `go test -tags=llvm20 ./...`; every emitted module still passes `opt -passes=verify`.
