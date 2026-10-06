package integration

// Gap R.53 / L12.1 at the CLI (ADR 0288). Comparison chains are the clearest case in this corpus of
// backends agreeing with each other and disagreeing with the reference: `a < b < c` parsed as
// `(a < b) < c` compares an int against a boolean, which this front end answers rather than refusing, so
// the wrong verdict arrived at exit 0 with no refusal, no crash and no odd digit — the class an agent
// driving the compiler reads as success. Every row below is a chain whose nested reading gives the
// OPPOSITE verdict, checked against a live `python3` before both engines run.

import (
	"testing"
)

func TestComparisonChainsAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"ascending", "print(1 < 2 < 3)\n", "True\n"},
		{"descending", "print(3 < 2 < 1)\n", "False\n"},
		{"up then down", "print(1 < 2 > 1)\n", "True\n"},
		{"down then up", "print(1 > 2 < 3)\n", "False\n"},
		{"name in range", "x = 5\nprint(1 < x < 10)\n", "True\n"},
		{"name out of range", "x = 50\nprint(1 < x < 10)\n", "False\n"},
		{"four operands", "print(1 < 2 < 3 < 4)\n", "True\n"},
		{"four operands last fails", "print(1 < 2 < 3 < 1)\n", "False\n"},
		{"texts", "print(\"a\" < \"b\" < \"c\")\n", "True\n"},
		{"chain as a test", "if 1 < 5 < 3:\n    print(\"in\")\nelse:\n    print(\"out\")\n", "out\n"},
		{"chain in a function", "def f(x):\n    if 1 < x < 10:\n        return 1\n\n    return 2\n\nprint(f(5))\nprint(f(50))\n", "1\n2\n"},
		{"call in the middle", "def g():\n    return 5\n\nprint(1 < g() < 10)\n", "True\n"},
		{"container slot in the middle", "xs = [1, 2, 3]\nprint(0 < xs[1] < 3)\n", "True\n"},
		{"dict slot in the middle", "d = {\"a\": 5}\nprint(1 < d[\"a\"] < 10)\n", "True\n"},
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
			for _, engine := range []string{"--interp", "--aot"} {
				p := writeSrc(t, dir, "chain", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (ADR 0166's compiler-bug code) on an ordinary comparison: %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — the nested reading `(a<b)<c` answers the opposite", engine, out, want)
				}
			}
		})
	}
}

// TestTheMiddleOperandOfAChainRunsOnceAtTheCLI asserts L12.1's requirement through the shipped binary.
// An effectful middle operand counts itself; a chain must count it once.
func TestTheMiddleOperandOfAChainRunsOnceAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	src := "def g():\n    print(\"evaluated\")\n\n    return 5\n\nprint(1 < g() < 10)\n"
	want, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Skip("no python3 available to cross-check")
	}
	if want != "evaluated\nTrue\n" {
		t.Fatalf("row is stale: python3 prints %q", want)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		p := writeSrc(t, dir, "chainonce", src)
		out, code := cliRunMerged(t, engine, "--file", p)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", engine, code, out)
		}
		if out != want {
			t.Errorf("%s printed %q, want %q — a repeated `evaluated` means the middle operand ran more than once (Gap R.53)", engine, out, want)
		}
	}
}
