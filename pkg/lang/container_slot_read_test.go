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
		// The same question asked of a container the program *built* rather than spelled out. No literal
		// describes these slots, so ADR 0241's compile-time promise is out — and the object's own tag
		// array answers, because every writer went through ADR 0187's payload-and-tag door (L11.1).
		{"appended to a literal-built list", "xs = [[1, 2]]\nxs.append([9])\nprint(len(xs[0]))\n", "2\n"},
		{"built entirely at run time", "xs = []\nxs.append([7, 8])\nxs.append([9])\nprint(len(xs[0]), len(xs[1]))\n", "2 1\n"},
		{"text slot measured in characters", "xs = []\nxs.append(\"abc\")\nprint(len(xs[0]))\n", "3\n"},
		{"one container, three slot kinds", "xs = []\nxs.append([1, 2])\nxs.append(\"abc\")\nxs.append([9])\nprint(len(xs[0]), len(xs[1]), len(xs[2]))\n", "2 3 1\n"},
		{"dict built at run time", "d = {}\nd[\"a\"] = [1, 2, 3]\nprint(len(d[\"a\"]))\n", "3\n"},
		{"dict slot holds a dict", "xs = []\nxs.append({\"k\": 1, \"j\": 2})\nprint(len(xs[0]))\n", "2\n"},
		{"a rebound name", "xs = [[1, 2]]\nxs = [[5]]\nprint(len(xs[0]))\n", "1\n"},
		{"an item-assigned name", "xs = [[1, 2]]\nxs[0] = [7, 8, 9]\nprint(len(xs[0]))\n", "3\n"},
		{
			// The name is handed to code this pass cannot see. The answer is the object's, not the
			// literal's; the function returns rather than prints, because printing a container parameter
			// is a separate measured defect (Gap R.80), not something to pin at what it does wrong.
			"a name handed to an unknown function",
			"def f(y):\n    return len(y)\n\nxs = [[1, 2]]\nprint(f(xs))\nprint(len(xs[0]))\n",
			"1\n2\n",
		},
		{"mixed keys, one slot measured", "d = {}\nd[1] = [1, 2]\nd[\"k\"] = [3]\nprint(len(d[1]), len(d[\"k\"]))\n", "2 1\n"},
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
		// The numeric use of the same reads (ADR 0243): a slot the compiler can see holding a number
		// literal *is* that number, so arithmetic runs on it instead of being refused.
		{"element as a number", "xs = [[1, 2], [3, 4]]\nprint(xs[0][0] + 1)\n", "2\n"},
		{"element times a literal", "xs = [[1.5, 2]]\nprint(xs[0][0] * 2)\n", "3.0\n"},
		{"mixed list element as a number", "xs = [1, \"a\"]\nprint(xs[0] + 1)\n", "2\n"},
		{"mixed list element compared", "xs = [1, \"a\"]\nprint(1 if xs[0] > 2 else 0)\n", "0\n"},
		{"mixed float element added", "xs = [1.5, \"a\"]\nprint(xs[0] + 1)\n", "2.5\n"},
		{"two elements added", "xs = [1.5, 2]\nprint(xs[0] + xs[1])\n", "3.5\n"},
		{"element through a function", "def f(v):\n    return v * 2\n\nxs = [3, \"a\"]\nprint(f(xs[0]))\n", "6\n"},
		{"bound element arithmetic", "xs = [[1, 2], [3, 4]]\ny = xs[1][0] + 1\nprint(y)\n", "4\n"},
		{"element divided", "xs = [10, \"a\"]\nprint(xs[0] / 4)\n", "2.5\n"},
		{"element floordiv and mod", "xs = [10, \"a\"]\nprint(xs[0] // 3, xs[0] % 3)\n", "3 1\n"},
		{"negated literal element", "xs = [-3, \"a\"]\nprint(xs[0] + 1)\n", "-2\n"},
		{"element subtracted", "xs = [1, \"a\"]\nprint(xs[0] - 1)\n", "0\n"},
		{"element equals a literal", "xs = [1, \"a\"]\nprint(1 if xs[0] == 1 else 0)\n", "1\n"},
		{"bool element in a test", "xs = [True, \"a\"]\nprint(1 if xs[0] else 0)\n", "1\n"},
		{"bool element compared", "xs = [True, \"a\"]\nprint(1 if xs[0] == True else 0)\n", "1\n"},
		{"dict value element arithmetic", "d = {\"a\": [1.5, 2]}\nprint(d[\"a\"][0] * 2)\n", "3.0\n"},
		{"element sum accumulates", "xs = [4, \"a\"]\ntotal = 0\nfor i in [0]:\n    total = total + xs[0]\nprint(total)\n", "4\n"},
		{"float element through two slots", "t = [[1.5, \"x\"], 2]\nprint(t[0][0] + 1)\n", "2.5\n"},
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
			// (Four rows used to sit here demanding refusals: `len(xs[0])` after an `append`, after a
			// rebinding, after an item assignment, and after handing the name to a function this pass
			// cannot see. All four answer CPython's answer now — the object carries the tags its writers
			// left — and they are pinned in TestContainerSlotReadsAnswerInBothEngines (ADR 0246). What is
			// left below really is out of reach: a payload the tag has not yet been allowed to explain.)
			"arithmetic on a container element",
			// The read is licensed and the answer needs the tag; the context wants a bare i32. That is
			// the other half of L11.1, and the message says so instead of blaming the shape.
			"xs = [[1, 2], [3]]\nprint(xs[0] + 1)\n",
			"needs a single static kind",
		},
		{
			// A slot holding text, None or a container has no number to read: CPython raises TypeError,
			// and the compile-time read declines instead of guessing a kind.
			"text element used as a number",
			"xs = [1, \"a\"]\nprint(xs[1] + 1)\n",
			"more than one kind",
		},
		{
			"None element used as a number",
			"xs = [None, \"a\"]\nprint(xs[0] + 1)\n",
			"more than one kind",
		},
		{
			// The promise ran out, so even the numeric read is not allowed: the slot might not hold what
			// the literal wrote, and a number read out of the wrong object is a wrong answer.
			"element of a mutated container used as a number",
			"xs = [[1, 2]]\nxs.append([3])\nprint(xs[0][0] + 1)\n",
			"cannot reach into xs's slots",
		},
		{
			// A loop variable over a mixed list takes its tag per iteration; arithmetic on it is the
			// tagged value word, not this read.
			"loop variable used as a number",
			"xs = [1, \"a\"]\nfor x in xs:\n    print(x + 1)\n",
			"needs a tagged value",
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
