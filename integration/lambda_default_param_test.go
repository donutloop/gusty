package integration

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// integration/lambda_default_param_test.go — the reference leg of roadmap Gap R.263 / ADR 0352: a
// `lambda`'s parameter list is spelled the same way a `def`'s is, so `lambda x, y=1: x + y` is a program.
//
// The unit file owns the tables and the AST claim. This one asks the same programs of the only interface an
// agent has — an exit code, stdout, and the pass's own JSON account — because the defect was a front-end
// refusal at exit 1 for a program the reference runs, and a refusal is only visible from outside. Every
// expectation is produced by the pinned CPython in the test body, never transcribed: the unit table's `want`
// strings are checked here against the reference, which is what keeps a `want` that was ever tuned honest.

var lambdaDefaultCLIRows = []string{
	"f = lambda x, y=1: x + y\nprint(f(1))",
	"f = lambda x, y=1: x + y\nprint(f(1, 5))",
	"f = lambda x=2, y=3: x * y\nprint(f(), f(2), f(2, 5))",
	"f = lambda x, y=1: x + y\nprint(f(1, y=9))",
	"def two():\n    return 2\n\nf = lambda x, y=two(): x * y\nprint(f(3))",
	"STEP = 2\nf = lambda x, k=STEP: x * k\nprint(f(3))",
	"flag = True\nf = lambda x, y=1 if flag else 2: x + y\nprint(f(1))",
	"f = lambda x, tag=\"?\": x + tag\nprint(f(\"a\"))",
	"k = 100\nf = lambda x, k=3: x * k\nprint(f(2), k)",
	"print(sorted([3, 1, 2], key=lambda v: 0 - v))",
	"def apply(g, v):\n    return g(v, 2)\n\nprint(apply(lambda x, k: x * k, 4))",
	"def d(a=2, b=3):\n    return a * b\n\nprint(d(), d(5), d(5, 5))",
}

// TestALambdaDefaultAnswersWhatTheReferenceAnswersThroughTheCLI runs each row twice, and forbids the three
// ways the row used to fail: exit 2 (the compiler's own bug, ADR 0166), a refusal at exit 1 for a program the
// reference runs (ADR 0249 — a diagnostic is not a way to make a hard case invisible), and a wrong value at
// the exit code of success.
func TestALambdaDefaultAnswersWhatTheReferenceAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range lambdaDefaultCLIRows {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference runs this program, so a refusal is a wrong answer\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheCallablesAccountReportsTheDefaultedLambda is the machine consumption path. `--callables` is what an
// agent reads to learn what the function-value pass did; before this row a defaulted `lambda` never reached
// the pass at all, because the parser had refused it — so the document is where "the grammar accepted it AND
// the pass took it" is visible in one read.
func TestTheCallablesAccountReportsTheDefaultedLambda(t *testing.T) {
	t.Parallel()
	out, code := cliRunCode(t, "--callables", "f = lambda x, y=1: x + y\nprint(f(1))")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("the callable account does not parse: %v\n%s", err, out)
	}
	if doc["rewritten"] != true {
		t.Fatalf("the pass reports it did not rewrite a program that hoists a defaulted lambda:\n%s", out)
	}
	hoisted, _ := doc["hoisted_lambdas"].(float64)
	if hoisted < 1 {
		t.Fatalf("the pass hoisted no lambda from a program that has one:\n%s", out)
	}
	if refused, ok := doc["refused"].([]any); ok && len(refused) > 0 {
		t.Fatalf("the pass refused a shape it must now take (%v):\n%s", refused, out)
	}
	syms, _ := doc["symbols"].([]any)
	if len(syms) < 1 {
		t.Fatalf("no IR symbol was minted for the lambda:\n%s", out)
	}
}

// TestTheLambdaDoorAndTheDefDoorAreNotTheSameDoorThroughTheCLI is roadmap Gap R.280's witness at the altitude
// a caller reads. `Compile` sends both roads to codegen; the CLI runs the checker first, and there a `def`
// gets a front-end diagnostic with a file:line while a `lambda` gets a pass's vocabulary. Same exit code, two
// doors — which is why the row is filed rather than waved through as "both fail anyway".
func TestTheLambdaDoorAndTheDefDoorAreNotTheSameDoorThroughTheCLI(t *testing.T) {
	t.Parallel()
	_, defErr, defCode := cliStreams(t, "--eval", "def d(x, y=1):\n    return x + y\n\nprint(d())")
	if defCode == 0 || !strings.Contains(defErr, "expects 2 arguments") {
		t.Fatalf("the `def` road no longer reaches the front end for its arity (exit %d):\n%s", defCode, defErr)
	}
	_, lamErr, lamCode := cliStreams(t, "--eval", "f = lambda x, y=1: x + y\nprint(f())")
	if lamCode == 0 || !strings.Contains(lamErr, `missing argument "x"`) {
		t.Fatalf("the `lambda` road's arity answer changed (exit %d):\n%s — if both roads now reach the same door, close Gap R.280 and delete this case", lamCode, lamErr)
	}
	if strings.Contains(lamErr, "expects 2 arguments") {
		t.Fatalf("the `lambda` now reaches the front end like a `def`: Gap R.280 is closed — delete this case, do not edit it")
	}
}

// TestTheLambdaProgramMatchesTheReference is the whole row as one program, line for line against the answer
// CPython gave it. `testdata/lambda_default_param.expected` was written by CPython and never by this backend.
func TestTheLambdaProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/lambda_default_param.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/lambda_default_param.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/lambda_default_param.gy")
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
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

// TestTheLambdaProgramIsOnTheRecord is ADR 0302's rule at this altitude: the program's answer survives as a
// record, and a record that disappeared fails here rather than passing by having nothing to compare against.
func TestTheLambdaProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/lambda_default_param.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/lambda_default_param.gy is not on the record — record it from the reference with tools/recmerge, never from this backend")
	}
}
