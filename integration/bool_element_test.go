package integration

import (
	"strings"
	"testing"
)

// The CLI half of "a bool is a nameable element kind" (roadmap Gap R.112, ADR 0259): what a person
// gets from `gustyc --file` is what `python3` prints, and what an agent gets from `--json` names the
// type. The unit table in pkg/lang/bool_element_test.go holds the shapes; this file holds the shipped
// binary's behaviour, including the ledger's claim about the three shapes this cycle measured and
// did not fix — a landing has to move a verdict as well as a number.

// TestCLIBoolElementPrintsWhatCPythonPrints runs the promoted program on both engines and against
// the oracle, from the file that is now parity corpus rather than a debt pin.
func TestCLIBoolElementPrintsWhatCPythonPrints(t *testing.T) {
	src := readProgramSrc("probe_bool_in_a_container")
	path := writeSrc(t, t.TempDir(), "bool_in_a_container.gy", src)
	py, ok := cpythonOut(t, path)
	if !ok {
		t.Skip("CPython unavailable")
	}
	if py != "[True, 1]\n[True]\n[False]\n{'k': True}\n{True}\n[1, 2, True]\nTrue\nTrue\nTrue\n2\n[True, 'a']\n{True: 1}\n[True, 1, 1]\n" {
		t.Fatalf("the reference itself answered %q", py)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, "--file", path)
		if code != 0 {
			t.Errorf("%s exited %d: %s", engine, code, out)
			continue
		}
		if out != py {
			t.Errorf("%s printed %q, want CPython's %q", engine, out, py)
		}
	}
	if out, code := cliRunCode(t, "--oracle", src); code != 0 {
		t.Errorf("the oracle called a bool element a divergence (exit %d):\n%s", code, out)
	}
}

// The machine path: an element read out of a container reports the type it is. Before this cycle the
// only honest answer was "int", because the slot held nothing but the 0/1.
func TestJSONCallsABoolElementABool(t *testing.T) {
	out, code := cliRunCode(t, "--json", "--eval", "xs = [True, 1]\nxs[0]")
	if code != 0 {
		t.Fatalf("--json --eval exited %d\n%s", code, out)
	}
	for _, want := range []string{`"result": "True"`, `"type": "bool"`, `"backend": "interpreter"`} {
		if !strings.Contains(out, want) {
			t.Errorf("--json --eval of a bool element did not report %s: %s", want, out)
		}
	}
	// The number beside it is still a number — the two live in one list.
	out, code = cliRunCode(t, "--json", "--eval", "xs = [True, 1]\nxs[1]")
	if code != 0 || !strings.Contains(out, `"type": "int"`) {
		t.Errorf(`an int element should still report type int, got %s (exit %d)`, out, code)
	}
}

// The shapes this cycle measured and filed rather than absorbed, pinned through the shipped binary so
// that closing one has to change a verdict here as well as in the ledger. Three are debts the oracle can
// judge (exit 6); the fourth is a program CPython itself refuses, so the honest answer is "no verdict"
// (exit 7) — never success, and never a silent collapse into 1.
//
// Two rows that used to be here are deliberately absent. The interpreter's dict comprehension keeping two
// entries under one key was paid by ADR 0260 the same day ADR 0259 filed it. And
// `probe_bool_chosen_by_an_operator` — the verdict a max/min fold chooses — became parity surface under
// ADR 0261: its program moved to the parity corpus, and a contract row left here expecting exit 6 for a
// paid debt passes forever without asserting anything.
func TestOracleStillCallsTheBoolNameLossShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int
	}{
		{"probe_bool_in_a_comprehension", 6},       // Gap R.116
		{"probe_minmax_candidate_unreadable", 6},   // Gap R.124 (the compiled leg's leg)
		{"probe_ternary_the_test_chose", 6},        // Gap R.125 (both engines, against CPython)
		{"probe_ternary_container_arms", 6},        // Gap R.128 (the compiled leg rejects the module)
		{"probe_round_digit_count_kind_unseen", 6}, // Gap R.129 (the compiled leg's leg)
		{"probe_float_loop_variable_as_number", 6}, // Gap R.130 (the compiled leg's leg)
		{"probe_negative_zero_constant", 6},        // Gap R.132 (the compiled renderer)
		// probe_builtin_without_arguments left this table when ADR 0287 paid it: int(), float(), bool()
		// and str() are constructors answering 0, 0.0, False and the empty text on all three legs, so the
		// program is parity surface now (conformanceStandalone). A contract row left here expecting exit 6
		// for a paid debt passes forever without asserting anything — the rule ADR 0261 wrote for
		// probe_bool_chosen_an_operator and ADR 0260 for the dict comprehension.
		{"probe_slot_order_in_a_ternary", 7}, // Gap R.119 (CPython raises, so there is no opinion)
	} {
		src := readProgramSrc(tc.name)
		out, code := cliRunCode(t, "--oracle", src)
		if code == 2 {
			t.Fatalf("%s: the oracle leg rejected the compiler's own module (ADR 0166):\n%s", tc.name, out)
		}
		if code != tc.want {
			t.Errorf("%s: --oracle exit = %d, want %d\n%s", tc.name, code, tc.want, out)
		}
	}
}
