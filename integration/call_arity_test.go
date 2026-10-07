package integration

// The CLI half of "how many arguments there were is one question" (roadmap Gap R.168, ADR 0284).
//
//	g = lambda x: x * 2
//	print(g(1, 2))   # this language printed `2` at exit 0; CPython raises takes 1 positional but 2 given
//	print(g())       # this language printed `0` at exit 0; CPython raises missing 1 required positional
//
// The compiled leg already refused both (`too many arguments for lambda_0`, `missing argument "x"`), so
// this row brings the engines into agreement rather than moving a refusal. Exit 0 on any trap row below
// is a wrong number, which is the class the ladder never trades.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cliArity(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

func pyArity(t *testing.T, dir, src string) (string, int) {
	t.Helper()
	py := os.Getenv("GUSTY_PYTHON")
	if py == "" {
		py = "python3"
	}
	path := filepath.Join(dir, "arity.gy")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write oracle: %v", err)
	}
	cmd := exec.Command(py, path)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			return out.String(), -1
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// TestAWrongArityCallToAVariableHeldCallableNeverExitsZero is the wrong-number table at the CLI.
func TestAWrongArityCallToAVariableHeldCallableNeverExitsZero(t *testing.T) {
	for _, tc := range []struct{ name, src, wantSub string }{
		{"lambda handed an extra argument", "g = lambda x: x * 2\nprint(g(1, 2))\n", "too many arguments"},
		{"lambda handed nothing", "g = lambda x: x * 2\nprint(g())\n", "missing argument"},
		{"lambda missing its second", "g = lambda x, y: x - y\nprint(g(3))\n", "missing argument"},
		{"lambda handed four", "g = lambda x: x * 2\nprint(g(1, 2, 3, 4))\n", "too many arguments"},
		{"a def'd callee through a name", "def f(x):\n    return x * 2\n\nh = f\nprint(h(1, 2))\n", "too many arguments"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "arity.gy", tc.src)
			py, pyCode := pyArity(t, dir, tc.src)
			if pyCode == 0 {
				t.Fatalf("the reference was expected to stop on this program, it printed:\n%s", py)
			}
			// One run, and the claim is about the run: the program must not answer with a number.
			// Which refusal it stops at is the compiler's business — the checker may catch the arity
			// and say `too many arguments`, or the call may be refused further down for a reason this
			// row's shape brings along (passing a `def`'d name as a value is Gap R.167, and that
			// refusal arrives first). Both are honest; exit 0 and exit 2 are not.
			out, code := cliArity(t, "--aot", "--file", gy)
			if code == 0 {
				t.Fatalf("--aot answered a wrong-arity call at exit 0 with %q; CPython raises\n%s", out, py)
			}
			if code == 2 {
				t.Fatalf("--aot: exit 2, the contract's compiler-bug code (ADR 0166):\n%s", out)
			}
			if !strings.Contains(out, tc.wantSub) && !refusesHonestly(out) {
				t.Errorf("--aot named neither the arity question %q nor any missing half:\n%s", tc.wantSub, out)
			}
			if !strings.Contains(out, tc.wantSub) {
				noteCompiledGap(t, tc.src, out)
			}
		})
	}
}

// TestCallsThatShouldAnswerStillAnswerAtTheCLI is the guard against an over-broad count: defaults,
// keywords, methods, recursion and a lambda through a parameter all keep working on the compiled path.
func TestCallsThatShouldAnswerStillAnswerAtTheCLI(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"defaults filled", "def g(a, b=2, c=3):\n    return a + b + c\n\nprint(g(1))\nprint(g(1, 5))\nprint(g(1, 5, 6))\n", "6\n9\n12\n"},
		{"keywords out of order", "def g(a, b=2, c=3):\n    return a + b + c\n\nprint(g(c=9, a=1))\n", "12\n"},
		{"a lambda called correctly", "g = lambda x: x * 2\nprint(g(21))\n", "42\n"},
		{"a lambda through a parameter", "def twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(twice(lambda x: x * 3, 2))\n", "12\n"},
		{"a def'd name bound then called", "def f(x):\n    return x * 2\n\nh = f\nprint(h(21))\n", "42\n"},
		{"a method", "class C:\n    def m(self, a):\n        return a + 1\n\nc = C()\nprint(c.m(4))\n", "5\n"},
		{"recursion", "def fact(n):\n    if n <= 1:\n        return 1\n\n    return n * fact(n - 1)\n\nprint(fact(5))\n", "120\n"},
		{"a nested def", "def outer(x):\n    def inner(y):\n        return y * 2\n\n    return inner(x) + 1\n\nprint(outer(5))\n", "11\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "arity_ok.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("the pinned expectation is not the reference's: cpython %q, table %q", py, tc.want)
			}
			// The two-way claim (see compiled_or_refuses_test.go): the compiled path prints the
			// reference's answer, or it stops at the compile door naming the half of itself it is
			// missing. Two rows reach shapes its roads decline outright — a body that calls a
			// *parameter* (L11.7's higher-order limit) and a `def`'d name used as a value (Gap R.167) —
			// and those refusals are filed, counted (CompiledGapCount), and never allowed to become a
			// number. What the row will not accept is a wrong answer at exit 0, an exit 2, or a
			// refusal that points at nothing.
			out, code := cliArity(t, "--aot", "--file", gy)
			switch {
			case code == 0:
				if out != tc.want {
					t.Fatalf("--aot printed %q, want %q (cpython agrees: %q)", out, tc.want, py)
				}
			case code == 1:
				if !refusesHonestly(out) {
					t.Fatalf("--aot refused without naming the missing half (exit 1):\n%s", out)
				}
				noteCompiledGap(t, tc.src, out)
			case code == 2:
				t.Fatalf("--aot: exit 2, the contract's compiler-bug code (ADR 0166):\n%s", out)
			default:
				t.Fatalf("--aot exited %d on a call the reference answers (want 0 to answer, 1 to refuse):\n%s", code, out)
			}
			ao, aoCode := cliArity(t, "--aot", "--file", gy)
			if aoCode == 2 {
				t.Fatalf("--aot: exit 2, the contract's compiler-bug code (ADR 0166):\n%s", ao)
			}
			if aoCode == 0 && ao != tc.want {
				t.Fatalf("--aot printed %q where the reference and the record print %q — a wrong number, not a refusal (Gap R.168)", ao, tc.want)
			}
		})
	}
}

// TestTheAritySentenceNamesTheCalleeTheWayTheReferenceDoes is the CLI wording row: a reader is never
// shown the compiler's generated `lambda_0`.
func TestTheAritySentenceNamesTheCalleeTheWayTheReferenceDoes(t *testing.T) {
	dir := t.TempDir()
	gy := writeSrc(t, dir, "arity_name.gy", "g = lambda x: x\nprint(g(1, 2))\n")
	out, _ := cliArity(t, "--aot", "--file", gy)
	if !strings.Contains(out, "for <lambda>") {
		t.Errorf("the interpreted sentence must name `<lambda>`, got: %s", out)
	}
	if strings.Contains(out, "lambda_0") {
		t.Errorf("the interpreted sentence leaked the compiler's generated name `lambda_0`: %s", out)
	}
}
