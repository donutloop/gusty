package integration

// integration/negation_kind_test.go — the unary minus asks its operand's kind at the CLI, against the
// reference, on both engines (roadmap Gaps R.89 and R.137, ADR 0266).
//
// Every row is the same source run three ways: CPython, `gustyc --file <path> --interp`, and `gustyc
// --file <path> -aot`. The legs are forced explicitly — a bare `--file` is the interpreter's default, and
// `-aot` written after the path becomes the flag's value rather than the compiled leg.
//
// The two classes of verdict are kept apart on purpose:
//
//   - a shape with a sign answers, on every engine, at exit 0;
//   - a shape without one *stops*, on every engine: the reference with its traceback at exit 1, this
//     toolchain with the same sentence in the same traceback at exit 3 (the contract's runtime-error
//     class). A row that printed a number instead would be the bug this cycle closed, and exit 2 — the
//     contract's "the compiler is broken" code — fails any row here, including the trap table, because a
//     half-finished raise is exactly what reaches for it (ADR 0166).

import (
	"strings"
	"testing"
)

// negationTrap is one shape the reference stops on, with the sentence it stops with.
type negationTrap struct {
	name, src, sentence string
}

func negationTraps() []negationTrap {
	return []negationTrap{
		{"a text literal", "print(-\"hi\")\n", "TypeError: bad operand type for unary -: 'str'"},
		{"a name holding a text", "x = \"hi\"\nprint(-x)\n", "TypeError: bad operand type for unary -: 'str'"},
		{"a call that answers text", "def f():\n    return \"hi\"\n\nprint(-f())\n", "TypeError: bad operand type for unary -: 'str'"},
		{"a parameter the caller filled with text", "def g(v):\n    print(-v)\n\ng(\"hi\")\n", "TypeError: bad operand type for unary -: 'str'"},
		{"str()'s answer", "print(-str(1))\n", "TypeError: bad operand type for unary -: 'str'"},
		{"a character read out of a text", "s = \"abc\"\nprint(-s[1])\n", "TypeError: bad operand type for unary -: 'str'"},
		{"an element of a list of texts", "xs = [\"a\"]\nprint(-xs[0])\n", "TypeError: bad operand type for unary -: 'str'"},
		{"an element of a list of texts, by a computed index", "xs = [\"a\", \"b\"]\ni = 1\nprint(-xs[i])\n", "TypeError: bad operand type for unary -: 'str'"},
		{"a slot the program built rather than spelled", "xs = []\nxs.append(\"hi\")\nprint(-xs[0])\n", "TypeError: bad operand type for unary -: 'str'"},
		{"a dict value that is text", "d = {\"k\": \"v\"}\nprint(-d[\"k\"])\n", "TypeError: bad operand type for unary -: 'str'"},
		{"None", "print(-None)\n", "TypeError: bad operand type for unary -: 'NoneType'"},
		{"a list literal — the shape that had llc rejecting the module", "print(-[1, 2])\n", "TypeError: bad operand type for unary -: 'list'"},
		{"a list variable", "xs = [1, 2]\nprint(-xs)\n", "TypeError: bad operand type for unary -: 'list'"},
		{"a dict literal", "print(-{\"a\": 1})\n", "TypeError: bad operand type for unary -: 'dict'"},
		{"a set literal", "print(-{1, 2})\n", "TypeError: bad operand type for unary -: 'set'"},
		{"a set variable", "xs = {1, 2}\nprint(-xs)\n", "TypeError: bad operand type for unary -: 'set'"},
		{"an instance names its own class", "class Token:\n    text = \"t\"\n\nprint(-Token())\n", "TypeError: bad operand type for unary -: 'Token'"},
		{"a text negated inside a float expression", "print(-\"hi\" + 1.5)\n", "TypeError: bad operand type for unary -: 'str'"},
	}
}

// TestTheReferenceStopsOnEveryShapeThisFilePins is the oracle's own vote: each row must be a program
// CPython *stops* on, with the sentence pinned below. It runs first, because a row whose expectation is
// not the reference's is a test that documents the bug as if it were the spec.
func TestTheReferenceStopsOnEveryShapeThisFilePins(t *testing.T) {
	for _, tc := range negationTraps() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeSrc(t, dir, "negation_trap.py", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if ok || !strings.Contains(py, tc.sentence) {
				t.Fatalf("the reference was expected to stop with %q, said %q (ok %v)\nsrc: %s", tc.sentence, py, ok, tc.src)
			}
		})
	}
}

// TestBothEnginesRaiseTheReferenceSentenceOnEveryTrap is the row: exit 3, the runtime-error class, and the
// reference's own sentence in the traceback — on the interpreted leg and the compiled one.
func TestBothEnginesRaiseTheReferenceSentenceOnEveryTrap(t *testing.T) {
	for _, tc := range negationTraps() {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "negation_trap.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliReport(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 3 {
					t.Errorf("%s: exit %d, want 3 (the runtime-error class)\n%s", engine, code, out)
				}
				if !strings.Contains(out, tc.sentence) {
					t.Errorf("%s raised with %q, want the reference's %q", engine, out, tc.sentence)
				}
				if !strings.Contains(out, "Traceback (most recent call last):") {
					t.Errorf("%s printed no traceback:\n%s", engine, out)
				}
				if strings.Contains(out, "COMPILER BUG") || strings.Contains(out, "LLVM ERROR") || strings.Contains(out, "codegen:") {
					t.Errorf("%s turned the trap into a toolchain failure:\n%s", engine, out)
				}
			}
		})
	}
}

// TestTheNegationTrapIsCatchableOnBothEngines: the reference's TypeError is an event a program can catch,
// and so is this one — the arm runs, the trailer runs, and the exit class is the ordinary one.
func TestTheNegationTrapIsCatchableOnBothEngines(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			"a text literal",
			"try:\n    print(-\"hi\")\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")\n",
			"caught\nafter\n",
		},
		{
			"a list literal, which used to be an exit-2 module",
			"try:\n    print(-[1, 2])\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a slot the literal describes",
			"xs = [\"a\"]\ntry:\n    print(-xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"a slot only the object describes",
			"xs = []\nxs.append(\"hi\")\ntry:\n    print(-xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"an instance",
			"class Token:\n    text = \"t\"\n\ntry:\n    print(-Token())\nexcept TypeError:\n    print(\"caught\")\n",
			"caught\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "negation_catch.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q", py, ok, tc.want)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestNegationOfANumberAnswersOnEveryEngine is the half that must not regress: the raise cannot be bought
// by breaking the negations the reference answers.
func TestNegationOfANumberAnswersOnEveryEngine(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"an int literal", "print(-7)\n", "-7\n"},
		{"a float literal", "print(-1.5)\n", "-1.5\n"},
		{"a bool is a number", "print(-True)\n", "-1\n"},
		{"an int variable", "x = 5\nprint(-x)\n", "-5\n"},
		{"a float variable", "x = 2.5\nprint(-x)\n", "-2.5\n"},
		{"a slot of a literal list", "xs = [3, 4]\nprint(-xs[1])\n", "-4\n"},
		{"a float slot keeps the float", "xs = [3.5, 4]\nprint(-xs[0])\n", "-3.5\n"},
		{"a slot of a container the program built", "xs = []\nxs.append(9)\nprint(-xs[0])\n", "-9\n"},
		{"a dict value", "d = {\"k\": 6}\nprint(-d[\"k\"])\n", "-6\n"},
		{"true division of a negation", "print(-7 / 2)\n", "-3.5\n"},
		{"a loop variable over a list of numbers", "t = 0\nfor v in [1, 2, 3]:\n    t = t - v\nprint(t)\n", "-6\n"},
		{"a parameter", "def f(x):\n    return -x\n\nprint(f(4))\n", "-4\n"},
		{"a comprehension", "print([-v for v in [1, 2]])\n", "[-1, -2]\n"},
		{"a text measured rather than negated", "s = \"abc\"\nprint(len(s))\n", "3\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "negation_ok.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 || out != tc.want {
					t.Errorf("%s: exit %d, stdout %q, want the reference's %q\nsrc: %s", engine, code, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestTheNegationCorpusProgramPrintsWhatTheLedgerSays runs the registered conformance file through both engines
// and against the reference: programs/negation_names_the_kind.gy is `oracle: match`, which is a claim
// about all three engines, and this is where it is checked rather than asserted.
func TestTheNegationCorpusProgramPrintsWhatTheLedgerSays(t *testing.T) {
	src := readProgram(t, "negation_names_the_kind.gy")
	want := "-7\n-1.5\n-1\n-7\n-4\n" +
		"a text has no sign\nnone has no sign\na list has no sign\na dict has no sign\na set has no sign\n" +
		"a text slot has no sign\nan appended text has no sign\nan instance has no sign\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "negation_names_the_kind.gy", src)
	if py, ok := cpythonPlainOut(t, dir, src); !ok || py != want {
		t.Fatalf("the ledger's expectation is not the reference's: %q (ok %v)", py, ok)
	}
	for _, engine := range []string{"--interp", "--aot"} {
		out, code := cliRunCode(t, engine, "--file", gy)
		if code == 2 {
			t.Fatalf("%s: exit 2 (ADR 0166):\n%s", engine, out)
		}
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, stdout %q, want %q", engine, code, out, want)
		}
	}
}
