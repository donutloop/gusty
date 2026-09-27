package lang

import (
	"strings"
	"testing"
)

// TestStringArgumentIsReportedNotMiscompiled: a string is an `i8*` global in the
// AOT backend, so passing one to a user function used to emit
// `call i32 @greet(i32 @.str1)` — IR LLVM rejects with "global variable reference
// must have pointer type", discovered only when something downstream ran the
// verifier. Codegen must refuse up front, name the parameter, and say the
// interpreter supports the case.
func TestStringArgumentIsReportedNotMiscompiled(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // substring the message must contain
	}{
		{
			"positional string argument names the parameter",
			"def greet(name):\n    return 1\n\ngreet(\"ada\")\n",
			`parameter "name" of greet`,
		},
		{
			"keyword string argument names the parameter",
			"def greet(name):\n    return 1\n\ngreet(name=\"ada\")\n",
			`parameter "name" of greet`,
		},
		{
			"string default argument names the parameter",
			"def greet(name=\"ada\"):\n    return 1\n\nprint(greet())\n",
			`parameter "name" of greet`,
		},
		{
			"f-string argument is a string too",
			"def greet(name):\n    return 1\n\nx = 2\ngreet(f\"hi {1}\")\n",
			`parameter "name" of greet`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("expected an actionable compile error")
			}
			msg := err.Error()
			if !strings.Contains(msg, "strings are not supported as function arguments") {
				t.Errorf("message should state the unsupported case, got: %v", err)
			}
			if !strings.Contains(msg, "the interpreter supports them") {
				t.Errorf("message should say the interpreter supports it, got: %v", err)
			}
			if !strings.Contains(msg, tc.want) {
				t.Errorf("message should name the parameter (%s), got: %v", tc.name, err)
			}
		})
	}
}

// TestStringArgumentDiagnosticIsStable: an agent branches on the message, so it
// must not drift with the program shape.
func TestStringArgumentDiagnosticIsStable(t *testing.T) {
	progs := []string{
		"def greet(name):\n    return 1\n\ngreet(\"ada\")\n",
		"def greet(name):\n    return 1\n\nn = 3\ngreet(\"hi\")\n",
	}
	first := ""
	for _, p := range progs {
		_, err := Compile(p)
		if err == nil {
			t.Fatalf("expected an error for %q", p)
		}
		// Compare only the fixed prefix before the per-case parameter text.
		head := strings.SplitN(err.Error(), "; ", 2)[0]
		if i := strings.Index(head, "(parameter"); i >= 0 {
			head = head[:i]
		}
		if first == "" {
			first = head
		} else if head != first {
			t.Errorf("diagnostic wording drifted: %q vs %q", head, first)
		}
	}
}

// TestNonStringArgumentsStillCompile is the non-regression half: the check must
// fire on strings only, so integers, floats and containers keep lowering.
func TestNonStringArgumentsStillCompile(t *testing.T) {
	progs := []string{
		"def twice(v):\n    return v + v\n\nprint(twice(21))\n",
		"def total(xs) -> int:\n    t = 0\n    for x in xs:\n        t = t + x\n    return t\n\nprint(total([1, 2, 3]))\n",
		"def f(v=7):\n    return v\n\nprint(f())\n",
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
	}
}
