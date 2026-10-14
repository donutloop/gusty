package integration

import (
	"strings"
	"testing"
)

// call_key_test.go — the CLI witness for the ordering builtins' `key=` door (roadmap L11.7,
// Gap R.249; ADR 0340).
//
// Before this row, `sorted` read its option arguments by POSITION. The reference reads them by NAME:
// `key=` and `reverse=` are options, a callable key is called on every element, and an invalid keyword
// stops the program with TypeError. The old backend read `key=abs` as if it meant "reverse flag" and
// answered the wrong thing, read `key=3` as a truthy flag and answered a sorted list where the reference
// raises, and sent `key=` into min/max's candidate list where it poisoned the answer.
//
// Three classes at the CLI:
//   - answers, asserted against the oracle and not merely against the previous build;
//   - traps, asserted at the reference's exit class (3) with its exact TypeError/ValueError words,
//     and catchable by the program's own `except`; and
//   - refusals, asserted at exit 1, never exit 2 (ADR 0166), with the refusal naming the key and the
//     roadmap row that owns the missing half.

func TestKeyedOrdersAtTheCLIMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"sorted by absolute value", "print(sorted([3, 1, 2], key=abs))\n", "[1, 2, 3]\n"},
		{"absolute value with a negative winner", "print(sorted([-3, 1, 2], key=abs))\n", "[1, 2, -3]\n"},
		{"key and reverse are two named options", "print(sorted([3, 1, -2], key=abs, reverse=True))\n", "[3, -2, 1]\n"},
		{"reverse=False is the same ascending order", "print(sorted([3, 1, 2], key=abs, reverse=False))\n", "[1, 2, 3]\n"},
		{"None is the identity key", "print(sorted([3, 1, 2], key=None))\n", "[1, 2, 3]\n"},
		{"equal keys are stable", "print(sorted([2, 1, 2, 1], key=abs))\n", "[1, 1, 2, 2]\n"},
		{"already-written order survives ties", "print(sorted([3, 3, 1], key=abs))\n", "[1, 3, 3]\n"},
		{"an empty keyed list", "print(sorted([], key=abs))\n", "[]\n"},
		{"a user function as the key", "def flip(x):\n    return 0 - x\nprint(sorted([3, 1, 2], key=flip))\n", "[3, 2, 1]\n"},
		{"a default parameter is callable with one argument", "def bump(x, bias=0):\n    return x + bias\nprint(sorted([3, 1, 2], key=bump))\n", "[1, 2, 3]\n"},
		{"min reads its key", "print(min(3, 1, -2, key=abs))\n", "1\n"},
		{"max reads its key", "print(max(3, 1, -2, key=abs))\n", "3\n"},
		{"min over a literal container", "print(min([3, 1, 2], key=abs))\n", "1\n"},
		{"max over a literal container", "print(max([3, 1, 2], key=abs))\n", "3\n"},
		{"min with a user key", "def flip(x):\n    return 0 - x\nprint(min([3, 1, -2], key=flip))\n", "3\n"},
		{"max with a user key", "def flip(x):\n    return 0 - x\nprint(max([3, 1, -2], key=flip))\n", "-2\n"},
		{"a text key orders by length", "print(sorted([\"bbb\", \"a\", \"cc\"], key=len))\n", "['a', 'cc', 'bbb']\n"},
		{"a text key reads reverse too", "print(sorted([\"bbb\", \"a\", \"cc\"], key=len, reverse=True))\n", "['bbb', 'cc', 'a']\n"},
		{"min reads a text key", "print(min([\"aaa\", \"b\", \"cc\"], key=len))\n", "b\n"},
		{"max reads a text key", "print(max([\"aaa\", \"b\", \"cc\"], key=len))\n", "aaa\n"},
		{"str as key compares texts, not interned indices", "print(sorted([10, 9, 1], key=str))\n", "[1, 10, 9]\n"},
		{"a keyed list behaves like a list", "xs = sorted([3, 1, 2], key=abs)\nprint(xs, len(xs), xs[1], xs[0] + 1)\n", "[1, 2, 3] 3 2 2\n"},
		{"a keyed order loops", "for v in sorted([3, 1, -2], key=abs, reverse=True):\n    print(v)\n", "3\n-2\n1\n"},
		{"a keyed loop over text", "for v in sorted([\"bbb\", \"a\", \"cc\"], key=len):\n    print(v)\n", "a\ncc\nbbb\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "key_argument.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

func TestKeyedOrdersRaiseWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct {
		name, src, msg string
	}{
		{"int key", "print(sorted([3, 1, 2], key=3))\n", "TypeError: 'int' object is not callable"},
		{"min int key", "print(min([3, 1, 2], key=3))\n", "TypeError: 'int' object is not callable"},
		{"float key", "print(sorted([3, 1, 2], key=1.5))\n", "TypeError: 'float' object is not callable"},
		{"text key", "print(sorted([3, 1, 2], key=\"abs\"))\n", "TypeError: 'str' object is not callable"},
		{"bool key", "print(sorted([3, 1, 2], key=True))\n", "TypeError: 'bool' object is not callable"},
		{"container key", "xs = [1, 2]\nprint(sorted([3, 1], key=xs))\n", "TypeError: 'list' object is not callable"},
		{"zero-arity key", "def z():\n    return 1\nprint(sorted([1, 2], key=z))\n", "TypeError: z() takes 0 positional arguments but 1 was given"},
		{"too-few-argument key", "def g(a, b):\n    return a\nprint(sorted([1, 2], key=g))\n", "TypeError: g() missing 1 required positional argument: 'b'"},
		{"reverse on min is not a key", "print(min([3, 1], reverse=True))\n", "TypeError: 'reverse' is an invalid keyword argument for min()"},
		{"reverse on max is refused the same way", "print(max([3, 1], key=abs, reverse=True))\n", "TypeError: 'reverse' is an invalid keyword argument for max()"},
		{"cmp was removed from sort", "print(sorted([1], cmp=3))\n", "TypeError: 'cmp' is an invalid keyword argument for sort()"},
		{"empty min", "print(min([], key=abs))\n", "ValueError: min() iterable argument is empty"},
		{"empty max", "print(max([], key=abs))\n", "ValueError: max() iterable argument is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "key_trap.gy", tc.src)
			py, pyCode := oracleTrap(t, path)
			if pyCode == 0 || !strings.Contains(py, tc.msg) {
				t.Fatalf("the oracle does not raise what the table claims: exit %d, %q", pyCode, py)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Errorf("%s exited 0 on a program the oracle dies on: stdout=%q", engine, out)
				}
				if code == 2 {
					t.Errorf("%s exited 2 (compiler bug, ADR 0166) on a program the oracle dies on:\n%s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, "Traceback (most recent call last):") || !strings.Contains(combined, tc.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, tc.msg, combined)
				}
			}
		})
	}
}

func TestKeyedTrapsAreCatchableAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"non-callable key caught", "try:\n    print(sorted([3, 1, 2], key=3))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"invalid keyword caught", "try:\n    print(min([3, 1], reverse=True))\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"empty max caught", "try:\n    print(max([], key=abs))\nexcept ValueError:\n    print(\"caught\")\n", "caught\n"},
		{"the next order still answers", "try:\n    print(sorted([3, 1, 2], key=3))\nexcept TypeError:\n    print(sorted([3, 1, 2], key=abs))\n", "[1, 2, 3]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "key_catch.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

func TestKeyedOrdersThatRemainRefusedStayRefusedAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, phrase string }{
		{"heap list sorted by key", "xs = [3, 1, 2]\nprint(sorted(xs, key=abs))\n", "key=abs"},
		{"float-returning user key", "def f(x):\n    return x / 2\nprint(sorted([3, 1, 2], key=f))\n", "key=f"},
		{"lambda key", "print(sorted([3, 1, 2], key=lambda x: 0 - x))\n", "lambda"},
		{"call-expression key", "def f(x):\n    return x\nprint(sorted([3, 1, 2], key=f(1)))\n", "key=f(1)"},
		{"builtin that cannot be called by this door", "print(sorted([3, 1, 2], key=hash))\n", "key=hash"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "key_refusal.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Fatalf("%s answered %q at exit 0; this shape is still owed by roadmap L11.7", engine, out)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on a refusal (ADR 0166): %s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, tc.phrase) || !strings.Contains(combined, "L11.") {
					t.Fatalf("%s refused without naming %q and the roadmap row:\n%s", engine, tc.phrase, combined)
				}
			}
		})
	}
}

func TestUnkeyedOrdersAreNotMovedByTheKeyDoor(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"sorted", "print(sorted([3, 1, 2]))\n", "[1, 2, 3]\n"},
		{"empty sorted", "print(sorted([]))\n", "[]\n"},
		{"min", "print(min(3, 1, 2))\n", "1\n"},
		{"max", "print(max(3, 1, 2))\n", "3\n"},
		{"min container", "print(min([3, 1, 2]))\n", "1\n"},
		{"max container", "print(max([3, 1, 2]))\n", "3\n"},
		{"text min", "print(min(\"b\", \"a\"))\n", "a\n"},
		{"float max", "print(max([1.5, 2.5]))\n", "2.5\n"},
		{"heap sorted", "xs = [3, 1, 2]\nprint(sorted(xs))\n", "[1, 2, 3]\n"},
		{"heap sorted loop", "xs = [3, 1, 2]\nfor v in sorted(xs):\n    print(v)\n", "1\n2\n3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "unkeyed_order.gy", tc.src)
			if py, ok := cpythonOut(t, path); ok && py != tc.want {
				t.Fatalf("the expectation is not CPython's: got %q want %q", py, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want CPython's %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}

func TestDocumentedPositionalRepeatFlagStillAnswers(t *testing.T) {
	// This is a gusty-only extension: CPython's `sorted` takes exactly one positional iterable. The
	// second word is this backend's documented repeat flag, not a reference option, so the CLI row
	// compares the compiled leg against its own pinned answer (roadmap L11.7, ADR 0340).
	for _, tc := range []struct{ name, src, want string }{
		{"positional repeat flag", "print(sorted([3, 1, -2], True))\n", "[3, 1, -2]\n"},
		{"positional flag on singleton", "print(sorted([7], True))\n", "[7]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "unkeyed_repeat.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code != 0 || out != tc.want {
					t.Fatalf("%s printed %q exit %d, want %q (%s)", engine, out, code, tc.want, cliRun(t, engine, path))
				}
			}
		})
	}
}
