package lang

import (
	"strings"
	"testing"
)

// Artifact-level coverage for roadmap Gap R.42 / L11.8 (ADR 0224): a string *value* is an index
// into the runtime @str_tab, and the address of a private global belongs only to the contexts that
// ask for bytes (printf and the compile-time folds). Before this, `value()` returned `@.str7` for a
// literal, so any ordinary program that compared or returned a string emitted an i32 holding a
// global and llc rejected the module -- exit 2, blaming the compiler for a source program.

func TestStringLiteralInValueContextIsInterned(t *testing.T) {
	res, err := Compile("x = \"hi\"\nprint(1 if x == \"hi\" else 0)\n")
	if err != nil {
		t.Fatalf("comparing a string with a literal must compile: %v", err)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
	if !strings.Contains(res.IR, "call i32 @rt_str_intern2") {
		t.Fatalf("the literal is not interned; the comparison would be an icmp against a global:\n%s", res.IR)
	}
	for _, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "icmp eq i32") && strings.Contains(ln, "@.str") {
			t.Fatalf("a string comparison still mixes an index with a global address:\n%s", ln)
		}
	}
}

func TestFoldedStringResultsAreInterned(t *testing.T) {
	// Folded paths (concatenation of constants, a string-method fold) are string *values* too;
	// they used to hand back the global, and `x.upper() == "HI"` was an invalid module.
	for _, src := range []string{
		"a = \"ab\"\nb = \"c\"\nprint(1 if a + b == \"abc\" else 0)\n",
		"x = \"hi\"\nprint(1 if x.upper() == \"HI\" else 0)\n",
		"s = \"abcdef\"\nprint(1 if s[1:3] == \"bc\" else 0)\n",
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("%q refused: %v", src, err)
		}
		if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
			t.Fatalf("%q does not verify: %v", src, verr)
		}
	}
}

func TestFStringInterpolatesAStringThroughItsText(t *testing.T) {
	res, err := Compile("n = \"world\"\nprint(f\"hi {n}\")\n")
	if err != nil {
		t.Fatalf("an f-string over a string variable must compile: %v", err)
	}
	if !strings.Contains(res.IR, "call i8* @rt_str_ptr(i32") {
		t.Fatalf("the interpolated part is not read back to text; printf would print the index:\n%s", res.IR)
	}
}

func TestMethodStringResultIsKnownToItsCallers(t *testing.T) {
	src := "class Dog:\n    def sound(self) -> str:\n        return \"woof\"\n\nprint(Dog().sound())\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("a method returning a string must compile: %v", err)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
	if !strings.Contains(res.IR, "ret i32 %t") || strings.Contains(res.IR, "ret i32 @.str") {
		t.Fatalf("the method returned the literal's address instead of its interned index:\n%s", res.IR)
	}
	// Printing it must reach the text path, not printf's %d: that is what made `Dog().sound()`
	// answer `0` -- the index -- where the interpreter and CPython answer `woof`.
	if !strings.Contains(res.IR, "rt_str_ptr") {
		t.Fatalf("printing a method's string result did not go through the text lookup:\n%s", res.IR)
	}
}

// A function whose answer is `str(…)` is a string-returning function too, and its callers must be told.
// The callee already asked the statement-level rendering door and returned the interned index, so nothing
// about the body was wrong: `print(g())` asked `printf` with `%d` and answered `0` — the index — where
// CPython and the interpreter answer `42` (roadmap L11.2, Gap R.163, ADR 0281). It is
// TestMethodStringResultIsKnownToItsCallers' rule, one door earlier: the same index, reached through a
// builtin call instead of a literal.
func TestStrResultOfAFuncIsKnownToItsCallers(t *testing.T) {
	for _, tc := range []struct {
		name, src, want, irWant string
	}{
		{
			"the row itself: return str(42)",
			"def g():\n    return str(42)\n\nprint(g())\n", "42\n", "rt_str_ptr",
		},
		{
			"a name the body cannot read at compile time",
			"x = 7\n\ndef g():\n    return str(x)\n\nprint(g())\n", "7\n", "rt_str_ptr",
		},
		{
			"a double keeps the round-tripping form print would use",
			"def g():\n    return str(2.5)\n\nprint(g())\n", "2.5\n", "rt_str_ptr",
		},
		{
			"repr is the same pair, quoted",
			"def g():\n    return repr(42)\n\nprint(g())\n", "42\n", "rt_str_ptr",
		},
		{
			// A container is the shape ADR 0258 refused rather than rendered: `str([1, 2])` used to answer
			// `0`, and a function returning it must not answer the index either.
			"a container asks its own object",
			"def g():\n    return str([1, 2])\n\nprint(g())\n", "[1, 2]\n", "rt_str_ptr",
		},
		{
			"None writes None, not 0",
			"def g():\n    return str(None)\n\nprint(g())\n", "None\n", "rt_str_ptr",
		},
		{
			// The answer is a text in the *value* positions too, not only under `print`: this is what makes
			// the verdict worth having, and each line is a different consumer of the same index.
			"bound, measured, and asked for a method",
			"def g():\n    return str(42)\n\ns = g()\nprint(s)\nprint(len(g()))\nprint(g().upper())\n",
			"42\n2\n42\n", "rt_str_ptr",
		},
		{
			// Gap R.6's rule survives the widening: a program that defines `str` itself gets its own
			// function, not the builtin's meaning, and the answer stays a number.
			"a program that took the name `str` is not the builtin",
			"def str(x):\n    return x + 7\n\ndef g():\n    return str(42)\n\nprint(g())\n", "49\n", "",
		},
		{
			"a number-returning function keeps its number answer",
			"def n():\n    return 42\n\nprint(n())\n", "42\n", "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compiled leg refused a program the oracle prints (%v):\n%s", err, tc.src)
			}
			if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
				t.Fatalf("the emitted module does not verify: %v", verr)
			}
			if tc.irWant != "" && !strings.Contains(res.IR, tc.irWant) {
				t.Fatalf("printing the answer did not go through the text lookup (%q); printf's %%d would print the index:\n%s", tc.irWant, res.IR)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

func TestCallArgumentInterningDoesNotSplitAnInstruction(t *testing.T) {
	// The interning call is an instruction of its own; it must be emitted before the call line it
	// feeds, or the operand list is torn in half (`call i32 @f(i32 %x  %t = call ...`).
	res, err := Compile("class G:\n    def greet(self, who: str) -> str:\n        return \"hi\"\n\nprint(G().greet(\"x\"))\n")
	if err != nil {
		t.Fatalf("a method call with a string argument must compile: %v", err)
	}
	if _, verr := VerifyModuleIR(res.IR, 0); verr != nil {
		t.Fatalf("the emitted module does not verify: %v", verr)
	}
	for _, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "= call i32 @gy_G_greet(") && strings.Contains(ln, "rt_str_intern2") {
			t.Fatalf("an argument's intern call was emitted inside the operand list:\n%s", ln)
		}
	}
}

func TestStringAttrReadPrintsText(t *testing.T) {
	src := "class C:\n    def __init__(self):\n        self.w = \"hi\"\n\nprint(C().w)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("reading a string attribute must compile: %v", err)
	}
	if !strings.Contains(res.IR, "rt_str_ptr") {
		t.Fatalf("the attribute read printed as a number; the class never learned the attr holds a string:\n%s", res.IR)
	}
}
