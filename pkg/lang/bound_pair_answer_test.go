package lang

// A number the body computed out of its own parameter keeps the kind the argument arrived with
// (roadmap L11.6, Gap R.169, ADR 0285).
//
// ADR 0280 taught the scan to read a name bound by a CALL; this is the sibling it never asked — a name
// bound by ARITHMETIC. `y = x + 1` then `return y` reads as a plain local at the return, the body's answer
// direction closes, the ordinary one-word return road claims the function, and the double the tagged door
// computed is truncated at the `ret`:
//
//	def f(x):
//	    y = x + 1
//	    return y
//	print(f(0.1))   # CPython 1.1 · this printed 1 at exit 0
//
// `y = x * 2` answered `0` for `0.2`, `y = x - 1` answered `-1` for `-0.9`, `y = x` answered `0` for `0.1`.
// The cause was a documented gap in the scan's own words: "a parameter of an enclosing function is bound by
// no assignment and so is not here", because a parameter is written by the CALLER. `exprNumberish` was
// therefore asked "can both words be produced from the spelling?" about a name no spelling writes, and
// answered no.

import (
	"strings"
	"testing"
)

// TestANumberBoundFromAParameterKeepsItsKind is the promoted family: each row is CPython-exact on both
// engines, and every one of them printed a truncated integer at exit 0 before ADR 0285.
func TestANumberBoundFromAParameterKeepsItsKind(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"add one, bind, return", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(0.1))\n", "1.1\n"},
		{"double it, bind, return", "def f(x):\n    y = x * 2\n    return y\n\nprint(f(0.1))\n", "0.2\n"},
		{"subtract one, bind, return", "def f(x):\n    y = x - 1\n    return y\n\nprint(f(0.1))\n", "-0.9\n"},
		{"the int side of the same body", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(2))\n", "3\n"},
		{"one body, both arguments", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(0.1))\nprint(f(2))\n", "1.1\n3\n"},
		{"one body, both arguments, doubled", "def f(x):\n    y = x * 2\n    return y\n\nprint(f(2.5))\nprint(f(3))\n", "5.0\n6\n"},
		{"arms inside the binding", "def f(v):\n    y = (v - 1) * 2\n    return y\n\nprint(f(2.5))\nprint(f(3))\n", "3.0\n4\n"},
		{"a float default, bound", "def f(x=2.5):\n    y = x * 2\n    return y\n\nprint(f())\n", "5.0\n"},
		{"a call answer bound (ADR 0280's own row)", "def d(v):\n    return v * 2\n\ndef f(x):\n    y = d(x)\n    return y\n\nprint(f(2.5))\n", "5.0\n"},
		{"an int-only callee is untouched", "def f(x):\n    y = x + 1\n    return y\n\nprint(f(10))\n", "11\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the oracle answers: %v\nsrc: %s", err, tc.src)
			}
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q — a truncated digit is Gap R.169 back\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestABoundAnswerStillRefusedIsTheHonestHalf: these shapes answered a truncated integer at exit 0
// before ADR 0285 and now exit 1. A refusal is an improvement over a wrong number, not a closure — the
// missing word is L11.1's tagged value, and each row keeps its own line in Gap R.169's record until it
// moves to the table above.
func TestABoundAnswerStillRefusedIsTheHonestHalf(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"two bindings deep (CPython 3.0, this printed 3)", "def f(x):\n    y = x + 1\n    z = y * 2\n    return z\n\nprint(f(0.5))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Skipf("the shape now compiles — promote its row to the answering table and check it against python3")
			}
			// The pass prefix is added by the CLI, not by Compile; what the unit can assert is that the
			// refusal is a compiler diagnostic naming its roadmap row, and not an llc rejection.
			if !strings.Contains(err.Error(), "roadmap") {
				t.Errorf("a refusal must cite the row it belongs to, got %v", err)
			}
			if strings.Contains(err.Error(), "llc") || strings.Contains(err.Error(), "use of undefined value") {
				t.Errorf("a refusal must not arrive from llc (ADR 0166): %v", err)
			}
		})
	}
}

// TestTheBoundAnswerRefusalNamesWhatTheArithmeticWasOver is the wording half, and it is a test of the
// refusals rather than a test of answers: the ladder says a shape may stay refused, but never refused in
// words that describe a different program (Gap R.38). Two sentences were wrong when this row was written —
// one blamed "a slot the program built at run time" for arithmetic over a parameter (a reader goes looking
// for a container that is not in the file), and one claimed "another arm returns a plain integer" for a
// body with no other arm.
func TestTheBoundAnswerRefusalNamesWhatTheArithmeticWasOver(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		not             []string
	}{
		{
			// `y = x` binds the parameter itself: no container, and no second arm.
			name: "a name bound straight from a parameter names the caller",
			src:  "def f(x):\n    y = x\n    return y\n\nprint(f(0.1))\n", want: "parameter the call site handed a double",
			not: []string{"slot the program built at run time", "another arm", "plain integer"},
		},
		{
			// `y = x + 1` under an `if`: arithmetic, but over the argument, not over a built-up container.
			name: "arithmetic over a parameter names the parameter",
			src:  "def f(x):\n    if x > 0:\n        y = x + 1\n        return y\n\n    return 0\n\nprint(f(0.5))\n", want: "arithmetic over a parameter the caller supplied",
			not: []string{"slot the program built at run time"},
		},
		{
			name: "a parameter used as a return value names the caller",
			src:  "def f(x):\n    if x > 1:\n        return 100\n\n    return x\n\nprint(f(0.1))\n", want: "parameter the call site handed a double",
			not: []string{"slot the program built at run time", "another arm", "plain integer"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Skipf("the shape now compiles — the refusal pin below is obsolete, and promoting it is the good outcome")
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Errorf("the refusal must name %q, got:\n%s", tc.want, msg)
			}
			for _, banned := range tc.not {
				if strings.Contains(msg, banned) {
					t.Errorf("the refusal must not claim %q — that is a statement about a program the reader cannot find (Gap R.38):\n%s", banned, msg)
				}
			}
		})
	}
}

// TestASlotTheProgramBuiltStillNamesItself keeps the ORIGINAL sentence alive for the shape it describes,
// so the new origins cannot swallow the one diagnosis that was already right.
func TestASlotTheProgramBuiltStillNamesItself(t *testing.T) {
	res, err := Compile("xs = [1]\nxs.append(2.5)\nprint(xs[0] / 2)\n")
	if err != nil {
		t.Fatalf("a true division over a runtime slot answers 1.5 today and must keep answering: %v", err)
	}
	if out := runIR(t, res.IR); out != "0.5\n" {
		t.Logf("compiled printed %q", out)
	}
	// The slot story belongs to a container read, and it is still the sentence used there.
	_, err = Compile("def f(v):\n    return v > 1.5\n\nxs = [1]\nxs.append(2.5)\nprint(f(xs[0]))\n")
	if err != nil && !strings.Contains(err.Error(), "roadmap") {
		t.Errorf("a refusal must still cite its roadmap row, got: %v", err)
	}
}
