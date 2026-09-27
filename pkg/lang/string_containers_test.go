package lang

import "testing"

// Unit tests for Gap I.2: strings inside runtime containers in the AOT backend.
//
// A string is a compile-time global (`@.strN`) and a container slot is an i32, so the first
// attempt at a compiled list of strings emitted `rt_set_elem(i32 %h, i32 0, i32 @.str1)`.
// LLVM's rejection was reported as a compiler bug (exit 2) for valid user code. Strings are
// now interned: the store site records an index into @str_tab, and a parallel @str_repr_tab
// holds the Python repr form so printing matches inside and outside a container.

func TestStringElementsAreInternedNotStoredAsPointers(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"assigned list printed", "xs = [\"a\"]\nprint(xs)\n"},
		{"assigned list", "xs = [\"a\", \"b\"]\nprint(len(xs))\n"},
		{"append", "xs = []\nxs.append(\"s\")\nprint(len(xs))\n"},
		{"item assign", "xs = [1]\nxs[0] = \"s\"\nprint(len(xs))\n"},
		{"set add", "s = set()\ns.add(\"q\")\nprint(len(s))\n"},
		{"dict key", "d = {}\nd[\"k\"] = 1\nprint(len(d))\n"},
		{"dict value", "d = {}\nd[1] = \"v\"\nprint(len(d))\n"},
		{"membership", "xs = [\"a\"]\nprint(\"a\" in xs)\n"},
	}
	for _, tc := range cases {
		res, err := Compile(tc.src)
		if err != nil {
			t.Errorf("%s: must compile: %v", tc.name, err)
			continue
		}
		if !contains(res.IR, "rt_str_intern2") {
			t.Errorf("%s: string element should be interned:\n%s", tc.name, res.IR)
		}
		if contains(res.IR, "i32 @.str") {
			t.Errorf("%s: a string global was passed where an i32 was required:\n%s", tc.name, res.IR)
		}
		v, verr := VerifyModuleIR(res.IR, 0)
		if verr != nil || !v.OK {
			t.Errorf("%s: emitted module must verify: %v %v", tc.name, v.Errors, verr)
		}
	}
}

// TestReprTableIsEmittedAlongsideRawText pins both halves of the interning contract: the raw
// text for print(x) and the repr form for print inside a container.
func TestReprTableIsEmittedAlongsideRawText(t *testing.T) {
	res, err := Compile("xs = []\nxs.append(\"a\")\nprint(len(xs))\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"@str_tab", "@str_repr_tab", "rt_str_repr_ptr", "rt_print_value"} {
		if !contains(res.IR, want) {
			t.Errorf("compiled module missing %s", want)
		}
	}
}

// TestPyReprStringMatchesPythonQuoteChoice pins the repr rule the interpreter and the
// interned repr table share: prefer single quotes, fall back to double quotes when the text
// contains a single quote and no double quote.
func TestPyReprStringMatchesPythonQuoteChoice(t *testing.T) {
	cases := map[string]string{
		`plain`:     `'plain'`,
		`it's`:      `"it's"`,
		`say "hi"`:  `'say "hi"'`,
		`both'"`:    `'both\'"'`,
		"":          `''`,
		"tab\there": `'tab\there'`,
	}
	for in, want := range cases {
		if got := pyReprString(in); got != want {
			t.Errorf("pyReprString(%q) = %s, want %s", in, got, want)
		}
	}
}

// TestInterpreterRendersContainersLikePython checks the shared printing rules on the
// interpreter side, including the dict-key case that used to print a raw heap handle.
func TestInterpreterRendersContainersLikePython(t *testing.T) {
	cases := []struct{ src, want string }{
		{`xs = ["a", "b"]
print(xs)
`, "['a', 'b']\n"},
		{`d = {"k": 1}
print(d)
`, "{'k': 1}\n"},
		{`d = {1: "v"}
print(d)
`, "{1: 'v'}\n"},
		{`s = {"q"}
print(s)
`, "{'q'}\n"},
		{`s = set()
print(s)
`, "set()\n"},
		{`print(["it's", "plain"])
`, `["it's", 'plain']` + "\n"},
	}
	for _, tc := range cases {
		got := captureStdout(t, tc.src)
		if got != tc.want {
			t.Errorf("interpreter print = %q, want %q", got, tc.want)
		}
	}
}

// Gap J.6 — dict/set literals with string contents build heap objects, and a container that
// would hold both strings and numbers is reported instead of misprinted.
func TestStringLiteralsBuildHeapContainers(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"dict literal string keys", "d = {\"a\": 1}\nprint(len(d))\n"},
		{"dict literal string values", "d = {1: \"a\"}\nprint(len(d))\n"},
		{"set literal strings", "s = {\"a\", \"b\"}\nprint(len(s))\n"},
		{"bare dict literal", "print(len({\"a\": 1}))\n"},
	}
	for _, tc := range cases {
		res, err := Compile(tc.src)
		if err != nil {
			t.Errorf("%s: must compile: %v", tc.name, err)
			continue
		}
		if !contains(res.IR, "rt_str_intern2") {
			t.Errorf("%s: string contents should intern:\n%s", tc.name, res.IR)
		}
		if !contains(res.IR, "rt_mark_estr") {
			t.Errorf("%s: the object should record that it holds strings:\n%s", tc.name, res.IR)
		}
		if contains(res.IR, "i32 @.str") {
			t.Errorf("%s: no string pointer may sit in an i32 slot:\n%s", tc.name, res.IR)
		}
		if v, verr := VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: module must verify: %v %v", tc.name, v.Errors, verr)
		}
	}
}

// TestLiteralNeedsHeapAndMixedKinds pins the two predicates the lowering decides with, so a
// future change to either is caught where the decision is made.
func TestLiteralNeedsHeapAndMixedKinds(t *testing.T) {
	mustHeap := []string{`xs = ["a"]`, `d = {"a": 1}`, `d = {1: "a"}`, `s = {"a"}`}
	for _, src := range mustHeap {
		e := parseExprForTest(t, src)
		if !literalNeedsHeap(e) {
			t.Errorf("%s should need the heap path", src)
		}
		if literalMixedKinds(e) {
			t.Errorf("%s is homogeneous and must not be refused", src)
		}
	}
	mixed := []string{`xs = [1, "a"]`, `s = {1, "a"}`, `d = {"a": 1, "b": "c"}`}
	for _, src := range mixed {
		e := parseExprForTest(t, src)
		if !literalMixedKinds(e) {
			t.Errorf("%s mixes kinds and must be reported", src)
		}
	}
	uniform := []string{`xs = [1, 2]`, `d = {1: 2}`, `s = {1, 2}`}
	for _, src := range uniform {
		e := parseExprForTest(t, src)
		if literalNeedsHeap(e) {
			t.Errorf("%s should stay on the static global path", src)
		}
	}
}

// parseExprForTest extracts the value expression of a single `x = <expr>` assignment.
func parseExprForTest(t *testing.T, src string) Expr {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	as, ok := prog.Stmts[0].(*AssignStmt)
	if !ok {
		t.Fatalf("%q did not parse to an assignment", src)
	}
	return as.Value
}
