package lang

import (
	"strings"
	"testing"
)

// container_eq_test.go — containers compare by value, on the compiled backend.
//
// `xs == ys` for two equal lists answered False in the record and in the compiled binary,
// because an element of the value model is an i32 and the comparison compared them: two handles,
// two slots, therefore unequal. Meanwhile a literal operand made the module invalid — `icmp eq
// i32 @.lst1, 1` — so `if [1] == 1:` could not be compiled at all. Python compares containers by
// value and reserves `is` for identity, and that is what this pins (roadmap L11.1, ADR 0189).

func TestContainersCompareByValueCompiled(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"def f(a, b):\n    return a == b\n\nxs = [1, 2]\nys = [1, 2]\nprint(f(xs, ys))\n", "True\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f([1, 2], [2, 1]))\n", "False\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f([\"a\"], [\"a\"]))\n", "True\n"},
		// The payload is not enough: a stored string is an index into @str_tab, and the
		// number 1 is a payload that can equal it. The tag is what tells them apart.
		{"def f(a, b):\n    return a == b\n\nprint(f([1], [\"1\"]))\n", "False\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f([1, \"a\", None], [1, \"a\", None]))\n", "True\n"},
		// Sets and dicts are unordered: equality is containment, not slot order.
		{"def f(a, b):\n    return a == b\n\nprint(f({1, 2}, {2, 1}))\n", "True\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f({1, 2}, {1, 2, 3}))\n", "False\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f({\"a\": 1, \"b\": 2}, {\"b\": 2, \"a\": 1}))\n", "True\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f({\"a\": 1}, {\"a\": 2}))\n", "False\n"},
		// A container against a scalar is simply unequal, as Python says — not a trap.
		{"xs = [1]\nprint(xs == 1)\n", "False\n"},
		{"print([1] == 1)\n", "False\n"},
		// Growth changes the answer, so the tags written by append must be real.
		{"xs = [1, 2]\nys = [1, 2]\nxs.append(3)\nif xs == ys:\n    print(\"same\")\nelse:\n    print(\"grown\")\n", "grown\n"},
		// A slot written by mutation keeps its tag, and a comparison reads the pair.
		{"d = {\"a\": 1}\ne = {\"a\": 1}\nd[\"b\"] = 2\nif d == e:\n    print(\"same\")\nelse:\n    print(\"grew\")\n", "grew\n"},
		{"s = {1, 2}\nt = {1, 2}\ns.add(3)\nif s == t:\n    print(\"same\")\nelse:\n    print(\"more\")\n", "more\n"},
		// `is` is still identity, and it is a different question from `==`.
		{"xs = [1, 2]\nys = [1, 2]\nprint(xs is xs)\nprint(xs is ys)\n", "True\nFalse\n"},
	} {
		res, err := JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("%q: JIT: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}

// The same table through the record: the compiled backend share an expectation, and it is
// CPython's (integration/container_equality_test.go checks the third leg).
func TestContainersCompareByValueInterpreted(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"xs = [1, 2]\nys = [1, 2]\nprint(xs == ys)\n", "True\n"},
		{"xs = [1, 2]\nys = [2, 1]\nprint(xs == ys)\n", "False\n"},
		{"xs = [1, 2]\nprint(xs != xs)\n", "False\n"},
		{"print([1] != [2])\n", "True\n"},
		{"print({1, 2} == {2, 1})\n", "True\n"},
		{"print({\"a\": 1, \"b\": 2} == {\"b\": 2, \"a\": 1})\n", "True\n"},
		{"print([1] == 1)\n", "False\n"},
		{"print([] == [])\n", "True\n"},
	} {
		res, err := JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("%q: JIT: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("compiled %q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}

// The shape that used to be invalid IR: a container literal on one side of a comparison reached
// the static path, and its elements-array global went into an icmp as an i32.
func TestContainerComparisonNeverEmitsAGlobalInValuePosition(t *testing.T) {
	for _, src := range []string{
		"if [1] == 1:\n    print(\"y\")\n",
		"if [1, 2] == [1, 2]:\n    print(\"y\")\n",
		"if {\"a\": 1} == {\"a\": 1}:\n    print(\"y\")\n",
		"if {1, 2} == {1, 2}:\n    print(\"y\")\n",
		"xs = [1]\nif xs == [1]:\n    print(\"y\")\n",
		"xs = [1]\nwhile xs == [1]:\n    print(\"y\")\n    xs = []\n",
	} {
		res, err := Compile(src)
		if err != nil {
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Fatalf("%q failed as an IR problem instead of a front-end decision: %v", src, err)
			}
			continue
		}
		for _, line := range strings.Split(res.IR, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "@.") {
				continue
			}
			if strings.Contains(trimmed, "i32 @.") {
				t.Errorf("%q puts a container global in a value position: %s", src, trimmed)
			}
		}
	}
}

// A comparison of a container against something whose kind the compiler cannot see refuses. The
// two i32s are both *some* number; deciding which of them is a handle needs the tagged value of
// L11.2, and guessing is how this repo keeps producing answers CPython does not give.
func TestContainerCompareWithUnknownKindRefuses(t *testing.T) {
	for _, src := range []string{
		"def make():\n    return [1]\n\nxs = [1]\nprint(xs == make())\n",
		"def probe(v):\n    return v\n\nxs = [1]\nprint(xs == probe(1))\n",
	} {
		_, err := Compile(src)
		if err == nil {
			t.Fatalf("%q compiled; comparing a container with an unknown kind must refuse", src)
		}
		if !strings.Contains(err.Error(), "needs a tagged value") {
			t.Errorf("%q refused with %q, want it to name the missing tagged value", src, err.Error())
		}
		if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
			t.Errorf("%q failed as an IR problem: %v", src, err)
		}
	}
}

// Every builder has to write tags, not just the mixed-list one: rt_slot_eq compares (payload,
// tag), and a slot whose tag was never written holds whatever the previous tenant of that heap
// slot left behind. This is the assertion that catches a new builder forgetting the pair — the
// bug lived here for a whole cycle as a *call argument* builder that hand-rolled its own loop.
func TestEveryContainerBuilderWritesTags(t *testing.T) {
	for _, tc := range []struct{ src, builder string }{
		{"xs = [1, \"a\"]\nprint(len(xs))\n", "list literal"},
		{"xs = []\nxs.append(1)\nxs.append(2)\nprint(len(xs))\n", "append"},
		{"xs = [1]\nxs[0] = \"a\"\nprint(len(xs))\n", "item write"},
		{"s = {1, 2}\nprint(len(s))\n", "set literal"},
		{"s = set()\ns.add(1)\ns.add(2)\nprint(len(s))\n", "set add"},
		{"d = {\"a\": 1, \"b\": None}\nprint(len(d))\n", "dict literal"},
		{"d = {}\nd[\"a\"] = 1\nd[\"b\"] = 2\nprint(len(d))\n", "dict item write"},
		{"def f(v):\n    return len(v)\n\nprint(f([1, \"a\"]))\n", "call argument list"},
		{"def f(v):\n    return len(v)\n\nprint(f({1, 2}))\n", "call argument set"},
		{"def f(v):\n    return len(v)\n\nprint(f({\"a\": 1, \"b\": \"c\"}))\n", "call argument dict"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q (%s): Compile: %v", tc.src, tc.builder, err)
		}
		if !strings.Contains(res.IR, "call void @rt_tag_elem(") &&
			!strings.Contains(res.IR, "rt_append_tagged(") &&
			!strings.Contains(res.IR, "rt_set_add_tagged(") &&
			!strings.Contains(res.IR, "rt_dict_put_tagged(") {
			t.Errorf("%q: the %s wrote payloads without tags:\n%s", tc.src, tc.builder,
				strings.Join(irLinesContaining(res.IR, "rt_"), "\n"))
		}
	}
}

// rt_container_eq must be one helper with the three container shapes in it, and the element
// comparison must read @heap_tags: without the tag half, [1] and ["1"] are the same i32.
func TestContainerEqHelperReadsTags(t *testing.T) {
	res, err := Compile("def f(a, b):\n    return a == b\n\nxs = [1]\nys = [\"1\"]\nprint(f(xs, ys))\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.IR, "call i32 @rt_container_eq(") {
		t.Fatalf("no rt_container_eq call:\n%s", strings.Join(irLinesContaining(res.IR, "rt_container_eq"), "\n"))
	}
	slot := runtimeFnBody(res.IR, "define internal i32 @rt_slot_eq")
	for _, want := range []string{"@heap_tags", "call i32 @rt_payload_eq("} {
		if !strings.Contains(slot, want) {
			t.Errorf("rt_slot_eq is missing %q:\n%s", want, slot)
		}
	}
	// The comparison itself moved to rt_payload_eq when a float slot joined the picture: an int
	// slot and a float slot holding the same number are equal, and no pair of i32 compares says
	// that. The pair is still what decides it, so the arithmetic-free half of the answer stays an
	// icmp of payload and tag (roadmap L11.1, ADR 0189 and ADR 0233).
	pay := runtimeFnBody(res.IR, "define internal i32 @rt_payload_eq")
	for _, want := range []string{"icmp eq i32", "and i1", "fcmp oeq double", "@rt_float_of"} {
		if !strings.Contains(pay, want) {
			t.Errorf("rt_payload_eq is missing %q:\n%s", want, pay)
		}
	}
	if !strings.Contains(runtimeFnBody(res.IR, "define internal i32 @rt_container_eq"), "call i32 @rt_slot_eq(") {
		t.Errorf("rt_container_eq must compare slots through rt_slot_eq")
	}
}

// The payload collision, which is the reason rt_slot_eq compares the pair and not just the
// payload: a stored string is an index into @str_tab, and the number 0 is a payload that equals
// the first index. Stub check — delete the builder tag stores from heapargs.go and this test
// answers 1 (True) where CPython answers False, so the tag, not the payload, is what separates
// a number from a word (ADR 0189).
func TestTaggedElementsDistinguishPayloadCollisions(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"def f(a, b):\n    return a == b\n\nprint(f([0], [\"zero\"]))\n", "False\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f([\"zero\"], [0]))\n", "False\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f({0: 1}, {\"zero\": 1}))\n", "False\n"},
		{"def f(a, b):\n    return a == b\n\nprint(f([0, \"zero\"], [0, \"zero\"]))\n", "True\n"},
	} {
		res, err := JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("%q: JIT: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Errorf("%q = %q, want %q", tc.src, res.Output, tc.want)
		}
	}
}
