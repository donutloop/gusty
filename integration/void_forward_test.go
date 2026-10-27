package integration

// ADR 0428 · roadmap Gap R.451 — the reference leg of the FORWARDED void answer.
//
// **ADR 0427** paid the question asked of a body that answers the void; this one pays the same question
// asked one call later:
//
//	def f():
//	    return None
//	def g():
//	    return f()
//	print(g() is None)   # the reference: True   this backend, before: False  — at exit 0
//	print(g() == 0)      # the reference: False  this backend, before: True
//
// Every expectation here is read off a live CPython in the same case rather than remembered, so a stale
// premise fails as loudly as a wrong answer does. The exit-code table keeps ADR 0166's contract for the
// shapes this row does NOT own: a refusal stays at exit 1 with its own sentence, a wrong answer never
// reaches exit 2, and the owed rows — a mixed-arm callee (**Gap R.324**) and a parameter handed back
// (**Gap R.417**) — are pinned with the reference's bytes beside this backend's (ADR 0302). The third row
// that used to stand here, a method's body (**Gap R.452**), is PAID by **ADR 0429** and lives, answering, in
// `integration/void_method_test.go`.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const voidForwardRow = "def f():\n    return None\ndef g():\n    return f()\n"

// voidForwardCLIFamily is the answered family as the CLI sees it. `want` is the bytes the reference is
// CHECKED against; the bytes the compiled leg must print are the reference's.
func voidForwardCLIFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program and its four spellings.
		{"the identity question", voidForwardRow + "print(g() is None)\n", "True\n"},
		{"its equality twin", voidForwardRow + "print(g() == None)\n", "True\n"},
		{"the negated equality", voidForwardRow + "print(g() != None)\n", "False\n"},
		{"the negated identity", voidForwardRow + "print(g() is not None)\n", "False\n"},
		{"the void written first (equality)", voidForwardRow + "print(None == g())\n", "True\n"},
		{"the void written first (identity)", voidForwardRow + "print(None is g())\n", "True\n"},
		// The positions the forwarded answer is read at.
		{"printed", voidForwardRow + "print(g())\n", "None\n"},
		{"rendered by str and repr", voidForwardRow + "print(str(g()), repr(g()))\n", "None None\n"},
		{"in a list, a dict and a truth test", voidForwardRow + "print([g()], {1: g()}, bool(g()))\n", "[None] {1: None} False\n"},
		{"membership in the list that holds it", voidForwardRow + "print(g() in [None])\n", "True\n"},
		{"bound, then asked", voidForwardRow + "r = g()\nprint(r is None, r)\n", "True None\n"},
		{"the forwarded void against zero, False and one", voidForwardRow + "print(g() == 0, g() == False, g() == 1)\n", "False False False\n"},
		// One frame deeper, and one deeper than that.
		{"forwarded twice", voidForwardRow + "def h():\n    return g()\nprint(h() is None, h(), bool(h()))\n", "True None False\n"},
		{"forwarded three times", voidForwardRow + "def h():\n    return g()\ndef i():\n    return h()\nprint(i() is None)\n", "True\n"},
		{"a forwarding body that takes a parameter", "def f():\n    return None\ndef g(x):\n    return f()\nprint(g(1) is None, g(2) == None)\n", "True True\n"},
		{
			"a forwarding body that also falls off its end",
			"def f():\n    return None\ndef g(x):\n    if x:\n        return f()\nprint(g(True), g(False), g(True) is None, g(False) is None)\n",
			"None None True True\n",
		},
		// The call chain survives a folded comparison, one frame and two.
		{
			"the callee's side effect survives the fold",
			"def f():\n    print(\"inner\")\n    return None\ndef g():\n    return f()\nr = g() == None\nprint(r)\n",
			"inner\nTrue\n",
		},
		{
			"the forwarder's own side effect survives the fold",
			"def f():\n    return None\ndef g():\n    print(\"side\")\n    return f()\nr = g() == None\nprint(r)\n",
			"side\nTrue\n",
		},
		// Two shapes that the parent REFUSED outright and the reference prints.
		{"a lambda-bound void callee, forwarded", "f = lambda: None\ndef g():\n    return f()\nprint(g() is None)\n", "True\n"},
		{"a mutual pair bottoming out in the void", "def a():\n    return b()\ndef b():\n    return None\nprint(a() is None)\n", "True\n"},
		// The control-flow positions.
		{"an if head", voidForwardRow + "if g() is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"a negated if head", voidForwardRow + "if g() != None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a while head", voidForwardRow + "while g() is None:\n    print(\"once\")\n    break\n", "once\n"},
		{"a short-circuit's right operand", voidForwardRow + "print(g() is None and \"t\")\n", "t\n"},
		{"a ternary test", voidForwardRow + "print(1 if g() is None else 0)\n", "1\n"},
		{"a loop element", voidForwardRow + "for v in [g()]:\n    print(v is None)\n", "True\n"},
		{"the answer used twice", voidForwardRow + "print(g() == None, g() == None)\n", "True True\n"},
		{"printed and compared on one line", voidForwardRow + "print(g(), g() == None, g() is None)\n", "None True True\n"},
		// The controls: what a body forwards keeps its own answer.
		{"a forwarded number keeps its answer", "def f():\n    return 1\ndef g():\n    return f()\nprint(g(), g() == None, g() == 1)\n", "1 False True\n"},
		{"a forwarded text keeps its answer", "def f():\n    return \"s\"\ndef g():\n    return f()\nprint(g(), g() == None, g() == \"s\")\n", "s False True\n"},
		{"a forwarded verdict keeps its answer", "def f():\n    return True\ndef g():\n    return f()\nprint(g(), g() is None, bool(g()))\n", "True False True\n"},
		{"a forwarded container keeps its answer", "def f():\n    return [1, 2]\ndef g():\n    return f()\nprint(g())\n", "[1, 2]\n"},
		{"a forwarded number in an if head", "def f():\n    return 1\ndef g():\n    return f()\nif g() == None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a forwarded verdict asked for its truth", "def f():\n    return True\ndef g():\n    return f()\nif g():\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
	}
}

// TestTheForwardedVoidAnswerFamilyAnswersLikeTheReference is the reference leg.
func TestTheForwardedVoidAnswerFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range voidForwardCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_forward.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the module reached the assembler broken (ADR 0166): %s\n%s", engine, stderr, tc.src)
				}
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, tc.src)
				}
				if stdout != want {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestTheForwardedVoidAnswerFamilyIsOnTheRecord keeps the coverage auditable (**ADR 0302**).
func TestTheForwardedVoidAnswerFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range voidForwardCLIFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d of the %d programs this row pays for are not on the record — write them from the reference "+
			"with tools/recmerge:\n  %s", len(missing), len(voidForwardCLIFamily()), strings.Join(missing, "\n  "))
	}
}

// TestTheForwardedVoidAnswerExitCodesAreTheContractsTheyClaimToBe is the exit-code table, including the
// one shape in the family that is a REFUSAL and must stay one (exit 1, never exit 2, never an answer the
// backend cannot earn), with the leak check the agentic interface depends on.
func TestTheForwardedVoidAnswerExitCodesAreTheContractsTheyClaimToBe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{"the row's program exits 0", voidForwardRow + "print(g() is None)\n", 0, "True\n", ""},
		{"the equality exits 0", voidForwardRow + "print(g() == None)\n", 0, "True\n", ""},
		{"the payload collisions exit 0", voidForwardRow + "print(g() == 0, g() == False, g() == 1)\n", 0, "False False False\n", ""},
		{"a folded answer keeps its effect", "def f():\n    print(\"inner\")\n    return None\ndef g():\n    return f()\nr = g() == None\nprint(r)\n", 0, "inner\nTrue\n", ""},
		{"a forwarded number exits 0", "def f():\n    return 1\ndef g():\n    return f()\nprint(g() == None)\n", 0, "False\n", ""},
		{
			name:     "a body whose callee touches a module-level container is refused in words",
			src:      "xs = []\ndef g():\n    return xs.clear()\nprint(g() is None, xs)\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "a compiled function body cannot reach module-level containers",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_forward_exit.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 — the module was the compiler's mistake, not the program's (ADR 0166)\n%s", engine, stderr)
				}
				if code != tc.wantExit {
					t.Fatalf("%s: exited %d, want %d (stdout %q, stderr %q)\n%s", engine, code, tc.wantExit, stdout, stderr, tc.src)
				}
				if stdout != tc.wantStdout {
					t.Fatalf("%s: printed %q, want %q\n%s", engine, stdout, tc.wantStdout, tc.src)
				}
				if tc.wantStderr != "" && !strings.Contains(stderr, tc.wantStderr) {
					t.Fatalf("%s: stderr %q does not carry %q", engine, stderr, tc.wantStderr)
				}
				for _, leak := range []string{"*lang.", "%!s(", "__cans", "0x"} {
					if strings.Contains(stdout+stderr, leak) {
						t.Fatalf("%s: the diagnostic leaks %q to the human and the agent:\n%s\n%s", engine, leak, stdout+stderr, tc.src)
					}
				}
			}
		})
	}
}

// TestTheForwardedVoidAnswerRefusalIsMachineReadable is the agentic half of the refusal above.
func TestTheForwardedVoidAnswerRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := "xs = []\ndef g():\n    return xs.clear()\nprint(g() is None, xs)\n"
	path := writeSrc(t, t.TempDir(), "void_forward_json.gy", src)
	out, code := cliRunCode(t, "--json", path)
	if code != 1 {
		t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
	}
	for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"error":`, "Gap R.35"} {
		if !strings.Contains(out, frag) {
			t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
		}
	}
	// The refusal is the DOOR's own sentence, at the phase that owes it, and it names the row that owns
	// the missing half (**ADR 0166**, **ADR 0346**): a script reads the phase, the exit and the row
	// without parsing prose. What this shape is NOT is a capability refusal — the door has a sentence of
	// its own, and inventing a capability code for it here would be a second name for one shape.
	if strings.Contains(out, "\"capability\":{") {
		t.Fatalf("this refusal grew a capability object, which is a second name for one shape:\n%s", out)
	}
}

// TestWhatTheForwardedVoidAnswerStillOwesAtTheCLI is the reference leg of the honest half: the
// reference's bytes beside this backend's, with the owning row named. When a program starts printing the
// reference's bytes the row is DELETED and moves into voidForwardCLIFamily().
func TestWhatTheForwardedVoidAnswerStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			name: "a body forwarding a call whose answer is one arm of a mixed body", gap: "Gap R.324",
			src:    "def f(x):\n    if x:\n        return None\n    return 1\ndef g():\n    return f(True)\nprint(g(), g() is None)\n",
			python: "None True\n", compiled: "None False\n",
		},
		// `Gap R.452` — the row that stood here (`def f():` / `    return None` above `class C():` /
		// `    def m(self):` / `        return f()` / `print(c.m() is None)`) — is PAID by **ADR 0429** and
		// DELETED rather than edited: the answer direction has a METHOD round now, so a body in a class hears
		// the void verdict through a call the way **ADR 0428** made a second `def` hear it. The program prints
		// the reference's `True`, and stands in `voidMethodCLIFamily()` in `integration/void_method_test.go`.

		{
			name: "a body forwarding a call that hands back a parameter", gap: "Gap R.417",
			src:    "def f(v):\n    return v\ndef g():\n    return f(None)\nprint(g() is None, g())\n",
			python: "True None\n", compiled: "False 0\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_forward_owed.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.python {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", r.python, want, r.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: %s reached the assembler with a broken module (ADR 0166): %s\n%s", engine, r.gap, stderr, r.src)
				}
				if code != 0 {
					t.Fatalf("%s: %s refused (exit %d) where the row pins an exit-0 wrong answer: %s\n%s",
						engine, r.gap, code, stderr, r.src)
				}
				if stdout == want {
					t.Fatalf("%s closed %s: the program prints the reference's bytes — delete this row and move it "+
						"into voidForwardCLIFamily()\n%s", engine, r.gap, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: %s changed shape: printed %q, this row pins %q (the reference prints %q)\n%s",
						engine, r.gap, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}
