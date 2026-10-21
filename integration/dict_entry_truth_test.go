package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// dict_entry_truth_test.go — roadmap Gap R.284 (ADR 0388) at the command line.
//
// `gustyc --eval 'd = {None: "hi"}\nif d[None]:\n    print("then")'` took the `else` arm at exit 0, and
// `bool({1: []}[1])` answered `True`: the condition tested the entry's payload — an interned text's table
// index, a float box's handle, a container's handle — where the reference tests the value. Through the CLI
// the arms now agree with the reference line for line, and the branch that does not run is the same branch
// CPython does not run.

func TestTheTruthOfASelfDescribingDictEntryAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"text_entry_takes_then", "d = {None: \"hi\"}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "t\n"},
		{"empty_text_takes_else", "d = {None: \"\"}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"empty_text_under_a_text_key", "d = {\"k\": \"\"}\nif d[\"k\"]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"falsy_float_takes_else", "d = {None: 0.0}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"negative_zero_takes_else", "d = {None: -0.0}\nprint(bool(d[None]))\n", "False\n"},
		{"float_takes_then", "d = {None: 3.5}\nprint(bool(d[None]))\n", "True\n"},
		{"void_takes_else", "d = {None: None}\nprint(bool(d[None]))\n", "False\n"},
		{"empty_list_takes_else", "d = {None: []}\nprint(bool(d[None]))\n", "False\n"},
		{"filled_list_takes_then", "d = {None: [0]}\nprint(bool(d[None]))\n", "True\n"},
		{"empty_dict_takes_else", "d = {None: {}}\nprint(bool(d[None]))\n", "False\n"},
		{"verdict_entry", "d = {None: False}\nprint(bool(d[None]))\n", "False\n"},
		{"int_key_entries", "d = {7: \"\", 8: \"x\"}\nprint(bool(d[7]), bool(d[8]))\n", "False True\n"},
		{"ternary_head", "d = {None: \"x\"}\nprint(\"y\" if d[None] else \"n\")\n", "y\n"},
		{"ternary_head_falsy", "d = {None: 0.0}\nprint(\"y\" if d[None] else \"n\")\n", "n\n"},
		{"while_head_runs_once", "d = {None: 2}\nwhile d[None]:\n    print(\"once\")\n    d[None] = 0\n", "once\n"},
		{"while_head_never_runs", "d = {None: \"\"}\nwhile d[None]:\n    print(\"never\")\nprint(\"done\")\n", "done\n"},
		{"run_built_dict_entry", "d = {}\nd[None] = 0.0\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		{"print_and_arm_agree", "d = {None: \"\"}\nprint(d[None])\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n", "\nf\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_truth.gy", tc.src)
			ref, code := oracleTrap(t, path)
			if code != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", code, ref)
			}
			if ref != tc.want {
				t.Fatalf("the reference prints %q, want %q", ref, tc.want)
			}
			for _, engine := range cliEngines {
				out := cliRun(t, engine, path)
				if _, exit := cliRunCode(t, engine, path); exit != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, exit, out)
				}
				if strings.Contains(out, "codegen:") {
					t.Errorf("%s refused a program the reference answers:\n%s", engine, out)
				}
				if out != tc.want {
					t.Errorf("%s printed %q, the reference prints %q", engine, out, tc.want)
				}
			}
		})
	}
}

// The operand `and`/`or` choose is chosen by its truth, so the same door decides it. At the CLI the entry's
// spelling now prints the operand the reference prints — an operand, not the verdict the operator is not.
func TestTheTruthOfTheEntryChoosesTheOperandAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"text_entry_and", "d = {None: \"hi\"}\nprint(d[None] and 1)\n", "1\n"},
		{"empty_text_entry_and", "d = {None: \"\"}\nprint(d[None] and 1)\n", "\n"},
		{"text_entry_or", "d = {None: \"hi\"}\nprint(d[None] or 2)\n", "hi\n"},
		{"empty_text_entry_or", "d = {None: \"\"}\nprint(d[None] or 2)\n", "2\n"},
		{"falsy_float_entry_or", "d = {None: 0.0}\nprint(d[None] or 2)\n", "2\n"},
		{"empty_list_entry_or", "d = {None: []}\nprint(d[None] or 2)\n", "2\n"},
		{"filled_list_entry_and", "d = {None: [1]}\nprint(d[None] and 2)\n", "2\n"},
		{"void_entry_or_text", "d = {None: None}\nprint(d[None] or \"d\")\n", "d\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "dict_entry_truth_choose.gy", tc.src)
			ref, code := oracleTrap(t, path)
			if code != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", code, ref)
			}
			if ref != tc.want {
				t.Fatalf("the reference prints %q, want %q", ref, tc.want)
			}
			stdout, stderr, ec := cliStreams(t, "--file", path)
			if ec == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if ec == 1 {
				t.Fatalf("the compiler refused a program the reference answers (exit 1):\n%s\n%s", stdout, stderr)
			}
			if stdout != tc.want {
				t.Errorf("the compiled leg printed %q, the reference prints %q", stdout, tc.want)
			}
		})
	}
}

// The boundary, witnessed at the CLI as it is today: the entry read handed to a call carries its pair and
// answers with its kind (roadmap Gap R.220, paid), and the entry read bound to a name keeps its refusal
// (Gap R.146). A commit that pays one must not be readable as paying the other, so both are asserted.
func TestTheTruthOfTheEntryCLIBoundaryKeepsItsOwnRows(t *testing.T) {
	t.Parallel()
	callArg := "def f(v):\n    return v\n\nd = {None: 3.5}\nprint(f(d[None]))\n"
	path := writeSrc(t, t.TempDir(), "dict_entry_truth_boundary.gy", callArg)
	stdout, stderr, code := cliStreams(t, "--file", path)
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 || stdout != "3.5\n" {
		t.Errorf("the pair call argument printed %q (exit %d), want the reference's `3.5` — Gap R.220's paid row; stderr %q", stdout, code, stderr)
	}
	bound := "d = {None: []}\nx = d[None]\nprint(bool(x))\n"
	bpath := writeSrc(t, t.TempDir(), "dict_entry_truth_bound.gy", bound)
	bout, berr, bcode := cliStreams(t, "--file", bpath)
	_ = bout
	if bcode != 1 || !strings.Contains(berr, "holds a slot the program built at run time") {
		t.Errorf("the bound entry did not keep its Gap R.146 refusal (exit %d):\n%s\n%s", bcode, bout, berr)
	}
}

// The conformance program carries the truth rows: the arms, `bool()` over each entry kind, the ternary. Its
// answer is CPython's line for line — the same bar every other conformance program is held to.
func TestTheTruthOfTheEntryProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	path := "programs/dict_none_bool_key.gy"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	want, err := os.ReadFile("testdata/dict_none_bool_key.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	for _, keep := range []string{`if t[None]:`, `print(bool(t[None]), bool({None: ""}[None]), bool({None: 0.0}[None]), bool({None: []}[None]))`, `1 if {False: [0]}[False] else 0`} {
		if !strings.Contains(string(src), keep) {
			t.Fatalf("the conformance program stopped carrying Gap R.284's row %q", keep)
		}
	}
	stdout, stderr, code := cliStreams(t, "--file", path)
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	gotLines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(gotLines), len(wantLines), strings.Join(gotLines, "\n"))
	}
	for i := range wantLines {
		if gotLines[i] != wantLines[i] {
			t.Fatalf("line %d: compiled %q, reference %q\n%s", i+1, gotLines[i], wantLines[i], strings.Join(gotLines, "\n"))
		}
	}
}

func TestTheTruthOfTheEntryProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/dict_none_bool_key.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/dict_none_bool_key.gy is not on the record — record it from the reference with tools/recmerge, never from this backend")
	}
	for _, one := range []string{
		"d = {None: \"hi\"}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {None: \"\"}\nif d[None]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {\"k\": \"\"}\nif d[\"k\"]:\n    print(\"t\")\nelse:\n    print(\"f\")\n",
		"d = {None: 0.0}\nprint(bool(d[None]))\n",
		"d = {None: []}\nprint(bool(d[None]))\n",
		"d = {None: \"hi\"}\nprint(d[None] or 2)\n",
		"d = {None: 2}\nwhile d[None]:\n    print(\"once\")\n    d[None] = 0\n",
	} {
		if !lang.HasGoldenAnswer(one) {
			t.Errorf("the truth-of-the-entry row %q is not on the record", one)
		}
	}
}
