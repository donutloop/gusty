package integration

// The CLI half of the printf-style `%` family (roadmap L11.2, Gap R.165, ADR 0282). The compiled leg
// printed `0.0` at **exit 0** for `print("%.2f" % 3.5)` — CPython formats it as `3.50` and the
// interpreter raises `TypeError`, so neither engine answers a number and the compiled one printed one.
// What ships instead is exit 1 naming the format string and the reference's own answer, with the
// numeric `%` controls pinned beside it: a guard that banned the operator would pass one table and
// fail the other. Exit 2 is forbidden throughout — `llc` rejecting a module for an ordinary program is
// the compiler's bug, not the program's (ADR 0166).

import (
	"strings"
	"testing"
)

func TestTextLeftPercentRefusesAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, refusal string }{
		{"the row: %.2f over a double", "print(\"%.2f\" % 3.5)\n", "printf"},
		{"%.1f over a double", "print(\"%.1f\" % 3.14159)\n", "printf"},
		{
			// `%f` over an *int* is refused by the i32 road (Gap R.82's sentence), not by the double
			// lift this family is about: no `floatValue` is reached when neither operand is a float.
			// Both are exit 1 and both are honest; only the wording's owner differs.
			"%f over an int", "print(\"%f\" % 3)\n", ""},
		{"%d with a text left", "print(\"%d items\" % 3)\n", ""},
		{"%s with a text left", "print(\"%s!\" % \"hi\")\n", ""},
		{"in a function's return", "def f(v):\n    return \"%.2f\" % v\n\nprint(f(3.5))\n", ""},
		{"bound to a name first", "s = \"%.2f\" % 3.5\nprint(s)\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "percent.gy", tc.src)
			// The reference is recorded in the failure text, so a row that stops being a refusal can
			// be read against CPython without re-deriving it by hand.
			py, _ := cpythonPlainOut(t, dir, tc.src)
			out, code := cliRunMerged(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2 where the front end should refuse (ADR 0166):\n%s", out)
			}
			if code == 0 {
				t.Fatalf("--aot answered a text-left %% with %q; CPython says %q and the record raises, so a number at exit 0 is the wrong answer (Gap R.165)\nsrc: %s", out, py, tc.src)
			}
			if code != 1 {
				t.Fatalf("--aot: exit %d, want the front-end refusal's 1\noutput: %s\nsrc: %s", code, out, tc.src)
			}
			if !strings.Contains(out, "codegen") {
				t.Errorf("the refusal must name the pass that declined, got: %s", out)
			}
			if tc.refusal != "" && !strings.Contains(out, tc.refusal) {
				t.Errorf("the refusal must name %q, got: %s", tc.refusal, out)
			}
		})
	}
}

func TestRemainderAtTheCLIIsUntouchedByThePercentGuard(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"int remainder", "print(7 % 3)\nprint(-7 % 3)\n", "1\n2\n"},
		{"float remainder", "print(7.5 % 2)\nprint(-7.5 % 2)\n", "1.5\n0.5\n"},
		{"both double", "print(7.5 % 2.5)\n", "0.0\n"},
		{"through a parameter, both kinds", "def f(v):\n    return v % 2\n\nprint(f(7.5))\nprint(f(7))\n", "1.5\n1\n"},
		{"float equality with a text still answers", "print(1 if 1.0 == \"a\" else 0)\nprint(1 if \"a\" == 1.0 else 0)\n", "0\n0\n"},
		{"a float still prints", "print(1.5 + 2)\nprint(7 / 2)\n", "3.5\n3.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "percent_control.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("the pinned expectation is not the reference's: cpython %q, table %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunMerged(t, engine, "--file", gy)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q (cpython agrees: %q)", engine, out, tc.want, py)
				}
			}
		})
	}
}
