package integration

// ternary_arms_test.go — the conditional expression at the CLI (roadmap Gap R.340, ADR 0374).
//
// `a if c else b` was lowered to one LLVM `select`, and a `select` takes both of its operands as
// arguments: the run time has computed both by the moment it picks. Every effect written in an arm —
// a call, a division, an index, an allocation — therefore happened on the road to the answer whatever
// the test said:
//
//	x = 5
//	print(1 if x > 0 else boom())    # the reference answers `1`; the compiled leg printed `boom` first
//	print(1 if x > 0 else 1 // 0)    # the reference answers `1`; the compiled leg died on a
//	                                 #   ZeroDivisionError raised by the arm the test had already rejected
//	def fact(n):                     # the reference answers 120; the compiled leg never returned — the
//	    return 1 if n <= 1 else n * fact(n - 1)   #   recursive call was unconditional, so the base case
//	                                             #   was a value and not a stop, and the artifact died
//	                                             #   with "root stack is full" at exit 2
//
// ADR 0275 had already paid that shape for `and`/`or`; this is the same skeleton under the ternary. The
// rows below are the reference's own answer, asked of the linked binary and of `--aot`: the arm that
// was not chosen contributes nothing to stdout, the arm that was chosen still raises its own class at
// exit 3, and a recursion whose base case is an arm stops.

import (
	"strings"
	"testing"
)

// TestTheUntakenTernaryArmDoesNotRunAtTheCLI is the parity table. Each program's non-taken arm does
// something the program would notice — prints, raises, calls — and the row passes only when the linked
// binary cannot notice it either. The row first checks the reference still answers `want`, so a row
// cannot pass by both the expectation and the compiler drifting together.
func TestTheUntakenTernaryArmDoesNotRunAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, r := range []struct{ name, src, want string }{
		{"a call in the untaken else arm",
			"def boom():\n    print(\"boom\")\n    return 1\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n"},
		{"a call in the untaken then arm",
			"def boom():\n    print(\"boom\")\n    return 1\n\n\nx = 5\nprint(boom() if x < 0 else 2)\n", "2\n"},
		{"a call in the untaken arm of the double half",
			"def boom():\n    print(\"boom\")\n    return 1.5\n\n\nx = 5\nprint(2.5 if x > 0 else boom())\n", "2.5\n"},
		{"a call in the untaken arm of the text half",
			"def boom():\n    print(\"SHOUT\")\n    return \"b\"\n\n\nx = 5\nprint(\"yes\" if x > 0 else boom())\n", "yes\n"},
		{"a call in the untaken arm of the verdict half",
			"def boom():\n    print(\"boom\")\n    return True\n\n\nx = 5\nprint(False if x > 0 else boom())\n", "False\n"},
		{"a call in the untaken arm beside a text arm",
			"def boom():\n    print(\"boom\")\n    return \"b\"\n\n\nx = 5\nprint(3 if x > 0 else boom())\n", "3\n"},
		{"a division by zero in the dead arm",
			"x = 5\nprint(1 if x > 0 else 1 // 0)\n", "1\n"},
		{"a missing key in the dead arm",
			"d = {}\nprint(1 if 1 else d[\"k\"])\n", "1\n"},
		{"an index past the end in the dead arm",
			"xs = [1, 2]\nprint(1 if 1 else xs[9])\n", "1\n"},
		{"a trap inside a function the dead arm names",
			"def boom():\n    return 1 // 0\n\n\nx = 5\nprint(1 if x > 0 else boom())\n", "1\n"},
		{"a slot written only on the other path",
			"def f(flag):\n    if flag:\n        y = 7\n    return (1 if flag else y) + 0\n\n\nprint(f(True))\n", "1\n"},
		{"a lambda whose arm would call",
			"def boom():\n    print(\"boom\")\n    return 1\n\n\nf = lambda x: 1 if x else boom()\nprint(f(True), f(False))\n", "boom\n1 1\n"},
		{"two ternaries in one expression",
			"def boom():\n    print(\"boom\")\n    return 1\n\n\nc = 1\nd = 1\nprint((1 if c else 2) + (3 if d else boom()))\n", "4\n"},
		{"a loop whose arm runs only on the passes that choose it",
			"def boom():\n    print(\"boom\")\n    return 0\n\n\ntotal = 0\nfor i in range(6):\n    total = total + (i if i % 2 == 0 else boom())\nprint(total)\n",
			"boom\nboom\nboom\n6\n"},
		// The rows the `select` road always answered, kept so the branch cannot cost them.
		{"literal arms, run-time test", "x = 3\nprint(1 if x > 2 else 2)\n", "1\n"},
		{"literal arms, the other way", "x = 1\nprint(1 if x > 2 else 2)\n", "2\n"},
		{"constant test", "print(7 if 1 else 9)\n", "7\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: the reference prints %q, the row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "ternary_arms", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (ADR 0166's compiler-bug code): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if strings.Contains(out, "codegen:") {
					t.Fatalf("%s refused what the reference runs: %s", engine, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q — an arm that ran when its test was rejected prints "+
						"from the side the reference never reaches (Gap R.340)", engine, out, want)
				}
			}
		})
	}
}

// TestTheChosenTernaryArmTrapsLikeTheReferenceAtTheCLI is the other half of the branch: choosing an arm
// lazily must not silence the raise the chosen arm produces. The reference dies on every row here; so
// does the compiled binary, at the trap exit and with the reference's class.
func TestTheChosenTernaryArmTrapsLikeTheReferenceAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, msg string }{
		{"the taken arm divides by zero",
			"x = 0\nprint(1 if x > 0 else 1 // 0)\n", "ZeroDivisionError"},
		{"the taken arm indexes past the end",
			"xs = [1, 2]\nprint(1 if 0 else xs[9])\n", "IndexError: list index out of range"},
		{"the taken arm reads a missing key",
			"d = {}\nprint(1 if 0 else d[\"k\"])\n", "KeyError"},
		{"the taken arm calls a function whose body divides by zero",
			"def boom():\n    return 1 // 0\n\n\nx = 5\nprint(boom() if x > 0 else 1)\n", "ZeroDivisionError"},
		{"the taken arm of a text ternary raises",
			"def boom():\n    xs = [1, 2]\n    return xs[9]\n\n\nx = 0\nprint(\"a\" if x > 0 else boom())\n",
			"IndexError: list index out of range"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := writeSrc(t, dir, "ternary_trap", r.src)
			want, pyCode := oracleTrap(t, path)
			if pyCode == 0 {
				t.Fatalf("the reference exited 0 on a program meant to trap:\n%s", want)
			}
			if !strings.Contains(want, r.msg) {
				t.Fatalf("the reference did not raise %q, it says %q", r.msg, want)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				_, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Errorf("%s exited 2 (a compiler bug, ADR 0166) on a program the reference traps:\n%s", engine, combined)
				}
				if code != 3 {
					t.Errorf("%s exited %d where a trap is the answer (ADR 0166): %s", engine, code, combined)
				}
				if !strings.Contains(combined, "Traceback (most recent call last):") {
					t.Errorf("%s printed no traceback:\n%s", engine, combined)
				}
				if !strings.Contains(combined, r.msg) {
					t.Errorf("%s did not raise %q:\n%s", engine, r.msg, combined)
				}
				if strings.Contains(combined, "codegen:") {
					t.Errorf("%s refused what the reference traps:\n%s", engine, combined)
				}
			}
		})
	}
}

// TestTheTernaryRecursionStopsAtTheCLI is the shape that made this a dead artifact rather than a wrong
// answer. The base case of these functions is an arm of the ternary; with the arm emitted beside the
// test, the recursive call happened unconditionally, so the base case was a value and not a stop, the
// root stack filled, and the harness reported the artifact as killed (exit 2, ADR 0166's class).
func TestTheTernaryRecursionStopsAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, r := range []struct{ name, src, want string }{
		{"factorial",
			"def fact(n):\n    return 1 if n <= 1 else n * fact(n - 1)\n\n\nprint(fact(5))\n", "120\n"},
		{"a countdown",
			"def f(n):\n    return 0 if n == 0 else 1 + f(n - 1)\n\n\nprint(f(3), f(0), f(10))\n", "3 0 10\n"},
		{"a double answer",
			"def f(n):\n    return 0.0 if n == 0 else 1.5 + f(n - 1)\n\n\nprint(f(3))\n", "4.5\n"},
		{"a text answer from nested arms",
			"def sign(n):\n    return \"zero\" if n == 0 else (\"pos\" if n > 0 else \"neg\")\n\n\nprint(sign(0), sign(3), sign(-2))\n",
			"zero pos neg\n"},
		{"deep enough to have overflowed the root stack",
			"def f(n):\n    return 0 if n == 0 else 1 + f(n - 1)\n\n\nprint(f(500))\n", "500\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: the reference prints %q, the row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "ternary_rec", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 on `%s` — the recursion never stopped, so the artifact died rather "+
						"than returned (Gap R.340):\n%s", engine, r.name, out)
				}
				if code != 0 {
					t.Errorf("%s exited %d where the reference prints %q: %s", engine, code, want, out)
				}
				if strings.Contains(out, "root stack is full") {
					t.Errorf("%s overflowed the root stack on a recursion the reference completes:\n%s", engine, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q", engine, out, want)
				}
			}
		})
	}
}
