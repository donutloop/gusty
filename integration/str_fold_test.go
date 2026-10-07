package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// str()/print() of the values whose text does not depend on a bool tag (ADR 0183). The
// expectation column is CPython's own output: `str(None)` is "None" and not "0", the
// empty-set rule and float texts are the language's, not a backend's. The compiled backend must
// reproduce it, and a future `--aot` change that re-breaks one of these rows fails here
// rather than in a user's terminal.
//
// Bools are deliberately absent: `print(True)` prints `1` on the compiled path because bools
// are not values yet in either (roadmap L11.1), which is tracked as gated there rather
// than half-fixed here.
func TestStrFormsMatchCPythonOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"print(str(None))\n", "None\n"},
		{"print(str(42), str(-7), str(0))\n", "42 -7 0\n"},
		{"print(str(1.5), str(2.0))\n", "1.5 2.0\n"},
		{`print(str("x"), str(""))` + "\n", "x \n"},
		{"print(len(str(None)), len(str(42)))\n", "4 2\n"},
		{"s = str(None)\nprint(s)\n", "None\n"},
		{`q = str("x")` + "\nprint(q)\n", "x\n"},
		{"w = str(42)\nprint(w)\nprint(len(w))\n", "42\n2\n"},
		{`print("v=" + str(None))`, "v=None\n"},
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}

// A module must never store a string global into a value slot — the shape llc rejects and
// that used to be what `s = str(None)` compiled to. Asserted module-wide over a program
// that mixes str(), printing and length so a regression is a compiler-visible failure.
func TestNoGlobalStoredInValuePosition(t *testing.T) {
	res, err := lang.Compile("s = str(None)\nt = str(1.5)\nprint(s, t, len(s))\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, line := range strings.Split(res.IR, "\n") {
		if strings.Contains(line, "store i32 @") {
			t.Fatalf("emitted IR stores a global in value position: %s", strings.TrimSpace(line))
		}
	}
}
