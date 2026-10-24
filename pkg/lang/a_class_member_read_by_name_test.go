// Roadmap Gap R.297 / ADR 0418 — a member the class BODY bound is an attribute of the CLASS, and reading it
// through the class name is a program the reference runs.
//
//	class C:                      #  the reference runs the body and keeps what it bound
//	    first = xs[0]
//	    label = "hi"
//	print(C.first)               #  1                ← this backend, before: refused, exit 1
//	print(C.label)               #  hi                ← `unsupported attr expression`
//
// The class walk registered the methods and dropped every other statement, so the read fell through the
// value road's cases and died on `unsupported attr expression` — a sentence naming an AST node rather than
// the program (the rule is **Gap R.116**) — where CPython simply answers. `registerClass` now keeps the
// body's bindings in the class's own member table, and the read asks it, distinct from the instance road
// that addresses an object's slot array through `@rt_inst_get`.
//
// The table holds the EXPRESSION, lowered where it is read: a class body runs once, and the read may stand
// anywhere after it — which is also why `C.first = 5` followed by `print(C.first)` answers 5. Only values
// whose re-lowering is a read rather than an event are kept that way (a container literal would mint a
// fresh object per read; a call would run twice), and a value the read cannot lower faithfully — fractional,
// verdict, void — is REFUSED with the kind named rather than printed as the payload word: 1.5 becoming 1.0
// and True becoming 1 is the answer class this contract ranks worst (**Gap R.431**).
//
// What this file pins: the answers on the compiled leg and against the record; the rebinding; the twin of
// the instance road being untouched; and the refusals, in words that name the missing half.
package lang

import (
	"strings"
	"testing"
)

// classMemberAnswered is the family this row pays.
func classMemberAnswered() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The program the row is pinned with.
		{"the row's own program", "xs = [1, 2, 3]\nclass C:\n    first = xs[0]\nprint(C.first)\n", "1\n"},
		{"a whole-number member", "class C:\n    first = 1\nprint(C.first)\n", "1\n"},
		{"a text member", "class C:\n    label = \"hi\"\nprint(C.label)\n", "hi\n"},
		{"two members, one class", "class C:\n    first = 1\n    label = \"hi\"\nprint(C.first)\nprint(C.label)\n", "1\nhi\n"},
		{"the member used as a number", "class C:\n    first = 1\nprint(C.first + 1)\n", "2\n"},
		{"the member used as a text", "class C:\n    label = \"hi\"\nprint(C.label + \"!\")\n", "hi!\n"},
		{"a member read twice", "class C:\n    first = 4\nprint(C.first)\nprint(C.first)\n", "4\n4\n"},
		{"a member read in a condition", "class C:\n    count = 2\nif C.count:\n    print(\"nonzero\")\nelse:\n    print(\"zero\")\n", "nonzero\n"},
		{"a member of each of two classes", "class A:\n    v = 1\nclass B:\n    v = 2\nprint(A.v)\nprint(B.v)\n", "1\n2\n"},
		{"the row's rebinding", "class C:\n    first = 1\nC.first = 5\nprint(C.first)\n", "5\n"},
		{"the rebinding leaves the text member alone", "class C:\n    first = 1\n    label = \"hi\"\nC.first = 5\nprint(C.first)\nprint(C.label)\n", "5\nhi\n"},
		{"the doc string still folds", "class C:\n    \"\"\"the doc\"\"\"\n    first = 1\nprint(C.__doc__)\nprint(C.first)\n", "the doc\n1\n"},
	}
}

// TestAClassMemberReadThroughTheClassName is the row's answer on both witness legs (ADR 0302: a missing
// record fails the case, so an answer is not certified by the backend that earned it).
func TestAClassMemberReadThroughTheClassName(t *testing.T) {
	t.Parallel()
	for _, tc := range classMemberAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the class-member read refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheInstanceRoadIsUntouchedByTheClassRoad is the control: the two attribute roads are neighbours, and
// the row that opens one must not move the other (**ADR 0232**'s instance slots, addressed by `@rt_inst_get`).
func TestTheInstanceRoadIsUntouchedByTheClassRoad(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"an instance attribute, set in __init__", "class C:\n    def __init__(self):\n        self.x = 7\n\nprint(C().x)\n", "7\n"},
		{"an instance attribute rebound", "class C:\n    def __init__(self):\n        self.x = 7\n\ninst = C()\ninst.x = 9\nprint(inst.x)\n", "9\n"},
		{"a method still dispatches", "class C:\n    def two(self):\n        return 2\n\nprint(C().two())\n", "2\n"},
		{"a method reads its own attribute", "class C:\n    def __init__(self):\n        self.x = 3\n    def twice(self):\n        return self.x * 2\n\nprint(C().twice())\n", "6\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the instance road was refused: %v\n%s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// TestWhatReadingAClassMemberRefusesInWords keeps the refusals honest: each names the class, the member and
// the missing half, and none of them answers a payload where the reference prints a word.
func TestWhatReadingAClassMemberRefusesInWords(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a fractional member", "class C:\n    ratio = 1.5\nprint(C.ratio)\n", "a fractional member"},
		{"a verdict member", "class C:\n    flag = True\nprint(C.flag)\n", "a verdict member"},
		{"a void member", "class C:\n    nothing = None\nprint(C.nothing)\n", "a void member"},
		{"a member the body never bound", "class C:\n    first = 1\nprint(C.other)\n", "no member of that name"},
		{"a member bound by a call", "class C:\n    n = abs(-1)\nprint(C.n)\n", "a member bound by a call"},
		{"a member bound to a container", "class C:\n    items = [1, 2]\nprint(C.items)\n", "a member bound to a container or a callable"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compileErr(t, tc.src)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refused with %q, want a sentence containing %q\n%s", msg, tc.want, tc.src)
			}
			if !strings.Contains(msg, "Gap R.431") {
				t.Fatalf("the refusal does not name the row that owns the rest: %q\n%s", msg, tc.src)
			}
		})
	}
}
