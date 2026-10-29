package integration

// integration/lambda_answer_kind_test.go — the reference leg for the answer of an applied `lambda` that
// spells its kind (roadmap L11.7 · Gap R.384, ADR 0402).
//
// `pkg/lang/lambda_answer_test.go` decides the shape — which bodies the pass turns into a declaration, which
// printers the emitted module carries, what the pass's own account says. This file asks the question only a
// run can answer: does the LINKED program print what `python3` prints? Every expectation is read off the
// reference at run time by `cpythonOut`, so a row cannot pass by agreeing with the thing it is being checked
// against (AGENTS.md's two legs — the record and the reference, never two engines).
//
// Three outcomes are distinguished, because the row spans all three:
//
//   - the reference ANSWERS with a kind the answer tables register → the linked program prints the
//     reference's bytes at exit 0, and never exit 2, which ADR 0166 reserves for a compiler bug;
//   - the reference ANSWERS with a kind the one-word road renders correctly (a number, a float) → the same
//     bytes, and the row says so, because the pass deliberately does not touch them;
//   - the reference ANSWERS with a kind no door registers yet (a body that is a CALL to a text-answering
//     `def`, an interpolation body, a void compared with `None`) → today's wrong byte is PINNED beside the
//     reference's and the roadmap row that owns it is named. A pinned wrong answer is not a debt forgiven;
//     a row that stops being true fails, and the case has to be deleted rather than left to certify a leg
//     that no longer exists.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// lambdaAnswerKindPrograms is the answered family. Each row is a program `python3` prints for, asked of the
// reference inside the case rather than remembered here.
func lambdaAnswerKindPrograms() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"the row: a verdict", "print((lambda: True)())\n"},
		{"the other verdict", "print((lambda: False)())\n"},
		{"the void", "print((lambda: None)())\n"},
		// The same answer asked against the void: **Gap R.388**, paid by **ADR 0427** — the comparison fold asks
		// the one predicate the print road asks, so one `lambda` cannot be answered twice. The pin these rows
		// replace is DELETED, not edited.
		{"the void compared with None", "print((lambda: None)() is None)\n"},
		{"the void compared with None, equality spelling", "print((lambda: None)() == None)\n"},
		{"a text", "print((lambda: \"hi\")())\n"},
		{"the verdict bound to a name", "v = (lambda: True)()\nprint(v)\n"},
		{"the text bound to a name, then measured and cased", "t = (lambda: \"hi\")()\nprint(t, len(t), t.upper())\n"},
		{"str of a text answer", "print(str((lambda: \"hi\")()))\n"},
		{"repr of a text answer", "print(repr((lambda: \"hi\")()))\n"},
		{"a text answer concatenated", "print((lambda: \"a\")() + \"!\")\n"},
		{"a text answer repeated", "print((lambda: \"hi\")() * 2)\n"},
		{"a text answer as a membership needle", "print((lambda: \"hi\")() in [\"hi\", \"x\"])\n"},
		{"a text answer as a loop iterable", "for c in (lambda: \"ab\")():\n    print(c)\n"},
		{"a text answer as a text method receiver", "print((lambda: \"hi\")().upper())\n"},
		{"a text answer asked of a call", "def echo(v):\n    return v\n\nprint(echo((lambda: \"hi\")()))\n"},
		{"the verdict as a condition", "if (lambda: True)():\n    print(\"yes\")\n"},
		{"the verdict chosen by `or`", "print((lambda: False)() or \"fallback\")\n"},
		{"the truthiness builtin over an empty text answer", "print(bool((lambda: \"\")()))\n"},
		{"an equality the verdict answers", "print((lambda: True)() == True)\n"},
		{"three kinds in one print", "print((lambda: True)(), (lambda: 2)())\n"},
		{"the void twice in one print", "print((lambda: None)(), (lambda: None)())\n"},
		{"two answers as list elements", "print([(lambda: True)(), (lambda: None)()])\n"},
		{"a text as a list element, read back", "xs = [(lambda: \"hi\")()]\nprint(xs[0])\n"},
		{"a text as a dict value", "print({\"k\": (lambda: \"hi\")()})\n"},
		{"a text as a dict key", "print({(lambda: \"k\")(): 1})\n"},
		{"a text as a set member", "print({(lambda: \"k\")()})\n"},
		{"the answer with a parameter filled", "print((lambda x: None)(1))\n"},
		{"the answer with a default filled", "print((lambda x=3: None)())\n"},
		{"a comprehension over the verdict", "print([v for v in [(lambda: True)()]])\n"},
		{"the answer from inside a def", "def answerer(x):\n    return (lambda: True)()\n\nprint(answerer(1))\n"},
		{"the answer from inside a method", "class Holder:\n    def show(self):\n        print((lambda: False)())\n\nHolder().show()\n"},
		{"the void in a `match` arm", "match 1:\n    case 1:\n        print((lambda: None)())\n"},
		{"the answer in a `try` and in a `finally`", "try:\n    print((lambda: None)())\nfinally:\n    print((lambda: True)())\n"},
		{"the f-string field of a verdict", "print(f\"{(lambda: True)()}\")\n"},
		// The controls: kinds the one-word road renders CORRECTLY, which the pass leaves exactly where the
		// source put them. They are on the reference leg for the same reason the answered family is — an answer
		// nobody measures is an answer nobody checked.
		{"a number answer keeps its number", "print((lambda: 5)())\n"},
		{"a float answer keeps its float", "print((lambda: 2.5)())\n"},
		{"a number and a text answer in one program", "print((lambda: 5)())\nprint((lambda: \"hi\")())\n"},
		{"the container answer beside the text answer", "print((lambda: [1, 2])())\nprint((lambda: \"hi\")())\n"},
	}
}

// lambdaAnswerKindPinned are the shapes this row MEASURED and does not own, each with the byte the linked
// program prints today beside the reference's answer, and the roadmap row that owns the missing road. The
// legs are asserted separately because they disagree for the interpolation: the CLI links and prints the
// payload, the leg that verifies never reaches the run time at all (roadmap Gap R.333).
func lambdaAnswerKindPinned() []struct {
	name  string
	src   string
	today string // what the LINKED program prints today (pinned, not forgiven)
	gap   string
} {
	return []struct {
		name  string
		src   string
		today string
		gap   string
	}{
		{
			"a body that is a CALL to a text-answering def answers the @str_tab position",
			"def inner():\n    return \"hi\"\n\nprint((lambda: inner())())\n",
			"0\n",
			"Gap R.387",
		},
		{
			"a body that is a CALL to a verdict-answering def answers the payload",
			"def ib():\n    return True\n\nprint((lambda: ib())())\n",
			"1\n",
			"Gap R.387",
		},
		{
			"a body that is a CALL to a void-answering def answers the void word",
			"def iN():\n    return None\n\nprint((lambda: iN())())\n",
			"0\n",
			"Gap R.387",
		},
		{
			"a body that is an interpolation answers the void word",
			"print((lambda: f\"hi{2}\")())\n",
			"0\n",
			"Gap R.387, Gap R.333",
		},
		// Gap R.331's half is PAID — **ADR 0461** — and its pin is DELETED rather than edited, on this file's own
		// rule: `t = "hey"` / `print((lambda: t)())` prints the reference's `hey`, because the pass now folds a read
		// of a never-rebound module literal into the body and the answer is spelled where the doors can read it. The
		// program, and the family around it, stands in
		// `integration/lambda_module_read_test.go::TestTheFoldedModuleReadAnswersWhatCPythonAnswersAtTheCLI`.
		//
		// Gap R.388 is PAID — **ADR 0427** — and the pin that stood there is DELETED rather than edited: the
		// program prints the reference's `True` and stands in the answered family above.
		//
		// Gap R.171's HAND-OFF half is PAID — **ADR 0445** — and its pin is DELETED on the same rule:
		// `def f(v): return v` called with `(lambda: None)()` prints the reference's `None`, and the program
		// stands in the answered family of
		// `integration/forwarded_answer_test.go::TestAHandOffAnswerWearsItsArgumentsKindThroughTheCLI`, record leg
		// included. What **Gap R.171** still owns is a body that COMPUTES from the void argument
		// (`def d(y=None): return y is None` answers `False`), pinned beside its own today's byte above.
	}
}

// TestAnAppliedLambdasAnswerPrintsWhatTheReferencePrints is the reference leg at the CLI.
func TestAnAppliedLambdasAnswerPrintsWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAnswerKindPrograms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "lambda_answer_kind.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheLambdaAnswerKindFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers has an answer on record, and a record that disappeared fails here rather than passing by having
// nothing to compare against. The records were written from the reference by tools/recmerge, never from this
// backend — the compiled leg is the thing under test, so it is never allowed to write its own answers.
func TestTheLambdaAnswerKindFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range lambdaAnswerKindPrograms() {
		src := tc.src
		name := tc.name
		t.Run(name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestWhatThisRowMeasuredAndDoesNotOwnIsPinnedWithTheReferencesAnswer holds the shapes the pass declines.
// Two assertions per row: the reference answers (so the row describes a program, not a broken one), and the
// linked program prints EXACTLY the byte pinned here. A row that stops being true because the program now
// answers the reference's bytes FAILS — which is what keeps a pinned wrong answer from turning into a rumour.
func TestWhatThisRowMeasuredAndDoesNotOwnIsPinnedWithTheReferencesAnswer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range lambdaAnswerKindPinned() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "lambda_answer_kind_owed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want == tc.today {
				t.Fatalf("%s: the program now prints the reference's %q — the row closed and this pin has to be DELETED, "+
					"with the program moved into the answered table\n%s", tc.gap, want, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 on a program the reference prints — ADR 0166 reserves it for a compiler bug, and this "+
						"row files it as %s rather than shipping it:\n%s", engine, tc.gap, out)
				}
				if out != tc.today {
					t.Fatalf("%s: %s pinned today's answer as %q, the program printed %q (the reference prints %q)\n%s",
						engine, tc.gap, tc.today, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheLambdaAnswerKindProgramIsTheReferenceLineForLine is the conformance corpus's own claim, asked of the
// file rather than of a table: `integration/programs/lambda_answer_kind.gy` prints the reference's bytes for
// every line, with no line dropped between the two.
func TestTheLambdaAnswerKindProgramIsTheReferenceLineForLine(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("lambda_answer_kind")
	want, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "lambda_answer_kind.gy", src)
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", engine, code, out)
		}
		if out != want {
			wl, gl := strings.Split(want, "\n"), strings.Split(out, "\n")
			for i := 0; i < len(wl) || i < len(gl); i++ {
				var w, g string
				if i < len(wl) {
					w = wl[i]
				}
				if i < len(gl) {
					g = gl[i]
				}
				if w != g {
					t.Fatalf("%s differs on line %d: printed %q, CPython prints %q", engine, i+1, g, w)
				}
			}
		}
	}
}
