# 0320. A citation that resolves to nothing is a claim with no witness

Status: accepted. Roadmap: `Gap R.205` (an Evidence cell cited two test files that are not in the tree),
measured immediately after Gap R.204 (ADR 0319, the counts), continues ADR 0317 (a guard that does not read its
own file never ran), ADR 0313 (the shard runner whose `-count=1` lets these guards run at all), ADR 0308 /
Gap R.190 (the witness ledger this ADR borrows as its exemption list), ADR 0186 (the promotion rule that makes an
Evidence cell a definition of done rather than a comment).

## Context

`roadmap.md`'s column contract says the `Status` cell is the authority and the `Evidence` cell is what makes it
worth reading. Gap R.189 — the `KeyError` row — carries 🟨 `PARTIAL`, and its Evidence cell named two files:

```
pkg/lang/key_error_message_test.go
integration/key_error_names_the_key_test.go
```

Neither exists in the tree. ADR 0301's cycle wrote those names; a later refactor folded the cases into
`container_methods_test.go`, where they live today as `TestKeyErrorNamesTheKey` and
`TestCLIKeyErrorNamesTheKeyOnTheLegThatCan`. The behaviour kept being tested. The citation stopped resolving,
and nothing — not the suite, not CI, not the doc guards ADR 0317 added — could see the difference.

That last clause is the whole defect: **a test that does not exist cannot fail.** `go test -run SomeName` is a
green no-op when the name matches nothing, and the full-suite runs certifying this cycle green were executing
the *renamed* cases under their new names. The tracker is the index of what is proven, and it is read that way by
agents as well as people; a dead citation silently converts "proven by these cases" into "asserted, trust us", in
the one direction that matters — a row can be marked `DONE` and keep its green status after its evidence has been
deleted out from under it.

Two neighbouring decisions made this worth doing rather than a one-line fix: ADR 0319 had just established that
a number in a document is a claim about a file and gets computed from it; and the same sweep over the tracker
found no other dead citations, which means the cost of the guard is one row's worth of prose and the benefit is
that the class cannot come back unnoticed.

## Decision

**Every path the tracker cites has to resolve, and the only exemption is a recorded deletion.**

* `pkg/lang/roadmap_evidence_test.go` walks every line of `roadmap.md`. Each backticked repository path — under
  `pkg/`, `integration/`, `cmd/`, `tools/`, `docs/`, with `.go`, `.json`, `.gy`, `.md` or `.txt` — must exist
  relative to the repository root. A citation spelled `path::TestName` must additionally find `func TestName`
  declared in that file (method receivers permitted), because the point of the `::` form is "this case, not just
  this file".
* The exemption is a **data file, not prose**: a citation is excused only when the path is a known-deleted
  artifact listed in `pkg/lang/testdata/witness-banned-phrases.txt` — the ledger ADR 0308 created, which names
  `pkg/lang/jit.go`, `EvalExpr`, `EvalProgram`, `InterpreterRun`. Citing one of those is a claim about a
  *deletion*, and the witness guard is the guard for that claim. A renamed test is not a recorded deletion and
  gets no licence.
* The alternative exemption was measured and rejected. Reusing `witness-history-markers.txt` (the "this line is
  talking about the past" list) silenced the guard completely: with it, **0 of the 3 dead citations were caught**,
  because a three-hundred-word row almost always contains `was`, `before`, `recorded` or `the record`. A marker
  word elsewhere on a row must not be able to licence a broken pointer.
* `(planned)` remains the one way to cite something that does not exist yet, and that spelling belongs to
  `integration/docs_citations_test.go`, which already owns `.gy` citations; this guard honours the marker and does
  not redefine it.
* `docs/roadmap-details.md` and `docs/adr/*` are **out of scope by decision**. They are the measurement narrative
  and the decision record: they name deleted files in order to explain the deletion, and an accepted ADR is a
  historical artifact — editing it to chase a rename destroys the thing it is kept for. The tracker is the live
  interface; the archive is the archive.
* Failure text names the row, the citation and the reason, and states the rule for the future: when a refactor
  moves a case into another file, the refactor edits the cell that cited it.
* The guard is tested against its own ability to fail, four ways: a missing file, a `::TestName` the file does not
  hold, a retired artifact that must stay exempt, and a real file + real case that must resolve — each required to
  name the row it came from. Then it was run red for real by putting Gap R.189's dead pair back.

## Agentic rationale

The loop's contract is that an agent picks work from `roadmap.md` and closes a row by editing its `Status` and
`Evidence` cells. That makes the Evidence cell the machine-readable half of the tracker: it is what an agent
writes, and what the next agent reads to decide whether a claim is checkable. If paths in it may be dead, the
tracker degrades into an unvalidated document, and the cheapest failure available to an agent — cite a plausible
filename nobody opens — becomes a way to buy a `DONE`. Checking resolution is a millisecond of `os.Stat` per
citation and removes that entire failure mode; the guard's message is written so an agent can act on it without
reading this ADR.

The exemption being a ledger rather than a prose rule is also deliberate: an agent that needs to cite a genuinely
deleted file adds a line to a data file and the reviewer sees the claim, instead of an agent discovering a magic
word it can sprinkle on a row to make a check disappear.

## Consequences

* Refactors that move cases between test files now have one more file to edit — the Evidence cells that cited
  them. That is the intended tax, and it is paid in the same commit that moved the cases, where the reviewer can
  see it.
* The tracker gains a new kind of red: a doc guard that fails because of the *shape* of a citation. The message
  distinguishes "no such file" from "the file holds no func X", so the fix is unambiguous.
* `testdata/witness-banned-phrases.txt` is now load-bearing for a second guard. Retiring an artifact means adding
  it there, which is already the workflow ADR 0308 set, so no new ritual is introduced.
* Coverage is `roadmap.md` only. The details file and the ADRs keep dead paths forever, and that is a feature: a
  decision record that gets silently rewritten to match today's filenames is worse than one that names a file
  that has since been renamed.
* Row-ID extraction is heuristic (the first cell, then the second, looking for `Gap `/`L…`) and is used only to
  phrase a failure. A mislabelled report is cosmetic; the check itself is exact.

## Alternatives rejected

* **Running every cited test** — a citation is a claim about a file, not a scheduling instruction. The suite runs
  what the suite runs; this guard checks the index, and executing thousands of cases to validate a footnote would
  make the fast suite slow and the claim no stronger.
* **Exempting lines that carry a history marker** — measured above: it silences the guard on the exact rows that
  need it, because long narrative rows always contain a marker word.
* **Fixing the one row and moving on** — the row was fixed, but the class survives: another refactor, another
  rename, another dead citation, and there is no signal at all.
* **Making `Status` cells machine-checkable against the ledger** (only mark `DONE` if the test exists *and passes*)
  — conflates a citation with an assertion; ADR 0302's drift ledgers are where "the answer changed" is ratcheted,
  and duplicating that here would make a doc guard depend on the whole compiled suite.
* **Putting the guard in `integration` instead** — `integration/docs_citations_test.go` is the sibling and stays
  there for `.gy`; the tracker's citations are mostly Go files, the claim is a `pkg/lang` concern (its ledger file
  is the exemption list), and ADR 0319's lesson stands: a guard belongs where it will be run, and both suites
  already gate.

## References

`pkg/lang/roadmap_evidence_test.go`, `pkg/lang/witness_claim_test.go`,
`pkg/lang/testdata/witness-banned-phrases.txt`, `pkg/lang/testdata/witness-history-markers.txt`,
`integration/docs_citations_test.go`, `roadmap.md` (column contract, Gap R.189, Gap R.190, Gap R.205),
ADR 0319, ADR 0317, ADR 0313, ADR 0308, ADR 0301, ADR 0186.
