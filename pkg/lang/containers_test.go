package lang

import (
	"strings"
	"testing"
)

// Gap K.3 — the container operations a program reaches for second: draining a list
// (`xs.pop()`) and growing an empty set (`set()` then `s.add(x)`). Neither existed in
// either backend, so the natural `while xs:` drain loop and the empty-set constructor
// were simply missing, and `.pop` in the AOT fell through to the *string* method path
// ("string method pop on non-constant string").

func evalOnly(t *testing.T, src string) (string, error) {
	t.Helper()
	return evalCapture(t, src)
}

func TestListPopMatchesPython(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"xs = [1, 2, 3]\nprint(xs.pop())\n", "3\n"},
		{"xs = [1, 2, 3]\nprint(xs.pop())\nprint(xs)\n", "3\n[1, 2]\n"},
		{"xs = [1, 2, 3]\nprint(xs.pop(0))\nprint(xs)\n", "1\n[2, 3]\n"},
		{"xs = [1, 2, 3]\nprint(xs.pop(1))\nprint(xs)\n", "2\n[1, 3]\n"},
		{"xs = [1, 2, 3]\nprint(xs.pop(-1))\n", "3\n"},
		{"xs = [1, 2, 3]\nprint(xs.pop(-3))\n", "1\n"},
		{"xs = []\nxs.append(4)\nprint(xs.pop())\nprint(len(xs))\n", "4\n0\n"},
		// draining in a while loop is the idiom this feature exists for
		{"xs = [1, 2, 3]\nt = 0\nwhile xs:\n    t = t + xs.pop()\n\nprint(t)\n", "6\n"},
	}
	for _, tc := range cases {
		got, err := evalOnly(t, tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s\n  compiled = %q, want %q (the reference's answer)", tc.src, got, tc.want)
		}
	}
}

func TestListPopErrorsAreTypedAndCatchable(t *testing.T) {
	cases := []struct {
		src     string
		exnType string
		msg     string
	}{
		{"xs = []\nxs.pop()\n", "IndexError", "pop from empty list"},
		{"xs = [1]\nxs.pop(5)\n", "IndexError", "pop index out of range"},
		{"xs = [1]\nxs.pop(-5)\n", "IndexError", "pop index out of range"},
	}
	for _, tc := range cases {
		_, err := evalOnly(t, tc.src)
		ee, ok := err.(*TrapError)
		if !ok {
			t.Errorf("%s: want *TrapError, got %T (%v)", tc.src, err, err)
			continue
		}
		if ee.ExnType != tc.exnType {
			t.Errorf("%s: ExnType = %q, want %q", tc.src, ee.ExnType, tc.exnType)
		}
		if !strings.Contains(ee.Msg, tc.msg) {
			t.Errorf("%s: Msg = %q, want it to contain %q", tc.src, ee.Msg, tc.msg)
		}
		// and catchable by class name
		handler := "try:\n    " + strings.ReplaceAll(strings.TrimRight(tc.src, "\n"), "\n", "\n    ") +
			"\nexcept " + tc.exnType + ":\n    print(\"caught\")\n"
		got, err := evalOnly(t, handler)
		if err != nil || got != "caught\n" {
			t.Errorf("%s: `except %s:` should catch it (got %q, err %v)", tc.src, tc.exnType, got, err)
		}
	}
}

func TestContainerConstructorsMatchPython(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// `{}` is the empty dict and `{1, 2}` a set; the empty set has no literal, so
		// set() is the only way to write one.
		{"s = set()\nprint(s)\n", "set()\n"},
		{"s = set()\ns.add(1)\ns.add(2)\ns.add(1)\nprint(len(s))\n", "2\n"},
		{"s = set()\ns.add(1)\ns.discard(1)\ns.discard(9)\nprint(len(s))\n", "0\n"},
		{"s = set()\ns.add(1)\ns.add(2)\ns.clear()\nprint(len(s))\n", "0\n"},
		{"xs = list()\nxs.append(7)\nxs.append(8)\nprint(xs)\n", "[7, 8]\n"},
		{"d = dict()\nd[1] = 9\nprint(d[1])\nprint(len(d))\n", "9\n1\n"},
		{"s = set()\nfor i in range(4):\n    s.add(i)\n    s.add(i)\n\nprint(len(s))\n", "4\n"},
	}
	for _, tc := range cases {
		got, err := evalOnly(t, tc.src)
		if err != nil {
			t.Errorf("%s: %v", tc.src, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s\n  compiled = %q, want %q (the reference's answer)", tc.src, got, tc.want)
		}
	}
}

func TestSetRemoveRaisesKeyErrorLikePython(t *testing.T) {
	_, err := evalOnly(t, "s = {1, 2}\ns.remove(9)\n")
	ee, ok := err.(*TrapError)
	if !ok {
		t.Fatalf("want *TrapError, got %T", err)
	}
	if ee.ExnType != "KeyError" {
		t.Errorf("ExnType = %q, want KeyError (discard is silent, remove is not)", ee.ExnType)
	}
}

// TestContainerMethodsAndConstructorsInIR pins the AOT lowering: a real heap allocation
// for the constructors (not a compile-time global), rt_pop for the removal, and the
// runtime set helpers.
func TestContainerMethodsAndConstructorsInIR(t *testing.T) {
	res, err := Compile("xs = list()\nxs.append(1)\nprint(xs.pop())\n")
	if err != nil {
		t.Fatalf("compile list(): %v", err)
	}
	for _, want := range []string{"call i32 @rt_alloc(i32 1)", "call void @rt_append", "call i32 @rt_pop(", "@rt_list_len"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("list() IR missing %q:\n%s", want, res.IR)
		}
	}
	// The constructor must not store a folded global into the variable slot — that is
	// the `store i32 @.set1, i32* %_s` bug class (ADR 0163).
	if strings.Contains(res.IR, "store i32 @.") {
		t.Errorf("container constructor must allocate, not store a folded global:\n%s", res.IR)
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("list() module must verify: %v %v", v.Errors, err)
	}

	res, err = Compile("s = set()\ns.add(1)\ns.add(2)\ns.discard(1)\nprint(len(s))\n")
	if err != nil {
		t.Fatalf("compile set(): %v", err)
	}
	for _, want := range []string{"call i32 @rt_alloc(i32 3)", "call void @rt_set_add", "call void @rt_set_discard"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("set() IR missing %q:\n%s", want, res.IR)
		}
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("set() module must verify: %v %v", v.Errors, err)
	}

	res, err = Compile("d = dict()\nd[1] = 9\nprint(d[1])\n")
	if err != nil {
		t.Fatalf("compile dict(): %v", err)
	}
	for _, want := range []string{"call i32 @rt_alloc(i32 2)", "call void @rt_dict_put"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("dict() IR missing %q:\n%s", want, res.IR)
		}
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("dict() module must verify: %v %v", v.Errors, err)
	}
}

// TestPopBoundsCheckRaisesInIR: an out-of-range pop must take the exception path rather
// than reading/writing outside the element array.
func TestPopBoundsCheckRaisesInIR(t *testing.T) {
	res, err := Compile("xs = [1, 2, 3]\ni = 9\nprint(xs.pop(i))\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{"pop index out of range", "@exn_flag", "@rt_pop("} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("pop IR missing %q:\n%s", want, res.IR)
		}
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("pop module must verify: %v %v", v.Errors, err)
	}
}

// TestContainerCopyIsADiagnosticNotMiscompile (ADR 0166): the AOT has no element loop for
// `list(xs)`/`set(xs)` copies, so it must refuse with a message that names the construct
// and the backend that works.
func TestContainerCopyIsADiagnosticNotMiscompile(t *testing.T) {
	for _, src := range []string{"xs = [1, 2]\nys = list(xs)\nprint(ys)\n", "xs = [1, 2]\ns = set(xs)\nprint(len(s))\n"} {
		_, err := Compile(src)
		if err == nil {
			t.Fatalf("%s: AOT should refuse the copy form", src)
		}
		if !strings.Contains(err.Error(), "copies are not supported in the AOT backend yet") ||
			!strings.Contains(err.Error(), "CPython answers them") {
			t.Errorf("%s: diagnostic should name the construct and the working backend, got %q", src, err.Error())
		}
	}
}

// TestStringConstantsEscapeQuotes: a '"' in the payload used to terminate the LLVM string
// literal early, and the module failed to verify with a nonsense array length. Traceback
// frames (which contain quotes) hit it first, but any program string with a quote did.
func TestStringConstantsEscapeQuotes(t *testing.T) {
	res, err := Compile("print(\"say \\\"hi\\\"\")\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "\\22") {
		t.Errorf("a double quote must be escaped as \\22 in the IR:\n%s", res.IR)
	}
	if strings.Contains(res.IR, `c"say "hi""`) {
		t.Errorf("an unescaped quote leaked into the IR literal:\n%s", res.IR)
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("module with a quoted string must verify: %v %v", v.Errors, err)
	}
}
