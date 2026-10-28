// Roadmap row **L12.4** / **ADR 0446** at the CLI: the class object, and every base an attribute read walks.
//
// The programs below were measured on `9605949` before the row was taken: `class C:` / `X = 1` /
// `print(C().X)` printed `0` at the exit code of success, `print(Sub.NAME)` for a member only a base binds was
// refused in words, a method handing back `self.NAME` printed the intern table's index, and two objects of one
// class — one of them written to — were read with one word. The reference answers all of them, and each case
// asks both execution legs: the JIT and the linked artifact, which is the pair that disagreed for **Gap R.504**.
//
// Every expectation is re-derived from a live `python3` inside the case (**ADR 0166**), and every answering
// program is on the record (**ADR 0302**: a missing record fails the case). A refusal is exit **1** in words
// that name the missing half and the row that owns the rest; exit **2** — a module `llc` had to reject — is
// forbidden (**ADR 0006**).
package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

func classObjectFamily() []struct{ name, src string } {
	return []struct{ name, src string }{
		// The member the class body binds, read through an instance.
		{"the row's own program", "class C:\n    X = 1\n\nprint(C().X)\n"},
		{"a text member read through an instance", "class C:\n    NAME = \"c\"\n\nc = C()\nprint(c.NAME)\n"},
		{"the enum idiom, read through an instance", "class Color:\n    RED = 1\n\nprint(Color().RED)\n"},
		{"a method reads its class's member", "class C:\n    X = 1\n\n    def get(self):\n        return self.X\n\nprint(C().get())\n"},
		{"a member goes into a list", "class C:\n    X = 3\n\nprint([C.X, C.X])\n"},
		{"a member goes into a dict", "class C:\n    K = 5\n\nprint({C.K: C.K})\n"},
		{"a member is an argument", "class C:\n    X = 4\n\ndef f(a):\n    return a * 2\n\nprint(f(C.X))\n"},
		{"a member is compared", "class C:\n    X = 4\n\nprint(C.X == 4, C.X < 5)\n"},
		{"a text member is concatenated", "class C:\n    NAME = \"n\"\n\nprint(C.NAME + \"!\")\n"},
		{"a text member is cased", "class C:\n    NAME = \"n\"\n\nc = C()\nprint(c.NAME.upper())\n"},
		{"a text member is interpolated", "class C:\n    NAME = \"c\"\n\nprint(f\"hi {C.NAME}\")\n"},
		{"two members of one class, looped", "class Color:\n    R = 1\n    G = 2\n\nfor v in [Color.R, Color.G]:\n    print(v)\n"},
		// Every base is walked.
		{"a member inherited from one base", "class A:\n    NAME = \"base\"\n\nclass B(A):\n    pass\n\nprint(B().NAME, B.NAME)\n"},
		{"a member inherited through two bases deep", "class A:\n    NAME = \"base\"\n\nclass B(A):\n    pass\n\nclass C(B):\n    pass\n\nprint(C().NAME, C.NAME)\n"},
		{"a member found in the second base", "class A:\n    pass\n\nclass B:\n    X = 7\n\nclass C(A, B):\n    pass\n\nprint(C().X, C.X)\n"},
		{"three bases, one member each", "class A:\n    X = 1\n\nclass B:\n    Y = 2\n\nclass C:\n    Z = 3\n\nclass D(A, B, C):\n    pass\n\nd = D()\nprint(d.X, d.Y, d.Z)\n"},
		{"the nearer binding wins", "class A:\n    X = 1\n\nclass B(A):\n    X = 2\n\nprint(B().X, B.X)\n"},
		{"the first of two bases wins when both bind it", "class A:\n    X = 1\n\nclass B:\n    X = 2\n\nclass C(A, B):\n    pass\n\nprint(C().X)\n"},
		{"a diamond resolves in the linearized order", "class A:\n    X = 1\n\nclass B(A):\n    X = 2\n\nclass C(A):\n    X = 3\n\nclass D(B, C):\n    pass\n\nprint(D().X, D.X)\n"},
		{"a method inherited from a base reads the base's member", "class A:\n    NAME = \"a\"\n\nclass B(A):\n    def show(self):\n        return self.NAME\n\nprint(B().show())\n"},
		// The object's own word wins over the class's.
		{"the instance's word wins", "class C:\n    X = 1\n\nc = C()\nc.X = 5\nprint(c.X)\n"},
		{"two objects of one class, one shadowed", "class A:\n    X = 1\n\nclass B(A):\n    pass\n\nb = B()\nb.X = 9\nprint(b.X, B().X, A().X)\n"},
		{"the class answer stands until the object is written", "class C:\n    X = 1\n\nc = C()\nprint(c.X)\nc.X = 7\nprint(c.X)\n"},
		{"__init__ writes what the class also binds", "class C:\n    X = 1\n\n    def __init__(self):\n        self.X = 8\n\nprint(C().X, C.X)\n"},
		{"a shadowed text still prints as a text", "class C:\n    NAME = \"c\"\n\nc = C()\nc.NAME = \"z\"\nprint(c.NAME, C().NAME)\n"},
		{"the instance road the class object must not disturb", "class C:\n    def __init__(self):\n        self.x = 7\n\ninst = C()\ninst.x = 9\nprint(inst.x)\n"},
	}
}

// TestTheClassObjectAtTheCLIAnswersWhatTheReferenceAnswers is the reference leg, on both execution legs.
func TestTheClassObjectAtTheCLIAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range classObjectFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "class_object.gy", tc.src)
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

// TestWhatTheClassObjectRefusesAtTheCLI keeps the residue honest at the interface: what the door does not
// answer, it says, in words that name the missing half and the row that owns it, at the refusal exit.
func TestWhatTheClassObjectRefusesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		says    string
		rowName string
	}{
		{
			"a fractional member is refused from an instance as from the class name",
			"class C:\n    ratio = 1.5\n\nprint(C().ratio)\n",
			"binds ratio to a fractional member", "Gap R.431",
		},
		{
			"a container member is refused where the reference loops it",
			"class C:\n    items = [1, 2]\n\nfor x in C.items:\n    print(x)\n",
			"binds items to a member bound to a container or a callable", "Gap R.431",
		},
		{
			"a class body may not read its own earlier bindings",
			"class A:\n    X = 1\n    Y = X + 1\n\nprint(A.Y)\n",
			"undefined name", "Gap R.505",
		},
		{
			"a class attribute may not be rebound outside the body",
			"class Base:\n    NAME = \"base\"\n\nclass Sub(Base):\n    pass\n\nSub.NAME = \"sub\"\nprint(Sub.NAME)\n",
			"cannot store Sub.NAME", "Gap R.506",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "class_object_owed.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 is the artifact's accusation, not a refusal\n%s", engine, tc.src)
				}
				if code == 0 {
					t.Fatalf("%s: answered %q where the row says this toolchain refuses\nthe row: roadmap %s\n%s",
						engine, stdout, tc.rowName, tc.src)
				}
				if code != 1 {
					t.Fatalf("%s: exit %d, the refusal exit is 1\n%s", engine, code, tc.src)
				}
				msg := stdout + stderr
				if !strings.Contains(msg, tc.says) {
					t.Fatalf("%s: refused with %q, which does not say %q\nthe row: roadmap %s\n%s", engine, msg, tc.says, tc.rowName, tc.src)
				}
			}
		})
	}
}
