package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// A module may claim a built-in name (ADR 0199) but only from its definition onwards: above that
// line the interpreter reaches the built-in, and the compiled backend — which emits every function
// before the module body — resolves the call to the program's own definition. One source file, two
// answers (roadmap Gap R.12, ADR 0205). The checker now refuses the program, and these tests keep the
// refusal honest in both directions: it must fire on a real divergence, and it must not fire on the
// ordered shape that both engines already agree on.

// rawEvalOutput runs the evaluator directly, with no front-end gate: this test has to see what each
// engine does with the source, not what the compiler allows it to do with.
func rawEvalOutput(t *testing.T, src string) string {
	t.Helper()
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	ev := lang.NewEvaluator()
	_, evalErr := ev.EvalProgram(prog)
	os.Stdout = old
	w.Close()
	buf := make([]byte, 1<<20)
	n, _ := r.Read(buf)
	if evalErr != nil {
		t.Fatalf("interpreter: %v", evalErr)
	}
	return string(buf[:n])
}

const r12Ambiguous = "for i in range(2):\n    print(i * 100)\n\n\ndef range(x):\n    return x * 3\n\n\nprint(range(4))\n"

// TestBackendsGenuinelyDifferOnTheRefusedProgram is the justification for the refusal, measured
// rather than asserted: run the same source through each engine directly — around the CLI gate — and
// require that they still disagree. If this test ever goes green by agreeing, the refusal has become
// unnecessary and should be deleted rather than left to rot.
func TestBackendsGenuinelyDifferOnTheRefusedProgram(t *testing.T) {
	interpreted := rawEvalOutput(t, r12Ambiguous)
	compiled, err := runAOTWithTimeout(t, r12Ambiguous, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if interpreted == compiled {
		t.Errorf("the refusal of %q is no longer justified: both backends print %q", r12Ambiguous, interpreted)
	}
	// And the disagreement is the one the gap describes: the interpreter used the built-in range
	// (two iterations), the compiled program iterated the program's `range(2)` = 6 as a count.
	if got := strings.Count(interpreted, "\n"); got != 3 {
		t.Errorf("interpreted output changed shape: %q", interpreted)
	}
	if strings.Count(compiled, "\n") <= strings.Count(interpreted, "\n") {
		t.Errorf("expected the compiled run to iterate more times than the interpreted one:\n interp  %q\n compiled %q", interpreted, compiled)
	}
}

// TestTheRefusalIsReachableFromTheCLI checks that an agent actually meets the diagnostic, on both
// gates: the check path and the build path.
func TestTheRefusalIsReachableFromTheCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	path := filepath.Join(dir, "prog.gy")
	if err := os.WriteFile(path, []byte(r12Ambiguous), 0o600); err != nil {
		t.Fatal(err)
	}

	out, _ := exec.Command(bin, "--check", r12Ambiguous).CombinedOutput()
	text := string(out)
	if !strings.Contains(text, `"range" is a built-in here`) {
		t.Fatalf("--check did not refuse the ambiguous call:\n%s", text)
	}
	if !strings.Contains(text, "move the definition above every use") {
		t.Errorf("the refusal does not say what to do about it:\n%s", text)
	}

	if out, err := exec.Command(bin, "--aot", path).CombinedOutput(); err == nil {
		t.Fatalf("--aot built a program whose two backends disagree:\n%s", out)
	}
}

// TestOrderedDefinitionRunsOnEveryPath is the over-refusal guard: the same vocabulary, in the order
// both engines agree on, must compile and run identically.
func TestOrderedDefinitionRunsOnEveryPath(t *testing.T) {
	src := `def range(x):
    return x * 3


def scale(v):
    return range(v)


print(range(4))
print(scale(5))
`
	want := "12\n15\n"
	if got := runInterp(t, src); got != want {
		t.Errorf("interpreted output =\n%q\nwant\n%q", got, want)
	}
	compiled, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if compiled != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", compiled, want)
	}
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// TestMethodNamedAfterABuiltInDoesNotTripTheRule is the class-body counterpart: a method called
// `range` is a method, not a module binding, and must leave the built-in loop working.
func TestMethodNamedAfterABuiltInDoesNotTripTheRule(t *testing.T) {
	src := `class Counter:
    def range(self, n):
        return n * 4


for i in range(2):
    print(i * 10)
`
	want := "0\n10\n"
	if got := runInterp(t, src); got != want {
		t.Errorf("interpreted output =\n%q\nwant\n%q", got, want)
	}
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range lang.Analyze(prog) {
		if d.Level == lang.LevelError {
			t.Errorf("a method named after a built-in was refused: %v", d)
		}
	}
}
