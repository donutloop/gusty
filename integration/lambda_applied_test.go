package integration

// integration/lambda_applied_test.go — the reference leg for the lambda that is APPLIED (roadmap L11.7 ·
// Gap R.265, Gap R.276, ADR 0370).
//
// `pkg/lang/lambda_applied_test.go` decides the shape — which programs the emitter answers, which it
// refuses, and what the emitted module contains. This file asks the question only a run can answer: does the
// compiled program print what `python3` prints? Every expectation is read off the reference at run time by
// `cpythonOut`, so a row cannot pass by agreeing with the thing it is being checked against, and an answer
// copied out of a previous build is not evidence about a language (AGENTS.md's two legs — the record and the
// reference, never two engines).
//
// Three outcomes are distinguished, because the row spans all three:
//
//   - the reference ANSWERS → the linked program and the JIT print the reference's bytes at exit 0;
//   - the door cannot answer (a captured name whose word the value table has no tag for, a float-answering
//     lambda applied inside a closure or a method, a parameter called with a builtin) → an exit-1 refusal
//     naming what is missing and the row that owns it, never exit 2, which ADR 0166 reserves for a compiler
//     bug and which is failed for explicitly in every loop below;
//   - the reference RAISES → the compiled program traps at exit 3 carrying the reference's own sentence.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// lambdaAppliedCLIPrograms is the answered family. Each row is a program `python3` prints for, asked of the
// reference inside the case rather than remembered here.
var lambdaAppliedCLIPrograms = []struct{ name, src string }{
	{
		"the row: an applied lambda calls its own parameter",
		"print((lambda f: f(3))(lambda x: x * 2))\n",
	},
	{
		"the composition the reference's own tutorials write",
		"print((lambda f, g, x: f(g(x)) + g(f(x)))(lambda v: v + 1, lambda v: v * 2, 3))\n",
	},
	{
		"one lambda, called twice at two arguments",
		"print((lambda f: f(1) + f(2))(lambda x: x * 3))\n",
	},
	{
		"a parameter that answers a float",
		"print((lambda x: x * 2.5)(3))\n",
	},
	{
		"a float division inside the inner body",
		"print((lambda m: m / 2)(3))\n",
	},
	{
		"the defunctionalised road: a lambda bound to a name, then applied",
		"twice = lambda x: x * 2\nprint(twice(21))\n",
	},
	{
		"a lambda applied inside a def body reads the parameter",
		"def f(x):\n    return (lambda y: y * 2)(x)\n\nprint(f(4))\n",
	},
	{
		"a lambda applied inside a def body reads a local",
		"def outer(x):\n    y = x + 1\n    return (lambda z: z + y)(2)\n\nprint(outer(3))\n",
	},
	{
		"a lambda inside a lambda reads the outer parameter",
		"print((lambda x: (lambda y: x + y)(1))(2))\n",
	},
	{
		"a lambda applied in a def that reads two of the enclosing frame's names",
		"def outer():\n    a = 7\n    b = 1\n    def bump(n):\n        return (lambda m: m + a + b)(n)\n    return bump(4)\n\nprint(outer())\n",
	},
	{
		"a lambda applied in a two-deep def reads both frames",
		"def add(a):\n    def inner(b):\n        return (lambda c: c + a + b)(1)\n    return inner(2)\n\nprint(add(3))\n",
	},
	{
		"a lambda applied inside a method",
		"class Foo:\n    def bar(self, x):\n        return (lambda y: y + 1)(x)\n\nprint(Foo().bar(3))\n",
	},
	{
		"a lambda applied inside a comprehension body",
		"print([(lambda k: k * 2)(x) for x in [1, 2, 3]])\n",
	},
	{
		"a lambda written as a hook, beside a def with the same body",
		"def twice(x):\n    return x * 2\n\nprint(twice(2), (lambda x: x * 2)(2))\n",
	},
	{
		"a higher-order def whose argument is a lambda that calls its own parameter",
		"def apply1(fn, v):\n    return fn(v)\n\nprint(apply1(lambda x: (lambda y: y * 2)(x), 4))\n",
	},
	{
		"the control: the same body written as a def",
		"def twice(x):\n    return x * 2\n\ndef use(f):\n    return f(3)\n\nprint(use(twice))\n",
	},
}

// lambdaAppliedRefusalPrograms is the family the door cannot answer: each `want` is the sentence half the
// refusal must carry, and every row is also checked to name L11.1 — the memory-model row that owns the word
// a value would travel in. The reference ANSWERS all of them, which is the point.
var lambdaAppliedRefusalPrograms = []struct{ name, src, want string }{
	{
		"a capture whose value is a container",
		"def f():\n    xs = [1, 2]\n    return (lambda: len(xs) + 1)()\n\nprint(f())\n",
		"L11.7",
	},
	{
		"a capture whose value is a text",
		"def f():\n    t = \"hey\"\n    return (lambda: t)()\n\nprint(f())\n",
		"a text",
	},
	{
		"a capture whose value is a bool",
		"def f():\n    b = True\n    return (lambda: b)()\n\nprint(f())\n",
		"a bool",
	},
	{
		"a capture three frames up, whose kind no call site states",
		"print((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1))\n",
		"L11.7",
	},
	{
		"a float-answering lambda applied inside a method",
		"class Foo:\n    def bar(self, x):\n        return (lambda m: m / 2)(x)\n\nprint(Foo().bar(3))\n",
		"L11.6",
	},
	{
		"a called parameter asked to be a builtin",
		"print((lambda f: f(3))(abs))\n",
		"is called but is not a function",
	},
	{
		"an application that omits the defaulted parameter of the lambda it applies",
		"print((lambda f, y=10: f(y))(lambda x: x + 1))\n",
		"is called but is not a function",
	},
}

// TestAnAppliedLambdaPrintsWhatTheReferencePrints is the reference leg at the CLI: the linked program and
// the in-process JIT are asked beside `python3`.
func TestAnAppliedLambdaPrintsWhatTheReferencePrints(t *testing.T) {
	t.Parallel()
	for _, tc := range lambdaAppliedCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "lambda_applied.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, cliRun(t, engine, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheAppliedLambdaFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers has an answer on record, and a record that disappeared fails here rather than passing by having
// nothing to compare against. The records were written from the reference by tools/recmerge, never from this
// backend — the compiled leg is the thing under test, so it is never allowed to write its own answers.
func TestTheAppliedLambdaFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range lambdaAppliedCLIPrograms {
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

// TestWhatTheAppliedLambdaDoorRefusesExitsOneAndNamesTheRow pins the other half: the family the backend
// cannot answer is a front-end refusal at exit 1, worded with the missing thing and the roadmap row, and it
// is never exit 2 and never a value.
func TestWhatTheAppliedLambdaDoorRefusesExitsOneAndNamesTheRow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range lambdaAppliedRefusalPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The reference has to ANSWER this source, or the row describes the wrong program: the point
			// of the refusal family is that it is a door that is shut, not a program that is broken.
			if _, ok := cpythonPlainOut(t, dir, tc.src); !ok {
				t.Skip("no python3 available to cross-check")
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "lambda_applied_refusal", tc.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("exit 2 on a program the reference answers — ADR 0166 reserves it for a compiler bug:\n%s", out)
				}
				if code == 0 {
					t.Fatalf("answered %q with no refusal; the rows own this shape and the build before ADR 0370 got it WRONG", out)
				}
				if code != 1 {
					t.Fatalf("exited %d, want 1 (a front-end refusal): %s", code, out)
				}
				if !strings.Contains(out, tc.want) {
					t.Fatalf("the refusal does not say what is missing (%q): %s", tc.want, out)
				}
				if !strings.Contains(out, "roadmap") {
					t.Fatalf("the refusal names no roadmap row: %s", out)
				}
			}
		})
	}
}
