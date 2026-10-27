package integration

// ADR 0429 · roadmap Gap R.452 — the reference leg of the void-answering METHOD.
//
// The unit suite (`pkg/lang/void_method_test.go`) proves what this compiler emits; this one proves what the
// CLI ANSWERS, and reads every expectation off a live CPython in the same case rather than from a comment:
//
//	class C():
//	    def m(self):
//	        return None
//	c = C()
//	print(c.m() is None)   # the reference: True    this backend, before: False  — at exit 0
//	print(c.m())           # the reference: None    this backend, before: 0      — at exit 0
//
// The row's claim is that ONE question — "does this body answer nothing but the void?" — is asked by ONE
// reading (`answerBodyPlan`, via `answersOnlyTheVoid` for a module `def` and `methodAnswersOnlyTheVoid` for a
// body in a class), and that the print door, the fold, the truth road, the binding road and the container
// element all consult the answer. So this file asks each spelling of the question of both the reference and
// the linked compiler.
//
// The owed rows are pinned with today's bytes beside the reference's — a wrong verdict at exit 0 stays a wrong
// verdict, a refusal stays a refusal, and the one shape whose module `llc` refuses is pinned as the compiler
// bug it is, because ADR 0166 reserves exit 2 for exactly that and a pin that hides it makes the defect
// invisible (ADR 0302).

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// voidMethodRowCLIClass is the row's own module as the CLI sees it.
const voidMethodRowCLIClass = "class C():\n    def m(self):\n        return None\nc = C()\n"

// voidMethodCLIFamily is the answered family. `want` is the expectation the REFERENCE is checked against; the
// bytes the compiler must print come from CPython itself in each case.
func voidMethodCLIFamily() []struct {
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
		{"the identity question", voidMethodRowCLIClass + "print(c.m() is None)\n", "True\n"},
		{"its equality twin", voidMethodRowCLIClass + "print(c.m() == None)\n", "True\n"},
		{"the negated equality", voidMethodRowCLIClass + "print(c.m() != None)\n", "False\n"},
		{"the negated identity", voidMethodRowCLIClass + "print(c.m() is not None)\n", "False\n"},
		{"the void written first (equality)", voidMethodRowCLIClass + "print(None == c.m())\n", "True\n"},
		{"the void written first (identity)", voidMethodRowCLIClass + "print(None is c.m())\n", "True\n"},
		// The positions the answer is read at — each one runs the call.
		{"printed", voidMethodRowCLIClass + "print(c.m())\n", "None\n"},
		{"printed twice", voidMethodRowCLIClass + "print(c.m(), c.m())\n", "None None\n"},
		{"rendered by str and repr", voidMethodRowCLIClass + "print(str(c.m()), repr(c.m()))\n", "None None\n"},
		{"in a list literal", voidMethodRowCLIClass + "print([c.m()])\n", "[None]\n"},
		{"in a dict value", voidMethodRowCLIClass + "print({1: c.m()})\n", "{1: None}\n"},
		{"asked for its truth", voidMethodRowCLIClass + "print(bool(c.m()))\n", "False\n"},
		{"against itself", voidMethodRowCLIClass + "print(c.m() == c.m())\n", "True\n"},
		{"bound, then asked", voidMethodRowCLIClass + "r = c.m()\nprint(r is None, r)\n", "True None\n"},
		{"membership in the list that holds it", voidMethodRowCLIClass + "print(c.m() in [None])\n", "True\n"},
		{"printed and compared on one line", voidMethodRowCLIClass + "print(c.m(), c.m() == None, c.m() is None)\n", "None True True\n"},
		// The payload collisions the fold used to walk into.
		{"the void against zero", voidMethodRowCLIClass + "print(c.m() == 0)\n", "False\n"},
		{"the void against False", voidMethodRowCLIClass + "print(c.m() == False)\n", "False\n"},
		{"the void against one", voidMethodRowCLIClass + "print(c.m() == 1)\n", "False\n"},
		// The body that has no `return` at all.
		{"a body that falls off its end", "class C():\n    def m(self):\n        pass\nc = C()\nprint(c.m() is None)\n", "True\n"},
		{"a body that falls off its end, printed", "class C():\n    def m(self):\n        pass\nc = C()\nprint(c.m())\n", "None\n"},
		{"a body that prints and falls off its end", "class C():\n    def m(self):\n        print(\"side\")\nc = C()\nprint(c.m(), c.m() is None)\n", "side\nside\nNone True\n"},
		{"a folded answer whose call prints", "class C():\n    def m(self):\n        print(\"side\")\n        return None\nc = C()\nr = c.m() == None\nprint(r)\n", "side\nTrue\n"},
		// The receiver, the parameters, the call shape.
		{"a method that takes a parameter", "class C():\n    def m(self, x):\n        return None\nc = C()\nprint(c.m(3) is None, c.m(3))\n", "True None\n"},
		{"a fresh instance as the receiver", "class C():\n    def m(self):\n        return None\nprint(C().m() is None)\n", "True\n"},
		{"two instances of one class", "class C():\n    def m(self):\n        return None\na = C()\nb = C()\nprint(a.m() is None, b.m() is None)\n", "True True\n"},
		// The dunder spellings the row's Definition of done asks for.
		{"a constructor asked as an ordinary call", "class C():\n    def __init__(self):\n        self.x = 1\nc = C()\nprint(c.__init__() is None, c.__init__())\n", "True None\n"},
		{"a setter body with no return at all", "class C():\n    def reset(self):\n        self.x = 0\nc = C()\nprint(c.reset(), c.reset() is None)\n", "None True\n"},
		{"two classes, each with its own constructor body", "class A():\n    def __init__(self):\n        self.x = 1\nclass B():\n    def __init__(self):\n        self.y = 2\na = A()\nb = B()\nprint(a.__init__() is None, b.__init__() is None)\n", "True True\n"},
		// One method name, two classes, two bodies: the verdict is keyed by the class.
		{"one method name, two classes, two answers", "class A():\n    def m(self):\n        return None\nclass B():\n    def m(self):\n        return 7\nprint(A().m(), B().m())\n", "None 7\n"},
		{"a module def sharing the method's name", "def m():\n    return 7\nclass C():\n    def m(self):\n        return None\nc = C()\nprint(m(), c.m())\n", "7 None\n"},
		{"an inherited method", "class A():\n    def m(self):\n        return None\nclass B(A):\n    pass\nb = B()\nprint(b.m() is None, b.m())\n", "True None\n"},
		// The void reached through a forwarded call (**ADR 0428**'s arm, one class later).
		{"a method forwarding a module def's void answer", "def f():\n    return None\nclass C():\n    def m(self):\n        return f()\nc = C()\nprint(c.m() is None)\n", "True\n"},
		{"a method forwarding a void call, printed and compared", "def f():\n    return None\nclass C():\n    def m(self):\n        return f()\nc = C()\nprint(c.m(), c.m() == None, bool(c.m()))\n", "None True False\n"},
		// The positions with a condition road of their own.
		{"an if head", voidMethodRowCLIClass + "if c.m() is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"a negated if head", voidMethodRowCLIClass + "if c.m() != None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a while head", voidMethodRowCLIClass + "while c.m() is None:\n    print(\"once\")\n    break\n", "once\n"},
		{"a short-circuit's right operand", voidMethodRowCLIClass + "print(c.m() is None and \"t\")\n", "t\n"},
		{"a short-circuit that chooses the void", voidMethodRowCLIClass + "print(c.m() is None or \"t\")\n", "True\n"},
		{"a ternary test", voidMethodRowCLIClass + "print(1 if c.m() is None else 0)\n", "1\n"},
		{"a negated test", voidMethodRowCLIClass + "print(not c.m())\n", "True\n"},
		{"a loop element", voidMethodRowCLIClass + "for v in [c.m()]:\n    print(v is None, v)\n", "True None\n"},
		// The controls: a method that answers something keeps it.
		{"a method answering a number keeps it", "class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m(), c.m() == None, c.m() is None)\n", "7 False False\n"},
		{"a method answering a text keeps it", "class C():\n    def m(self):\n        return \"s\"\nc = C()\nprint(c.m(), c.m() == None, c.m() is None)\n", "s False False\n"},
		{"a method answering a verdict keeps it", "class C():\n    def m(self):\n        return True\nc = C()\nprint(c.m(), c.m() is None, bool(c.m()))\n", "True False True\n"},
		{"a void method beside a number method", "class C():\n    def m(self):\n        return None\n    def n(self):\n        return 7\nc = C()\nprint(c.m(), c.n(), c.m() is None, c.n() is None)\n", "None 7 True False\n"},
		{"a void method beside a module function", voidMethodRowCLIClass + "def f():\n    return None\nprint(f() is None, c.m() is None)\n", "True True\n"},
		// The arm that ran, kept in a word (**ADR 0430**, **Gap R.453**). These three programs are the rows that
		// used to stand in `TestWhatTheVoidAnsweringMethodStillOwesAtTheCLI` and in the exit-2 pin below them.
		{"a method with one void arm and one number arm", "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\nc = C()\nprint(c.m(1), c.m(0), c.m(0) is None)\n", "7 None True\n"},
		{"a method whose body answers a container list", "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m())\n", "[1, 2]\n"},
		{"a method whose body answers a dict", "class C():\n    def m(self):\n        return {\"a\": 1}\nc = C()\nprint(c.m())\n", "{'a': 1}\n"},
	}
}

// TestTheVoidAnsweringMethodFamilyAnswersLikeTheReference is the reference leg: every program is run by a live
// CPython and by the linked compiler, and the two must print the same bytes. A reference that stopped printing
// what this row pins fails the case too — a stale premise is a bug in the test.
func TestTheVoidAnsweringMethodFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range voidMethodCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_method.gy", tc.src)
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

// TestTheVoidAnsweringMethodFamilyIsOnTheRecord keeps the coverage auditable: the answer a family earns may
// not be certified by the backend that earned it, and may not be deleted by deleting a record (ADR 0302).
func TestTheVoidAnsweringMethodFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range voidMethodCLIFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("%d of the %d programs this row pays for are not on the record — write them from the reference "+
			"with tools/recmerge:\n  %s", len(missing), len(voidMethodCLIFamily()), strings.Join(missing, "\n  "))
	}
}

// TestTheVoidAnsweringMethodExitCodesAreTheContractsTheyClaimToBe is the exit-code table: an answered question
// exits 0, an answer the backend declines is refused in words at exit 1, and nothing that this row answers
// reaches the assembler broken (ADR 0166: a refusal is exit 1, exit 2 is the compiler's own mistake).
func TestTheVoidAnsweringMethodExitCodesAreTheContractsTheyClaimToBe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{"the row's program exits 0", voidMethodRowCLIClass + "print(c.m() is None)\n", 0, "True\n", ""},
		{"the equality exits 0", voidMethodRowCLIClass + "print(c.m() == None)\n", 0, "True\n", ""},
		{"the fall-off-the-end twin exits 0", "class C():\n    def m(self):\n        pass\nc = C()\nprint(c.m() is None)\n", 0, "True\n", ""},
		{"a folded answer keeps its effect", "class C():\n    def m(self):\n        print(\"side\")\n        return None\nc = C()\nr = c.m() == None\nprint(r)\n", 0, "side\nTrue\n", ""},
		{"the truth of the answer exits 0", voidMethodRowCLIClass + "print(bool(c.m()))\n", 0, "False\n", ""},
		{"a method answering a number exits 0", "class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m() == None)\n", 0, "False\n", ""},
		{"an inherited void method exits 0", "class A():\n    def m(self):\n        return None\nclass B(A):\n    pass\nb = B()\nprint(b.m() is None)\n", 0, "True\n", ""},
		{
			// A refusal, refused in words: exit 1, never exit 2, never an answer it cannot earn
			// (**Gap R.174**'s standing — the neighbour this row must not widen).
			name:     "a container pop over a name is refused in words",
			src:      "d = {\"a\": 1}\nprint(d.pop(\"a\") is None)\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "the compiled backend folds container methods only over a literal written at the call",
		},
		{
			// A generator METHOD is refused before any answer is asked (**Gap R.455**).
			name:     "a generator method is refused in words",
			src:      "class C():\n    def m(self):\n        yield 1\nc = C()\nprint(c.m() is None)\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "yield outside a generator function",
		},
		{
			// A method appending to a FIELD container is refused by the string-method door (**Gap R.456**).
			name:     "a method appending to a field container is refused in words",
			src:      "class C():\n    def __init__(self):\n        self.items = []\n    def add(self, x):\n        self.items.append(x)\nc = C()\nprint(c.add(1))\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "string-method append on a receiver that is not a text the compiler can read",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_method_exit.gy", tc.src)
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

// TestTheVoidAnsweringMethodRefusalIsMachineReadable is the agentic half of the honest boundary: the refusal
// this row leaves standing is a machine-readable document — a capability code, a phase, a suggestion, the
// roadmap rows that own it — so a script never has to read prose (**ADR 0006**, **ADR 0346**).
func TestTheVoidAnsweringMethodRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := "d = {\"a\": 1}\nprint(d.pop(\"a\") is None)\n"
	path := writeSrc(t, t.TempDir(), "void_method_json.gy", src)
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

// TestWhatTheVoidAnsweringMethodStillOwesAtTheCLI is the reference leg of the honest half: the reference's
// bytes beside this backend's, with the row that owns the fix named. When a program starts printing the
// reference's bytes the row is DELETED and moves into voidMethodCLIFamily().
func TestWhatTheVoidAnsweringMethodStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		// `Gap R.453` — the row that stood here (`class C():` / `    def m(self, x):` / `        if x:` /
		// `            return 7` / `        return None` / `c = C()` / `print(c.m(1), c.m(0), c.m(0) is None)`,
		// pinned at `7 0 False` beside the reference's `7 None True`) — is PAID by **ADR 0430** and DELETED
		// rather than edited: the answer direction has a METHOD round now, so a body in a class keeps the arm
		// that ran in a word of its own. The program prints the reference's bytes, and stands in
		// `voidMethodCLIFamily()` above, beside a live CPython.
		{
			name: "a method forwarding a call to another method of its class", gap: "Gap R.454",
			src:    "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			python: "True None\n", compiled: "False 0\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_method_owed.gy", r.src)
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
						"into voidMethodCLIFamily()\n%s", engine, r.gap, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: %s changed shape: printed %q, this row pins %q (the reference prints %q)\n%s",
						engine, r.gap, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}

// TestWhatTheVoidAnsweringMethodFiledNextDoorDiesInTheAssembler pins the shape whose module `llc` refuses.
// It is an ordinary program the reference prints, and a missing road seen from the side that reaches the
// `ret`. ADR 0166 reserves exit 2 for a compiler bug; this is one, filed rather than hidden, and a row that
// starts assembling must be DELETED — which is what happened to its twin, the body that answers a container,
// when **ADR 0430** gave the answer direction its METHOD round.
func TestWhatTheVoidAnsweringMethodFiledNextDoorDiesInTheAssembler(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		desc     string
		src      string
		gap      string
		contains string
	}{
		{
			// The nested `def` inside a method still emits the GC frame prologue twice, and `llc` is the one
			// that says so. The shape beside it — a body answering a container, which reached this same
			// assembler with `ret i32 @.lst1` — is PAID by **ADR 0430** and DELETED: it builds the object before
			// it answers, and stands in `voidMethodCLIFamily()` above.
			"a method containing a nested def",
			"class C():\n    def m(self):\n        def inner():\n            return 7\n        return None\nc = C()\nprint(c.m())\n",
			"Gap R.457", "multiple definition of local value named 'gc.frame.base'",
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "void_method_llc.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if !strings.Contains(stderr, tc.contains) {
					t.Fatalf("%s: this shape was filed as %s (%q) and now behaves differently (exit %d, stderr %q): the "+
						"row needs re-measuring — and if it now prints %q, close the row and delete this case",
						engine, tc.gap, tc.contains, code, stderr, want)
				}
				if code != 2 {
					t.Fatalf("%s: exit %d, want 2 — the contract (**ADR 0166**) reserves 2 for a compiler bug, and `llc` "+
						"refusing a program the reference prints is exactly that\n%s", engine, code, stderr)
				}
				if stdout != "" {
					t.Fatalf("%s: stdout %q is not empty while the module was rejected: a program that does not compile "+
						"prints nothing and says it failed", engine, stdout)
				}
			}
		})
	}
}
