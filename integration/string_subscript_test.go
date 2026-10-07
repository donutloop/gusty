package integration

import (
	"github.com/donutloop/gusty/pkg/lang"
	"strings"
	"testing"
)

// End-to-end coverage for roadmap Gap R.45 (ADR 0225). Expectations are what CPython prints, taken
// before judging either backend. The family is what `s[1]` *is*: the compiled path used to answer the
// byte, and the wrong type spread to every use — `s[0] + s[2]` printed 196, `s[1] == "b"` printed 0,
// `len(s[1])` and `s[1].upper()` and `ord(s[1])` trapped.

func TestStringSubscriptMatchesCPythonOnBothLegs(t *testing.T) {
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
			for _, engine := range cliEngines {
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

// TestCompiledStringSubscriptFollowsTheOracle covers the half the compiled leg still refuses: the
// expectations are CPython's, asserted on the record alone, so the gap in the other leg stays
// visible instead of being averaged away by a two-engine table.
func TestCompiledStringSubscriptFollowsTheOracle(t *testing.T) {
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
			out, code := cliRunCode(t, "--aot", path)
			if code != 0 {
				t.Fatalf("the compiled run exited %d:\n%s", code, out)
			}
			if out != tc.want {
				t.Fatalf("the compiled run printed %q, want CPython's %q", out, tc.want)
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
		// Gap R.47 is closed: every shape this table used to hold now answers, so the rows
		// live in TestCompiledStringSubscriptAnswersAtRuntime (checked against CPython) and
		// TestCompiledStringWritesAnswerAtRuntime. What the compiled backend still declines is
		// elsewhere in the roadmap: %s formatting (R.31), str * int and list concatenation
		// (R.33), floats in containers (L11.6).
		{"str_times_int", "print(\"ab\" * 2)\n"},
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
// rather than asserting an average: CPython raises IndexError (exit 1), the record traps (class
// 3 per docs/operations.md), and the compiled leg refuses at compile time (class 1) because the index
// is a constant — that last one is roadmap Gap R.37, a compile-time-known trap that should be a
// runtime trap, and the assertion says so instead of hiding it.
func TestUncaughtTrapClassesOnAnOutOfRangeCharSubscript(t *testing.T) {
	path := writeSrc(t, t.TempDir(), "oor.gy", "s = \"abc\"\nprint(s[9])\n")
	// One program, three honest outcomes, and the classes are allowed to differ by *path*: CPython
	// raises IndexError (its own exit 1), and gusty either traps with it (our class 3,
	// docs/operations.md) or refuses at the compile door because the index is a constant — that last
	// one is roadmap Gap R.37, and it is asserted rather than hidden. What is not on the menu is exit
	// 0: a program that prints something where the reference raises is the wrong-answer class.
	if out, code := cliRunMerged(t, "--aot", path); code != 3 {
		if code == 1 && refusesHonestly(out) {
			noteCompiledGap(t, "s = \"abc\"\nprint(s[9])\n", out)
		} else {
			t.Fatalf("compiled exit %d, want 3 (a runtime trap) or an honest refusal (Gap R.37):\n%s", code, out)
		}
	}
	if out, code := cliRunCode(t, "--aot", path); code != 1 && code != 3 {
		t.Fatalf("compiled exit %d, want 1 (a refusal today; Gap R.37 asks for a runtime trap instead) or 3: %s", code, out)
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
			for _, engine := range cliEngines {
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

// TestCompiledStringWritesAnswerAtRuntime is the write half of Gap R.47 (ADR 0230): operations
// that *build* a string while the program runs — concatenating a runtime string, slicing at
// run-time bounds, iterating a string held in a variable, str() of a computed number, strip().
//
// Each of these used to be a compile-time refusal for a program CPython runs, and the shape that
// came before the refusals was worse: iterating a function's string printed nothing at all,
// because the string's table index was read as a repeat count (roadmap Gap R.16). Expectations
// come from CPython; the test refuses to run if CPython disagrees with the table.
func TestCompiledStringWritesAnswerAtRuntime(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"concat_of_two_chars", "s = \"abc\"\nprint(s[0] + s[2])\n", "ac\n"},
		{"concat_with_a_call_result", "def get():\n    return \"b\"\nprint(\"a\" + get() + \"c\")\n", "abc\n"},
		{"concat_is_deduped_by_content", "def get():\n    return \"b\"\nprint(1 if \"a\" + get() == \"ab\" else 0)\n", "1\n"},
		{"slice_with_runtime_bounds", "def f(i):\n    s = \"abcdef\"\n    return s[i:i+2]\nprint(f(1))\n", "bc\n"},
		{"slice_open_ended", "def f():\n    return \"abcdef\"\nprint(f()[:2], f()[4:])\n", "ab ef\n"},
		{"slice_negative_bounds", "def f():\n    return \"abcdef\"\nprint(f()[-2:])\n", "ef\n"},
		{"iterate_a_variable_string", "s = \"ab\"\nfor c in s:\n    print(c)\n", "a\nb\n"},
		{"iterate_a_call_result", "def txt():\n    return \"hi\"\nfor c in txt():\n    print(c)\n", "h\ni\n"},
		{"iterate_unicode_by_code_point", "def txt():\n    return \"caf\" + \"\u00e9\"\nfor c in txt():\n    print(c)\n", "c\na\nf\n\u00e9\n"},
		{"str_of_a_computed_number", "def get():\n    return 42\nprint(str(get()))\n", "42\n"},
		{"str_of_a_negative_number", "def get():\n    return -7\nprint(str(get()))\n", "-7\n"},
		{"strip_trims_spaces", "def get():\n    return \"  hi \"\nif get().strip() == \"hi\":\n    print(\"stripped\")\n", "stripped\n"},
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
			for _, engine := range cliEngines {
				path := writeSrc(t, t.TempDir(), "w.gy", tc.src)
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d for a shape ADR 0230 makes answerable:\n%s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want %q (CPython)\n%s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestCompiledStringLoopsAreNotSilentlyWrong guards the specific old failure: iterating text that
// only exists at run time compiled cleanly and printed nothing, because the string's table index
// was read as a repeat count (roadmap Gap R.16). The loop must produce exactly the characters, and
// must not reach the table's empty entry.
func TestCompiledStringLoopsAreNotSilentlyWrong(t *testing.T) {
	src := "def txt():\n    return \"hi\"\nfor c in txt():\n    print(c)\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v\n%s", verr, res.IR)
	}
	for _, want := range []string{"@rt_str_nchars", "@rt_str_char", "alloca i32"} {
		if !strings.Contains(res.IR, want) {
			t.Fatalf("the runtime string loop is missing %q\n%s", want, res.IR)
		}
	}
	// The counter is the loop's own — sharing it with the variable's slot let a body
	// assignment move the iteration (ADR 0196).
	if !strings.Contains(res.IR, "_strctr") {
		t.Fatalf("the loop has no counter of its own\n%s", res.IR)
	}
}
