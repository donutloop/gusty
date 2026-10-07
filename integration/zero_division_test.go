package integration

import (
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap R.18 (ADR 0212) in the harness's own terms: division by zero is one behaviour across the
// interpreter, the compiled binary and CPython — raised, catchable, worded the way the
// reference words it. The corpus program covers the six arithmetic shapes plus a divisor
// computed at run time, because a guard that only works for constants guards nothing.

func TestZeroDivisionAgreesOnEveryPath(t *testing.T) {
	src := readProgram(t, "zero_division.gy")
	want := "2.0\n2\n3\ncaught division\ncaught modulo\ncaught floor division\ncaught float modulo\ncaught a divisor computed at run time\n"

	lang.RecordedStdoutIs(t, src, want)
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

// TestUncaughtDivisionByZeroIsTheRuntimeClass checks the trap against the exit-code contract
// as well: an uncaught ZeroDivisionError is class 3 on both run paths (ADR 0211 made the
// compiled path able to say so), and CPython calls it an error too.
func TestUncaughtDivisionByZeroIsTheRuntimeClass(t *testing.T) {
	src := "x = 6\nprint(x / 0)\n"

	bin := cliBin(t)
	dir := t.TempDir()
	file := writeTrapCase(t, dir, "divzero.gy", src)
	for _, args := range [][]string{{"--aot", file}, {"--file", file}, {"--aot", file}} {
		if code := runCode(t, bin, args...); code != 3 {
			t.Errorf("gustyc %v: exit = %d, want 3 (an uncaught ZeroDivisionError is the runtime class)", args, code)
		}
	}
	_, pyErr, perr := lang.PythonRun(src)
	if perr == nil {
		t.Fatal("CPython accepted a division by zero")
	}
	if !strings.Contains(pyErr, "ZeroDivisionError") {
		t.Fatalf("CPython said something else: %s", pyErr)
	}
}

// The measured failure this cycle exists to retire: an unguarded `srem`/`fdiv` produced a
// value instead of a trap, so the program printed a number — `inf` for the float case, and a
// fresh garbage integer each run for the modulo case — and exited 0. The assertion is on
// stdout alone: the traceback belongs to stderr, and a trap that puts anything on stdout is
// still answering the question it was supposed to refuse.
func TestCompiledDivisionNeverPrintsGarbage(t *testing.T) {
	bin := cliBin(t)
	dir := t.TempDir()
	for i, src := range []string{
		"x = 7\nprint(x % 0)\n",
		"x = 7\nprint(x / 0)\n",
		"x = 7\nprint(x // 0)\n",
		"print(7.0 % 0)\n",
		"def z():\n    return 0\n\nx = 7\nprint(x % z())\n",
	} {
		file := writeTrapCase(t, dir, "garbage"+string(rune('a'+i))+".gy", src)
		cmd := exec.Command(bin, "--aot", file)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		if exitOf(err) == 0 {
			t.Errorf("compiled program exited 0 and printed %q for a division by zero:\n%s", stdout.String(), src)
			continue
		}
		if stdout.String() != "" {
			t.Errorf("the trap printed %q on stdout — a trap does not print:\n%s", stdout.String(), src)
		}
		if !strings.Contains(stderr.String(), "ZeroDivisionError") {
			t.Errorf("the trap should name ZeroDivisionError on stderr, got %q:\n%s", stderr.String(), src)
		}
	}
}
