package lang

// pkg/lang/golden_evidence_test.go — the drift adjudication as a function of its inputs (roadmap
// Gap R.199, ADR 0315).
//
// `checkDriftAgainst` used to read two pieces of process-global state — the divergences this process
// reported and the sources it put a question to — which made it a function of *one process's* run. That
// is fine serially and wrong under `make testshards` (ADR 0313), where the round-robin partition moves
// whenever a case is added: one source's witnesses land in different shards, and the shard that merely ran
// it reports the ledger row PAID while the shard that diverged reports it owed. The runner now merges the
// shards' evidence and calls `CheckDriftAgainst` once, so the rules have one implementation and this file
// pins that implementation — including the half that only matters for the merge: it must not peek at the
// process it happens to be running in.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLedgerFixture(t *testing.T, rows []Divergence) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "interpreter-golden-drift.json")
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatalf("marshal ledger: %v", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	return path
}

// TestCheckDriftAgainstReadsOnlyItsInputs is the property the sharded runner depends on: the verdict comes
// from the evidence handed in, not from what this process happened to accumulate. A process-local read
// would look identical in a serial run and wrong in a sharded one, which is exactly how Gap R.199 arrived.
func TestCheckDriftAgainstReadsOnlyItsInputs(t *testing.T) {
	const src = "xs = []\nxs.append(7)\nn = xs[0]\nprint(min(n, 3))\n"
	ledger := writeLedgerFixture(t, []Divergence{{
		Source: src, Reason: "the compiled backend cannot build a program the engine ran",
		Expected: `stdout "3\n"`,
	}})

	// This process reports a divergence for a source the ledger does not hold. The adjudicator handed a
	// clean evidence set must not mention it — the process's own set is not an input to this call.
	// (Set by hand rather than through noteDivergence, which ends the case with a Skipf.)
	const leaked = "print(the_process_remembered_this_on_its_own)"
	divergenceMu.Lock()
	divergenceSet[leaked] = Divergence{Source: leaked, Reason: "stdout differs from the record", Expected: "1\n", Actual: "2\n"}
	divergenceMu.Unlock()
	t.Cleanup(func() {
		divergenceMu.Lock()
		delete(divergenceSet, leaked)
		divergenceMu.Unlock()
	})

	clean := CheckDriftAgainst(GoldenEvidence{
		Ledger: ledger,
		Asked:  []string{src},
		Divergences: []Divergence{{
			Source: src, Reason: "the compiled backend cannot build a program the engine ran",
			Expected: `stdout "3\n"`, Actual: "roadmap L11.1",
		}},
	}, ledger)
	if clean != "" {
		t.Fatalf("the run's evidence matches the ledger, and the process's own set leaked in:\n%s", clean)
	}

	// The same evidence with the divergence withheld says PAID — the other direction of the ratchet, and
	// the one a shard used to answer wrongly.
	paid := CheckDriftAgainst(GoldenEvidence{Ledger: ledger, Asked: []string{src}}, ledger)
	if !strings.Contains(paid, "PAID debt still on the ledger") {
		t.Fatalf("asked and not diverged is paid off; got %q", paid)
	}

	// Asked by nobody: the row stays, because this evidence is not a full run.
	untouched := CheckDriftAgainst(GoldenEvidence{}, ledger)
	if untouched != "" {
		t.Fatalf("a source no shard asked must leave the row alone; got %q", untouched)
	}

	// New debt, a changed shape, and an unreadable ledger each say so in their own sentence.
	newDebt := CheckDriftAgainst(GoldenEvidence{Asked: []string{"print(a_source_the_ledger_never_heard_of())"}, Divergences: []Divergence{
		{Source: "print(a_source_the_ledger_never_heard_of())", Reason: "stdout differs from the record", Expected: "3\n", Actual: "2\n"},
	}}, ledger)
	if !strings.Contains(newDebt, "NEW divergence") {
		t.Fatalf("new debt is unannounced debt; got %q", newDebt)
	}
	changed := CheckDriftAgainst(GoldenEvidence{Asked: []string{src}, Divergences: []Divergence{
		{Source: src, Reason: "the trap's class or message differs from the record", Expected: "TypeError: x"},
	}}, ledger)
	if !strings.Contains(changed, "divergence changed shape") {
		t.Fatalf("a divergence that changed shape is not the row on the ledger; got %q", changed)
	}
	if got := CheckDriftAgainst(GoldenEvidence{}, filepath.Join(t.TempDir(), "absent.json")); !strings.Contains(got, "has no entries") && got != "" {
		t.Fatalf("an absent ledger reads as an empty one (a brand-new package has none); got %q", got)
	}
}

// TestMergeGoldenEvidenceIsTheUnionOfTheShards pins the merge the runner performs: a divergence any shard
// saw is the run's, a source any shard asked counts as asked, and both lists come out sorted and free of
// duplicates so the report an agent reads is deterministic.
func TestMergeGoldenEvidenceIsTheUnionOfTheShards(t *testing.T) {
	merged := MergeGoldenEvidence([]GoldenEvidence{
		{Ledger: "/ledger/a.json", Asked: []string{"print(1)", "print(2)"}, Divergences: []Divergence{
			{Source: "print(1)", Reason: "r", Expected: "e"},
		}},
		{Ledger: "/ledger/a.json", Asked: []string{"print(2)", "print(3)"}, Divergences: []Divergence{
			{Source: "print(3)", Reason: "r3", Expected: "e3"},
		}},
		{Ledger: "/ledger/a.json", Asked: []string{"print(2)"}, Divergences: []Divergence{
			// The same row reported twice (the source asked in two shards): one row in the output.
			{Source: "print(1)", Reason: "r", Expected: "e"},
		}},
	})
	if merged.Ledger != "/ledger/a.json" {
		t.Errorf("the merge lost the ledger: %q", merged.Ledger)
	}
	if len(merged.Asked) != 3 || merged.Asked[0] != "print(1)" || merged.Asked[2] != "print(3)" {
		t.Errorf("asked = %q, want the three sources sorted and deduped", merged.Asked)
	}
	if len(merged.Divergences) != 2 || merged.Divergences[0].Source != "print(1)" || merged.Divergences[1].Source != "print(3)" {
		t.Errorf("divergences = %+v, want the two rows sorted", merged.Divergences)
	}
	if empty := MergeGoldenEvidence(nil); len(empty.Asked) != 0 || len(empty.Divergences) != 0 {
		t.Errorf("merging nothing produced %+v", empty)
	}
}

// TestGoldenEvidenceFromLedgerCollectsWhatTheProcessKnows asks the collector directly: the ledger arrives as
// a path a test binary resolves from its own package directory — the merger groups shards by that path, so
// it has to be absolute — and the sources asked are the ones this process put a question to.
func TestGoldenEvidenceFromLedgerCollectsWhatTheProcessKnows(t *testing.T) {
	ledger := writeLedgerFixture(t, nil)
	const src = "print(sum([1, 2])) — the collector's own source\n"
	askedAbout.Store(src, true)
	e := GoldenEvidenceFromLedger(ledger)
	if !filepath.IsAbs(e.Ledger) {
		t.Errorf("the evidence must carry an absolute ledger path so the merger can group shards: %q", e.Ledger)
	}
	if e.Ledger != filepath.Clean(ledger) {
		t.Errorf("evidence ledger = %q, want %q", e.Ledger, filepath.Clean(ledger))
	}
	found := false
	for _, s := range e.Asked {
		if s == src {
			found = true
		}
	}
	if !found {
		t.Errorf("a source the process asked about is missing from the evidence it hands the runner")
	}
	if e.Divergences == nil {
		t.Errorf("Divergences must be a list, not nil — an empty run's JSON should read []")
	}
}
