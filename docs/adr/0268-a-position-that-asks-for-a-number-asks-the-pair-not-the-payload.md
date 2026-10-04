# 0268. A position that asks for a number asks the pair, not the payload

## Status

Accepted. Ships with the pair read in the compiled backend's number positions — an operand of a sum
(`print(n + 1)`), a negation, an ordering against a number (`print(n > 13)`, `13 > n`), a condition's
head (`if n:`, `while n > 0:`, `if n > 1 and n < 20:`), an f-string field, `str`/`repr`, and the target
of an augmented assignment (`n += 1`) — as one helper set in `pkg/lang/heapargs.go`
(`numericPairVar`, `numericPairRegs`, `liftPair`, `numericPairLift`, `numericExprPair`,
`staticNumberDouble`, `pairOrder`, `bindArithmeticPair`) and hooks at four doors in
`pkg/lang/codegen.go` (the truth road, the hoisted comparison road, the f-string field arm) and
`pkg/lang/render.go` (the renderer's arms).

Closes roadmap **Gap R.143** ("A name the pair road bound is refused in the positions that ask for one
static number"), the row ADR 0267 filed when it opened the binding.

Files **Gap R.146** (the positions that keep one word for a whole *value*: a builtin's argument, a
container's element, an `and`'s operand), **Gap R.147** (`and`/`or` answer the verdict where CPython
returns the operand — `print(2 and 3)` prints `1` on both engines, `3` in CPython; measured while
writing this cycle's probe, and neither engine is right), and **Gap R.148** (a pair-bound name entering
the float domain: `print(n / 4)`, `print(n > d)` where `d = 2.5` — refused by the float road, which
cannot store a double into the i32 slot a tagged name owns).

Promotes `programs/probe_pair_bound_name_as_a_number` from the debt ledger to standalone parity (fifteen
lines, three engines, the same bytes) and registers `programs/probe_pair_bound_name_takes_a_value` as the
debt row for R.146 and R.147.

## Context

ADR 0267 gave `n = xs[0][0] * 2` a payload and a tag: `_n` and `_n_tag`, the shape the print dispatch had
read since ADR 0185. `print(n)` answered. Everything else about that name refused:

```
xs = []
xs.append([7, 8])
n = xs[0][0] * 2
print(n)        # 14   — the print door takes the pair
print(n + 1)    # exit 1 — "n holds the answer of arithmetic over a slot …"
if n:           # exit 1
print(str(n))   # exit 1
n += 1          # exit 1
```

Same value, same two words, five more doors that had never been told the second word existed. Each of
them asks the ordinary numeric road — `value()` — and that road's contract is one `i32`, so it refused a
name whose number's family lives in a tag. That is the honest refusal, in the right exit class, on a
program CPython answers; which makes it exactly the debt the row was written for rather than a bug.

The interesting part is that the *proof* was already in the record. ADR 0267 wrote
`taggedOrigin[name] = taggedOriginArith` so a refusal could name where a tag came from. The door that
writes that origin is `@rt_num_arith`, and `@rt_num_arith` stores the int tag or the float tag after it
has already raised on every other kind. So a name with that origin is not "some tagged value whose kind
nobody can state" — it is provably a number, and the only open question is which of the two families. The
module already had the answer to *that* question, twice over: `@rt_lift_num` (ADR 0249's widening,
reused by ADR 0253's division door) turns a `(payload, tag)` pair into a `double`, and
`@rt_str_of_value` (ADR 0258's renderer, the one `print`, `str` and `repr` agree on) renders a pair
directly. Nothing had to be invented; four doors had to ask.

## Decision

**Give every number-asking position the pair, by lifting it — and leave the value-asking positions
refusing.**

1. **The proof is the gate.** Every new arm starts from `numericPairVar(name)`: the name is in
   `taggedVars` *and* its origin is `taggedOriginArith`. A tagged name from another door is not admitted —
   a slot read bound by ADR 0241 and a loop variable bound by ADR 0185 can hold a text or `None`, and
   opening the `+`/`*` gate for them would raise where CPython answers `"a" + "b"` (Gap R.82). This is why
   the origin record was worth writing in the previous commit: it is not decoration on a message, it is a
   fact the compiler can dispatch on.
2. **Lift, don't guess.** A position that wants a number gets `@rt_lift_num`'s `double`, never the payload:
   `fcmp one double %d, 0.0` for truth (so a `0.0` is false and a NaN true, as `bool()` says), `fcmp olt`
   …`fcmp oge` for an ordering, and the widening of the other operand by `staticNumberDouble` (an int
   literal through `sitofp`, a float literal through its own constant, a float variable through
   `floatValue`, a plain int variable through the ordinary road and a `sitofp`). An int compared to an int
   through a `double` is exact for every value this backend's `int` word holds, so the widening costs one
   call and buys the correct answer for the mixed cases without a second dispatch.
3. **Exactly one side may be a pair.** `pairOrder` requires one operand pairable and the other
   *statically* numeric; both-pairs or neither returns `ok=false` and the ordinary roads — the tagged-order
   door with its per-kind raises, the plain `icmp` — keep their programs. Landing a door must not reroute
   a program that was already answered, which is why the ordering hook sits where the comparison's operands
   are still unlowered (next to ADR 0250's hoist) and not after them.
4. **The renderer is asked first.** `renderPair`'s new arm precedes the container probe, because the
   container probe lowers its argument as a number to find out whether it is a container — for a pair-bound
   name that probe is the refusal, and the pair arm never runs. Measured: `str(n)` kept refusing after the
   arm was written, until it moved above the probe. The arm calls `@rt_str_of_value(payload, tag, quote)`,
   the same capture-buffer call `str` and `repr` already share, so the two halves cannot disagree about
   `14` versus `14.0` and the f-string field arm calls it too (`f"v={n}!"` is the same bytes as
   `"v=" + str(n) + "!"`).
5. **An augmented assignment is an assignment.** `n += 1` builds a `BinOp` over the target and asks
   `bindArithmeticPair`, the one road ADR 0267 inlined at the plain assignment — so both assignments now
   share one door, and `+=`, `-=` and `*=` get the pair for the same price. `/=` and the rest stay where
   they were: the pair door is `+ - *` and unary `-`, and true division has its own owed road (Gap R.148).
6. **A position that stores a value is not a number position, and stays refused.** `abs(n)`, `min(n, 3)`,
   `[n]`, `n and 3` each keep one word for the thing they hold — an argument slot, an element slot, the
   result of the expression — and a tag has nowhere to live beside it. They refuse with the sentence that
   names the missing half ("this position keeps one word for its operand, so the tag has nowhere to go"),
   cited to Gap R.146 rather than to the paid row. The alternative — writing the payload and letting the
   reader guess — is ADR 0187's forbidden answer.

## Measurement

Reference leg CPython 3.12.3; compiled leg Ubuntu LLVM 20.1.2 (`llc-20`, `opt-20`). Every parity row runs
the same source through `python3`, `gustyc --file … --interp` and `gustyc --file … --aot`; exit 2 fails a
row by itself (ADR 0166), and no row in this commit reached it.

| Position | Before | After |
|---|---|---|
| `print(n + 1)`, `print(n - 1)` | exit 1 | `15`, `13` on three legs |
| `print(-n)` | exit 1 | `-14` |
| `print(a + b)` (both pair-bound) | exit 1 | `17` |
| `if n:` / `else` | exit 1 | `truthy` / `no` (a zero pair answers false) |
| `while n > 0 and i < 2:` | exit 1 | the reference's two lines |
| `print(n > 13)`, `print(13 > n)` | exit 1 | `True`, `False` |
| `print(n > k)` (int variable) | exit 1 | `True` |
| `print(1 if n > 1 else 0)` | exit 1 | `1` |
| `print(f"{n}")`, `print(f"v={n}!")` | exit 1 | `14`, `v=14!` |
| `print(str(n))`, `print(repr(n))`, `print(str(n) + "!")` | exit 1 | `14`, `14`, `14!` |
| `str(r)` where the pair is a float | exit 1 | `17.5` — the tag, not a guess |
| `n += 1`, `n *= 2`, `n += 0.5` | exit 1 | `15`, `28`, `14.5` |
| `abs(n)`, `min(n, 3)`, `[n]`, `n and 3` | exit 1 | still exit 1, refusal names the one-word operand (Gap R.146) |
| `print(n / 4)`, `print(n > d)` where `d = 2.5` | exit 1 | still exit 1, refused by the float road (Gap R.148) |
| `print(2 and 3)` (any source) | `1` both engines | `1` — CPython says `3`; filed as Gap R.147 |

Counts, from one green run of `go test -tags=llvm20 ./...`: `pkg/lang/pair_binding_test.go` 10 tests — 10 +
9 + 8 rows carried over from ADR 0267, plus 16 new both-engine rows in
`TestAPairBoundNameIsReadWhereverANumberIsAsked`, the module-level `TestThePairRoadCarriesTheLiftAndTheRenderer`
(the module carries `@rt_lift_num`, `@rt_str_of_value`, `fcmp one double`, `fcmp ogt double`, and a program
that binds no pair carries neither helper), and 4 filed refusal rows.
`integration/pair_binding_test.go` 9 tests — 20 three-engine rows in
`TestAPairBoundNameAnswersWhereverANumberIsAskedAtTheCLI`, 6 filed rows in
`TestThePairRoadStillRefusesThePositionsThatTakeAValueAtTheCLI`, and the ADR 0267 tables unchanged. Corpus:
`programs/probe_pair_bound_name_as_a_number` promoted to standalone parity, `programs/probe_pair_bound_name_takes_a_value`
registered with per-leg pins, one `debt` row out and one in.

## Agentic rationale

* **A refusal cites a row that still owes.** The message these positions print ended `(roadmap L11.1, Gap
  R.143)` when R.143 was open; the same sentence now ends `(roadmap L11.1, Gap R.146)` and the paid
  positions never reach it. A caller reading `--json` decides between rewriting the program and filing an
  issue based on that citation, so it has to be true at the moment it is printed.
* **The machine contract did not move.** Same flags, same JSON diagnostics, same exit classes: programs the
  reference answers now exit 0 where they exited 1, and the refusals stay exit 1 with the missing half
  named. Nothing here can produce exit 2, and both new tables fail the row if it appears — the float road
  is the one place a naive version of this change *would* have produced it (Gap R.148 records why).
* **The gates are askable.** `numericPairVar` is exported to the test file as a fact, not inferred from
  output: `TestThePairGateIsTheSameDoorInTheBindingAsInThePrint` (ADR 0267) and the new IR-shape test ask
  the compiler and the module directly, so a future cycle that loosens the proof finds a failing test rather
  than a plausible-looking print.

## Alternatives rejected

* **Register a pair-bound name as a float variable.** One line in `isFloat`, and half the table above works
  immediately — including the two rows this commit leaves refused. It is wrong twice: `print(n)` would
  choose the float formatter and print `14.0`, and an augmented assignment would route through the double
  road and `store double` into the i32 slot the tagged binding allocated, which is the module `llc`
  rejects. That experiment was built and measured, and it is why Gap R.148 is a row with a hazard note
  rather than a TODO.
* **Widen the gate to every tagged name.** `taggedVars` alone would admit ADR 0241's slot-read bindings and
  ADR 0185's loop variables, whose tags can be text or `None`; `+` and `*` would then raise where CPython
  joins two texts (Gap R.82), and the ordering door would need its full cross-kind machinery to stay
  honest. Those names get their own rows and their own doors.
* **Answer the ordering with the payload when the tag says int.** Saves a call, and needs the tag at
  compile time — which is the one thing this language does not have. The `phi`-per-arm version is ADR
  0250's door, and it exists for names whose tag can say three things; here it would be a chain of two
  branches and a raise arm that cannot run.
* **Put the pair into `value()`'s return protocol.** A third return value threaded through ~90 call sites,
  most of which cannot use it and would silently ignore it — the shape of bug ADR 0187 is, and the opposite
  of "a part that cannot be lower is a compile error" (ADR 0225). The two-door approach keeps each consumer
  asking the question it can actually answer.
* **Fix `and`/`or` in this commit.** `print(2 and 3)` printing `1` on both engines is a wrong answer at exit
  0 on a core operator, and it is the most valuable thing this cycle found by accident. It is also not this
  cycle's door: nothing to do with tags, two backends, and a truthiness table to redo. Filed as Gap R.147
  with its numbers and its probe line, in the ledger where the next cycle will find it.
* **Leave `str(n)` rendering through `rt_fmt_double`.** It prints `14.0` for the int family — the exact
  disagreement ADR 0258 was written to end. The renderer takes the pair; that is what it is for.

## Consequences

* L11.1's pair now reaches: the print dispatch, equality and ordering, the container writers, the binding,
  and — since this commit — the number positions. It does not reach a position that stores a value
  (Gap R.146), a call argument (Gap R.139), a tuple-unpacking target (Gap R.144), the float domain
  (Gap R.148), or a membership test and loop over a built container (Gaps R.95, R.83). Each is refused in
  words, each is pinned.
* `bindArithmeticPair` is now the only assignment road that binds a pair, so the plain and augmented
  assignments cannot drift apart; the two inline copies of the bind body are gone.
* The measured hazard is recorded rather than latent: routing a pair-bound name into the float road stores
  a `double` into an `i32` slot. Gap R.148's rows assert those shapes stay refusals, so the day someone
  routes them, the tests fail with the class of failure named instead of an `llc` dump.
* Gap R.147 is priority-1 class (wrong digits, exit 0, both engines, core operator) and was found by writing
  a probe rather than by a test failing. The probe is in the corpus with both legs pinned, so the fix has
  its own evidence trail before it starts.
* No CLI surface moved; `docs/operations.md`'s refusal examples are updated in this commit, and no schema
  changed. The corpus stands at 149 programs.
