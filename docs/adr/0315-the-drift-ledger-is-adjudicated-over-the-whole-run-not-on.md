# 0315. The drift ledger is adjudicated over the whole run, not over one shard

Status: accepted. Roadmap: `Gap R.199` (the golden drift ratchet read process-global state, so a sharded
run judged ledger rows by a fraction of the run's evidence), `Phase 0` (hygiene — CI must be green for the
right reason), continues ADR 0302 (the record leg and its divergence ledger), ADR 0313 (the shard runner
whose partition made the defect observable), ADR 0308 (a shard is still a run).

## Context

ADR 0313 made CI run the suite in one process per core and argued that splitting was legal because a case's
answer depends on its source and the pinned toolchain, never on which cases ran before it. That argument had
one hole, and the fold door (ADR 0316) stepped into it: adding cases moved the round-robin partition, one
shard ended up holding `TestEvalImportModuleConst` without the corpus case that also asks about its source,
and the shard failed with

```
PAID debt still on the ledger: "import constlib\nconstlib.base + 1" (the program answered where the record
says it traps) now agrees with the record. Remove the row from testdata/interpreter-golden-drift.json.
```

The row is not paid. Nothing about that program changed. What changed was which process ran which case.

The mechanism, in the file that owns it (`pkg/lang/golden.go`):

```go
var askedAbout sync.Map                 // which sources this process put a question to
func checkDriftAgainst(ledger string) string {
    got := Divergences()                // which sources this process diverged on
    …
    if _, asked := askedAbout.Load(src); !asked { continue }   // "the case did not run"
    problems = append(problems, "PAID debt still on the ledger: …")
}
```

Both inputs are **process-global**, and each `TestMain` adjudicates its own process. Serially — and under
`-run` subsets, which is what the guard was written for — that is exactly right: the process *is* the run.
Sharded, the process is a shard, and a ledger row is a claim about the run:

* **The false failure.** Source S is on the ledger as owed. Case X (which runs S and agrees) lands in shard
  2; case Y (which runs S and diverges, keeping the row honest) lands in shard 7. Shard 2 saw S asked and
  not diverged → it declares the debt PAID and fails the build. Shard 7 correctly reports it owed. The run
  fails over nothing, and the message tells the reader to delete a ledger row that is still owed — the worst
  kind of advice, because the next agent obeys it with `GUSTY_GOLDEN_UPDATE=1` and the divergence vanishes
  from the only file that would have caught it.
* **The quiet direction, worse than the first.** A row whose divergence would be NEW is reported only by the
  shard that ran the case. That is caught. But a row whose *asking* is split so that no shard both asks and
  diverges reports nothing at all: new debt hides behind a partition. Neither direction is detectable from
  the shard's own evidence, because the evidence is a subset by construction.

The partition is `sort` + round-robin over `go test -list`, so where a source's two witnesses land is a
function of how many cases exist — which is why this survived ADR 0313's own test suite (a fixture package
with two cases has one witness per source) and surfaced the first time the real suite grew.

## Decision

**Each shard writes its evidence; the runner adjudicates each ledger once, over the merged evidence, using
the rules from the package that owns them.**

* **The evidence is a value, not a side effect.** `lang.GoldenEvidence{Ledger, Divergences, Asked}` is what
  one process knows; `lang.GoldenEvidenceFromLedger(ledger)` collects it, and
  `lang.MergeGoldenEvidence(sets)` unions it: a divergence any shard saw is the run's, and a source any shard
  asked counts as asked. Both lists come out sorted and deduplicated, so the report an agent reads is
  deterministic.
* **The adjudication became a function of its inputs.** `lang.CheckDriftAgainst(e GoldenEvidence, ledger
  string) string` holds a given evidence set against a given ledger file. `checkDriftAgainst(ledger)` — the
  process-local entry a package's `TestMain` calls — is now two lines: collect, then call the exported form.
  There is one implementation of "what does this run owe"; the runner does not reimplement it and cannot
  drift from it. The unit test asserts the property rather than the outcome: hand in a clean evidence set
  while the process itself is holding an unrelated divergence, and the verdict must stay clean.
* **A shard reports instead of judging.** `GUSTY_GOLDEN_REPORT=<file>` makes `GoldenDriftMain` write its
  evidence and skip the adjudication, printing one line on the tool channel saying who will judge it — so a
  shard's log never reads like a run that checked nothing.
* **`tools/testshards` owns the merge**, not the ledger: it creates a temp evidence dir, names each shard's
  file after its package and index (so two packages' shard 1 cannot collide — the two test binaries keep two
  ledgers over the one record), groups the files by the absolute ledger path each shard recorded, and calls
  `lang.CheckDriftAgainst` once per ledger. Any problem reddens the run with the same sentences the serial
  run prints. A green run says so explicitly: `<ledger> adjudicated over 20 shard(s) — 338 divergence(s)
  over 2765 source(s) asked, all on the ledger` — the numbers an agent needs to see that the merge happened.
* **A shard that failed skips the adjudication.** It already reddens the run, and its evidence may be
  truncated mid-run; a second failure assembled from partial evidence sends someone chasing the wrong thing.
* **The artifact-writing modes are untouched.** With `GUSTY_GOLDEN_UPDATE` or `GUSTY_GOLDEN_MISSING` in the
  environment the runner does not hand adjudication over at all, so the serial semantics ADR 0313 fixed
  ("those modes are single-process") keep exactly the behaviour they had.

Nothing in the ledger, the record, or any case's assertions changed. What changed is which body of evidence
the ratchet is applied to.

## Agentic rationale

The drift ledger is the machine surface that says *what this compiler still owes* (`jq . testdata/interpreter-golden-drift.json`), and a ratchet that can be satisfied by a partition move is not a ratchet. Three things follow for the interface:

* The adjudication is exported and total — `lang.CheckDriftAgainst(evidence, ledger)` — so a script, an
  agent, or a future third test binary can ask the same question without scraping a log or copying the rules.
* A shard's evidence is a JSON file with a stable shape, so "what did shard 7 see?" is a readable artifact of
  every run rather than a mystery.
* `testshards -json` carries each shard's `evidence` path, so the merged verdict and the per-shard evidence
  can be cross-read by a tool that did not run the suite.

## Consequences

* `make testshards` now fails the run for a ledger problem found in *any* shard and passes when the merged
  evidence matches — the behaviour CI needs, and the reason the fold cycle could land: with the per-shard
  reading, adding `pkg/lang/pair_fold_test.go`'s 47 cases turned an unrelated corpus row red.
* The suite is green again for the right reason: the constlib row is still owed, still reported, and the run
  that asks about it now asks over 20 processes rather than one.
* `pkg/lang/golden.go` grows an exported surface (`GoldenEvidence`, `GoldenEvidenceFromLedger`,
  `MergeGoldenEvidence`, `CheckDriftAgainst`) where it had one (`Divergences`). It is deliberately small and
  all read-only: no setter, no way to write the ledger from outside, so the tool cannot certify its own work.
* Five new rows in `tools/testshards/main_test.go`, each a way the split can lie: merged evidence must not
  report the paid-off row (with the per-shard reading asserted to still report it, so the test cannot pass by
  the check being removed); a row no shard saw diverge must still be reported paid; one shard's new debt must
  redden the run; a failed shard must invent nothing; evidence names must not collide. Plus
  `pkg/lang/golden_evidence_test.go` pinning the merge and the input-purity property directly.
* A later cycle that gives the ledger a `--json` face (Gap R.65's manifest work) can reuse
  `GoldenEvidence` as the payload instead of inventing one.

## Alternatives rejected

* **Skip the paid-debt direction under sharding** (keep "new divergence" per shard, drop "PAID"). Cheap, and
  it makes CI green — by cutting the ratchet in half, so a fixed divergence stops failing the build and the
  ledger silently becomes a list of things nobody has to shrink. This is the same trade Gap R.190's witness
  guard had to refuse.
* **`-shards 1` in CI.** Restores correctness by giving up ADR 0313's whole point (10 m 06 s against a
  10 m 00s default), and leaves the defect in place for the next runner to rediscover.
* **Tell each shard which sources the whole run will ask** (pre-computing the asked set). That needs an index
  from test names to sources — which does not exist, cannot exist (a table-driven case asks 60 sources; a
  corpus case asks a whole program), and would be a second source of truth about coverage.
* **Adjudicate in the runner with its own copy of the rules.** Two implementations of "what does this run
  owe" is how the ledger starts disagreeing with itself; the exported function costs one wrapper and keeps
  one.
* **Move the ledger to a shared file the shards append to.** Concurrent writers, partial lines, and a
  per-run artifact that a crashed shard leaves poisoned — the merge of finished shards has none of those
  failure modes.

## References

`pkg/lang/golden.go` (`GoldenEvidence`, `MergeGoldenEvidence`, `CheckDriftAgainst`),
`pkg/lang/golden_main.go` (`GUSTY_GOLDEN_REPORT`), `pkg/lang/golden_evidence_test.go`,
`tools/testshards/main.go` (`adjudicate`, `evidenceName`), `tools/testshards/main_test.go`,
`docs/operations.md` (§ Running the suite), ADR 0302, ADR 0308, ADR 0313, ADR 0316.
