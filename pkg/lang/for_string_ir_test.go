package lang

import (
	"strings"
	"testing"
)

// An unrolled loop stores each element into the loop variable's slot. When the element was a string
// the store used the scalar value path, which hands back the raw `@.strN` global — so the module
// contained `store i32 @.str1, i32* %_c`: a global in an i32 slot. llc rejects it, and the exit-code
// contract calls an LLVM rejection a compiler bug, not a source error (roadmap Gap R.15, ADR 0208).
// The rule that fixes it is Gap I.2's: anything that has to live in an i32 slot stores the @str_tab
// index instead.

func stringLoopIR(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return res.IR
}

func TestStringLoopStoresAnInternedIndex(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a string iterable", "for c in \"ab\":\n    print(c)\n"},
		{"a literal list of strings", "for w in [\"x\", \"y\"]:\n    print(w)\n"},
		{"a mixed literal", "for m in [1, \"a\", 2]:\n    print(m)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ir := stringLoopIR(t, tc.src)
			if strings.Contains(ir, "store i32 @.str") {
				t.Errorf("%s: a string global was stored directly into an i32 slot; llc will reject the module\n%s", tc.name, ir)
			}
			if !strings.Contains(ir, "@rt_str_intern2(") {
				t.Errorf("%s: the string elements were never interned, so they cannot print as text\n%s", tc.name, ir)
			}
			if !strings.Contains(ir, "define internal i32 @rt_str_intern2(") {
				t.Errorf("%s: the module calls @rt_str_intern2 without defining it — an invalid module (Gap R.2's signature)\n%s", tc.name, ir)
			}
		})
	}
}

// TestNumberLoopStillStoresAPlainNumber guards the other direction: routing elements through the heap
// element lowering must not turn integers into heap values.
func TestNumberLoopStillStoresAPlainNumber(t *testing.T) {
	ir := stringLoopIR(t, "for x in [1, 2, 3]:\n    print(x)\n")
	if strings.Contains(ir, "@rt_str_intern2(") {
		t.Errorf("a numeric loop started interning strings:\n%s", ir)
	}
	if !strings.Contains(ir, "store i32 1, i32* %_x") {
		t.Errorf("a numeric loop no longer stores its constant elements plainly:\n%s", ir)
	}
}
