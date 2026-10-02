package integration

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// semicolon_entry_points_test.go — one program, every entry point, one verdict (roadmap Gap R.72,
// ADR 0242).
//
// `x = 5; print(x+1)` used to be three different programs depending on which flag asked. The interpreter
// printed 6; the JIT/AOT refused it with `unexpected character ";"`; `--emit-llvm` emitted a module that
// `llc` accepted and never mentioned the diagnostic. The cause was a lexer with no `;` case, whose
// "unexpected character" token was turned into an error diagnostic that each entry point weighed
// differently — and the fix is that a separator is a token, so nobody has an opinion to disagree about.
//
// The definition of done is not "the message reads better" (that was ADR 0240). It is this table: the
// same separated program and its newline-separated control get the same exit code and the same stdout
// from every door, and a program CPython rejects is rejected by every door too.

func TestSemicolonProgramsGetOneVerdictFromEveryEntryPoint(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"two_statements", "x = 5; print(x + 1)\n", "6\n"},
		{"three_statements", "a = 1; b = 2; print(a + b)\nprint(\"done\");\n", "3\ndone\n"},
		{"inline_suite_statements", "x = 1\nif x: print(\"in\"); print(\"body\")\nprint(\"after\")\n", "in\nbody\nafter\n"},
		{"control_without_semicolon", "x = 5\nprint(x + 1)\n", "6\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "semi.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			legs := []struct {
				name string
				args []string
				path bool
			}{
				{"--interp --file", []string{"--interp", "--file"}, true},
				{"--aot --file", []string{"--aot", "--file"}, true},
				{"--jit --file", []string{"--jit", "--file"}, true},
				{"--eval", []string{"--eval"}, false},
			}
			for _, leg := range legs {
				var args []string
				if leg.path {
					args = append(append([]string{}, leg.args...), path)
				} else {
					args = append(append([]string{}, leg.args...), tc.src)
				}
				out, code := cliRunCode(t, args...)
				if code != 0 {
					t.Errorf("%s exited %d for a program CPython prints %q: %s", leg.name, code, tc.want, cliRun(t, args...))
					continue
				}
				if out != tc.want {
					t.Errorf("%s printed %q, want CPython's %q", leg.name, out, tc.want)
				}
			}
			// The REPL echoes each statement's value as well as running it; the answer must be
			// there, and the separator must not surface as an error.
			repl := replOutput(t, tc.src)
			if strings.Contains(repl, "error") || strings.Contains(repl, "unexpected character") {
				t.Errorf("--repl reported an error for a separator: %q", repl)
			}
			for _, want := range strings.Split(strings.TrimRight(tc.want, "\n"), "\n") {
				if !strings.Contains(repl, want) {
					t.Errorf("--repl output %q is missing %q", repl, want)
				}
			}
			// --emit-llvm: no run, but the same verdict about the program — it compiles, and the
			// module verifies. A refusal here would be the old disagreement in a new coat.
			out, code := cliRunCode(t, "--emit-llvm", tc.src)
			if code != 0 {
				t.Errorf("--emit-llvm exited %d: %s", code, out)
			}
			if strings.Contains(out, "unexpected character") {
				t.Errorf("--emit-llvm reported the old separator diagnostic: %q", out)
			}
		})
	}
}

// TestSemicolonRejectsTheEmptyStatementEverywhere is the other half of one verdict: the empty statement
// (`x = 1;;y = 2`) is a syntax error CPython rejects, so no door may run it, and none may report a
// different answer from its neighbour.
func TestSemicolonRejectsTheEmptyStatementEverywhere(t *testing.T) {
	const src = "a = 1;;b = 2\nprint(a)\n"
	for _, args := range [][]string{
		{"--interp", "--file"}, {"--aot", "--file"}, {"--jit", "--file"}, {"--eval"},
	} {
		full := args
		if args[len(args)-1] == "--file" {
			full = append(append([]string{}, args...), writeSrc(t, t.TempDir(), "empty.gy", src))
		} else {
			full = append(append([]string{}, args...), src)
		}
		out, code := cliRunCode(t, full...)
		if code == 0 {
			t.Errorf("%v ran a program the oracle rejects: %q", args, out)
			continue
		}
		combined := cliRun(t, full...)
		if !strings.Contains(combined, "empty statement") {
			t.Errorf("%v refused without naming the rule: %s", args, combined)
		}
	}
}

// replOutput feeds one program to the REPL on stdin and returns everything it wrote.
func replOutput(t *testing.T, src string) string {
	t.Helper()
	cmd := exec.Command(cliBin(t), "--repl")
	cmd.Stdin = strings.NewReader(src)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("gustyc --repl: %v\n%s\n%s", err, out.String(), errb.String())
	}
	return out.String() + errb.String()
}
