# A number handed on by a body keeps the kind its caller's argument had

**Status:** accepted
**Date:** 2026-10-05
**Amends:** ADR 0273 (the `(payload, tag)` pair crosses a call, in both directions), ADR 0276 (a number handed to a function keeps the kind its argument had)
**Closes:** roadmap Gap R.161; the emitted-module half of Gap R.139's caller side
**Owes:** Gap R.164 (a pair-call answer bound to a name inside a forwarding frame), Gap R.162 (its `//`/`%` half, now measured one frame deeper)

## The decision

The pair road's gate asks the call graph, not just the callee's own text. A parameter is pair-carrying when
either

* some argument written at one of its call sites needs the pair or carries a double (ADR 0276's rule), or
* the parameter is **forwarded** — handed on by an enclosing body to a callee position that needs the pair —
  or forwards to one, in either direction, to any depth the program reaches.

The answer direction is a scan answer too: a function that hands a pair-returning callee's result straight
back is pair-returning whether its `define` was emitted before or after its caller's, and the tag word its
answer travels in is declared where its own `define` is emitted, tracked separately from the flag that
records "this body carries a tag".

## Why the gate needed the call graph

`def twice(v): return v * 2` / `def outer(x): return twice(x)` / `print(outer(2.5))` is CPython's `5.0` and
the interpreted leg's `5.0`. The compiled leg printed `4` at **exit 0**.

Nothing inside `outer` says `x` can be a float. ADR 0276's scan reads a name's recorded *bindings*, and a
parameter has none: `x` is written by no assignment, only by `outer`'s call site, which the walk files under
`twice`. So `exprCarriesDouble(Name x)` answered false, `twice`'s parameter was never marked, the pair that
existed one frame earlier was read as one `i32`, and the number came out truncated. The first attempt at this
cycle asked `s.calls[name]` from the expression question instead — that asks whether the name is *called*
somewhere, which is a different question, and it marked the wrong parameters. It is filed as owed rather than
patched over.

## What the scan does now

Three passes, all of them before any IR exists, so a `define` and every `call` to it agree on the arity
whichever comes first in the file:

1. **Direct evidence**, per parameter position: its call-site arguments and its default (ADR 0276).
2. **The call graph.** The walk now brackets a `def`'s body, so each call site knows whose parameter it is
   handing over; a call site inside `f` that passes `f`'s own parameter — positionally or by keyword, and
   only if the body never rebinds that name — is a forwarding edge. Marks then propagate along those edges in
   **both** directions, because both are load-bearing:
   * *caller → callee*: the caller holds a pair-bound name and hands it to this position, so the position has
     to carry it (this is the row above);
   * *callee → caller*: another call site proved the callee's position (`twice(2.5)` written somewhere else),
     so every caller — including a forwarding one — must supply the tag.
   The propagation runs in bounded rounds and only ever grows, so the fixed point is reachable.
3. **The gate**, unchanged in shape: supply (every argument of a marked position readable from its own
   spelling, extended with the case the call graph creates — the argument is a name the enclosing function
   received as its own parameter and that parameter is marked, which is exactly what `bindPairParams` binds
   as a pair), the body half (`pairUsesServed`), and the answer half (`pairBodyAnswers`), settled together.

A mark made only by an edge is kept only while its support lives. Each grown mark records the other parameter
it rests on; `pruneUnsuppliedForwards` closes a mark whose support was closed by the rounds — the callee's
body was not served, its answer does not come back as a pair, its own caller was closed — and the settle loop
re-runs the rounds after any closure, because closing a callee can close a caller in turn. Without this, a
caller whose body the gate closed would still call a two-word parameter with a one-word argument: the
truncation this file exists to remove, re-imported through the call graph. A forwarding **cycle** proves
nothing and is closed at a bounded depth, which refuses rather than guesses.

The prune may only ever *shrink*. Its first version re-derived every spec's `params` from its `wants` after
pruning, which silently restored the tag word to a function whose body the rounds had just closed — and the
program that exposed it was in the corpus already: `def scale(v): return round(v, 2)` printed `2` / `0`
(pinned divergence, `programs/probe_round_digit_count_kind_unseen.gy`) and started *refusing* instead. That is
the ladder rule working as an instrument: the pinned output came back unchanged, and the bug was found by the
whole-corpus binary sweep, not by the suite, which stayed green throughout.

## The tag word is declared where the callee is emitted

`pairRetDone` — "this function's answer carries a tag" — used to be filled while emitting that function's
body, and the same one-shot flag wrote its `@gy_f.anst` definition. A caller emitted *before* its callee
therefore asked a question the emission order had not answered yet: `def f(v): return other(v)` written above
`def other(w): return w * 2` refused with ADR 0273's sentence, for two lines whose text says nothing about
order. Now:

* the read is answered from the scan (`returnsPair`), pre-seeded before any emission, which is the same
  principle that put arity in the scan in ADR 0273 — asked of the program, not of the order;
* the definition is written at the callee's own `define`, guarded by its own `pairTagDeclared` set, because a
  read that can precede the write cannot share the write's guard.

Both orders are pinned by `TestTheForwardedPairIsSettledWhicheverOrderTheDefsAreWritten`, which asserts the
arity of both frames, one tag word per pair-answering function, and that each is both stored (by the callee)
and loaded (by the caller).

## Measured

CPython · compiled before · compiled after (the interpreted leg agrees with CPython throughout):

| program | reference | before | after |
| --- | --- | --- | --- |
| `def outer(x): return twice(x)` / `outer(2.5)` | `5.0` | `4` (exit 0) | `5.0` |
| the same, `outer(3)` | `6` | `6` | `6` |
| `outermost → middle → outer → twice`, `outermost(2.5)` | `5.0` | `4` (exit 0) | `5.0` |
| `def outer(a, b): return twice(b)` / `outer(1, 2.5)` · `outer(1, 2)` | `5.0` · `4` | `4` · `4` | `5.0` · `4` |
| `def outer(x): return add(x, 1)` / `outer(2.5)` · `outer(2)` | `3.5` · `3` | `3` · `3` | `3.5` · `3` |
| `outer(ys[1])` over `ys = [1, 2.5]` | `5.0` | `4` (exit 0) | `5.0` |
| float-state `x = 8` / `x = 2.5`, forwarded | `5.0` | `4` (exit 0) | `5.0` |
| `print(outer(v=x))`, the keyword form | `5.0` | `4` (exit 0) | `5.0` |
| `def f(v): return other(v)` over a float slot | `3.0` · `6` | exit 1 refusal | `3.0` · `6` |
| the same, callee written first / callee written second | `3.0` · `6` | exit 1 (order-dependent) | both `3.0` · `6` |

Nothing else in the corpus moved. The sweep that says so compares the pre-cycle binary with the new one over
every program in `integration/programs/` and every fixture of this cycle: the only output changes are
refusal → answer and wrong number → right number, and the two rows that look like diffs (`probe_ternary_container_arms.gy`'s
`llc` message, `probe_builtin_without_arguments.gy`'s panic) differ only in a temp-file path and a goroutine
address.

## Alternatives rejected

* **Consult `s.calls[name]` from the expression question.** Asks whether a name is called somewhere, not what
  flows into a parameter; it marked parameters whose arguments no caller writes. Filed as Gap R.161's origin
  story rather than left as an unmeasured tweak.
* **Widen the parameter whenever the callee is pair-marked, without a prune.** A callee closed by the body
  rounds would still be called with two words by a caller whose own body cannot read the pair it was handed —
  the truncation returning as a *new* wrong number, the one outcome the ladder forbids.
* **Answer `returnsPair` from the emission.** Cheapest edit, and it makes a program's arity question depend on
  where in the file a `def` sits — the class of bug ADR 0273 was written to end. The pinned both-orders test is
  the permanent answer to it.
* **Serve the `round`/`str`/index bodies by teaching the gate to trust those calls.** Their compiled roads read
  one word for the operand; teaching the gate otherwise turned a pinned wrong-number probe into a refusal, and
  the refusal of a call the pair cannot answer is the honest state (ADR 0166's exit-1 class).
* **Let a forwarding cycle settle by whichever mark arrived first.** Non-monotone and order-dependent; a cycle
  now proves nothing and closes, so the program keeps the road (and the refusal) it had.

## What is still owed

* **Gap R.164** — `def outer(x): y = twice(x); return y` prints `4` where CPython and the interpreted leg print
  `5.0` (exit 0). The frame above forwards, and the call *is* asked as a pair when its answer is returned
  directly; but when the body binds the answer to a name, and the answer direction closes that body, the call
  falls back to the ordinary road instead of binding through `bindPairCallResult` — the same door
  `print(twice(xs[0][0]))`'s binding rows already use. Cure: bind the pair at the call regardless of the body's
  own answer direction; the value-position read of the bound name is Gap R.146's remaining half.
* **Gap R.162**, now measured one frame deeper: `def outer(x): return floorit(x)` with `floorit(v) = v // 2`
  prints `2` for CPython's `2.0`, and `%` likewise. Consistent with ADR 0216's identity only because both
  halves truncate together.
