package lang

import (
	"strings"
	"testing"
)

// A bool is a nameable element kind (roadmap Gap R.112, ADR 0259).
//
// The tag table had a bool from ADR 0182 onwards; what no container slot was allowed to say was
// bool, so `print([True, 1])` answered `[1, 1]` on both backends and both backends agreed — the
// shape of defect parity can never see. ADR 0232 left the promise in a comment ("when bool gets
// its own kind this line returns TagBool"); this is the file that holds it to it.
//
// Two rules the rows keep apart, because CPython keeps them apart:
//   - a slot says True — the printing question, answered by the tag the builder wrote;
//   - a bool is still 1 — every numeric, ordering, equality and membership question, answered by
//     the payload the same slot carries.
//
// Expectations are CPython's, taken from `python3` on the same source, never from either backend:
// pinning a rendering to itself is how this divergence survived three cycles.

func TestABoolSlotNamesItselfOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print([True, 1])\n", "[True, 1]\n"},
		{"print([True])\n", "[True]\n"},
		{"print([False])\n", "[False]\n"},
		{"print([False, True, False])\n", "[False, True, False]\n"},
		{"d = {\"k\": True}\nprint(d)\n", "{'k': True}\n"},
		{"print([True, \"a\"])\n", "[True, 'a']\n"},
		{"print([True, 1.5])\n", "[True, 1.5]\n"},
		{"print([[True], 1])\n", "[[True], 1]\n"},
		{"b = 1 == 1\nxs = [1, 2]\nxs.append(b)\nprint(xs)\n", "[1, 2, True]\n"},
		{"ys = []\nys.append(1 == 1)\nprint(ys)\n", "[True]\n"},
		{"def verdict():\n    return 2 > 1\n\nprint([verdict(), 0])\n", "[True, 0]\n"},
		{"xs = [True, 1]\nprint(xs[0])\n", "True\n"},
		{"for x in [True, 1]:\n    print(x)\n", "True\n1\n"},
		{"xs = [True, 1]\nfor x in xs:\n    print(x)\n", "True\n1\n"},
		// A comprehension slot that copies the loop variable copies an item, and the item is what says
		// True: the fold declines and the runtime builder asks the item (roadmap Gap R.112, ADR 0259).
		{"print([x for x in [True, 1, 1]])\n", "[True, 1, 1]\n"},
		{"print([x for x in [True, 1] if x])\n", "[True, 1]\n"},
		// A slot whose verdict comes from an expression rather than a literal is the same question asked
		// of that expression — the ternary among strings and the dict key that is a name bound to a
		// comparison, both measured against CPython.
		{"y = 1 == 1\nprint([True if y else False, \"a\"])\n", "[True, 'a']\n"},
		{"y = 1 == 1\nprint({y: 1})\n", "{True: 1}\n"},
		{"y = 1 == 1\nprint({\"k\": y})\n", "{'k': True}\n"},
		{"print(sorted([True, 1, False]))\n", "[False, True, 1]\n"},
		// The pair asks the same renderer, so a bool renders the same way in it (ADR 0258).
		{"print(str([True, 1]))\n", "[True, 1]\n"},
		{"print(repr([True, False]))\n", "[True, False]\n"},
		{"print(str({\"k\": True}))\n", "{'k': True}\n"},
	} {
		name := strings.ReplaceAll(strings.Split(tc.src, "\n")[0], " ", "_")
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("interpreter %s = %q, want %q", name, got, tc.want)
		}
		code, out := negBuildRun(t, "bool_slot_"+name, tc.src)
		if code != 0 {
			t.Errorf("compiled %s exited %d: %s", name, code, out)
			continue
		}
		if out != tc.want {
			t.Errorf("compiled %s = %q, want %q", name, out, tc.want)
		}
	}
}

// The other half of what CPython says: a bool is the number it behaves like. Closing the printing
// question must not buy this one a name of its own — the payload is what arithmetic, ordering,
// equality and membership read, and the compiled comparison's numeric family is int, float AND bool
// (rt_payload_eq), which is why these answers are CPython's rather than True-vs-1 arguments.
func TestABoolSlotIsStillTheNumberItBehavesLike(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(True + 1)\n", "2\n"},
		{"xs = [True, 1]\nprint(xs[0] + 1)\n", "2\n"},
		{"xs = [True]\nprint(xs[0] * 3)\n", "3\n"},
		{"print(sum([True, 1]))\n", "2\n"},
		{"print([True] == [1])\n", "True\n"},
		{"print(True in [1])\n", "True\n"},
		{"print(1 in {True})\n", "True\n"},
		{"d = {1: 5}\nprint(d[True])\n", "5\n"},
		{"d = {True: \"a\"}\nprint(d[1])\n", "a\n"},
		{"sa = {True, 1}\nprint(len(sa))\n", "1\n"},
		{"sa = {1, True}\nprint(sa)\n", "{1}\n"},
		{"sa = {True, 1}\nprint(sa)\n", "{True}\n"},
		{"if [True][0]:\n    print(\"yes\")\n", "yes\n"},
		{"if [False][0]:\n    print(\"no\")\nelse:\n    print(\"yes\")\n", "yes\n"},
	} {
		name := strings.ReplaceAll(strings.Split(tc.src, "\n")[0], " ", "_")
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("interpreter %s = %q, want %q", name, got, tc.want)
		}
		code, out := negBuildRun(t, "bool_num_"+name, tc.src)
		if code != 0 {
			t.Errorf("compiled %s exited %d: %s", name, code, out)
			continue
		}
		if out != tc.want {
			t.Errorf("compiled %s = %q, want %q", name, out, tc.want)
		}
	}
}

// One vocabulary, three containers. The list, the set and the dict each used to keep their own list
// of "tags that need the tagged build path", and bool slipped through the one that was edited — the
// reason `[True]` printed correctly while `{True}` and `{1: True}` were still refused. These three
// rows are the drift test for that (roadmap Gap R.112).
func TestEveryContainerShapeAsksTheOneTagTable(t *testing.T) {
	g := &irGen{}
	list := func(src string) bool {
		prog, err := parseProgram(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		return g.taggableMixedList(prog.Stmts[0].(*ExprStmt).Expr.(*ListLit))
	}
	set := func(src string) bool {
		prog, err := parseProgram(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		return g.taggableMixedSet(prog.Stmts[0].(*ExprStmt).Expr.(*SetLit))
	}
	dict := func(src string) bool {
		prog, err := parseProgram(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		return g.taggableMixedDict(prog.Stmts[0].(*ExprStmt).Expr.(*DictLit))
	}
	for _, tc := range []struct {
		src  string
		want bool
		why  string
	}{
		{`[True]`, true, "a list of nothing but verdicts has no kind to print from"},
		{`{True}`, true, "the same question, a set"},
		{`{1: True}`, true, "the same question, a dict value"},
		{`{True: 1}`, true, "the same question, a dict key"},
		{`[1, True]`, true, "a bool among numbers still needs its tag"},
		{`[1, 2]`, false, "numbers alone have a kind"},
	} {
		var got bool
		switch tc.src[0] {
		case '[':
			got = list(tc.src)
		case '{':
			if strings.Contains(tc.src, ":") {
				got = dict(tc.src)
			} else {
				got = set(tc.src)
			}
		}
		if got != tc.want {
			t.Errorf("the tag table on %s = %v, want %v (%s)", tc.src, got, tc.want, tc.why)
		}
	}
}

// A bool's payload fits an i32 slot perfectly, which is exactly why the static container path is
// the wrong place for one: nothing beside the number says what it is. Every container shape has to
// reach the tagged builder and the tag-aware printer, and say so on the object.
func TestABoolSlotIsBuiltTaggedInEveryContainerShape(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"list", "print([True, 1])\n", "call void @rt_print_list_mixed("},
		{"set", "print({True, 1})\n", "call void @rt_set_print_mixed("},
		{"dict", "print({\"k\": True})\n", "call void @rt_dict_print_mixed("},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("Compile %s: %v", tc.name, err)
		}
		for _, want := range []string{"@heap_tags", "call void @rt_tag_elem(", tc.want, "call void @rt_print_mixed_value(i32"} {
			if !strings.Contains(res.IR, want) {
				t.Errorf("%s module is missing %q\ngot tag lines: %s", tc.name, want, irLinesContaining(res.IR, "rt_tag_elem"))
			}
		}
		// The tag written beside a bool slot is the canonical TagBool, not the TagInt the number
		// it behaves like would answer with.
		if !strings.Contains(res.IR, ", i32 2)") {
			t.Errorf("%s module never tags a slot as a bool\ngot tag lines: %s", tc.name, irLinesContaining(res.IR, "rt_tag_elem"))
		}
	}
}

// A landing may not break a green promise. `xs = [True, 1]` / `i = 0` / `print(xs[i] + 1)` printed
// 2 before bools were taggable, because the list was not mixed at all and the slot read was the
// plain number; becoming a tagged container would have made that program a refusal, so the numeric
// read got a second door (numericSlotUse) that asks only the tag list.
func TestNumericUseOfABoolSlotUnderARuntimeIndexStillAnswers(t *testing.T) {
	src := "xs = [True, 1]\ni = 0\nprint(xs[i] + 1)\n"
	if got, want := captureStdout(t, src), "2\n"; got != want {
		t.Errorf("interpreter = %q, want %q", got, want)
	}
	code, out := negBuildRun(t, "bool_runtime_index", src)
	if code != 0 {
		t.Fatalf("compiled exited %d: %s (the numeric door closed on a tagged bool slot)", code, out)
	}
	if out != "2\n" {
		t.Errorf("compiled = %q, want %q", out, "2\n")
	}
}

// A set or dict comprehension of verdicts folds to a compile-time global, and a global has no tag
// table: the fold now declines, and the refusal names the half it is missing (roadmap Gap R.116,
// ADR 0259). The interpreter answers all three lines, which is what makes the compiled refusal the
// honest answer rather than the same wrong one in different clothes.
func TestAComprehensionOfVerdictsRefusesInWords(t *testing.T) {
	for _, tc := range []struct{ src, wantText, wantOut string }{
		{"print({True for x in [1]})\n", "tagged set builder", "{True}\n"},
		{"print({1: True for x in [1]})\n", "tagged dict builder", "{1: True}\n"},
		{"print({True: 1 for x in [1]})\n", "tagged dict builder", "{True: 1}\n"},
	} {
		_, err := Compile(tc.src)
		if err == nil {
			t.Errorf("%q compiled; a folded container global has no tag to write", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantText) {
			t.Errorf("%q refused with %q, want it to name the %q it is missing", tc.src, err.Error(), tc.wantText)
		}
		if got := captureStdout(t, tc.src); got != tc.wantOut {
			t.Errorf("interpreter %q = %q, want %q", tc.src, got, tc.wantOut)
		}
	}
}

// The compiled refusal boundary is unchanged by this feature: a form no expression and no tag can
// name is still refused in words with exit 1, never answered with the number underneath and never
// reaching llc with a module it rejects (ADR 0166's exit-class rule).
func TestABoolFormNothingCanNameIsStillRefused(t *testing.T) {
	for _, src := range []string{
		"print(str((1, 2)))\n",
	} {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled; the tuple pair refusal belongs to L11.3", strings.TrimSpace(src))
		}
	}
}
