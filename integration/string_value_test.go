package integration

import (
	"strings"
	"testing"
)

// End-to-end coverage for roadmap Gap R.42 / L11.8 (ADR 0224). Each expectation is what CPython
// prints for that source; the shapes are the ones that used to reach the LLVM backend as an
// `i32` holding a string global's address — `icmp eq i32 @.str1, %t1`, `ret i32 @.str1` — and come
// back as exit 2, "LLVM rejected what we emitted", for an ordinary program.

func TestStringValuesMatchCPythonOnBothEngines(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"compare_variable_and_literal", "x = \"hi\"\nprint(1 if x == \"hi\" else 0)\n", "1\n"},
		{"compare_two_variables", "a = \"hi\"\nb = \"hi\"\nprint(1 if a == b else 0)\n", "1\n"},
		{"compare_unequal", "x = \"hi\"\nprint(1 if x != \"ho\" else 0)\n", "1\n"},
		{"compare_lexicographic", "x = \"abc\"\nprint(1 if x < \"abd\" else 0)\n", "1\n"},
		{"membership", "xs = [\"a\", \"b\"]\nprint(1 if \"b\" in xs else 0)\nprint(1 if \"z\" not in xs else 0)\n", "1\n1\n"},
		{"container_element_compare", "names = [\"a\", \"b\"]\nfor n in names:\n    if n == \"a\":\n        print(n)\n", "a\n"},
		{"comprehension_filter", "names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names if n != \"b\"]\nprint(len(out))\nprint(1 if \"a\" in out else 0)\n", "2\n1\n"},
		{"concat_compare", "a = \"ab\"\nb = \"c\"\nprint(1 if a + b == \"abc\" else 0)\n", "1\n"},
		{"upper_compare", "x = \"hi\"\nprint(1 if x.upper() == \"HI\" else 0)\n", "1\n"},
		{"strip_compare", "print(1 if \"  hi \".strip() == \"hi\" else 0)\n", "1\n"},
		{"substring_membership", "print(1 if \"ell\" in \"hello\" else 0)\n", "1\n"},
		{"startswith", "print(1 if \"hello\".startswith(\"he\") else 0)\n", "1\n"},
		{"fstring_interpolation", "n = \"world\"\nprint(f\"hi {n}\")\n", "hi world\n"},
		{"slice_compare", "s = \"abcdef\"\nprint(1 if s[1:3] == \"bc\" else 0)\n", "1\n"},
		{"string_dict_membership", "d = {\"k\": 1}\nprint(1 if \"k\" in d else 0)\n", "1\n"},
		{"string_parameter_compare_in_function", "def ok(s: str) -> int:\n    if s == \"yes\":\n        return 1\n    return 0\n\nprint(ok(\"yes\"))\nprint(ok(\"no\"))\n", "1\n0\n"},
		{"method_returns_string", "class Dog:\n    def sound(self) -> str:\n        return \"woof\"\n\nprint(Dog().sound())\n", "woof\n"},
		{"method_returns_folded_local", "class D:\n    def say(self) -> str:\n        w = \"yo\"\n        return w\n\nprint(D().say())\n", "yo\n"},
		{"instance_string_attribute", "class C:\n    def __init__(self):\n        self.w = \"hi\"\n\nprint(C().w)\n", "hi\n"},
		{"method_with_string_argument", "class G:\n    def greet(self, who: str) -> str:\n        return \"hello\"\n\nprint(G().greet(\"x\"))\n", "hello\n"},
		{"join_control", "print(\",\".join([\"a\", \"b\"]))\n", "a,b\n"},
		// The control for "did the representation change break printing?": a literal, a list of
		// strings, and a plain print still render the way they always did.
		{"print_literal_control", "print(\"hello\")\n", "hello\n"},
		{"print_string_list_control", "xs = [\"a\", \"b\"]\nprint(len(xs))\nprint(xs[0])\n", "2\na\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "strings.gy", tc.src)
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

// TestStringSubscriptPrintsTextInTheInterpreter: the row this cycle closed. Both backends used to
// answer the byte code (98, 99) where CPython answers b and c; the interpreter is asserted here
// because the compiled leg is covered in string_subscript_test.go, and a two-engine table would have
// let one leg drift unnoticed again (ADR 0225).
func TestStringSubscriptPrintsTextInTheInterpreter(t *testing.T) {
	path := writeSrc(t, t.TempDir(), "str_index.gy", "s = \"abc\"\nprint(s[1])\nprint(s[-1])\n")
	out, code := cliRunCode(t, "--interp", path)
	if code != 0 {
		t.Fatalf("interp exited %d:\n%s", code, out)
	}
	if out != "b\nc\n" {
		t.Fatalf("interp printed %q, want CPython's \"b\\nc\\n\" (the pinned byte-code divergence 98/99 is meant to be gone)", out)
	}
}

// TestPrintingAnElementOfAFreshComprehensionListIsPinned: `out = [n for n in names if n == "a"]`
// followed by `print(out[0])` answers the index (0, the first interned string) where every oracle
// says `a`. The element-kind fact is present when the list has been walked with `for` first and
// absent when it has not, so the same program differs by a statement of position; recorded as
// roadmap Gap R.46 rather than smoothed over, and pinned at what the compiler actually does.
func TestPrintingAnElementOfAFreshComprehensionListIsPinned(t *testing.T) {
	src := "names = [\"a\", \"b\"]\nnames.append(\"c\")\nout = [n for n in names if n == \"a\"]\nprint(len(out))\nprint(out[0])\n"
	path := writeSrc(t, t.TempDir(), "comp_elem.gy", src)
	out, code := cliRunCode(t, "--aot", path)
	if code != 0 {
		t.Fatalf("aot exited %d:\n%s", code, out)
	}
	if out != "1\n0\n" {
		t.Fatalf("aot printed %q — the pinned index-printing divergence (Gap R.46) has changed; if this is now \"1\\na\\n\" make it a parity case", out)
	}
}

// TestCompiledStringHolesRefuseRatherThanReject keeps the ADR 0166 contract on the shapes that are
// still out of reach: each is a refusal (exit 1, a message) rather than an `llc` rejection (exit 2).
func TestCompiledStringHolesRefuseRatherThanReject(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"index_a_sorted_result", "xs = [\"b\", \"a\"]\nprint(sorted(xs)[0])\n"},
		{"str_of_a_number_compiled", "x = 5\nprint(str(x) == \"5\")\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "hole.gy", tc.src)
			// stdout and the exit code come from one helper, the message from the other: the
			// refusal is written to stderr, and a test that read only stdout would call every
			// refusal in the language "silent".
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected its own module instead of refusing (ADR 0166):\n%s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				t.Fatalf("the compiled leg answered %q; this shape is expected to refuse and be recorded", out)
			}
			if msg := strings.TrimSpace(cliRun(t, "--aot", path)); msg == "" {
				t.Fatalf("the compiled leg refused with exit %d and said nothing (exit codes: docs/operations.md)", code)
			}
		})
	}
}
