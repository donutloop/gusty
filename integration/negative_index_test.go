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

// L11.4 (ADR 0210) is the subscript rule: a negative index counts from the end for
// anything positional, and stays a key for anything keyed. The corpus programs run on all
// three engines here, because the whole point of closing this gap is that the two backends
// and the reference implementation now answer the same question the same way.

func TestNegativeSubscriptsAgreeOnEveryPath(t *testing.T) {
	src := readProgram(t, "negative_index.gy")
	want := "3\n1\n2\n30\n2\nminus\nzero\n"

	if got := runInterp(t, src); got != want {
		t.Errorf("interpreted output =\n%q\nwant\n%q", got, want)
	}
	built, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// The literal shape is the one that used to end the compiler's life rather than the
// program's: `print([1, 2, 3][-1])` made irGen.value index a Go slice with -1, and the
// tool died with a Go panic. Under the exit-code contract (ADR 0168) no tested shape may
// leave the compiler as a panic, so this drives the real binary and insists on IR.
func TestNegativeLiteralSubscriptEmitsIRInsteadOfPanicking(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	out, err := exec.Command(bin, "--emit-llvm", "print([1, 2, 3][-1])\n").CombinedOutput()
	if err != nil {
		t.Fatalf("compiler died on a negative constant subscript: %v\n%s", err, out)
	}
	text := string(out)
	if strings.Contains(text, "panic:") || strings.Contains(text, "index out of range [-1]") {
		t.Fatalf("compiler panic text reached the user:\n%s", text)
	}
	if !strings.Contains(text, "define") || !strings.Contains(text, "@main") {
		t.Fatalf("no module came out of a valid program:\n%s", text)
	}
}

// The promoted programs are ordinary parity cases now: registered in the corpus, with no
// ledger row — a row would be a claim of divergence the harness has to be able to falsify,
// and these no longer diverge.
func TestNegativeIndexProgramsCarryNoOracleDebt(t *testing.T) {
	for _, name := range []string{"negative_index", "negative_literal_index"} {
		found := false
		for _, c := range conformanceCases() {
			if strings.HasSuffix(c.ID, "/"+name) {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s is not in the conformance corpus", name)
		}
		if row, ok := oracleLedger["programs/"+name]; ok {
			t.Errorf("%s still carries an oracle row (%s): it matches CPython, delete the row", name, row.reason)
		}
	}
}

// negInterpErr runs the evaluator with no front-end gate and returns only the error, so a
// trap can be inspected rather than printed.
func negInterpErr(t *testing.T, src string) error {
	t.Helper()
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	old := os.Stdout
	_, r, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("pipe: %v", perr)
	}
	os.Stdout = r
	ev := lang.NewEvaluator()
	_, evalErr := ev.EvalProgram(prog)
	os.Stdout = old
	r.Close()
	return evalErr
}

// An index past either end is still an error, and the compiled binary has to say so the
// way the interpreter and CPython do — normalisation must not have swallowed the check.
func TestNegativeIndexOutOfRangeIsSaidEverywhere(t *testing.T) {
	src := "xs = [1, 2, 3]\nprint(xs[-4])\n"
	_, pyErr, perr := lang.PythonRun(src)
	if perr == nil {
		t.Fatal("CPython accepted xs[-4]")
	}
	if !strings.Contains(pyErr, "IndexError") {
		t.Fatalf("CPython said something else: %s", pyErr)
	}
	// The trap has to be *said*. Note what is deliberately not asserted here: the exit
	// status. The compiled binary dies with 1 on an uncaught exception while the
	// interpreter path reports 3, and `gustyc --aot` reports 0 for both — that split is
	// Gap R.17's own subject, and pinning it in a subscript test would freeze it in place.
	compiled, cerr := runAOTWithTimeout(t, src, 120*time.Second)
	if cerr == nil && !strings.Contains(compiled, "IndexError") {
		t.Fatalf("compiled run accepted xs[-4] and printed %q", compiled)
	}
	if !strings.Contains(compiled, "IndexError") {
		t.Fatalf("compiled run did not say IndexError: %q (%v)", compiled, cerr)
	}
	if ierr := negInterpErr(t, src); ierr == nil || !strings.Contains(ierr.Error(), "index out of range") {
		t.Fatalf("interpreter accepted xs[-4]: %v", ierr)
	}
}
