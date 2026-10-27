package integration

// ADR 0430 · roadmap Gap R.453 — the reference leg of the answer direction's METHOD round.
//
// The unit suite (`pkg/lang/method_answer_test.go`) proves what this compiler emits; this one proves what the
// CLI ANSWERS, and reads every expectation off a live CPython in the same case rather than from a comment:
//
//	class C():
//	    def m(self, x):
//	        if x:
//	            return 7
//	        return None
//
//	c = C()
//	print(c.m(1), c.m(0), c.m(0) is None)   # the reference: 7 None True   this backend, before: 7 0 False
//
//	class D():
//	    def m(self):
//	        return [1, 2]
//
//	print(D().m())                          # the reference: [1, 2]        this backend, before: exit 2
//
// The first program is the run-time fact the fold answered from the source: the void arm's payload is the
// word `0` (**ADR 0187**), so the number written on the other side of `==` matched it by coincidence. The
// second is the same missing round seen from the `ret`, where the container's GLOBAL reached an i32 slot and
// `llc` refused the module — **ADR 0166**'s compiler-bug exit code, filed rather than hidden.
//
// Every row runs on both CLI engines, prints the reference's bytes, and is on the record
// (`testdata/interpreter-golden.json`), so an answer cannot be deleted by deleting a test (**ADR 0302**).

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// methodAnswerCLIClass is the row's own module as the CLI sees it.
const methodAnswerCLIClass = "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\nc = C()\n"

// methodAnswerCLIFamily is the answered family. `want` is the expectation the REFERENCE is checked against;
// the bytes the compiler must print come from CPython itself in each case.
func methodAnswerCLIFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program, in the positions the answer is read at.
		{"the arm that ran, printed and asked", methodAnswerCLIClass + "print(c.m(1), c.m(0), c.m(0) is None)\n", "7 None True\n"},
		{"printed alone", methodAnswerCLIClass + "print(c.m(0))\n", "None\n"},
		{"the equality twin", methodAnswerCLIClass + "print(c.m(1) == 7, c.m(0) == None)\n", "True True\n"},
		{"the void arm against the number the other arm answers", "class C():\n    def m(self, x):\n        if x:\n            return None\n        return 0\nc = C()\nprint(c.m(1) == None, c.m(0) == None, c.m(1) == 0, c.m(0) == 0)\n", "True False False True\n"},
		{"asked for its truth", methodAnswerCLIClass + "print(bool(c.m(1)), bool(c.m(0)))\n", "True False\n"},
		{"bound, then asked", methodAnswerCLIClass + "r = c.m(0)\nprint(r is None, r)\n", "True None\n"},
		{"rendered by str and repr", methodAnswerCLIClass + "print(str(c.m(1)), str(c.m(0)), repr(c.m(0)))\n", "7 None None\n"},
		{"written into an f-string", methodAnswerCLIClass + "print(f\"{c.m(1)}|{c.m(0)}\")\n", "7|None\n"},
		{"an element of a list literal", methodAnswerCLIClass + "print([c.m(1), c.m(0)])\n", "[7, None]\n"},
		{"a condition head", methodAnswerCLIClass + "if c.m(0):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		// Two arms of two different non-void kinds: the fold answered the second from the first.
		{"a text arm and a number arm", "class C():\n    def m(self, x):\n        if x:\n            return \"x\"\n        return 0\nc = C()\nprint(c.m(1))\nprint(c.m(0))\n", "x\n0\n"},
		{"a text arm compared with a number and with its own text", "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return 1\nc = C()\nprint(c.m(1) == \"s\", c.m(0) == \"s\", c.m(1) == 1, c.m(0) == 1)\n", "True False False True\n"},
		{"three arms — a number, a text and the void", "class C():\n    def m(self, x):\n        if x == 1:\n            return 1\n        elif x == 2:\n            return \"a\"\n        return None\nc = C()\nprint(c.m(1), c.m(2), c.m(3))\n", "1 a None\n"},
		// The container arms — the half whose module `llc` refused.
		{"a body that answers a list", "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m())\n", "[1, 2]\n"},
		{"a body that answers a dict", "class C():\n    def m(self):\n        return {\"a\": 1}\nc = C()\nprint(c.m())\n", "{'a': 1}\n"},
		{"a container arm beside a void arm", "class C():\n    def m(self, x):\n        if x:\n            return [1, 2]\n        return None\nc = C()\nprint(c.m(1), c.m(0))\n", "[1, 2] None\n"},
		{"a list arm and a dict arm", "class C():\n    def m(self, x):\n        if x:\n            return [1, 2]\n        return {\"a\": 1}\nc = C()\nprint(c.m(1), c.m(0))\n", "[1, 2] {'a': 1}\n"},
		{"the container answer measured and subscripted", "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(len(c.m()), c.m()[0])\n", "2 1\n"},
		{"the dict answer measured, keyed and asked", "class C():\n    def m(self):\n        return {\"a\": 1}\nc = C()\nprint(len(c.m()), c.m()[\"a\"], \"a\" in c.m())\n", "1 1 True\n"},
		{"a container answer compared with the literal it answers", "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m() == [1, 2])\n", "True\n"},
		{"a container answer walked", "class C():\n    def m(self):\n        return [1, 2]\nfor v in C().m():\n    print(v)\n", "1\n2\n"},
		{"a container answer whose slot holds another container", "class C():\n    def m(self):\n        return {\"k\": [1, 2]}\nc = C()\nprint(c.m(), c.m()[\"k\"])\n", "{'k': [1, 2]} [1, 2]\n"},
		// Inheritance and the class key: the word belongs to the body, not to the name.
		{"an inherited mixed-arm method", "class Base():\n    def m(self, x):\n        if x:\n            return 5\n        return None\nclass Child(Base):\n    pass\nprint(Child().m(1), Child().m(0), Child().m(0) is None)\n", "5 None True\n"},
		{"one method name, two classes, two bodies", "class A():\n    def m(self, x):\n        if x:\n            return 1\n        return None\nclass B():\n    def m(self, x):\n        if x:\n            return 2\n        return None\nprint(A().m(1), A().m(0), B().m(1), B().m(0))\n", "1 None 2 None\n"},
		// The arm still runs: an answer is a kind, never a substitution.
		{"a fall-off-the-end arm that prints", "class C():\n    def m(self, x):\n        if x:\n            return 7\n        print(\"fall\")\nc = C()\nprint(c.m(1), c.m(0))\n", "fall\n7 None\n"},
		// The module `def` twins, in the same file, asked the same questions (**ADR 0232**).
		{"a module body's void arm compared with None", "def f(x):\n    if x:\n        return None\n    return 1\nprint(f(True) is None)\n", "True\n"},
		{"a module body's void arm compared with a number", "def f(x):\n    if x:\n        return None\n    return 0\nprint(f(True) == 0, f(False) == 0)\n", "False True\n"},
		// The controls: a method answering ONE kind keeps every road it had.
		{"a method answering a number keeps it", "class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m(), c.m() is None, c.m() == 7)\n", "7 False True\n"},
		{"a method answering a text keeps it", "class C():\n    def m(self):\n        return \"s\"\nc = C()\nprint(c.m(), c.m() == None)\n", "s False\n"},
		{"a one-kind container method keeps its roads", "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m(), len(c.m()), c.m()[1])\n", "[1, 2] 2 2\n"},
	}
}

// TestTheMethodAnswerFamilyAnswersLikeTheReference is the reference leg: every program is run by a live
// CPython and by the linked compiler, and the two must print the same bytes. A reference that stopped
// printing what this row pins fails the case too — a stale premise is a bug in the test.
func TestTheMethodAnswerFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range methodAnswerCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_answer.gy", tc.src)
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
					t.Fatalf("%s: the module reached the assembler broken (**ADR 0166**): %s\n%s", engine, stderr, tc.src)
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

// TestTheMethodAnswerFamilyIsOnTheRecord keeps the coverage auditable: the answer a family earns may not be
// certified by the backend that earned it, and may not be deleted by deleting a record (**ADR 0302**).
func TestTheMethodAnswerFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range methodAnswerCLIFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("%d program(s) of this family are not on the record leg — record them from the reference with "+
			"tools/recmerge (the compiled leg is cross-checked, never the author):\n%s", len(missing), strings.Join(missing, "\n---\n"))
	}
}

// TestTheMethodAnswerRefusalIsMachineReadable is the agentic half: the refusal this row leaves standing is a
// machine-readable document — a capability code, a phase, a suggestion, the roadmap rows that own it — so a
// script never has to read prose (**ADR 0006**, **ADR 0346**).
func TestTheMethodAnswerRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nys = c.m()\nprint(len(ys))\n"
	path := writeSrc(t, t.TempDir(), "method_answer_json.gy", src)
	out, code := cliRunCode(t, "--json", path)
	if code != 1 {
		t.Fatalf("a refused program exited %d, want 1 (**ADR 0006**): %s", code, out)
	}
	for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"error":`, `"capability":`, `"code":`, `"suggestion":`, `"rows":`} {
		if !strings.Contains(out, frag) {
			t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
		}
	}
}

// TestWhatTheMethodAnswerStillOwesAtTheCLI is the reference leg of the honest half: the reference's bytes
// beside this backend's, with the row that owns the fix named. When a program starts printing the
// reference's bytes the row is DELETED and moves into methodAnswerCLIFamily().
func TestWhatTheMethodAnswerStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			name: "a container the method answers, asked of sum", gap: "Gap R.458",
			src:    "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(sum(c.m()))\n",
			python: "3\n", compiled: "<refused> sum requires an inline list/set/dict literal",
		},
		{
			name: "a bound method answer, measured", gap: "Gap R.459",
			src:    "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nys = c.m()\nprint(len(ys))\n",
			python: "2\n", compiled: "<refused> len of a non-string variable",
		},
		{
			// One kind word per carried BODY, not per call (**ADR 0368**'s choice, inherited by the method
			// round): two calls of one body in one expression leave the word saying whichever ran last.
			name: "two calls of one carried body, compared", gap: "Gap R.460",
			src:    "def f(x):\n    if x:\n        return \"s\"\n    return None\n\nprint(f(1) == f(0))\n",
			python: "False\n", compiled: "True\n",
		},
		{
			name: "two calls of one carried body, asked for their truth", gap: "Gap R.460",
			src:    "def f(x):\n    if x:\n        return \"s\"\n    return None\n\nprint(bool(f(1)), bool(f(0)))\n",
			python: "True False\n", compiled: "False False\n",
		},
		{
			// The one item this cycle moved AWAY from a right answer, pinned beside the row that owns it: the
			// parent's `True` for the text arm came from the text-method table claiming a carried body, and
			// **ADR 0430**'s `answerOwnsMethod` gate removes that claim — which leaves both arms reading one word.
			name: "two calls of one carried method, asked for their truth", gap: "Gap R.460",
			src:    "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return None\n\nc = C()\nprint(bool(c.m(1)), bool(c.m(0)))\n",
			python: "True False\n", compiled: "False False\n",
		},
		{
			name: "a mixed container answer, compared with the literal one arm answers", gap: "Gap R.461",
			src:    "class C():\n    def m(self, x):\n        if x:\n            return [1, 2]\n        return None\n\nc = C()\nprint(c.m(1) == [1, 2])\n",
			python: "True\n",
			compiled: "<refused> comparing a container with *lang.Call needs a tagged value: the compiler cannot " +
				"see whether that side is a container, and comparing the two i32s would compare a heap slot with a " +
				"number (roadmap L11.1/L11.2, ADR 0189)",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_answer_owed.gy", r.src)
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
					t.Fatalf("%s: %s reached the assembler with a broken module (**ADR 0166**): %s\n%s", engine, r.gap, stderr, r.src)
				}
				got := stdout
				if code != 0 {
					first := strings.SplitN(strings.TrimSpace(stderr), "\n", 2)[0]
					got = "<refused> " + strings.TrimPrefix(first, "gustyc: jit: codegen: ")
				}
				if got == want {
					t.Fatalf("%s closed %s: the program prints the reference's bytes — delete this row and move it "+
						"into methodAnswerCLIFamily()\n%s", engine, r.gap, r.src)
				}
				if got != r.compiled {
					t.Fatalf("%s: %s changed shape: %q, this row pins %q (the reference prints %q)\n%s",
						engine, r.gap, got, r.compiled, want, r.src)
				}
			}
		})
	}
}
