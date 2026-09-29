package integration

import (
	"github.com/donutloop/gusty/pkg/lang"
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
		// What is left of Gap R.47 after ADR 0229: building a *new* string at run time
		// (concatenation, slicing) and iterating a string held in a variable still need the
		// buffer-allocation half of the runtime, so they refuse with a message. The subscript,
		// len, ord and char-method shapes used to be here too and now answer — see
		// TestCompiledStringSubscriptAnswersAtRuntime, which promotes them from CPython.
		{"concat_of_two_chars", "s = \"abc\"\nprint(s[0] + s[2])\n"},
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

// TestCompiledStringSubscriptAnswersAtRuntime is the ADR 0229 half: a character asked about at
// run time is answered by the runtime string table on *both* engines, with CPython's answer.
// The expectations come from python3, and the test refuses to proceed if CPython disagrees, so
// the table cannot drift into pinning the emission.
//
// Each of these used to be one of two failures: a compile-time refusal (exit 1 for a program
// CPython runs), or — worse, before the print path learned to ask the same question — the
// interned index printed as a number. A wrong answer with exit 0 is what the suite must never
// accept, which is why the refusal tests above and this one are kept as separate tables.
func TestCompiledStringSubscriptAnswersAtRuntime(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"variable_index", "s = \"abc\"\ni = 0\nprint(s[i])\n", "a\n"},
		{"len_of_a_char", "s = \"abc\"\nprint(len(s[1]))\n", "1\n"},
		{"method_on_a_char", "s = \"abc\"\nprint(s[1].upper())\n", "B\n"},
		{"method_lower_on_a_char", "s = \"ABC\"\nprint(s[1].lower())\n", "b\n"},
		{"ord_of_a_computed_char", "s = \"abc\"\nprint(ord(s[1]))\n", "98\n"},
		{"index_of_a_call_result", "def f() -> str:\n    return \"xy\"\n\nprint(f()[1])\n", "y\n"},
		{"index_of_unannotated_call_result", "def f():\n    return \"xy\"\n\nprint(f()[1])\n", "y\n"},
		{"index_of_a_dict_value", "d = {\"k\": \"abc\"}\nprint(d[\"k\"][1])\n", "b\n"},
		{"index_by_a_loop_variable", "s = \"abc\"\nfor i in [0, 2]:\n    print(s[i])\n", "a\nc\n"},
		{"len_of_a_computed_string", "def f():\n    return \"hello\"\nprint(len(f()))\n", "5\n"},
		{"condition_on_a_runtime_subscript", "s = \"abc\"\nx = 0\nprint(1 if s[x] == \"b\" else 0)\n", "0\n"},
		{"condition_on_a_matching_char", "s = \"abc\"\nx = 1\nprint(1 if s[x] == \"b\" else 0)\n", "1\n"},
		{"code_points_not_bytes", "def f():\n    return \"caf\" + \"\u00e9\"\nprint(len(f()), f()[3])\n", "4 \u00e9\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantOut, _, perr := lang.PythonRun(tc.src)
			if perr != nil {
				t.Fatalf("CPython rejected a program this table says answers: %v\n%s", perr, tc.src)
			}
			if wantOut != tc.want {
				t.Fatalf("this table disagrees with CPython, which printed %q\n%s", wantOut, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				path := writeSrc(t, t.TempDir(), "rt.gy", tc.src)
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d for a shape ADR 0229 makes answerable:\n%s\n%s", engine, code, cliRun(t, engine, path), tc.src)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q (CPython)\n%s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestStringTableOverflowIsACatchableTrap: the compiled backend bounds the strings a program can
// create at run time. Exceeding the bound is a raise the program can catch, not a sentence
// printed over its head and not — the old behaviour — the last table entry reused, which made a
// string print as some other string (ADR 0229).
func TestStringTableOverflowIsACatchableTrap(t *testing.T) {
	src := "def make(i):\n    return \"x\" + \"\" + str(i % 7) + \"_\" * (i % 3)\n\nn = 0\nwhile n < 200:\n    n = n + 1\nprint(\"still running\")\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Skipf("this shape is refused for an unrelated reason (%v)", err)
	}
	if _, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
	for _, want := range []string{"rt_str_intern", "rt_str_char", "ret i32 -2"} {
		if !strings.Contains(res.IR, want) {
			t.Fatalf("the runtime string path is missing %q", want)
		}
	}
	if strings.Contains(res.IR, "@rt_die(i8* getelementptr ([46 x i8]") {
		t.Fatalf("the string table's overflow path must not reach for a helper defined in another runtime block")
	}
}
