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
		// roadmap L11.7 · Gap R.330 (ADR 0380): the name belongs to a frame TWO lambdas above. Each frame
		// forwards it one step — the middle body already has `y` as a parameter and gains only `x` — and the
		// deepest body reads both. Its refusal row below is DELETED, as that row's Definition of done says.
		"a name two lambdas up, forwarded by the frame in between",
		"print((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1))\n",
	},
	{
		"four binding forms deep",
		"print((lambda a: (lambda b: (lambda c: (lambda d: a + b + c + d)(4))(3))(2))(1))\n",
	},
	{
		"a capture three frames deep whose owner is a def local",
		"def f():\n    x = 1\n    return (lambda y: (lambda z: x + y + z)(100))(10)\n\nprint(f())\n",
	},
	{
		// The middle frame SHADOWS the outer name: the reference reads the inner binding, and a capture road
		// that forwarded the shadowed name would have minted a second parameter of the same spelling.
		"a capture the middle frame shadows",
		"print((lambda x: (lambda x: (lambda z: x + z)(1))(5))(99))\n",
	},
	{
		// The owner is a `def` two frames up and the frames in between are lambdas: this is the shape the
		// nested-`def` lift (ADR 0245) already carried for a `def`, and the lambda road now carries it too.
		"a closure-captured name read by a body two lambdas deep",
		"def add(a):\n    def inner(b):\n        return (lambda c: (lambda d: c + d + a + b)(1))(2)\n    return inner(10)\n\nprint(add(100))\n",
	},
	{
		"a closure-captured name beside one the middle lambda binds",
		"def outer():\n    total = 0\n    def bump(n):\n        return (lambda m: (lambda k: m + n + total + k)(1))(2)\n    return bump(5)\n\nprint(outer())\n",
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
	{
		// roadmap L11.7 · Gap R.329 (ADR 0370): the application OMITS the parameter the body reads. The
		// entry tuple is the list of functions the BODY asks for — the slot the call left out arrives with
		// its default, the called one with the entry. The twin below, naming both, is the same body.
		"an application that omits the lambda's defaulted read parameter",
		"print((lambda f, y=10: f(y))(lambda x: x + 1))\n",
	},
	{
		"an application that names the lambda's defaulted parameter",
		"print((lambda f, y=10: f(y))(lambda x: x * 2, 5))\n",
	},
	{
		"an application that omits between a called slot and a read slot",
		"print((lambda f, k=3, n=40: f(k) + n)(lambda x: x * 2))\n",
	},
	{
		"an application that omits a called parameter whose default is a lambda",
		"print((lambda f, g=lambda v: v + 4: f(g(2)))((lambda n: n * 10)))\n",
	},
	{
		"the same omission on the def road: called slot named, read slot defaulted",
		"def apply(f, y=10):\n    return f(y)\n\nprint(apply(lambda x: x + 1))\n",
	},
	{
		"the def road with a called slot left to a lambda default",
		"def use(f, g=lambda v: v + 4):\n    return f(g(2))\n\nprint(use(lambda n: n * 10))\n",
	},
	{
		// roadmap L11.7 · Gap R.334 (ADR 0372): the body BOTH calls one of its own parameters AND reads a
		// name from the frame the application is written in. Each half answered alone before; the clone now
		// carries the entries its calls name AND one capture parameter per free name.
		"a capture read by a body that calls its own parameter",
		"def f(x):\n    return (lambda h: h(1) + x)(lambda q: q * 10)\n\nprint(f(2))\n",
	},
	{
		"a capture, a called parameter and an omitted default in one body",
		"def g(v):\n    return (lambda f, k=3: f(k) + v)(lambda a: a * 2)\n\nprint(g(5))\n",
	},
	{
		"two captures beside one entry",
		"def h(a, b):\n    return (lambda f: f(a) + b)(lambda z: z * 2)\n\nprint(h(3, 4))\n",
	},
	{
		"a local of the frame, not its parameter, captured by a body that calls its parameter",
		"def k():\n    t = 7\n    return (lambda f: f(1) + t)(lambda z: z * 2)\n\nprint(k())\n",
	},
	{
		"two entries and one capture",
		"def m(v):\n    return (lambda f, g: f(g(v)))(lambda a: a * 2, lambda b: b + 1)\n\nprint(m(3))\n",
	},
	{
		"a capture read by a body that calls its parameter, applied in a comprehension",
		"print([(lambda h: h(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]])\n",
	},
	{
		// The ENTRY side of the capture road: the lambda the CALLEE calls reads a name of the frame the
		// application is written in. An entry is still a bare symbol, so the word is minted as a parameter of
		// the CLONE that dispatches on it, filled at the application from the host frame's own reading, and
		// handed to the body's call of the entry positionally (roadmap L11.7 · Gap R.336, ADR 0395). These
		// three are the shapes the row was filed with, at the terminal, on both compiled legs.
		"an entry lambda that reads the frame its application is written in",
		"def apply1(fn, v):\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
	},
	{
		"an entry lambda that reads its frame, called twice by a higher-order def",
		"def twice(fn, seed):\n    return fn(fn(seed))\n\ndef use_b(seed):\n    base = 1\n    return twice(lambda x: x * 2 + base, seed)\n\nprint(use_b(2))\n",
	},
	{
		// The body reads the frame's local `shift` (ADR 0372's union) and the entry reads the frame's loop
		// target `step` (this row's) — one body, both lists.
		"an entry lambda that reads the loop target of the frame around its application",
		"def stepper():\n    total = 0\n    for step in [1, 2, 3]:\n        shift = step * 10\n        total = total + (lambda fn: fn(2) + shift)(lambda n: n * step)\n    return total\n\nprint(stepper())\n",
	},
	{
		"two entries of one callee, each reading its own name of the frame, and a closed entry beside them",
		"def apply1(fn, v):\n    return fn(v)\n\ndef both(f, g, v):\n    return f(v) + g(v)\n\ndef use(seed):\n    base = 2\n    off = 10\n    return both(lambda x: x + base, lambda x: x * off, seed) + apply1(lambda x: x + 1, seed)\n\nprint(use(3))\n",
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
		// The capture is a CONTAINER three frames deep too: the word that would carry it is L11.1's, and the
		// depth is not a reason to answer the print door with a heap handle (Gap R.332's rule, at depth).
		"a container capture three frames up",
		"def g():\n    b = [1, 2]\n    return (lambda y: (lambda z: len(b) + y + z)(100))(10)\n\nprint(g())\n",
		"holds a container",
	},
	{
		// The same depth with a TEXT: the forwarding carries the verdict the supplying frame gave, and that
		// verdict was "a text" — which is a refusal, not a permission (Gap R.332).
		"a text capture three frames up",
		"def g():\n    t = \"hey\"\n    return (lambda y: (lambda z: t + str(y) + str(z))(100))(10)\n\nprint(g())\n",
		"a text",
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
		// The omission leaves the CALLED slot with a number: the entry tuple would have to carry "10" as
		// the function the clone calls, which is the question ADR 0166's value word answers. The reference
		// raises TypeError on it (`'int' object is not callable`) — roadmap Gap R.329, Gap R.31.
		"an application whose omitted slot is followed by a required one",
		"print((lambda f, y: f(y))(lambda x: x))\n",
		"is called but is not a function",
	},
	{
		// The omission lands on a slot the body READS whose default is a list: binding an omitted container
		// default is Gap R.277's road, and the entry tuple has one word to carry it in — so the pass
		// declines and the ordinary road answers, rather than reading a heap handle as a count.
		"an application whose omitted read slot defaults to a container",
		"print((lambda f, y=[]: f(y))(lambda x: len(x)))\n",
		"is called but is not a function",
	},
	{
		// The capture reaches PAST the frame the application is written in, into the one that encloses it, AND
		// the body calls its own parameter: the nested `def` lift (ADR 0245) owns that frame's names and does not
		// compose with the higher-order road (roadmap **Gap R.347**, the half of Gap R.334 the lift still owes).
		// The READ-only twin — the same depth, a body that reads both names and calls nothing — answers since
		// ADR 0380, two rows above.
		"a capture two frames up, read by a body that calls its own parameter",
		"def outer(a):\n    def inner(b):\n        return (lambda f: f(1) + a + b)(lambda z: z * 3)\n    return inner(2)\n\nprint(outer(4))\n",
		"is called but is not a function",
	},
	{
		// The same omission written in a method body: the callable would have to reach a method frame, the
		// road Gap R.150/R.151 owns — the def-shaped twin above answers, the method-shaped one is shut.
		"the omission written inside a method body",
		"class Box:\n    def show(self, f, y=10):\n        return f(y)\n\nprint(Box().show(lambda x: x + 2))\n",
		"a lambda used as a value",
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
