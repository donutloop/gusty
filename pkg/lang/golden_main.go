package lang

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// GoldenDriftMain runs a package's tests and then adjudicates its divergence ledger: it writes the
// sources no record covers (when GUSTY_GOLDEN_MISSING names a file), rewrites the ledger (when
// GUSTY_GOLDEN_UPDATE is set), and otherwise fails the run on any new debt, paid-off debt still on
// the list, or change of shape. It returns the process exit code so a package's TestMain stays two
// lines and the two test binaries cannot drift apart in how they judge themselves.
//
// GUSTY_GOLDEN_REPORT names a file to write this process's evidence to (lang.GoldenEvidence: the ledger,
// the divergences, the sources asked) and hands the adjudication to whoever reads it. tools/testshards
// sets it, because a ledger row is a claim about a RUN and a sharded run is N processes: judged per
// process, a row whose witnesses are split across shards reads "paid" in one and "never asked" in another
// (roadmap Gap R.199, ADR 0315). The runner merges the files and calls lang.CheckDriftAgainst once.
// runner is the part of *testing.M this adjudicator needs, spelled as an interface so the production
// package does not import "testing" for a helper the test binaries call.
type runner interface{ Run() int }

func GoldenDriftMain(m runner, ledger string) int {
	code := m.Run()
	if p := os.Getenv("GUSTY_GOLDEN_REPORT"); p != "" {
		evidence := GoldenEvidenceFromLedger(ledger)
		if body, err := json.MarshalIndent(evidence, "", "  "); err == nil {
			if err := os.WriteFile(p, append(body, '\n'), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "could not write the golden evidence to %s: %v\n", p, err)
				return 1
			}
		}
		// The adjudication belongs to the merger; saying so on the tool channel keeps a shard's log from
		// reading like a run that checked nothing.
		fmt.Fprintf(os.Stderr, "golden evidence for %s written to %s (%d divergence(s), %d source(s) asked) — adjudicated by the runner\n",
			filepath.Base(ledger), p, len(evidence.Divergences), len(evidence.Asked))
		return code
	}
	if p := os.Getenv("GUSTY_GOLDEN_MISSING"); p != "" {
		var missing []string
		missingSources.Range(func(k, _ any) bool {
			if src, ok := k.(string); ok {
				missing = append(missing, src)
			}
			return true
		})
		sort.Strings(missing)
		if body, err := marshalGoldenMissing(missing); err == nil {
			_ = os.WriteFile(p, append(body, '\n'), 0o644)
		}
		fmt.Fprintf(os.Stderr, "%d source(s) with no recorded expectation written to %s\n", len(missing), p)
	}
	if os.Getenv("GUSTY_GOLDEN_UPDATE") != "" {
		if err := writeDriftLedgerTo(ledger, Divergences()); err != nil {
			fmt.Fprintln(os.Stderr, "could not rewrite the drift ledger:", err)
			return 1
		}
		fmt.Fprintf(os.Stderr, "interpreter golden drift ledger rewritten: %d divergence(s)\n", len(Divergences()))
		return code
	}
	if report := checkDriftAgainst(ledger); report != "" {
		fmt.Fprintln(os.Stderr, report)
		return 1
	}
	return code
}
