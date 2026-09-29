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

// TestFloatContainersRefuseRatherThanRejectTheModule is the exit-code half of the contract: each of
// these is a refusal (class 1 per docs/operations.md) with something actionable in the message. The
// two failure modes the test forbids are the two this cycle fixed: exit 2 (the compiler rejecting its
// own module) and exit 0 with an answer nobody checked.
func TestFloatContainersRefuseRatherThanRejectTheModule(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"literal_list_float_eq", "print(1 if [1] == [1.0] else 0)\n"},
		{"literal_list_float_eq_rev", "print(1 if [1.0] == [1] else 0)\n"},
		{"literal_list_float_self", "print(1 if [1.5, 2] == [1.5, 2] else 0)\n"},
		{"print_literal_float_list", "print([1.5, 2])\n"},
		{"read_back_a_float_element", "xs = [1.5]\nprint(xs[0])\n"},
		{"float_set_equality", "print(1 if {1.0} == {1.0} else 0)\n"},
		// The one that used to be a green answer: {1.5} == {1.6} printed 1 because both elements
		// had been truncated to the same word. A refusal is not a regression from a wrong answer.
		{"float_set_equality_by_luck", "print(1 if {1.5} == {1.6} else 0)\n"},
		{"float_dict_equality_by_luck", "d = {\"a\": 1.5}\ne = {\"a\": 1.6}\nprint(1 if d == e else 0)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "floats.gy", tc.src)
			_, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected its own module (ADR 0166 / exit-code contract):\n%s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				out, _ := cliRunCode(t, "--aot", path)
				t.Fatalf("the compiled leg answered %q for a shape whose element it cannot represent; the interpreter is the honest comparison", out)
			}
			msg := cliRun(t, "--aot", path)
			if !strings.Contains(msg, "float") {
				t.Fatalf("the refusal does not name the element kind:\n%s", msg)
			}
			if !strings.Contains(msg, "container") {
				t.Fatalf("the refusal does not say where the value was being stored:\n%s", msg)
			}
		})
	}
}

// TestInterpreterAnswersWhatTheCompilerRefuses keeps the other half of the record: every shape the
// compiled leg refuses has an answer on the human path, so the gap is a codegen hole (roadmap L11.6)
// and not a semantic decision. These assertions are CPython's, asserted on the interpreter alone —
// a two-engine table would have hidden the compiled hole behind the interpreter's answer once again.
func TestInterpreterAnswersWhatTheCompilerRefuses(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"literal_list_float_eq", "print(1 if [1] == [1.0] else 0)\n", "1\n"},
		{"literal_list_float_self", "print(1 if [1.5, 2] == [1.5, 2] else 0)\n", "1\n"},
		{"print_literal_float_list", "print([1.5, 2])\n", "[1.5, 2]\n"},
		{"read_back_a_float_element", "xs = [1.5]\nprint(xs[0])\n", "1.5\n"},
		{"float_set_equality", "print(1 if {1.0} == {1.0} else 0)\n", "1\n"},
		{"float_set_inequality", "print(1 if {1.5} == {1.6} else 0)\n", "0\n"},
		{"float_dict_inequality", "d = {\"a\": 1.5}\ne = {\"a\": 1.6}\nprint(1 if d == e else 0)\n", "0\n"},
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
		})
	}
}
