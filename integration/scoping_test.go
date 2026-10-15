package integration

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Integration coverage for Gap R.24 (roadmap), ADR 0217: names bound inside compound statements
// belong to the enclosing function/module scope, because Python has one flat scope per def and
// per module. The checker analysed try/handler/finally/while/match bodies in child scopes it then
// discarded (and never walked `finally` at all), so `--check` said `undefined name` and the
// compiled backend refused programs the record ran and CPython agreed with. The fix is only
// worth anything if the SAME programs now run identically on the compiled path and match CPython.

func TestCompoundStatementBindingsRunOnTheCompiledBackend(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			"try body, every path assigns",
			"def pick() -> int:\n    try:\n        a = 7\n    except:\n        a = 0\n    return a\n\nprint(pick())\n",
			"7\n",
		},
		{
			"name assigned in the try body, read after",
			"try:\n    t = 2\nexcept:\n    t = 3\nprint(t)\n",
			"2\n",
		},
		{
			"name assigned in the finally clause, read after",
			"try:\n    pass\nexcept:\n    pass\nfinally:\n    v = 4\nprint(v)\n",
			"4\n",
		},
		{
			"finally clause still runs when the try raises",
			"ran = 0\ntry:\n    x = 1 // 0\nexcept:\n    pass\nfinally:\n    ran = 1\nprint(ran)\n",
			"1\n",
		},
		{
			"name first assigned in a while body",
			"n = 0\nwhile n < 3:\n    k = n * 10\n    n = n + 1\nprint(k)\n",
			"20\n",
		},
		{
			"name assigned in both match arms",
			"z = 1\nmatch z:\n    case 1:\n        m = 11\n    case _:\n        m = 22\nprint(m)\n",
			"11\n",
		},
		{
			"try inside a while, both paths assign",
			"i = 0\ns = 0\nwhile i < 3:\n    try:\n        s = s + i\n    except:\n        s = 0\n    i = i + 1\nprint(s)\n",
			"3\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			iout := runCompiled(t, tc.src)
			if iout != tc.want {
				t.Fatalf("interpreter printed %q, want %q", iout, tc.want)
			}
			aout, err := runAOTWithTimeout(t, tc.src, 60*time.Second)
			if err != nil {
				t.Fatalf("compiled program failed: %v\n%s", err, aout)
			}
			if aout != tc.want {
				t.Fatalf("compiled program printed %q, want %q", aout, tc.want)
			}
			pout, perrStr, perr := lang.PythonRun(tc.src)
			if perr != nil {
				t.Fatalf("CPython disagreed with the expectation %q: %v\n%s", tc.want, perr, perrStr)
			}
			if pout != tc.want {
				t.Fatalf("CPython printed %q, want %q — the expectation itself is wrong", pout, tc.want)
			}
		})
	}
}

func TestUnboundAfterPartialMatchTrapsLikeCPythonOnTheCompiledBackend(t *testing.T) {
	t.Parallel()
	// The other half of the visibility rule: making a name visible must not invent a value.
	// Only `case y:` would bind it, the literal arm runs, and reading it afterwards is a
	// NameError — the class CPython raises. Widening the checker's scope did not paper over a
	// real bug, and that is the assertion that matters here.
	//
	// This used to be a reference-debt row: the compiled leg printed `16` — the module slot's
	// leftover word — and exited 0 while the reference raised. A capture now binds in the arm the
	// branch ENTERED (ADR 0358, which closes Gap R.68 and the match row of Gap R.36), both legs
	// raise the reference's own class and message, and the ledger row that described the garbage is
	// deleted rather than re-measured. What is asserted below is the parity itself; the reason it is
	// spelled out here instead of going through `requireReferenceTrapOrHonestRefusal` is that the
	// trap's sentence is on stderr, and a case that wants to say both "it raised the right thing"
	// and "it printed nothing" needs the two streams kept apart.
	//
	// This leg goes through the real CLI: `runCompiled` treats any eval error as a test failure,
	// and here the trap IS the expected outcome.
	src := "z = 1\nmatch z:\n    case 1:\n        pass\n    case y:\n        pass\nprint(y)\n"
	const trap = "NameError: name 'y' is not defined"

	// The record leg holds this answer too (ADR 0302). Asking first is what keeps the case from
	// rotting into a compiled-only assertion if the entry is ever deleted.
	if !lang.HasGoldenAnswer(src) {
		t.Fatalf("the record holds nothing for %q — one witness leg is not a parity claim", src)
	}

	path := filepath.Join(t.TempDir(), "unbound_match.gy")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(cliBin(t), "--file", path)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc --file %s: %v", path, runErr)
		}
		code = ee.ExitCode()
	}
	if code != 3 {
		t.Fatalf("compiled exit %d, want 3 (an uncaught trap, ADR 0211)\nstdout %q\nstderr %q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), trap) {
		t.Fatalf("the compiled trap does not carry the reference's sentence\nstderr %q", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("compiled printed %q before trapping — the read must raise, not answer the slot", stdout.String())
	}

	py, pyErrText, perr := lang.PythonRun(src)
	if perr == nil {
		t.Fatalf("CPython accepted an unbound read, so our expectation is wrong: %q", py)
	}
	if !strings.Contains(pyErrText, "NameError") {
		t.Fatalf("CPython trapped with something other than NameError: %s", pyErrText)
	}
	if strings.TrimSpace(py) != "" {
		t.Fatalf("CPython printed %q before trapping", py)
	}
}

// TestUnboundAfterPartialMatchTrapIsEmittedNotSkipped is the same program one layer in, through the
// compiler API rather than the CLI. Gap R.36's own note asked for its pinned-garbage test to be
// deleted once the compiled leg trapped; this is what replaces it — the IR is asserted to carry the
// trap and the written-flag test in front of the read, which is the shape the defect lacked. An
// uncaught trap is an ordinary typed raise (ADR 0212), so the module still has to verify.
func TestUnboundAfterPartialMatchTrapIsEmittedNotSkipped(t *testing.T) {
	t.Parallel()
	src := "z = 1\nmatch z:\n    case 1:\n        pass\n    case y:\n        pass\nprint(y)\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := lang.VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("module does not verify: %v\n%s", err, res.IR)
	}
	if !strings.Contains(res.IR, "NameError: name 'y' is not defined") {
		t.Fatalf("the module carries no NameError sentence, so the read would answer the slot\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "%bnd_y = alloca i8") || !strings.Contains(res.IR, "load i8, i8* %bnd_y") {
		t.Fatalf("the read is not guarded by a written-flag test (ADR 0228)\n%s", res.IR)
	}
}
