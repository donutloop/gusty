package integration

// integration/order_kind_test.go — an ordering between two kinds that do not order against each
// other stops the program the way the reference stops it, at the CLI, on the compiled path
// (roadmap Gap R.85 and Gap R.202, ADR 0382).
//
// Each row is one source run three ways: CPython, and `gustyc --file` on each CLI engine. The
// reference's answer is never written into this file by hand — every expectation is read out of
// `python3` in the row itself — because a row that quotes a remembered sentence is a row that
// documents the bug as soon as the sentence changes.
//
// The two verdict classes:
//
//   - an ordering the reference answers, answers at exit 0 with byte-identical stdout;
//   - an ordering the reference stops, stops with the same class and the same sentence at exit 3,
//     the contract's runtime-error class, with a traceback and no `codegen:` refusal;
//   - exit 2 — the code ADR 0166 reserves for a compiler bug — fails every row here.
//
// The filed-not-fixed table at the bottom is what this door still cannot name: an operand whose kind
// is a run-time fact has no word to travel in (L11.1), so the raise cannot name both types and the
// door stays out. Those rows pin the reference's sentence beside the compiled leg's silent answer,
// and they are roadmap Gap R.354 rather than a green tick.

import (
	"strings"
	"testing"
)

type orderCLI struct {
	name, src, sentence string
}

func orderCLITraps() []orderCLI {
	return []orderCLI{
		{"an int against None", "print(1 < None)\n", "TypeError: '<' not supported between instances of 'int' and 'NoneType'"},
		{"None against an int", "print(None < 1)\n", "TypeError: '<' not supported between instances of 'NoneType' and 'int'"},
		{"None against None", "print(None >= None)\n", "TypeError: '>=' not supported between instances of 'NoneType' and 'NoneType'"},
		{"a float against None", "print(1.5 <= None)\n", "TypeError: '<=' not supported between instances of 'float' and 'NoneType'"},
		{"a verdict against None", "print(True > None)\n", "TypeError: '>' not supported between instances of 'bool' and 'NoneType'"},
		{"a text against a float", "print(\"a\" < 1.5)\n", "TypeError: '<' not supported between instances of 'str' and 'float'"},
		{"a float against a text", "print(1.5 >= \"a\")\n", "TypeError: '>=' not supported between instances of 'float' and 'str'"},
		{"a text against an int", "print(\"a\" < 1)\n", "TypeError: '<' not supported between instances of 'str' and 'int'"},
		{"an int against a text", "print(1 <= \"a\")\n", "TypeError: '<=' not supported between instances of 'int' and 'str'"},
		{"a name bound to None", "x = None\nprint(1 < x)\n", "TypeError: '<' not supported between instances of 'int' and 'NoneType'"},
		{"a name bound to a text against a float", "s = \"a\"\nprint(s > 1.5)\n", "TypeError: '>' not supported between instances of 'str' and 'float'"},
		{"a call that answers the void", "def f():\n    return None\n\nprint(1 < f())\n", "TypeError: '<' not supported between instances of 'int' and 'NoneType'"},
		{"a builtin's count against None", "print(len(\"abc\") < None)\n", "TypeError: '<' not supported between instances of 'int' and 'NoneType'"},
		{"a verdict against a list names 'bool'", "print(True < [1])\n", "TypeError: '<' not supported between instances of 'bool' and 'list'"},
		{"None against a list names 'NoneType'", "print(None < [1])\n", "TypeError: '<' not supported between instances of 'NoneType' and 'list'"},
		{"a text against a list names 'str'", "print(\"a\" > [1])\n", "TypeError: '>' not supported between instances of 'str' and 'list'"},
		{"a float against a dict", "print(1.5 < {\"a\": 1})\n", "TypeError: '<' not supported between instances of 'float' and 'dict'"},
		{"in a ternary test", "print(\"y\" if None < 1 else \"n\")\n", "TypeError: '<' not supported between instances of 'NoneType' and 'int'"},
		{"in a condition", "x = None\nif x < 1:\n    print(\"y\")\n", "TypeError: '<' not supported between instances of 'NoneType' and 'int'"},
		{"in a chain, on the link that fails", "print(1.5 < \"a\" < 2)\n", "TypeError: '<' not supported between instances of 'float' and 'str'"},
	}
}

func orderCLIAnswers() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"two ints", "print(1 < 2)\n", "True\n"},
		{"a float against an int", "print(2.5 > 1)\n", "True\n"},
		{"two texts", "print(\"a\" < \"b\")\n", "True\n"},
		{"two texts the wrong way", "print(\"b\" <= \"a\")\n", "False\n"},
		{"equality with the void is total", "print(1 == None)\n", "False\n"},
		{"a container against its own kind", "a = [1]\nb = [2]\nprint(a < b)\n", "True\n"},
		{"a chain", "print(1 < 2 < 3)\n", "True\n"},
		{"a chain the middle fails", "print(2 < 1 < 3)\n", "False\n"},
		{"a while loop over numbers", "i = 0\nwhile i < 3:\n    print(i)\n    i = i + 1\n", "0\n1\n2\n"},
		{"a slot of a literal list", "xs = [3, 4]\nprint(xs[1] > 3)\n", "True\n"},
		{"a dict entry", "d = {\"k\": 5}\nprint(d[\"k\"] >= 5)\n", "True\n"},
	}
}

// TestTheReferenceStopsOnEveryOrderingThisFilePins reads the expectation out of CPython first: each
// row must be a program the reference stops, with the sentence pinned in the table.
func TestTheReferenceStopsOnEveryOrderingThisFilePins(t *testing.T) {
	t.Parallel()
	for _, tc := range orderCLITraps() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if ok || !strings.Contains(py, tc.sentence) {
				t.Fatalf("the reference was expected to stop with %q, said %q (ok %v)\nsrc: %s", tc.sentence, py, ok, tc.src)
			}
		})
	}
}

// TestTheCompiledLegRaisesTheReferenceSentenceOnEveryOrdering is the row at the CLI.
func TestTheCompiledLegRaisesTheReferenceSentenceOnEveryOrdering(t *testing.T) {
	t.Parallel()
	for _, tc := range orderCLITraps() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "order_trap.gy", tc.src)
			for _, engine := range cliEngines {
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

// TestAnOrderingTheReferenceAnswersStillAnswersAtTheCLI is the ladder's other side: nothing that
// compiled and answered before the door exists may come back as a raise or a refusal.
func TestAnOrderingTheReferenceAnswersStillAnswersAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range orderCLIAnswers() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "order_answer.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q", py, ok, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 on an ordinary program (ADR 0166):\n%s", engine, out)
				}
				if code != 0 {
					t.Errorf("%s: exit %d, want 0\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Errorf("%s printed %q, want %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestTheOrderingTrapIsCatchableOnBothLegs: the raise is a language event — `except TypeError:`
// reaches it, the arm runs, the statement after the try runs, and the exit class is the ordinary one.
func TestTheOrderingTrapIsCatchableOnBothLegs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"the void against a number", "try:\n    print(1 < None)\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")\n", "caught\nafter\n"},
		{"a text against a float", "try:\n    print(\"a\" < 1.5)\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a name bound to None", "x = None\ntry:\n    print(x > 1)\nexcept TypeError:\n    print(\"caught\")\nprint(\"after\")\n", "caught\nafter\n"},
		{"in a condition, inside the try", "x = None\ntry:\n    if 1 < x:\n        print(\"y\")\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a wrong arm does not catch it", "try:\n    print(1 < None)\nexcept ValueError:\n    print(\"wrong\")\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"a text against a list", "try:\n    print(\"a\" < [1])\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "order_catch.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q", py, ok, tc.want)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code != 0 {
					t.Errorf("%s: exit %d, want 0 (the program handled it)\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Errorf("%s printed %q, want %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// TestWhatTheOrderingDoorStillCannotNameIsFiledNotFixed is Gap R.354 at the CLI: where only one side
// has a kind the compiler can read, the door stays out and the comparison road answers the two words
// it was handed. The rows pin the reference's sentence beside the compiled leg's answer, so the
// divergence cannot move and cannot be closed by deleting the row.
func TestWhatTheOrderingDoorStillCannotNameIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, compiledAnswer, sentence string }{
		{
			"the void against an int variable",
			"i = 0\nwhile None < i:\n    print(i)\n    i = i + 1\n",
			"",
			"TypeError: '<' not supported between instances of 'NoneType' and 'int'",
		},
		{
			"the void against a parameter",
			"def g(v):\n    return v < None\n\nprint(g(3))\n",
			"False\n",
			"TypeError: '<' not supported between instances of 'int' and 'NoneType'",
		},
		{
			"a text against a call the scan cannot describe",
			"def pick(c):\n    if c:\n        return \"z\"\n    return 7\n\nprint(1.5 < pick(1))\n",
			"False\n",
			"TypeError: '<' not supported between instances of 'float' and 'str'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "order_filed.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if ok || !strings.Contains(py, tc.sentence) {
				t.Fatalf("the filed shape is not the reference's trap with that sentence any more: %q\nwant %q", py, tc.sentence)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 on a program the reference merely stops on (ADR 0166):\n%s", engine, out)
				}
				if code != 0 {
					t.Errorf("%s: exit %d, want the silent 0 that Gap R.354 pins\n%s", engine, code, out)
				}
				if out != tc.compiledAnswer {
					t.Errorf("%s printed %q, pinned %q\nthe reference raises: %s\nsrc: %s",
						engine, out, tc.compiledAnswer, tc.sentence, tc.src)
				}
			}
		})
	}
}
