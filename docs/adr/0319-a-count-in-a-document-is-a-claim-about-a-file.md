# 0319. A count in a document is a claim about a file, so the file has to be opened

Status: accepted. Roadmap: `Gap R.204` (the record's size was a number someone remembered), measured while
closing Gap R.197 (ADR 0318), continues ADR 0317 (a guard that does not read its own file never ran), ADR 0313
(the shard runner, whose `-count=1` is what lets these guards run at all), ADR 0302 (the record leg, whose
`testdata` files are the subject), Gap R.193 (the witness guard, which already reads the agent-facing docs by
path).

## Context

`tools/recmerge` finished the fold-ordering cycle's record additions with `checked 35, added 35, entries 5948`.
Three sentences elsewhere in the tree said the record holds **5623** sources:

* `docs/operations.md`'s artifact table — the machine-facing description of the four ledger files, the one an
  agent reads to decide what the suite can certify;
* `AGENTS.md`'s record-leg paragraph — the loop's own contract, read before an agent decides which leg witnesses
  its feature;
* this cycle's own recorder paragraph, which quoted a *third* figure (5913, true for one hour).

Each figure had been correct on the day it was written. Nothing recomputed any of them. The other three counts
in the same table — 338 `pkg/lang` divergences, 21 `integration` divergences, 7 CPython-debt rows — were
correct, and correct by luck: those files change only when a run registers a row a human then writes about. The
record is the one artifact that grows as a side effect of closing a feature, so its number was the one that
rotted.

This is ADR 0317's defect class with the cache removed. There, a guard existed and did not read the file, so its
`ok` was a lie. Here, no guard existed, so the lie had no author: a claim whose subject is a file, never checked
against it. And the same asymmetry makes it survive review — the number is plausible, so nothing about it looks
wrong.

Then the same test was pointed at `roadmap.md`'s Snapshot table — the one captioned **"(measured, not
remembered)"** — and eight of its numbers were wrong, in three kinds of way. Some had simply rotted
(`306 records, highest 0314` against 311 files up to `0319`; the record's 5913 against 5948; `101 of 117 queue
rows` against a table of 119). Some had never been updated after a legitimate change: closing Gap R.197 promoted
a conformance probe out of debt, which moves one matrix row from divergent to parity-asserted, so the snapshot's
`135 parity-asserted + 39 recorded divergences` and `120 match · 33 debt` were each one off **because of work
this very cycle did** — no checklist step in that cycle could have noticed. And one was a rule the tracker had
not absorbed: the row describing the test suite still sold `go test -tags=llvm20 ./...`, the cached command ADR
0317 took out of the shipped gate. A reader who copies a command out of the tracker is copying an interface, and
the tracker was shipping the unsafe one.

## Decision

**Every count an artifact owns is computed from the artifact, in a test, in both suites.**

* `pkg/lang/golden_artifact_counts_test.go` reads the four artifacts named in `docs/operations.md`'s table —
  the record and the three ledgers — counts them, and requires the number in the table's row for that file to be
  the number in the file. It requires the same of the two *prose* counts (`AGENTS.md`, `docs/operations.md`),
  matching `([0-9][0-9,]*)\s+sources\b` — plural only, because the same documents say "1 source" and
  "2765 source(s)" about other things.
* `integration/docs_artifact_counts_test.go` asks the identical questions from `integration/`'s directory. The
  duplication is the decision, not an oversight: the guard has to be in the gate that gets gated, and an agent
  that runs only `go test ./integration` must still be stopped.
* `pkg/lang/roadmap_snapshot_test.go` does the same for `roadmap.md`'s Snapshot table, on the eight rows whose
  value cell is a claim about files in this repository: the ADR count and highest number against `docs/adr/`, the
  record count and the three ledger counts against the ledgers, the matrix total and its asserted/divergent halves
  and the oracle verdicts against `integration/conformance-matrix.json`, the conformance-program count against
  `integration/programs/*.gy`, and the owed/total queue-row counts against the Open queue table itself. A ninth
  assertion is not a count at all: the "Test suite" row must name `go test -tags=llvm20 -count=1 ./...`, because
  the tracker quoting ADR 0317's superseded command is the same defect wearing a sentence instead of a digit.
  Rows are matched by their **label** and read one cell wide, so a row that is renamed fails as "no such row"
  rather than as a pass, and prose in the neighbouring columns cannot smuggle digits into a comparison.
* The record is counted **twice, by two decodings that must agree**: the map decode the suite itself uses
  (`loadGolden`), and a token-stream walk of the `entries` object that keeps repeats. A JSON object with one key
  spelled twice decodes cleanly, keeps the last of the pair, and reports a count no case can reach — and the file
  is one that `tools/recmerge` appends to, which is how a repeated key arrives.
* Rows are located by **full artifact path**, never by basename: `pkg/lang/testdata/interpreter-golden-drift.json`
  and `integration/testdata/interpreter-golden-drift.json` share a basename, and a locator that found the first
  would certify the second from a file it never opened.
* The guard is tested against its own ability to fail — `TestTheCountGuardCanFail` bumps each of the four counts
  in memory (after requiring the row to be a unique slice of the document) and demands the disagreement be
  reported. It was additionally run red for real by editing the table to 5947 and `AGENTS.md` to 5900.
* Where a count in prose is decorative, it is **removed rather than guarded**. Three sentences added this cycle
  quoted the record's size for effect ("a 2300-line reformatting of 5913 entries", "a record whose 5900 entries
  are re-sorted"); all three now say it without a number. `_001_session_learnings.md` keeps its dated figures —
  it is a journal, and "the record was 5913 when this was measured" is a fact that cannot rot.

## Agentic rationale

The record leg's authority is the whole reason the suite can say anything about values, and its coverage number
is how an agent decides whether the leg can witness a feature at all. An agent that reads "5623 sources" in the
contract plans against a corpus 325 entries smaller than the one that exists, and — worse — learns that the
documents in this repository may be approximately true. The failure mode of a stale count is not a wrong
verdict, it is a lost trust boundary, and this toolchain's agent interface is made of those boundaries.

A count is also the cheapest possible thing to get wrong at scale: the record changes in most feature cycles,
and every cycle that edits it without recomputing the number makes the docs less true. Putting the computation
in both suites converts an invisible drift into a red test whose message names the file, the artifact, both
numbers, and the exact cell to edit — which is the same property the exit-code contract (ADR 0006) and the
schema'd refusals (ADR 0004) are built to have: the interface tells you what it needs.

## Consequences

* `docs/operations.md`'s artifact table and `AGENTS.md`'s record-leg sentence become **pinned surface**. Editing
  the record without editing the sentence fails the suite; that is the point, and the failure text is written to
  be actionable rather than alarming.
* `roadmap.md`'s Snapshot table becomes pinned surface too, and it now carries a promise its caption always
  made: a number in that table is produced by reading something. The cost is real and small — closing a
  conformance row or appending to a ledger now fails until the matching snapshot cell is edited, which is the
  same friction `roadmap.md`'s row contract already imposes, applied to the summary instead of the rows.
* Any new artifact added to the table must also be added to `countedArtifacts`, or its row will pass unread. The
  locator `t.Fatalf`s when a named artifact has no row, so a row cannot silently disappear either.
* `pkg/lang` and `integration` each read the record's 5948 entries at guard time. Both decodes are `encoding/json`
  over a ~1.5 MB file and complete in milliseconds; this is why the guard does not reuse `loadGolden`'s cached map
  exclusively — the token walk has to see what the map cannot.
* The suite gets two more guards over files outside their packages, and so depends again on ADR 0313's
  `-count=1` (ADR 0317) to be run at all after a doc edit. A future runner must keep that flag.
* The record file itself is unchanged by this row: the counts moved toward the file, never the other way.

## Alternatives rejected

* **A generator that writes the table** (`go generate ./docs`) — the table would still be true as of whenever
  someone last ran it, and the failure would be a diff in a review nobody reads rather than a red test in the gate
  that is read.
* **`go:embed` the artifacts so the docs' numbers become build inputs** — copies a 1.5 MB record and three ledgers
  into two test binaries to avoid writing a test, and still does not track a file opened by a computed path. ADR
  0317 rejected this for the same reason.
* **Deleting the counts** — the coverage claim is an agent-facing interface, not decoration; the fix is to compute
  it, not to hide the thing agents plan with.
* **Guarding only the record** — the three ledgers' numbers were right today by luck alone, and a divergence row
  appears whenever a compiled leg disagrees; covering all four costs one struct literal.
* **A single guard in `pkg/lang`** — the package most agents run, but not the only suite: `integration` owns two
  of the four files and is the suite a CLI-facing change runs.
* **Trusting `tools/recmerge` to keep the number fresh** — the recorder writes the record, not the docs, and a
  recorder that also edited prose would be an artifact writing its own coverage claim.

## References

`pkg/lang/golden_artifact_counts_test.go`, `pkg/lang/roadmap_snapshot_test.go`,
`integration/docs_artifact_counts_test.go`,
`pkg/lang/golden.go` (`loadGolden`, `GoldenFile`), `tools/recmerge/main.go`, `docs/operations.md`
(§ The suite's own interface), `AGENTS.md` (the record leg), ADR 0317, ADR 0313, ADR 0302, ADR 0318,
Gap R.193, Gap R.200, Gap R.204.
