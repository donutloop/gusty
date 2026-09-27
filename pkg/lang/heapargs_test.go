package lang

import (
	"strings"
	"testing"
)

// --- inference -------------------------------------------------------------

func parseProg(t *testing.T, src string) *Program {
	t.Helper()
	p, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return p
}

func TestHeapArgKindsInference(t *testing.T) {
	tests := []struct {
		name string
		src  string
		fn   string
		want map[int]int
	}{
		{
			"list literal call site",
			"def total(xs):\n    return 0\n\ntotal([1, 2, 3])\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"annotation is authoritative",
			"def total(xs: list[int]):\n    return 0\n\ntotal(0)\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"Sequence annotation is a list handle",
			"def total(xs: Sequence[int]):\n    return 0\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"Iterator annotation is a list handle",
			"def drain(xs: Iterator[int]):\n    return 0\n",
			"drain", map[int]int{0: HeapList},
		},
		{
			"dict annotation",
			"def lookup(d: dict[str, int]):\n    return 0\n",
			"lookup", map[int]int{0: HeapDict},
		},
		{
			"set annotation",
			"def size(s: set[int]):\n    return 0\n",
			"size", map[int]int{0: HeapSet},
		},
		{
			"variable assigned a list is a witness",
			"def total(xs):\n    return 0\n\nnums = [1, 2]\ntotal(nums)\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"variable assigned an int is not a witness",
			"def total(xs):\n    return 0\n\nnums = 3\ntotal(nums)\n",
			"total", nil,
		},
		{
			"keyword argument",
			"def total(xs):\n    return 0\n\ntotal(xs=[1])\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"container default argument",
			"def total(xs=[1, 2]):\n    return 0\n\ntotal()\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"comprehension argument",
			"def total(xs):\n    return 0\n\ntotal([x for x in [1, 2]])\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"generator call argument",
			"def upTo(n):\n    yield n\n\ndef total(xs):\n    return 0\n\ntotal(upTo(3))\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"later call site still witnesses an earlier definition",
			"def total(xs):\n    return 0\n\ntotal(0)\ntotal([1, 2])\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"witness from inside another function body",
			"def total(xs):\n    return 0\n\ndef driver():\n    return total([1, 2])\n",
			"total", map[int]int{0: HeapList},
		},
		{
			"second parameter",
			"def pick(xs, k):\n    return 0\n\npick([1, 2], 1)\n",
			"pick", map[int]int{0: HeapList},
		},
		{
			"dict literal argument",
			"def lookup(d):\n    return 0\n\nlookup({1: 2})\n",
			"lookup", map[int]int{0: HeapDict},
		},
		{
			"set literal argument",
			"def size(s):\n    return 0\n\nsize({1, 2})\n",
			"size", map[int]int{0: HeapSet},
		},
		{
			"no call sites means no inference",
			"def total(xs):\n    return 0\n",
			"total", nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := heapArgKinds(parseProg(t, tt.src))
			if len(got[tt.fn]) != len(tt.want) {
				t.Fatalf("heapArgKinds(%s) = %v, want %v", tt.fn, got[tt.fn], tt.want)
			}
			for i, k := range tt.want {
				if got[tt.fn][i] != k {
					t.Errorf("param %d of %s: got kind %s, want %s", i, tt.fn, HeapKindName(got[tt.fn][i]), HeapKindName(k))
				}
			}
		})
	}
}

func TestHeapKindName(t *testing.T) {
	for k, want := range map[int]string{
		HeapNone: "none", HeapList: "list", HeapDict: "dict", HeapSet: "set", HeapInst: "instance",
	} {
		if got := HeapKindName(k); got != want {
			t.Errorf("HeapKindName(%d) = %q, want %q", k, got, want)
		}
	}
}

func TestHeapLiteralKindIgnoresNonLiterals(t *testing.T) {
	// Only literals are materialised at the call site: a comprehension or a
	// variable already produces a heap handle through the normal lowering path.
	if k := heapLiteralKind(&ListLit{}); k != HeapList {
		t.Errorf("ListLit: got %s", HeapKindName(k))
	}
	if k := heapLiteralKind(&Comp{Kind: CompList}); k != HeapNone {
		t.Errorf("Comp must not be re-materialised, got %s", HeapKindName(k))
	}
	if k := heapLiteralKind(&Name{Value: "xs"}); k != HeapNone {
		t.Errorf("Name must not be re-materialised, got %s", HeapKindName(k))
	}
}

func TestHeapArgKindsDeterministic(t *testing.T) {
	src := "def f(a, b):\n    return 0\n\nf([1], {1: 2})\nf({3}, [4])\n"
	first := heapArgKinds(parseProg(t, src))
	for i := 0; i < 5; i++ {
		got := heapArgKinds(parseProg(t, src))
		if len(got) != len(first) || got["f"][0] != first["f"][0] || got["f"][1] != first["f"][1] {
			t.Fatalf("heapArgKinds not deterministic: %v vs %v", got, first)
		}
	}
}

// --- codegen ---------------------------------------------------------------

func TestHeapContainerArgumentIR(t *testing.T) {
	ir := llcCompiles(t, `def total(xs) -> int:
    n = 0
    for x in xs:
        n = n + x
    return n

print(total([1, 2, 3]))
`)
	// The literal is materialised into the runtime heap ...
	if !strings.Contains(ir, "call i32 @rt_alloc(i32 1)") {
		t.Errorf("expected a heap list allocation in:\n%s", ir)
	}
	if !strings.Contains(ir, "@rt_set_elem(i32 %") {
		t.Errorf("expected heap element stores in:\n%s", ir)
	}
	// ... and never passed as a compile-time global (the verifier bug).
	if strings.Contains(ir, "call i32 @total(i32 @.") {
		t.Errorf("list global was passed where a handle was required:\n%s", ir)
	}
	// The callee iterates the handle with the runtime list helpers, instead of
	// treating it as an integer range bound (the silent-miscompile bug).
	body := ir[strings.Index(ir, "define i32 @total"):]
	if !strings.Contains(body, "@rt_list_len(") || !strings.Contains(body, "@rt_get_elem(") {
		t.Errorf("callee does not iterate the container handle:\n%s", body)
	}
}

func TestHeapContainerParameterAccessorsUseHeap(t *testing.T) {
	cases := []struct {
		name, src, want string
	}{
		{
			"indexing a list parameter",
			"def head(xs) -> int:\n    return xs[0]\n\nprint(head([7, 8, 9]))\n",
			"@rt_get_elem(",
		},
		{
			"len of a list parameter",
			"def count(xs) -> int:\n    return len(xs)\n\nprint(count([1, 2, 3]))\n",
			"@rt_list_len(",
		},
		{
			"indexing a dict parameter",
			"def lookup(d: dict[int, int]) -> int:\n    return d[1]\n\nprint(lookup({1: 5}))\n",
			"@rt_dict_get(",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ir := llcCompiles(t, tc.src)
			if !strings.Contains(ir, tc.want) {
				t.Errorf("expected %s in:\n%s", tc.want, ir)
			}
		})
	}
}

func TestHeapContainerArgumentStringElementIsDiagnostic(t *testing.T) {
	// A string element cannot live in an i32 heap slot. The old behaviour was
	// IR that LLVM's verifier rejected; it must be a compile error instead.
	_, err := Compile("def f(xs):\n    return 0\n\nf([\"a\"])\n")
	if err == nil {
		t.Fatalf("expected an unsupported-container diagnostic")
	}
	if !strings.Contains(err.Error(), "strings inside runtime containers") {
		t.Errorf("unexpected error: %v", err)
	}
	// ... and the message says where it does work.
	if !strings.Contains(err.Error(), "interpreter") {
		t.Errorf("error should point at the working backend: %v", err)
	}
}

func TestHeapContainerParamsRootedPerFunction(t *testing.T) {
	// Two functions with the same parameter name need two GC roots: root keys
	// are per (function, parameter), not per name.
	ir := llcCompiles(t, `def a(xs) -> int:
    return len(xs)

def b(xs) -> int:
    return len(xs) + 1

print(a([1]) + b([2, 3]))
`)
	if n := strings.Count(ir, "call i32 @rt_list_len("); n != 2 {
		t.Fatalf("expected both callees to measure the handle (got %d):\n%s", n, ir)
	}
	if !strings.Contains(ir, "store i32* %_xs, i32** %gc.slot") {
		t.Errorf("expected a container parameter to be rooted:\n%s", ir)
	}
}

func TestHeapContainerDoesNotBreakScalars(t *testing.T) {
	// A function whose parameter is never handed a container still compiles as
	// a plain i32 parameter.
	ir := llcCompiles(t, "def inc(x) -> int:\n    return x + 1\n\nprint(inc(41))\n")
	if strings.Contains(ir, "define i32 @inc(i32 %p0)") == false {
		t.Errorf("scalar parameter changed shape:\n%s", ir)
	}
	if strings.Contains(ir, "@rt_alloc(i32 1)\n  call void @rt_set_elem") && strings.Contains(ir, "@inc(i32") {
		t.Errorf("scalar argument must not be heap-allocated:\n%s", ir)
	}
}

// TestHeapContainerParamScopeIsLocal pins the scoping rule behind the above:
// registering a container parameter must not leak the name into module scope.
// The module-level `xs = []` then has to allocate its own slot; when the
// registration leaked, codegen emitted a load of `%_xs` that only the callee
// declared (llc: "use of undefined value '%_xs'").
func TestHeapContainerParamScopeIsLocal(t *testing.T) {
	ir := llcCompiles(t, `def total(xs) -> int:
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
`)
	// The module's list variable allocates its own slot ...
	if !strings.Contains(ir, "%_xs = alloca i32") {
		t.Errorf("module-level list variable did not allocate a slot:\n%s", ir)
	}
	// ... and every load of it is dominated by an alloca in the same function.
	mainIdx := strings.Index(ir, "define i32 @main(")
	if mainIdx < 0 {
		t.Fatalf("no main in module:\n%s", ir)
	}
	mainPart := ir[mainIdx:]
	if strings.Contains(mainPart, "%_xs") && !strings.Contains(mainPart, "%_xs = alloca i32") {
		t.Errorf("main reads the _xs slot without declaring it:\n%s", mainPart)
	}
}
