package integration

import (
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
			for _, engine := range []string{"--interp", "--aot"} {
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

// TestFloatContainersAnswerOnBothBackends is the exit-code half of the contract, flipped by ADR
// 0233: a float now has a representation in a container slot — the handle of a float box, tagged
// TagFloat, compared by rt_payload_eq and rendered by the mixed printer — so the shapes this test
// held as refusals are expected to compile and print CPython's answer. The two failure modes stay
// forbidden: exit 2 (the compiler rejecting its own module) and exit 0 with an answer nobody
// checked against the oracle. The oracle leg is now in the table rather than assumed.
func TestFloatContainersAnswerOnBothBackends(t *testing.T) {
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
			for _, engine := range []string{"--interp", "--aot"} {
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

// TestInterpreterAnswersWhatTheCompilerRefuses keeps the other half of the record: every shape the
// compiled leg still refuses has an answer on the human path, so the gap is a codegen hole (roadmap
// L11.1's nested containers) and not a semantic decision. These assertions are CPython's, asserted
// on the interpreter alone — a two-engine table would hide the compiled hole behind the interpreter's
// answer, which is the thing this file's rule forbids.
func TestInterpreterAnswersWhatTheCompilerRefuses(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"nested_list_equality", "print(1 if [[1, 2]] == [[1, 2]] else 0)\n", "1\n"},
		{"nested_dict_value", "d = {\"a\": [1, 2]}\nprint(d)\n", "{'a': [1, 2]}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floats.gy", tc.src)
			out, code := cliRunCode(t, "--interp", path)
			if code != 0 {
				t.Fatalf("interp exited %d:\n%s", code, cliRun(t, "--interp", path))
			}
			if out != tc.want {
				t.Fatalf("interp printed %q, want CPython's %q", out, tc.want)
			}
			if _, aotCode := cliRunCode(t, "--aot", path); aotCode == 2 {
				t.Fatalf("the compiled leg rejected its own module for %q (ADR 0166):\n%s", tc.src, cliRun(t, "--aot", path))
			}
		})
	}
}
