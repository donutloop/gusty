package integration

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The unhashable key, through the CLI: the same doors the unit rows assert, judged by a caller who
// sees only an exit code, stdout and the traceback the runtime wrote. Three of these doors used to
// ANSWER — `False` for `[1] in {"a": 1}`, `{[1]: 2}` for the store — which is why the exit code and
// the empty stdout are asserted here and not merely the sentence (roadmap Gap R.217, ADR 0328).

// cliStreams runs the built gustyc and returns stdout, stderr and the exit code separately: a trap
// is a sentence on stderr *and* a silence on stdout, and merging the two would let a stray print
// through.
func cliStreams(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	code = 0
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc %v: %v", args, runErr)
		}
		code = ee.ExitCode()
	}
	return out.String(), errb.String(), code
}

func TestAnUnhashableKeyExitsAsATrapThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want string
	}{
		{"a list read of a dict literal", `print({"a": 1}[[1]])`, "TypeError: unhashable type: 'list'"},
		{"a dict read of a dict literal", `print({"a": 1}[{1: 2}])`, "TypeError: unhashable type: 'dict'"},
		{"a set read of a dict literal", `print({"a": 1}[{1}])`, "TypeError: unhashable type: 'set'"},
		{"a list read of a dict variable", "d = {\"a\": 1}\nprint(d[[1]])", "TypeError: unhashable type: 'list'"},
		{"a list read through a bound name", "x = [1]\nd = {\"a\": 1}\nprint(d[x])", "TypeError: unhashable type: 'list'"},
		{"a list needle in a dict", `print([1] in {"a": 1})`, "TypeError: unhashable type: 'list'"},
		{"a dict needle in a dict", `print({1: 2} in {"a": 1})`, "TypeError: unhashable type: 'dict'"},
		{"a list needle in a set", `print([1] in {1, 2})`, "TypeError: unhashable type: 'list'"},
		{"a list key on the store road", "d = {}\nd[[1]] = 2\nprint(d)", "TypeError: unhashable type: 'list'"},
		{"a set key on the store road", "d = {}\nd[{1}] = 2", "TypeError: unhashable type: 'set'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 3 {
				t.Fatalf("exit %d, want 3 (the uncaught-exception exit)\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if out := strings.TrimSpace(stdout); out != "" {
				t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tt.src, out)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Fatalf("%q did not raise %q:\n%s", tt.src, tt.want, stderr)
			}
		})
	}
}

// The soundness half at the same altitude: a list haystack answers membership by walking, so an
// unhashable needle is legal and the program must run.
func TestMembershipOfAnUnhashableNeedleInAListRunsThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`print([1] in [[1], [2]])`, "True"},
		{`print({1} in [{1}])`, "True"},
		{`print({"a": 1} in [{"a": 1}])`, "True"},
		{"x = [[1]]\nprint([1] in x)", "True"},
		{`print("a" in ["a"])`, "True"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this test\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != tt.want {
				t.Fatalf("printed %q, want %q", got, tt.want)
			}
		})
	}
}

// The whole matrix as one program, run the way a person runs a program, with CPython's stdout as
// its own golden file — line for line, because a program that prints fifteen lines and gets one of
// them wrong is a program that prints fourteen right answers and a lie.
func TestTheUnhashableKeyProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/unhashable_key.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/unhashable_key.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/unhashable_key.gy")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(got), len(wantLines), strings.Join(got, "\n"))
	}
	for i := range wantLines {
		if got[i] != wantLines[i] {
			t.Fatalf("line %d: the program answered %q where CPython answers %q\n%s", i+1, got[i], wantLines[i], strings.Join(got, "\n"))
		}
	}
}

// The row that owns the program, and the claim the harness runs it under: an asserted match, not
// a probe. A program that only runs unasserted is a program nobody is watching.
func TestTheUnhashableKeyProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "unhashable_key.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs unhashable_key.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	if !strings.Contains(row.Source, "a list key raises TypeError") || !strings.Contains(row.Source, "a list store key raises TypeError") {
		t.Fatalf("row %s runs a program without the store and read refusals", row.ID)
	}
}
