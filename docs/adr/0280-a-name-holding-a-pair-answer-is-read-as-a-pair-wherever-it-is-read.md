# ADR 0280 — a name holding a pair answer is read as a pair wherever it is read

Date: 2026-10-06
Status: Accepted
Roadmap: L11.6 (numeric truth in the compiled backend), closing **Gap R.164**; narrows the
one-word positions **Gap R.146** and **Gap R.143** still own.
Depends on: ADR 0273 (the pair crosses a call), ADR 0276 (an argument keeps its kind),
ADR 0277 (a body hands the kind on), ADR 0278 (`//` and `%` ask the tag), ADR 0279 (an arm is
asked the whole question), ADR 0181 (every handle-carrying store needs a root).

## The measure

`python3` is the oracle (3.12.3). `--interp` and `--aot` are the two engines. Every number below
was typed at the CLI, not remembered.

| program | CPython | `--interp` | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `def outer(x): y = twice(x); return y` → `print(outer(2.5))`, `outer(3)` | `5.0`, `6` | `5.0`, `6` | **`4`, `6` at exit 0** | `5.0`, `6` |
| same with `return y + 0` | `5.0` | `5.0` | **`4`** | `5.0` |
| `def outer(x): h = floorit(x); return h` | `2.0`, `2` | `2.0`, `2` | **`2`, `2`** | `2.0`, `2` |
| three frames: `b → a(x) → y = twice(x); return y` | `5.0`, `6` | `5.0`, `6` | **`4`, `6`** | `5.0`, `6` |
| `return (v // 2) + other(v)` | `4.5`, `4` | `4.5`, `4` | refusal | `4.5`, `4` |
| `return half(v) * 2 + 1` | `7.0`, `7` | `7.0`, `7` | refusal | `7.0`, `7` |
| `half(7.5) + twice(7.5)` / arms swapped | `18.0`, `18.0` | same | refusal | `18.0`, `18.0` |
| `idn(v) = floorit(v) * 2 + modit(v)` | `7.5`, `7` | `7.5`, `7` | refusal | `7.5`, `7` |
| `print(twice(xs[0][0]) + 1)` | `15` | `15` | refusal | `15` |
| `y = twice(x); y = 3; return y` | `3` | `3` | `3` | `3` |
| `y = twice(x); z = twice(y); return z` | `10.0` | `10.0` | **`8` at exit 0** | refusal (exit 1) |
| `y = twice(x); y += 1; return y` | `6.0` | `6.0` | **`5` at exit 0** | refusal (exit 1) |

The two rows the cycle *closed* and the two rows it *turned into refusals* are the same family.
The difference is which half of the machinery could be told the truth about them.

## The decision

**A name the pair road bound is read as a pair in every position that reads it, and a position
that cannot hold both words says so.** Four rules, all in `pkg/lang/paircall.go` /
`pkg/lang/heapargs.go`:

1. **A body may hold a pair without being handed one.** `pairBodyAnswers` now asks
   `pairBoundCallNames(body, callees, pairParams)`: a local whose binding is a call, by name, to a
   callee the scan judged pair-returning, whose arguments mention a pair-carrying parameter. Any
   other write to that name — `y = 3`, `y += 1` — retires it, so ADR 0276's rebinding row still
   answers `3` on the road it always used.
2. **A call's answer is a number-ish value.** `exprNumberish` grew a `*Call` case: a user callee
   whose every `return` is numberish hands back a number. It is asked only on the **return side**
   (`pairScan.readsCallAnswers`), because the same predicate answers ADR 0276's *argument* gate —
   "can the pair road produce both words for this argument from its spelling alone?" — and there
   `make()` must keep answering "no", which is what closes a parameter and keeps a pinned refusal
   refused. One predicate, two questions; the flag says which is being asked.
3. **An arm may be a call.** `arithOperandPair`, `arithWouldRefuse` and `slotArithmeticIsProven`
   read a pair-returning call as an operand, through `pairCallPair` — the same door `print` and a
   binding use. Before this, `return other(v)` answered and `(v // 2) + other(v)` refused.
4. **A closed position is not a free pass.** The scan records the positions it *considered* for the
   pair and then declined (`closed`), including the settle round that declines a body whose callee
   closed under it. Handing a float into such a position is a refusal, not a truncation.

## The wrong numbers this cycle nearly shipped, and what they cost

Three of them, each found by measurement rather than reading, and each of them *silent*:

* **`half(7.5) + twice(7.5)` printed `30.0` for `18.0` — and only in one arm order.** A double
  answer leaves `@rt_num_arith` as a heap box, and the door kept no root for it: the callee's frame
  closed, the next allocation recycled the slot, and the left arm's payload became a handle to
  somebody else's bits. Order-dependence is the loudest kind of bug this file takes seriously,
  because it passes a test that asks one order. Fixed by `rt_root_put` on the door's out-payload
  (`taggedArithPair`) and on a pair-returning call's payload (`pairCallPair`), both popped by the
  enclosing frame (ADR 0181's rule, taken at the call instead of at the binding).
* **`idn(v) = floorit(v) * 2 + modit(v)` printed `3.0` for `7.5`** — same cause one door deeper.
* **`def outer(x): y = twice(x); z = twice(y); return z` printed `8` for `10.0`** — the pair never
  entered `outer` at all, because ADR 0274's float-return road claimed the body: `exprNumberish`
  had no answer for a `*Call` and said "not a number", which is the answer that hands a body to the
  promotion road. Fixing the *answer* exposed the *hole* — with the callee closed, `outer`'s own
  marked position fell to the one-word road and truncated `2.5` to `2` at exit 0. Rule 4 above is
  what that hole looks like now: exit 1, naming the parameter and the missing kind.

## Alternatives rejected

* **Answer the arms and bindings with `tag = 0`.** Prints `7` for `identity(7.5)`. Every pinned
  refusal in `floor_pair_test.go` was written *before* the fix precisely so this could not pass.
* **Widen `exprNumberish` once, globally.** It took back three families that answer today —
  `def f(x): x = x + 1.5; return int(x)`, `return round(x)`, `def cmpf(v): return v > 1.5` — all
  turned from working answers into refusals by the first draft. The ladder forbids that far more
  loudly than a missing digit; the return-side flag is the narrowing that made them answer again.
* **Decline the ordinary road for *any* `return name` bound to a call.** Same three families, same
  failure: `int()` and `round()` are builtins whose answers the promotion road prints correctly.
  The rule asks whether the callee is a *user function* the pair road speaks.
* **Record every not-served body as a closed position.** The second draft. It refused `f(1.0)` and
  `cmpf(2.0)`, both of which printed CPython's answer before. Recording is limited to a body that
  actually holds a pair (`holdsPairFromPairCall`): binds a name to a user call's answer *and* reads
  that name in a `return`.
* **Delete the promoted refusal rows.** `print(twice(xs[0][0]) + 1)` now prints `15`, so its row
  *moved* from the refusal table to the answering table in all three files that pinned it
  (`pkg/lang/pair_call_test.go`, `integration/pair_call_test.go`,
  `integration/numeric_slot_arith_test.go`) — a pinned refusal earns a promotion by failing, and a
  deleted row cannot fail again.

## Agentic rationale

An agent consuming this compiler needs the *exit class* to mean something. Before this ADR the
class was a lie in one direction: exit 0 with `4`, `8` and `5` where CPython prints `5.0`, `10.0`
and `6.0`. Exit 0 now means "the number CPython prints", and the shapes this cycle could not
answer say so at exit 1 with the parameter, the position and the missing word named:

```
codegen: 2.5 is a float handed to parameter 0 of outer, a position this pass considered for the
(payload, tag) pair and closed because another call site hands it something it cannot name: the
ordinary road keeps one word per argument and would truncate the double to its int half, which is
a wrong number rather than a refusal (roadmap L11.6, Gap R.164, ADR 0280)
```

## Codegen / IR notes

* `@rt_num_arith`'s out-payload is rooted in the `numok` block; so is a pair-returning call's
  payload in `pairCallPair`. Both are `rt_root_put(i32* %tN)` against the frame `rt_frame_close`
  pops, so an int payload costs one dead frame entry and no correctness risk.
* A `define i32 @gy_f(i32 %p0, i32 %q0)` still means "this parameter arrives as a pair" and
  `@gy_f.anst` still means "the tag the callee stored beside its own return"; neither changed shape.
* Refusals are front-end errors (exit 1). Exit 2 — `llc` rejecting a module for an ordinary
  program — remains the compiler's own bug (ADR 0166) and is asserted against in every new row.

## Tests

* `pkg/lang/floor_pair_test.go` — `TestAPairExpressionCombinedWithMoreArithmeticAnswersOnTheCompiledBackend`
  gains the call-answer arm rows (`a call answer as an arm`, `two call answers as the two arms,
  either order`, `an identity assembled from two call answers`), and
  `TestAPairAnswerHeldByAPositionThatKeepsOneWordStillRefuses` gains the two rows this cycle left
  as refusals (`a pair answer bound and handed on as an argument`,
  `a pair answer read back by an augmented assignment`).
* `integration/forwarded_pair_test.go` — `forwardedParity()` gains five Gap R.164 rows, including
  the control that must not move: a name bound to a pair answer and then to an ordinary int
  answers `3`.
* `integration/floor_pair_test.go` — the same three-leg rows, and the refusal family's moved rows.
* `integration/programs/probe_bind_a_pair_call_answer.gy` — eleven lines CPython prints and both
  engines now print identically; registered in `conformanceStandalone()`. The corpus matrix went
  148 → 149 rows, 112 → 113 pass, 97 → 98 oracle match, 0 fail, 0 drift; the whole-corpus binary
  sweep against the pre-cycle compiler moved **only** the four probe files this cycle is about.
