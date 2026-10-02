package integration

import (
	"strings"
	"testing"
)

// The CLI half of "bools are values" (roadmap L11.1 step 2, ADR 0257): the human path
// prints the verdict, and the machine path names its type. `gustyc --eval 'True'` answers
// True the way `python -c 'print(True)'` does, and `--json` reports
// {"result": "True", "type": "bool"} rather than the 1 the verdict is stored as.

func TestCLIEvalWritesTheVerdict(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`True`, "True\n"},
		{`False`, "False\n"},
		{`1 == 1`, "True\n"},
		{`0 == None`, "False\n"},
		{`"yes" == "yes"`, "True\n"},
		{`not (1 > 2)`, "True\n"},
		{`1 + 1`, "2\n"},
		{`"text"`, "text\n"},
	} {
		out, code := cliRunCode(t, "--eval", tc.src)
		if code != 0 || out != tc.want {
			t.Errorf("--eval %q = %q (exit %d), want %q", tc.src, out, code, tc.want)
		}
	}
}

func TestCLIJSONNamesTheBoolType(t *testing.T) {
	out, code := cliRunCode(t, "--json", "--eval", "1 == 1")
	if code != 0 {
		t.Fatalf("--json --eval exit = %d\n%s", code, out)
	}
	if !strings.Contains(out, `"result": "True"`) || !strings.Contains(out, `"type": "bool"`) {
		t.Errorf(`a verdict should report {"result": "True", "type": "bool"}, got %s`, out)
	}
	// A verdict bound to a name keeps the type, because the question is asked of the
	// binding the name was last given.
	out, code = cliRunCode(t, "--json", "--eval", "ok = 2 < 3\nok")
	if code != 0 || !strings.Contains(out, `"type": "bool"`) {
		t.Errorf(`a bound verdict should report type bool, got %s (exit %d)`, out, code)
	}
	// and a number still reports as a number
	out, code = cliRunCode(t, "--json", "--eval", "1")
	if code != 0 || !strings.Contains(out, `"type": "int"`) {
		t.Errorf(`an int should report type int, got %s (exit %d)`, out, code)
	}
}

func TestCLIFileWritesTheVerdictOnTheCompiledPath(t *testing.T) {
	dir := t.TempDir()
	src := writeSrc(t, dir, "bools.gy",
		"print(True)\nprint(False)\nprint(1 == 1)\nprint(0 == None)\nprint(\"yes\" == \"yes\")\n")
	out, code := cliRunCode(t, "--file", src)
	if code != 0 {
		t.Fatalf("--file exit = %d\n%s", code, out)
	}
	if got, want := out, "True\nFalse\nTrue\nFalse\nTrue\n"; got != want {
		t.Errorf("compiled bools printed %q, want %q", got, want)
	}
}

// The promoted row: the program that opened this cycle prints CPython's answer on both
// backends, so the oracle agrees with it.
func TestOracleAgreesOnBoolValues(t *testing.T) {
	src := writeSrc(t, t.TempDir(), "probe_bool_value.gy", boolValueProbeSrc)
	py, ok := cpythonOut(t, src)
	if !ok {
		t.Skip("CPython unavailable")
	}
	if py != "True\nTrue\nFalse\nFalse\nTrue\n" {
		t.Fatalf("the reference itself answered %q", py)
	}
	if out, code := cliRunCode(t, "--file", src); code != 0 || out != py {
		t.Errorf("compiled printed %q (exit %d), want CPython's %q", out, code, py)
	}
	if out, code := cliRunCode(t, "--eval", strings.TrimSuffix(boolValueProbeSrc, "\n")); code != 0 || out != py {
		t.Errorf("interpreter printed %q (exit %d), want CPython's %q", out, code, py)
	}
}

const boolValueProbeSrc = "print(True)\n" +
	`print("yes" == "yes")` + "\n" +
	"print(False)\n" +
	"print(0 == None)\n" +
	`print("a" in ["a", "b"])` + "\n"

// The two shapes still owed, pinned from the same source text the probe programs carry:
// the authoritative pins live in conformance_cases.go, and what is asserted here is that
// the oracle still calls them divergences, so a closure has to change a verdict as well
// as a number.
func TestOracleStillCallsTheRemainingBoolShapesDivergences(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"probe_bool_through_a_call", "def show(f):\n    print(f)\n\nshow(1 == 1)\nshow(True)\n"},
		{"probe_bool_in_a_container", "print([True, 1])\nprint({\"k\": True})\n"},
	} {
		out, code := cliRunCode(t, "--oracle", tc.src)
		if code != 6 {
			t.Errorf("%s diverges from CPython but --oracle exited %d, want the divergence code\n%s", tc.name, code, out)
		}
	}
}
