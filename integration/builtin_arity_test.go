package integration

// Gap R.131 / ADR 0287 at the CLI. The contract being asserted here is the exit-code one: a builtin
// called with no argument either IS a program the reference runs (then exit 0 with the reference's
// answer, on the compiled path) or it IS a program the reference rejects (then a trap carrying the
// reference's own sentence). What neither class may do is exit 2 — the code ADR 0166 reserves for a
// compiler bug — and every row in the first two tables did exactly that before this cycle.

import (
	"strings"
	"testing"
)

func TestBuiltinWithNoArgumentAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	// Constructors: the reference answers, so the compiled path must too.
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"int()", "print(int())\n", "0\n"},
		{"float()", "print(float())\n", "0.0\n"},
		{"bool()", "print(bool())\n", "False\n"},
		{"str()", "print(str())\n", "\n"},
		{"int() used", "print(int() + 1)\n", "1\n"},
		{"float() compared", "print(float() < 1.0)\n", "True\n"},
		{"bool() as a test", "print(1 if bool() else 2)\n", "2\n"},
		{"bool of a number", "print(bool(1))\n", "True\n"},
		{"bool of an empty text", "print(bool(\"\"))\n", "False\n"},
		{"bool of a filled text", "print(bool(\"x\"))\n", "True\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "ctor", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 — ADR 0166's compiler-bug code — on `int()`-shaped source: %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q", engine, out, want)
				}
			}
		})
	}
}

func TestBuiltinArityTrapAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		msg  string
	}{
		{"ord()", "print(ord())\n", "ord() takes exactly one argument (0 given)"},
		{"chr()", "print(chr())\n", "chr() takes exactly one argument (0 given)"},
		{"abs()", "print(abs())\n", "abs() takes exactly one argument (0 given)"},
		{"repr()", "print(repr())\n", "repr() takes exactly one argument (0 given)"},
		{"abs of two", "print(abs(1, 2))\n", "abs() takes exactly one argument (2 given)"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			// The reference must raise too, or the row is describing the wrong program.
			pyOut, pyOK := cpythonPlainOut(t, dir, r.src)
			if !pyOK {
				t.Skip("no python3 available to cross-check")
			}
			if !strings.Contains(pyOut, "TypeError") {
				t.Fatalf("row is stale: python3 does not raise TypeError for %q (%s)", r.src, pyOut)
			}
			// The record raises the reference's sentence and exits 3 (a trap the reference traps on).
			p := writeSrc(t, dir, "arity", r.src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code == 2 {
				t.Fatalf("--aot exited 2 where the reference raises: %s", out)
			}
			if code != 3 {
				t.Errorf("--aot exited %d, want 3 (a trap the reference also traps on): %s", code, out)
			}
			if !strings.Contains(out, r.msg) {
				t.Errorf("--aot did not carry the reference's sentence %q: %s", r.msg, out)
			}
			if strings.Contains(out, "index out of range") || strings.Contains(out, "goroutine") {
				t.Errorf("the Go runtime described the program instead of the compiler: %s", out)
			}
			// The compiled leg either refuses in words (exit 1) or answers the trap; never a panic.
			outA, codeA := cliRunMerged(t, "--aot", "--file", p)
			if codeA == 2 {
				t.Fatalf("--aot exited 2 (ADR 0166): %s", outA)
			}
			if codeA != 1 && codeA != 3 {
				t.Errorf("--aot exited %d, want 1 (a refusal) or 3 (the reference's trap): %s", codeA, outA)
			}
			if strings.Contains(outA, "index out of range") || strings.Contains(outA, "goroutine") {
				t.Errorf("--aot leaked a Go panic: %s", outA)
			}
		})
	}
}

// TestBuiltinWithNoArgumentNeverExitsTwo is the sweep-in-one-test version of the same contract, over every
// no-argument spelling rather than the ones that happened to be reported.
func TestBuiltinWithNoArgumentNeverExitsTwo(t *testing.T) {
	dir := t.TempDir()
	calls := []string{"int", "float", "bool", "str", "repr", "ord", "chr", "abs", "len", "hex", "oct", "bin", "type", "min", "max", "round", "input", "print", "list", "dict", "set"}
	for _, name := range calls {
		name := name
		t.Run(name, func(t *testing.T) {
			src := "print(" + name + "())\n"
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "noarg", src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Errorf("%s exited 2 on `%s` — ADR 0166 reserves that code for a compiler bug: %s", engine, src, out)
				}
				if strings.Contains(out, "index out of range") || strings.Contains(out, "panic:") {
					t.Errorf("%s leaked a Go panic on `%s`: %s", engine, src, out)
				}
			}
		})
	}
}
