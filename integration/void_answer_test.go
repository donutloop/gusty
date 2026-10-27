package integration

// ADR 0427 · roadmap Gap R.388 — the reference leg of the void answer.
//
// The unit suite (`pkg/lang/void_answer_test.go`) proves what this compiler emits; this one proves what the
// CLI ANSWERS, and reads every expectation off a live CPython in the same case rather than from a comment:
//
//	def f():
//	    return None
//	print(f() is None)   # the reference: True   this backend, before: False  — at exit 0
//	print(f() == None)   # the reference: True   this backend, before: False
//
// The row's claim is that ONE predicate (`isNoneExpr`, consulted by `print`, `str()` and the REPL) owns the
// question "does this callee answer the void, and nothing else", and that the `is`/`==` fold now asks it
// instead of asking whether the body has a `return` statement somewhere. So this file asks each spelling of
// the question of both the reference and the linked compiler, and keeps the exit-code contract honest about
// what the fold still declines (ADR 0166: a refusal is exit 1, a broken module is exit 2 and is never a
// program's fault).
//
// The owed rows are pinned with today's bytes beside the reference's — a wrong verdict at exit 0 stays a
// wrong verdict, and a refusal stays a refusal — because a pin that quietly agrees with the compiler is how
// a defect becomes a feature (ADR 0302).

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// voidAnswerCLIFamily is the answered family as the CLI sees it. `want` is only the expectation the reference
// is CHECKED against; the bytes a program must print come from CPython itself in each case.
func voidAnswerCLIFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program and its four spellings of the question.
		{"the identity question", "def f():\n    return None\nprint(f() is None)\n", "True\n"},
		{"its equality twin", "def f():\n    return None\nprint(f() == None)\n", "True\n"},
		{"the negated equality", "def f():\n    return None\nprint(f() != None)\n", "False\n"},
		{"the negated identity", "def f():\n    return None\nprint(f() is not None)\n", "False\n"},
		// The void written on either side of the question.
		{"the void written first (equality)", "def f():\n    return None\nprint(None == f())\n", "True\n"},
		{"the void written first (identity)", "def f():\n    return None\nprint(None is f())\n", "True\n"},
		// The applied-`lambda` and name-bound-`lambda` twins: the same body, hoisted.
		{"the applied lambda's identity", "print((lambda: None)() is None)\n", "True\n"},
		{"the applied lambda's equality", "print((lambda: None)() == None)\n", "True\n"},
		{"a lambda bound to a name", "g = lambda: None\nprint(g() == None)\n", "True\n"},
		// The payload collisions the fold used to walk into: the void's word is `0`.
		{"the void answer against zero", "def f():\n    return None\nprint(f() == 0)\n", "False\n"},
		{"the void answer against False", "def f():\n    return None\nprint(f() == False)\n", "False\n"},
		{"the void answer against one", "def f():\n    return None\nprint(f() == 1)\n", "False\n"},
		// The positions the answer is read at — each runs the call.
		{"printed", "def f():\n    return None\nprint(f())\n", "None\n"},
		{"rendered by str and repr", "def f():\n    return None\nprint(str(f()), repr(f()))\n", "None None\n"},
		{"in a list literal", "def f():\n    return None\nprint([f()])\n", "[None]\n"},
		{"in a dict value", "def f():\n    return None\nprint({\"k\": f()})\n", "{'k': None}\n"},
		{"asked for its truth", "def f():\n    return None\nprint(bool(f()))\n", "False\n"},
		{"against itself", "def f():\n    return None\nprint(f() == f())\n", "True\n"},
		{"of a body that takes a parameter", "def f(x):\n    return None\nprint(f(1) is None)\n", "True\n"},
		{"membership in the list that holds a void", "def f():\n    return None\nprint(f() in [None])\n", "True\n"},
		{"bound, then asked", "def f():\n    return None\nr = f()\nprint(r is None, r)\n", "True None\n"},
		{"the body that falls off its end", "def f():\n    pass\nprint(f() is None)\n", "True\n"},
		{"an in-place method's void", "xs = [1]\nprint(xs.append(2) is None)\n", "True\n"},
		// The positions with a control-flow road of their own (Gap R.449 is the proof that roads disagree).
		{"an if head", "def f():\n    return None\nif f() is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"a negated if head", "def f():\n    return None\nif f() != None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a while head", "def f():\n    return None\nwhile f() is None:\n    print(\"once\")\n    break\n", "once\n"},
		{"a short-circuit's right operand", "def f():\n    return None\nprint(f() is None and \"t\")\n", "t\n"},
		{"a short-circuit that chooses the void", "def f():\n    return None\nprint(f() is None or \"t\")\n", "True\n"},
		{"a ternary test", "def f():\n    return None\nprint(1 if f() is None else 0)\n", "1\n"},
		{"a loop element", "def f():\n    return None\nfor v in [f()]:\n    print(v is None)\n", "True\n"},
		{"a folded answer whose call prints", "def f():\n    print(\"side\")\n    return None\nr = f() == None\nprint(r)\n", "side\nTrue\n"},
		// The controls: bodies that are NOT void answers.
		{"a two-arm body prints both arms", "def f(x):\n    if x:\n        return None\n    return 1\nprint(f(True), f(False))\n", "None 1\n"},
		{"a body that answers a number", "def f():\n    return 1\nprint(f() == None)\n", "False\n"},
		{"a body that answers a text", "def f():\n    return \"s\"\nprint(f() == None)\n", "False\n"},
		{"printed and compared on one line", "def f():\n    return None\nprint(f(), f() == None, f() is None)\n", "None True True\n"},
	}
}

// TestTheVoidAnswerFamilyAnswersLikeTheReference is the reference leg: every program is run by a live CPython
// and by the linked compiler, and the two must print the same bytes. A reference that stopped printing what
// this row pins fails the case too — a stale premise is a bug in the test.
func TestTheVoidAnswerFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range voidAnswerCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_answer.gy", tc.src)
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

// TestTheVoidAnswerFamilyIsOnTheRecord keeps the coverage auditable: the answer a family earns may not be
// certified by the backend that earned it, and may not be deleted by deleting a record (ADR 0302).
func TestTheVoidAnswerFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range voidAnswerCLIFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d of the %d programs this row pays for are not on the record — write them from the reference "+
			"with tools/recmerge:\n  %s", len(missing), len(voidAnswerCLIFamily()), strings.Join(missing, "\n  "))
	}
}

// TestTheVoidAnswerExitCodesAreTheContractsTheyClaimToBe is the exit-code table: an answered question exits 0,
// an answer the backend declines is refused in words at exit 1 with the machine-readable capability beside
// it, and NOTHING in this family reaches the assembler broken. The leak check is here too: a diagnostic that
// prints the compiler's own types is a diagnostic an agent cannot parse (ADR 0006, ADR 0346).
func TestTheVoidAnswerExitCodesAreTheContractsTheyClaimToBe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{"the row's program exits 0", "def f():\n    return None\nprint(f() is None)\n", 0, "True\n", ""},
		{"the equality exits 0", "def f():\n    return None\nprint(f() == None)\n", 0, "True\n", ""},
		{"the fall-off-the-end twin exits 0", "def f():\n    pass\nprint(f() is None)\n", 0, "True\n", ""},
		{"a folded answer keeps its effect", "def f():\n    print(\"side\")\n    return None\nr = f() == None\nprint(r)\n", 0, "side\nTrue\n", ""},
		{"the truth of the answer exits 0", "def f():\n    return None\nprint(bool(f()))\n", 0, "False\n", ""},
		{"a body answering a number exits 0", "def f():\n    return 1\nprint(f() == None)\n", 0, "False\n", ""},
		{
			// A refusal, refused in words: exit 1, never exit 2, and never an answer it cannot earn
			// (Gap R.174's standing).
			name:     "a container pop over a name is refused in words",
			src:      "d = {\"a\": 1}\nprint(d.pop(\"a\") is None)\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "the compiled backend folds container methods only over a literal written at the call",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_answer_exit.gy", tc.src)
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

// TestTheVoidAnswerRefusalIsMachineReadable is the agentic half of that same refusal: a capability code, a
// suggestion and the roadmap rows that own it, so a script never has to read prose.
func TestTheVoidAnswerRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := "d = {\"a\": 1}\nprint(d.pop(\"a\") is None)\n"
	path := writeSrc(t, t.TempDir(), "void_answer_json.gy", src)
	out, code := cliRunCode(t, "--json", path)
	if code != 1 {
		t.Fatalf("a refused program exited %d, want 1 (ADR 0006): %s", code, out)
	}
	for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"code":"lower.unsupported.container-method-over-a-name"`, `"suggestion":`, `"rows":`} {
		if !strings.Contains(out, frag) {
			t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
		}
	}
	if !strings.Contains(out, "Gap R.174") {
		t.Fatalf("the refusal does not name the row that owns it:\n%s", out)
	}
}

// TestWhatTheVoidAnswerStillOwesAtTheCLI is the reference leg of the honest half: the reference's bytes beside
// this backend's, with the row that owns the fix named. When a program starts printing the reference's bytes
// the row is DELETED and moves into voidAnswerCLIFamily().
func TestWhatTheVoidAnswerStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		// `Gap R.451` — the forwarding row that stood here — is PAID by **ADR 0428** and DELETED rather than
		// edited: the program prints the reference's `True`, and stands in `voidForwardCLIFamily()` in
		// `integration/void_forward_test.go`, beside a live CPython.
		{
			name: "a method whose body returns None", gap: "Gap R.452",
			src:    "class C():\n    def m(self):\n        return None\nc = C()\nprint(c.m() is None)\n",
			python: "True\n", compiled: "False\n",
		},
		{
			name: "the arm that answered the void, in a body that also answers a number", gap: "Gap R.324",
			src:    "def f(x):\n    if x:\n        return None\n    return 1\nprint(f(True) is None)\n",
			python: "True\n", compiled: "False\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_answer_owed.gy", r.src)
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
						"into voidAnswerCLIFamily()\n%s", engine, r.gap, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: %s changed shape: printed %q, this row pins %q (the reference prints %q)\n%s",
						engine, r.gap, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}
