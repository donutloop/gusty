package integration

import (
	"path/filepath"
	"strings"
	"testing"
)

// Gap: None was the integer 0 in both backends, so `print(None)` printed 0, a procedure
// "returned" the value of its last statement, `f() == None` was false, and `gustyc --file`
// echoed a stray `0` after every program. ADR 0172 makes None a singleton; this file pins
// the answers Python gives — not merely that the two backends agree, because they happily
// agreed on the wrong answer before.

// noneExpected is CPython's own answer for the program, line for line. The three verdict
// lines used to read 1/0/1, which is what both backends printed before ADR 0257 gave a
// bool its rendering; the last 1 stays a 1 because that line prints `1 if ... else 0`.
const noneExpected = "None\nNone\nside\nNone\nside\nTrue\nFalse\nTrue\nfalsy\n2\nonce\n1\n"

func TestNoneValuesInterpreter(t *testing.T) {
	out := runInterp(t, readProgramSrc("none_values"))
	if out != noneExpected {
		t.Errorf("interpreter output =\n%s\nwant\n%s", out, noneExpected)
	}
}

func TestNoneValuesAOT(t *testing.T) {
	out := compileAndRun(t, readProgramSrc("none_values"))
	if out != noneExpected {
		t.Errorf("AOT output =\n%s\nwant\n%s", out, noneExpected)
	}
}

// TestNoneIsNotZero is the specific lie: None and the integer 0 must not be interchangeable.
func TestNoneIsNotZero(t *testing.T) {
	cases := []struct{ src, want string }{
		{"print(None)\n", "None\n"},
		{"x = None\nprint(x)\n", "None\n"},
		{"print(0 == None)\n", "False\n"},
		{"print(None == 0)\n", "False\n"},
		{"print(None == None)\n", "True\n"},
		{"def f():\n    x = 1\n\nprint(f() == None)\n", "True\n"},
		{"def f():\n    return 0\n\nprint(f() == None)\n", "False\n"},
		{"def f():\n    pass\n\nif f():\n    print(\"truthy\")\nelse:\n    print(\"falsy\")\n", "falsy\n"},
	}
	for _, tc := range cases {
		got := runInterp(t, tc.src)
		if got != tc.want {
			t.Errorf("interpreter %q = %q, want %q", tc.src, got, tc.want)
		}
		if out := compileAndRun(t, tc.src); out != tc.want {
			t.Errorf("AOT %q = %q, want %q", tc.src, out, tc.want)
		}
	}
}

// TestVoidCallSideEffectsStay: deciding `f() == None` statically must not delete the call,
// and printing a void call must still run it (output order is observable).
func TestVoidCallSideEffectsStay(t *testing.T) {
	src := "def emit():\n    print(\"side\")\n\nprint(emit())\nprint(emit() == None)\n"
	want := "side\nNone\nside\nTrue\n"
	got := runInterp(t, src)
	if got != want {
		t.Errorf("interpreter = %q, want %q", got, want)
	}
	if out := compileAndRun(t, src); out != want {
		t.Errorf("AOT = %q, want %q", out, want)
	}
}

// TestCLIEvalDoesNotEchoVoid: the CLI used to append the value print() returned (the int 0)
// to every program's stdout. Snippets still echo their value; programs do not.
func TestCLIEvalDoesNotEchoVoid(t *testing.T) {
	bin := cliBin(t)
	out, code := cliRunCode(t, "--file", writeSrc(t, filepath.Dir(bin), "prog.gy", "print(1)\n"))
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if strings.TrimRight(out, "\n") != "1" {
		t.Errorf("program stdout = %q, want just the program's own line \"1\"", out)
	}

	out, code = cliRunCode(t, "--eval", "x = 1 + 2\nx")
	if code != 0 || strings.TrimSpace(out) != "3" {
		t.Errorf("snippet echo broke: exit=%d out=%q, want 3", code, out)
	}

	out, code = cliRunCode(t, "--json", "--eval", "print(1)")
	if code != 0 {
		t.Fatalf("json eval exit = %d\n%s", code, out)
	}
	if !strings.Contains(out, `"result": null`) || !strings.Contains(out, `"type": "None"`) {
		t.Errorf(`void program should report {"result": null, "type": "None"}, got %s`, out)
	}
}
