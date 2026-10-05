# What floors a number and what remains ask the tag the number arrived with

**Status:** accepted
**Date:** 2026-10-05
**Amends:** ADR 0216 (flooring follows the reference, by operator and by kind), ADR 0273 (the `(payload, tag)`
pair crosses a call), ADR 0276 (a number handed to a function keeps the kind its argument had), ADR 0277 (a
number handed on by a body keeps the kind its caller's argument had)
**Closes:** roadmap Gap R.162
**Owes:** Gap R.166 (a floored answer combined with other arithmetic in one expression — refusal today),
Gap R.165 (printf-style `%` on a text answers `0.0`), Gap R.164, Gap R.148 (`/` over a pair)

## The decision

The pair door serves the flooring operators. `+ - *` were already asked of the tagged runtime
(`@rt_num_arith`); `//` and `%` now go through the same helper, as two new operator codes, and the answer's
tag is the operands' — computed from `bothint`, not from what the source text looked like.

Consequences, all of them the same claim seen from a different side:

* a parameter whose pair carries a double floors to a double: `def floorit(v): return v // 2` answers `2.0`
  for `floorit(5.0)` and `2` for `floorit(5)`, from one `define`, on both engines;
* the flooring *rules* are ADR 0216's, unchanged and un-duplicated: the floor is `@llvm.floor.f64` (rounds
  toward negative infinity, not toward zero) and the remainder is `frem` corrected to the divisor's sign. One
  helper holds them, so the statement road and the pair road cannot drift into two definitions of `//`;
* the divide-by-zero *sentence* is the operand kind's, and there are four of them, not two: the reference
  answers `7 // 0` with "integer division or modulo by zero" and `7 % 0` with "integer modulo by zero", and
  each float case has its own. Over a parameter, which of the four a line gets is decided by the tag.

## Why the door had to answer it itself

Every one of these printed a truncated digit at **exit 0** on the compiled leg, while the interpreted leg —
which boxes the value and asks it — printed the reference's answer:

| program | reference | compiled before | compiled after |
| --- | --- | --- | --- |
| `floorit(v) = v // 2` / `floorit(5.0)` | `2.0` | `2` | `2.0` |
| the same, `floorit(5)` | `2` | `2` | `2` |
| `modit(v) = v % 2` / `modit(5.0)` | `1.0` | `1` | `1.0` |
| the same, `modit(5)` | `1` | `1` | `1` |
| `floorit(-7.5)` | `-4.0` | `-4` | `-4.0` |
| `modit(-7.5)` | `0.5` | `1` | `0.5` |
| `floordiv(a, b) = a // b` / `(7.5, 2)` | `3.0` | `3` | `3.0` |
| `modop(a, b) = a % b` / `(7.5, 2)` · `(-7.5, 2)` | `1.5` · `0.5` | `1` · `1` | `1.5` · `0.5` |
| `modop(-7, 2)` (integer control) | `1` | `1` | `1` |
| `def outer(x): return floorit(x)` / `outer(5.0)` | `2.0` | `2` | `2.0` |
| `if v > 10: return v // 2` / `big(18.5)` | `9.0` | `9` | `9.0` |
| `f(v) = v // 0` / `f(5.0)` | `float floor division by zero` | `integer division or modulo by zero` | `float floor division by zero` |
| `f(v) = v % 0` / `f(5.0)` | `float modulo` | `integer modulo by zero` | `float modulo` |
| `f(v) = v % 3` over `xs[0][0]` (int slot) | `1` | exit 1, slot door declines | `1` |
| `f(v) = other(v)` / `other(w) = w % 3` over a slot | `1` | exit 1 | `1` |

The last two rows are the same defect seen from the gate side: ADR 0273 pinned them as refusals because the
body could not read a tagged parameter back. They left the refusal tables with this ADR and joined the parity
tables, which is the promotion the ladder asks for — a refusal becoming an answer, never the other way.

The reason the truncation was silent is the same as ADR 0276's: a parameter has one word. `//` and `%` were
in neither list of served shapes — not the pair door's (`+ - *`), not the condition doors (which take
comparisons and answer `True`/`False`) — so the parameter was never marked pair-carrying, the double was
read as an `i32` at the boundary, and the floor ran on the truncated payload. `print(modop(-7.5, 2))` printing
`1` for `0.5` is the sharpest of the set: the integer road's `%` is CPython's for positives and wrong for
negatives, so the truncation hid a second error underneath it.

## What the runtime gained

Inside `numArithRuntimeIR` (`pkg/lang/codegen.go`), the code `@rt_num_arith` already dispatches on:

* **op 4 — floor division of doubles**: `fdiv double`, then `@llvm.floor.f64`. Not `sdiv`: the integer
  instruction truncates toward zero, which is the C behaviour ADR 0216 rejected.
* **op 5 — remainder of doubles**: `@frem double`, then the correction that turns LLVM's truncated remainder
  into the reference's floored one (add the divisor when the signs of divisor and remainder disagree). Then
  `floor(x / y) * y + (x % y) == x` holds of the doubles, which is what makes `//` and `%` a pair rather than
  two builtins.
* **a `divzero` block** with a status of its own (`3`), chosen before any answer is written, and the four
  sentences as `@rt.num.dzi` / `@rt.num.dzm2` / `@rt.num.dzf` / `@rt.num.dzm`, selected by `%bothint` and by
  the operator. `rt_num_bad`'s operator-symbol table learned `//` and `%` too, so the TypeError wording for a
  text operand still names the operators it was given.

The tag the pair door hands back is `%bothint` and nothing else: `f(5)` and `f(5.0)` differ in the tag, and the
same source line answers with the integer sentence or the float one accordingly. The integer overflow guard is
unchanged, still driven by `%bothint` — a pair whose tag says double cannot overflow the `int` word, and a pair
whose tags both say int is guarded as before.

The emitter side is `taggedArithPair` (`pkg/lang/heapargs.go`), which maps `"//"` → 4 and `"%"` → 5, plus the
status decode the pair door already runs for its other codes: `1` → `TypeError`, `2` → `OverflowError`,
`3` → `ZeroDivisionError`, each raised through the catchable machinery rather than printed.

## Guarded, deliberately: `%` beside a text

`"%.2f" % 3.5` is the reference's string formatting, not a remainder. The pair door keeps its guard: an
operand that may be text or is not a proven number leaves `//` and `%` on the ordinary road, exactly as `%`
already did for the shapes Gap R.82 refuses. That guard is load-bearing in both directions, and this cycle
measured the half the guard does *not* cover — see Gap R.165: three shapes of text-`%` currently print `0.0`
at exit 0 (unchanged by this ADR, measured before and after on the same binary pair), which is the reason the
guard is described here instead of quietly kept.

## Machine path

* `--emit-llvm 'def f(v): return v // 2'` shows the pair door's call — `@rt_num_arith(i32 4, …)` — and
  `--emit-llvm 'def f(v): return v % 2'` shows `@rt_num_arith(i32 5, …)`, so an agent can tell from the module
  which road a floored answer took without reading the source.
* `--json` diagnostics cover the refusals that remain (`pair road cannot answer`, `hands back the (payload,
  tag) pair`) with the roadmap ID in the message; the four ZeroDivisionError sentences are in the traceback the
  CLI writes to stderr and in the `--json` report's `message` field, unchanged in wording from the reference —
  that is the contract ADR 0216 pinned and this one extends.
* Every module above passes `llc-20` and the module verifier at exit 0; the refusals exit 1 and exit 2 remains
  the compiler's own bug (ADR 0166).

## Alternatives rejected

* **`sdiv`/`srem` for the double cases, or `fdiv` without `floor`.** Both answer the positive probes and lie
  on the negative ones (`-7.5 // 2` would be `-3`), which is how the C rule keeps re-entering a language that
  means the Python one. The correction after `frem` is three instructions and is why the sign rows are in the
  test table.
* **Reuse the statement-level float road by lifting the pair to a `double` at the door.** This is what
  ADR 0268 does for number positions and what Gap R.162's own `Free text` suggested. Rejected: the lift is
  lossy in the other direction — a pair whose tag says `int` must answer an `int` (`floorit(5)` is `2`, not
  `2.0`), so the door would have to lift, floor, and re-decide the answer's kind from a tag it had thrown away.
  Asking `@rt_num_arith` keeps one decision point.
* **Two sentences instead of four for the divide-by-zero family.** The first draft of the runtime table shared
  "integer division or modulo by zero" across `//` and `%`. The reference does not: it answers `7 % 0` with
  "integer modulo by zero". The whole-corpus sweep caught the drift by comparing the interpreter's and the
  compiled leg's wording line by line; the row in `floor_pair_test.go` is the permanent answer.
* **Teaching the gate to accept `(v // 2) * 2 + (v % 2)` by answering nested arithmetic with the payload
  alone.** That would print `7` for `identity_check(7.5)` — a number, and the wrong one. The combined shapes
  stay refused (pinned by `TestAFlooredAnswerCombinedWithOtherArithmeticRefusesRatherThanTruncates`) and are
  filed as Gap R.166, where the cure is to give the arms of a pair expression their own pair answers.
* **Marking the parameter for `//`/`%` without serving the answer half.** A marked parameter whose body cannot
  compute with the pair is ADR 0273's refusal, so the two halves had to land together; the gate asks
  `pairUsesServed` and `pairAnswerShape` about the same operator list, and a change to one without the other
  turns answers into refusals — which is why the operator list is one list in each question, not three.

## Tests

* `pkg/lang/floor_pair_test.go` — the parity table (interpreter and compiled from the same source), the module
  table (the door is asked with the two new codes; the shared helper's `@llvm.floor.f64`, `frem double`, and
  the four sentences are present; a divide-by-zero branch exists on the path), the trap table (all four
  sentences, chosen by tag, exit non-zero), and the refusal table (Gap R.166's shapes stay exit 1).
* `integration/floor_pair_test.go` — the same family at the CLI against `python3` on three legs, the promoted
  probe, the traps' sentences read from the merged stream, and the refusals' exit class and sentence.
* `integration/programs/probe_floor_a_pair.gy` — ten lines, promoted from this cycle's probes, registered as a
  `conformanceStandalone()` case; the matrix moved 146 → 147 rows and 95 → 96 oracle matches with no demotion.
* `pkg/lang/pair_call_test.go` — two rows *moved* rather than deleted: `{"floor division", "return v % 3"}`
  left the "must not be marked" table for the parity table, and the prune table's "the callee floors it"
  became "the callee takes a power of it", so the gate keeps a witness while the served list grows.

## What is still owed

* **Gap R.166** — `(v // 2) * 2 + (v % 2)`, `(v // 2) * 2`, and `v * 2 + 1` over a pair-marked parameter are
  refused (they printed a truncated integer before this ADR, at exit 0). The arms of a pair expression need
  their own pair answers; today only the top of the expression is asked.
* **Gap R.165** — `print("%.2f" % 3.5)` prints `0.0` at exit 0 on the compiled leg, `3.50` in the reference,
  and raises `TypeError` in the interpreter. Measured unchanged by this cycle, filed fresh.
* **Gap R.146's remaining half** — a pair answer appended to a container is still refused (`hands back the
  (payload, tag) pair … this position keeps one word`), where this cycle's change means the program used to
  print `2` and `2` for CPython's `2.0` and `2`. Refusal is the standing the ladder allows until the box exists.
* **Gap R.148** — `/` over a pair, the third operator of the family, still on the ordinary road.
