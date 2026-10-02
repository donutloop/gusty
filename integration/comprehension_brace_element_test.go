package integration

import (
	"strings"
	"testing"
)

// comprehension_brace_element_test.go — roadmap Gap R.74, Gap R.75, ADR 0244.
//
// Two defects met in one shape, `[{1, 2} for x in xs]`:
//
//   - The parser let a `{…}` display finish itself into a comprehension when a `for` followed its
//     `}` — correct for the call-argument form `len({x*x} for x in xs)`, wrong inside a `[`, where
//     the `for` belongs to the *enclosing* list comprehension. The program parsed as a list holding
//     one set comprehension, and both backends answered that program in agreement: `{1}` where
//     CPython prints `{1, 2}`, and one dict holding every entry where CPython prints one dict per
//     item. A parity-only suite cannot see this, so every row here is checked against CPython.
//   - The comprehension builder wrote its elements with `g.value` alone, so a container element
//     reached the slot as the compiler's *global* (`call void @rt_append_tagged(i32 %h1, i32 @.set1,
//     i32 7)`) — `llc` rejected the module and the CLI exited 2, which the exit-code contract says is
//     a compiler bug, not a refusal. Non-container elements fared differently: a float element
//     printed its box handle (`[1.5]` came out `[1]`), a `None` element folded to the integer 0, and
//     a text element made the *variable* an interned-string variable, so `print(xs)` called the
//     string printer on a list handle and wrote a bare `a`.
//
// Both are fixed: the display stops at its brace, and the element is written through the same door
// `xs.append(v)` uses — payload and tag together, with the object marked self-describing when a slot
// can only be read through its tag.

func TestComprehensionElementsMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		// The mis-parsed shape: a brace display as the element of a list comprehension.
		{"comp_over_set_literals", "d = [{1, 2} for x in [1, 2]]\nprint(len(d))\n", "2\n"},
		{"comp_over_dict_literals", "d = [{\"k\": x} for x in [1, 2]]\nprint(len(d))\n", "2\n"},
		{"print_set_element_comp", "d = [{1, 2} for x in [1]]\nprint(d)\n", "[{1, 2}]\n"},
		{"print_dict_element_comp", "d = [{\"k\": 1} for x in [1]]\nprint(d)\n", "[{'k': 1}]\n"},
		// The untagged-element writes: each of these printed the machine word, not the value.
		{"print_list_element_comp", "xs = [[1, 2] for x in [1, 2]]\nprint(xs)\n", "[[1, 2], [1, 2]]\n"},
		{"read_list_element_comp", "xs = [[1, 2] for x in [1]]\nprint(xs[0])\n", "[1, 2]\n"},
		{"read_dict_element_comp", "xs = [{\"a\": 1} for x in [1]]\nprint(xs[0])\n", "{'a': 1}\n"},
		{"float_element_comp", "xs = [1.5 for x in [1]]\nprint(xs)\n", "[1.5]\n"},
		{"float_element_read", "xs = [1.5 for x in [1]]\nprint(xs[0])\n", "1.5\n"},
		{"none_element_comp", "xs = [None for x in [1]]\nprint(xs)\n", "[None]\n"},
		{"none_element_read", "xs = [None for x in [1]]\nprint(xs[0])\n", "None\n"},
		{"text_element_comp", "xs = [\"a\" for x in [1]]\nprint(xs)\n", "['a']\n"},
		{"text_element_read", "xs = [\"a\" for x in [1]]\nprint(xs[0])\n", "a\n"},
		// Shapes that already worked, pinned so the new door cannot take them away.
		{"int_element_comp", "xs = [x + 1 for x in [1, 2]]\nprint(xs)\n", "[2, 3]\n"},
		{"int_element_arithmetic", "xs = [x + 1 for x in [1, 2]]\nprint(xs[0] + 1)\n", "3\n"},
		{"call_element_comp", "def f(v):\n    return v * 2\n\nxs = [f(x) for x in [1, 2]]\nprint(xs)\n", "[2, 4]\n"},
		{"filtered_comp", "xs = [x for x in [1, 2] if x > 1]\nprint(xs)\n", "[2]\n"},
		{"set_comp_still_a_set", "sa = {x for x in [1, 2, 2]}\nprint(len(sa))\n", "2\n"},
		{"dict_comp_still_a_dict", "da = {k: k * 2 for k in [1, 2]}\nprint(da[2])\n", "4\n"},
		{"bare_set_literal", "s = {1, 2}\nprint(len(s))\n", "2\n"},
		{"empty_dict_literal", "d = {}\nd[\"a\"] = 1\nprint(d[\"a\"])\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s",
						engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestComprehensionShapesStillRefusedHonestly pins what the compiled backend still declines, and that
// it declines by naming the missing promise. CPython answers these and the interpreter answers them
// too; the compiled leg must refuse (exit 1) and never exit 2, which would be the compiler rejecting
// its own module rather than the program.
func TestComprehensionShapesStillRefusedHonestly(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// The comprehension built the object, so no literal describes its slots: ADR 0241's
			// compile-time promise is out, and reaching into a slot by `len` says so.
			"len_of_a_slot_of_a_built_comp",
			"d = [{1, 2} for x in [1]]\nprint(len(d[0]))\n",
			"cannot reach into d's slots",
		},
		{
			"membership_in_a_slot_of_a_built_comp",
			"d = [{1, 2} for x in [1]]\nprint(1 if 2 in d[0] else 0)\n",
			"more than one kind",
		},
		{
			"comparing two slots of a built comp",
			"d = [{x} for x in [1, 2]]\nprint(1 if d[0] == d[1] else 0)\n",
			"more than one kind",
		},
		{
			"indexing a dict slot of a built comp",
			"d = [{\"k\": x} for x in [1, 2]]\nprint(d[1][\"k\"])\n",
			"index must be a constant",
		},
		{
			"len of a dict slot of a built comp",
			"d = [{\"k\": x} for x in [1, 2]]\nprint(len(d[0]))\n",
			"cannot reach into d's slots",
		},
		{
			"reaching into a built comprehension's slot",
			"xs = [[1, 2] for x in [1]]\nprint(xs[0][0] + 1)\n",
			"cannot reach into xs's slots",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_refuse.gy", tc.src)
			if py, ok := cpythonOut(t, path); !ok || py == "" {
				t.Fatalf("this row is about a program CPython answers")
			}
			if out, code := cliRunCode(t, "--interp", path); code != 0 {
				t.Fatalf("--interp exited %d on a program CPython answers: %s", code, out)
			}
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("--aot rejected the compiler's own module (exit 2): %s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				t.Fatalf("--aot answered %q where this row is about a refusal", out)
			}
			if combined := cliRun(t, "--aot", path); !strings.Contains(combined, tc.want) {
				t.Fatalf("refusal %q does not name %q", combined, tc.want)
			}
		})
	}
}

// TestComprehensionFailuresMatchCPython pins the rows where CPython itself dies: the element exists
// but the program gets a position wrong. Neither engine may exit 0 with a value.
func TestComprehensionFailuresMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"index_out_of_range",
			"xs = [[1, 2] for x in [1]]\nprint(xs[5])\n",
			"list index out of range",
		},
		{
			"subscript_a_number",
			"xs = [[1, 2] for x in [1]]\nprint(xs[0][0][0])\n",
			"'int' object is not subscriptable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 || !strings.Contains(py, tc.want) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, "Traceback") && !strings.Contains(combined, "codegen") {
					t.Errorf("%s neither raised nor refused: %s", engine, combined)
				}
			}
		})
	}
}
