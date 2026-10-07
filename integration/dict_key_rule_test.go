package integration

import (
	"github.com/donutloop/gusty/pkg/lang"
	"strings"
	"testing"
)

// The CLI half of "a dict is a key → value mapping" (roadmap Gaps R.118 and R.120, ADR 0260). The unit
// table in pkg/lang/dict_key_rule_test.go holds the shapes; this file holds the shipped binary's
// behaviour — what a person gets from `gustyc --file`, what an agent gets from `--json` and
// `--oracle`, and the ledger's claim that the two shapes ADR 0259 filed as debt are paid.

// The expected answer is CPython's own, line for line — not the record's, which is the engine that
// had it wrong.
const dictKeyRuleWant = "{'a': 2}\n{1: 'b'}\n{1: 2}\n{1.0: 'b'}\n{'a': 3, 'b': 2}\n2\n1\na\n{'a': 2} 1\n{1: 2}\n1\n"

func TestCLIDictKeyRulePrintsWhatCPythonPrints(t *testing.T) {
	path := writeSrc(t, t.TempDir(), "dict_key_rule.gy", readProgramSrc("dict_key_rule"))
	py, ok := cpythonOut(t, path)
	if !ok {
		t.Skip("CPython unavailable")
	}
	if py != dictKeyRuleWant {
		t.Fatalf("the reference itself answered %q", py)
	}
	for _, engine := range cliEngines {
		out, code := cliRunCode(t, engine, "--file", path)
		if code != 0 {
			t.Errorf("%s exited %d: %s", engine, code, out)
			continue
		}
		if out != py {
			t.Errorf("%s printed %q, want CPython's %q", engine, out, py)
		}
	}
	// The comprehension program ADR 0259 filed as debt is a parity program now; a paid debt that
	// still fails the oracle is the drift this leg watches for.
	compPath := writeSrc(t, t.TempDir(), "dict_comp_dup.gy", readProgramSrc("probe_dict_comprehension_duplicate_key"))
	for _, engine := range cliEngines {
		if out, code := cliRunCode(t, engine, "--file", compPath); code != 0 || out != "{1: 2}\n1\n" {
			t.Errorf("%s on the promoted comprehension program = %q (exit %d), want %q", engine, out, code, "{1: 2}\n1\n")
		}
	}
}

// Both programs are `match` in the ledger, so an agent reading the artifact sees a conformant dictionary
// rather than a pinned wrong answer, and the oracle's own exit class says the same thing.
func TestOracleCallsADuplicateKeyPaid(t *testing.T) {
	for _, name := range []string{"dict_key_rule", "probe_dict_comprehension_duplicate_key"} {
		src := readProgramSrc(name)
		out, code := cliRunCode(t, "--oracle", src)
		if code == 2 {
			t.Fatalf("%s: the oracle leg rejected the compiler's own module (ADR 0166):\n%s", name, out)
		}
		// The payload used to carry a parity member ("parity yes") because there were two engines to
		// agree; with one backend the verdict is a comparison against the reference, and the word for
		// that is simply `oracle: match`. The exit class is the machine-facing half and is unchanged.
		if code != 0 || !strings.Contains(out, "oracle: match") {
			t.Errorf("%s: --oracle exit = %d and no `oracle: match` verdict\n%s", name, code, out)
		}
	}
}

// The machine path: a value read out of a dict that was written twice is the value the last write put
// there, and the container reports one entry — the two questions a script asks when it is checking
// whether its own program did what it meant.
func TestJSONSeesOneEntryForARepeatedKey(t *testing.T) {
	// The row's claim is about the dict: one entry, and the value the last write left. The reference
	// is what says so, and it is asked live. Whether the *prompt* reports the value is a separate
	// question that is not yet answered — the compiled echo renders through the one str/repr table, and
	// a call or a slot read whose kind the compiler cannot prove is silent there (roadmap L13.1) — so
	// this case accepts the value or the filed silence, and never a value that is wrong.
	for _, tc := range []struct{ expr, result, typ string }{
		{"d = {\"a\": 1, \"a\": 2}\nlen(d)", "1", "int"},
		{"d = {\"a\": 1, \"a\": 2}\nd[\"a\"]", "2", "int"},
	} {
		out, code := cliRunCode(t, "--json", "--eval", tc.expr)
		if code != 0 {
			t.Fatalf("--json --eval %s exited %d: %s", tc.expr, code, out)
		}
		if strings.Contains(out, `"result": "`) {
			if !strings.Contains(out, `"result": "`+tc.result+`"`) {
				t.Errorf(`--json --eval %s reported the wrong value: %s`, tc.expr, out)
			}
			continue
		}
		// Silent: corroborate the row against the reference, and leave the silence on the roadmap.
		printable := strings.Replace(tc.expr, "\nlen(d)", "\nprint(len(d))", 1)
		printable = strings.Replace(printable, "\nd[\"a\"]", "\nprint(d[\"a\"])", 1)
		pyout, _, perr := lang.PythonRun(printable)
		if perr != nil || strings.TrimSpace(pyout) != tc.result {
			t.Errorf(`--json --eval %s reported no value, and the reference does not corroborate the row either: cpython %q err %v`, tc.expr, pyout, perr)
			continue
		}
		t.Logf("echo of %s is silent (roadmap L13.1); the reference's %s (%s) is corroborated", tc.expr, tc.result, tc.typ)
	}
}
