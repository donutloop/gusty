package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The roadmap's headline case for per-element tagging: a heterogeneous list, printed. The
// expectation column is CPython's own output, checked against both of our backends (ADR 0184).
// Before @heap_tags, every row here failed to compile on the AOT path with "a compiled list
// holds either strings or numbers, not both".
func TestMixedListsMatchCPythonOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{`print([1, "a", None])` + "\n", "[1, 'a', None]\n"},
		{"xs = [1, \"a\", None, 2]\nprint(xs)\n", "[1, 'a', None, 2]\n"},
		{"xs = [7, \"x\", None, 0, \"y\"]\nprint(xs)\n", "[7, 'x', None, 0, 'y']\n"},
		{"x = \"hi\"\nxs = [1, x, None]\nprint(xs)\n", "[1, 'hi', None]\n"},
		{"y = 1\ny = \"s\"\nxs = [1, y]\nprint(xs)\n", "[1, 's']\n"},
		{"def greet():\n    return \"yo\"\n\nxs = [1, greet()]\nprint(xs)\n", "[1, 'yo']\n"},
		{"xs = [\"a\"]\nxs2 = [1, \"a\", None]\nprint(xs, xs2, len(xs2))\n", "['a'] [1, 'a', None] 3\n"},
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}

// The collector runs while a loop builds mixed lists: 150 iterations rebind xs, so the dead
// ones must be reclaimed while the live one still prints correctly (ADR 0181's rooting, ADR
// 0184's elements). Asserting the output only would pass with a collector that never ran, so
// the gc line is checked too.
func TestMixedListsSurviveCollection(t *testing.T) {
	src := "for i in range(150):\n    xs = [i, \"a\", None, i]\n    if i == 149:\n        print(xs)\n"
	lang.SetGCReport(true)
	defer lang.SetGCReport(false)
	jit, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("JIT: %v", err)
	}
	if jit.Output != "[149, 'a', None, 149]\n" {
		t.Fatalf("output = %q", jit.Output)
	}
	if !strings.Contains(jit.Stderr, "freed=") {
		t.Fatalf("the collector never reported; stderr = %q", jit.Stderr)
	}
}

// The loop case, against CPython: `print(x)` over a mixed list shows str() (unquoted), while
// `print(xs)` shows repr() (quoted) -- the same tag, two rendering contexts (ADR 0185).
func TestLoopOverMixedListMatchesCPython(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"xs = [1, \"a\", None]\nfor x in xs:\n    print(x)\n", "1\na\nNone\n"},
		{"xs = [\"a\", 1, None]\nfor x in xs:\n    print(x)\nprint(xs)\n", "a\n1\nNone\n['a', 1, None]\n"},
		{"xs = [1, \"a\", None]\nfor x in xs:\n    print(x, \"tag\", sep=\":\")\n", "1:tag\na:tag\nNone:tag\n"},
		{"xs = [1, \"a\"]\nfor x in xs:\n    print(x)\nx = 5\nprint(x)\n", "1\na\n5\n"},
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}

// Element-level access is the half of roadmap L11.1 that ADR 0187 opened: reading one element
// out of a mixed list produces the (value, tag) pair at the read site, and writing or appending
// an element writes its tag together with its payload. Expectations are CPython's own output,
// checked against both of our backends.
func TestMixedElementAccessMatchesCPython(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{`xs = [1, "a", None]` + "\n" + "print(xs[1])\n", "a\n"},
		{`xs = [1, "a", None]` + "\n" + "print(xs[0])\nprint(xs[2])\n", "1\nNone\n"},
		{"xs = [1, \"a\", None]\nfor i in range(3):\n    print(xs[i])\n", "1\na\nNone\n"},
		{"xs = [1, \"a\", None]\nv = xs[1]\nprint(v)\n", "a\n"},
		// The stale-tag bug this cycle fixed: the slot kept the tag it was allocated with, so
		// this printed [1, 'a', None] — the interned table answering as if it were the value.
		{"xs = [1, \"a\", None]\nxs[0] = \"z\"\nprint(xs)\nprint(xs[0])\n", "['z', 'a', None]\nz\n"},
		{"xs = [1, \"a\", None]\nxs[1] = 42\nprint(xs)\n", "[1, 42, None]\n"},
		{"xs = [1, \"a\", None]\nxs[2] = 0\nprint(xs)\n", "[1, 'a', 0]\n"},
		{"xs = [1, \"a\"]\nxs.append(2)\nxs.append(\"b\")\nxs.append(None)\nprint(xs)\nprint(len(xs))\n", "[1, 'a', 2, 'b', None]\n5\n"},
		{"xs = [1, \"a\", None]\nxs.append(\"z\")\nprint(xs[3])\n", "z\n"},
		// Rebinding a tagged variable over a plain value retires the tag: y prints 5, not a
		// stale 'a'.
		{"xs = [1, \"a\", None]\ny = xs[1]\nprint(y)\ny = 5\nprint(y)\n", "a\n5\n"},
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}

// Tagged slots and the collector have to coexist: a string element lives in @str_tab and the
// only reference to it is the payload-and-tag pair inside the list, so a stale tag shows up as
// a wrong render rather than a crash. These build and rewrite mixed lists across collection
// cycles and read elements back on both sides of them (ADR 0181, ADR 0187).
func TestMixedElementAccessSurvivesCollection(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			"xs = [1, \"a\", None]\nn = 0\nfor i in range(120):\n    ys = [i, \"t\"]\n    xs.append(i)\n    xs.append(\"u\")\n    n = n + 1\nprint(n)\nprint(len(xs))\nprint(xs[0])\nprint(xs[1])\nprint(xs[2])\nprint(xs[3])\n",
			"120\n243\n1\na\nNone\n0\n",
		},
		{
			// Slot 0 is rewritten to a string 80 times while short-lived lists churn, and slot 1
			// keeps the tag it was allocated with: both halves have to stay right.
			"xs = [1, \"a\", None]\nfor i in range(80):\n    zs = [i, i]\n    xs[0] = \"w\"\nprint(xs[0])\nprint(xs[1])\nprint(xs)\n",
			"w\na\n['w', 'a', None]\n",
		},
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}
