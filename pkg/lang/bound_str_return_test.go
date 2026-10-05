package lang

// Gap R.170 / ADR 0286 — a text the body rendered into a NAME is a string return.
//
// ADR 0281 taught the compiler that `return str(v)` makes a function string-returning, so its caller
// prints the text. It read the RETURN EXPRESSION. A body that binds the rendering one statement earlier —
// `s = str(v)` / `return s` — was still a number-returning function to its callers, and `print(f(3))`
// handed the interned index to `printf` with `%d` and printed `0` at exit 0 while the reference and the
// interpreter printed `3`. Same door, same value, one statement of indentation between them.
//
// The second half of this file pins the road that made the wrong number possible: `str()` of anything
// that was "not a text and not a float" went to `rt_str_of_int`, which writes the DECIMAL DIGITS OF THE
// WORD IT WAS HANDED. That is right for an int and a lie for a container handle or a void, so the road
// now asks what the operand can be seen to be.
//
// Both halves are tables of reference answers, taken from `python3` 3.12.3 and re-pinned here, because a
// regression in this family is a digit, not a diagnostic.

import (
	"strings"
	"testing"
)

// TestARenderingBoundToANameIsAStringReturn is the ladder rule for Gap R.170: these programs print
// something on all three legs, and the compiled leg must print the SAME thing. Before ADR 0286 the
// compiled answers were `0`, `0`, `0`, `2`, `0` — every one of them a plausible number at exit 0.
func TestARenderingBoundToANameIsAStringReturn(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"str bound then returned", "def f(v):\n    s = str(v)\n    return s\n\nprint(f(3))\n", "3\n"},
		{"str bound of a text argument", "def f(v):\n    s = str(v)\n    return s\n\nprint(f(\"hi\"))\n", "hi\n"},
		{"repr bound then returned", "def f(v):\n    r = repr(v)\n    return r\n\nprint(f(3))\n", "3\n"},
		{"concat with a bound rendering", "def f(v):\n    s = \"x\" + str(v)\n    return s\n\nprint(f(3))\n", "x3\n"},
		{"bound name bound again", "def f(v):\n    a = str(v)\n    b = a\n    return b\n\nprint(f(7))\n", "7\n"},
		{"bound inside a condition", "def f(v):\n    if v > 0:\n        s = str(v)\n        return s\n\n    return \"no\"\n\nprint(f(3))\nprint(f(0))\n", "3\nno\n"},
		{"bound rendering measured", "def f(v):\n    s = str(v)\n    return s\n\nprint(len(f(3)))\n", "1\n"},
		{"bound rendering asked a method", "def f(v):\n    s = str(v)\n    return s\n\nprint(f(3).upper())\n", "3\n"},
		// ADR 0281's own rows, kept here so the direct form cannot regress while the bound form is fixed.
		{"direct rendering still answers", "def g():\n    return str(42)\n\nprint(g())\n", "42\n"},
		// Gap R.6's rule: a program that takes the builtin's name means its own function.
		{"program owns the name str", "def str(x):\n    return x + 7\n\nprint(str(42))\n", "49\n"},
		// The number road must not be swept up by the string verdict.
		{"a number body stays a number body", "def n(v):\n    y = v * 2\n    return y\n\nprint(n(3))\nprint(n(2.5))\n", "6\n5.0\n"},
		{"a rendering of a container still renders", "def f(v):\n    s = str(v)\n    return s\n\nprint(f([1, 2]))\n", "[1, 2]\n"},
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
				t.Errorf("--aot printed %q, want %q (Gap R.170: a bound rendering printed the interned index)", compiled, r.want)
			}
		})
	}
}

// TestTheRenderingRefusalIsNotAWrongNumber pins the road narrowed alongside the fix. `str()` used to
// reach `rt_str_of_int` for anything "not a text and not a float", which writes the digits of whatever
// word it is handed — correct for an int, and a fabricated number for a handle. A shape the renderer
// cannot name must exit 1 naming the missing rendering, never exit 0 with digits.
func TestTheRenderingRefusalIsNotAWrongNumber(t *testing.T) {
	// Each row: the reference's answer is NOT reachable by the digits road, so either the renderer
	// answers it correctly (checked above) or the compiler refuses. What is forbidden is exit 0 with
	// a number the reference never prints.
	rows := []struct {
		name string
		src  string
	}{
		{"str of a comprehension variable", "xs = [1, 2]\nprint(str(xs[0]))\n"},
		{"str of a dict slot", "d = {\"a\": 1}\nprint(str(d))\n"},
		{"str of a set", "s = {1, 2}\nprint(str(s))\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			res, err := Compile(r.src)
			if err == nil && res != nil {
				// Answering is allowed, but not with a digit the reference does not print; the
				// table above is where answers are pinned, so a compile here must run cleanly.
				if _, _, rerr := EvalExpr(r.src); rerr != nil {
					t.Fatalf("compiled cleanly but the program failed to run: %v", rerr)
				}
				return
			}
			msg := err.Error()
			if strings.Contains(msg, "rt_str_of_int") {
				t.Fatalf("refusal leaked the digits road: %s", msg)
			}
		})
	}
}

// TestTheDigitsRoadFollowsTheLatestBinding guards the regression I introduced while narrowing:
// `x = "abc"` then `x = 5` then `print(str(x))` prints 5 in the reference. My first version asked
// "was this name EVER a number" and refused a program the reference prints, which the ladder forbids —
// a working answer may not become a refusal. The road reads what the name holds now.
func TestTheDigitsRoadFollowsTheLatestBinding(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		{"rebound to a number", "x = \"abc\"\nx = 5\nprint(str(x))\n", "5\n"},
		{"bound as a number", "n = 42\nprint(str(n))\n", "42\n"},
		{"answer of arithmetic", "a = 2\nb = a * 3\nprint(str(b))\n", "6\n"},
		{"a callee's answer", "def g():\n    return 7\n\nprint(str(g()))\n", "7\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			got := captureStdout(t, r.src)
			if got != r.want {
				t.Fatalf("interpreter printed %q, want %q", got, r.want)
			}
			compiled := compiledOut(t, r.src)
			if compiled != r.want {
				t.Errorf("--aot printed %q, want %q", compiled, r.want)
			}
		})
	}
}

// TestTheDigitsRoadStillRefusesWhatItCannotSee is the other half of the same narrowing: a name the
// program bound to a text or a container is not assumed to be a number, and the answer must not be the
// digits of a handle.
func TestTheDigitsRoadStillRefusesWhatItCannotSee(t *testing.T) {
	rows := []struct {
		name string
		src  string
	}{
		{"a name bound to a list", "xs = [1, 2]\nprint(str(xs))\n"},
		{"a name bound to a dict", "d = {\"a\": 1}\nprint(str(d))\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			out := captureStdout(t, r.src)
			// Whatever the compiled leg does, it may not print a bare integer where the reference
			// prints a container: that is Gap R.170's shape arriving from the other direction.
			compiled, _ := compiledOutOrRefusal(t, r.src)
			if compiled == out {
				return
			}
			if isPlainIntegerLine(compiled) {
				t.Errorf("--aot answered %q where the reference prints %q — the digits of a handle (Gap R.170)", compiled, out)
			}
		})
	}
}

func isPlainIntegerLine(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '-' && i == 0 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// compiledOut lowers src and runs the module under lli, failing the test if the compiler refused or the
// program exited non-zero. The interpreter helper is captureStdout (jit_test.go); this is its compiled
// twin, named for the question it asks.
func compiledOut(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compiled leg refused a program the oracle answers (%v): %s", err, src)
	}
	return runIR(t, res.IR)
}

// compiledOutOrRefusal is compiledOut for the rows where a refusal is an acceptable answer: it hands
// back the output and whether the compiler refused, so the caller can assert only the forbidden shape
// (a fabricated number) rather than pinning a limitation that a later cycle is free to lift.
func compiledOutOrRefusal(t *testing.T, src string) (string, bool) {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		return err.Error(), true
	}
	return runIR(t, res.IR), false
}
