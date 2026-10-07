package integration

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// container_slot_read_test.go — reading a container back out of a container slot (roadmap L11.1,
// ADR 0241).
//
// A slot is one i32. ADR 0239 put the inner object's *handle* in it and the container's tag beside
// it, which is what lets `print([[1, 2]])` print a container at all. What was still missing was the
// read that *uses* what comes back: `len(xs[0])`, `xs[0][1]`, `d["a"][1]`, `m[0][1]`, `t[0][0][0]`,
// `xs[0] == [1, 2]`, `2 in xs[0]` and `for v in xs[0]` were each refused by the compiled backend
// ("len requires an inline list/dict/set literal") while the record answered every one of them
// — the compiled backend disagreeing out loud about the same source.
//
// The read is granted by a compile-time promise rather than a runtime guess: the name must be bound
// exactly once to a container literal and never mutated, sorted, item-assigned or handed to a
// function this pass cannot see, because then the tag the compiler remembers is still the tag the
// object holds. Where the promise runs out the program is refused by name — a payload read back as a
// handle is a number wearing another object's bits, and that is the class of wrong answer this
// language has decided it does not ship.

func TestContainerSlotReadsMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"len_of_element", "xs = [[1, 2], [3, 4]]\nprint(len(xs[0]))\n", "2\n"},
		// A container the program built instead of spelled out: the answer comes from the object's own
		// tag array, which every writer filled under ADR 0187's payload-and-tag rule. The compiler has
		// no literal here at all, so this is L11.1's dynamic half arriving one read at a time.
		{"len_of_a_run_time_built_container", "xs = []\nxs.append([7, 8])\nxs.append([9])\nprint(len(xs[0]), len(xs[1]))\n", "2 1\n"},
		{"len_after_appending_to_a_literal_list", "xs = [[1, 2]]\nxs.append([9])\nprint(len(xs[0]))\n", "2\n"},
		{"len_of_a_text_slot", "xs = []\nxs.append(\"abc\")\nprint(len(xs[0]))\n", "3\n"},
		{"len_of_each_slot_of_a_container_that_grew", "xs = []\nxs.append([1, 2])\nxs.append(\"abc\")\nxs.append([9])\nprint(len(xs[0]), len(xs[1]), len(xs[2]))\n", "2 3 1\n"},
		{"len_of_a_run_time_built_dict_slot", "d = {}\nd[\"a\"] = [1, 2, 3]\nprint(len(d[\"a\"]))\n", "3\n"},
		{"len_of_a_dict_slot_built_at_run_time", "xs = []\nxs.append({\"k\": 1, \"j\": 2})\nprint(len(xs[0]))\n", "2\n"},
		{"len_of_dict_value", "d = {\"a\": [1, 2], \"b\": [3]}\nprint(len(d[\"a\"]))\n", "2\n"},
		{"len_of_set_element", "s = [{1, 2}]\nprint(len(s[0]))\n", "2\n"},
		{"reindex_element", "xs = [[1, 2], [3, 4]]\nprint(xs[0][1])\n", "2\n"},
		{"reindex_dict_value", "d = {\"a\": [1, 2]}\nprint(d[\"a\"][1])\n", "2\n"},
		{"reindex_int_keyed_dict", "m = {0: [1, 2], 1: 3}\nprint(m[0][1])\n", "2\n"},
		{"three_levels", "t = [[[1]]]\nprint(t[0][0][0])\n", "1\n"},
		{"dict_of_dict_of_list", "d = {\"a\": {\"b\": [7, 8]}}\nprint(d[\"a\"][\"b\"][1])\n", "8\n"},
		{"negative_index", "xs = [[1, 2, 3]]\nprint(xs[0][-1])\n", "3\n"},
		{"element_equality", "xs = [[1, 2], [3, 4]]\nprint(1 if xs[0] == [1, 2] else 0)\n", "1\n"},
		{"element_inequality", "xs = [[1, 2], [3, 4]]\nprint(1 if xs[0] == [1, 3] else 0)\n", "0\n"},
		{"membership_in_element", "xs = [[1, 2], [3, 4]]\nprint(1 if 2 in xs[0] else 0)\n", "1\n"},
		{"membership_absent", "xs = [[1, 2], [3, 4]]\nprint(1 if 9 in xs[0] else 0)\n", "0\n"},
		{"text_element", "xs = [[\"a\", 1], [2]]\nprint(xs[0][0])\n", "a\n"},
		{"float_element", "xs = [[1.5, 2]]\nprint(xs[0][0])\n", "1.5\n"},
		{"none_element", "xs = [[None, 1], [2]]\nprint(xs[0][0])\n", "None\n"},
		{"print_dict_value", "d = {\"a\": [1, 2]}\nprint(d[\"a\"])\n", "[1, 2]\n"},
		{"iterate_element", "xs = [[1, 2]]\nfor v in xs[0]:\n    print(v)\n", "1\n2\n"},
		{"iterate_mixed_element", "xs = [[1, \"a\"], [3, 4]]\nfor v in xs[0]:\n    print(v)\n", "1\na\n"},
		{"iterate_dict_value", "d = {\"a\": [1, 2, 3]}\nfor v in d[\"a\"]:\n    print(v)\n", "1\n2\n3\n"},
		{"two_reads_one_line", "xs = [[1, 2], [3, 4]]\nprint(xs[0][0], xs[1][1])\n", "1 4\n"},
		{"bound_then_printed", "xs = [[1, 2], [3, 4]]\ny = xs[1][0]\nprint(y)\n", "3\n"},
		// The numeric half (ADR 0243): a slot whose literal is a number is that number for arithmetic,
		// so `xs[0] + 1` is an addition rather than a refusal.
		{"element_added", "xs = [[1, 2], [3, 4]]\nprint(xs[0][0] + 1)\n", "2\n"},
		{"element_multiplied", "xs = [[1.5, 2]]\nprint(xs[0][0] * 2)\n", "3.0\n"},
		{"mixed_element_added", "xs = [1, \"a\"]\nprint(xs[0] + 1)\n", "2\n"},
		{"mixed_element_compared", "xs = [1, \"a\"]\nprint(1 if xs[0] > 2 else 0)\n", "0\n"},
		{"mixed_float_element", "xs = [1.5, \"a\"]\nprint(xs[0] + 1)\n", "2.5\n"},
		{"two_elements_added", "xs = [1.5, 2]\nprint(xs[0] + xs[1])\n", "3.5\n"},
		{"element_divided", "xs = [10, \"a\"]\nprint(xs[0] / 4)\n", "2.5\n"},
		{"element_floordiv_mod", "xs = [10, \"a\"]\nprint(xs[0] // 3, xs[0] % 3)\n", "3 1\n"},
		{"element_negated", "xs = [1.5, \"a\"]\nprint(-xs[0])\n", "-1.5\n"},
		{"element_via_function", "def f(v):\n    return v * 2\n\nxs = [3, \"a\"]\nprint(f(xs[0]))\n", "6\n"},
		{"element_in_condition", "xs = [7, \"a\"]\nif xs[0] > 3:\n    print(\"big\")\nelse:\n    print(\"small\")\n", "big\n"},
		{"element_in_loop_body", "xs = [2, \"a\"]\nfor i in [1, 2]:\n    print(xs[0] * i)\n", "2\n4\n"},
		{"element_negated_literal", "xs = [-3, \"a\"]\nprint(xs[0] + 1)\n", "-2\n"},
		{"element_subtracted", "xs = [1, \"a\"]\nprint(xs[0] - 1)\n", "0\n"},
		{"element_equals_literal", "xs = [1, \"a\"]\nprint(1 if xs[0] == 1 else 0)\n", "1\n"},
		{"bool_element_in_test", "xs = [True, \"a\"]\nprint(1 if xs[0] else 0)\n", "1\n"},
		{"dict_value_element_arithmetic", "d = {\"a\": [1.5, 2]}\nprint(d[\"a\"][0] * 2)\n", "3.0\n"},
		{"element_accumulates", "xs = [4, \"a\"]\ntotal = 0\nfor i in [0]:\n    total = total + xs[0]\nprint(total)\n", "4\n"},
		{"float_element_two_slots", "t = [[1.5, \"x\"], 2]\nprint(t[0][0] + 1)\n", "2.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "read.gy", tc.src)
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

// TestContainerSlotReadTrapsMatchCPython pins the failures. An element is a container only when its
// tag says so, and a slot index is a position the program can get wrong. CPython raises; the
// interpreter raises too; the compiled backend either raises the same way or refuses to build a
// module that would answer with the wrong thing. What none of them may do is exit 0 with a value.
func TestContainerSlotReadTrapsMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"subscript_a_number_element",
			"xs = [[1, 2]]\nprint(xs[0][0][0])\n",
			"'int' object is not subscriptable",
		},
		{
			"index_out_of_range",
			"xs = [[1, 2]]\nprint(xs[0][9])\n",
			"list index out of range",
		},
		{
			"length_of_a_number_element",
			"xs = [[1, 2]]\nprint(len(xs[0][0]))\n",
			"object of type 'int' has no len()",
		},
		{
			// A slot holding text has no number to read. CPython raises; the compiled backend refuses to
			// build a module that would have to guess the kind (ADR 0243).
			"text_element_used_as_a_number",
			"xs = [1, \"a\"]\nprint(xs[1] + 1)\n",
			"can only concatenate str",
		},
		{
			"container_element_used_as_a_number",
			"xs = [[1], 2]\nprint(xs[0] + 1)\n",
			"can only concatenate list (not \"int\") to list",
		},
		// The same four questions asked of a container the program built at run time. Each is answered by
		// the slot's own tag rather than by a compile-time guess: the length is asked of the object when
		// the tag names one, and the TypeError CPython raises is raised, per kind, when it does not
		// (roadmap L11.1, ADR 0241; the answers are in TestContainerSlotReadsMatchCPython).
		{
			"length_of_a_run_time_built_int_slot",
			"xs = []\nxs.append([7, 8])\nxs.append(5)\nprint(len(xs[1]))\n",
			"object of type 'int' has no len()",
		},
		{
			"length_of_a_run_time_built_float_slot",
			"xs = []\nxs.append([7, 8])\nxs.append(1.5)\nprint(len(xs[1]))\n",
			"object of type 'float' has no len()",
		},
		{
			"length_of_a_run_time_built_none_slot",
			"xs = []\nxs.append([7, 8])\nxs.append(None)\nprint(len(xs[1]))\n",
			"object of type 'NoneType' has no len()",
		},
		{
			"length_of_a_run_time_built_dict_slot",
			"d = {}\nd[\"a\"] = 5\nprint(len(d[\"a\"]))\n",
			"object of type 'int' has no len()",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "read_trap.gy", tc.src)
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

// oracleTrap runs the pinned oracle and reports what it really did, exit status included:
// cpythonOut is for programs that succeed, and the rows above are about ones that do not.
func oracleTrap(t *testing.T, path string) (string, int) {
	t.Helper()
	py := os.Getenv("GUSTY_PYTHON")
	if py == "" {
		py = "python3"
	}
	if _, err := exec.LookPath(py); err != nil {
		t.Skipf("no %s to act as the oracle (set GUSTY_PYTHON)", py)
	}
	out, err := exec.Command(py, path).CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("%s %s: %v", py, path, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}
