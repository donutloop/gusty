package integration

import (
	"strings"
	"testing"
)

// End-to-end coverage for roadmap Gap R.45 (ADR 0225). Expectations are what CPython prints, taken
// before judging either backend. The family is what `s[1]` *is*: both backends used to answer the
// byte, and the wrong type spread to every use — `s[0] + s[2]` printed 196, `s[1] == "b"` printed 0,
// `len(s[1])` and `s[1].upper()` and `ord(s[1])` trapped.

func TestStringSubscriptMatchesCPythonOnBothEngines(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"positive", "s = \"abc\"\nprint(s[1])\n", "b\n"},
		{"negative", "s = \"abc\"\nprint(s[-1])\n", "c\n"},
		{"compares_with_text", "s = \"abc\"\nprint(1 if s[1] == \"b\" else 0)\n", "1\n"},
		{"does_not_compare_with_code", "s = \"abc\"\nprint(1 if s[1] == 98 else 0)\n", "0\n"},
		{"membership_of_text", "s = \"abc\"\nprint(1 if s[0] in \"xyzabc\" else 0)\n", "1\n"},
		{"stored_in_a_list", "s = \"abc\"\nxs = [s[1]]\nprint(xs[0])\n", "b\n"},
		{"slice_still_works", "s = \"abcdef\"\nprint(s[1:3])\n", "bc\n"},
		{"code_point_len", "s = \"café\"\nprint(len(s))\n", "4\n"},
		{"code_point_index", "s = \"café\"\nprint(s[3])\n", "é\n"},
		{"code_point_slice", "s = \"café\"\nprint(s[2:])\n", "fé\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "sub.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q", engine, out, tc.want)
				}
			}
		})
	}
}

// TestInterpreterStringSubscriptFollowsTheOracle covers the half the compiled leg still refuses: the
// expectations are CPython's, asserted on the interpreter alone, so the gap in the other leg stays
// visible instead of being averaged away by a two-engine table.
func TestInterpreterStringSubscriptFollowsTheOracle(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"concat_of_two_chars", "s = \"abc\"\nprint(s[0] + s[2])\n", "ac\n"},
		{"len_of_a_char", "s = \"abc\"\nprint(len(s[1]))\n", "1\n"},
		{"method_on_a_char", "s = \"abc\"\nprint(s[1].upper())\n", "B\n"},
		{"ord_roundtrip", "s = \"abc\"\nprint(ord(s[1]))\n", "98\n"},
		{"index_of_a_call_result", "def f() -> str:\n    return \"xy\"\n\nprint(f()[1])\n", "y\n"},
		{"index_of_a_dict_value", "d = {\"k\": \"abc\"}\nprint(d[\"k\"][1])\n", "b\n"},
		{"iterate_a_variable_string", "s = \"ab\"\nfor c in s:\n    print(c)\n", "a\nb\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "sub.gy", tc.src)
			out, code := cliRunCode(t, "--interp", path)
			if code != 0 {
				t.Fatalf("interp exited %d:\n%s", code, out)
			}
			if out != tc.want {
				t.Fatalf("interp printed %q, want CPython's %q", out, tc.want)
			}
		})
	}
}

// TestCompiledStringSubscriptHolesRefuseWithAMessage keeps the ADR 0166 contract on the shapes the
// compiled leg cannot reach yet: each is a refusal (exit 1, a message on stderr), never exit 2 (the
// compiler rejecting its own module) and never exit 0 with a wrong answer. The list is the content
// of roadmap Gap R.47 — a compiled string is a compile-time value, so anything that asks about a
// character at runtime has no runtime string to ask about.
func TestCompiledStringSubscriptHolesRefuseWithAMessage(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"variable_index", "s = \"abc\"\ni = 0\nprint(s[i])\n"},
		{"concat_of_two_chars", "s = \"abc\"\nprint(s[0] + s[2])\n"},
		{"len_of_a_char", "s = \"abc\"\nprint(len(s[1]))\n"},
		{"method_on_a_char", "s = \"abc\"\nprint(s[1].upper())\n"},
		{"ord_of_a_computed_char", "s = \"abc\"\nprint(ord(s[1]))\n"},
		{"index_of_a_call_result", "def f() -> str:\n    return \"xy\"\n\nprint(f()[1])\n"},
		{"index_of_a_dict_value", "d = {\"k\": \"abc\"}\nprint(d[\"k\"][1])\n"},
		{"iterate_a_variable_string", "s = \"ab\"\nfor c in s:\n    print(c)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "hole.gy", tc.src)
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected its own module instead of refusing (ADR 0166):\n%s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				t.Fatalf("the compiled leg answered %q for a shape recorded as unreachable (Gap R.47) — if it is right, promote this case", out)
			}
			if msg := strings.TrimSpace(cliRun(t, "--aot", path)); msg == "" {
				t.Fatalf("the compiled leg refused with exit %d and said nothing", code)
			}
		})
	}
}

// TestUncaughtTrapClassesOnAnOutOfRangeCharSubscript pins the three exit classes for one program
// rather than asserting an average: CPython raises IndexError (exit 1), the interpreter traps (class
// 3 per docs/operations.md), and the compiled leg refuses at compile time (class 1) because the index
// is a constant — that last one is roadmap Gap R.37, a compile-time-known trap that should be a
// runtime trap, and the assertion says so instead of hiding it.
func TestUncaughtTrapClassesOnAnOutOfRangeCharSubscript(t *testing.T) {
	path := writeSrc(t, t.TempDir(), "oor.gy", "s = \"abc\"\nprint(s[9])\n")
	if _, code := cliRunCode(t, "--interp", path); code != 3 {
		t.Fatalf("interpreter exit %d, want 3 (docs/operations.md: a runtime trap is class 3)", code)
	}
	if _, code := cliRunCode(t, "--aot", path); code != 1 {
		t.Fatalf("compiled exit %d, want 1 (a refusal today; Gap R.37 asks for a runtime trap instead)", code)
	}
}
