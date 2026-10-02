package lang

import (
	"strings"
	"testing"
)

// A container slot is one machine word, so ADR 0239 storing the inner object's handle there was only
// half the answer: the other half is the read that *uses* what comes back. `len(xs[0])`, `xs[0][1]`,
// `d["a"][1]`, `m[0][1]`, `t[0][0][0]`, `xs[0] == [1, 2]`, `2 in xs[0]`, `for v in xs[0]` — every one
// of those used to be refused with "needs an inline literal" while the interpreter answered all of
// them, which is the two-backends-must-not-disagree rule broken in the loud direction (roadmap
// L11.1, ADR 0241).
//
// The permission is the tag the builder wrote, remembered at compile time: a name bound exactly once
// to a container literal and never mutated or handed off. Where that promise runs out the answer is a
// refusal that names the promise, not a payload read back as a handle.

func TestContainerSlotReadsAnswerInBothEngines(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"length of an element", "xs = [[1, 2], [3, 4]]\nprint(len(xs[0]))\n", "2\n"},
		{"length of a dict value", "d = {\"a\": [1, 2], \"b\": [3]}\nprint(len(d[\"a\"]))\n", "2\n"},
		{"length of a set element", "s = [{1, 2}]\nprint(len(s[0]))\n", "2\n"},
		{"reindex an element", "xs = [[1, 2], [3, 4]]\nprint(xs[0][1])\n", "2\n"},
		{"reindex a dict value", "d = {\"a\": [1, 2]}\nprint(d[\"a\"][1])\n", "2\n"},
		{"reindex an int-keyed dict value", "m = {0: [1, 2], 1: 3}\nprint(m[0][1])\n", "2\n"},
		{"three levels deep", "t = [[[1]]]\nprint(t[0][0][0])\n", "1\n"},
		{"dict of dict of list", "d = {\"a\": {\"b\": [7, 8]}}\nprint(d[\"a\"][\"b\"][1])\n", "8\n"},
		{"negative index", "xs = [[1, 2, 3]]\nprint(xs[0][-1])\n", "3\n"},
		{"element equality", "xs = [[1, 2], [3, 4]]\nprint(1 if xs[0] == [1, 2] else 0)\n", "1\n"},
		{"element inequality", "xs = [[1, 2], [3, 4]]\nprint(1 if xs[0] == [1, 3] else 0)\n", "0\n"},
		{"membership in an element", "xs = [[1, 2], [3, 4]]\nprint(1 if 2 in xs[0] else 0)\n", "1\n"},
		{"absent membership", "xs = [[1, 2], [3, 4]]\nprint(1 if 9 in xs[0] else 0)\n", "0\n"},
		{"text element", "xs = [[\"a\", 1], [2]]\nprint(xs[0][0])\n", "a\n"},
		{"float element", "xs = [[1.5, 2]]\nprint(xs[0][0])\n", "1.5\n"},
		{"None element", "xs = [[None, 1], [2]]\nprint(xs[0][0])\n", "None\n"},
		{"container prints itself", "d = {\"a\": [1, 2]}\nprint(d[\"a\"])\n", "[1, 2]\n"},
		{"iterate an element", "xs = [[1, 2]]\nfor v in xs[0]:\n    print(v)\n", "1\n2\n"},
		{"iterate a mixed element", "xs = [[1, \"a\"], [3, 4]]\nfor v in xs[0]:\n    print(v)\n", "1\na\n"},
		{"iterate a dict value", "d = {\"a\": [1, 2, 3]}\nfor v in d[\"a\"]:\n    print(v)\n", "1\n2\n3\n"},
		{"two reads in one expression", "xs = [[1, 2], [3, 4]]\nprint(xs[0][0], xs[1][1])\n", "1 4\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
		}
		if out := captureStdout(t, tc.src); out != tc.want {
			t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
		}
	}
}

func TestContainerSlotReadRefusesWhatItCannotProve(t *testing.T) {
	// The read is a compile-time promise: the literal the name was bound to is the object, unchanged.
	// Every one of these breaks the promise, and the answer must be a refusal that says which — not a
	// number wearing another object's bits, and not the generic "needs an inline literal", which sends
	// an agent off to fix a shape that was already fine.
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			"appended to",
			"xs = [[1, 2]]\nxs.append([9])\nprint(len(xs[0]))\n",
			"cannot reach into xs's slots",
		},
		{
			"rebound",
			"xs = [[1, 2]]\nxs = [[5]]\nprint(len(xs[0]))\n",
			"cannot reach into xs's slots",
		},
		{
			"item assigned",
			"xs = [[1, 2]]\nxs[0] = [7, 8, 9]\nprint(len(xs[0]))\n",
			"cannot reach into xs's slots",
		},
		{
			"handed to an unknown function",
			"def f(y):\n    print(y)\n\nxs = [[1, 2]]\nf(xs)\nprint(len(xs[0]))\n",
			"cannot reach into xs's slots",
		},
		{
			// The read is licensed and the answer needs the tag; the context wants a bare i32. That is
			// the other half of L11.1, and the message says so instead of blaming the shape.
			"numeric use of an element",
			"xs = [[1, 2]]\nprint(xs[0][0] + 1)\n",
			"needs the tagged value word still owed",
		},
		{
			// Same rule from the other side: binding an element to a name and then adding is a plain
			// numeric context too.
			"bound element used as a number",
			"xs = [[1, 2]]\nprint(xs[0][0] * 2)\n",
			"needs the tagged value word still owed",
		},
	} {
		_, err := Compile(tc.src)
		if err == nil {
			t.Fatalf("%s (%q): compiled; want a refusal", tc.name, tc.src)
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s refused with %q, want it to mention %q", tc.name, err.Error(), tc.want)
		}
		if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
			t.Errorf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
		}
	}
}

// TestMutatedContainerStillAnswersWhatNeedsNoPromise is the other half of honesty: taking a name out
// of the provable set must not take away the answers that never asked for it. print, == and `in` go
// through the object's own printers and comparisons, which carry the tag at runtime and stay correct
// however the container was built.
func TestMutatedContainerStillAnswersWhatNeedsNoPromise(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"xs = [[1, 2]]\nxs.append([9])\nprint(xs)\n", "[[1, 2], [9]]\n"},
		{"xs = [[1, 2]]\nxs.append([9])\nprint(1 if xs == [[1, 2], [9]] else 0)\n", "1\n"},
		{"xs = [[1, 2]]\nxs.append([9])\nprint(1 if [9] in xs else 0)\n", "1\n"},
		{"xs = [[1, 2]]\nxs.append([9])\nprint(len(xs))\n", "2\n"},
		{"xs = [[1, 2]]\nxs[0] = [7, 8, 9]\nprint(xs)\n", "[[7, 8, 9]]\n"},
		{"d = {\"a\": [1, 2]}\nd[\"b\"] = [3]\nprint(d)\n", "{'a': [1, 2], 'b': [3]}\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q refused: %v", tc.src, err)
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("%q AOT ran %q, want CPython's %q", tc.src, out, tc.want)
		}
		if out := captureStdout(t, tc.src); out != tc.want {
			t.Errorf("%q interpreter printed %q, want CPython's %q", tc.src, out, tc.want)
		}
	}
}
