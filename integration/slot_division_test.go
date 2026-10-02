package integration

import (
	"strings"
	"testing"
)

// slot_division_test.go — the true division of a slot **no literal describes**, at the CLI and against
// CPython (roadmap L11.1's open clause Gap R.96; ADR 0253).
//
// The sibling unit file pins the same claim through Compile/EvalExpr; this one runs the shipped binary
// on both legs and the oracle on the same source. The measured defect:
//
//	xs = []
//	xs.append(3)
//	print(xs[0] / 4)   # CPython 0.75 · --interp 0.75 · --aot printed 0.0, exit 0
//
// `0.0` was ADR 0249's empty-operand `fdiv` substituted into silence — the one answer worse than a
// refusal, because the program looks like it worked. `/` earns its place as the arithmetic operator the
// object can be asked about because **true division is a float whatever arrives**: the module's one
// advance commitment, the result's kind, is settled, and the tag then says whether to unbox, to
// convert, or to raise CPython's sentence with the slot's real kind in it.
//
// The rows are split the way the claim is: what answers must answer identically on three engines; what
// traps must be *raised* by both legs with the same class and sentence and must never be a refusal; and
// what is still refused must be refused at the front end with exit 1 and the missing half named — never
// exit 2, the contract's class for a compiler that broke its own module (ADR 0166).

func TestTrueDivisionOfAnUnliteralisedSlotMatchesCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"appended_int_slot", "xs = []\nxs.append(3)\nprint(xs[0] / 4)\n", "0.75\n"},
		{"appended_float_slot", "xs = []\nxs.append(1.5)\nprint(xs[0] / 2)\n", "0.75\n"},
		{"appended_int_slot_evenly", "xs = []\nxs.append(6)\nprint(xs[0] / 4)\n", "1.5\n"},
		{"bool_slot", "xs = []\nxs.append(True)\nprint(xs[0] / 2)\n", "0.5\n"},
		{"negative_slot", "xs = []\nxs.append(-6)\nprint(xs[0] / 4)\n", "-1.5\n"},
		{"slot_is_the_divisor", "xs = []\nxs.append(4)\nprint(6 / xs[0])\n", "1.5\n"},
		{"divided_by_a_float_literal", "xs = []\nxs.append(6)\nprint(xs[0] / 4.0)\n", "1.5\n"},
		{"divided_by_a_settled_variable", "xs = []\nxs.append(6)\nd = 4.0\nprint(xs[0] / d)\n", "1.5\n"},
		{"settled_variable_over_slot", "xs = []\nxs.append(4)\nd = 6.0\nprint(d / xs[0])\n", "1.5\n"},
		{"bool_divisor", "xs = []\nxs.append(4)\nprint(xs[0] / True)\n", "4.0\n"},
		{
			"int_slot_and_float_slot_in_one_container",
			"xs = []\nxs.append(6)\nxs.append(3.0)\nprint(xs[0] / 4)\nprint(xs[1] / 4)\n", "1.5\n0.75\n",
		},
		{
			"loop_built_container",
			"xs = []\nfor i in [3, 6]:\n    xs.append(i * 2)\nprint(xs[0] / 4)\nprint(xs[1] / 4)\n", "1.5\n3.0\n",
		},
		{"computed_index", "xs = []\nxs.append(5)\ni = 0\nprint(xs[i] / 4)\n", "1.25\n"},
		{"dict_slot_by_key", "d = {}\nd[\"a\"] = 4\nprint(d[\"a\"] / 2)\n", "2.0\n"},
		{"slot_one_level_below", "xs = []\nxs.append([4])\nprint(xs[0][0] / 2)\n", "2.0\n"},
		{"three_levels_down", "xs = []\nxs.append([[4]])\nprint(xs[0][0][0] / 2)\n", "2.0\n"},
		{"dict_slot_of_a_built_container", "xs = []\nxs.append({\"k\": 5})\nprint(xs[0][\"k\"] / 2)\n", "2.5\n"},
		{"nested_dict_read_twice", "d = {}\nd[\"a\"] = {\"k\": 4}\nprint(d[\"a\"][\"k\"] / 2)\n", "2.0\n"},
		{"bound_then_printed_twice", "xs = []\nxs.append(6)\ny = xs[0] / 4\nprint(y)\nprint(y * 2)\n", "1.5\n3.0\n"},
		{"in_a_condition", "xs = []\nxs.append(6)\nif xs[0] / 4 > 1:\n    print(\"big\")\nelse:\n    print(\"small\")\n", "big\n"},
		{"in_a_while_head", "xs = []\nxs.append(6)\ni = 0\nwhile xs[0] / 4 > i:\n    print(i)\n    i = i + 1\n", "0\n1\n"},
		{"in_a_float_accumulation", "xs = []\nxs.append(6)\nf = 0.0\nf += xs[0] / 4\nprint(f)\n", "1.5\n"},
		{"in_an_f_string", "xs = []\nxs.append(6)\nprint(f\"got {xs[0] / 4}\")\n", "got 1.5\n"},
		{"under_a_builtin", "xs = []\nxs.append(-6)\nprint(abs(xs[0] / 4))\n", "1.5\n"},
		{"negated", "xs = []\nxs.append(4)\nprint(-(xs[0] / 2))\n", "-2.0\n"},
		{"stored_in_a_container", "xs = []\nxs.append(3)\nys = []\nys.append(xs[0] / 2)\nprint(ys[0])\n", "1.5\n"},
		{"int_slot_power", "xs = []\nxs.append(6)\nprint(xs[0] ** 2)\n", "36\n"},
		{"int_slot_floor_divide", "xs = []\nxs.append(7)\nprint(xs[0] // 2)\n", "3\n"},
		// What the older doors owned, pinned so this one cannot take it over.
		{"literal_container_still_divides", "xs = [10, 4]\nprint(xs[0] / 4)\n", "2.5\n"},
		{"loop_variable_of_a_built_container", "xs = []\nxs.append(6)\nfor v in xs:\n    print(v / 4)\n", "1.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_division.gy", tc.src)
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

// TestTrueDivisionOfAnUnliteralisedSlotTrapsLikeCPython is the half Gap R.96 measured as a printed
// number: the oracle dies on these programs, so both legs have to die too, with the same class and the
// same sentence — including which ZeroDivisionError wording the pair of operand *kinds* earns, which is
// why the guard is emitted inside the arm that knows. A row that prints a number, or that gets refused
// (`codegen:`) instead of trapped, fails: the compiler is answering a question about the program's data.
func TestTrueDivisionOfAnUnliteralisedSlotTrapsLikeCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, msg, oracle string }{
		{"text_slot", "xs = []\nxs.append(\"a\")\nprint(xs[0] / 2)\n", "TypeError: unsupported operand type(s) for /: 'str' and 'int'", ""},
		{"none_slot", "xs = []\nxs.append(None)\nprint(xs[0] / 2)\n", "TypeError: unsupported operand type(s) for /: 'NoneType' and 'int'", ""},
		{"list_slot", "xs = []\nxs.append([1, 2])\nprint(xs[0] / 2)\n", "TypeError: unsupported operand type(s) for /: 'list' and 'int'", ""},
		{"dict_slot", "xs = []\nxs.append({\"k\": 1})\nprint(xs[0] / 2)\n", "TypeError: unsupported operand type(s) for /: 'dict' and 'int'", ""},
		{"set_slot", "xs = []\nxs.append({1, 2})\nprint(xs[0] / 2)\n", "TypeError: unsupported operand type(s) for /: 'set' and 'int'", ""},
		{"text_slot_is_the_divisor", "xs = []\nxs.append(\"a\")\nprint(2 / xs[0])\n", "TypeError: unsupported operand type(s) for /: 'int' and 'str'", ""},
		{"text_slot_over_a_settled_float", "xs = []\nxs.append(\"a\")\nd = 2.0\nprint(xs[0] / d)\n", "TypeError: unsupported operand type(s) for /: 'str' and 'float'", ""},
		{"nested_text_slot", "xs = []\nxs.append([\"a\"])\nprint(xs[0][0] / 2)\n", "TypeError: unsupported operand type(s) for /: 'str' and 'int'", ""},
		{"int_slot_over_zero", "xs = []\nxs.append(0)\nprint(3 / xs[0])\n", "ZeroDivisionError: division by zero", ""},
		{"float_slot_over_zero", "xs = []\nxs.append(0.0)\nprint(3 / xs[0])\n", "ZeroDivisionError: float division by zero", ""},
		{"int_zero_literal_divisor", "xs = []\nxs.append(3)\nprint(xs[0] / 0)\n", "ZeroDivisionError: division by zero", ""},
		{"float_slot_int_zero_literal", "xs = []\nxs.append(3.5)\nprint(xs[0] / 0)\n", "ZeroDivisionError: float division by zero", ""},
		{"float_zero_literal_divisor", "xs = []\nxs.append(3)\nprint(xs[0] / 0.0)\n", "ZeroDivisionError: float division by zero", ""},
		{"settled_zero_float_divisor", "xs = []\nxs.append(3)\nd = 0.0\nprint(xs[0] / d)\n", "ZeroDivisionError: float division by zero", ""},
		{"bounds_check_still_fires", "xs = []\nxs.append([1, 2])\nprint(xs[0][9] / 1)\n", "IndexError: index out of range", "IndexError: list index out of range"},
		{"missing_key_still_raises", "d = {}\nd[\"a\"] = 4\nprint(d[\"z\"] / 2)\n", "KeyError", "KeyError"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_division_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			// The class is what both legs owe; the wording is CPython's except where the roadmap already
			// records a divergence of gusty's (Gap R.90 names the wrong container), which the `oracle`
			// field pins instead.
			want := tc.msg
			if tc.oracle != "" {
				want = tc.oracle
			}
			if pyCode == 0 || !strings.Contains(py, want) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if code == 2 {
					t.Errorf("%s exited 2 (compiler bug, ADR 0166) on a program the oracle dies on:\n%s", engine, out)
				}
				if code == 1 {
					t.Errorf("%s refused at compile time what the oracle raises at run time:\n%s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused at compile time what the oracle raises at run time:\n%s", engine, combined)
				}
				if !strings.Contains(combined, "Traceback (most recent call last):") || !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.msg, combined)
				}
			}
		})
	}
}

// TestTrueDivisionOfAnUnliteralisedSlotIsCatchable pins that the raise is a trap: `except` reaches it on
// both legs, which is the difference between exit 3 and a compile-time exit 1 and the reason the arm may
// not quietly become a refusal.
func TestTrueDivisionOfAnUnliteralisedSlotIsCatchable(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"typeerror_is_caught",
			"xs = []\nxs.append(\"a\")\ntry:\n    print(xs[0] / 2)\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"zerodivision_is_caught",
			"xs = []\nxs.append(0)\ntry:\n    print(1 / xs[0])\nexcept ZeroDivisionError:\n    print(\"no\")\n",
			"no\n",
		},
		{
			"the_next_slot_still_divides",
			"xs = []\nxs.append(\"a\")\nxs.append(6)\ntry:\n    print(xs[0] / 2)\nexcept TypeError:\n    print(xs[1] / 4)\n",
			"1.5\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_division_catch.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

// TestTrueDivisionOfAnUnliteralisedSlotRefusesHonestly keeps the shapes the door still declines out of
// `llc`: each one is refused at the front end, with exit 1 and the missing half named, and none of them
// reaches the module with an operand the compiler invented (the failure Gap R.96 measured).
func TestTrueDivisionOfAnUnliteralisedSlotRefusesHonestly(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"two_built_slots",
			"xs = []\nxs.append(6)\nys = []\nys.append(4)\nprint(xs[0] / ys[0])\n",
			"has no number the compiled backend can lift",
		},
		{
			"double_to_a_call_argument",
			"def f(a, b):\n    return b\n\nxs = []\nxs.append(6)\nprint(f(1, xs[0] / 2))\n",
			"this context stores an i32 word",
		},
		{
			"double_to_str",
			"xs = []\nxs.append(6)\nprint(str(xs[0] / 2))\n",
			"this context stores an i32 word",
		},
		{
			"double_to_a_dict_slot",
			"xs = []\nxs.append(6)\nd = {}\nd[\"k\"] = xs[0] / 2\nprint(d[\"k\"])\n",
			"this context stores an i32 word",
		},
		{
			"double_to_an_int_variable",
			"xs = []\nxs.append(6)\nn = 0\nn += xs[0] / 2\nprint(n)\n",
			"writes a double into the i32 slot",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "slot_division_refuse.gy", tc.src)
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected the compiler's own module (ADR 0166):\n%s", cliRun(t, "--aot", path))
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a front-end refusal): %s", code, out)
			}
			// The refusal is written to stderr, where a person and a script both read it: the assertions
			// below take the combined output, and the exit class above is what a script branches on.
			combined := cliRun(t, "--aot", path)
			if !strings.Contains(combined, tc.want) {
				t.Errorf("refusal does not name the missing half (%q):\n%s", tc.want, combined)
			}
			for _, bad := range []string{"LLVM ERROR", "verifier", "Instruction does not dominate"} {
				if strings.Contains(combined, bad) {
					t.Errorf("refusal arrived as an IR problem (%s):\n%s", bad, combined)
				}
			}
		})
	}
}
