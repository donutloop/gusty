package lang

import (
	"fmt"
	"os"
	"sort"
)

// GoldenDriftMain runs a package's tests and then adjudicates its divergence ledger: it writes the
// sources no record covers (when GUSTY_GOLDEN_MISSING names a file), rewrites the ledger (when
// GUSTY_GOLDEN_UPDATE is set), and otherwise fails the run on any new debt, paid-off debt still on
// the list, or change of shape. It returns the process exit code so a package's TestMain stays two
// lines and the two test binaries cannot drift apart in how they judge themselves.
// runner is the part of *testing.M this adjudicator needs, spelled as an interface so the production
// package does not import "testing" for a helper the test binaries call.
type runner interface{ Run() int }

func GoldenDriftMain(m runner, ledger string) int {
	code := m.Run()
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
