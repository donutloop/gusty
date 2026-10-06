package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

var _ = lang.Compile

// AOT heap containers across function boundaries (roadmap Gap I).
//
// A list/dict/set is an i32 handle into the runtime heap in the AOT backend.
// Two bugs lived here: passing a container *literal* to a function lowered the
// literal to its compile-time global (`@.lst1`), which LLVM's verifier rejects
// ("global variable reference must have pointer type"); and a container
// *parameter* was treated as a plain integer, so `for x in xs` silently
// compiled into a 0..handle range loop — wrong answers, no error.
//
// Every case below is checked on BOTH backends: the interpreter (which always
// had reference semantics) and the native AOT pipeline.

func TestAOTHeapContainerArguments(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			"list literal argument, iterated",
			`def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total([1, 2, 3]))
`,
			"6\n",
		},
		{
			"list literal argument, indexed",
			`def head(xs) -> int:
    return xs[0]

print(head([7, 8, 9]))
`,
			"7\n",
		},
		{
			"list literal argument, measured",
			`def count(xs) -> int:
    return len(xs)

print(count([1, 2, 3, 4]))
`,
			"4\n",
		},
		{
			"annotated list parameter, variable argument",
			`def total(xs: list[int]) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

vals: list[int] = [1, 2, 3]
print(total(vals))
`,
			"6\n",
		},
		{
			"Sequence parameter (read-only covariant view)",
			`def total(xs: Sequence[int]) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total([4, 5, 6]))
`,
			"15\n",
		},
		{
			"list variable argument",
			`def scale(xs, k) -> int:
    t = 0
    for x in xs:
        t = t + x * k
    return t

nums: list[int] = [1, 2, 3]
print(scale(nums, 3))
`,
			"18\n",
		},
		{
			"keyword argument receives a list",
			`def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total(xs=[2, 3, 4]))
`,
			"9\n",
		},
		{
			"default argument is a list",
			`def total(xs=[1, 2]) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total())
`,
			"3\n",
		},
		{
			"set literal argument",
			`def size(s: set[int]) -> int:
    return len(s)

print(size({5, 6, 7}))
`,
			"3\n",
		},
		{
			"comprehension argument",
			`def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total([x * 2 for x in [1, 2, 3]]))
`,
			"12\n",
		},
		{
			"generator call argument",
			`def upTo(n):
    i = 0
    while i < n:
        yield i
        i = i + 1

def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total(upTo(4)))
`,
			"6\n",
		},
		{
			"nested calls through a container",
			`def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

def double_total(xs) -> int:
    return total(xs) * 2

print(double_total([1, 2, 3]))
`,
			"12\n",
		},
		{
			"callee mutates the list it received",
			`def append_two(xs) -> int:
    xs.append(9)
    return len(xs)

nums: list[int] = [1, 2]
print(append_two(nums))
print(len(nums))
`,
			"3\n3\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			interp := runInterp(t, tt.src)
			if interp != tt.want {
				t.Errorf("interpreter output mismatch\n got: %q\nwant: %q", interp, tt.want)
			}
			aot := runAOT(t, tt.src)
			if aot != tt.want {
				t.Errorf("AOT output mismatch\n got: %q\nwant: %q", aot, tt.want)
			}
			if interp != aot {
				t.Errorf("parity mismatch: interpreter %q vs AOT %q", interp, aot)
			}
		})
	}
}

// TestAOTHeapContainerVerifier asserts the emitted modules verify cleanly: the
// original failure was a *verifier* error, not a wrong answer.
func TestAOTHeapContainerVerifier(t *testing.T) {
	progs := []string{
		"def total(xs) -> int:\n    n = 0\n    for x in xs:\n        n = n + x\n    return n\n\nprint(total([1, 2, 3]))\n",
		"def head(xs) -> int:\n    return xs[0]\n\nprint(head([7, 8, 9]))\n",
		"def size(s: set[int]) -> int:\n    return len(s)\n\nprint(size({1, 2, 3}))\n",
		"def total(xs=[1, 2]) -> int:\n    n = 0\n    for x in xs:\n        n = n + x\n    return n\n\nprint(total())\n",
	}
	for _, src := range progs {
		// compileAndRun drives llc (which runs the module verifier) and cc, and
		// fails the test on any verifier error.
		compileAndRun(t, src)
	}
}

// TestAOTHeapContainerStringElement: a string in a heap container used to be a verifier
// failure (an @.strN global in an i32 parameter). Strings now live in the runtime interned
// table, so a list of strings can be built and passed to a function (roadmap Gap I.2).
func TestAOTHeapContainerStringElement(t *testing.T) {
	src := "def f(xs) -> int:\n    return len(xs)\n\nprint(f([\"a\", \"b\"]))\n"
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("string container argument must compile now: %v", err)
	}
	if !strings.Contains(res.IR, "rt_str_intern2") {
		t.Errorf("string elements should be interned into the runtime string table:\n%s", res.IR)
	}
	if strings.Contains(res.IR, "i32 @.str") {
		t.Errorf("no string pointer may be stored in an i32 slot:\n%s", res.IR)
	}
	if got := compileAndRun(t, src); got != "2\n" {
		t.Errorf("AOT output = %q, want \"2\\n\"", got)
	}
}

// TestHeapContainerConformanceProgram runs the checked-in conformance program.
func TestHeapContainerConformanceProgram(t *testing.T) {
	src := readProgram(t, "heap_containers.gy")
	checkBackendParityWant(t, runInterp(t, src), runAOT(t, src), "heap_containers.want")
}

// TestAOTHeapContainerParamScopeDoesNotLeak pins the scoping rule: a container
// parameter named `xs` must not make an unrelated module-level `xs` look like a
// container. Before the registration was scoped to the function body, the
// module's `xs = []` / `xs.append(i)` was lowered against a slot that only
// existed inside the callee (`llc: use of undefined value '%_xs'`).
func TestAOTHeapContainerParamScopeDoesNotLeak(t *testing.T) {
	src := `def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

xs = []
i = 0
while i < 4:
    xs.append(i)
    i = i + 1

print(total(xs))
print(len(xs))
print(total([10, 20]))
`
	want := "6\n4\n30\n"
	lang.RecordedStdoutIs(t, src, want)
	if got := runAOT(t, src); got != want {
		t.Errorf("AOT: got %q want %q", got, want)
	}
}
