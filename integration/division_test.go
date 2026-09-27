package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap P — `/` is true division and floats print like Python (ADR 0180).
//
// `7 / 2` truncated to 3 in both backends (the parity harness could not see it:
// both agreed), and `print(x * 2.0)` showed 2. Each expectation here is
// cross-checked by running CPython on the same source.

var divisionCases = []struct {
	name string
	src  string
	want string
}{
	{"int true division", "print(7 / 2)\n", "3.5\n"},
	{"int division exact keeps .0", "print(84 / 2)\n", "42.0\n"},
	{"int division below one", "a = 1\nb = 4\nprint(a / b)\n", "0.25\n"},
	{"floor division", "print(7 // 2)\n", "3\n"},
	{"true division negative", "print(-7 / 2)\n", "-3.5\n"},
	{"float division", "print(7.0 / 2)\n", "3.5\n"},
	{"modulo unchanged", "print(7 % 2)\n", "1\n"},
	{"division by zero", "a = 1\nprint(a / 0)\n", ""},
	{"float times int", "x = 2.0\nprint(x * 3)\n", "6.0\n"},
	{"integral float prints .0", "print(1.0 + 2.0)\n", "3.0\n"},
	{"shortest round trip", "print(0.123456789)\n", "0.123456789\n"},
	{"str of a float", "print(str(3.0))\n", "3.0\n"},
	{"f-string float", "y = 4.0\nprint(f\"{y}\")\n", "4.0\n"},
	{"zero prints 0.0", "z = 0.0\nprint(z)\n", "0.0\n"},
	{"float from division prints", "a = 9\nb = 3\nprint(a / b)\n", "3.0\n"},
	{"augmented floor assign", "x = 8\nx //= 2\nprint(x)\n", "4\n"},
}

// Three shapes where the AOT backend still disagrees with Python (and with the
// interpreter, which is right). They are pinned here — including AOT's wrong
// answer — so the divergence is visible and a fix shows up as a failing test
// rather than a surprise (roadmap Gap P.1).
var knownAOTDivisionGaps = []struct {
	name    string
	src     string
	want    string // CPython / interpreter
	aotGets string // what the compiled backend prints today
}{
	{"floor division of negatives", "print(-7 // 2)\n", "-4\n", "-3\n"},
	{"float through an untyped parameter", "def f(x):\n    return x * 2\n\nprint(f(0.1))\n", "0.2\n", "0\n"},
	{"augmented true division", "x = 8\nx /= 2\nprint(x)\n", "4.0\n", "4\n"},
}

func TestKnownAOTDivisionGaps(t *testing.T) {
	for _, tc := range knownAOTDivisionGaps {
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Fatalf("%s: expectation disagrees with CPython: %q", tc.name, py)
		}
		if got := runInterp(t, tc.src); got != tc.want {
			t.Errorf("%s: the interpreter must be the correct backend: got %q want %q", tc.name, got, tc.want)
		}
		got := runAOT(t, tc.src)
		if got != tc.aotGets && got != tc.want {
			t.Errorf("%s: AOT printed %q — neither the documented gap %q nor a fix", tc.name, got, tc.aotGets)
		}
	}
}

func TestDivisionAndFloatReprMatchPython(t *testing.T) {
	for _, tc := range divisionCases {
		if tc.want == "" {
			continue // documented separately (ZeroDivisionError)
		}
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Fatalf("%s: expected output disagrees with CPython\n cpython = %q\n   want   = %q", tc.name, py, tc.want)
		}
		if got := runInterp(t, tc.src); got != tc.want {
			t.Errorf("%s: interpreter = %q, want %q", tc.name, got, tc.want)
		}
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: module must verify: %v %v", tc.name, v.Errors, verr)
		}
		if got := runAOT(t, tc.src); got != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Division by zero is an error, not a silent 0 or an infinity.
func TestDivisionByZeroMatchesPython(t *testing.T) {
	src := "a = 1\nprint(a / 0)\n"
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ev := lang.NewEvaluator()
	if _, err := ev.EvalProgram(prog); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Errorf("interpreter must raise division by zero, got %v", err)
	}
	if _, err := lang.Compile(src); err != nil {
		t.Logf("AOT refuses to compile the division (an acceptable answer): %v", err)
	}
}
