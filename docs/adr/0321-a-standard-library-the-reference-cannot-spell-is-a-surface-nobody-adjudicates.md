# 0321. A standard library the reference cannot spell is a surface nobody adjudicates

Status: accepted. Roadmap: `L11.6` closed (numeric truth in the compiled backend — its last clause was this),
files `Gap R.206` (the other data modules answer only our spellings) and `Gap R.207` (`math.inf`, `math.nan`,
`math.floor` as a module member), measured after Gap R.204/R.205 (ADRs 0319/0320), continues ADR 0272 (a data
module's constant keeps the type the module declares), ADR 0186 (the promotion rule that makes a conformance row
a definition of done), ADR 0302/0308 (the two witness legs, and what each can be asked).

## Context

`import math` / `print(math.PI)` has printed `3.141592653589793` on every engine since ADR 0272. It is still,
today, a row the conformance ledger records as `not_applicable` — not because the answer is wrong but because
**the reference has no `PI`**. CPython's module has `pi`, `e` and `tau`. The program our probe runs is therefore
not a CPython program, the reference leg returns a traceback about a missing attribute, and the comparison had
lived ever since in a hand-written twin class inside `integration/module_const_test.go`:

```go
const moduleConstTwin = `class consts:
    PI = 3.141592653589793
    E = 2.718281828459045
    ...`
```

That twin is honest about the *value* — same number, same rendering rules — and it proves nothing whatever about
the *name*. A name is exactly where a Python-like language has to agree with the language it resembles, and it is
the one thing a twin cannot witness: the twin adopts our spelling rather than comparing it.

This is what ADR 0302's two legs mean in practice. The record leg can tell you what our engine answered for
`math.PI`; it cannot tell you whether `math.pi` should work, because the record was recorded from a program that
already chose the spelling. Only the reference leg can adjudicate a name, and only when the reference can say it.

The same argument applies to every data module: `string.DIGITS`, `collections.EMPTY_DICT`, `json.NULL`. The
`math` case was chosen first because L11.6 — the numeric-truth row — was open on precisely that constant, and its
Definition of done named `probe_math_const`.

## Decision

**The reference's spelling is the declaration; ours is the alias.**

* `stdlib/math.gy` declares `pi`, `e`, `tau` as the literals, and `PI = pi`, `E = e`, `TAU = tau` as aliases of
  them — one number, written once, two names. The fold treats `PI = pi` as a folded reference to the same
  expression, so there is no second constant to drift.
* The four constants the reference does not have (`PHI`, `SQRT2`, `LN2`, `LN10`) keep their documented
  upper-case spelling as canonical — they are this language's extension, no CPython program names them — and
  gain lower-case aliases so a reader reaching for the reference's style is not met by a refusal.
* `integration/programs/probe_math_constant_answers_the_references_spelling.gy` is written the reference's way
  and asks the questions L11.6 owned through the reference-spelled name: the negation, the product, a binding
  then a comparison, `round(…, 2)`, `//`, `%`, a container element, an f-string field. It is registered in
  `conformanceStandalone()`, so the harness runs CPython against it and the matrix row is asserted and `match` —
  a promotion, not a re-description.
* `probe_math_const.gy` (the upper-case program) **stays** `not_applicable` in the ledger with its reason
  intact. A row that says "the reference cannot run this because of the spelling" is true, and deleting it to make
  a tally look better would hide the reason the other row exists.
* The record leg is extended through `tools/recmerge`, which asks CPython first: the 13 reference-spelled sources
  are on the record only because the reference answered them and the compiled leg agreed byte for byte. The
  extension names are *not* on the record — CPython traps on `math.phi`, and a recorder that invented an entry for
  a name the reference has never heard would be writing the backend's answer into the witness.
* Three guards, not a prose claim: `TestTheTwoSpellingsOfAModuleConstantAreOneDeclaration` reads the module's text
  and requires the alias form (`PHI = phi`, not a copied literal); `TestTheRecordStillAnswersForTheNamesTheCorpusHolds`
  keeps every spelling on record, where a missing entry fails the case; and
  `TestTheMathProbeIsAssertedParityInTheMatrix` fails if the promoted row drifts back to `debt`.
* What is **not** answered stays a refusal in words, and is filed rather than smoothed: `math.inf`, `math.nan` and
  `math.floor` as a module member are `Gap R.207`; the other data modules' spellings are `Gap R.206`. The unit
  table pins today's refusals (including those two names) so nobody can quietly turn one into a plausible answer.

## Agentic rationale

An agent reading the conformance matrix sees `oracle: not_applicable` and reasonably concludes the surface is
uncompared. The fix had to produce a row the harness can *assert*, because the machine path is the one an agent
plans with: `conformance-matrix.json` is payload, `integration/programs/*.gy` is the corpus, and a hand-written
twin inside a `_test.go` file is neither. The twin stays (it is the value's own witness) but it is no longer the
only comparison.

Declaring the reference's name rather than renaming ours is also the agent-safe direction: every record entry,
every doc example and every program in the corpus already says `math.PI`, and a rename would have turned the
record leg into the seven `import math` entries it holds under the upper-case spellings (measured off the file:
`print(math.PI)`, `print(math.E)`, `print(math.PI * 2)`, `print(math.SQRT2 * math.SQRT2)`, `x or math.PI`,
`x and math.E` and the `isFloat` snippet) with no answer — ADR 0302's "a missing entry fails the case" turns a gratuitous rename
into a suite-wide failure, which is the right way to discover you broke an interface and the wrong way to plan one.

## Consequences

* `L11.6` closes. Its last un-struck clause was the stdlib constant; the numeric battery beside it was measured
  against CPython on the compiled leg before the row was flipped (`-7 // 2` → `-4`, `-3.5 % 2.0` → `0.5`,
  `x /= 2` → `4.0`, `t += 1.5` → `1.5`, `dbl(0.1)` → `0.2`, `bump(1.5)` → `2.5`, `addf(1)` → `2.5`,
  `round(2.5)` → `2`, `round(2.675, 2)` → `2.67`, `floor(2.7)` → `2`, `ceil(-2.2)` → `-2`).
* The artifact table and the roadmap Snapshot moved with the record: 5,948 → 5,961 entries, 185 → 186 programs,
  174 → 175 matrix rows, 121 → 122 `match`. Those cells are computed by the guards ADRs 0319/0320 shipped, so
  the cycle that changed the artifacts was told about every number it changed.
* `stdlib/math.gy` now has a shape as well as contents: canonical literal, alias name. Adopting it for `string`
  (and any future data module) is a decision already made, recorded as Gap R.206 with the guard to copy.
* A data module's members are now documented in two spellings, which is more surface to keep answerable — and
  that is why the refusals are pinned: `math.inf`, `math.nan` and `math.floor` are named as absent in a test, so
  the row that pays them must change a table and cannot be landed as a surprise.
* `docs/language.md`'s standard-library list and `README.md`'s carry both spellings; the citation guard
  (`pkg/lang/roadmap_evidence_test.go`) keeps the evidence cells that point at the new tests honest.

## Alternatives rejected

* **Rename `PI` → `pi` and keep one spelling** — cleaner on disk, and it silently voids the record entries, the
  docs and the corpus that use the upper-case names. A language may break compatibility deliberately; it should
  not do it as a side effect of a conformance fix.
* **Keep only the upper-case names and compare with a twin forever** — the twin cannot adjudicate a name, which is
  the whole defect; it also hides the surface from the machine path, where an agent reads the matrix rather than
  the Go file.
* **Teach the checker to accept either spelling as an alias for the module's constant** — a special case in the
  resolver for a data module whose contents are already data; the module can say it, so the compiler should not.
* **Answer `math.inf`/`math.nan`/`math.floor` in this cycle** — an appealing three-line diff, but `inf`/`nan` need
  the emitter's float spelling rather than a literal the number lexer accepts (Gap R.135 is still open on the
  lexer), and `math.floor` is a module *call*, which is Gap I.2's road. Landing them here would have meant the
  naming row also carrying two unrelated semantics changes.
* **Delete `probe_math_const.gy` as superseded** — it is the program the record holds and the row that documents
  why a spelling can make a row un-CPython-runnable. Both rows are true; the matrix distinguishes them.

## References

`stdlib/math.gy`, `integration/programs/probe_math_constant_answers_the_references_spelling.gy`,
`integration/programs/probe_math_const.gy`,
`pkg/lang/math_const_spelling_test.go` (`TestTheReferenceSpellsItsMathConstantsAndTheCompiledLegAnswers`,
`TestTheTwoSpellingsOfAModuleConstantAreOneDeclaration`, `TestTheRecordStillAnswersForTheNamesTheCorpusHolds`,
`TestAMathNameTheModuleDoesNotDeclareIsRefusedInWords`),
`pkg/lang/module_const.go`, `pkg/lang/module_const_test.go`,
`integration/math_const_spelling_test.go` (`TestCLIAgentTheReferenceRunsTheProgramThatSpellsMathItsOwnWay`,
`TestTheMathProbeIsAssertedParityInTheMatrix`, `TestTheStdlibModuleOnDiskIsWhatTheTestsImport`),
`integration/module_const_test.go`, `integration/conformance_cases.go`,
`tools/recmerge/main.go`, `docs/language.md` (§ Standard library), ADR 0272, ADR 0302, ADR 0308, ADR 0186,
ADR 0319, ADR 0320, L11.6, Gap R.206, Gap R.207.
