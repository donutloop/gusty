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
func TestMixedListsMatchCPythonOnBothBackends(t *testing.T) {
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
		if interped := runInterp(t, tc.src); interped != tc.want {
			t.Errorf("interpreter %q = %q, want %q", tc.src, interped, tc.want)
		}
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
