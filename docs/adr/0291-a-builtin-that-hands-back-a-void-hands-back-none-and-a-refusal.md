# ADR 0291 — a builtin that hands back a void hands back None, and a refusal calls a dict a dict

Date: 2026-07-06
Status: Accepted
Roadmap: closes **Gap R.174** (found by the 2026-07-06 surface sweep). Adjacent to Gap R.171 (a void leaving
a function) and owned in the same place: L11.1's tagged value word.
Depends on: ADR 0172 (the front end renders a void), ADR 0215 (the reference's own sentence), Gap R.38 (a
refusal describes the program the reader is holding), ADR 0229 (one predicate per question), ADR 0234 (a
container global in a word is our bug), ADR 0166 (exit codes).

## The measure

`python3` 3.12.3 is the oracle. The probe's six lines, on the pre-cycle binary:

| program | CPython | `--interp` before | `--aot` before | both after |
| --- | --- | --- | --- | --- |
| `print({"a": 1}.get("a"))` | `1` | `1` | `1` | `1` |
| `print({"a": 1}.get("z"))` | `None` | **`0`** | **refused: `get: key not found and no default`** | `None` |
| `print({"a": 1}.get("z", 42))` | `42` | `42` | `42` | `42` |
| `print({1: "x"}.get(9))` | `None` | **`0`** | **refused** | `None` |
| `v = …get("z")` / `print(v == None)` | `True` | **`False`** | **refused** | `True` |
| `print(v == 0)` | `False` | **`True`** | **refused** | `False` |

Four of six wrong on the interpreter — including the two equality questions, so a program could not even
*test* for the missing key. And a third defect, only visible in the refusal:

```
d = {"a": 1}; print(d.keys())
  → gustyc: jit: codegen: string method keys on non-constant string
```

A dict, called a string, in a message quoted back to the author.

## The diagnosis

**The void was a word.** `callDictMethod`'s `get` case ended `return 0, nil` for a missing key with no
default: the bare word zero, not the interpreter's `noneVal`. Gap R.171 caught the identical shape leaving a
*function body*; this is the same representation arriving from a *builtin*, and the loop had not enumerated
the other producers. Any place the language means "there is no value" and writes `0` will print `0` and
compare equal to `0`.

**The compiled fold refused a fact it already knew.** The dict-literal `get` road walks the literal's keys;
finding none, it refused — even though "no matching key" is precisely the answer the reference gives. A
refusal is owed when the compiler cannot see the answer, not when the answer is one it dislikes.

**One road served two receiver kinds.** The container-method road is entered when `stringVal(attr.Obj)` fails.
A name bound to a dict fails that test for the same reason a runtime string does, so `d.keys()` fell through
to a message about non-constant strings. ADR 0229's rule — one predicate per question — applies to
diagnostics too: the road asked no predicate before speaking.

## The decision

**The void comes from the road that produces it.** `return e.noneVal, nil` in the interpreter, and
`g.value(b, &NoneLit{})` in the compiled fold — the arrangement ADR 0172 already sets for a void *return*,
reached from a builtin. On the compiled side, `isNoneExpr` grows a `Call` arm so print chooses `rt_print_none`
over `%d`, and it asks `g.dictFoldMisses` — **the same predicate the fold itself used** — so the two roads
cannot disagree about whether the key was there. A key the fold cannot evaluate is *unknown*, not absent, and
the answer is "not None": the conservatism that keeps a guess from becoming a printed value.

**The refusal asks the records before it speaks.** `nameHoldsContainer` consults exactly the tables the
iteration, subscript and length roads read (`runtimeDicts`, `mixedDicts`, `runtimeSets`, `mixedSets`,
`listVars`, `mixedLists`, and the four static-literal tables), so a diagnostic cannot contradict the codegen
beside it. A container receiver is named, quoted, and told which representation it waits for; anything else is
a *receiver* that is not a text the compiler can read — no longer a "non-constant string" whatever it is.

**What stays refused, and why.** A **name** bound to a container: `d.get("a")`, `d.keys()` over a module dict
are answered by the interpreter and declined by the compiled leg — a name's slots belong to the runtime, and
answering through a word is the wrong-number class ADR 0166 counts as our bug. And a dict whose **answer is a
text** still prints `0` (`print({1: "x"}.get(1))`), the intern table's position in an untagged word — pinned
as still-owed rather than claimed, and byte-identical to the pre-cycle binary. Both belong to L11.1's tagged
value word; `TestADictAnswerThatIsATextStillOwesItsWord` logs when the compiled leg starts answering them so
the rows get promoted.

## Errors made in this cycle, recorded

* **Three table rows I could not fix.** I first wrote int-keyed-value, text-default and None-in-a-list into
  the both-engines table. All three are the tagged-word wall, not this cycle's fix. A both-engines table
  forces either a wrong pin or a fake fix — measured against the pre-cycle binary and moved to a named
  still-owed test.
* **Two existing pins broke, and both broke *correctly*.** `TestGenDictGet` asserted
  `{"a": 1}.get("b") == 0` via a helper that reads a raw word: it had pinned the void as the number. It now
  asks the reference's question (`v == None`), because pinning through the buggy representation is how the
  bug stayed legal. `TestThePredicateRefusalIsStillHonest` matched the literal phrase "string method", which
  this ADR renames — the test was checking a wording, not a contract, so it now matches the stem.
* **A refusal that duplicated its own prefix** (`codegen: codegen: …`) — caught by printing the message rather
  than reading the format string, the same lesson as cycle 14's leaked-module refusals.

## Alternatives rejected

* **Give a void a tag so it prints itself.** Rejected for this rung: that is L11.1, and ADR 0171's rule is
  that the front end reads the program so the runtime does not have to guess. `isNoneExpr` is the front end.
* **Return `0` and teach `print` that `0` means None here.** Rejected twice over: `0` is a legitimate dict
  value, and `print(v == 0)` would stay wrong — the probe's sixth line exists to catch exactly this.
* **Answer container methods over names via a runtime table.** Rejected: it is a feature, not a fix, and it
  would need the tagged word to return texts. Refusing in words is the deliverable until then.
* **Keep one refusal sentence for all non-foldable receivers.** Rejected by Gap R.38, for the third time: a
  message that misdescribes the program is a second defect, not a consolation.

## Agentic rationale

`0` for a missing key is invisible to a harness that only checks exit codes, and the compiled leg's
`get: key not found and no default` gave no hint that the interpreter disagreed. Both are now pinned against
`python3` in a registered probe (four of six lines previously wrong), a 12-row unit table and a 10-row CLI
table. The refusals that remain name the receiver and the missing representation, stay exit 1, and never leak
a module llc rejects — so a harness can decide "index the dict instead" rather than re-reading IR. Exit codes
and JSON output unchanged.

## Tests

* `pkg/lang/dict_get_none_test.go` — 12 rows × both engines (present/absent/empty/int keys, `== None`,
  `== 0`, if-test truthiness, explicit default), the container-name refusals, the receiver-wording split, and
  the still-owed text-answer rows.
* `integration/dict_get_none_test.go` — 10 CLI rows × both engines with a live `python3` cross-check and an
  exit-2 ban, plus the dict-is-not-a-string refusal check through the shipped binary.
* `integration/programs/probe_dict_get_answers_none.gy` — six lines, registered in `conformanceStandalone()`;
  the pre-cycle interpreter got **four** wrong.
* `pkg/lang/gen_test.go` — `TestGenDictGet`'s void row moved from "expected 0" to the reference's question.
* Suite green; 167-file sweep against the pre-cycle binary moved nothing but the new probe. Matrix 156 → 157
  rows, 121 → 122 parity, 106 → 107 `match`, 0 fail, 0 drift.
