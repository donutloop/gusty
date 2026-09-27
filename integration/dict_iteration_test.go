package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Iterating a container in AOT (Gap K.2).
//
// `for k in d:` used to compile to a loop that compared the index against the dict's
// *handle* instead of its length, so it ran zero times and printed nothing — silently,
// with a verified module. The interpreter yielded the keys. A parity harness that only
// diffs the backends cannot see a shape no program exercises, so these cases exist to
// pin the shape itself, against Python's answer.

var containerIterCases = []struct {
	name string
	src  string
	want string
}{
	{
		"dict keys in insertion order",
		"d = {3: 30, 1: 10, 2: 20}\nfor k in d:\n    print(k)\n",
		"3\n1\n2\n",
	},
	{
		"dict keys summed",
		"d = {1: 2, 3: 4}\ns = 0\nfor k in d:\n    s = s + k\n\nprint(s)\n",
		"4\n",
	},
	{
		"dict values via the key inside the loop",
		"d = {1: 2, 3: 4}\nt = 0\nfor k in d:\n    t = t + d[k]\n\nprint(t)\n",
		"6\n",
	},
	{
		"key membership inside the loop",
		"d = {1: 2, 5: 6}\nhits = 0\nfor k in d:\n    if k in d:\n        hits = hits + 1\n\nprint(hits)\n",
		"2\n",
	},
	{
		"empty dict iterates zero times",
		"d = {}\nn = 0\nfor k in d:\n    n = n + 1\n\nprint(n)\n",
		"0\n",
	},
	{
		"single entry dict",
		"d = {7: 70}\nseen = 0\nfor k in d:\n    seen = k\n\nprint(seen)\n",
		"7\n",
	},
	{
		"dict built by mutation then iterated",
		"d = {}\nfor i in range(3):\n    d[i] = i * 10\n\nt = 0\nfor k in d:\n    t = t + d[k]\n\nprint(t)\nprint(len(d))\n",
		"30\n3\n",
	},
	{
		"dict returned from a function",
		"def make(n):\n    d = {}\n\n    for i in range(n):\n        d[i] = i\n\n    return d\n\nd = make(4)\ns = 0\nfor k in d:\n    s = s + k\n\nprint(s)\n",
		"6\n",
	},
	{
		"set members",
		"s = {3, 1, 2}\nt = 0\nfor x in s:\n    t = t + x\n\nprint(t)\n",
		"6\n",
	},
	{
		"dict built by mutation and iterated for keys",
		"d = {}\nn = 0\nfor i in range(4):\n    d[i] = 1\n\nfor k in d:\n    n = n + 1\n\nprint(n)\n",
		"4\n",
	},
	{
		"list iteration still works",
		"xs = [5, 6, 7]\nt = 0\nfor x in xs:\n    t = t + x\n\nprint(t)\n",
		"18\n",
	},
	{
		"generator iteration still works",
		"def up(n):\n    k = 0\n\n    while k < n:\n        yield k\n        k = k + 1\n\nt = 0\nfor v in up(4):\n    t = t + v\n\nprint(t)\n",
		"6\n",
	},
	// Two container loops that reuse the same variable name: the loop variable's
	// slot must be allocated in the block that branches into the loop, because an
	// alloca inside the body block does not dominate the blocks after the loop
	// ("Instruction does not dominate all uses").
	{
		"two loops reusing the loop variable",
		"p = {1: 10, 2: 20}\nq = {3: 30}\nt = 0\nfor k in p:\n    t = t + p[k]\n\nfor k in q:\n    t = t + q[k]\n\nprint(t)\n",
		"60\n",
	},
	{
		"container loop after a range loop reusing the name",
		"d = {4: 40, 5: 50}\ns = 0\nfor k in range(3):\n    s = s + k\n\nfor k in d:\n    s = s + d[k]\n\nprint(s)\n",
		"93\n", // (0+1+2) + (40+50)
	},
	{
		"list loop and dict loop reusing the name",
		"xs = [1, 2, 3]\nd = {7: 70}\nt = 0\nfor k in xs:\n    t = t + k\n\nfor k in d:\n    t = t + d[k]\n\nprint(t)\n",
		"76\n",
	},
	{
		"dict keys printed alongside values",
		"d = {2: 20, 4: 40}\nfor k in d:\n    print(k, d[k])\n",
		"2 20\n4 40\n",
	},
}

func TestContainerIterationMatchesPythonOnBothBackends(t *testing.T) {
	for _, tc := range containerIterCases {
		t.Run(tc.name, func(t *testing.T) {
			ip, ierr := interpRunChecked(t, tc.src)
			if ierr != nil {
				t.Fatalf("interpreter rejected a valid program: %v\n%s", ierr, tc.src)
			}
			if ip != tc.want {
				t.Errorf("interpreter = %q, want %q (Python's answer)\n%s", ip, tc.want, tc.src)
			}
			aot, aerr := runAOTConformance(t, tc.src)
			if aerr != nil {
				t.Fatalf("AOT rejected a valid program: %v\n%s", aerr, tc.src)
			}
			if aot != tc.want {
				t.Errorf("AOT = %q, want %q (Python's answer)\n%s", aot, tc.want, tc.src)
			}
		})
	}
}

// TestDictIterationUsesTheRuntimeLength is the shape check: the loop bound must be
// rt_dict_len(handle), and the key of entry i is read from slot 2*i because the runtime
// stores dict entries as [key, value] pairs. Comparing the index against the handle is
// what made every dict loop exit immediately.
func TestDictIterationUsesTheRuntimeLength(t *testing.T) {
	res, err := lang.Compile("d = {1: 2, 3: 4}\nt = 0\nfor k in d:\n    t = t + k\n\nprint(t)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	ir := res.IR
	for _, want := range []string{"call i32 @rt_dict_len(", "mul i32 ", "call i32 @rt_get_elem("} {
		if !strings.Contains(ir, want) {
			t.Errorf("dict iteration IR should contain %q:\n%s", want, ir)
		}
	}
	if strings.Contains(ir, "icmp slt i32 %_k.ld") {
		t.Errorf("the loop bound must be the dict length, not a raw counter vs handle:\n%s", ir)
	}
	if v, err := lang.VerifyModuleIR(ir, 0); err != nil || !v.OK {
		t.Errorf("dict iteration module must verify: %v %v", v.Errors, err)
	}
}

// TestDictIterationIRHasNoTruthyShortcut: an empty dict must produce zero iterations
// because the *length* is 0 — not because a null handle happened to compare false.
func TestDictIterationIRHasNoTruthyShortcut(t *testing.T) {
	res, err := lang.Compile("d = {}\nn = 0\nfor k in d:\n    n = n + 1\n\nprint(n)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(res.IR, "call i32 @rt_dict_len(") {
		t.Errorf("empty-dict loop must still test the runtime length:\n%s", res.IR)
	}
	if v, err := lang.VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("module must verify: %v %v", v.Errors, err)
	}
}
