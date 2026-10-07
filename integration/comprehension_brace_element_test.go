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
//     one set comprehension, and the compiled path answered that program in agreement: `{1}` where
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
		// Reaching into a slot the comprehension itself built, by the object's own tag array: the
		// comprehension wrote payload and tag together, so `len` asks the object (ADR 0246) — these
		// two were refusal rows until the read stopped needing a literal.
		{"len_of_a_slot_of_a_built_comp", "d = [{1, 2} for x in [1]]\nprint(len(d[0]))\n", "2\n"},
		{"len_of_a_dict_slot_of_a_built_comp", "d = [{\"k\": x} for x in [1, 2]]\nprint(len(d[0]))\n", "1\n"},
		{
			// Two slots of a comprehension the program built, compared: each side is a (payload, tag)
			// pair and the pair is the operand the equality needed, so this refusal row moved here
			// (ADR 0247, Gap R.79's family).
			"comparing_two_slots_of_a_built_comp",
			"d = [{x} for x in [1, 2]]\nprint(1 if d[0] == d[1] else 0)\n",
			"0\n",
		},
		{
			// Reaching one level below a slot of a comprehension the program built. The element was
			// written payload-and-tag by the comprehension's own builder, so the read asks the object
			// which kind it was given instead of asking for a literal that never existed; this refusal
			// row moved here with ADR 0251 (roadmap L11.1).
			"indexing a dict slot of a built comp",
			"d = [{\"k\": x} for x in [1, 2]]\nprint(d[1][\"k\"])\n",
			"2\n",
		},
		{
			"indexing a list slot of a built comp",
			"xs = [[x, x * 2] for x in [1, 2]]\nprint(xs[1][1])\n",
			"4\n",
		},
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
			for _, engine := range cliEngines {
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
// it declines by naming the missing promise. CPython answers these and the record answers them
// too; the compiled leg must refuse (exit 1) and never exit 2, which would be the compiler rejecting
// its own module rather than the program.
func TestComprehensionShapesStillRefusedHonestly(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// (Two `len(...)` rows used to sit here demanding refusals — `len(d[0])` of a comprehension
			// whose element is a set, and of one whose element is a dict. The object carries its own tags,
			// so both answer CPython's answer now and are pinned in TestComprehensionElementsMatchCPython;
			// ADR 0246.)
			"membership_in_a_slot_of_a_built_comp",
			"d = [{1, 2} for x in [1]]\nprint(1 if 2 in d[0] else 0)\n",
			"more than one kind",
		},
		{
			// (A `indexing a dict slot of a built comp` row used to sit here demanding the refusal
			// "index must be a constant". The read below a comprehension's slot is answered by the tag the
			// comprehension's builder wrote, so it prints CPython's answer now and is pinned in
			// TestComprehensionElementsMatchCPython; ADR 0251.)
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
			// CPython answers, which is what makes this row's refusal a limit of this backend rather
			// than a limit of the language. The claim, then, is the two-way one: the compiled path
			// answers, or it refuses with the missing half named — and the row's own `want` column
			// checks the sentence, so an honest refusal is checked and a mute one fails.
			if _, code := cliRunCode(t, "--aot", path); code == exitIRVerify {
				t.Fatalf("--aot: exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", cliRun(t, "--aot", path))
			} else if code != 0 {
				combined := cliRun(t, "--aot", path)
				if code != 1 || !refusesHonestly(combined) {
					t.Fatalf("--aot exited %d on a program CPython answers, without naming the missing half: %s", code, combined)
				}
				noteCompiledGap(t, tc.src, combined)
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
			for _, engine := range cliEngines {
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

// TestComprehensionOverAMixedContainerMatchesCPython is Gap R.76 answered rather than refused.
//
// The comprehension's loop variable used to be a plain load: `for` over a container whose slots mix
// kinds binds the element's tag alongside its payload (ADR 0185, ADR 0241), and the comprehension loop
// never caught up. So `out = [x for x in sa]` over {1, "a", None} printed [1, 0, 0] — three numbers,
// two of them zeros, where CPython prints [1, 'a', None] — and iterating a dict read slot 0 and slot 1
// of a two-word entry, answering ['a', 1] for {"a": 1, 2: "b"}: a key and a value wearing the two keys'
// place. The loop now binds the same (payload, tag) pair `for` binds, walks a dict's keys at stride 2,
// and registers the list it builds as one whose slots speak for themselves, because nothing static says
// what is in it (ADR 0232).
//
// A set's iteration order is this runtime's own business — CPython's moves with the hash seed — so the
// set rows ask length and membership, and pin a printed list only where the order is agreed.
func TestComprehensionOverAMixedContainerMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"mixed_list",
			"xs = []\nxs.append(1)\nxs.append(\"a\")\nxs.append(None)\nout = [x for x in xs]\nprint(out)\n",
			"[1, 'a', None]\n",
		},
		{
			"mixed_list_every_slot",
			"xs = []\nxs.append(1)\nxs.append(\"a\")\nxs.append(None)\nout = [x for x in xs]\nprint(out[0])\nprint(out[1])\nprint(out[2])\n",
			"1\na\nNone\n",
		},
		{
			// The float is the row ADR 0238 exists for: the slot holds the handle of a box, and only
			// the tag says so.
			"mixed_list_with_a_float",
			"xs = []\nxs.append(1)\nxs.append(1.5)\nxs.append(\"a\")\nxs.append(None)\nout = [x for x in xs]\nprint(out)\n",
			"[1, 1.5, 'a', None]\n",
		},
		{"mixed_list_membership", "xs = []\nxs.append(1)\nxs.append(\"a\")\nout = [x for x in xs]\nprint(1 if \"a\" in out else 0)\nprint(1 if \"z\" in out else 0)\nprint(len(out))\n", "1\n0\n2\n"},
		{
			"mixed_list_filtered_by_a_number",
			"xs = []\nxs.append(1)\nxs.append(\"a\")\nsel = [x for x in xs if x == 1]\nprint(len(sel))\nprint(sel[0])\n",
			"1\n1\n",
		},
		{
			"mixed_list_against_an_equal_list",
			"xs = []\nxs.append(1)\nxs.append(\"a\")\nys = [x for x in xs]\nzs = [x for x in xs]\nprint(1 if ys == zs else 0)\n",
			"1\n",
		},
		{
			"mixed_set",
			"sa = {1, \"a\", None}\nout = [x for x in sa]\nprint(len(out))\nprint(1 if 1 in out else 0)\nprint(1 if \"a\" in out else 0)\nprint(1 if None in out else 0)\n",
			"3\n1\n1\n1\n",
		},
		{
			// A set that has grown, walked into a list. What the row asserts is length and membership,
			// not the printed order: CPython's order for a set containing text moves with the hash seed
			// (an unseeded `python3` re-orders `{1, "a"}` between runs), so the oracle leg used to fail
			// on roughly one run in six — a row that pins the *oracle's* nondeterminism is worse than no
			// row, because it reads as a regression whenever the seed says so. The engines' own
			// insertion order is pinned without an oracle leg in
			// `pkg/lang/comprehension_brace_element_test.go` (roadmap Gap R.84).
			"mixed_set_grown",
			"sa = set()\nsa.add(1)\nsa.add(\"a\")\nout = [x for x in sa]\nprint(len(out))\nprint(1 if 1 in out else 0)\nprint(1 if \"a\" in out else 0)\n",
			"2\n1\n1\n",
		},
		{"mixed_set_into_a_set", "sa = {1, \"a\", None}\nout = {x for x in sa}\nprint(len(out))\nprint(1 if \"a\" in out else 0)\n", "3\n1\n"},
		{
			// Iterating a dict yields its keys, and a dict entry is two words: the position is the
			// counter doubled, the same pair of facts `for v in d:` applies (ADR 0188).
			"mixed_dict_keys",
			"d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = [k for k in d]\nprint(out)\n",
			"['a', 2]\n",
		},
		{"mixed_dict_values_asked_as_keys", "d = {1: \"x\", \"k\": 2}\nout = [v for v in d]\nprint(out)\n", "[1, 'k']\n"},
		{
			// A dict comprehension whose key arrives from a mixed dict: the entry is two (payload,
			// tag) pairs, and a pair is only ever written as a pair.
			"mixed_dict_into_a_dict",
			"d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = {k: 1 for k in d}\nprint(out)\nprint(len(out))\n",
			"{'a': 1, 2: 1}\n2\n",
		},
		{
			"mixed_dict_into_a_dict_value_read",
			"d = {}\nd[\"a\"] = 1\nd[2] = \"b\"\nout = {k: 1 for k in d}\nprint(out[\"a\"])\nprint(out[2])\n",
			"1\n1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_mixed.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (exit 2): %s", engine, cliRun(t, engine, path))
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

// TestComprehensionOverAMixedContainerStillRefusesHonestly keeps the shapes the tagged loop variable
// does not reach. Each is answered by CPython and by the record; the compiled backend declines by
// naming the promise it is missing, and never by exiting 2.
func TestComprehensionOverAMixedContainerStillRefusesHonestly(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// A name the escape analysis kept as a compile-time list has no heap object to walk at all,
			// and emitting the load is the module `llc` rejects.
			"compile_time_list_as_iterable",
			"xs = [1, \"a\", None]\nout = [x for x in xs]\nprint(out)\n",
			"kept as a compile-time constant",
		},
		{
			"literal_bound_text_list_read_by_slot",
			"names = [\"a\", \"b\"]\nout = [n for n in names]\nprint(out[0])\n",
			"kept as a compile-time constant",
		},
		// (A third row sat here demanding that `out[1] == "a"` refuse: "the slot's tag is carried but a
		// `==` against text needs it as an operand the comparison lowering cannot build yet." It was
		// right when written and wrong afterwards — the pair the printer already had is exactly the
		// operand an equality needs — so the row moved to TestSlotEqualityMatchesCPython
		// (`comprehension_slot_against_text`) and is asserted against CPython on the compiled path there
		// (roadmap Gap R.79, ADR 0247).)
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_tagged_refuse.gy", tc.src)
			if py, ok := cpythonOut(t, path); !ok || py == "" {
				t.Fatalf("this row is about a program CPython answers")
			}
			// CPython answers, which is what makes this row's refusal a limit of this backend rather
			// than a limit of the language. The claim, then, is the two-way one: the compiled path
			// answers, or it refuses with the missing half named — and the row's own `want` column
			// checks the sentence, so an honest refusal is checked and a mute one fails.
			if _, code := cliRunCode(t, "--aot", path); code == exitIRVerify {
				t.Fatalf("--aot: exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", cliRun(t, "--aot", path))
			} else if code != 0 {
				combined := cliRun(t, "--aot", path)
				if code != 1 || !refusesHonestly(combined) {
					t.Fatalf("--aot exited %d on a program CPython answers, without naming the missing half: %s", code, combined)
				}
				noteCompiledGap(t, tc.src, combined)
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

// TestComprehensionOverAOneKindContainerMatchesCPython is the same shape with the mixed kinds taken
// out, and it works on the compiled path: an element that *is* the loop variable has to leave the bound
// list registered with the kind its slots hold. It used to be registered as a list of numbers, so
// `print(out)` asked the object and printed ['a'] while `print(out[0])` asked the compiler and printed
// 0 — two answers to one question. ADR 0241's pair (mark the object *and* the variable) is what the
// rows below check, each printing the container and reading a slot of it.
func TestComprehensionOverAOneKindContainerMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"grown_text_list",
			"names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names]\nprint(out)\nprint(out[0])\n",
			"['a', 'b', 'c']\na\n",
		},
		{
			"grown_text_list_filter",
			"names = []\nnames.append(\"a\")\nnames.append(\"b\")\nout = [n for n in names if n == \"a\"]\nprint(len(out))\nprint(out[0])\nprint(out)\n",
			"1\na\n['a']\n",
		},
		{
			"grown_text_list_every_slot",
			"names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names]\nprint(out[0])\nprint(out[1])\nprint(out[2])\n",
			"a\nb\nc\n",
		},
		{
			// A set of strings has no agreed iteration order — CPython's own varies with the hash
			// seed — so this row asks length and membership, which is all of the element's identity
			// that does not depend on the order.
			"text_set_element",
			"sa = {\"a\", \"b\"}\nout = [n for n in sa]\nprint(len(out))\nprint(1 if \"a\" in out else 0)\nprint(1 if \"z\" in out else 0)\n",
			"2\n1\n0\n",
		},
		{
			"dict_keys_all_text",
			"d = {}\nd[\"a\"] = 1\nd[\"b\"] = 2\nout = [k for k in d]\nprint(out)\n",
			"['a', 'b']\n",
		},
		{
			"dict_values_all_text",
			"d = {}\nd[\"a\"] = 1\nout = [v for v in d]\nprint(out)\nprint(out[0])\n",
			"['a']\na\n",
		},
		{
			"grown_int_list",
			"xs = []\nxs.append(1)\nxs.append(2)\nout = [x * 2 for x in xs]\nprint(out)\nprint(out[1])\n",
			"[2, 4]\n4\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_onekind.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (exit 2): %s", engine, cliRun(t, engine, path))
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

// TestComprehensionOverAMixedContainerFailsLikeCPython — `xs = [1, "a"]; [x + 0 for x in xs]` is
// CPython's `TypeError: can only concatenate str (not "int") to str`, and the record raises it:
// the tagged element reached the `+` as what it is. The compiled backend never gets that far, because
// the same shape is the refusal above; the row only checks that neither engine exits 0 with a value,
// and that the record's own failure names what CPython names.
func TestComprehensionOverAMixedContainerFailsLikeCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"text_element_in_addition", "xs = [1, \"a\"]\nxs.append(2)\nout = [x + 0 for x in xs]\nprint(out)\n", "can only concatenate str"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_mixed_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 || !strings.Contains(py, tc.want) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if code == 2 {
					t.Errorf("%s exited 2 on a program the oracle rejects: %s", engine, cliRun(t, engine, path))
				}
			}
			// The row names the sentence the reference uses. The compiled path may say it, or may name
			// the half of itself it is missing instead — the important property is that the program is
			// refused with an explanation, not that our diagnostic copies CPython's phrasing word for word.
			if combined := cliRun(t, "--aot", path); !strings.Contains(combined, tc.want) {
				if !refusesHonestly(combined) {
					t.Errorf("--aot named neither %q nor any missing half: %s", tc.want, combined)
				}
				noteCompiledGap(t, tc.src, combined)
			}
		})
	}
}

// TestDictComprehensionEntriesCarryTheirOwnKeysAndValues is Gap R.77 and Gap R.78: the two ways a dict
// comprehension used to answer without knowing what it was writing.
//
// Iterating a dict yields its **keys**, and an entry occupies two words, so the iteration has to scale
// its counter by 2 and measure the object in entries (ADR 0188) — `for v in d:` already did, and the
// comprehension loop did not. Reading slots 0 and 1 of a two-entry dict answers one key and one value:
// `{1: "x", 2: "y"}` printed `[1, 0]`, with no mixed kind anywhere in the program to blame.
//
// And the entry write asked `elemKindTag` for the tag of a key that is the loop variable of a text
// container, which answers *int* — so the interned index went in as an integer, `print(out)` wrote
// `{0: 1}`, and `out["a"]`, which looks text up by index and tag together, died with `KeyError: key not
// found` while the record and CPython both printed 1.
func TestDictComprehensionEntriesCarryTheirOwnKeysAndValues(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"text_key_is_findable_by_its_text",
			"d = {}\nd[\"a\"] = 1\nout = {k: 1 for k in d}\nprint(len(out))\nprint(out[\"a\"])\n",
			"1\n1\n",
		},
		{
			"text_key_prints_as_text",
			"d = {}\nd[\"a\"] = 1\nout = {k: 1 for k in d}\nprint(out)\n",
			"{'a': 1}\n",
		},
		{
			"text_key_as_its_own_value",
			"d = {}\nd[\"a\"] = 1\nout = {k: k for k in d}\nprint(out)\n",
			"{'a': 'a'}\n",
		},
		{
			// The stride row, with no mixed kind anywhere: keys 1 and 2, values text.
			"int_keys_are_both_keys",
			"d = {}\nd[1] = \"x\"\nd[2] = \"y\"\nout = [k for k in d]\nprint(out)\nprint(len(out))\n",
			"[1, 2]\n2\n",
		},
		{
			"int_keys_into_a_dict",
			"d = {}\nd[1] = \"x\"\nd[2] = \"y\"\nout = {k: 1 for k in d}\nprint(out)\nprint(out[2])\n",
			"{1: 1, 2: 1}\n1\n",
		},
		{
			"text_dict_keys_into_a_list",
			"d = {}\nd[\"a\"] = 1\nd[\"b\"] = 2\nout = [k for k in d]\nprint(out)\nprint(1 if \"a\" in out else 0)\n",
			"['a', 'b']\n1\n",
		},
		{
			// A set's elements are keys too, and the same two facts decide what lands in the entry.
			"text_set_into_a_dict",
			"sa = {\"a\", \"b\"}\nout = {x: 1 for x in sa}\nprint(len(out))\nprint(out[\"b\"])\nprint(1 if out[\"a\"] == 1 else 0)\n",
			"2\n1\n1\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "comp_dict_entry.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (exit 2): %s", engine, cliRun(t, engine, path))
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
