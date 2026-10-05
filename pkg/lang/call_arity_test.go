package lang

// How many arguments there were is one question, asked once, on the road every caller shares
// (roadmap Gap R.168, ADR 0284).
//
// The checked call road counted arguments; `callClosure` — the road for a callable read out of a
// variable — counted nothing, and `callFunc`'s bind loop treated a short `argVals` as "use the default"
// even when there was no default and ignored the extras entirely. So:
//
//	g = lambda x: x * 2
//	print(g(1, 2))   # answers 2   at exit 0 — CPython: <lambda>() takes 1 positional argument but 2 were given
//	print(g())       # answers 0   at exit 0 — CPython: <lambda>() missing 1 required positional argument: 'x'
//
// Three wrong numbers at exit 0, the class the ladder always outranks. The count moved into the shared
// road rather than into one caller, so a fifth road added later cannot forget it again — and the wording
// is shared too, because one program must not read two ways depending on whether its callee was declared
// with `def` or bound with `=` (ADR 0215's wording rule).

import (
	"strings"
	"testing"
)

// TestACallableFromAVariableIsAskedItsArgumentCount is the wrong-number table: each row must stop the
// program, and must not print a digit.
func TestACallableFromAVariableIsAskedItsArgumentCount(t *testing.T) {
	for _, tc := range []struct{ name, src, wantSub string }{
		{"lambda handed one argument too many", "g = lambda x: x * 2\nprint(g(1, 2))\n", "too many arguments for <lambda>"},
		{"lambda handed nothing", "g = lambda x: x * 2\nprint(g())\n", `missing argument "x" for <lambda>`},
		{"lambda missing its second parameter", "g = lambda x, y: x - y\nprint(g(3))\n", `missing argument "y" for <lambda>`},
		{"lambda handed three extras", "g = lambda x: x * 2\nprint(g(1, 2, 3, 4))\n", "it accepts 1 argument, got 4"},
		{"two lambdas, one wrong call each", "a = lambda x: x + 1\nb = lambda x, y: x - y\nprint(a(1, 2))\nprint(b(1))\n", "too many arguments for <lambda>"},
		// A literal `f(1, 2)` is answered by the checker (`verify: function "f" expects 1 argument,
		// got 0`) before any road runs; the road's own sentence is reached where the callee arrives as a
		// value, which is exactly the road Gap R.168 was measured on.
		{"a def'd callee reads the same sentence", "def f(x):\n    return x * 2\n\nh = f\nprint(h(1, 2))\n", "too many arguments for f"},
		{"a def'd callee missing an argument", "def f(x):\n    return x * 2\n\nh = f\nprint(h())\n", `missing argument "x" for f`},
		{"an unknown keyword still names itself", "def g(a):\n    return a\n\nprint(g(z=1))\n", "unknown keyword"},
		{"a value given twice still names itself", "def g(a):\n    return a\n\nprint(g(1, a=2))\n", "multiple values"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := EvalExpr(tc.src)
			if err == nil {
				t.Fatalf("the program ran to completion — this is the shape that printed a number at exit 0 (Gap R.168)\nsrc: %s", tc.src)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.wantSub) {
				t.Errorf("reported %q, which does not contain %q\nsrc: %s", msg, tc.wantSub, tc.src)
			}
			// EvalExpr answers (value, diagnostics, error): the value is the last expression's, not
			// printed text, so "did it print a digit" is a stdout question — and stdout is empty here
			// precisely because the raise happened before the print.
			if ee, isEval := err.(*EvalError); isEval && ee.ExnType != "" && ee.ExnType != "TypeError" {
				t.Errorf("the arity trap raised %q; the reference stops these calls with TypeError-class errors\nsrc: %s", ee.ExnType, tc.src)
			}
		})
	}
}

// TestDefaultsAndKeywordsStillAnswer is the other half of the row, and the reason the count sits where
// it does: a missing argument is only an error when nothing can fill it.
func TestDefaultsAndKeywordsStillAnswer(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"one argument, two defaults", "def g(a, b=2, c=3):\n    return a + b + c\n\nprint(g(1))\n", "6\n"},
		{"two arguments, one default", "def g(a, b=2, c=3):\n    return a + b + c\n\nprint(g(1, 5))\n", "9\n"},
		{"every argument given", "def g(a, b=2, c=3):\n    return a + b + c\n\nprint(g(1, 5, 6))\n", "12\n"},
		{"keywords out of order", "def g(a, b=2, c=3):\n    return a + b + c\n\nprint(g(c=9, a=1))\n", "12\n"},
		{"a default that computes", "def g(a, b=None):\n    if b is None:\n        b = a * 2\n\n    return a + b\n\nprint(g(3))\n", "9\n"},
		{"a lambda called correctly", "g = lambda x: x * 2\nprint(g(21))\n", "42\n"},
		{"a lambda through a parameter, twice", "def twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(twice(lambda x: x * 3, 2))\n", "12\n"},
		{"a nested def called from its owner", "def outer(x):\n    def inner(y):\n        return y * 2\n\n    return inner(x) + 1\n\nprint(outer(5))\n", "11\n"},
		{"a method called with its one parameter", "class C:\n    def m(self, a):\n        return a + 1\n\nc = C()\nprint(c.m(4))\n", "5\n"},
		{"recursion", "def fact(n):\n    if n <= 1:\n        return 1\n\n    return n * fact(n - 1)\n\nprint(fact(5))\n", "120\n"},
		{"five parameters", "def h(a, b, c, d, e):\n    return a + b + c + d + e\n\nprint(h(1, 2, 3, 4, 5))\n", "15\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("interpreter: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
		})
	}
}

// TestTheAritySentenceIsOneSentenceForBothRoads is the wording row: the reference names `<lambda>` for a
// lambda and the callee's own name for a `def`, and this language must not name the compiler's generated
// `lambda_0` at a reader who never wrote it.
func TestTheAritySentenceIsOneSentenceForBothRoads(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
		not             []string
	}{
		{
			name: "a lambda is named the way the reference names it",
			src:  "g = lambda x: x\nprint(g(1, 2))\n", want: "too many arguments for <lambda>",
			// `lambda_0` is the compiler's internal name; `None` would be a missing fallback.
			not: []string{"lambda_0", "None", "nil"},
		},
		// (A direct `measure(1, 2)` is NOT in this table: the checker answers it first —
		// `verify: function "measure" accepts 1 argument, got more` — so the run-time sentence below is
		// never reached for a literal call. It is reached for the calls the checker cannot count, which
		// is why the callee here is written twice and called through a name the checker leaves alone.)
		{
			name: "a def reached through a variable keeps the name the program gave it",
			src:  "def measure(x):\n    return x\n\nh = measure\nh(1, 2)\n", want: "too many arguments for measure",
			not: []string{"<lambda>", "lambda_0"},
		},
		{
			name: "the count the callee accepts is stated for a lambda",
			src:  "g = lambda x, y: x\nprint(g(1))\n", want: `missing argument "y" for <lambda>`,
			not: []string{"lambda_0"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := EvalExpr(tc.src)
			if err == nil {
				t.Fatalf("expected an arity error\nsrc: %s", tc.src)
			}
			msg := err.Error()
			if !strings.Contains(msg, tc.want) {
				t.Errorf("the sentence must read %q, got %q\nsrc: %s", tc.want, msg, tc.src)
			}
			for _, banned := range tc.not {
				if strings.Contains(msg, banned) {
					t.Errorf("the sentence must not contain %q (a name the reader never wrote, or a missing fallback): %q", banned, msg)
				}
			}
		})
	}
}

// TestTheCompiledRoadAlreadyAskedItIs the record that this row was interpreter-only: the compiled
// backend refused every one of these at exit 1 with its own arity sentence, so closing the interpreter's
// wrong numbers brings the engines into agreement rather than moving a refusal.
func TestTheCompiledRoadAlreadyAskedIt(t *testing.T) {
	for _, tc := range []struct{ name, src, wantSub string }{
		{"lambda too many", "g = lambda x: x * 2\nprint(g(1, 2))\n", "too many arguments"},
		{"lambda too few", "g = lambda x: x * 2\nprint(g())\n", "missing argument"},
		{"lambda missing second", "g = lambda x, y: x - y\nprint(g(3))\n", "missing argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("the compiled leg accepted a call the reference refuses — check whether the checker moved (Gap R.168)\nsrc: %s", tc.src)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("compiled refusal %q must name the arity question %q", err.Error(), tc.wantSub)
			}
		})
	}
}
