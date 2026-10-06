package lang

import (
	"os"
	"testing"
)

// TestMain holds the drift check after every case has run, because the ledger describes the whole
// run rather than one case: a divergence found by the 3,000th source has to be weighed against the
// list alongside the first.
//
// It lives in a _test.go file for a reason worth keeping in mind when refactoring this harness: the
// testing package calls TestMain only when it is declared in a test file. The same function moved
// into a plain package file becomes an ordinary exported-looking function that nothing calls, the
// suite goes green, and the ratchet that fails a run on a new divergence simply stops running — silently,
// with the ledger file sitting there looking authoritative. GoldenDriftMain itself is in golden_main.go
// so the integration binary can call the same adjudicator against its own ledger.
func TestMain(m *testing.M) {
	os.Exit(GoldenDriftMain(m, GoldenDriftFile))
}
