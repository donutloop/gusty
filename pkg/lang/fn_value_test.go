package lang

// A declared name is not a variable, and a function is not a number (roadmap Gap R.150, Gap R.151,
// Gap R.168, ADR 0283).
//
// `f + 1`, `xs = [f]`, `str(f)` and `print(lambda x: x)` left the compiler through **exit 2** — `llc-20`
// rejecting the module for an ordinary program — because a name a `def` declared was read as if it owned
// a slot: `%_f.ld1 = load i32, i32* %_f`, for a slot nothing ever allocated (`def` emits a global
// `@gy_f` and allocates nothing). `print(lambda x: x)` went further and wrote the closure's function
// *global* into a printf operand: `printf(…, i32 lambda_0)`. Both are the instruction family ADR 0271
// deleted for containers, and both are measured here against `python3`.
//
// The second table is the ladder's other direction, and the reason it is in the same file: binding a
// `def`'d name so that reading it stops being a NameError also routed `f(1, 2)` into the closure call
// road, which evaluates its arguments and pads or drops them without asking — `print(f(1, 2))` answered
// `2` at exit 0 where CPython raises. A refusal may become an answer and a wrong number may become a
// refusal; a working answer must never become a wrong number.

import (
	"strings"
	"testing"
)

// TestADeclaredNameUsedAsAValueNeverReachesLLVM is the exit-2 table. Every row must leave through the
// front end (exit class 1), never through `llc`: an error naming the pass is the pass saying "I have no
// value for this", while an `llc` rejection is the compiler's own bug (ADR 0166).
func TestADeclaredNameUsedAsAValueNeverReachesLLVM(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src string }{
		{"a function name in arithmetic", head + "print(f + 1)\n"},
		{"a function name on the left", head + "print(1 + f)\n"},
		{"a function name in a list", head + "xs = [f]\nprint(len(xs))\n"},
		{"a function name passed to str()", head + "print(str(f))\n"},
		{"a function name as a print argument", head + "print(f)\n"},
		{"a function name as a call argument", head + "def g(v):\n    return 1\n\nprint(g(f))\n"},
		{"a function name compared", head + "print(1 if f == 1 else 0)\n"},
		{"a function name in a dict", head + "d = {1: f}\nprint(len(d))\n"},
		{"a module name in arithmetic", "import math\n\nprint(math + 1)\n"},
		{"a module name in a list", "import math\n\nxs = [math]\nprint(len(xs))\n"},
		// A lambda is declared too: it has no value until a call site binds one.
		{"a lambda printed", "print(lambda x: x)\n"},
		{"a lambda in arithmetic", "print((lambda x: x) + 1)\n"},
		{"a lambda in a list", "xs = [lambda x: x]\nprint(len(xs))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				if strings.Contains(err.Error(), "llc") || strings.Contains(err.Error(), "use of undefined value") {
					t.Fatalf("the refusal arrived from `llc`, which the exit-code contract reserves for a compiler bug (ADR 0166): %v\nsrc: %s", err, tc.src)
				}
				if !strings.Contains(err.Error(), "codegen:") {
					t.Errorf("the refusal must be a front-end diagnostic, got %v", err)
				}
				return
			}
			// It compiled: the module must at least verify, and must not carry a load of a slot that
			// was never allocated — the exact instruction that made these rows exit 2.
			if bad := undefinedSlotLoads(res.IR, "f", "math"); bad != "" {
				t.Fatalf("the module reads a slot no `def`/`import` allocates (%s) — this is what llc-20 rejected (Gap R.150):\n%s", bad, firstLines(res.IR, 6))
			}
			if out := runIR(t, res.IR); out != "" {
				t.Logf("compiled and printed %q", out)
			}
		})
	}
}

// undefinedSlotLoads finds `load …, i32* %_<name>` for a name that is declared rather than assigned:
// the instruction the front end must never emit.
func undefinedSlotLoads(ir, name string, extra ...string) string {
	for _, n := range append([]string{name}, extra...) {
		if strings.Contains(ir, "load i32, i32* %_"+n) || strings.Contains(ir, "load double, double* %_"+n) {
			return "load of %_" + n
		}
	}
	return ""
}

func firstLines(ir string, n int) string {
	out := strings.Join(strings.Split(ir, "\n")[:min(n, len(strings.Split(ir, "\n")))], "\n")
	return out
}

// TestTheNumericDoorNamesAFunctionTheWayTheReferenceDoes is Gap R.151's own table: the negation and
// `abs` roads raise CPython's sentence with CPython's word, on both engines, rather than reaching the
// arithmetic the door exists to guard.
func TestTheNumericDoorNamesAFunctionTheWayTheReferenceDoes(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src, class, message string }{
		{"negation of a def'd name", head + "print(-f)\n", "TypeError", "bad operand type for unary -: 'function'"},
		{"abs of a def'd name", head + "print(abs(f))\n", "TypeError", "bad operand type for abs(): 'function'"},
		{"negation of a lambda", "print(-(lambda x: x))\n", "TypeError", "bad operand type for unary -: 'function'"},
		{"abs of a lambda", "print(abs(lambda x: x))\n", "TypeError", "bad operand type for abs(): 'function'"},
		{"negation of a module", "import math\n\nprint(-math)\n", "TypeError", "bad operand type for unary -: 'module'"},
		{"abs of a module", "import math\n\nprint(abs(math))\n", "TypeError", "bad operand type for abs(): 'module'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter raised %q, want %q (msg %q) — the wrong class breaks `except %s:` (ADR 0211, ADR 0228)", ee.ExnType, tc.class, ee.ExnMsg, tc.class)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q\nsrc: %s", ee.ExnMsg, tc.message, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle only traps on: %v\n%s", err, tc.src)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled message missing %q (one word for both engines):\n%s", tc.message, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the reference traps on:\n%s", out)
			}
			if strings.Contains(out, "COMPILER BUG") || strings.Contains(out, "LLVM ERROR") {
				t.Errorf("the trap became a toolchain failure:\n%s", out)
			}
		})
	}
}

// TestADeclaredNameReadIsNotANameErrorOnTheInterpreter is the interpreter half: a program that just
// declared `f` does not get `NameError: name 'f' is not defined` for reading it. That wrong *class* is
// the catchability defect ADR 0211 and ADR 0228 count as a misclassing — `except TypeError:` matched
// neither arm, because NameError matched neither either.
func TestADeclaredNameReadIsNotANameErrorOnTheInterpreter(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src, want string }{
		// The reference prints a function object; this language prints the object it has, and the point
		// pinned here is that it prints something rather than raising the wrong class.
		{"a lambda bound and printed answers a name for the object", "g = lambda x: x * 2\nprint(g)\n", "<closure>\n"},
		{"a def'd name printed answers a name for the object", head + "print(f)\n", "<closure>\n"},
		// A name handed on is callable at the call site — CPython's `42`, the row that must stay right.
		{"a def'd name assigned then called", head + "g = f\nprint(g(21))\n", "42\n"},
		{"a def'd name as a list element counts", head + "xs = [f]\nprint(len(xs))\n", "1\n"},
		// Reading it is not a NameError any more, and the raise that follows is CPython's class.
		{"abs of a def'd name raises TypeError, catchable", head + "try:\n    abs(f)\n    print('no raise')\nexcept TypeError:\n    print('caught')\n", "caught\n"},
		{"negating a def'd name raises TypeError, catchable", head + "try:\n    -f\n    print('no raise')\nexcept TypeError:\n    print('caught')\n", "caught\n"},
		// (A name the program never bound is caught earlier than a trap in this language: the checker
		// refuses it — `verify: undefined name "nope"` — which is why it is not in this table. The row
		// this one replaces expected a runtime NameError and the checker, correctly, got there first.)
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// TestACalledDefNameKeepsTheCheckedRoadIsTheLadderRule'sOwnRow. Binding the name made it *look* like
// the lambda case, and routing calls on the value would have sent `f(1, 2)` to the closure road, which
// never counts arguments: `print(f(1, 2))` answered `2` and `print(f())` answered `0`, both at exit 0.
// Those two wrong numbers are what this row fails on if the roads are ever reordered again.
func TestACalledDefNameKeepsTheCheckedRoadIsTheLadderRule(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src, wantSub string }{
		{"too many arguments is still an error", head + "print(f(1, 2))\n", "argument"},
		{"too few arguments is still an error", head + "print(f())\n", "argument"},

		{"a function of two parameters called with one", "def g(a, b):\n    return a + b\n\nprint(g(1))\n", "argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Arity is answered by the checker in this language (`verify: function "f" accepts 1
			// argument, got more`), not by a run-time trap, so the contract to hold is that *some*
			// stage refuses — the failure mode this row guards is silence: `2` and `0` at exit 0.
			_, _, err := EvalExpr(tc.src)
			if err == nil {
				t.Fatalf("the program ran to completion; the arity question was dropped and this is the shape that printed `2` and `0` at exit 0 (Gap R.168)\nsrc: %s", tc.src)
			}
			msg := err.Error()
			// Two stages may answer this, and they use different words: the checker says
			// `function "f" expects 1 argument, got 0`, the interpreter's own call road says
			// `missing argument x`. Either is a refusal; the failure this row guards is neither — a
			// program that runs and prints a number (Gap R.168's measured `2` and `0`).
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("the program reported %q, which does not contain %q — the declared road lost to the un-checked closure road, and the arity question was dropped (Gap R.168)\nsrc: %s", msg, tc.wantSub, tc.src)
			}
		})
	}
	// The working call beside them: the checked road must not have become a refusal either.
	if out := captureStdout(t, head+"print(f(21))\n"); out != "42\n" {
		t.Errorf("the right call stopped answering: stdout %q, want \"42\" — binding the name must not break a call (Gap R.168)", out)
	}
}

// TestAParameterHoldingACallableIsNotRefused is the narrowing: the guard names a *declaration*, so a
// body that receives a callable keeps the meaning the program gave it. Without this the fix would be a
// ban on functions as arguments.
func TestAParameterHoldingACallableIsNotRefused(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a lambda called through a parameter", "def twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(twice(lambda x: x * 3, 2))\n", "12\n"},
		{"a lambda bound then called", "g = lambda x: x * 3\nprint(g(4))\n", "12\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}
