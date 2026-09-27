package lang

import (
	"os"
	"strings"
	"testing"
)

// One table, three users (ADR 0169): the interpreter's isExnClass, the codegen's
// exnCode, and the checker's name set all come from pkg/lang/exceptions.go. These
// tests pin that they cannot drift apart.

func TestExceptionTableIsConsistent(t *testing.T) {
	seen := map[int]string{}
	for _, c := range exnClasses {
		if !isExnClass(c.name) {
			t.Errorf("isExnClass(%q) = false, but it is in the table", c.name)
		}
		if got := exnClassCode(c.name); got != c.code {
			t.Errorf("exnClassCode(%q) = %d, want %d", c.name, got, c.code)
		}
		if got := exnCode(c.name); got != c.code {
			t.Errorf("codegen exnCode(%q) = %d, want %d", c.name, got, c.code)
		}
		if prev, dup := seen[c.code]; dup {
			t.Errorf("exception codes collide: %s and %s both %d", prev, c.name, c.code)
		}
		seen[c.code] = c.name
	}
	if isExnClass("NotAnError") || exnCode("NotAnError") != 0 {
		t.Error("an unknown class must not resolve")
	}
	m := builtinExceptions()
	if len(m) != len(exnClasses) {
		t.Errorf("builtinExceptions has %d entries, table has %d", len(m), len(exnClasses))
	}
	for name := range m {
		if !isExnClass(name) {
			t.Errorf("checker knows %q but the interpreter does not", name)
		}
	}
}

// diagHas reports whether any diagnostic message contains sub.
func diagHas(diags []Diagnostic, sub string) bool {
	for _, d := range diags {
		if strings.Contains(d.Msg, sub) {
			return true
		}
	}
	return false
}

// TestBuiltinExceptionClassesResolveInChecker: the checker's `exceptions` map was
// declared and never populated, so the interpreter's most ordinary raise was an AOT
// compile error (`undefined name "ValueError"`).
func TestBuiltinExceptionClassesResolveInChecker(t *testing.T) {
	for _, c := range exnClasses {
		src := "raise " + c.name + "(\"boom\")\n"
		if diags := analyzeSrc(t, src); diagHas(diags, "undefined name") {
			t.Errorf("%s: should analyze cleanly, got %v", src, diags)
		}
	}
	// A user class derived from an exception is raisable and catchable too.
	if diags := analyzeSrc(t, "class MyError(Exception):\n    pass\n\nraise MyError(\"x\")\n"); diagHas(diags, "undefined name") {
		t.Errorf("exception subclass should resolve: %v", diags)
	}
	// A name that is not an exception class is still undefined.
	if diags := analyzeSrc(t, "raise Nope(\"x\")\n"); !diagHas(diags, "undefined name") {
		t.Errorf("an unknown class should still be an undefined name: %v", diags)
	}
}

// evalCapture runs src on the interpreter, returning what it printed and the error
// (without failing the test on the error, so error shapes can be asserted).
func evalCapture(t *testing.T, src string) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	_, _, evalErr := EvalExpr(src)
	os.Stdout = old
	w.Close()
	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	return string(buf[:n]), evalErr
}

// TestRuntimeIndexErrorsAreTypedAndCatchable: an operation that fails must raise a typed
// exception, so `except IndexError:` runs on the interpreter as it does in compiled code.
// Runtime errors used to abort instead — while the AOT caught them, i.e. the fast
// backend was more correct than the reference one.
func TestRuntimeIndexErrorsAreTypedAndCatchable(t *testing.T) {
	cases := []struct {
		src     string
		exnType string
		msg     string
	}{
		{"xs = [1]\nprint(xs[5])\n", "IndexError", "index out of range"},
		{"xs = [1]\nxs[5] = 2\n", "IndexError", "index out of range"},
		{"d = {1: 2}\nprint(d[9])\n", "KeyError", "key not found"},
		{"s = \"abc\"\ns[0] = \"z\"\n", "TypeError", "strings are immutable"},
		{"s = {1, 2}\ns[0] = 1\n", "TypeError", "cannot assign to a set element"},
		{"xs = [1]\nprint(xs[0])\n", "", ""}, // control: this one succeeds
	}
	for _, tc := range cases {
		out, err := evalCapture(t, tc.src)
		if tc.exnType == "" {
			if err != nil {
				t.Errorf("%s: should succeed, got %v", tc.src, err)
			}
			continue
		}
		ee, ok := err.(*EvalError)
		if !ok || ee == nil {
			t.Errorf("%s: want an *EvalError, got %T (%v)", tc.src, err, err)
			continue
		}
		if ee.ExnType != tc.exnType {
			t.Errorf("%s: ExnType = %q, want %q", tc.src, ee.ExnType, tc.exnType)
		}
		if !strings.Contains(ee.Msg, tc.msg) {
			t.Errorf("%s: Msg = %q, want it to contain %q", tc.src, ee.Msg, tc.msg)
		}
		// The class name is what `except <Class>:` matches on.
		body := strings.TrimRight(tc.src, "\n")
		try := "try:\n    " + strings.ReplaceAll(body, "\n", "\n    ") +
			"\nexcept " + tc.exnType + ":\n    print(\"caught\")\n"
		got, err := evalCapture(t, try)
		if err != nil {
			t.Errorf("%s: the handler should have caught it: %v", tc.src, err)
			continue
		}
		if got != "caught\n" {
			t.Errorf("%s: handler output = %q, want \"caught\\n\" (got %q)", tc.src, got, out)
		}
	}
}

// TestBareRaiseOfClassWorks: `raise IndexError` (the class, not a call) raises that class.
func TestBareRaiseOfClassWorks(t *testing.T) {
	_, err := evalCapture(t, "raise IndexError\n")
	ee, ok := err.(*EvalError)
	if !ok {
		t.Fatalf("want *EvalError, got %T", err)
	}
	if ee.ExnType != "IndexError" {
		t.Errorf("ExnType = %q, want IndexError", ee.ExnType)
	}
}

// TestTracebackNamesTheClass: Python's last line is `ValueError: boom`; dropping the
// class made the report less specific than the raise that produced it.
func TestTracebackNamesTheClass(t *testing.T) {
	_, err := evalCapture(t, "raise ValueError(\"boom\")\n")
	ee, ok := err.(*EvalError)
	if !ok {
		t.Fatalf("want *EvalError, got %T", err)
	}
	if ee.ExnType != "ValueError" || ee.ExnMsg != "boom" {
		t.Fatalf("typed exception = %+v, want ValueError/boom", ee)
	}
	// Rendered with a frame present (EvalProgram fills Traceback in; the CLI prints it).
	tb := (&EvalError{Msg: ee.Msg, ExnType: ee.ExnType, ExnMsg: ee.ExnMsg,
		Traceback: []Frame{{Name: "<module>", Line: 1, Col: 1}}}).RenderTraceback()
	if !strings.Contains(tb, "ValueError: boom") {
		t.Errorf("traceback = %q, want it to name the class and message", tb)
	}
	if !strings.Contains(tb, "Traceback (most recent call last)") {
		t.Errorf("traceback lost its header: %q", tb)
	}
	// An error with no class keeps its bare message — no invented "Exception:" prefix.
	plain := (&EvalError{Msg: "boom", Traceback: []Frame{{Name: "<module>", Line: 1, Col: 1}}}).RenderTraceback()
	if strings.Contains(plain, "Exception: boom") {
		t.Errorf("untyped error gained a class it never had: %q", plain)
	}
}

// TestRaiseCarriesTheMessageIntoCodegen: the report and the matched code come from one
// site, so `@exn_msg` must say what `@exn_code` means.
func TestRaiseCarriesTheMessageIntoCodegen(t *testing.T) {
	res, err := Compile("raise ValueError(\"boom\")\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, want := range []string{
		"store i32 1, i32* @exn_flag",
		"store i32 1, i32* @exn_code", // ValueError
		"@exn_msg",
		"ValueError: boom",
		"call void @rt_die",
		"ret i32 1",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("raise IR missing %q:\n%s", want, res.IR)
		}
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("raise module must verify: %v %v", v.Errors, err)
	}
}

// TestNoRaiseRuntimeWithoutARaise: a program that cannot raise carries none of the raise
// machinery, so clean programs' IR stays byte-identical.
func TestNoRaiseRuntimeWithoutARaise(t *testing.T) {
	res, err := Compile("print(42)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, unwanted := range []string{"rt_die", "@exn_msg", "rt_dict_has"} {
		if strings.Contains(res.IR, unwanted) {
			t.Errorf("a raise-free program should not carry %q:\n%s", unwanted, res.IR)
		}
	}
}

// TestContainerReadsAreCheckedInIR pins the read-site tests that turn a silent wrong
// value into a raise (the AOT used to print 0 for xs[5] and for a missing dict key).
func TestContainerReadsAreCheckedInIR(t *testing.T) {
	res, err := Compile("xs = [1, 2, 3]\ni = 5\nprint(xs[i])\n")
	if err != nil {
		t.Fatalf("compile list read: %v", err)
	}
	for _, want := range []string{"@rt_list_len", "IndexError: index out of range", "@rt_get_elem"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("list-read IR missing %q:\n%s", want, res.IR)
		}
	}
	res, err = Compile("d = {1: 2}\nk = 9\nprint(d[k])\n")
	if err != nil {
		t.Fatalf("compile dict read: %v", err)
	}
	for _, want := range []string{"@rt_dict_has", "KeyError: key not found", "@rt_dict_get"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("dict-read IR missing %q:\n%s", want, res.IR)
		}
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("dict-read module must verify: %v %v", v.Errors, err)
	}
}

// TestDictMembershipScansEveryKey is the regression for rt_dict_has, whose walk of the
// flat [key, value] array was bounded by the entry count and so never reached the later
// keys: `3 in {1: 2, 3: 4}` was false in compiled binaries.
func TestDictMembershipScansEveryKey(t *testing.T) {
	res, err := Compile("d = {1: 2, 3: 4, 5: 6}\nprint(5 in d)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "%limit = mul i32 %len, 2") {
		t.Errorf("rt_dict_has must bound its key scan at 2*count:\n%s", res.IR)
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("membership module must verify: %v %v", v.Errors, err)
	}
}
