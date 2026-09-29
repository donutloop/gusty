package lang

import (
	"strings"
	"testing"
)

// `str()` of a foldable argument used to be decided in two places, and they disagreed:
// the AST-level folder (stringConst) knew only int literals, and the codegen's
// string-value folder (irGen.stringVal, what print/len/concat consult) knew int literals
// and float constants. `str(None)` folded to "0" in one and not at all in the other, so
// the same program both printed the wrong text and emitted `store i32 @.str1` — a string
// global in a value position, which llc rejects (ADR 0183).

func TestStrFoldAgreesBetweenTheTwoFolders(t *testing.T) {
	for _, c := range []struct {
		src  string
		want string
	}{
		{`str(None)`, "None"},
		{`str(42)`, "42"},
		{`str(-7)`, "-7"},
		{`str("x")`, "x"},
	} {
		prog, err := parseProgram(c.src)
		if err != nil {
			t.Fatalf("parse %q: %v", c.src, err)
		}
		expr, ok := prog.Stmts[0].(*ExprStmt)
		if !ok {
			t.Fatalf("%q did not parse to one expression statement", c.src)
		}
		// The AST-level folder.
		got, ok := stringConst(expr.Expr, nil)
		if !ok || got != c.want {
			t.Errorf("stringConst(%s) = (%q, %v), want (%q, true)", c.src, got, ok, c.want)
		}
		// The interpreter's str(), which is the text this must agree with.
		prog1, err := parseProgram(c.src)
		if err != nil {
			t.Fatalf("parse %q: %v", c.src, err)
		}
		ev := NewEvaluator()
		v, err := ev.EvalProgram(prog1)
		if err != nil {
			t.Fatalf("EvalProgram %q: %v", c.src, err)
		}
		if text := strings.Trim(ev.Repr(v), `"`); text != c.want {
			t.Errorf("interpreter %s = %q, want %q", c.src, text, c.want)
		}
		// The codegen's string-value folder, through a module that uses it. The
		// invariant is not *how* the text is printed (a folded constant prints with
		// printf("%s", i8* @.strN), a computed one through rt_print_str) but that the
		// value reaches the call as a pointer: `i32 @.` anywhere in a module is the
		// constant-in-value-position shape llc rejects.
		res, err := Compile("print(" + c.src + ")\n")
		if err != nil {
			t.Fatalf("Compile print(%s): %v", c.src, err)
		}
		for _, line := range strings.Split(res.IR, "\n") {
			if strings.Contains(line, "i32 @.") {
				t.Errorf("print(%s) put a global in an i32 position: %s", c.src, strings.TrimSpace(line))
			}
		}
		// The text the compiled program prints is decided by the codegen's own fold, so
		// check the bytes it emitted, not just the call shape: a fold that still says
		// "0" prints 0 and this fails.
		if want := `c"` + c.want; !strings.Contains(res.IR, want) {
			t.Errorf("print(%s) did not fold to the text %q (no %s global in the module)", c.src, c.want, want)
		}
		if !strings.Contains(res.IR, "i8* @.str") && !strings.Contains(res.IR, "@rt_print_str(") {
			t.Errorf("print(%s) printed no string at all: %s", c.src, irLinesContaining(res.IR, "printf"))
		}
	}
}

func TestStrNoneCompilesAndStoresAValue(t *testing.T) {
	// The regression in one line: `s = str(None)` used to emit
	// `store i32 @.str1, i32* %_s`, which llc rejects with "global variable reference
	// must have pointer type" — turning a print statement into exit code 2.
	for _, src := range []string{
		"s = str(None)\nprint(s)\n",
		"q = str(\"x\")\nprint(q)\n",
		"w = str(42)\nprint(w)\nprint(len(w))\n",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("Compile %q: %v", src, err)
		}
		for _, line := range strings.Split(res.IR, "\n") {
			if strings.Contains(line, "store i32 @") {
				t.Fatalf("%q stores a global in value position: %s", src, strings.TrimSpace(line))
			}
		}
	}
}

func irLinesContaining(ir, needle string) []string {
	var out []string
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, strings.TrimSpace(line))
		}
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}
