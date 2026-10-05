package lang

// float_arg_test.go — the (payload, tag) pair handed to a function by an argument that *carries a
// double* (roadmap L11.6's numeric truth, Gap P.1's argument half, ADR 0276).
//
// ADR 0273 opened the pair across the call for one reason: an argument the ordinary numeric road would
// *refuse* (a slot of a container the program built at run time). The gate asked nothing about an
// argument that carries a double, so `twice(2.5)` never reached the door at all — it went down the
// ordinary road, which writes one i32 into the parameter, and printed 4 at exit 0. CPython prints 5.0.
// The same truncation answered a float-state variable handed to a function (Gap R.157, R.158), a default
// the call site never wrote, and a slot of a literal list whose elements hold a double.
//
// Two questions decide it, both answered by the scan and never by the emitting order:
//
//   - does the ordinary road refuse this argument, or answer a double it cannot carry? then the
//     parameter arrives as two words;
//   - what does the body read the *other* parameters as? `def area(w, h): return w * h` called with
//     `area(2.5, 2)` needs a kind for both operands, and `h` has one only if the call sites prove it.
//
// And one rule keeps the answers that already worked: a body emitted under a convention that owns its
// return word — ADR 0274's double, ADR 0174's string index — owns its parameter words too, so the scan
// leaves those functions alone entirely. Every row below is pinned to CPython's answer, and the shapes
// the door withholds are pinned to the answer they had before it.

import (
	"strings"
	"testing"
)

const floatArgTwice = "def twice(v):\n    return v * 2\n\n"

// TestTheFloatArgumentAnswersOnBothBackends is the payoff: every row is a program the compiled leg
// answered with a truncated number at exit 0, now CPython's answer on both engines.
func TestTheFloatArgumentAnswersOnBothBackends(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"the row Gap P.1 filed",
			floatArgTwice + "print(twice(2.5))\n", "5.0\n",
		},
		{
			// One `define`, both conventions: the answer's kind follows the argument, which is why a
			// single `double` parameter could not express this function at all.
			"the same function, an int and a float",
			floatArgTwice + "print(twice(2.5))\nprint(twice(2))\n", "5.0\n4\n",
		},
		{
			"a bool is the number CPython says it is",
			floatArgTwice + "print(twice(True))\n", "2\n",
		},
		{
			// The double arrived from a variable's second binding (ADR 0274's pair), which is the shape
			// Gap R.158 measured: the call was handed the name and the name carried a box.
			"a float-state variable handed to a call",
			"def twice(v):\n    return v * 2\n\nx = 8\nx = 2.5\nprint(twice(x))\n", "5.0\n",
		},
		{
			// The quotient the tagged numeric door answers is an argument like any other: it travels as
			// the pair rather than losing its half to a one-word parameter.
			"a true-division quotient handed to a call",
			"def f(a, b):\n    return b\n\nxs = []\nxs.append(6)\nprint(f(1, xs[0] / 2))\n", "3.0\n",
		},
		{
			// The parameter no call site writes: a default is an argument the caller did not have to
			// spell, and an arity-only scan would never open it.
			"a default is an argument too",
			"def greet(name, times=1.5):\n    return times\n\nprint(greet(\"a\"))\nprint(greet(\"a\", 2))\n", "1.5\n2\n",
		},
		{
			// `w` needs the tag; `h` needs only the evidence that its call sites hand integers, which is
			// what lets the shared arithmetic door name a kind for both operands.
			"a parameter beside a provably-integer one",
			"def area(w, h):\n    return w * h\n\nprint(area(2.5, 2))\nprint(area(3, 4))\n", "5.0\n12\n",
		},
		{
			"a slot of a literal list that holds a double",
			floatArgTwice + "ys = [1, 2.5]\nprint(twice(ys[1]))\nprint(twice(ys[0]))\n", "5.0\n2\n",
		},
		{
			// Gap R.154's shape: the pair-carrying parameter and the ordinary one share one body.
			"a pair parameter beside an ordinary parameter",
			"def shift(a, b=100):\n    return a + b\n\nxs = []\nxs.append([7, 8])\nprint(shift(xs[0][1]))\nprint(shift(xs[0][1], 2))\n",
			"108\n10\n",
		},
		{
			// The augmented body rebinds the parameter before handing it back, and the loop accumulator
			// is the same pair one statement earlier (Gaps R.155, R.157).
			"a body that rebinding adds the double to",
			"def bump(v):\n    v += 0.5\n    return v\n\nprint(bump(1))\n", "1.5\n",
		},
		{
			// The answer direction: the callee's answer is a pair and the caller, whose own convention is
			// a double, lifts it. Printing the payload alone would have read a box handle as a number.
			"a pair answer handed through another function",
			"def g(y):\n    return y * 2\n\ndef f(x):\n    x = x + 1.5\n    return g(x)\n\nprint(f(1.0))\n", "5.0\n",
		},
		// The neighbours that must not move: an integer-only function keeps its integer answers, and a
		// recursive one keeps the fib it always computed.
		{"integers keep answering integers", floatArgTwice + "print(twice(3))\nprint(twice(0))\n", "6\n0\n"},
		{
			"the recursive integer function is untouched",
			"def fib(n):\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(10))\n", "55\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiler refused a program the oracle answers: %v", err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("compiled program printed %q, want %q", out, tc.want)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter printed %q, want %q", out, tc.want)
			}
		})
	}
}

// wordParams counts the value words a `define` line takes, which is the arity question: one per
// parameter, plus one tag word for every parameter the pair road carries.
func wordParams(define string) (payload, tag int) {
	return strings.Count(define, "i32 %p"), strings.Count(define, "i32 %q")
}

// defineLine returns the `define` line a module carries for an emitted symbol, so an arity can be
// asserted without reading the whole module into a failure message.
func defineLine(ir, symbol string) string {
	for _, ln := range strings.Split(ir, "\n") {
		if strings.HasPrefix(ln, "define ") && strings.Contains(ln, "@"+symbol+"(") {
			return ln
		}
	}
	return "(no such define)"
}

// TestTheFloatArgumentDoorNamesTheTruncationItRefuses pins the refusal half of the same door. Where the
// pair cannot be built the program is refused by name, because the alternative is the number the ordinary
// road truncates — Gap P.1's exit-0 `4`.
func TestTheFloatArgumentDoorNamesTheTruncationItRefuses(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// A container element is not a number to box: the element road owns it and this one declines.
			"a container argument keeps its own road",
			"def pack(v):\n    return [v, 1]\n\nprint(pack(2.5))\n",
			"must be integers",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%q compiled; this shape is owed a refusal, not a truncated answer\nsrc: %s", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused without naming the shape: %q does not mention %q", err, tc.want)
			}
			for _, bad := range []string{"LLVM ERROR", "verifier", "Instruction does not dominate", "panic"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
				}
			}
		})
	}
}

// TestTheFloatArgumentDoorLeavesTheRoadsThatAlreadyWorked is the no-regression half of the gate, and the
// reason the scan — not the emitting order — decides. A function emitted under a convention that owns its
// return word owns its parameter words with it; taking the pair road over those printed a handle as a
// number, which is worse than the refusal the program had.
func TestTheFloatArgumentDoorLeavesTheRoadsThatAlreadyWorked(t *testing.T) {
	// ADR 0274's float-return promotion: the define keeps its `(double %p0)` shape exactly, with no tag
	// word beside it, because the double already carries the value.
	res, err := Compile("def f(x):\n    x = x + 1.5\n    return -x\n\nprint(f(1.0))\n")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !strings.Contains(res.IR, "define double @gy_f(double %p0)") {
		t.Errorf("the float-return road gained a tag word it does not use:\n%s", irFunctions(t, res.IR)["f"])
	}
	if out := runIR(t, res.IR); out != "-2.5\n" {
		t.Errorf("the promoted return printed %q, want CPython's -2.5", out)
	}

	// A comparison read as a *value* has no pair door, so the parameter stays on the ordinary road and
	// the program prints the answer it printed before this door — not a refusal it never needed. Asked as
	// a condition, the same comparison is served (the row below pins that).
	for _, tc := range []struct{ name, src, want string }{
		{
			"a comparison returned as a value",
			"def cmpf(v):\n    return v > 1.5\n\nprint(cmpf(2.0))\nprint(cmpf(1.0))\n", "True\nFalse\n",
		},
		{
			// The condition position *is* served: the truth door reads a tagged operand (ADR 0269,
			// ADR 0275), so the parameter takes the pair and the arithmetic in the arm keeps its half.
			"a comparison read as a condition",
			"def big(v):\n    if v > 10:\n        return v * 2\n    return v\n\nprint(big(18.5))\nprint(big(7))\n", "37.0\n7\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%q was refused; this shape already answered before the door opened: %v", tc.name, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s printed %q, want %q", tc.name, out, tc.want)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want %q", tc.name, out, tc.want)
			}
		})
	}

	// ADR 0174's string-return road: `return str(v)` hands back an @str_tab index. The pair road stays out
	// and the program keeps the road — and the answer or refusal — that road had.
	if _, err := Compile("def fmt(v):\n    return str(v)\n\nprint(fmt(2.5))\n"); err == nil {
		t.Errorf("the string-return road took the pair road with it; the index it prints is not an answer")
	}
	if _, err := Compile("def fmt(v):\n    s = str(v)\n    return s\n\nprint(fmt(2.5))\n"); err == nil {
		t.Errorf("a name bound to str() and returned printed an interned index; the string road owns it")
	}
}

// TestTheFloatArgumentIsInTheModule is the IR half: the parameter arrives as two words, the box is made
// by the one door that boxes a double, and the arithmetic is the tagged door — while an integer-only
// function in the same program keeps the one-word signature it always had.
func TestTheFloatArgumentIsInTheModule(t *testing.T) {
	const src = "def twice(v):\n    return v * 2\n\ndef plus1(v):\n    return v + 1\n\nprint(twice(2.5))\nprint(plus1(3))\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	body := irFunctions(t, res.IR)["twice"]
	// The arity is the `define`'s own spelling: payload and tag, both i32, beside the frame pointer the
	// calling convention already passes (roadmap L11.1, ADR 0273's shape, widened to this argument).
	if payload, tag := wordParams(defineLine(res.IR, "gy_twice")); payload != 1 || tag != 1 {
		t.Errorf("the pair-carrying parameter arrived as %d payload word(s) and %d tag word(s), want one of each:\n%s",
			payload, tag, defineLine(res.IR, "gy_twice"))
	}
	if !strings.Contains(res.IR, "call i32 @rt_float_new(double") {
		t.Errorf("the argument's double was never boxed — no @rt_float_new in the module:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "call i32 @rt_num_arith") {
		t.Errorf("the callee's arithmetic did not go through the tagged door:\n%s", body)
	}
	if payload, tag := wordParams(defineLine(res.IR, "gy_plus1")); payload != 1 || tag != 0 {
		t.Errorf("an integer-only function was given %d payload word(s) and %d tag word(s), want one and none:\n%s",
			payload, tag, defineLine(res.IR, "gy_plus1"))
	}
	if !strings.Contains(body, "ret i32") {
		t.Errorf("the pair-returning body has no i32 return to carry the payload in:\n%s", body)
	}
	assertNoForbiddenIR(t, src, res.IR)
}

// TestThePairCallScanIsAskedOfTheProgramNotTheEmissionOrder is the gate's own question, one function
// earlier than the emitter: the mark, the integer proof and the withheld road are all scan answers.
func TestThePairCallScanIsAskedOfTheProgramNotTheEmissionOrder(t *testing.T) {
	parse := func(t *testing.T, src string) map[string]*pairFnSpec {
		t.Helper()
		prog, err := Parse(src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return pairCallSpecs(prog)
	}
	t.Run("a float argument marks the parameter", func(t *testing.T) {
		specs := parse(t, floatArgTwice+"print(twice(2.5))\n")
		spec := specs["twice"]
		if spec == nil || !spec.params[0] {
			t.Fatalf("the parameter carrying a double's argument was left unmarked: %+v", spec)
		}
	})
	t.Run("an integer-only call leaves the function out entirely", func(t *testing.T) {
		specs := parse(t, floatArgTwice+"print(twice(3))\n")
		if spec, seen := specs["twice"]; seen {
			t.Errorf("nothing in this program needs a tag, and the function took one anyway: %+v", spec)
		}
	})
	t.Run("the provably-integer parameter is named", func(t *testing.T) {
		specs := parse(t, "def area(w, h):\n    return w * h\n\nprint(area(2.5, 2))\nprint(area(3, 4))\n")
		spec := specs["area"]
		if spec == nil || !spec.params[0] {
			t.Fatalf("`w` arrived without a pair: %+v", spec)
		}
		if len(spec.params) != 1 {
			t.Errorf("`h` was opened as a pair too; the call sites hand it nothing but integers: %+v", spec.params)
		}
		if !spec.intParams["h"] {
			t.Errorf("`h` was not named an integer parameter, so `w * h` has no kind to read: %+v", spec.intParams)
		}
	})
	t.Run("a default marks the parameter no call site writes", func(t *testing.T) {
		specs := parse(t, "def greet(name, times=1.5):\n    return times\n\nprint(greet(\"a\"))\n")
		spec := specs["greet"]
		if spec == nil || !spec.params[1] {
			t.Fatalf("the default was not read as an argument: %+v", spec)
		}
	})
	t.Run("the float-return road is left alone", func(t *testing.T) {
		specs := parse(t, "def f(x):\n    x = x + 1.5\n    return -x\n\nprint(f(1.0))\n")
		if spec, seen := specs["f"]; seen {
			t.Errorf("ADR 0274's road owns this body's words, and the scan opened a pair anyway: %+v", spec)
		}
	})
	t.Run("the string-return road is left alone", func(t *testing.T) {
		specs := parse(t, "def fmt(v):\n    s = str(v)\n    return s\n\nprint(fmt(2.5))\n")
		if spec, seen := specs["fmt"]; seen {
			t.Errorf("ADR 0174's road owns this body's answer, and the scan opened a pair anyway: %+v", spec)
		}
	})
	t.Run("a comparison read as a value leaves the parameter alone", func(t *testing.T) {
		specs := parse(t, "def cmpf(v):\n    return v > 1.5\n\nprint(cmpf(2.0))\n")
		if spec, seen := specs["cmpf"]; seen {
			t.Errorf("no door reads a comparison's pair operand, and the parameter was opened anyway: %+v", spec)
		}
	})
	t.Run("the self-call keeps its ordinary answer", func(t *testing.T) {
		specs := parse(t, "def fib(n):\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(10))\n")
		if spec, seen := specs["fib"]; seen && spec.returnsPair {
			t.Errorf("a self-call would write the tag word its own caller has not read: %+v", spec)
		}
	})
}
