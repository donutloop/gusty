# 0267. A binding takes the answer in the pair the arithmetic arrived in

## Status

Accepted. Ships with the pair road in the assignment statement (`pkg/lang/codegen.go`, the
`AssignStmt`/`*Name` branch: `arithWouldRefuse` → `taggedArithPair` → `bindTaggedVar`), the provenance
record and honest refusal (`pkg/lang/heapargs.go`, `taggedOrigin`, `taggedOriginArith`,
`(*irGen).taggedVarErr`), and the retraction half of the binding rule (`(*irGen).forgetTaggedBinding`,
called by the container, comprehension, lambda, module-global, float and plain-number bindings).

Closes roadmap **Gap R.138** ("The arithmetic the print position answers is refused one statement
earlier": `xs = []` / `xs.append([7, 8])` / `n = xs[0][0] * 2` / `print(n)` — CPython `14`, the
interpreted leg `14`, the compiled leg exit 1).

Closes **Gap R.142**, the wrong answer this cycle's own probe found: a tagged name rebound through a
road that is not a pair kept the tag, so `n = xs[0][0] * 2` then `n = [1, 2]` printed `2` — the heap
handle — and `n = 2.5` printed `0`. The same omission had been answering a rebound loop variable
`(null)` since ADR 0185 bound it with a tag (measured on the pre-cycle binary, so it is not this
cycle's doing; it is this cycle's rule).

Files **Gap R.143** (a pair-bound name read back as one static number: `print(n + 1)`, `-n`, `abs(n)`,
`if n:`, a `while` head, an f-string, `str(n)`, `n += 1`), **Gap R.144** (a tuple-unpacking target does
not take the pair road), and **Gap R.145** (a text binding's interned-string status survives a container
rebinding — `n = "text"` then `n = [1, 2]` prints `text`; the same latest-binding rule, a different
status, and Gap Q.1's cousin).

Retires the probe row for `programs/probe_arith_result_bound_to_a_name`, which moves from
`conformanceProbes` to `conformanceStandalone`: the program now prints ten lines that all three engines
agree on. Files `programs/probe_pair_bound_name_as_a_number` (Gap R.143) and
`programs/probe_pair_from_a_tuple_unpack` (Gap R.144), each pinned per leg.

## Context

ADR 0265 opened a door. `xs = []` / `xs.append([7, 8])` / `print(xs[0][0] + 1)` asks a question the
compiler cannot answer — *what kind of number is in this slot* — and the door asks the object: both
operands become `(payload, tag)` pairs, `@rt_num_arith` does the sum in the one word that holds both
families, and the answer comes back with its own kind so `rt_print_mixed_value` knows whether to write
`8` or `15.0`.

The door was opened in one position. The same expression one statement earlier asked a different road:

```
xs = []
xs.append([7, 8])
print(xs[0][0] * 2)   # 14 on all three engines — the pair road
n  = xs[0][0] * 2
print(n)              # CPython 14, --interp 14, --aot exit 1:
                      #   "index cannot reach into xs's slots: the name was rebound, mutated, or
                      #    handed to code this pass cannot see…"
```

That message is honest and it is exit 1 on a program the reference runs, which ADR 0166 counts as the
compiler spending its own error class on a working program. It is also a half-lift of exactly the kind
this ledger keeps writing down: the same `*` node, the same operands, the same object. `print(e)` had
been given a way to ask the object; `n = e` had not been given one, and nothing about the language
explains why the second is harder.

The two roads differed in one word. `value()`'s numeric road returns a single `i32` — "give me the
number" — and refuses when the number's kind lives in the object. `taggedArithPair` returns two strings,
a payload and a tag, and every consumer of a tagged value in this backend already reads that shape: the
printer (`rt_print_mixed_value`), the equality door (`rt_payload_eq`), the ordering door, the container
writers (`rt_append_tagged`, `rt_set_add_tagged`, `rt_dict_put_tagged`), and the two bindings that
already existed — a loop variable over a mixed container (ADR 0185) and a slot read bound by
`taggedContainerRead` (ADR 0241). Those two bind `%_n` and `%_n_tag`, and `print(n)` already dispatches
on the pair. An arithmetic answer is no different in kind from a slot read: it is a value whose kind the
object decided.

## Decision

**Take the pair road in the binding, and make the binding obey the retraction rule the pair implies.**

1. **The assignment asks the same door the print dispatch asks.** In the `AssignStmt`/`*Name` path, after
   the container and comprehension roads and beside the existing tagged-read bindings: if
   `g.arithWouldRefuse(n.Value)` — the ordinary numeric road would refuse this expression, the same
   question the print road asks — then try `g.taggedArithPair` and, when it answers,
   `g.bindTaggedVar(name, payload, tag)`. The gate is unchanged from ADR 0265 and is *not* loosened here:
   a text anywhere in any container keeps `+` and `*` refused (the reference answers `"a" + "b"` and
   `[1] * 2`, and this backend builds neither from a slot — Gap R.82), the operands still have to be
   pairable, and `/` keeps its own door (ADR 0253). Because the road opens only where the ordinary road
   refused, no program that was answered before is rerouted through it — checked by re-running the whole
   conformance corpus in the same commit.
2. **A pair is two words, so both are written.** `bindTaggedVar` is the one door: free the old heap slot,
   clear the stale roots, allocate `%_n` *and* `%_n_tag`, store both, mark both bound for ADR 0228's
   definite-assignment graph, and record the variable as tagged. A binding that wrote the payload alone
   is the bug ADR 0187 closed for container slots, and an IR row asserts both stores appear.
3. **A binding that is not a pair retires the tag.** `forgetTaggedBinding(name)` deletes the record, and
   it is called by every other road that binds the name — the plain-number store, the float store, the
   list/set/dict literals, the runtime comprehension, the folded literal, the lambda, the union store,
   and the module-global store. This is ADR 0172's rule for the `None` and bool statuses ("the
   variable's *latest* assignment decides how print, truthiness and equality lower, and any other
   assignment clears the status"), which ADR 0185/ADR 0187 carried to the tagged variable but never
   enforced at the early-returning bindings. Without it the feature is not merely incomplete, it is a
   wrong-answer generator: `n = xs[0][0] * 2` then `n = [1, 2]` printed `2`, and `n = 2.5` printed `0`.
4. **A refusal names where its tag came from.** `taggedVarErr` reads the `taggedOrigin` record: a name the
   loop bound says so, and a name this statement bound says "*n* holds the answer of arithmetic over a
   slot the program built at run time… using it as one static number needs the same pair to reach this
   position (roadmap L11.1, Gap R.143)". Gap R.38's rule is that a refusal may not claim something false
   about either backend, and the sentence a tagged variable used to get blamed every refusal on a loop
   that was never written.
5. **The raise stays the statement's, not the helper's.** The arithmetic the binding performs fills a
   buffer and returns a status; the emitted code does the store-and-branch every trap in this language
   uses, so `except TypeError:` catches `bad operand type for unary -: 'str'` and `except
   OverflowError:` catches the guard for an answer past the compiled `int` word (ADR 0228, ADR 0264's
   guard, ADR 0265's door). Nothing about the binding changes the class or the wording.

## Measurement

The reference leg is CPython 3.12.3; the compiled leg is Ubuntu LLVM 20.1.2 (`llc-20`, `opt-20`). Every
parity row runs the same source through `python3`, `gustyc --file … --interp` and `gustyc --file …
--aot`; exit 2 fails the row by itself (ADR 0166).

| Shape | Before | After |
|---|---|---|
| `n = xs[0][0] * 2` / `print(n)` | aot exit 1 | `14` on all three legs |
| `n = xs[0][0] + 1`, `n = xs[0][1] - 3`, `n = -xs[0][0]` | aot exit 1 | `8`, `5`, `-7` |
| `n = xs[0][0] + xs[0][1]`, `n = xs[0][0] * xs[0][1]` | aot exit 1 | `15`, `56` |
| `n = xs[0][0] * 2` over a float slot | aot exit 1 | `15.0` (the pair says float, the printer follows) |
| `n = xs[0][0] * 2.5` | aot exit 1 (`Gap R.82`'s message) | `17.5` |
| `d["k"] = 40` / `n = d["k"] + 2` | aot exit 0 already (the tracked dict kinds) | `42`, unchanged |
| `n = -xs[0]` over a text slot | both engines raise | raise on both, now *through* `@rt_num_arith`, catchable |
| `n = xs[0][0] * 2` then `n = [1, 2]` | aot exit 1 (the binding refused) | `[1, 2]` on all three legs |
| `n = xs[0][0] * 2` then `n = 2.5` | — | `2.5` (before the rule landed: `0`) |
| `for v in [1.5, "a"]` then `v = [1, 2]` | **aot `(null)`**, exit 0 | `[1, 2]` on all three legs |
| `print(n + 1)`, `-n`, `abs(n)`, `if n:`, `f"{n}"`, `str(n)`, `n += 1` | exit 1 | still exit 1, message names the missing pair (Gap R.143) |
| `a, b = xs[0][0] + 1, xs[0][1] + 2` | exit 1 | still exit 1 (Gap R.144) |

Counts, all measured in the same run of `go test -tags=llvm20 ./...` (green).
`pkg/lang/pair_binding_test.go` — 8 tests, 27 table rows: 10 parity rows run through both `evalRun` and
`Compile`+`llc` (the row's own shape, `+ - *`, both operands from slots, float and double-literal cases, a
bool slot, a dict slot by key, three levels, a binding between two prints, a binding inside an `if` arm), the
two-slot IR shape (both stores named, and its mirror row proving an ordinary literal binding pulls neither
`@rt_num_arith` nor `_v_tag` into the module), 9 retirement rows (plain number, float, **list literal**, set,
dict, comprehension, a second tagged binding replacing the first, an integer rebinding, and the loop-variable
case ADR 0185 left open), 8 refusal rows, the gate asked one statement later than the print road asks it, and
the raise leaving through the statement's `rt_raise_buffer`.
`integration/pair_binding_test.go` — 8 tests, 41 rows at this commit (ADR 0268 has since promoted the
number-position refusals in it to parity rows, so the file now stands at 9 tests and the counts below are the
ones this cycle measured): 17 three-engine parity rows, 8 retirement rows, 2
traps × both engines, 2 catchable-by-`except` rows, 10 refusal rows, the filed `int`-word overflow row, the
Gap R.145 row, and the promoted corpus program. The conformance corpus stands at **102/139 parity-asserted,
0 failures, 0 oracle drift**, oracle 87 `match` / 31 `debt` / 21 `not_applicable` over 139 rows (37 recorded),
148 programs in `integration/programs`.

The corpus program `programs/probe_arith_result_bound_to_a_name.gy` grew from one line of output to ten
(`14 8 -7 5 15 15.0 42 3 [1, 2] {5, 6}`), and the ledger now says `match` about it — which the harness
checks by failing when the legs move in either direction (ADR 0186).

## The bug this cycle found in itself

The first version of this ADR's road shipped in the working tree with `bindTaggedVar` and nothing else,
and the conformance corpus caught the rest. Rebinding a pair-bound name — to a list, to a float, to a
comprehension — left `taggedVars[name]` set, because each of those paths returns early and none of them
had ever needed to clear a record that did not exist for them when they were written. `print(n)` then
followed the tag the arithmetic left behind and printed the *payload*: `2` for a two-element list (its
heap handle), `0` for a double stored into a word sized for an int. Exit 0, digits on the screen, and the
interpreter and CPython agreeing beside it — the exact shape ADR 0166 says no backend may be trusted
about, arriving because a status outlived its binding.

The loop variable had been in the same state since ADR 0185 bound it: `for v in [1.5, "a"]: print(v)`
followed by `v = [1, 2]` printed `(null)`. That one was never on the roadmap. It is now, as Gap R.142,
and it is fixed by the same three lines, because "the latest binding decides" is one rule and not two.

## Agentic rationale

An agent compiling a program does not read this ADR; it reads `--json` and the exit code. Three things
follow from the shape above.

* **The refusal is machine-legible and names its own half.** `n holds the answer of arithmetic over a
  slot the program built at run time … using it as one static number needs the same pair to reach this
  position (roadmap L11.1, Gap R.143)` tells a caller which statement, which value and which missing
  door, and the row cites the item that owns the fix — so an agent can decide between rewriting the
  program and reporting the gap, instead of guessing from `index cannot reach into xs's slots`.
* **Exit classes stay clean.** A program the reference runs either answers (exit 0), raises (exit 3,
  catchable by `except TypeError:`/`except OverflowError:`), or is declined (exit 1). Exit 2 — the
  contract's "the compiler is broken" code — appears in no row of this commit, and both new test files
  fail on sight of it.
* **The tracker's ratchet did the reviewing.** `probe_arith_result_bound_to_a_name` was a `debt` row
  pinned with `aot: Missing, "index cannot reach into xs's slots"`. Landing the road made the harness
  fail it with *"oracle debt is paid"*, which is how the promotion (probe → standalone, and the two new
  probe rows with per-leg pins) got written down rather than remembered.

## Alternatives rejected

* **Answer the binding statically with the int word and print it through `%d`.** Cheapest in lines, and
  it prints `15` as `15.0`'s box handle for the float case — the failure ADR 0187 exists to prevent. A
  payload without its tag is not a number; it is a number wearing another object's bits.
* **Leave the binding refusing, since the print position already answers.** A half-lift is a
  divergence with extra steps: the reference and the interpreted leg answer `14`, the compiled leg spends
  exit 1 on a program nobody asked a question about. Roadmap Gap R.138 was filed as a priority-1 class
  precisely for that.
* **Widen the gate so `+` and `*` open for any container.** Rejected for ADR 0265's reason, restated here
  because the binding is the tempting place to "just do it": the reference *answers* `"a" + "b"` and
  `[1] * 2`, and this backend builds neither from a slot (Gap R.82), so an open door would raise where
  CPython returns a joined text. The gate is unchanged, and `TestThePairGateIsTheSameDoorInTheBindingAsInThePrint`
  asks it one statement later.
* **Open the plain-number positions in the same commit** (`print(n + 1)`, `-n`, `if n:`). `arithOperandPair`
  can already read a tagged name's two slots, so it is tempting; but `slotArithmeticIsProven` takes the
  `+`/`*` gate from the container's literal kind tree, and a name bound by arithmetic has no entry there.
  Answering would mean *assuming* the slots hold numbers, which is the assumption the gate exists to
  refuse. Filed as Gap R.143 with its ten refusal rows so the day it opens, the tests fail — which is what
ADR 0268 did: those rows are parity rows now.
* **Clear the record at the top of the assignment instead of at each store.** One call, and it breaks
  `n = n + 1`: the right-hand side reads the name *before* the binding replaces it, and clearing early
  makes that read the payload alone — a silently wrong number instead of a refusal. The rule belongs where
  the new value is written.
* **Say "comes from a loop over a mixed list" for every tagged name.** It is the sentence ADR 0185 wrote
  for the loop, and for a name this statement bound it is simply false; Gap R.38 exists to keep refusals
  honest about what they describe.
* **Fix the interned-string status in the same pass.** `n = "text"` then `n = [1, 2]` printing `text` is
  the same latest-binding rule about a different status (`strVals`/`internedVars`), reproducible on the
  pre-cycle binary and not reachable from this road. Filed as Gap R.145 rather than bundled, so the
  commit stays one feature.

## Consequences

* L11.1's remaining list shrinks by one position and gains three named ones: the pair reaches *print*,
  the equality and ordering doors, the container writers, and now **the binding**. What it does not reach
  is a plain-number read of the bound name (Gap R.143), a tuple-unpacking target (Gap R.144), and a call
  argument (Gap R.139, pre-existing) — each refused in words, each pinned.
* `bindTaggedVar` is now the only door that binds a tagged variable and `forgetTaggedBinding` the only one
  that retires it. The mixed-list and mixed-dict read paths, which each carried their own eleven-line copy of
  the bind body, now call the door; the next cycle that adds a tagged binding has one place to add it.
* A float answer is still a handle on a `@float_box`, and the tagged binding does not register the name as
  a GC root — the same footing the loop-variable binding has had since ADR 0185. A GC stress row
  (`for i in range(120): ys.append([i, i])` between the binding and the print) is pinned in the unit file
  so a regression in either is measured, and precise rooting of tagged slots is L7.1/L7.2's.
* The compiled `int` word still owns the overflow guard at this door: `n = xs[0][0] * 1000000000` raises a
  catchable `OverflowError` naming L12.12 where the interpreted leg answers `7000000000`, pinned as a
  filed divergence rather than averaged into a "parity" claim.
* Nothing new is added to the CLI, and nothing about the machine contract moves: the same flags, the same
  JSON diagnostics, the same exit classes. What moved is which programs get which class — one fewer exit 1
  family, no new exit 2, and two new probe rows so the remaining refusals cannot quietly become answers.
