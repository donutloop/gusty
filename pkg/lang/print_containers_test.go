package lang

import (
	"strings"
	"testing"
)

// print_containers_test.go — the compiled backend must print a container as a container.
//
// Print position used to ask a storage question ("does this literal contain a string, so does it
// need a heap object?"). An all-int or empty literal answered "no" and fell through to the static
// path, whose value is a *global struct* — and the module then handed @.lstN to printf as an i32.
// llc refused those programs outright; the ones that did compile printed a handle (`0`) or, for a
// recycled slot, the interned indices of a previous tenant's strings (`{(null), (null)}`). All of
// it is roadmap Gap J.6 item (3) and Gap K.3, and all of it is pinned here (ADR 0188).

func TestContainerLiteralsPrintThroughTheRuntimePrinters(t *testing.T) {
	for _, tc := range []struct {
		src     string
		printer string
	}{
		{"print([1, 2])\n", "rt_print_list"},
		{"print([])\n", "rt_print_list"},
		{"print({1, 2})\n", "rt_set_print"},
		{"print({1})\n", "rt_set_print"},
		{"print({})\n", "rt_dict_print"},
		{"print({\"a\": 1})\n", "rt_dict_print"},
		// The constructors are containers the same way a literal is; the empty set in
		// particular has no literal spelling at all.
		{"print(set())\n", "rt_set_print"},
		{"print(list())\n", "rt_print_list"},
		{"print(dict())\n", "rt_dict_print"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q: Compile: %v", tc.src, err)
		}
		if !strings.Contains(res.IR, "call void @"+tc.printer+"(") {
			t.Errorf("%q does not print through %s:\n%s", tc.src, tc.printer,
				strings.Join(irLinesContaining(res.IR, "print"), "\n"))
		}
		// The shape that llc rejects: a container global used as an i32 operand.
		for _, line := range strings.Split(res.IR, "\n") {
			if strings.Contains(line, "i32 @.") {
				t.Fatalf("%q puts a container global in a value position: %s", tc.src, strings.TrimSpace(line))
			}
		}
	}
}

// The three empty containers print CPython's own spellings. These were the answers the two-leg
// matrix could not see: the record said set(), the compiled binary said 0, and parity was
// green because the legs were never compared to anything outside the project.
func TestEmptyContainersPrintAsContainers(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(set())\n", "set()\n"},
		{"print(list())\n", "[]\n"},
		{"print(dict())\n", "{}\n"},
		{"print([])\n", "[]\n"},
		{"print({})\n", "{}\n"},
		{"print([1, 2])\n", "[1, 2]\n"},
		{"print([1, 2], sep=\", \")\n", "[1, 2]\n"},
		{"print([1], [2], sep=\" + \")\n", "[1] + [2]\n"},
		{"print([], {}, sep=\"|\")\n", "[]|{}\n"},
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

// @estr[h] is the container-wide "my elements are interned text" flag and the printers dispatch
// on it, so its lifetime has to match the container's. Slots are recycled; a flag inherited from
// the previous occupant rendered a numeric set through the string table as {(null), (null)}.
// rt_alloc clears kind and length on both paths today — the flag has to join them.
func TestAllocClearsTheStringElementFlag(t *testing.T) {
	res, err := Compile("print([1])\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	alloc := runtimeFnBody(res.IR, "define internal i32 @rt_alloc")
	for _, label := range []string{"alloc_new:", "alloc_reuse:"} {
		at := strings.Index(alloc, label)
		if at < 0 {
			t.Fatalf("rt_alloc has no %s block:\n%s", label, alloc)
		}
		body := alloc[at:]
		if next := strings.Index(body[1:], "\nalloc_"); next > 0 {
			body = body[:next+1]
		} else if end := strings.Index(body, "\n}"); end > 0 {
			body = body[:end+1]
		}
		if !strings.Contains(body, "@estr") || !strings.Contains(body, "store i32 0, i32* %eslot") {
			t.Errorf("rt_alloc %s does not clear @estr:\n%s", label, body)
		}
	}
}

// runtimeFnBody returns the text of a runtime helper's definition, from its opening brace.
func runtimeFnBody(ir, header string) string {
	at := strings.Index(ir, header)
	if at < 0 {
		return ""
	}
	rest := ir[at:]
	if end := strings.Index(rest, "\n}\n"); end > 0 {
		return rest[:end+2]
	}
	return rest
}

// A container inside a container used to be the worst kind of failure: `print([["a"], ["b"]])`
// compiled, verified, ran, and printed [1, 2] — the interned indices of the inner strings, as
// numbers, because the printer was chosen from the outer container's one recorded kind. It answers
// now: the slot carries a tag, the tag routes the print to rt_print_container_value, and that asks
// the inner object what its own elements are (roadmap L11.1). The old failure modes stay forbidden —
// a global in a value position, and an answer that is really a handle.
func TestNestedContainersPrintTheirOwnContents(t *testing.T) {
	for _, tc := range []struct {
		src     string
		printer string
		want    string
	}{
		{"print([[1], [2]])\n", "rt_print_container_value", "[[1], [2]]\n"},
		{"print([[\"a\"], [\"b\"]])\n", "rt_print_container_value", "[['a'], ['b']]\n"},
		{"xs = [[1, 2], [3]]\nprint(xs)\n", "rt_print_container_value", "[[1, 2], [3]]\n"},
		{"xs = [{\"a\": 1}]\nprint(xs)\n", "rt_print_container_value", "[{'a': 1}]\n"},
		{"xs = []\nys = [1]\nxs.append(ys)\nprint(xs)\n", "rt_print_container_value", "[[1]]\n"},
		{"print({1: {2, 3}})\n", "rt_print_container_value", "{1: {2, 3}}\n"},
		{"xs = [1]\nys = [2]\nxs.append(ys)\nprint(xs)\n", "rt_print_container_value", "[1, [2]]\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q refused: %v", tc.src, err)
		}
		assertNoForbiddenIR(t, tc.src, res.IR)
		if !strings.Contains(res.IR, "define internal void @"+tc.printer) {
			t.Errorf("%q never reached the printer that renders a nested container (%s)", tc.src, tc.printer)
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("%q AOT printed %q, want %q", tc.src, out, tc.want)
		}
		jit, jerr := JIT(tc.src, 0)
		if jerr != nil {
			t.Fatalf("%q: JIT: %v", tc.src, jerr)
		}
		if jit.Output != tc.want {
			t.Errorf("%q interpreter printed %q, want %q", tc.src, jit.Output, tc.want)
		}
	}
}

// The module-wide invariant the roadmap asked for as the L11.1 closing condition: no container
// global (@.lstN/@.dictN/@.setN) and no string-table global ever appears as an i32 value. Every
// one of those shapes — passing a literal to a function, returning one, appending one, printing
// one — has been a wrong answer or a refused module at some point.
func TestNoContainerGlobalInAValuePosition(t *testing.T) {
	for _, src := range []string{
		"xs = [1, 2]\nprint(xs)\n",
		"print([1, 2])\n",
		"print({1, 2})\n",
		"print({\"a\": 1})\n",
		"print(set())\n",
		"def f(v):\n    return v\n\nprint(f([1, 2]))\n",
		"xs = []\nxs.append(3)\nprint(xs)\n",
		"xs = [1, \"a\", None]\nprint(xs)\nfor x in xs:\n    print(x)\n",
		"xs = [1, \"a\", None]\nxs[0] = \"z\"\nxs.append(None)\nprint(xs)\nprint(xs[0])\n",
	} {
		res, err := Compile(src)
		if err != nil {
			// A refusal is fine; a module with the bad shape is not.
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Fatalf("%q failed as an IR problem: %v", src, err)
			}
			continue
		}
		for _, line := range strings.Split(res.IR, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "@.") || strings.HasPrefix(trimmed, ";") {
				continue // the global definitions themselves
			}
			if strings.Contains(trimmed, "i32 @.") {
				t.Errorf("%q emits a global in an i32 value position: %s", src, trimmed)
			}
		}
	}
}
