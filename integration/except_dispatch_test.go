package integration

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap R.20 (ADR 0213) was a whole family hiding behind one symptom. The compiled `try` lowered
// only `Excepts[0]` and then cleared the exception flag, so: a matching arm in position two or
// three never ran; a bare `except:` after a typed arm never ran; a nested try never reached
// its outer arm; and an exception no arm matched was deleted — the program continued past the
// try, printed nothing, and exited 0. Every case below was measured against the interpreter and
// CPython first, and both were right, which is the shape of a bug that a two-engine parity check
// cannot see.

func TestExceptArmDispatchAgreesOnEveryPath(t *testing.T) {
	src := readProgram(t, "except_arm_order.gy")
	want := "second arm\nthird arm\nbare arm\nouter arm\nexception base\n"

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

// The promoted program is an ordinary parity case now; a ledger row would be a claim of
// divergence the harness must be able to falsify.
func TestExceptArmOrderCarriesNoOracleDebt(t *testing.T) {
	found := false
	for _, c := range conformanceCases() {
		if strings.HasSuffix(c.ID, "/except_arm_order") {
			found = true
		}
	}
	if !found {
		t.Fatal("except_arm_order is not in the conformance corpus")
	}
	if row, ok := oracleLedger["programs/except_arm_order"]; ok {
		t.Errorf("except_arm_order still carries an oracle row (%s); it matches CPython, delete it", row.reason)
	}
}

// An exception that no arm matches has to behave like the uncaught exception it is: reported,
// non-zero, and nothing printed after the try. This is the case that used to exit 0.
func TestUnhandledExceptionEscapingATryIsTheRuntimeClass(t *testing.T) {
	src := "try:\n    xs = [1]\n    print(xs[5])\nexcept KeyError:\n    print(\"wrong arm\")\nprint(\"after\")\n"
	_, pyErr, perr := lang.PythonRun(src)
	if perr == nil {
		t.Fatal("CPython accepted an uncaught IndexError")
	}
	if !strings.Contains(pyErr, "IndexError") {
		t.Fatalf("CPython said something else: %s", pyErr)
	}

	bin := cliBin(t)
	file := writeTrapCase(t, t.TempDir(), "escape.gy", src)
	for _, args := range [][]string{{"--aot", file}, {"--file", file}, {"--interp", file}} {
		cmd := exec.Command(bin, args...)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if exitOf(err) == 0 {
			t.Errorf("gustyc %v exited 0: the exception was swallowed (stdout %q)", args, stdout.String())
			continue
		}
		if stdout.String() != "" {
			t.Errorf("gustyc %v printed %q; the statement after the try must not run", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "IndexError") {
			t.Errorf("gustyc %v did not name the class on stderr: %q", args, stderr.String())
		}
	}
}

// A raise inside a handler is a fresh exception in the enclosing scope: the try that dispatched
// to that handler must not capture it.
func TestRaiseInsideAHandlerEscapes(t *testing.T) {
	src := "try:\n    xs = [1]\n    print(xs[5])\nexcept IndexError:\n    raise ValueError(\"from the handler\")\nprint(\"after\")\n"
	bin := cliBin(t)
	file := writeTrapCase(t, t.TempDir(), "handler_raises.gy", src)
	cmd := exec.Command(bin, "--aot", file)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if exitOf(err) == 0 {
		t.Fatalf("the handler's raise was swallowed: stdout %q", stdout.String())
	}
	if stdout.String() != "" {
		t.Errorf("stdout %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ValueError") {
		t.Errorf("want ValueError reported, got %q", stderr.String())
	}
}
