package lang

// Gap R.131 / ADR 0287 — a builtin called with no argument is asked two different questions, and the
// interpreter used to ask NEITHER.
//
// `int()`, `float()`, `bool()` and `str()` are CONSTRUCTORS: the reference answers `0`, `0.0`, `False`
// and the empty text. `ord()`, `chr()`, `abs()` and `repr()` are NOT: the reference raises
// `TypeError: <name>() takes exactly one argument (0 given)`. All eight cases used to reach
// `n.Args[0]` first, so both classes died the same way — a Go runtime panic,
// `index out of range [0] with length 0`, and **exit 2**, the code ADR 0166 reserves for a compiler bug.
//
// The tables below are split on the same line the reference draws, and each row is pinned to the
// reference's own answer or its own sentence, taken from python3 3.12.3 rather than paraphrased: ADR
// 0215 makes trap wording part of the contract, so "abs expects 1 argument" is as wrong as a wrong
// number even though it exits with the right code.

import (
	"strings"
	"testing"
)

// TestConstructorBuiltinsAnswerWithoutAnArgument is the exit-2 half: programs the reference runs, which
// must exit 0 with the reference's answer on BOTH engines. Before ADR 0287 every row here panicked.
func TestConstructorBuiltinsAnswerWithoutAnArgument(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"int of nothing", "print(int())\n", "0\n"},
		{"float of nothing", "print(float())\n", "0.0\n"},
		{"bool of nothing", "print(bool())\n", "False\n"},
		{"str of nothing", "print(str())\n", "\n"},
		// A constructor is still a constructor when its result is used rather than printed.
		{"int of nothing measured", "print(int() + 1)\n", "1\n"},
		{"float of nothing in an expression", "print(float() < 1.0)\n", "True\n"},
		{"bool of nothing in a test", "print(1 if bool() else 2)\n", "2\n"},
		// The one-argument spellings of the same four names, kept green beside the new ones.
		{"int of a text", "print(int(\"42\"))\n", "42\n"},
		{"float of a number", "print(float(3))\n", "3.0\n"},
		{"bool of a verdict's truthiness", "print(bool(1))\n", "True\n"},
		{"bool of an empty text", "print(bool(\"\"))\n", "False\n"},
		{"bool of a filled text", "print(bool(\"x\"))\n", "True\n"},
		// A program that took the name owns it (Gap R.6's rule), constructor or not.
		{"the program owns bool", "def bool(x):\n    return 7\n\nprint(bool(0))\n", "7\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			got := captureStdout(t, r.src)
			if got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			compiled := compiledOut(t, r.src)
			if compiled != r.want {
				t.Errorf("--aot printed %q, want %q", compiled, r.want)
			}
		})
	}
}

// TestOneArgumentBuiltinsRaiseTheReferencesSentence is the other half of the same missing check. These are
// programs the reference REJECTS, so the honest answer is a trap carrying the reference's own wording.
func TestOneArgumentBuiltinsRaiseTheReferencesSentence(t *testing.T) {
	rows := []struct {
		name string
		src  string
		ty   string
		msg  string
	}{
		{"ord of nothing", "print(ord())\n", "TypeError", "ord() takes exactly one argument (0 given)"},
		{"chr of nothing", "print(chr())\n", "TypeError", "chr() takes exactly one argument (0 given)"},
		{"abs of nothing", "print(abs())\n", "TypeError", "abs() takes exactly one argument (0 given)"},
		{"repr of nothing", "print(repr())\n", "TypeError", "repr() takes exactly one argument (0 given)"},
		// Too many arguments is the same sentence with the count the call actually had.
		{"abs of two", "print(abs(1, 2))\n", "TypeError", "abs() takes exactly one argument (2 given)"},
		{"ord of two", "print(ord(\"a\", \"b\"))\n", "TypeError", "ord() takes exactly one argument (2 given)"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			err := trapRun(t, r.src)
			if err == nil {
				t.Fatalf("the program answered where the reference raises")
			}
			if err.ExnType != r.ty {
				t.Errorf("raised %s, want %s", err.ExnType, r.ty)
			}
			if err.ExnMsg != r.msg {
				t.Errorf("message is %q, want the reference's own %q (ADR 0215: trap wording is a contract)", err.ExnMsg, r.msg)
			}
		})
	}
}

// TestBuiltinWithoutAnArgumentNeverPanics is the ADR 0166 half, asserted structurally rather than by
// exit code: a Go panic inside the evaluator or the codegen is the compiler's bug, and it must not be
// reachable from any of these spellings — including the ones the compiled backend still declines, which
// decline in words.
func TestBuiltinWithoutAnArgumentNeverPanics(t *testing.T) {
	srcs := []string{
		"print(int())\n", "print(float())\n", "print(bool())\n", "print(str())\n",
		"print(ord())\n", "print(chr())\n", "print(abs())\n", "print(repr())\n",
		"print(len())\n", "print(hex())\n", "print(oct())\n", "print(bin())\n",
		"x = int()\nprint(x)\n", "print(int() == 0)\n", "print(bool() == False)\n",
	}
	for _, src := range srcs {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			// The interpreter must not panic. A panic escapes as a Go runtime error; a trap is an answer.
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("interpreter panicked on %q: %v (ADR 0166 exit 2)", src, p)
					}
				}()
				_, _, _ = EvalExpr(src)
			}()
			// The compiler must not panic either. Refusing is allowed; dying is not.
			func() {
				defer func() {
					if p := recover(); p != nil {
						t.Fatalf("codegen panicked on %q: %v (ADR 0166 exit 2)", src, p)
					}
				}()
				if _, err := Compile(src); err != nil {
					// A refusal must read as a refusal: a sentence, not a Go runtime message.
					if strings.Contains(err.Error(), "index out of range") || strings.Contains(err.Error(), "panic:") {
						t.Fatalf("the refusal leaked a Go panic: %v", err)
					}
				}
			}()
		})
	}
}

// TestReprIsNotStrWithNoArgument pins the asymmetry this cycle measured rather than assumed. str() and
// repr() share one renderer, one table and one refusal message, so the natural implementation gives them
// the same arity — and the reference does not.
func TestReprIsNotStrWithNoArgument(t *testing.T) {
	if got := captureStdout(t, "print(str())\n"); got != "\n" {
		t.Fatalf("str() printed %q, want the empty text", got)
	}
	err := trapRun(t, "print(repr())\n")
	if err == nil {
		t.Fatalf("repr() answered where the reference raises TypeError")
	}
	if err.ExnType != "TypeError" || err.ExnMsg != "repr() takes exactly one argument (0 given)" {
		t.Fatalf("repr() raised %s: %s, want the reference's TypeError for a missing argument", err.ExnType, err.ExnMsg)
	}
}

// TestCompiledBoolAnswersAVerdictNotAText guards the specific wrong-answer bug this cycle produced on
// the way: selecting between the interned strings "True"/"False" and handing the index to rt_print_bool,
// which asks only whether its word is zero. The pair of answers came out INVERTED.
func TestCompiledBoolAnswersAVerdictNotAText(t *testing.T) {
	rows := []struct {
		src  string
		want string
	}{
		{"print(bool(0))\n", "False\n"},
		{"print(bool(1))\n", "True\n"},
		{"print(bool(2))\n", "True\n"},
		{"print(bool(-1))\n", "True\n"},
		{"print(bool(\"\"))\n", "False\n"},
		{"print(bool(\"x\"))\n", "True\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q", got, r.want)
			}
		})
	}
}

// TestBoolOfAContainerRefusesRatherThanComparingAHandle is the half of bool() the compiled backend cannot
// answer, pinned so it stays a refusal and cannot quietly become "non-empty for every handle". The first
// version compared the operand's word against zero: for a list literal that word is a GLOBAL and llc-20
// rejected the module (`icmp ne i32 @.lst1, 0`), and for any allocated container the answer would have
// been True whatever the contents — including bool([]).
func TestBoolOfAContainerRefusesRatherThanComparingAHandle(t *testing.T) {
	for _, src := range []string{
		"print(bool([1]))\n",
		"print(bool([]))\n",
		"print(bool([1, 2, 3]))\n",
		"print(bool({\"a\": 1}))\n",
	} {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			// The interpreter answers the reference, which is the half that can be answered today.
			want := captureStdout(t, src)
			if want == "" {
				t.Fatalf("the interpreter printed nothing for %q", src)
			}
			res, err := Compile(src)
			if err == nil {
				out := runIR(t, res.IR)
				if out != want {
					t.Errorf("--aot answered %q where the reference answers %q, and the only honest alternative is a refusal", out, want)
				}
				return
			}
			for _, banned := range []string{"index out of range", "global variable reference", "@.lst"} {
				if strings.Contains(err.Error(), banned) {
					t.Errorf("refusal leaked `%s` — the compiler should name the missing feature, not the module it failed to build: %v", banned, err)
				}
			}
			if !strings.Contains(err.Error(), "bool()") && !strings.Contains(err.Error(), "truthiness") {
				t.Errorf("refusal %q does not name what is missing (Gap R.38)", err)
			}
		})
	}
}

// TestFloatConstructorTravelsAsADouble is the second wrong-answer bug this cycle produced: answering the
// call site with a textual double put `sitofp` on a value that was already one, and llc-20 rejected the
// module. The fold is what makes the emitted shape indistinguishable from the literal.
func TestFloatConstructorTravelsAsADouble(t *testing.T) {
	res, err := Compile("print(float())\n")
	if err != nil {
		t.Fatalf("float() refused: %v", err)
	}
	if strings.Contains(res.IR, "sitofp i32 0.0") || strings.Contains(res.IR, "sitofp i32 0.0e+00") {
		t.Fatalf("the float constructor widened a double it was already given:\n%s", res.IR)
	}
	if got := runIR(t, res.IR); got != "0.0\n" {
		t.Errorf("float() printed %q, want 0.0", got)
	}
	// The literal and the constructor must not be tellable apart by their IR.
	lit, lerr := Compile("print(0.0)\n")
	if lerr != nil {
		t.Fatalf("0.0 refused: %v", lerr)
	}
	if !strings.Contains(res.IR, "rt_fmt_double") || !strings.Contains(lit.IR, "rt_fmt_double") {
		t.Fatalf("one of the two did not take the float renderer")
	}
}

// TestBuiltinArityIsAskedBeforeTheOperandNamesTheProgram is the Gap R.38 half: a refusal or a trap
// caused by a missing argument must say which builtin, in the reference's words, rather than reaching
// for an operand and letting the runtime describe the program.
func TestBuiltinArityIsAskedBeforeTheOperandNamesTheProgram(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(abs())\n", "abs()"},
		{"print(ord())\n", "ord()"},
		{"print(chr())\n", "chr()"},
		{"print(repr())\n", "repr()"},
	} {
		tc := tc
		t.Run(tc.src, func(t *testing.T) {
			err := trapRun(t, tc.src)
			if err == nil {
				t.Fatalf("%s answered where the reference raises", tc.src)
			}
			if !strings.Contains(err.ExnMsg, tc.want) {
				t.Errorf("message %q does not name the builtin %s the program called", err.ExnMsg, tc.want)
			}
			if strings.Contains(err.ExnMsg, "index out of range") {
				t.Errorf("the Go runtime described the program instead of the compiler: %s", err.ExnMsg)
			}
		})
	}
}
