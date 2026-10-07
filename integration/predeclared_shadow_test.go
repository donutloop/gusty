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
// line a call reaches the built-in, and the compiled backend — which emits every function before the
// module body — resolves the call to the program's own definition. One source file, two possible
// answers (roadmap Gap R.12, ADR 0205). The checker refuses the program, and these tests keep the
// refusal honest in both directions: it must fire where the meaning really is undetermined, and it
// must not fire on the ordered shape that has one answer.
//
// The guard used to read "and the two engines really do disagree", which is the cleanest possible
// justification a refusal can have: the same source produced two answers. With one backend the second
// opinion is CPython's, and the question stays the same — is there a defensible single answer here, or
// does the meaning depend on an ordering the language never fixed? If the compiled program and CPython
// ever print the same thing, the refusal is masking a program that means something, and it should be
// deleted rather than left to rot.

const r12Ambiguous = "for i in range(2):\n    print(i * 100)\n\n\ndef range(x):\n    return x * 3\n\n\nprint(range(4))\n"

// TestTheRefusedProgramReallyIsAmbiguous is the justification for the refusal, measured rather than
// asserted. The compiled build path — what the refusal protects callers from — iterates the program's
// own `range(2)`, which returns the single value 6, so the loop body runs six times; CPython, reading
// the same text top to bottom, reaches the built-in and runs it twice. Two answers to one program is
// the situation the checker refuses rather than chooses between, and this is the case that keeps the
// refusal honest: if the two ever agree, the rule has outlived its reason.
func TestTheRefusedProgramReallyIsAmbiguous(t *testing.T) {
	compiled, err := runAOTWithTimeout(t, r12Ambiguous, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	pyOut, pyErr, perr := lang.PythonRun(r12Ambiguous)
	if perr != nil {
		t.Skipf("no usable oracle to compare against: %v\n%s", perr, pyErr)
	}
	if pyOut == compiled {
		t.Errorf("the refusal of %q is no longer justified: the compiled program and CPython both print %q", r12Ambiguous, compiled)
	}
	// And the disagreement is the one the gap describes, in shape as well as in value: three lines out
	// of CPython (two iterations plus the closing print), more out of the compiled program, which read
	// `range(2)` as the program's own definition and iterated its result as a count.
	if got := strings.Count(pyOut, "\n"); got != 3 {
		t.Errorf("the reference answer changed shape — this test is pointed at the wrong program: %q", pyOut)
	}
	if strings.Count(compiled, "\n") <= strings.Count(pyOut, "\n") {
		t.Errorf("expected the compiled run to iterate more times than CPython:\n python   %q\n compiled %q", pyOut, compiled)
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
		t.Fatalf("--aot built a program whose answer depends on the order its definitions happen to be emitted in:\n%s", out)
	}
}

// TestOrderedDefinitionRunsOnEveryPath is the over-refusal guard: the same vocabulary, in the order
// the language does fix, must compile, run, and mean the same thing on every path it is asked — the
// compiled run, the linked binary, and CPython.
func TestOrderedDefinitionRunsOnEveryPath(t *testing.T) {
	src := `def range(x):
    return x * 3


def scale(v):
    return range(v)


print(range(4))
print(scale(5))
`
	want := "12\n15\n"
	lang.RecordedStdoutIs(t, src, want)
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
	lang.RecordedStdoutIs(t, src, want)
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
