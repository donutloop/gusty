# ADR 0261 — the operator that chooses an operand renders what the chosen candidate is

Date: 2026-10-03 · Status: Accepted · Roadmap: closes Gap R.117; files Gaps R.124 and R.125 ·
Related: ADR 0256 (a fold returns the candidate it chose), ADR 0257 (a verdict is printed from the
expression that made it), ADR 0259 (a container slot may say `bool`; the numeric family int/bool/float),
ADR 0260 (the dict's own key rule — the same sweep, and the same “one door“ shape), ADR 0186 (the
three-leg oracle and the promotion rule), ADR 0166 (a refusal is a divergence, never a pass)

## Context

`print(max([True, 0]))` printed `1` compiled and `True` in the interpreter, with CPython on the
interpreter's side. `print(min([False, 1]))` printed `0` compiled and `False` in CPython. Both engines
exit 0; the compiled answer is a number, and a number is what a verdict looks like when nothing renders
it. ADR 0259's sweep measured these two lines while separating the bool-in-a-container rows and filed
them as Gap R.117 rather than absorbing them.

Reading them beside each other says what is really missing (measured with `python3`, `--interp`,
`--aot`):

| program | CPython | `--interp` | `--aot` (before) |
|---|---|---|---|
| `print(max([True, 0]))` | `True` | `True` | **`1`** |
| `print(min([False, 1]))` | `False` | `False` | **`0`** |
| `print(max(True, 0))` (varargs) | `True` | **`1`** | **`1`** |
| `print(max([True, 1.5]))` | `1.5` | `1.5` | **`1`** |
| `print(str(max([True, 0])))` | `True` | `True` | **`1`** |
| `print([max([True, 0])])` | `[True]` | `[True]` | **`[1]`** |
| `print(max([True, 1]))` / `max([1, True])` | `True` / `1` | same | **`1` / `1`** |
| `ys = [True, 1]` · `print(ys[0] and ys[1])` | `1` | `1` | `1` |

Three things about that table.

The last row is the constraint. `and` also hands back an operand, and CPython prints its **number** —
`1`, not `True`. Any fix that made every chosen operand print like a verdict would break a line that is
conformant today, which is why the probe that reproduced this kept its rows together.

The tie rows are the acid test. `max([True, 1])` is `True` and `max([1, True])` is `1`, because the
comparison is strict and the first maximal candidate stays. No question about the *elements* — “are they
bools?“ — can produce that pair of answers. Only the chosen candidate can.

And `max([True, 1.5])` printing `1` is not a rendering bug at all: the fold that compares candidates
declined to see a verdict as a number, so the double underneath it was truncated. Two defects were
hiding in one line — the winner's kind, and the winner itself.

## Decision

**The candidate the comparison chose decides what the answer is, and it is asked once.**

1. **One candidate rule.** `minMaxCandidateValue` (in `boolvalue.go`, the file that already owns the
   shared AST question `IsBoolExpr`) reports the number a min/max comparison makes of a candidate and
   whether it arrived as a double: `IntLit`, `FloatLit`, `-` of either, and **`BoolLit` as the number it
   compares to** — 1 or 0 — which is ADR 0259's numeric family applied where it had not been applied.
   The compiled fold's `numericFoldElems` asks that function, with its own `g.foldableCandidate` hook
   for candidates that are not bare literals (a constant-folded name, an element a literal wrote into a
   slot — ADR 0243's “reading the slot and reading the expression that filled it are the same number“).
2. **One winner.** `numericWinner(vals, wantMin)` is the only place the comparison is written. It is
   strict, so a tie keeps the first candidate — CPython's rule, and the reason the two spellings of a
   tie disagree with each other. The float fold, the int fold, `minMaxReturnsFloat`, `minMaxFoldedWinner`
   and the verdict question all call it; the hand-written loops they each carried are gone, and
   `TestTheFoldAndTheVerdictQuestionChooseFromOneDoor` fails if a `vals[i] < vals[best]` reappears.
3. **The verdict question asks the winner.** `IsBoolExpr`'s `*Call` arm consults the builtins that
   *choose* — `min` and `max` — by picking the winner with the same two helpers and asking whether that
   candidate is a verdict. Everything the predicate already drives follows for free: `print`, `str()`,
   `repr()`, an f-string, the container element tag, `--json`'s `"type"`. Nothing was added to any of
   those doors.
4. **The interpreter boxes the candidates it is given.** `min(a, b, …)` evaluates its argument
   expressions, and until now an argument written `True` arrived as the immediate `1`. It now enters
   through `slotVal`, the same door a list literal uses: a candidate written as a verdict is boxed, and
   every numeric question about it still unboxes (ADR 0259). That is what `print(max(True, 0))` was
   waiting for — the interpreter, not just the compiled path, was wrong on that line.
5. **The fold's TypeError sentence names the candidate's kind.** The static-order trap compares bools as
   numbers and prints `bool`: `max(["a", True])` raises `'>' not supported between instances of 'bool'
   and 'str'` on both engines, which is what the interpreter's `compareOrder` and ADR 0259's element tag
   already said for the same operand.
6. **The same rule one level up, where it is decidable.** `x if <constant> else y` renders the arm the
   test selects (`constantTruth` covers the values the source wrote: a verdict, a number, `None`, `""`,
   an empty container), so `print(False if 1 else 2)` is `False` and `print(1 if 0 else False)` is
   `False`. Where the test is not a constant, ADR 0257's conservative rule stands — both arms must agree
   — and that half is filed as Gap R.125 rather than guessed.

The interpreter needed one boxing site; the compiled backend needed no new rendering. That is the point:
the kind of the answer was always a fact about the chosen candidate, and both engines were asking the
wrong question.

## Consequences

Fixed, measured before and after against `python3`: the 12 lines of
`programs/probe_bool_chosen_by_an_operator.gy` print CPython's answer on both backends, so the program
left the debt ledger for the parity corpus (ADR 0186's promotion rule) and the CLI exit-class contract
row that expected exit 6 for it was deleted rather than re-pinned. `programs/probe_bool_through_a_call.gy`
stays debt — a verdict crossing a call boundary needs a parameter tag, which this does not touch
(Gap R.111).

The float rows came with it: `max([True, 1.5])` and `max([0.0, True])` print `1.5`/`True` instead of `1`,
because the fold no longer declines a verdict as a candidate.

Two shapes measured and **not** fixed, filed with per-leg pins (ADR 0166's discipline — a probe that
cannot match is recorded, not skipped):

* **Gap R.124** — `i = 0` / `print(max([True, i]))`. A candidate the source does not write as a number
  settles nothing statically; the run-time `select` keeps the payload and nothing beside it says it came
  from `True`, so the compiled leg prints `1` while the interpreter and CPython print `True`. The
  literal-backed spelling in the same program (`max([True, xs[0]])`) is parity, which is what makes the
  boundary visible. Owner L11.1 with Gaps R.107–R.110: the kind has to travel with the value.
* **Gap R.125** — `print(True if xs else 2)`, `print(True if c else 2)`. When the test is not a constant,
  neither arm is known to run, and the conservative rule prints the number: the compiled leg is wrong on
  all four probe lines, the interpreter on two, CPython right on all four. Two engines agreeing is not
  evidence; this row is where the same lesson applies again.

`--json` is the machine path, and it followed without a schema change: `--json --eval 'max([True, 0])'`
reports `"type": "bool"`, because the renderer, the tag and the JSON report all ask one predicate.

## Agentic rationale

An agent writing `max([...])` over a list that mixes verdicts and numbers cannot discover from an exit
code that the answer it prints is not the answer CPython prints — exit 0, plausible digits, no diagnostic.
What the agent can do is ask the toolchain what a value is: `--json` now reports `bool` for the chosen
candidate, and the conformance ledger carries a `match` row (`programs/probe_bool_chosen_by_an_operator.gy`)
whose lines include the two spellings of a tie and the `and` row that must stay a number.

The tripwires are the part that keeps that true in two years. `TestTheFoldAndTheVerdictQuestionChooseFromOneDoor`
fails if a fold re-writes the comparison by hand or stops asking the shared candidate rule — the failure
mode this family has had three times (`taggableMixedList`/`Set`/`Dict` in ADR 0259, the four dict builders
in ADR 0260, and four copies of the min/max winner loop here). The refusal rows assert their text, so the
day the tagged value word lets a slot's kind travel, the rows that pin `1` fail and point at the ledger
rows that have to move with them.

## Alternatives rejected

* **Ask the elements.** “If every element is a bool, print a verdict“ is the obvious rule and it is wrong
  twice: `max([True, 1])` is a verdict and `max([1, True])` is not, and a mixed list with a verdict winner
  (`max([True, 0.0])`) is a verdict too. The winner is the only thing that knows.
* **Make the fold return a tagged pair now.** Correct, and not this rung: it is L11.1's tagged value word
  (Gaps R.107–R.110), a layout change across every container operation. The static rule lands the answers
  the source can decide and pins the rest, which is also the honest way to find out how much the tag is
  actually worth.
* **Refuse every min/max whose winner is not statically known.** A refusal is better than a wrong answer,
  but here it would retire programs that print the right *value* today (`i = 0` / `max([True, i])` answers
  `1`, which is the right number and the wrong rendering). Those are pinned as Gap R.124 instead: the
  compiler says what it can say, and the row keeps the rest visible.
* **Promote every chosen operand to a verdict.** That breaks `ys[0] and ys[1]`, whose `1` is CPython's
  answer — the row that has not moved, and the reason the probe still carries it.
* **Give the renderer a bool tag at print time.** The print site does not know which candidate won either;
  moving the question there would put a second copy of the comparison beside the first.
* **Leave the interpreter's varargs path alone because “only the compiled leg was reported”.** The table
  above says otherwise: `print(max(True, 0))` was wrong on *both* engines, and the ADR that closes a row
  has to close it for both paths (AGENTS' two-backends rule).
