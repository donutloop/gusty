package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

var _ = lang.Compile

// Assigned comprehensions must behave like assigned literals on the AOT backend.
//
// A comprehension whose elements are all constants is constant-folded into a
// compile-time list global. Storing that global into the variable's i32 slot is
// IR LLVM rejects outright, and reading it back printed the address instead of
// the list. The AOT path now materialises the folded elements into a runtime
// heap list, exactly like `ys = [..literal..]` does. Every case is checked on
// both backends.

func TestAssignedComprehensionParity(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"assigned list comprehension prints as a list",
			"ys = [x * 2 for x in [1, 2, 3]]\nprint(ys)\n",
			"[2, 4, 6]\n",
		},
		{
			"len of an assigned comprehension",
			"ys = [i * i for i in range(5)]\nprint(len(ys))\n",
			"5\n",
		},
		{
			"indexing an assigned comprehension",
			"ys = [i * i for i in range(4)]\nprint(ys[2])\n",
			"4\n",
		},
		{
			"iterating an assigned comprehension",
			"ys = [x + 1 for x in [1, 2, 3]]\nt = 0\nfor y in ys:\n    t = t + y\nprint(t)\n",
			"9\n",
		},
		{
			"assigned comprehension passed to a function",
			`def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

ys = [x * 2 for x in [1, 2, 3]]
print(total(ys))
`,
			"12\n",
		},
		{
			"rebinding a comprehension variable",
			"ys = [x * 2 for x in [1, 2]]\nys = [i for i in range(3)]\nprint(ys)\n",
			"[0, 1, 2]\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := runInterp(t, tc.src)
			if got != tc.want {
				t.Errorf("interpreter = %q, want %q", got, tc.want)
			}
			aot, err := runAOTConformance(t, tc.src)
			if err != nil {
				t.Fatalf("AOT: %v", err)
			}
			if aot != tc.want {
				t.Errorf("AOT = %q, want %q", aot, tc.want)
			}
		})
	}
}

// TestAssignedComprehensionModulesVerify: the emitted module for every shape
// above must pass LLVM's own module verifier (L8.2), which is how this bug was
// found in the first place.
func TestAssignedComprehensionModulesVerify(t *testing.T) {
	srcs := []string{
		"ys = [x * 2 for x in [1, 2]]\nprint(ys)\n",
		"ys = [i * i for i in range(5)]\nprint(len(ys))\nprint(ys[1])\n",
		"ys = [x for x in [3, 1, 2]]\nfor y in ys:\n    print(y)\n",
		"def f(xs) -> int:\n    return len(xs)\n\nys = [i for i in range(3)]\nprint(f(ys))\n",
	}
	for _, src := range srcs {
		res, err := lang.Compile(src)
		if err != nil {
			t.Errorf("Compile(%q): %v", src, err)
			continue
		}
		if _, err := runAOTConformance(t, src); err != nil {
			t.Errorf("module for %q failed to lower/link: %v", src, err)
		}
		if strings.Contains(res.IR, "store i32 @.lst") {
			t.Errorf("folded list stored as a scalar in %q", src)
		}
	}
}

// TestModuleContainersSharedWithFunctions: a module-level container shared with
// functions — including one whose parameter has the *same name* — must keep its
// own slot in main, be GC-rooted, and be visible to print/len/index/iteration.
func TestModuleContainersSharedWithFunctions(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"parameter named like the module variable",
			`def total(xs) -> int:
    t = 0
    for x in xs:
        t = t + x
    return t

xs = [1, 2, 3]
print(total(xs))
`,
			"6\n",
		},
		{
			"empty module list built up then passed",
			`def total(qs) -> int:
    t = 0
    for q in qs:
        t = t + q
    return t

ys = []
for i in range(3):
    ys.append(i * 2)
print(total(ys))
`,
			"6\n",
		},
		{
			"comprehension variable named like a parameter",
			`def f(xs) -> int:
    return len(xs)

xs = [x * 2 for x in [1, 2, 3]]
print(f(xs))
print(len(xs))
print(xs[2])
`,
			"3\n3\n6\n",
		},
		{
			"iterated module comprehension",
			"ys = [x + 1 for x in [1, 2]]\nfor y in ys:\n    print(y)\n",
			"2\n3\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := runInterp(t, tc.src); got != tc.want {
				t.Errorf("interpreter = %q, want %q", got, tc.want)
			}
			got, err := runAOTConformance(t, tc.src)
			if err != nil {
				t.Fatalf("AOT: %v", err)
			}
			if got != tc.want {
				t.Errorf("AOT = %q, want %q", got, tc.want)
			}
		})
	}
}
