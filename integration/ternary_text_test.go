package integration

// Gap R.173 / ADR 0290 at the CLI. A ternary with text arms hands back one of its arms, and the compiled leg
// printed the arm's @str_tab INDEX: `print("y" if 1 else "n")` exited 0 with `0`, and `print("big" if x > 2
// else "small")` exited 0 with `0` or `1` depending on which way the test went — the intern table's position
// where the reference writes the word. ADR 0262 had paid the ternary's number half and left this half filed
// as Gap R.127 with a probe pinned at exit 6; the probe now matches the reference on all three legs and moved
// to the parity list (ADR 0261's rule: a promoted probe takes its exit-code pin with it).

import (
	"os"
	"testing"
)

func TestTernaryWithTextArmsAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"constant test, then arm", "print(\"y\" if 1 else \"n\")\n", "y\n"},
		{"constant test, else arm", "print(\"y\" if 0 else \"n\")\n", "n\n"},
		{"run-time test, then arm", "x = 5\nprint(\"big\" if x > 2 else \"small\")\n", "big\n"},
		{"run-time test, else arm", "x = 1\nprint(\"big\" if x > 2 else \"small\")\n", "small\n"},
		{"parity by a function", "def f(x):\n    return \"even\" if x % 2 == 0 else \"odd\"\n\n\nprint(f(4))\nprint(f(5))\n", "even\nodd\n"},
		{"arms agree", "c = 1\nprint(\"same\" if c else \"same\")\n", "same\n"},
		{"empty arm chosen", "x = 0\nprint(\"hi\" if x else \"\")\n", "\n"},
		{"in a list", "print([\"y\" if 1 else \"n\"])\n", "['y']\n"},
		{"concatenated", "print((\"a\" if 1 else \"b\") + \"c\")\n", "ac\n"},
		{"through a method", "x = 2\nprint((\"big\" if x > 1 else \"small\").upper())\n", "BIG\n"},
		{"two in one print", "print(\"a\" if 1 else \"b\", \"c\" if 0 else \"d\")\n", "a d\n"},
		{"bound to a name", "c = 1\ns = \"a\" if c else \"b\"\nprint(s)\n", "a\n"},
		// The number half ADR 0262 already paid, kept so this cycle cannot regress it.
		{"int arms", "print(7 if 1 else 9)\n", "7\n"},
		{"double arms", "print(1 if 0 else 2.5)\n", "2.5\n"},
		{"verdict arms", "print(True if 1 else False)\n", "True\n"},
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
				p := writeSrc(t, dir, "tern", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (ADR 0166's compiler-bug code): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — an @str_tab index printed instead of the text is Gap R.173", engine, out, want)
				}
			}
		})
	}
}

// TestThePromotedTernaryTextProbeMatchesTheReference pins the promotion itself. probe_ternary_text_arms.gy was
// recorded debt with the compiled leg pinned at `0\n2\n`; all three legs now agree with `python3`. The test
// fails if the compiled leg ever prints an index again, which is the point of keeping it after promotion.
func TestThePromotedTernaryTextProbeMatchesTheReference(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile("programs/probe_ternary_text_arms.gy")
	if err != nil {
		t.Fatalf("probe is gone (it was promoted, not deleted): %v", err)
	}
	src := string(raw)
	want, ok := cpythonPlainOut(t, dir, src)
	if !ok {
		t.Skip("no python3 available to cross-check")
	}
	if want != "a\nx\n" {
		t.Fatalf("probe changed underneath the ledger: python3 prints %q", want)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		p := writeSrc(t, dir, "ternprobe", src)
		out, code := cliRunMerged(t, engine, "--file", p)
		if code != 0 || out != want {
			t.Errorf("%s exited %d with %q, want %q", engine, code, out, want)
		}
	}
}
