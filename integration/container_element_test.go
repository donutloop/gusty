package integration

import (
	"strings"
	"testing"
)

// End-to-end coverage for roadmap Gap R.40 (ADR 0226). Expectations are CPython's, taken before
// judging either backend. What this family used to produce was exit 2 — `llc` rejecting a module the
// compiler invented an operand for — for programs whose answers CPython prints in one line:
//
//	print(1 if [1] == [1.0] else 0)   -> exit 2   `[1 x i32] [@env_store = internal global ...`
//	print([1.5, 2])                   -> exit 2   `%t1 = sitofp i32  to double`
//	1.0 == [1]                        -> exit 2   `%t2 = sitofp i32 @.lst1 to double`
//	xs = [1.5]; print(xs[0])          -> printed 1 (a truncated float, silent)
//	{1.5} == {1.6}                    -> printed 1 (CPython says 0 — true by luck)

func TestFloatAndKindAnswersMatchCPython(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"float_eq_int", "print(1 if 2.0 == 2 else 0)\n", "1\n"},
		{"float_ne_int", "print(1 if 1.5 == 2 else 0)\n", "0\n"},
		{"float_eq_str", "print(1 if 1.0 == \"a\" else 0)\n", "0\n"},
		{"str_eq_float", "print(1 if \"a\" == 1.0 else 0)\n", "0\n"},
		{"number_eq_container", "print(1 if 1.0 == [1] else 0)\n", "0\n"},
		{"container_eq_number", "print(1 if [1] == 1.0 else 0)\n", "0\n"},
		{"number_ne_container", "print(1 if 1.0 != [1] else 0)\n", "1\n"},
		{"container_eq_container", "print(1 if [1, 2] == [1, 2] else 0)\n", "1\n"},
		{"container_ne_container", "print(1 if [1, 2] == [1, 3] else 0)\n", "0\n"},
		{"string_container_eq", "print(1 if [\"a\"] == [\"a\"] else 0)\n", "1\n"},
		{"string_container_ne", "print(1 if [\"a\"] == [\"b\"] else 0)\n", "0\n"},
		{"dict_eq_dict", "print(1 if {\"a\": 1} == {\"a\": 1} else 0)\n", "1\n"},
		{"set_eq_set", "print(1 if {1, 2} == {2, 1} else 0)\n", "1\n"},
		{"list_len", "print(len([1.5, 2.5]))\n", "2\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "kinds.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestFloatContainersAnswerOnTheCompiledBackend is the exit-code half of the contract, flipped by ADR
// 0233: a float now has a representation in a container slot — the handle of a float box, tagged
// TagFloat, compared by rt_payload_eq and rendered by the mixed printer — so the shapes this test
// held as refusals are expected to compile and print CPython's answer. The two failure modes stay
// forbidden: exit 2 (the compiler rejecting its own module) and exit 0 with an answer nobody
// checked against the oracle. The oracle leg is now in the table rather than assumed.
func TestFloatContainersAnswerOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"literal_list_float_eq", "print(1 if [1] == [1.0] else 0)\n", "1\n"},
		{"literal_list_float_eq_rev", "print(1 if [1.0] == [1] else 0)\n", "1\n"},
		{"literal_list_float_self", "print(1 if [1.5, 2] == [1.5, 2] else 0)\n", "1\n"},
		{"print_literal_float_list", "print([1.5, 2])\n", "[1.5, 2]\n"},
		{"read_back_a_float_element", "xs = [1.5]\nprint(xs[0])\n", "1.5\n"},
		{"float_set_equality", "print(1 if {1.0} == {1.0} else 0)\n", "1\n"},
		// The answer that used to be green by truncation: {1.5} == {1.6} printed 1 because both
		// elements had been squeezed into the same word. It is 0 now, on all three engines.
		{"float_set_equality_by_luck", "print(1 if {1.5} == {1.6} else 0)\n", "0\n"},
		{"float_dict_equality_by_luck", "d = {\"a\": 1.5}\ne = {\"a\": 1.6}\nprint(1 if d == e else 0)\n", "0\n"},
		{"float_membership", "xs = [1, \"a\"]\nprint(1 if 1.0 in xs else 0)\n", "1\n"},
		{"float_key_read", "d = {1.5: \"x\", 2.5: \"y\"}\nprint(d[2.5])\nprint(1 if 1.5 in d else 0)\nprint(d)\n", "y\n1\n{1.5: 'x', 2.5: 'y'}\n"},
		{"none_element_prints_as_none", "xs = [None, 1.5, \"a\"]\nprint(xs)\n", "[None, 1.5, 'a']\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floats.gy", tc.src)
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

// TestCompiledAnswersWhatTheCompilerRefuses keeps the other half of the record: every shape the
// compiled leg still refuses has an answer on the human path, so the gap is a codegen hole (roadmap
// L11.1's nested containers) and not a semantic decision. These assertions are CPython's, asserted
// on the record alone — a two-engine table would hide the compiled hole behind the record's
// answer, which is the thing this file's rule forbids.
func TestCompiledAnswersWhatTheCompilerRefuses(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"nested_list_equality", "print(1 if [[1, 2]] == [[1, 2]] else 0)\n", "1\n"},
		{"nested_dict_value", "d = {\"a\": [1, 2]}\nprint(d)\n", "{'a': [1, 2]}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floats.gy", tc.src)
			out, code := cliRunCode(t, "--aot", path)
			if code != 0 {
				t.Fatalf("the compiled run exited %d:\n%s", code, cliRun(t, "--aot", path))
			}
			if out != tc.want {
				t.Fatalf("the compiled run printed %q, want CPython's %q", out, tc.want)
			}
			if _, aotCode := cliRunCode(t, "--aot", path); aotCode == 2 {
				t.Fatalf("the compiled leg rejected its own module for %q (ADR 0166):\n%s", tc.src, cliRun(t, "--aot", path))
			}
		})
	}
}

// TestNestedContainersAnswerOnTheCompiledBackend is roadmap L11.1 step 2, the half the float commit left
// open: an element that is itself a container. The slot holds the inner object's handle and the
// slot's tag is what routes the print to rt_print_container_value and the comparison to
// rt_container_eq, both at run time — which is the only sound place to make that choice, because
// the outer container has no single element kind to ask.
//
// The two forbidden outcomes stay forbidden here. Exit 2 is the compiler blamed for an ordinary
// program (ADR 0166). Exit 0 with an answer nobody checked is the subtler one, and this family
// produced it twice: [[1, "a"]] printed [['b', 'a']] (the inner integers through the interned
// string table, because the inner list had been built claiming one element kind), and
// `for row in [[1, 2], [3, 4]]` printed 0 and 1 (the handles, because the unrolled body printed
// the element without its tag). Both are pinned as answers now.
func TestNestedContainersAnswerOnTheCompiledBackend(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"list_of_lists", "print([[1, 2], [3, 4]])\n", "[[1, 2], [3, 4]]\n"},
		{"list_of_lists_and_a_number", "print([[1, 2], 3])\n", "[[1, 2], 3]\n"},
		{"list_of_lists_of_text", "print([[\"a\"], [\"b\"]])\n", "[['a'], ['b']]\n"},
		{"inner_mixed_list", "print([[1, \"a\"], [2, \"b\"]])\n", "[[1, 'a'], [2, 'b']]\n"},
		{"inner_float_and_none", "print([[1.5, \"a\"], [None, 2]])\n", "[[1.5, 'a'], [None, 2]]\n"},
		{"three_deep", "print([[[1]], [2]])\n", "[[[1]], [2]]\n"},
		{"dict_of_lists", "print({\"a\": [1, 2], \"b\": [3]})\n", "{'a': [1, 2], 'b': [3]}\n"},
		{"dict_of_dict", "print({\"a\": {\"b\": 1}})\n", "{'a': {'b': 1}}\n"},
		{"int_keyed_dict_of_set", "print({1: {2, 3}})\n", "{1: {2, 3}}\n"},
		{"nested_equality_by_content", "print(1 if [[1, 2]] == [[1, 2]] else 0)\n", "1\n"},
		{"nested_inequality_by_content", "print(1 if [[1, 2]] == [[1, 3]] else 0)\n", "0\n"},
		{"nested_membership", "print(1 if [1, 2] in [[1, 2], 3] else 0)\n", "1\n"},
		{"nested_membership_absent", "print(1 if [1, 4] in [[1, 2], 3] else 0)\n", "0\n"},
		{"cross_numeric_inner_equality", "print(1 if [1, 2] == [1.0, 2] else 0)\n", "1\n"},
		{"literal_index_of_a_literal", "print([[1, 2], [3, 4]][0])\n", "[1, 2]\n"},
		{"append_a_container_variable", "xs = [1]\nys = [2]\nxs.append(ys)\nprint(xs)\n", "[1, [2]]\n"},
		{"append_a_container_literal", "xs = []\nxs.append([7, 8])\nprint(xs)\n", "[[7, 8]]\n"},
		{"assign_a_container_element", "xs = [[1, 2]]\nxs[0] = [9]\nprint(xs)\n", "[[9]]\n"},
		{"store_a_container_in_a_dict", "d = {\"a\": 1}\nd[\"b\"] = [2, 3]\nprint(d)\n", "{'a': 1, 'b': [2, 3]}\n"},
		{"length_of_a_nested_list", "print(len([[1, 2], [3, 4]]))\n", "2\n"},
		{"loop_over_inner_containers", "for row in [[1, 2], [3, 4]]:\n    print(row)\n", "[1, 2]\n[3, 4]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "nested.gy", tc.src)
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

// TestNestedShapesThatStillRefuse is the other half of the record. A dict keyed by a container has
// no hashing rule, and an element reached for as a plain number is the tagged value word L11.1 still
// owes; both are refused by name, and none of them is allowed to reach llc — the shapes below used
// to be exit-2 modules (a container global in an i32 slot) before they were gates.
//
// `len(xs[0])` and `m[0][1]` are not in this list any more: they were the last two rows of it, and
// ADR 0241 moved them to the answering table in container_slot_read_test.go.
func TestNestedShapesThatStillRefuse(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"container_key", "print({[1, 2]: 3})\n", "cannot hold"},
		{"container_key_in_a_dict_variable", "d = {[1]: 1}\nprint(d)\n", "constant integer keys only"},
		{"arithmetic_on_a_container_element", "xs = [[1, 2], [3]]\nprint(xs[0] + 1)\n", "needs a single static kind"},
		// (`element_of_a_mutated_container_as_a_number` — `xs = [[1, 2]]` / `xs.append([3])` /
		// `print(xs[0][0] + 1)` — sat in this table demanding a refusal until ADR 0265: the pair (payload,
		// tag) now travels to the target, which does the sum and answers a pair back, so the answer brings
		// its own kind. It is a parity row in numeric_slot_arith_test.go, with the gate that proves the
		// slots hold numbers.)
		// (`xs = [[1, 2]]; xs.append([9]); len(xs[0])` used to sit here demanding a refusal. The object
		// knows its own slots — every writer tags them — so it answers 2 on the compiled path and is pinned
		// against CPython in TestContainerSlotReadsMatchCPython (ADR 0246).
		{"nested_element_as_a_number", "xs = [[1, 2], [3]]\nprint(xs[0] + 1)\n", "needs a single static kind"},
		{"sum_of_containers", "print(sum([[1], [2]]))\n", "sum adds numbers"},
		{"any_of_containers", "print(any([[1], [2]]))\n", "no word for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "nested.gy", tc.src)
			// The diagnostic goes to stderr, so the message is read with the combined capture
			// and the exit code with the coded one.
			_, code := cliRunCode(t, "--aot", path)
			msg := cliRun(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected its own module (ADR 0166):\n%s", msg)
			}
			if code == 0 {
				t.Fatalf("%q answered %q; this shape is owed, and a wrong answer is worse than a refusal", tc.src, msg)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("%q refused with %q, want it to mention %q", tc.src, msg, tc.want)
			}
			// The compiled leg refuses; that is a codegen hole, not a semantic decision of the
			// language, so the human path must still be standing: the record either answers
			// or fails cleanly, never with exit 2.
			if iout, icode := cliRunCode(t, "--aot", path); icode == 2 {
				t.Fatalf("the interpreter exited 2 on %q:\n%s", tc.src, iout)
			}
		})
	}
}
