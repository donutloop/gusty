# The join seam is the concatenation: `str.join` asks the text question the `+` road already asks

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.403** (`str.join` over a list whose slots the compiler cannot read
as literals). Files three rows it measured on the way and did not buy: **Gap R.404** (the list the program
built), **Gap R.405** (the separator the program built), **Gap R.406** (a body that hands back a NAME beside an
arm of another kind). One commit, one feature: `pkg/lang/codegen_method.go`, `pkg/lang/codegen_text.go`,
`pkg/lang/rtstrings.go`, `pkg/lang/capability.go`, `pkg/lang/join_seam_test.go`, `integration/join_seam_test.go`.

## Context

Two programs, one method, two fates:

```
print("-".join(["a", "b"]))        →  a-b            answered, always
n = 3
print("-".join([f"a{n}", "z"]))    →  exit 1         join() list elements must be constant strings
```

The second is the one people write: you build the pieces and glue them. The first is a program nobody writes
unless the compiler forces them to. The road was a **fold** — read every element as a `*StringLit`, `strings.Join`
them at compile time, hand back the interned index — and because a fold can only read literals, a list with one
interpolation in it fell out of the road entirely. The sentence it printed blamed the program for a limitation
the backend did not have.

The backend has had a runtime text builder since **ADR 0229**: `@rt_str_cat(i32, i32) -> i32`, the door `s + t`
walks, interning its buffer into the same `@str_tab` every other text door reads. A join is that concatenation
with the separator between the neighbours — same value word (one interned index), same door, same order. So the
missing feature was never a runtime; it was a fold that had not been asked whether the door beside it was open.

Measuring the fix found three more shapes behind it, each answered at **exit 0 with wrong bytes** rather than
refused, and each one a *pre-scan* verdict rather than a codegen one. `str.join` was in the method fold's table
of text-answering methods (`textMethodAnswersText`) but not in the two shape predicates the pre-passes ask
(`isStringExpr`, `isStrUnder`) and not in the answer-kind census's vocabulary:

| program | CPython | this backend, first attempt |
|---|---|---|
| `def f(): n = 3` / `    return "-".join([f"a{n}", "z"])` / `print(f())` | `a3-z` | `6` — the `@str_tab` **index** of the text, printed as a number |
| `def f(n):` / `    s = "-".join([f"a{n}", "z"])` / `    return s` / `print(f(3))` | `a3-z` | `6`, then `10` |
| `def f(k):` / `    if k:` / `        return "-".join(["a", "b"])` / `    return 7` / `print(f(True))` / `print(f(False))` | `a-b`, `7` | `a-b`, `(null)` |

The first two are **Gap R.219**'s mechanism exactly — the caller holds one word and a name, and which printer
that word reaches is decided by a scan that never sees the body. The third is that row's remaining shape: the
arms are text and int, so **ADR 0368**'s gate should have carried the body and let the answer read its own tag,
but the census could not see a join arm as a text, so the body looked one-kind and kept one printer.

## Decision

1. **The seam is emitted as the concatenation it is.** For a list literal the fold cannot read end to end: ask
   each element through `strReg` — the question `+` asks, *is this a text, and where is its index* — and emit one
   `@rt_str_cat` per neighbour, left-leaning, in the order the source wrote, the separator interned once outside
   the chain. A one-element list never reaches the seam: its answer *is* its element's index. No new runtime entry
   point, and `TestTheJoinSeamIsBuiltByTheDoorTheConcatenationRoadAlreadyUses` pins both halves of that claim
   against the emitted module (the chain is there for a runtime slot; the module contains no `define` this row
   added; the constant join still emits no `@rt_str_cat` at all).

2. **The fold is untouched and still folds.** An all-constant list takes the same compile-time `strings.Join` it
   always took. The new road is the one the fold declines, not a replacement for it — which is why the guard
   asserts the constant program emits *no* concatenation calls, so a rewrite that quietly routes the fold through
   the runtime shows up as a failing test rather than a slower build.

3. **An element that is provably not a text is a raise, not a refusal.** `print("-".join([1, "z"]))` exits **3**
   carrying `TypeError: sequence item 0: expected str instance, int found` — the class, the sentence, and the
   position the reference counted, with `bool` spelled `bool` and `None` spelled `NoneType` — raised through the
   constant-trap door so the program's own `except TypeError:` catches it (`TestTheJoinTrapIsCatchableByTheProgram`).
   An element whose kind the compiler *cannot* read is refused in words at exit 1: interning somebody's `repr`
   where the reference raises is a wrong answer at exit 0, and **ADR 0166**/**ADR 0273** settle which of those is
   cheaper to live with.

4. **Textness stays one question.** `joinShapeIsText` joins `isStringExpr`'s arms beside the formatting door and
   the repeat door (**ADR 0360**, **ADR 0365**), and `isStrUnder` answers `join` beside the case family. That is
   what lets the caller print a returned join as text, the argument census let a join cross into a parameter
   (`show("-".join([f"a{3}", "z"]))` — refused before), the print road print it, and `answerArmOf` classify a join
   arm as `answerWordStr` so a body with a join arm and a number arm is *carried* by the answer plan and prints
   both arms right, which is **ADR 0368**'s design rather than a new one. The receiver must be a text the shape can
   see: `s.join([...])` with `s` a name is **Gap R.405**, still refused, and the shape predicate says no rather
   than guessing.

5. **A body is judged after its own bindings are read.** `scanStringBindings` now walks a `def`'s body *before*
   asking whether the function hands back a text. `s = "-".join([...])` / `return s` is judged from the notebook
   its own body fills; asking first meant the local name was not yet in `internedVars` and the caller printed the
   intern index. A `def` written later already saw those names either way, so the only verdict that changes is the
   one the body itself earns.

## Machine path

- The refusal the row leaves standing — the list the program built — keeps its capability code
  `lower.unsupported.join-needs-constant`, and its advice was rewritten to say what is now answered
  (`"".join([f"a{n}", "z"])` is answered, runtime slots and all) and what is owed (`Gap R.404`: a list bound to a
  NAME is the run-time walk of the object). `Rows` now names `Gap R.404`, `Gap I.2`, `L11.1`. The stale marker
  `join() list elements must be constant strings` is gone with the sentence it matched, so an agent that greps for
  a refusal this backend no longer issues gets nothing rather than a false positive.
- The raise family is a **trap**, not a refusal: exit 3, the reference's class and sentence on stderr, and no
  capability code, because nothing was declined — the program asked a question the reference answers by raising.
  `TestTheJoinSeamKeepsTheCapabilityRegistryHonest` asserts the refusals that remain still resolve to a code.
- `--emit-llvm` on a runtime-slot join shows the chain, and the IR guard reads it, so an agent can tell a folded
  join from a built one without running anything.

## IR implications

- One `@rt_str_cat(i32, i32) -> i32` call per neighbour pair, in source order, chained left-associatively; the
  separator interned once before the chain. Every operand is an `i32` `@str_tab` index, the same word the `+` road
  passes, so nothing new is declared: **the module gains no `define` and no `@rt_str_join`** — asserted by
  `TestTheJoinSeamIsBuiltByTheDoorTheConcatenationRoadAlreadyUses`, which fails if this row grows a door.
- The trap path emits the constant-trap call with the class and the counted sentence, at the element's own span,
  on the road `except TypeError:` already reads.
- The verdicts the pre-passes take changed, not the IR shape: `strFuncs` gains `def`s whose return is a join, and
  `answerKindPlan` gains bodies with a join arm. Both are the mechanism **ADR 0368** already owns; the change is
  that a join now answers the text question they ask.

## Alternatives rejected

- **A `@rt_str_join` runtime entry point.** It would duplicate a builder the artifact already has, add a runtime
  symbol for an agent to disambiguate from the one that matters, and buy nothing: the chain of `@rt_str_cat` calls
  is the join. Rejected for the reason **ADR 0229**'s rule exists — one door per construction.
- **Keeping the refusal** because "constant list of constant strings" was the historical contract. The sentence
  described a limitation this backend does not have; **ADR 0166**'s honest-refusal rule is about exit 1 where the
  answer would be wrong, not about refusing an answer the backend can build.
- **Refusing the known non-text elements instead of raising.** Exit 1 with prose where CPython raises a catchable
  `TypeError` moves a runtime event into compile time and makes the program's own `except TypeError:` untestable.
  (Measured: the fold-era road did exactly this, at exit 1, for `[1, "z"]`.)
- **Intercepting at the print door** — special-casing a join operand where the answer is printed — the same
  per-node interception **ADR 0407** measured and rejected: it answers recognised positions and leaves every other
  position to guess. The pre-scan verdict is one fact, taken once, program-wide.
- **Letting the compiled leg write the record** for the programs still owed. `tools/recmerge` **refused** them
  (**ADR 0309**), which is how **Gap R.404** and **Gap R.405** became rows instead of quiet passes, and why
  **Gap R.406** is pinned with `(null)` in its expectation rather than deleted.

## Measurements (taken, not remembered)

The family is the 36 unique programs these two test files carry (24 answered, 7 traps, 3 refused, 2 wrong), each
asked of the pinned CPython, with a build of the parent commit `72d1755` beside a build of this one:

| | parent `72d1755` | this commit |
|---|---|---|
| agree with the reference (bytes + exit, traps by class and sentence) | **3** | **31** |
| newly answering | — | **28** |
| regressed | — | **0** |
| owed | — | **5** (Gap R.404, Gap R.405 and the unnamed-kind slot refusals; Gap R.406 × 2 wrong answers) |

The three that agreed before are the pure-fold controls (`["a","b"]`, `[]`, `["a"]`): every program with a runtime
slot was refused by the parent, which is the row this ADR closes.

Wrong-answer bytes pinned rather than fixed, all measured against the parent build to prove who owns them:

- `def f(k):` / `    if k:` / `        s = "-".join(["a", "b"])` / `    return s` / `return 7` → `a-b`, `(null)`
  for CPython's `a-b`, `7`.
- the same body spelled `s = "ab".upper()` → `AB`, `(null)` **on the parent build too**, which is the control that
  files **Gap R.406** against the census and not against this row.

Record leg: `tools/recmerge` wrote **33** entries for this family from CPython (record `8851` → `8884`) and refused
the five the compiled leg has no answer for. Both witness legs are walked per program in
`TestTheJoinSeamAnswersWhatTheFoldRefused` (record stdout) and `TestTheJoinSeamFamilyAnswersLikeTheReference`
(reference stdout), and no expectation in either file was written by this backend.
