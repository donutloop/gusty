# The arms of a pair expression are asked the same question as the whole

**Status:** accepted
**Date:** 2026-10-05
**Amends:** ADR 0278 (`//` and `%` ask the tag the number arrived with), ADR 0276 (a number handed to a
function keeps the kind its argument had), ADR 0265 (the `+`/`*` gate over a slot), ADR 0216 (flooring follows
the reference, by operator and by kind), ADR 0273 (the `(payload, tag)` pair crosses a call)
**Closes:** roadmap Gap R.166
**Owes:** Gap R.146 (a pair answer in a position that keeps one word — a container's element, a builtin's
argument), Gap R.164 (a call's pair answer used as an arm of an expression), Gap R.148 (`/` over a pair)

## The decision

The pair door's operand question is asked of an expression's **leaves**, not of its top node. When
`taggedArithPair` is asked to combine two operands, each operand's "can a slot this pass cannot see reach
this?" test — `slotArithmeticIsProven` — now walks a `BinOp`, an `UnOp` and a `CondExpr` and asks the same
question of everything under them, the way `slotLiteralMayBeNumber` and `arithWouldRefuse` already did.

So a body may be:

```python
def identity(v):
    return (v // 2) * 2 + (v % 2)     # CPython's 7.5 for identity(7.5), on both engines
```

and every arm of it — `v // 2`, `v % 2`, and each `v` inside them — is answered by the one tagged arithmetic
door, with the tag the caller supplied. The nesting is not a special case bolted onto the flooring operators:
the same walk answers `v * 2 + 1`, `v + 1 + 1`, `2 * (v + 1)`, `(v + v) * (v - 1)` and a float-state name in
`(x + 1) * 2`, because all of them were the same missing recursion.

## Why the top node was the wrong question

Three questions about an operand were already walked recursively — `slotLiteralMayBeNumber` (may this be a
text or a container?), `arithWouldRefuse` (would the ordinary road have refused this?), and the door's own
shape test. One was not:

```go
nm, depth := chainDepth(e)          // (nil, 0) for a BinOp
if nm == nil || depth < 1 || … { return false }
```

`slotArithmeticIsProven` answered "is this a read of a slot the program is shown to hold only numbers?" by
looking at the node it was handed. A bare `v` — a pair-carrying parameter — passed its `numericPairVar` check
and was proven. `(v - 1)` was not a name and not a chain, so the answer was *no*, the door declined, and the
whole expression fell back to the ordinary numeric road, which has one word for a parameter and refused.

That is why ADR 0278 could land `v // 2` as an answer and leave `(v // 2) * 2` refusing in the same commit: the
operators were served, but only at the top. It also explains the stranger-looking half of the row —
`v * 2 + 1` had refused since ADR 0276, three ADRs before this one, for the identical reason.

## The shortcut that was rejected

The arms are `Expr`s and `arithOperandPair` already returns `(payload, tag)` for the cases it knows. Emitting
each arm through the ordinary numeric road and taking `tag = 0` would have made all twelve of this ADR's
programs **print something**, and one of the rows — `identity(7.5)` — would have printed `7`.

A number, at exit 0, and the wrong one: the outcome ADR 0273 was written to make impossible and the ladder rule
forbids. So the sequence of this cycle was: ADR 0278 pinned those three shapes as refusals (a wrong number
becoming a refusal is the allowed direction), this ADR turns the refusals into answers by asking the honest
question of the leaves, and the two pinned tables moved together — parity rows in, refusal rows kept for the
positions that still cannot hold a tag.

## Measured

CPython · compiled before · compiled after (the interpreted leg agrees with CPython throughout; the pre-cycle
binary is the one ADR 0278 shipped):

| program | reference | before | after |
| --- | --- | --- | --- |
| `(v // 2) * 2 + (v % 2)` / `f(7.5)` | `7.5` | exit 1, "the pair road cannot answer" | `7.5` |
| the same / `f(7)` · `f(-3.5)` | `7` · `-3.5` | exit 1 | `7` · `-3.5` |
| `(v // 2) * 2 + (v % 2) == v` / `f(7.5)` | `True` | exit 1 | `True` |
| `(v // 2) * 2` / `f(7.5)` | `6.0` | exit 1 | `6.0` |
| `v * 2 + 1` / `f(7.5)` (refused since ADR 0276) | `16.0` | exit 1 | `16.0` |
| `(v - 1) * 2` / `f(2.5)` | `3.0` | exit 1 | `3.0` |
| `v + 1 + 1` / `f(2.5)` | `4.5` | exit 1 | `4.5` |
| `2 * (v + 1)` / `f(2.5)` | `7.0` | exit 1 | `7.0` |
| `(v % 3) + (v // 3)` / `f(7.5)` | `3.5` | exit 1 | `3.5` |
| `(v + v) * (v - 1)` / `f(2.5)` | `7.5` | exit 1 | `7.5` |
| `x = 2` / `x = x / 2` / `(x + 1) * 2` | `4.0` | exit 1, "x holds a double…" | `4.0` |
| `(xs[0][0] - 1) * 2` over `xs.append([7.5, 8])` | `13.0` | exit 1, "index cannot reach…" | `13.0` |
| `def g(w): return (w - 1) * 2` over a slot | `3.0` | exit 1 | `3.0` |

Unchanged, and pinned so: a pair answer appended to a list (`hands back the (payload, tag) pair…`, Gap R.146),
a nested answer as a list-literal element (`list literal elements must be integers`), a *call's* pair answer as
an arm (`the pair road cannot answer`, Gap R.164), and a pair answer as another function's argument. No program
in `integration/programs/` changed output at all on this change — the whole-corpus sweep's diff was empty,
which is the check that a widened gate has not rerouted anything that already answered.

## Machine path

* `--emit-llvm 'def f(v): return (v // 2) * 2 + (v % 2)'` shows two `@rt_num_arith` calls with operator codes
  `4` and `5` feeding an outer `i32 0` call, each arm loading its own payload and tag — the module is the
  evidence that the arms were tagged rather than assumed.
* The refusals that remain keep ADR 0273's sentences and exit 1 (`--json`'s diagnostic stream included);
  exit 2 stays the compiler's own bug (ADR 0166), and every module above passes `llc-20` and the verifier.
* `programs/probe_combine_a_floored_pair.gy` is the cycle's closing event: twelve lines, three legs, one
  answer, registered as a standalone conformance case (matrix 147 → 148 rows, 111 → 112 parity, 96 → 97 oracle
  matches, no demotion).

## Alternatives rejected

* **Answer the arms by payload with `tag = 0`.** Prints `7` for `identity(7.5)`. Rejected above; it is the
  reason the refusal pins were written *before* the recursion, so the temptation had a failing test attached.
* **Special-case a flooring arm (`//`, `%`) while leaving `+`/`*`/`-` arms to the top-node answer.** Half the
  family, and the wrong half: `v * 2 + 1` had been refused longest, and a per-operator list of which arms are
  walkable is exactly the "three lists that must agree" trap ADR 0278's lesson names.
* **Widen `chainDepth` to reach through a `BinOp` to a nested slot read.** Would answer the question about a
  container the pass cannot see by picking one of its leaves; the leaf walk asks each leaf's own question
  instead, and an unprovable slot still returns `false`.
* **Let the door emit a payload-only arm when the arm's own arms are all literals.** Reduces to the first
  alternative for the shapes that matter (`(v // 2) * 2` is exactly that shape) and would have shipped the
  wrong `7`.
* **Ask the gate at emit time per arm.** The gate is a scan answer everywhere else in this file's history
  (ADR 0273's arity rule); an arm question that depends on emission order would have made the two-order
  pinned test fail, which is how that rule was earned once already.

## Tests

* `pkg/lang/floor_pair_test.go` — `TestAPairExpressionCombinedWithMoreArithmeticAnswersOnTheCompiledBackend`
  (interpreter and compiled from the same source, twelve rows) and
  `TestAPairAnswerHeldByAPositionThatKeepsOneWordStillRefuses` (the four positions that still decline, each
  pinned by the sentence it must say).
* `integration/floor_pair_test.go` — the same family at the CLI against `python3` on three legs, plus the
  refusal rows' exit class and sentence.
* `integration/programs/probe_combine_a_floored_pair.gy` — the promoted program above.
* Both files lost their `Gap R.166` refusal rows in the same commit that gained the parity rows: the pinned
  refusals were the cycle's acceptance test, and they failed before the fix landed, which is the only way a
  pinned refusal earns its keep.

## What is still owed

* **Gap R.146** — a pair answer in a one-word position: `out.append(floorit(5.0))`, `[v * 2 + 1]`,
  `abs(v * 2)` (which answers `4` for CPython's `5.0` — a pre-existing wrong number in that family, measured
  identical before and after this change and left as that row's evidence rather than moved into this one).
* **Gap R.164** — `(v // 2) + other(v)`: a *call's* pair answer is refused as an arm, and the same body with
  the answer returned directly answers. Bind the pair at the call whoever the body's answer is, and this
  ADR's arm walk picks it up for free.
* **Gap R.148** — `/` over a pair, still on its own door since ADR 0253 and still not tag-aware.
