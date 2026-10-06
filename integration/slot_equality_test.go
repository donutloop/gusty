package integration

import (
	"strings"
	"testing"
)

// slot_equality_test.go — comparing a container slot against a value (roadmap L11.1, Gap R.79).
//
// A slot of a container whose slots describe themselves is a (payload, tag) pair. The tag was
// already carried to the printer — `print(out[1])` rendered `a` for a slot the compiler could not
// type — but it stopped there, so the ordinary question `out[1] == "a"` was refused while the
// ordinary statement `print(out[1])` answered. Two ways of reading one slot, one of them usable.
//
// The door here is the pair on both sides of `==`/`!=` and one comparison answering it:
// `rt_payload_eq`, the same equality `rt_slot_eq` uses to walk two containers, so a slot cannot
// answer one question one way and the next question another. That also retired `rt_mixed_eq`, which
// compared the words once the tags matched — two float slots holding 1.5 hold two different box
// handles, and it called them unequal while the printer one line away called them 1.5.
//
// Every row below is checked against CPython first, on the compiled path: the interpreter answers these
// today, so a compiled refusal is a divergence, not a limitation the program deserves.

func TestSlotEqualityMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"mixed_slot_against_text", "xs = [1, \"a\"]\nprint(1 if xs[1] == \"a\" else 0)\n", "1\n"},
		{"mixed_slot_the_other_answer", "xs = [1, \"a\"]\nprint(1 if xs[0] == \"a\" else 0)\n", "0\n"},
		{"two_slot_reads", "xs = [1, \"a\"]\nprint(1 if xs[0] == xs[1] else 0)\nprint(1 if xs[0] == xs[0] else 0)\n", "0\n1\n"},
		{"int_and_the_index_of_its_own_text", "xs = [0, \"zero\"]\nprint(1 if xs[0] == xs[1] else 0)\n", "0\n"},
		{"none_against_zero_and_text", "xs = [None, 0, \"None\"]\nprint(1 if xs[0] == xs[1] else 0)\nprint(1 if xs[0] == xs[2] else 0)\nprint(1 if xs[0] == None else 0)\n", "0\n0\n1\n"},
		{"float_slot_against_its_number", "xs = [1.5, \"a\"]\ny = xs[0]\nprint(1 if y == 1.5 else 0)\nprint(1 if y == 1.6 else 0)\n", "1\n0\n"},
		{"across_the_numeric_tags", "xs = [1.0, \"a\"]\nprint(1 if xs[0] == 1 else 0)\nxs2 = [1, \"a\"]\nprint(1 if xs2[0] == 1.0 else 0)\n", "1\n1\n"},
		{"runtime_index", "xs = [1, \"a\"]\ni = 1\nprint(1 if xs[i] == \"a\" else 0)\nj = -1\nprint(1 if xs[j] == \"a\" else 0)\n", "1\n1\n"},
		{"container_slot_against_literal", "xs = []\nxs.append([1, 2])\nprint(1 if xs[0] == [1, 2] else 0)\nprint(1 if xs[0] != [1, 2] else 0)\n", "1\n0\n"},
		{"dict_entry_holding_a_container", "d = {}\nd[\"k\"] = [1, 2]\nprint(1 if d[\"k\"] == [1, 2] else 0)\n", "1\n"},
		{"comprehension_slot_against_text", "xs = []\nxs.append(1)\nxs.append(\"a\")\nout = [x for x in xs]\nprint(1 if out[1] == \"a\" else 0)\nprint(1 if out[0] == 1 else 0)\n", "1\n1\n"},
		{"int_key_and_text_key", "d = {1: \"one\", \"a\": 2}\nprint(1 if d[\"a\"] == 2 else 0)\nprint(1 if d[1] == \"one\" else 0)\n", "1\n1\n"},
		{"set_slot_against_set", "xs = [{1, 2}, \"a\"]\nprint(1 if xs[0] == {2, 1} else 0)\n", "1\n"},
		{"in_a_condition", "xs = [1, \"a\"]\nif xs[1] == \"a\":\n    print(\"hit\")\nelse:\n    print(\"miss\")\n", "hit\n"},
		{"loop_variable_three_kinds", "xs = [1, \"a\", None, 1.5]\nfor x in xs:\n    print(1 if x == \"a\" else 0, 1 if x == 1 else 0, 1 if x == None else 0)\n", "0 1 0\n1 0 0\n0 0 1\n0 0 0\n"},
		{"uniform_text_container", "xs = [\"a\", \"b\"]\nprint(1 if xs[1] == \"b\" else 0)\n", "1\n"},
		{"int_keyed_dict_mixed_values", "m = {0: [1, 2], 1: 3}\nprint(1 if m[0] == [1, 2] else 0)\nprint(1 if m[1] == 3 else 0)\n", "1\n1\n"},
		{"nested_dict_of_lists", "d = {\"a\": [1, 2], \"b\": [3]}\nprint(1 if d[\"a\"] == [1, 2] else 0)\nprint(1 if d[\"b\"] == [1, 2] else 0)\n", "1\n0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "eq.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
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

// The comparison keeps the traps the read already had: an out-of-range position and a missing key
// are the same failures they were for `print(xs[i])`, and they arrive as exceptions, not as exit 0.
func TestSlotEqualityTrapsMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"position_out_of_range",
			"xs = [1, \"a\"]\ni = 5\nprint(1 if xs[i] == \"a\" else 0)\n",
			"list index out of range",
		},
		{
			"missing_key",
			"d = {}\nd[\"a\"] = 1\nprint(1 if d[\"b\"] == 1 else 0)\n",
			"KeyError",
		},
		{
			// Ordering is the comparison this door does not open: an ordering of two tags needs the
			// relational operand too, and until it exists the compiled path declines rather than
			// picking a winner by payload.
			"text_below_a_number",
			"xs = [1, \"a\"]\nprint(1 if xs[1] < 1 else 0)\n",
			"'<' not supported between instances of 'str' and 'int'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "eq_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 ||
				!strings.Contains(py, "Traceback (most recent call last):") ||
				!strings.Contains(py, tc.want) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if strings.Contains(out, "Traceback") {
					t.Errorf("%s put the report on stdout: %q", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, "Traceback") && !strings.Contains(combined, "codegen") {
					t.Errorf("%s neither raised nor refused: %s", engine, combined)
				}
			}
		})
	}
}

// The comparison against an expression whose kind cannot be proven used to answer with the two
// words, which is how `xs[0] == f()` was true whenever f returned the text whose interned index
// happened to be the number in the slot. It is refused now, in exit class 1, and the message names
// the missing kind rather than blaming the shape of the program.
func TestSlotEqualityRefusesAnUnprovableKind(t *testing.T) {
	src := "def pick(c):\n    return \"z\"\n    return 7\n\nxs = [1, \"a\"]\nprint(1 if xs[0] == pick(1) else 0)\n"
	path := writeSrc(t, t.TempDir(), "eq_refuse.gy", src)
	if py, ok := cpythonOut(t, path); !ok || py != "0\n" {
		t.Fatalf("this row is about a program CPython answers 0, got %q", py)
	}
	// Answer CPython's 0, or refuse naming the unprovable kind (which half of the row is pinned just
	// below, where the refusal's words are checked).
	if out, code := cliRunMerged(t, "--aot", path); !(code == 0 && out == "0\n") && !(code == 1 && refusesHonestly(out)) {
		t.Fatalf("--aot printed %q (exit %d), want CPython's 0 or a refusal naming the missing half", out, code)
	} else if code == 1 {
		noteCompiledGap(t, src, out)
	}
	out, code := cliRunCode(t, "--aot", path)
	if code == 2 {
		t.Fatalf("--aot rejected the compiler's own module (exit 2): %s", cliRun(t, "--aot", path))
	}
	if code == 0 {
		t.Fatalf("--aot answered %q where an untagged compare would have answered 1 — a coincidence, not a verdict", out)
	}
	if combined := cliRun(t, "--aot", path); !strings.Contains(combined, "kind the compiler can prove") {
		t.Fatalf("refusal does not name the missing kind: %s", combined)
	}
}
