package integration

// Gap R.170 / ADR 0286 at the CLI: a text the body rendered into a NAME.
//
// `def f(v): s = str(v); return s` / `print(f(3))` is a program the reference answers `3`. The
// interpreter answered `3`; the compiled leg answered `0` at exit 0, because the caller did not know the
// callee's answer was a text and handed `printf` an interned string index with `%d`.
//
// The compiled leg is held to "the reference's answer, or a refusal naming what is missing — never a
// number the reference never prints", which is the exit-code contract (ADR 0166) applied to this family.

import (
	"strings"
	"testing"
)

func TestARenderingBoundToANameAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"str bound then returned", "def f(v):\n    s = str(v)\n    return s\n\nprint(f(3))\n", "3\n"},
		{"str bound of a text", "def f(v):\n    s = str(v)\n    return s\n\nprint(f(\"hi\"))\n", "hi\n"},
		{"repr bound then returned", "def f(v):\n    r = repr(v)\n    return r\n\nprint(f(3))\n", "3\n"},
		{"concat with a bound rendering", "def f(v):\n    s = \"x\" + str(v)\n    return s\n\nprint(f(3))\n", "x3\n"},
		{"rendering bound twice", "def f(v):\n    a = str(v)\n    b = a\n    return b\n\nprint(f(7))\n", "7\n"},
		{"rendering bound in a branch", "def f(v):\n    if v > 0:\n        s = str(v)\n        return s\n\n    return \"no\"\n\nprint(f(3))\nprint(f(0))\n", "3\nno\n"},
		{"bound rendering measured", "def f(v):\n    s = str(v)\n    return s\n\nprint(len(f(3)))\n", "1\n"},
		{"bound rendering asked a method", "def f(v):\n    s = str(v)\n    return s\n\nprint(f(3).upper())\n", "3\n"},
		{"a number body is untouched", "def n(v):\n    y = v * 2\n    return y\n\nprint(n(3))\nprint(n(2.5))\n", "6\n5.0\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			// The reference first: a row whose oracle answer drifted is a stale row, not a bug.
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "bound_render", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (the compiler's bug, forbidden by ADR 0166): %s", engine, out)
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

// TestACompilationThatCannotRenderRefusesRatherThanCounting checks the narrowed digits road at the CLI:
// a shape the renderer cannot name exits 1 with a sentence, and under no circumstance exits 0 with the
// digits of a heap handle.
func TestACompilationThatCannotRenderRefusesRatherThanCounting(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
	}{
		{"str of a dict", "d = {\"a\": 1}\nprint(str(d))\n"},
		{"str of a list", "xs = [1, 2]\nprint(str(xs))\n"},
		{"str of a set", "s = {1, 2}\nprint(str(s))\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			p := writeSrc(t, dir, "render_refuse", r.src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code == 2 {
				t.Fatalf("exit 2 is the compiler's bug (ADR 0166): %s", out)
			}
			if code == 0 && out != want {
				t.Errorf("exit 0 with %q where the reference prints %q — a fabricated number (Gap R.170)", out, want)
			}
			if code == 1 && !strings.Contains(out, "codegen") {
				t.Errorf("exit 1 without naming the missing feature: %s", out)
			}
		})
	}
}
