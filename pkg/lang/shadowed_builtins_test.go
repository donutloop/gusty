package lang

import (
	"strings"
	"testing"
)

// A built-in call name is a name, not a keyword: `def str(x): ...` then `str(1)` means what
// the program's definition says. The interpreter and CPython both resolve it that way; the
// compiled path read such a call by *name* through the built-in's meaning, so `float(1)` folded
// to a conversion and `str(1)` was emitted as the user's call and then used as the built-in's
// string result (roadmap Gap R.6, ADR 0199).

func mustShadowIR(t *testing.T, src string) string {
	t.Helper()
	ir, err := GenerateIR(parseOrFatal(t, src))
	if err != nil {
		t.Fatalf("GenerateIR: %v", err)
	}
	return ir
}

// TestProgramDefinitionWinsOverTheBuiltinName is the IR contract: for a name the program
// defines, the module must call the program's function — not fold the call as the built-in.
func TestProgramDefinitionWinsOverTheBuiltinName(t *testing.T) {
	for _, name := range []string{"float", "str", "chr", "int", "ord", "round", "abs", "sum", "len", "min", "max", "sqrt", "floor", "ceil"} {
		src := "def " + name + "(x):\n    return x + 7\n\nprint(" + name + "(1))\n"
		ir := mustShadowIR(t, src)
		if !strings.Contains(ir, "define i32 @gy_"+name+"(i32 %p0)") {
			t.Errorf("%s: the program's function is not defined:\n%s", name, ir)
		}
		if !strings.Contains(ir, "call i32 @gy_"+name+"(") {
			t.Errorf("%s: the call was resolved through the built-in instead of the program's definition:\n%s", name, ir)
		}
	}
}

// TestShadowedFloatCallIsNotFoldedAsConversion is the measured silent-wrong-answer: `float(1)`
// folded to the conversion, so the program printed 1.0 while the interpreter printed 8.
func TestShadowedFloatCallIsNotFoldedAsConversion(t *testing.T) {
	ir := mustShadowIR(t, "def float(x):\n    return x + 7\n\nprint(float(1) + 0.5)\n")
	if !strings.Contains(ir, "call i32 @gy_float(") {
		t.Errorf("the call to the program's own float is missing:\n%s", ir)
	}
	// Its i32 result has to be lifted before it can join float arithmetic — an i32 operand in
	// an fadd is what llc rejected once the fold was removed but the lift was not added.
	if strings.Count(ir, "sitofp") < 1 {
		t.Errorf("the int result of the call is never lifted to double:\n%s", ir)
	}
}

// TestShadowedStrIsPrintedAsWhatItReturns: `str` used to be emitted as the user's call and then
// printed with the built-in's `%s`, which llc rejected.
func TestShadowedStrIsPrintedAsWhatItReturns(t *testing.T) {
	ir := mustShadowIR(t, "def str(x):\n    return x + 7\n\nprint(str(1))\n")
	if !strings.Contains(ir, "call i32 @gy_str(") {
		t.Errorf("the call to the program's own str is missing:\n%s", ir)
	}
	format := printFormatFor(t, ir, "call i32 @gy_str(")
	if !strings.Contains(format, "%d") {
		t.Errorf("a program-defined str returning an int must print as a number, got format %q:\n%s", format, ir)
	}
	if strings.Contains(format, "%s") {
		t.Errorf("the built-in's string reading of str() leaked into the print, format %q:\n%s", format, ir)
	}
}

// TestProgramFloatFunctionKeepsItsShape guards the precedence inside the guard: a function the
// program defines that *does* return a float is a fact about the program, and must still be
// treated as float — this is the regression that programs/floatfn.gy caught.
func TestProgramFloatFunctionKeepsItsShape(t *testing.T) {
	ir := mustShadowIR(t, "def half(x):\n    return x / 2.0\n\nprint(half(5.0))\n")
	if !strings.Contains(ir, "fdiv double") {
		t.Errorf("a program-defined float function must keep float arithmetic:\n%s", ir)
	}
}

// TestBuiltinsStillFoldWhenNobodyShadowedThem: the guards must not cost the ordinary program
// its constant folding — that would trade a rare bug for a common slowdown.
func TestBuiltinsStillFoldWhenNobodyShadowedThem(t *testing.T) {
	ir := mustShadowIR(t, "print(str(42))\n")
	if strings.Contains(ir, "call i32 @gy_str(") {
		t.Errorf("an unshadowed str() should still fold to its constant:\n%s", ir)
	}
	ir2 := mustShadowIR(t, "print(abs(-3))\n")
	if strings.Contains(ir2, "call i32 @gy_abs(") {
		t.Errorf("an unshadowed abs() should still fold:\n%s", ir2)
	}
}

// TestShadowedBuiltinInsideAnotherExpression covers the fold paths that run on sub-expressions.
func TestShadowedBuiltinInsideAnotherExpression(t *testing.T) {
	ir := mustShadowIR(t, "def len(x):\n    return 3\n\nitems = [1, 2, 3, 4]\nprint(len(items))\n")
	if !strings.Contains(ir, "call i32 @gy_len(") {
		t.Errorf("a shadowed len used on a container must be the program's call:\n%s", ir)
	}
	// The number that reaches print() is the program's answer, not the container's length:
	// 3 from the def, where the built-in reading would have folded 4 from the list.
	if format := printFormatFor(t, ir, "call i32 @gy_len("); !strings.Contains(format, "%d") {
		t.Errorf("the shadowed len's result is not printed as the number it returns: %q\n%s", format, ir)
	}
}

// TestShadowingIsPerProgramNotGlobal: shadowing in one program must not change another. The
// guard is program state, so this checks that nothing leaked into a constant.
func TestShadowingIsPerProgramNotGlobal(t *testing.T) {
	first := mustShadowIR(t, "def float(x):\n    return x + 7\n\nprint(float(1))\n")
	if !strings.Contains(first, "call i32 @gy_float(") {
		t.Fatalf("shadowed float was not called:\n%s", first)
	}
	second := mustShadowIR(t, "x = float(2)\nprint(x)\n")
	if strings.Contains(second, "@gy_float") {
		t.Errorf("the second program defines no float, yet the guard followed it:\n%s", second)
	}
}

// printFormatFor finds the printf that consumes the register produced by the first line
// containing want, and returns the format string it was given. Assertions about *how a value
// prints* have to follow the register and then the format global, because a module contains
// many formats — including a `%s` inside the GC report's snprintf, which is what a naive
// whole-module search used to find instead.
func printFormatFor(t *testing.T, ir, want string) string {
	t.Helper()
	at := strings.Index(ir, want)
	if at < 0 {
		t.Fatalf("no line containing %q in the module", want)
	}
	line := strings.SplitN(ir[at:], "\n", 2)[0]
	fields := strings.Fields(strings.TrimSpace(line[strings.LastIndex(line, "=")+1:]))
	if len(fields) == 0 {
		t.Fatalf("no result register on %q", line)
	}
	reg := fields[0]
	body := ir[at:]
	if end := strings.Index(body, "\n}"); end > 0 {
		body = body[:end] // the same function body only
	}
	for _, ln := range strings.Split(body, "\n") {
		if !strings.Contains(ln, "@printf(") || !strings.Contains(ln, reg) {
			continue
		}
		name := formatGlobalIn(ln)
		if name == "" {
			return ln
		}
		if fmt := formatText(ir, name); fmt != "" {
			return fmt
		}
		return ln
	}
	t.Fatalf("no printf in the function consumes %s (from %q)", reg, line)
	return ""
}

// formatGlobalIn picks the @.fmtN global a printf line was handed.
func formatGlobalIn(line string) string {
	i := strings.Index(line, "@.fmt")
	if i < 0 {
		return ""
	}
	name := line[i:]
	for j, r := range name {
		if r == ',' || r == ')' || r == ' ' {
			return name[:j]
		}
	}
	return name
}

// formatText reads the bytes of a format global's initializer.
func formatText(ir, global string) string {
	at := strings.Index(ir, global+" = ")
	if at < 0 {
		return ""
	}
	line := strings.SplitN(ir[at:], "\n", 2)[0]
	q := strings.Index(line, "c\"")
	if q < 0 {
		return ""
	}
	rest := line[q+2:]
	if e := strings.Index(rest, "\""); e >= 0 {
		return strings.ReplaceAll(rest[:e], "\\00", "")
	}
	return rest
}
