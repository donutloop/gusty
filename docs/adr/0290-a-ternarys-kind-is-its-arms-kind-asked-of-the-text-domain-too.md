# ADR 0290 — a ternary's kind is its arms' kind, asked of the text domain too

Date: 2026-07-06
Status: Accepted
Roadmap: closes **Gap R.173** (found by the 2026-07-06 surface sweep) and **pays Gap R.127's text half**
(recorded debt from ADR 0262). Container arms stay open as Gap R.128.
Depends on: ADR 0262 (a ternary's answer travels in a word its arms agree on), ADR 0224/0229 (a text is an
`@str_tab` index and every road asks one predicate), ADR 0257 (the front end reads the program, the runtime
renders), ADR 0261 (a promoted probe takes its exit-code pin with it), ADR 0234 (a container global in an
operand is this compiler's bug), ADR 0166 (exit codes).

## The measure

`python3` 3.12.3 is the oracle. The interpreter was right throughout; the compiled leg printed the intern
table's **position** at exit 0:

| program | CPython | `--interp` | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `print("y" if 1 else "n")` | `y` | `y` | `0` | `y` |
| `print("y" if 0 else "n")` | `n` | `n` | `2` | `n` |
| `x = 5` / `print("big" if x > 2 else "small")` | `big` | `big` | `0` | `big` |
| `x = 1` / same | `small` | `small` | `1` | `small` |
| `def f(x): return "even" if x % 2 == 0 else "odd"` / `print(f(4))`, `print(f(5))` | `even`,`odd` | ✓ | `0`,`1` | `even`,`odd` |
| `print(["y" if 1 else "n"])` | `['y']` | `['y']` | `[0]` | `['y']` |
| `x = 2` / `print(("big" if x > 1 else "small").upper())` | `BIG` | `BIG` | `` (empty!) | `BIG` |
| `print(7 if 1 else 9)` | `7` | `7` | `7` | `7` |
| `print(1 if 0 else 2.5)` | `2.5` | `2.5` | `2.5` | `2.5` |

`0` and `2` are not arbitrary: they are the slots `y` and `n` got in `@str_tab`. The empty output for the
`.upper()` row is the index handed to the text printer, which found no bytes to write. Every row was exit 0 —
no refusal, no crash, nothing an agent-driving harness would notice.

## The diagnosis

ADR 0262 (Gap R.102) established the rule — **a ternary hands back one of its arms, so the answer's kind and
word are facts about the arms** — and applied it to the numeric domain only. Four separate predicates
throughout the compiled backend ask "what kind is this expression?", and none of them had a `*CondExpr` arm:

| predicate | asked by | consequence of missing `CondExpr` |
| --- | --- | --- |
| `stringVal` | `print`'s constant-fold road | a constant-test ternary never folded to its running text |
| `exprIsString` | every operation path | the arms were not text, so the select was never emitted |
| `printsAsInternedStr` | the print formatter choice | the index printed instead of the text |
| `methodReturnsStr` | the `strFuncs` pre-scan | a caller of a ternary-returning function printed the index |

That is the real shape of "the print road never asked". And `printsAsInternedStr` had **no `*StrLit` case at
all** — it consulted `internedVars`, `strFuncs`, `strAttrs`, min/max and `str`/`repr`, but a literal text was
answered "not text". The ternary arm added on top of it would have silently failed for that reason alone.

## The decision

**Ask the arms, in all four places, with the same conservatism ADR 0262 uses.** A constant test settles which
arm runs, so the answer's kind is that arm's kind. A run-time test needs **both** arms to agree — a function
whose returns are `"yes"` and `0` is not a string-returning function, and registering it as one would print
`True`-style text for the numeric branch.

**The runtime-test pair gets a real `select`** (`ternaryI32`): both arms intern independently, then
`select i1 %c, i32 %then, i32 %els` picks the index. Legal because `rt_str_intern2` is idempotent — it returns
an existing slot — so both branches name the same index for the same text. The arms are evaluated **together**
before the join: interleaving one arm's lowering with the other's would let a branch skip the other's
interning. ADR 0262's constant-test rule still applies first, so `print("a" if 1 else shout())` does not run
`shout` — pinned by `TestATernaryEvaluatesOnlyTheArmItsTestChooses` on both engines.

**`printsAsInternedStr` grows its missing `*StrLit` case.** Correct on its own merits; found only because the
ternary arm exposed it.

**What stays refused.** A **container** arm (`print([1, 2] if c > 0 else [3])`) lowers a container to the
address of a compile-time global; putting it in a select operand reproduces Gap R.128's
`global variable reference must have pointer type`. And a text ternary in a **container slot** with a run-time
test still prints `[1]`: the slot holds the index and carries no tag saying it is text — the same missing tag
as `print(["abc".upper()])`. Both belong to L11.1's tagged value word. `TestAContainerArmTernaryIsNotAnsweredByTheTextRoad`
guards the first, and fails loudly if the new branch ever emits an invalid module;
`TestATextInAContainerSlotStillOwesItsTag` pins the second as still-owed rather than pretending it is fixed.

## Errors made in this cycle, recorded because each is a class

* **The first fix was in the wrong place.** I added the `CondExpr` arms to `exprIsString` and
  `printsAsInternedStr` and measured — output unchanged. The `print` road folds via `stringVal` *before*
  consulting either, so the constant-test case never reached them. Four predicates, one question: fixing
  three of them changes nothing. (This is ADR 0166's "one road" rule with the quantifier made explicit.)
* **Inserting a statement before a type switch is a syntax error.** I put a debug `if` between
  `switch v := e.(type) {` and its first `case` — Go requires the type switch's guard be the first statement.
  Four cascading syntax errors followed. Debug traces that need to log a `case` value belong *inside* the case.
* **A bad edit deleted a guard I did not mean to touch.** While adding a `*CondExpr` case to
  `methodReturnsStr` I passed a second edit that removed the `ReturnAnno != nil && Kind == KindString` check.
  Two edits touching one function should be one edit; caught by reading the diff, not by the tests.
* **I pinned a container-slot row I could not fix.** `print(["y" if c else "n"])` with a run-time test still
  prints `[1]` (verified byte-identical on the pre-cycle binary); a both-engines table row would have forced
  either a wrong pin or a fake fix. It moved to a named still-owed test instead.
* **`readFile` did not exist** in the integration package (three different call sites use `os.ReadFile`
  directly); `os.ReadFile` returns `[]byte`, which `cpythonPlainOut` does not take.

## Alternatives rejected

* **Give text a tag in the word.** Rejected: that is L11.1, and ADR 0171's rule is that the front end reads
  the program so the runtime does not have to guess.
* **Intern both arms eagerly at module level so the select can use constants.** Rejected: it would intern
  texts the program never reaches, and `@str_tab` insertion order would become observable through any row
  that prints an index — which is the bug class this ADR removes rather than entrenches.
* **Fold a run-time-test ternary to its then-arm.** Rejected: `x = 1` and `x = 5` are different programs;
  printing `big` for both is a wrong answer, not a partial one.
* **Refuse text arms until L11.1.** Rejected by measurement: the answer is available today from a `select`
  over an idempotent intern, and ADR 0166 ranks a needless refusal above an answer that is verifiably right.

## Agentic rationale

An `@str_tab` index is a plausible integer, exit 0, valid module — invisible to every structural check. The
observable is now the answer, pinned against `python3` in a registered probe, a 23-row unit table, and a
15-row CLI table. The refusals that remain are pinned to stay exit-1/exit-6 **in words** and never become a
malformed module, so a harness can rewrite rather than re-read IR. Exit codes and JSON output unchanged.

## Tests

* `pkg/lang/ternary_text_test.go` — 23 rows × both engines (constant and run-time tests, empty arms, list
  slot, concatenation, `.upper()`, two ternaries in one `print`, bound to a name, function return, and the
  int/double/verdict arms ADR 0262 and 0257 already owed); the arm-that-does-not-run side-effect test;
  `TestAFunctionReturningTextOnlyUnderARunTimeTestIsStillATextFunction` for the pre-scan; and the two
  still-owed pins.
* `integration/ternary_text_test.go` — 15 CLI rows × both engines with a live `python3` cross-check and an
  exit-2 ban, plus the promoted probe re-pinned against the reference.
* `integration/programs/probe_ternary_text_arms.gy` — **promoted from `OracleDebt` to standalone** (its
  `0\n2\n` pin and its exit-6 entry both left the ledger with it, ADR 0261). Matrix 156 rows,
  120 → 121 parity, 36 → 35 skipped, 105 → 106 `match`, 0 fail, 0 drift.
* Pre-cycle binary built from `HEAD` in a `git worktree` fails 7 of the new rows (`0`, `1`, `[0]`, empty); the
  166-file sweep moved only the intended probes.
