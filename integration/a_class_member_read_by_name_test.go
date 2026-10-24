// Roadmap Gap R.297 / ADR 0418 at the CLI: a member the class body bound is read through the class name, and
// the answer is the reference's, byte for byte.
//
// Before this row `class C:` / `first = xs[0]` / `print(C.first)` stopped the build with `unsupported attr
// expression` — a sentence naming an AST node rather than the program (the rule is Gap R.116) — while CPython
// simply printed 1. The class walk kept the methods and dropped the rest of the body; the read now asks the
// class's own member table, which is a different road from the instance attributes the object's slot array
// holds (`@rt_inst_get`), and the values are held as the expression the body bound, so `C.first = 5` followed
// by `print(C.first)` answers 5.
//
// Every expectation is re-derived from a live `python3` inside the case (ADR 0166). A refusal is exit **1** in
// words that name the missing half and the row that owns the rest, and exit **2** — a module `llc` had to
// reject — is forbidden (ADR 0006).
package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

func classMemberCLIFamily() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"the row's own program", "xs = [1, 2, 3]\nclass C:\n    first = xs[0]\nprint(C.first)\n"},
		{"a whole-number member", "class C:\n    first = 1\nprint(C.first)\n"},
		{"a text member", "class C:\n    label = \"hi\"\nprint(C.label)\n"},
		{"the member used as a number", "class C:\n    first = 1\nprint(C.first + 1)\n"},
		{"the member used as a text", "class C:\n    label = \"hi\"\nprint(C.label + \"!\")\n"},
		{"a member read in a condition", "class C:\n    count = 2\nif C.count:\n    print(\"nonzero\")\nelse:\n    print(\"zero\")\n"},
		{"two classes, one member name each", "class A:\n    v = 1\nclass B:\n    v = 2\nprint(A.v)\nprint(B.v)\n"},
		{"the row's rebinding", "class C:\n    first = 1\nC.first = 5\nprint(C.first)\n"},
		{"the instance road, unchanged", "class C:\n    def __init__(self):\n        self.x = 7\n\ninst = C()\ninst.x = 9\nprint(inst.x)\n"},
	}
}

// TestAClassMemberAtTheCLIAnswersWhatTheReferenceAnswers is the reference leg.
func TestAClassMemberAtTheCLIAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range classMemberCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "class_member.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
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

// TestReadingAClassMemberRefusesInWordsAtTheCLI is the refused family: exit 1, a sentence naming the class, the
// member and the missing half, nothing on stdout, and no module that `llc` had to reject.
func TestReadingAClassMemberRefusesInWordsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"a verdict member", "class C:\n    flag = True\nprint(C.flag)\n", "a verdict member"},
		{"a fractional member", "class C:\n    ratio = 1.5\nprint(C.ratio)\n", "a fractional member"},
		{"a member the body never bound", "class C:\n    first = 1\nprint(C.other)\n", "no member of that name"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "class_member.gy", tc.src)
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code != 1 {
				t.Fatalf("exit %d, want the front-end refusal's 1; stdout %q stderr %q\n%s", code, stdout, stderr, tc.src)
			}
			if stdout != "" || !refusesHonestly(stderr) {
				t.Fatalf("the refusal answered %q instead of refusing in words\n%s", stdout, tc.src)
			}
			if !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "Gap R.431") {
				t.Fatalf("refused with %q, want %q and the row that owns the rest\n%s", stderr, tc.want, tc.src)
			}
		})
	}
}
