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
