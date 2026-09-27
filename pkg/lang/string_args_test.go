package lang

import (
	"strings"
	"testing"
)

// Gap J.5 — string arguments to user functions in the AOT backend.
//
// `greet("ada")` used to emit `call i32 @greet(i32 @.str1)`: a global pointer in an i32
// parameter, which LLVM rejects. With the interned string table from Gap I.2 the argument is
// interned at the call site and the callee receives the index, so the common shapes compile.
// What is still unsupported (concatenation, string methods, arithmetic on a string) must be an
// actionable compile diagnostic, never IR the verifier rejects (ADR 0166).

// TestStringArgumentsCompile: the shapes that used to be refused, and what they must produce.
func TestStringArgumentsCompile(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"positional", "def greet(name):\n    print(name)\n\ngreet(\"ada\")\n"},
		{"keyword", "def greet(name):\n    print(name)\n\ngreet(name=\"ada\")\n"},
		{"default", "def greet(name=\"ada\"):\n    print(name)\n\ngreet()\n"},
		{"annotation", "def greet(name: str):\n    print(name)\n\ngreet(\"ada\")\n"},
		{"returned", "def echo(s):\n    return s\n\nprint(echo(\"yo\"))\n"},
		{"measured", "def size(s):\n    return len(s)\n\nprint(size(\"abcd\"))\n"},
		{"compared", "def is_yes(s):\n    return s == \"yes\"\n\nprint(is_yes(\"yes\"))\n"},
		{"both sides runtime", "def eq2(a, b):\n    return a == b\n\nprint(eq2(\"x\", \"x\"))\n"},
		{"forwarded", "def a(s):\n    print(s)\n\ndef b(s):\n    a(s)\n\nb(\"fwd\")\n"},
		{"stored in container", "def fill(out, v):\n    out.append(v)\n\nxs = []\nfill(xs, \"hi\")\nprint(xs)\n"},
		{"membership needle", "def has(xs, want):\n    return want in xs\n\nprint(has([\"a\", \"b\"], \"a\"))\n"},
		{"dict key", "def put(d, k):\n    d[k] = 1\n\nd = {}\nput(d, \"kk\")\nprint(d)\n"},
		{"set member", "def ins(s, v):\n    s.add(v)\n\nt = set()\nins(t, \"q\")\nprint(t)\n"},
	}
	for _, tc := range cases {
		res, err := Compile(tc.src)
		if err != nil {
			t.Errorf("%s: must compile: %v", tc.name, err)
			continue
		}
		if strings.Contains(res.IR, "i32 @.str") {
			t.Errorf("%s: a string global was used where an i32 was required:\n%s", tc.name, res.IR)
		}
		if v, verr := VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: emitted module must verify: %v %v", tc.name, v.Errors, verr)
		}
	}
}

// TestUnsupportedStringUsesStayDiagnostics: the cases the runtime cannot serve yet report
// themselves, name the backend that works, and never reach the verifier.
func TestUnsupportedStringUsesStayDiagnostics(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			"concatenation needs an allocator",
			"def f(s):\n    return s + \"!\"\n\nprint(f(\"hi\"))\n",
			"concatenating a runtime string",
		},
		{
			"arithmetic on a string parameter",
			"def f(s):\n    return s * 2\n\nprint(f(\"hi\"))\n",
			"is not supported in the AOT backend",
		},
		{
			"ordering on a string parameter",
			"def f(s):\n    return s < \"z\"\n\nprint(f(\"hi\"))\n",
			"is not supported in the AOT backend",
		},
	}
	for _, tc := range cases {
		_, err := Compile(tc.src)
		if err == nil {
			t.Errorf("%s: expected an actionable compile error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: message should contain %q, got: %v", tc.name, tc.want, err)
		}
		if !strings.Contains(err.Error(), "interpreter") {
			t.Errorf("%s: message should name the backend that works, got: %v", tc.name, err)
		}
	}
}

// TestMixedCallSitesAreNotGuessedAt: one helper called with a string and with a number cannot
// have its parameter treated as a string — printing 7 through the string table produced
// "(null)", which is worse than a diagnostic.
func TestMixedCallSitesAreNotGuessedAt(t *testing.T) {
	mixed := "def f(s):\n    print(s)\n\nf(\"str\")\nf(7)\n"
	if _, err := Compile(mixed); err == nil {
		t.Fatalf("a parameter used as both string and int must not be classified as a string")
	}
	// Integers only still compile, unchanged by the string path.
	ints := "def f(s):\n    print(s)\n\nf(7)\nf(8)\n"
	res, err := Compile(ints)
	if err != nil {
		t.Fatalf("integer arguments must keep compiling: %v", err)
	}
	if strings.Contains(res.IR, "rt_str_intern2") {
		t.Errorf("an integer-only helper should not touch the string table:\n%s", res.IR)
	}
}

// TestNonStringArgumentsStillCompile is the non-regression half: the string machinery must not
// disturb ordinary numeric or container arguments.
func TestNonStringArgumentsStillCompile(t *testing.T) {
	progs := []string{
		"def twice(v):\n    return v + v\n\nprint(twice(21))\n",
		"def total(xs) -> int:\n    t = 0\n    for x in xs:\n        t = t + x\n    return t\n\nprint(total([1, 2, 3]))\n",
		"def f(v=7):\n    return v\n\nprint(f())\n",
		"def put(d, k, v):\n    d[k] = v\n\nd = {}\nput(d, 1, 2)\nprint(d)\n",
	}
	for _, p := range progs {
		res, err := Compile(p)
		if err != nil {
			t.Errorf("Compile(%q): %v", p, err)
			continue
		}
		if strings.Contains(res.IR, "i32 @.str") {
			t.Errorf("no string global may be passed as an i32:\n%s", res.IR)
		}
		if v, verr := VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("module must verify for %q: %v %v", p, v.Errors, verr)
		}
	}
}

// TestStrArgKindsIsDeterministic: the inference runs to a fixed point over a map, so an
// unordered walk would make compilation reproducible only by luck.
func TestStrArgKindsIsDeterministic(t *testing.T) {
	src := "def a(s):\n    b(s)\n\ndef b(t):\n    print(t)\n\na(\"x\")\n"
	var first string
	for i := 0; i < 8; i++ {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if first == "" {
			first = res.IR
			continue
		}
		if res.IR != first {
			t.Fatalf("compiled IR differs between runs of the same source")
		}
	}
}
