package integration

import (
	"os"
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
// compiled backend refused programs the interpreter ran and CPython agreed with. The fix is only
// worth anything if the SAME programs now run identically on both backends and match CPython.

func TestCompoundStatementBindingsRunOnBothBackends(t *testing.T) {
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
			iout := runInterp(t, tc.src)
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

func TestUnboundAfterPartialMatchTrapsLikeCPythonInInterpreter(t *testing.T) {
	// The other half of the visibility rule: making a name visible must not invent a value.
	// Only `case y:` would bind it, the literal arm runs, and reading it afterwards is a
	// NameError — the class CPython raises. Widening the checker's scope did not paper over a
	// real bug, and that is the assertion that matters here.
	//
	// This leg goes through the real CLI: `runInterp` treats any eval error as a test failure,
	// and here the trap IS the expected outcome.
	src := "z = 1\nmatch z:\n    case 1:\n        pass\n    case y:\n        pass\nprint(y)\n"
	path := filepath.Join(t.TempDir(), "unbound_match.gy")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if iout, code := cliRunCode(t, "--file", path); iout != "" || code == 0 {
		t.Fatalf("interpreter leg printed %q with exit %d; the name is unbound on this path", iout, code)
	}
	if combined := cliRun(t, "--file", path); !strings.Contains(combined, "NameError") {
		t.Fatalf("interpreter did not raise NameError: %s", combined)
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

// TestUnboundAfterPartialMatchReadsGarbageInCompiled pins a KNOWN compiled-backend defect, roadmap
// Gap R.36: a module-level slot whose only assignment never ran is loaded as-is and printed, so the
// program reports a number the source never wrote instead of trapping. The interpreter leg above is
// correct; only this leg is wrong, which is why it is asserted rather than skipped.
//
// DELETE THIS TEST when Gap R.36 is fixed, and fold the program into the interpreter test above:
// once the compiled leg traps with NameError too, asserting the garbage would be asserting a bug
// that no longer exists.
func TestUnboundAfterPartialMatchReadsGarbageInCompiled(t *testing.T) {
	src := "z = 1\nmatch z:\n    case 1:\n        pass\n    case y:\n        pass\nprint(y)\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, err := lang.VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("module does not verify: %v", err)
	}
	out, cerr := runAOTWithTimeout(t, src, 60*time.Second)
	if cerr != nil {
		t.Fatalf("compiled leg trapped — if it now raises NameError, Gap R.36 is fixed and this test must go: %v\n%s", cerr, out)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatalf("compiled leg printed nothing; if it now traps, Gap R.36 is fixed — see this test's note")
	}
	// The defect's signature is that it prints something confidently. There is no correct value to
	// compare against — the arm that would bind `y` did not run — so the one thing that must never
	// appear is a plausible answer: 1 is `z`, not `y`.
	if strings.TrimSpace(out) == "1" {
		t.Fatalf("compiled leg printed %q, which is not a possible value of y", out)
	}
}
