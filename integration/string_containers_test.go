package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Gap I.2 — strings inside runtime containers, AOT.
//
// Strings are compile-time globals (@.strN) while a container slot is an i32, so the first
// attempt at `xs = ["a", "b"]` emitted `rt_set_elem(i32 %h, i32 0, i32 @.str1)` — a global in
// an i32 parameter — and LLVM's rejection made valid user code look like a compiler bug.
// The runtime now keeps an interned string table: rt_str_intern(i8*) -> i32 (content
// addressed, so two spellings of the same text are the same key), rt_str_ptr(i32) -> i8*,
// and container slots hold the index. Per-container element kinds (listElemStr /
// setElemStr / dictKeyStr / dictValStr) decide how elements print and how reads behave, so
// both backends render exactly what Python does: ['a', 'b'], {'k': 1}, {1: 's'}, set().

var stringContainerCases = []struct {
	name string
	src  string
	want string
}{
	{"assigned string list literal", "xs = [\"a\", \"b\"]\nprint(xs)\n", "['a', 'b']\n"},
	{"string list with len", "xs = [\"a\", \"b\"]\nprint(len(xs))\n", "2\n"},
	{"append string", "xs = []\nxs.append(\"s\")\nprint(xs)\n", "['s']\n"},
	{"list item assign string", "xs = [1]\nxs[0] = \"s\"\nprint(xs)\n", "['s']\n"},
	{"set literal string", "s = {\"a\"}\nprint(s)\n", "{'a'}\n"},
	{"empty set renders set()", "s = set()\nprint(s)\n", "set()\n"},
	{"set add string", "s = set()\ns.add(\"s\")\nprint(s)\n", "{'s'}\n"},
	{"dict string key", "d = {}\nd[\"k\"] = 1\nprint(d)\n", "{'k': 1}\n"},
	{"dict string value", "d = {}\nd[1] = \"s\"\nprint(d)\n", "{1: 's'}\n"},
	{"dict string key and value", "d = {}\nd[\"k\"] = \"v\"\nprint(d)\n", "{'k': 'v'}\n"},
	{"dict read by string key", "d = {}\nd[\"k\"] = 7\nprint(d[\"k\"])\n", "7\n"},
	{"folded string element", "xs = []\nxs.append(str(42))\nprint(xs)\n", "['42']\n"},
	{"string variable element", "t = \"hi\"\nxs = []\nxs.append(t)\nprint(xs)\n", "['hi']\n"},
	{"iterate string list", "xs = [\"a\", \"b\"]\nfor x in xs:\n    print(x)\n", "a\nb\n"},
	{"iterate string set", "s = {\"q\"}\nfor x in s:\n    print(x)\n", "q\n"},
	{"iterate dict keys", "d = {}\nd[\"kk\"] = 1\nfor k in d:\n    print(k)\n", "kk\n"},
	{"membership string", "xs = [\"a\", \"b\"]\nprint(\"a\" in xs)\nprint(\"z\" in xs)\n", "1\n0\n"},
	{"quote choice follows Python", "xs = [\"it's\", \"q\"]\nprint(xs)\n", "[\"it's\", 'q']\n"},
	{"string list argument", "def f(xs):\n    return len(xs)\n\nprint(f([\"a\", \"b\"]))\n", "2\n"},
	{"mixed dict int and str", "d = {}\nd[1] = \"a\"\nd[2] = \"b\"\nprint(d)\n", "{1: 'a', 2: 'b'}\n"},
}

func TestStringContainersMatchPython(t *testing.T) {
	for _, tc := range stringContainerCases {
		gotInterp := runInterp(t, tc.src)
		if gotInterp != tc.want {
			t.Errorf("%s: interpreter = %q, want %q", tc.name, gotInterp, tc.want)
		}
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: emitted module must verify: %v %v", tc.name, v.Errors, verr)
			continue
		}
		gotAOT := compileAndRun(t, tc.src)
		if gotAOT != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, gotAOT, tc.want)
		}
		// the shape that started this: a string global handed to an i32 container helper
		if strings.Contains(res.IR, "i32 @.str") {
			t.Errorf("%s: IR stores a string pointer in an i32 slot:\n%s", tc.name, res.IR)
		}
	}
}

// TestStringInterningIsContentAddressed pins the property that makes string dict keys work:
// two separate literals with the same text are the same key.
func TestStringInterningIsContentAddressed(t *testing.T) {
	src := "d = {}\nd[\"k\"] = 1\nd[\"k\"] = 2\nprint(len(d))\nprint(d[\"k\"])\n"
	want := "1\n2\n"
	if got := runInterp(t, src); got != want {
		t.Errorf("interpreter = %q, want %q", got, want)
	}
	if got := compileAndRun(t, src); got != want {
		t.Errorf("AOT = %q, want %q", got, want)
	}
}

// TestStringsOutsideContainersStillRefuse keeps the ADR 0166 rule for the cases the runtime
// table cannot help with: a string that is not resolvable to text at compile time.
func TestStringsOutsideContainersStillRefuse(t *testing.T) {
	_, err := lang.Compile("def f(s):\n    return 0\n\nf(\"abc\")\n")
	if err == nil {
		t.Skip("string parameters are supported now; nothing left to refuse here")
	}
	if !strings.Contains(err.Error(), "AOT backend yet") || !strings.Contains(err.Error(), "interpreter") {
		t.Errorf("refusal should stay actionable: %v", err)
	}
}

// TestIntContainersStillBuild guards against the interned-string path becoming a blanket
// change of behaviour for containers that never contained strings.
func TestIntContainersStillBuild(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"xs = [1, 2, 3]\nprint(len(xs))\n", "3\n"},
		{"xs = []\nxs.append(7)\nprint(xs[0])\n", "7\n"},
		{"s = set()\ns.add(3)\nprint(len(s))\n", "1\n"},
		{"d = {}\nd[1] = 2\nprint(d[1])\n", "2\n"},
		{"xs = [1]\nxs[0] = 9\nprint(xs)\n", "[9]\n"},
	}
	for _, tc := range cases {
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("int container %q must compile: %v", tc.src, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("int container %q must verify: %v %v", tc.src, v.Errors, verr)
		}
	}
}

// Gap J.6 — dict and set *literals* with string contents, and the rule for containers that try
// to hold both strings and numbers.
var containerLiteralCases = []struct {
	name string
	src  string
	want string
}{
	{"dict literal string keys", "d = {\"a\": 1, \"b\": 2}\nprint(d)\n", "{'a': 1, 'b': 2}\n"},
	{"dict literal string values", "d = {1: \"a\", 2: \"b\"}\nprint(d)\n", "{1: 'a', 2: 'b'}\n"},
	{"dict literal both strings", "d = {\"k\": \"v\"}\nprint(d)\n", "{'k': 'v'}\n"},
	{"bare dict literal prints", "print({\"a\": 1})\n", "{'a': 1}\n"},
	{"bare set literal prints", "print({\"a\", \"b\"})\n", "{'a', 'b'}\n"},
	{"bare list literal prints", "print([\"a\", \"b\"])\n", "['a', 'b']\n"},
	{"len of a dict literal", "print(len({\"a\": 1, \"b\": 2}))\n", "2\n"},
	{"len of a set literal", "print(len({\"a\", \"b\"}))\n", "2\n"},
	{"dict literal indexed by its key", "d = {\"a\": 7}\nprint(d[\"a\"])\n", "7\n"},
	{"empty dict literal", "d = {}\nprint(d)\nprint(len(d))\n", "{}\n0\n"},
}

func TestContainerLiteralsMatchPython(t *testing.T) {
	for _, tc := range containerLiteralCases {
		if got := runInterp(t, tc.src); got != tc.want {
			t.Errorf("%s: interpreter = %q, want %q", tc.name, gotInterpHint(got), tc.want)
		}
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: module must verify: %v %v", tc.name, v.Errors, verr)
			continue
		}
		if got := compileAndRun(t, tc.src); got != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func gotInterpHint(got string) string { return got }

// These four used to be the refusal list. A container that *grows* with a second kind, and a
// literal whose keys or values mix kinds, are now built: every slot carries its tag from the moment
// it is written (ADR 0189), so the container stops claiming one kind and the slots answer for
// themselves (ADR 0232). Asserted by output, because the failure mode of this whole area is a
// plausible-looking wrong print — `(null)`, an index, a number where a word belongs.
var promotedContainerCases = []struct {
	src  string
	want string
}{
	{"xs = [1]\nxs.append(\"a\")\nprint(xs)\n", "[1, 'a']\n"},
	{"xs = [\"a\"]\nxs.append(1)\nprint(xs)\n", "['a', 1]\n"},
	{"s = {1}\ns.add(\"a\")\nprint(s)\n", "{1, 'a'}\n"},
	{"print({\"a\": 1, \"b\": \"c\"})\n", "{'a': 1, 'b': 'c'}\n"},
}

func TestGrowingAContainerPrintsInsteadOfRefusing(t *testing.T) {
	for _, tc := range promotedContainerCases {
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Fatalf("%q should compile since slots carry their own tags: %v", tc.src, err)
		}
		if !strings.Contains(res.IR, "@heap_tags") {
			t.Errorf("%q compiled without the per-element tag array", tc.src)
		}
		jit, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if jit.Output != tc.want {
			t.Errorf("%q printed %q, want %q (CPython)", tc.src, jit.Output, tc.want)
		}
	}
}

// What the container family still refuses, and why the refusal is the answer rather than a lazy
// copy of the interpreter: each shape would store a word whose meaning the compiler cannot say
// afterwards. The wording is checked because these messages are an agent's only input (a float has
// no word yet — L11.6; a handle in a slot built for a word is unmarked by the collector — L11.1
// (5); a call returning text on one path and a number on another has no single tag to write).
var mixedContainerCases = []string{
	"xs = [1]\nxs.append(1.5)\nprint(xs)\n",
	"print({\"a\": [1]})\n",
	"s = {1}\ns.add([2])\nprint(s)\n",
	"def f(c):\n    if c:\n        return \"z\"\n    return 7\n\nxs = [f(1), 2]\nprint(xs)\n",
}

// The case ADR 0175 refused and ADR 0184 fixed: a list literal that mixes numbers with
// interned strings. Asserting the *output*, not just that it compiles — the failure mode this
// whole area has is printing something plausible but wrong.
var mixedLiteralCases = []struct {
	src  string
	want string
}{
	{"print([1, \"a\"])\n", "[1, 'a']\n"},
	{"xs = [1, \"a\"]\nprint(xs)\n", "[1, 'a']\n"},
}

func TestMixedLiteralsPrintInsteadOfRefusing(t *testing.T) {
	for _, tc := range mixedLiteralCases {
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Fatalf("%q should compile since per-element tags landed: %v", tc.src, err)
		}
		if !strings.Contains(res.IR, "@heap_tags") {
			t.Errorf("%q compiled without the per-element tag array", tc.src)
		}
		jit, err := lang.JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("compile %q: %v", tc.src, err)
		}
		if jit.Output != tc.want {
			t.Errorf("%q printed %q, want %q (CPython)", tc.src, jit.Output, tc.want)
		}
	}
}

func TestMixedContainersAreADiagnosticNotAMisprint(t *testing.T) {
	for _, src := range mixedContainerCases {
		res, err := lang.Compile(src)
		if err == nil {
			t.Errorf("%q must be refused (it used to print (null)); got IR", src)
			continue
		}
		if !strings.Contains(err.Error(), "cannot hold a float") &&
			!strings.Contains(err.Error(), "cannot hold another container") &&
			!strings.Contains(err.Error(), "cannot prove one kind") {
			t.Errorf("%q: unexpected diagnostic: %v", src, err)
		}
		if !strings.Contains(err.Error(), "interpreter") && !strings.Contains(err.Error(), "interpreted") &&
			!strings.Contains(err.Error(), "printing it works") {
			t.Errorf("%q: should name the path that works: %v", src, err)
		}
		if res != nil {
			// nothing usable was emitted, and it must not be an invalid module
			_ = res
		}
	}
}

// Item assignment replaces an element, so the container's kind follows the new element rather
// than colliding with the old one: `xs = [1]` then `xs[0] = "s"` is a list holding one string.
func TestItemAssignmentReplacesElementKind(t *testing.T) {
	src := "xs = [1]\nxs[0] = \"s\"\nprint(xs)\n"
	want := "['s']\n"
	if got := runInterp(t, src); got != want {
		t.Errorf("interpreter = %q, want %q", got, want)
	}
	if got := compileAndRun(t, src); got != want {
		t.Errorf("AOT = %q, want %q", got, want)
	}
}
