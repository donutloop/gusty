package integration

// ADR 0431 · roadmap Gap R.454 — the reference leg of the answer direction's forwarding arm.
//
// The unit suite (`pkg/lang/method_forward_test.go`) proves what this compiler emits; this one proves what
// the CLI ANSWERS, and reads every expectation off a live CPython in the same case rather than from a comment:
//
//	class C():
//	    def m(self):
//	        return None
//	    def n(self):
//	        return self.m()
//
//	c = C()
//	print(c.n() is None, c.n())   # the reference: True None   this backend, before: False 0 — at exit 0
//
// `answerArmOf`'s forwarding arm read a `*Name` callee, so a body that forwards a call to another METHOD of
// its own class was one un-nameable road, and one un-nameable road retires a whole body: the void's payload is
// the word `0`, which an int slot legally carries (**ADR 0187**), and the number printer printed it. **ADR
// 0431** gives the arm the attribute road — resolve the callee through the class's own declarations and every
// base, then answer the callee's OWN word out of the two tables the METHOD round keeps.
//
// Every answered row runs on both CLI engines, prints the reference's bytes, and is on the record
// (`testdata/interpreter-golden.json`), so an answer cannot be deleted by deleting a test (**ADR 0302**). The
// owed rows pin this backend's bytes beside the reference's, so the day a door opens the pin fails rather than
// quietly passing.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// methodForwardCLIBases are the modules the family is written against, spelled the same as the unit file's.
const (
	methodForwardCLIVoidBase = "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\n"
	methodForwardCLIListBase = "class C():\n    def m(self):\n        return [1, 2, 3]\n    def n(self):\n        return self.m()\nc = C()\n"
)

// methodForwardCLIFamily is the answered family — the same programs as
// `pkg/lang/method_forward_test.go::methodForwardFamily`, with `want` serving as the expectation the REFERENCE
// is checked against and the bytes the compiler must print read from CPython itself in each case.
func methodForwardCLIFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			name: "the row's program",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "printed alone",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "None\n",
		},
		{
			name: "the equality twin",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() == None)\n",
			want: "True\n",
		},
		{
			name: "the negated identity",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is not None)\n",
			want: "False\n",
		},
		{
			name: "asked for its truth",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(bool(c.n()))\n",
			want: "False\n",
		},
		{
			name: "rendered by str and repr",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(str(c.n()), repr(c.n()))\n",
			want: "None None\n",
		},
		{
			name: "bound, then asked",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nr = c.n()\nprint(r is None, r)\n",
			want: "True None\n",
		},
		{
			name: "an element of a list literal",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint([c.n()])\n",
			want: "[None]\n",
		},
		{
			name: "written into an f-string",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(f\"{c.n()}\")\n",
			want: "None\n",
		},
		{
			name: "a condition head",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nif c.n():\n    print(\"t\")\nelse:\n    print(\"f\")\n",
			want: "f\n",
		},
		{
			name: "the callee's side effect still runs",
			src:  "class C():\n    def m(self):\n        print(\"side\")\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "side\nNone\n",
		},
		{
			name: "the callee's side effect, asked twice",
			src:  "class C():\n    def m(self):\n        print(\"side\")\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n(), c.n())\n",
			want: "side\nside\nNone None\n",
		},
		{
			name: "a forwarding chain two deep",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\n    def o(self):\n        return self.n()\nc = C()\nprint(c.o() is None, c.o())\n",
			want: "True None\n",
		},
		{
			name: "a call inherited from a base",
			src:  "class B():\n    def m(self):\n        return None\nclass C(B):\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "a shadowed base method is the one forwarded",
			src:  "class B():\n    def m(self):\n        return None\nclass C(B):\n    def m(self):\n        return 7\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "7\n",
		},
		{
			name: "a method that takes the argument through",
			src:  "class C():\n    def m(self, x):\n        return None\n    def n(self):\n        return self.m(3)\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "a body with no return at all",
			src:  "class C():\n    def m(self):\n        self.x = 1\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "one method name, two classes",
			src:  "class A():\n    def m(self):\n        return None\nclass B():\n    def m(self):\n        return 7\n    def n(self):\n        return self.m()\na = A()\nb = B()\nprint(a.m() is None, b.n())\n",
			want: "True 7\n",
		},
		{
			name: "the arm that ran, forwarded",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(c.n(1), c.n(0), c.n(0) is None)\n",
			want: "7 None True\n",
		},
		{
			name: "a forwarded text arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(c.n(1), c.n(0))\n",
			want: "s None\n",
		},
		{
			name: "a forwarded number arm beside its own arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        if x > 1:\n            return 1\n        return self.m(x)\nc = C()\nprint(c.n(2), c.n(1), c.n(0))\n",
			want: "1 7 None\n",
		},
		{
			name: "the forwarded answer bound",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nr = c.n(0)\nprint(r is None, r)\n",
			want: "True None\n",
		},
		{
			name: "the forwarded answer, str and repr",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(str(c.n(1)), str(c.n(0)))\n",
			want: "7 None\n",
		},
		{
			name: "the forwarded answer in a container",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint([c.n(1), c.n(0)])\n",
			want: "[7, None]\n",
		},
		{
			name: "the forwarded answer in an f-string",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(f\"{c.n(1)}|{c.n(0)}\")\n",
			want: "7|None\n",
		},
		{
			name: "a forwarded list",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "[1, 2]\n",
		},
		{
			name: "a forwarded dict",
			src:  "class C():\n    def m(self):\n        return {\"a\": 1}\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "{'a': 1}\n",
		},
		{
			name: "a forwarded set",
			src:  "class C():\n    def m(self):\n        return {1, 2}\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "{1, 2}\n",
		},
		{
			name: "a forwarded list, measured",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(len(c.n()))\n",
			want: "2\n",
		},
		{
			name: "a forwarded list, walked",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nfor v in c.n():\n    print(v)\n",
			want: "1\n2\n",
		},
		{
			name: "a forwarded list, membership",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(1 in c.n(), 9 in c.n())\n",
			want: "True False\n",
		},
		{
			name: "a forwarded dict, measured",
			src:  "class C():\n    def m(self):\n        return {\"a\": 1}\n    def n(self):\n        return self.m()\nc = C()\nprint(len(c.n()))\n",
			want: "1\n",
		},
		{
			name: "two arms forwarding two containers",
			src:  "class C():\n    def a(self):\n        return [1, 2]\n    def b(self):\n        return {\"k\": 1}\n    def n(self, x):\n        if x:\n            return self.a()\n        return self.b()\nc = C()\nprint(c.n(1), c.n(0))\n",
			want: "[1, 2] {'k': 1}\n",
		},
		{
			name: "a forwarded container beside the callee's own",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(c.m(), c.n(), c.m() == c.n())\n",
			want: "[1, 2] [1, 2] True\n",
		},
		{
			name: "a forwarded call to a number-answering method",
			src:  "class C():\n    def seven(self):\n        return 7\n    def n(self):\n        return self.seven()\nc = C()\nprint(c.n())\n",
			want: "7\n",
		},
		{
			name: "a forwarded call to a verdict-answering method",
			src:  "class C():\n    def flag(self):\n        return True\n    def n(self):\n        return self.flag()\nc = C()\nprint(c.n())\n",
			want: "True\n",
		},
		{
			// Two calls of ONE forwarding body, compared: right, and right for a reason worth pinning — the
			// identity door leaves two carried answers to `comparePair` rather than reading one word twice,
			// which is why the truth road beside it is **Gap R.460**'s and this one is answered.
			name: "two calls of one forwarding body, compared",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(c.n(1) == c.n(0))\n",
			want: "False\n",
		},
		{
			name: "a forwarded call to a constructor",
			src:  "class C():\n    def __init__(self):\n        self.x = 1\n    def reset(self):\n        return self.__init__()\nc = C()\nprint(c.reset() is None, c.reset())\n",
			want: "True None\n",
		},
		{
			name: "a method forwarding a call to itself",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return self.m(0)\nc = C()\nprint(c.m(1))\n",
			want: "7\n",
		},
	}
}

// TestTheForwardingMethodFamilyAnswersLikeTheReference is the reference leg: every program is run by a live
// CPython and by the linked compiler, and the two must print the same bytes. A reference that stopped printing
// what this row pins fails the case too — a stale premise is a bug in the test.
func TestTheForwardingMethodFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range methodForwardCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_forward.gy", tc.src)
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

// TestTheForwardingMethodFamilyIsOnTheRecord keeps the coverage auditable: the answer a family earns may not
// be certified by the backend that earned it, and may not be deleted by deleting a record (**ADR 0302**).
func TestTheForwardingMethodFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range methodForwardCLIFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("%d program(s) of this family are not on the record leg — record them from the reference with "+
			"tools/recmerge (the compiled leg is cross-checked, never the author):\n%s", len(missing), strings.Join(missing, "\n---\n"))
	}
}

// TestTheForwardingMethodRefusalIsMachineReadable is the agentic half of the honest boundary: the refusal this
// round leaves standing is a machine-readable document — a capability code, a phase, a suggestion, the roadmap
// rows that own it — so a script never has to read prose (**ADR 0006**, **ADR 0346**).
func TestTheForwardingMethodRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := methodForwardCLIListBase + "print(sum(c.n()))\n"
	path := writeSrc(t, t.TempDir(), "method_forward_json.gy", src)
	out, code := cliRunCode(t, "--json", path)
	if code != 1 {
		t.Fatalf("a refused program exited %d, want 1 (**ADR 0006**): %s", code, out)
	}
	for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"error":`, `"capability":`, `"code":"lower.unsupported.`, `"suggestion":`, `"rows":`} {
		if !strings.Contains(out, frag) {
			t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
		}
	}
}

// TestWhatTheMethodForwardingStillOwesAtTheCLI is the reference leg of the honest half: the reference's bytes
// beside this backend's, with the row that owns the fix named. When a program starts printing the reference's
// bytes the row is DELETED and moves into methodForwardCLIFamily().
func TestWhatTheMethodForwardingStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			name: "a call forwarded to a method answering one text", gap: "Gap R.462",
			src:    "class C():\n    def hi(self):\n        return \"hi\"\n    def greet(self):\n        return self.hi()\nc = C()\nprint(c.greet())\n",
			python: "hi\n", compiled: "0\n",
		},
		{
			name: "a call forwarded to a method answering one float", gap: "Gap R.466",
			src:    "class C():\n    def half(self):\n        return 1.5\n    def n(self):\n        return self.half()\nc = C()\nprint(c.n())\n",
			python: "1.5\n", compiled: "1\n",
		},
		{
			// The DIRECT spelling of the row above, pinned beside it because it is the measurement that says
			// the forwarding arm is not the cause: the un-forwarded call is already wrong, and its module `def`
			// twin is already right.
			name: "a method answering one float, called directly", gap: "Gap R.466",
			src:    "class C():\n    def half(self):\n        return 1.5\nc = C()\nprint(c.half())\n",
			python: "1.5\n", compiled: "1\n",
		},
		{
			name: "a module def forwarding a call through a parameter", gap: "Gap R.463",
			src:    "class C():\n    def m(self):\n        return None\nc = C()\ndef f(o):\n    return o.m()\nprint(f(c) is None)\n",
			python: "True\n", compiled: "False\n",
		},
		{
			name: "a forwarded list, subscripted", gap: "Gap R.464",
			src:    "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n()[0])\n",
			python: "1\n", compiled: "<refused> index requires an inline list/dict/set literal",
		},
		{
			name: "a forwarded dict, read by key", gap: "Gap R.464",
			src:    "class C():\n    def m(self):\n        return {\"a\": 1}\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n()[\"a\"])\n",
			python: "1\n", compiled: "<refused> index must be a constant",
		},
		{
			name: "a forwarded container answer, summed", gap: "Gap R.458",
			src:    "class C():\n    def m(self):\n        return [1, 2, 3]\n    def n(self):\n        return self.m()\nc = C()\nprint(sum(c.n()))\n",
			python: "6\n", compiled: "<refused> sum requires an inline list/set/dict literal",
		},
		{
			name: "two calls of one forwarding body, asked for their truth", gap: "Gap R.460",
			src:    "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(bool(c.n(1)), bool(c.n(0)))\n",
			python: "True False\n", compiled: "False False\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_forward_owed.gy", r.src)
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
				if strings.HasPrefix(r.compiled, "<refused>") {
					if code == 0 {
						t.Fatalf("%s: %s printed %q where this row pins a refusal (%s)\n%s",
							engine, r.gap, stdout, r.compiled[len("<refused> "):], r.src)
					}
					if !strings.Contains(stderr, r.compiled[len("<refused> "):]) {
						t.Fatalf("%s: %s refused with %q, this row pins %q\n%s", engine, r.gap, stderr, r.compiled, r.src)
					}
					continue
				}
				if code != 0 {
					t.Fatalf("%s: %s refused (exit %d) where the row pins an exit-0 wrong answer: %s\n%s",
						engine, r.gap, code, stderr, r.src)
				}
				if stdout == want {
					t.Fatalf("%s closed %s: the program prints the reference's bytes — delete this row and move it "+
						"into methodForwardCLIFamily()\n%s", engine, r.gap, r.src)
				}
				if stdout != r.compiled {
					t.Fatalf("%s: %s changed shape: printed %q, this row pins %q (the reference prints %q)\n%s",
						engine, r.gap, stdout, r.compiled, want, r.src)
				}
			}
		})
	}
}
