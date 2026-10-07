package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The oracle ledger is data, and data rots. These tests keep it honest: every row must describe a case
// that exists, every exception must be explained and owned, and the reference leg must actually be
// running — a harness comparing the compiler to a stub looks exactly like a harness comparing it to
// CPython until someone asks a question the stub cannot answer. Roadmap L11.9, ADR 0186; ADR 0302 took
// out the second backend and left CPython as the only opinion that is not the compiler's own.

func TestOracleLedgerOnlyDescribesRegisteredCases(t *testing.T) {
	registered := map[string]bool{}
	for _, c := range conformanceCases() {
		registered[c.ID] = true
	}
	for id := range oracleLedger {
		if !registered[id] {
			t.Errorf("ledger row %s describes no registered case — delete it (or register the program)", id)
		}
	}
}

func TestOracleLedgerRowsAreExplainedOwnedAndPinned(t *testing.T) {
	for _, c := range conformanceCases() {
		switch c.Oracle {
		case lang.OracleMatch:
			if len(c.Pins) > 0 {
				t.Errorf("%s: a match row carries pins; pins record what a *wrong* answer looks like", c.ID)
			}
			if c.Reason != "" || c.Ref != "" {
				t.Errorf("%s: a conformant row should not carry a debt reason", c.ID)
			}
		case lang.OracleNA:
			if strings.TrimSpace(c.Reason) == "" {
				t.Errorf("%s: not_applicable without a reason (which gusty-only surface is it?)", c.ID)
			}
		case lang.OracleDebt:
			if strings.TrimSpace(c.Reason) == "" {
				t.Errorf("%s: debt row without a reason", c.ID)
			}
			if strings.TrimSpace(c.Ref) == "" {
				t.Errorf("%s: debt row without a roadmap reference — an unowned divergence is an unfixable one", c.ID)
			}
			if len(c.Pins) == 0 {
				t.Errorf("%s: debt row without a pin", c.ID)
			}
			if !strings.Contains(c.Ref, "L") && !strings.Contains(c.Ref, "Gap") {
				t.Errorf("%s: ref %q should name a roadmap item (L…) or a Gap", c.ID, c.Ref)
			}
			sawAOT := false
			for _, p := range c.Pins {
				switch p.Backend {
				case "aot":
					sawAOT = true
				default:
					t.Errorf("%s: pin for unknown backend %q — with one backend there is one leg to pin, and CPython is the other side of the comparison, not a leg", c.ID, p.Backend)
				}
			}
			if !sawAOT {
				t.Errorf("%s: a debt row must pin what the compiled program prints, or \"wrong\" has no definition", c.ID)
			}
		default:
			t.Errorf("%s: unknown declared oracle state %q", c.ID, c.Oracle)
		}
	}
}

// TestOracleThirdLegIsNotAStub runs a program whose whole point is that gusty and
// Python disagree, and requires the CPython leg to disagree: if the oracle ever
// quietly reported gusty's own answer, every debt row would turn into a match and the
// build would go green for the wrong reason.
// Its subject is Gap R.111: a bool handed to a function. Python renders the parameter as True and
// gusty renders the 1 the caller's comparison produced, because nothing carries the verdict across
// the call boundary. (It used to be probe_bool_in_a_container, whose disagreement ADR 0259 paid —
// a stub check has to be pointed at a debt that is still open, or it proves nothing.)
func TestOracleThirdLegIsNotAStub(t *testing.T) {
	c := lang.ConformanceCase{ID: "programs/probe_bool_through_a_call", Name: "probe_bool_through_a_call.gy", Source: readProgramSrc("probe_bool_through_a_call")}
	row := runLegs(t, c)

	if !row.PythonOK {
		t.Fatalf("the CPython leg failed: %s", row.PythonErr)
	}
	if !strings.Contains(row.PythonOut, "True") {
		t.Errorf("CPython should render a bool parameter as True, got %q", row.PythonOut)
	}
	if !strings.Contains(row.AOTOut, "1") || strings.Contains(row.AOTOut, "True") {
		t.Errorf("the compiled program is expected to print the stored number here (that is the debt); got %q", row.AOTOut)
	}
	if row.PythonOut == row.AOTOut {
		t.Errorf("the reference leg returned the compiler's answer — it is not an independent opinion")
	}
	if row.Oracle != lang.OracleDebt {
		t.Errorf("status = %q, want %q", row.Oracle, lang.OracleDebt)
	}
	if row.AOTMatchesPython {
		t.Errorf("the compiled program may not claim a match here: aot=%q python=%q", row.AOTOut, row.PythonOut)
	}
}

// TestOracleHarnessCanFail is the stub check for the whole L11.9 harness: with one
// pin changed, the same real observation must be reported as drift. A harness that
// cannot fail is not a harness.
func TestOracleHarnessCanFail(t *testing.T) {
	c := conformanceCaseByID(t, "programs/probe_print_atomic")
	row := runLegs(t, c)
	if len(row.OracleDrift) != 0 {
		t.Fatalf("the registry row is accurate today, so it should report no drift: %v", row.OracleDrift)
	}

	// Same program, same legs, one wrong pin: drift must appear.
	c.Pins = []lang.OraclePin{{Backend: "aot", Stdout: "<< 21 >>\ngot 42\nafter\n"}}
	bad := runLegs(t, c)
	if len(bad.OracleDrift) == 0 {
		t.Fatalf("a wrong pin produced no drift — the pin is not being checked")
	}
	if !strings.Contains(strings.Join(bad.OracleDrift, " | "), "pin says the aot leg prints") {
		t.Errorf("drift should name the leg and both answers: %v", bad.OracleDrift)
	}

	// And a row declared conformant for a program that is not: drift too.
	c = conformanceCaseByID(t, "programs/probe_print_atomic")
	c.Oracle = lang.OracleMatch
	c.Reason, c.Ref, c.Pins = "", "", nil
	if drift := runLegs(t, c).OracleDrift; len(drift) == 0 {
		t.Errorf("declaring a divergent program conformant produced no drift")
	}
}

func TestOracleProbeRowsAreRecordedAsDebt(t *testing.T) {
	m := conformanceMatrix(t)
	seen := map[string]lang.ConformanceResult{}
	for _, r := range m.Results {
		seen[r.Case.ID] = r
	}
	for _, c := range conformanceProbes() {
		r, ok := seen[c.ID]
		if !ok {
			t.Errorf("probe %s is not in the matrix artifact", c.ID)
			continue
		}
		if r.Oracle == lang.OracleMatch {
			t.Errorf("%s: a probe that now matches CPython is a paid debt — promote the program to conformanceStandalone and delete its ledger row", c.ID)
		}
		if r.Case.Ref == "" {
			t.Errorf("%s: probe row without an owner", c.ID)
		}
		if r.PythonOut == "" && r.PythonErr == "" {
			t.Errorf("%s: the CPython leg recorded nothing at all", c.ID)
		}
	}
}

// TestOracleCorpusHasNoUndocumentedDivergence is the ratchet on the other side: a
// program in the parity corpus that stops matching CPython fails the build with a
// message that says what to do, instead of waiting for someone to notice.
func TestOracleCorpusHasNoUndocumentedDivergence(t *testing.T) {
	m := conformanceMatrix(t)
	for _, r := range m.Results {
		if r.Case.Oracle == lang.OracleNA {
			continue // the oracle could not run this source; that is recorded separately
		}
		if r.OracleDeclared != r.Oracle {
			t.Errorf("%s: declared %q, observed %q — update the ledger with the reason and the owner",
				r.Case.ID, r.OracleDeclared, r.Oracle)
		}
		if len(r.OracleDrift) > 0 {
			t.Errorf("%s: %v", r.Case.ID, r.OracleDrift)
		}
	}
}

func conformanceCaseByID(t *testing.T, id string) lang.ConformanceCase {
	t.Helper()
	for _, c := range conformanceCases() {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no registered case with id %s", id)
	return lang.ConformanceCase{}
}
