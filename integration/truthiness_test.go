package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Truthiness parity: `if count:`, `while total:`, `if a and b:`, `if a < b or …`,
// `not x`, and the ternary all consume a *value* as a condition. A value in IR is
// either an i32 (integers, booleans as 0/1) or an i1 (a comparison result), and the
// backend must accept either — feeding one where the other is required is a verifier failure, not a
// source error. The other half of the risk is semantic: a condition holding a heap value
// (float, string, container) must ask the value, not the handle that represents it, which is how
// `if [1]:` ends up false and `if "":` ends up true.

type truthCase struct {
	name string
	src  string
	want string // the answer CPython gives, which the compiled program must print
}

var truthCases = []truthCase{
	{"if_int_true", "a = 1\nif a:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"if_int_zero", "a = 0\nif a:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"if_int_negative", "a = -3\nif a:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"while_int", "a = 3\nt = 0\nwhile a:\n    t = t + a\n    a = a - 1\nprint(t)\n", "6\n"},
	{"and_int", "a = 1\nb = 2\nif a and b:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"and_int_zero", "a = 0\nb = 2\nif a and b:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"or_int", "a = 0\nb = 2\nif a or b:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"or_int_both_zero", "a = 0\nb = 0\nif a or b:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"not_int", "a = 0\nif not a:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"cmp_and", "a = 1\nb = 2\nif a < b and b < 9:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"cmp_and_false", "a = 1\nb = 2\nif a > b and b < 9:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"cmp_or", "a = 1\nb = 2\nif a > b or b > 9:\n    print(8)\nelse:\n    print(7)\n", "7\n"},
	{"cmp_not", "a = 1\nb = 2\nif not (a > b):\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"mixed_and", "a = 3\nif a and a < 9:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"bool_lit_and", "if True and False:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"bool_lit_or", "if True or False:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"and_value", "a = 1\nb = 2\nprint(a and b)\n", "2\n"},
	{"and_value_falsy", "a = 0\nb = 2\nprint(a and b)\n", "0\n"},
	{"or_value", "a = 0\nb = 2\nprint(a or b)\n", "2\n"},
	{"or_value_keeps_first", "a = 5\nb = 0\nprint(a or b)\n", "5\n"},
	{"or_value_default", "a = \"\"\nprint(a or \"d\")\n", "d\n"},
	{"and_value_verdict", "print(True and False)\n", "False\n"},
	{"or_value_verdict", "print(False or True)\n", "True\n"},
	{"not_value", "a = 0\nprint(not a)\n", "True\n"},
	{"not_value_false", "a = 5\nprint(not a)\n", "False\n"},
	{"ternary_int", "a = 5\nprint(1 if a else 2)\n", "1\n"},
	{"ternary_zero", "a = 0\nprint(1 if a else 2)\n", "2\n"},
	{"ternary_cmp", "a = 5\nprint(1 if a > 9 else 2)\n", "2\n"},
	{"while_and", "a = 3\nt = 0\nwhile a and a > 0:\n    t = t + a\n    a = a - 1\nprint(t)\n", "6\n"},
	{"if_float_zero", "x = 0.0\nif x:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"if_float_nonzero", "x = 0.5\nif x:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"if_float_neg", "x = -0.0\nif x:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"elif_chain", "a = 0\nb = 5\nif a:\n    print(1)\nelif b:\n    print(2)\nelse:\n    print(3)\n", "2\n"},
	{"membership_and", "xs = [1, 2]\nif 2 in xs and 3 not in xs:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"nested_not", "a = 0\nb = 0\nif not a and not b:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"bool_from_cmp_stored", "a = 1\nb = 2\nflag = a < b\nif flag:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"bool_stored_false", "a = 1\nb = 2\nflag = a > b\nif flag:\n    print(7)\nelse:\n    print(8)\n", "8\n"},

	// Containers and strings test their content. In IR a container is a heap
	// handle and a string is an i8*, so testing the representation would make
	// `if [1]:` false and `if "":` either true or unverifiable.
	{"empty_list", "xs = []\nif xs:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"nonempty_list", "xs = [1, 2]\nif xs:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"empty_str", "s = \"\"\nif s:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"nonempty_str", "s = \"a\"\nif s:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"empty_dict", "d = {}\nif d:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	{"nonempty_dict", "d = {1: 2}\nif d:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"set_literal", "s = {1, 2}\nif s:\n    print(7)\nelse:\n    print(8)\n", "7\n"},
	{"heap_list_empty", "def build(n):\n    xs = []\n    for i in range(n):\n        xs.append(i)\n    return xs\n\nys = build(0)\nif ys:\n    print(7)\nelse:\n    print(8)\n", "8\n"},
	// `while xs:` is the interesting one: the loop must run while the list has
	// content and stop when it does not. (Rebinding to [] is how we empty it —
	// the interpreter has no list `pop` yet, which is its own gap.)
	{"while_over_list", "xs = [i for i in range(3)]\nys = []\nn = 0\nwhile xs:\n    n = n + 1\n    if n >= 3:\n        xs = []\n\nprint(n)\nif ys:\n    print(7)\nelse:\n    print(8)\n", "3\n8\n"},
}

func TestTruthinessMatchesPython(t *testing.T) {
	for _, tc := range truthCases {
		t.Run(tc.name, func(t *testing.T) {
			// The compiled run is checked against the recorded answer first, so a case where the two
			// have come apart is reported as that, rather than as a surprising CPython mismatch.
			compiled, err := runAOTConformance(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled backend rejected a valid program: %v\n%s", err, tc.src)
			}
			if compiled != tc.want {
				t.Errorf("compiled = %q, want %q (CPython's answer)\n%s", compiled, tc.want, tc.src)
			}
			lang.RecordedPrints(t, tc.src, trimWant(tc.want))
		})
	}
}

// trimWant turns a whole-output expectation into the one-line form the record holds.
func trimWant(s string) string { return strings.TrimSuffix(s, "\n") }

// TestTruthinessIRIsWellTyped is the verification half: the whole battery must
// produce modules LLVM itself accepts, which is what catches an i1/i32 mix.
func TestTruthinessIRIsWellTyped(t *testing.T) {
	for _, tc := range truthCases {
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Fatalf("Compile(%s): %v", tc.name, err)
		}
		if v, err := lang.VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
			t.Errorf("%s: LLVM rejected the module: %v %v", tc.name, v.Errors, err)
		}
	}
}
