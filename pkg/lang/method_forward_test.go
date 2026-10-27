package lang

// ADR 0431 · roadmap Gap R.454 — the answer direction's forwarding arm reaches a METHOD.
//
// **ADR 0428** taught `answerArmOf` that `return other(...)` is an arm: the body that writes it answers
// whatever the callee answers, so the arm carries the callee's own kind word instead of a constant one.
// The arm read `v.Fn.(*Name)` and nothing else, so the same sentence written one scope into a class
//
//	class C():
//	    def m(self):
//	        return None
//	    def n(self):
//	        return self.m()
//
//	c = C()
//	print(c.n() is None, c.n())   # the reference: True None   this backend, before: False 0 — at exit 0
//
// was one UN-NAMEABLE road, and one un-nameable road retires a whole body (**ADR 0368**'s rule): the
// forwarding method kept the ordinary return road, the ordinary return road handed the payload to the
// number printer, and the void — whose payload is the word `0`, which an int slot legally carries
// (**ADR 0187**) — printed as a digit. This file is the attribute arm, asked in both directions.
//
// What the arm may say about a callee it resolves (`self.m`, resolved through the class's own declarations
// and then every base, the walk `resolveMethod` walks at the call site) comes from the two tables the METHOD
// round keeps — `methodVoid` and `methodCarry` — and never from re-reading the callee: two methods that
// forward to each other are a reading that never ends, and a round reads a table (**ADR 0273**'s fixed
// point). The VOID half of that pair is new with this row: a void-only body is never carried (**ADR 0429**
// answers it statically), so an arm that looked only at `methodCarry` would name a forwarded call to a
// carried method and decline the same call over a void one — two answers to one question, which is what
// **ADR 0232** exists to forbid.
//
// Three shapes the arm refuses to claim, each pinned by a decision row below and each named in the roadmap:
// a receiver the body's own spelling cannot place (only `self` is written in the body that wrote the arm),
// a call to the forwarding body ITSELF (**ADR 0273**'s exclusion, kept), and a class the plan never
// registered or declared twice (**ADR 0299**'s rule about a duplicated name).
//
// Rows are judged on BOTH witness legs (**ADR 0302**): this compiler's module and the answer
// `testdata/interpreter-golden.json` holds for that exact source, where a MISSING record fails the case. The
// reference leg — a live CPython beside the linked compiler — is `integration/method_forward_test.go`.

import (
	"strings"
	"testing"
)

// methodForwardVoidBase is the row's own module: a void-answering method and a body that forwards a call to
// it, which is the program the row was filed with.
const methodForwardVoidBase = "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\n"

// methodForwardCarriedBase is the same shape over a body whose arms answer two kinds — the half that needs a
// run-time WORD and not a constant one.
const methodForwardCarriedBase = "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\n"

// methodForwardListBase and methodForwardDictBase are the container half: the callee builds an OBJECT, and the
// forwarding body has to answer with it — including for the doors that WALK an answer, which read a container
// by its kind and so need the kind to travel through the arm too.
const methodForwardListBase = "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\n"
const methodForwardDictBase = "class C():\n    def m(self):\n        return {\"a\": 1}\n    def n(self):\n        return self.m()\nc = C()\n"

// methodForwardFamily is the answered half. `want` is what the reference prints AND what this compiler must
// print; the same list is the reference leg in `integration/method_forward_test.go`, where every `want` is
// read off a live CPython rather than from this file.
func methodForwardFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			name: "the row's program",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "printed alone",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "None\n",
		},
		{
			name: "the equality twin",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() == None)\n",
			want: "True\n",
		},
		{
			name: "the negated identity",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is not None)\n",
			want: "False\n",
		},
		{
			name: "asked for its truth",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(bool(c.n()))\n",
			want: "False\n",
		},
		{
			name: "rendered by str and repr",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(str(c.n()), repr(c.n()))\n",
			want: "None None\n",
		},
		{
			name: "bound, then asked",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nr = c.n()\nprint(r is None, r)\n",
			want: "True None\n",
		},
		{
			name: "an element of a list literal",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint([c.n()])\n",
			want: "[None]\n",
		},
		{
			name: "written into an f-string",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nprint(f\"{c.n()}\")\n",
			want: "None\n",
		},
		{
			name: "a condition head",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nc = C()\nif c.n():\n    print(\"t\")\nelse:\n    print(\"f\")\n",
			want: "f\n",
		},
		{
			name: "the callee's side effect still runs",
			src:  "class C():\n    def m(self):\n        print(\"side\")\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "side\nNone\n",
		},
		{
			name: "the callee's side effect, asked twice",
			src:  "class C():\n    def m(self):\n        print(\"side\")\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n(), c.n())\n",
			want: "side\nside\nNone None\n",
		},
		{
			name: "a forwarding chain two deep",
			src:  "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\n    def o(self):\n        return self.n()\nc = C()\nprint(c.o() is None, c.o())\n",
			want: "True None\n",
		},
		{
			name: "a call inherited from a base",
			src:  "class B():\n    def m(self):\n        return None\nclass C(B):\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "a shadowed base method is the one forwarded",
			src:  "class B():\n    def m(self):\n        return None\nclass C(B):\n    def m(self):\n        return 7\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "7\n",
		},
		{
			name: "a method that takes the argument through",
			src:  "class C():\n    def m(self, x):\n        return None\n    def n(self):\n        return self.m(3)\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "a body with no return at all",
			src:  "class C():\n    def m(self):\n        self.x = 1\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n() is None, c.n())\n",
			want: "True None\n",
		},
		{
			name: "one method name, two classes",
			src:  "class A():\n    def m(self):\n        return None\nclass B():\n    def m(self):\n        return 7\n    def n(self):\n        return self.m()\na = A()\nb = B()\nprint(a.m() is None, b.n())\n",
			want: "True 7\n",
		},
		{
			name: "the arm that ran, forwarded",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(c.n(1), c.n(0), c.n(0) is None)\n",
			want: "7 None True\n",
		},
		{
			name: "a forwarded text arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(c.n(1), c.n(0))\n",
			want: "s None\n",
		},
		{
			name: "a forwarded number arm beside its own arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        if x > 1:\n            return 1\n        return self.m(x)\nc = C()\nprint(c.n(2), c.n(1), c.n(0))\n",
			want: "1 7 None\n",
		},
		{
			name: "the forwarded answer bound",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nr = c.n(0)\nprint(r is None, r)\n",
			want: "True None\n",
		},
		{
			name: "the forwarded answer, str and repr",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(str(c.n(1)), str(c.n(0)))\n",
			want: "7 None\n",
		},
		{
			name: "the forwarded answer in a container",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint([c.n(1), c.n(0)])\n",
			want: "[7, None]\n",
		},
		{
			name: "the forwarded answer in an f-string",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(f\"{c.n(1)}|{c.n(0)}\")\n",
			want: "7|None\n",
		},
		{
			name: "a forwarded list",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "[1, 2]\n",
		},
		{
			name: "a forwarded dict",
			src:  "class C():\n    def m(self):\n        return {\"a\": 1}\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "{'a': 1}\n",
		},
		{
			name: "a forwarded set",
			src:  "class C():\n    def m(self):\n        return {1, 2}\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			want: "{1, 2}\n",
		},
		{
			name: "a forwarded list, measured",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(len(c.n()))\n",
			want: "2\n",
		},
		{
			name: "a forwarded list, walked",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nfor v in c.n():\n    print(v)\n",
			want: "1\n2\n",
		},
		{
			name: "a forwarded list, membership",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(1 in c.n(), 9 in c.n())\n",
			want: "True False\n",
		},
		{
			name: "a forwarded dict, measured",
			src:  "class C():\n    def m(self):\n        return {\"a\": 1}\n    def n(self):\n        return self.m()\nc = C()\nprint(len(c.n()))\n",
			want: "1\n",
		},
		{
			name: "two arms forwarding two containers",
			src:  "class C():\n    def a(self):\n        return [1, 2]\n    def b(self):\n        return {\"k\": 1}\n    def n(self, x):\n        if x:\n            return self.a()\n        return self.b()\nc = C()\nprint(c.n(1), c.n(0))\n",
			want: "[1, 2] {'k': 1}\n",
		},
		{
			name: "a forwarded container beside the callee's own",
			src:  "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\nc = C()\nprint(c.m(), c.n(), c.m() == c.n())\n",
			want: "[1, 2] [1, 2] True\n",
		},
		{
			name: "a forwarded call to a number-answering method",
			src:  "class C():\n    def seven(self):\n        return 7\n    def n(self):\n        return self.seven()\nc = C()\nprint(c.n())\n",
			want: "7\n",
		},
		{
			name: "a forwarded call to a verdict-answering method",
			src:  "class C():\n    def flag(self):\n        return True\n    def n(self):\n        return self.flag()\nc = C()\nprint(c.n())\n",
			want: "True\n",
		},
		{
			// Two calls of ONE forwarding body, compared: right, and right for a reason worth pinning — the
			// identity door leaves two carried answers to `comparePair` rather than reading one word twice,
			// which is why the truth road beside it is **Gap R.460**'s and this one is answered.
			name: "two calls of one forwarding body, compared",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(c.n(1) == c.n(0))\n",
			want: "False\n",
		},
		{
			name: "a forwarded call to a constructor",
			src:  "class C():\n    def __init__(self):\n        self.x = 1\n    def reset(self):\n        return self.__init__()\nc = C()\nprint(c.reset() is None, c.reset())\n",
			want: "True None\n",
		},
		{
			name: "a method forwarding a call to itself",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return self.m(0)\nc = C()\nprint(c.m(1))\n",
			want: "7\n",
		},
	}
}

// TestTheForwardingMethodAnswersLikeTheReference is the compiled leg: every program runs through the LLVM JIT,
// prints the reference's bytes, and is on the record — a source with no entry fails the case rather than
// passing on the compiled answer alone (**ADR 0302**).
func TestTheForwardingMethodAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range methodForwardFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if res.Code != 0 {
				t.Fatalf("exited %d with stderr %q\n%s", res.Code, res.Stderr, tc.src)
			}
			if res.Output != tc.want {
				t.Fatalf("printed %q, want %q\n%s", res.Output, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q — one of them is "+
					"stale against the reference\n%s", recorded, tc.want, tc.src)
			}
		})
	}
}

// selfCall builds the one AST the arm reads: `self.<name>()`, the shape the parser hands
// `answerArmOf` for a body that forwards a call to another method of its own class. Written by hand rather
// than parsed, so a decision row can ask about a receiver, a name or a body the parser would refuse to
// produce — and so a row can say "the arm declined" without borrowing the arm to notice it declined.
func selfCall(name string) *Call {
	return &Call{Fn: &Attr{Obj: &Name{Value: "self"}, Name: &Name{Value: name}}}
}

// TestTheForwardingArmIsAskedOfTheCalleeAndNotTheBody asks the new arm DIRECTLY, over parsed programs, in
// both directions. The end-to-end rows cannot tell "the arm read the callee's table" from "the call happened
// to lower to bytes that print right", and these rows are where a widening past the arm's own remit shows up:
// a callee the round describes is answered, and one it does not is DECLINED whole — never guessed.
func TestTheForwardingArmIsAskedOfTheCalleeAndNotTheBody(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		src         string
		class       string // the class whose body the arm is read from
		method      string // the forwarding body
		callee      string // the name written after `self.`
		owner       string // the class that DECLARES the callee (the base, for an inherited row); "" = `class`
		wantWord    string // the word the arm must answer
		wantDyn     bool   // the arm forwards a run-time word rather than a constant one
		declines    bool   // the arm must say nothing at all
		wantNeither bool   // and the decline must be a callee in neither table, not a walk that missed it
		// wantTables is what the METHOD round must say about the callee for the row to be honest: a void arm
		// comes from `methodVoid`, a carried one from `methodCarry`, and a decline must be a callee in NEITHER.
		wantVoid  bool
		wantCarry bool
	}{
		{
			name: "a call forwarded to a void-answering method", wantWord: answerWordNone,
			src:   "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m", wantVoid: true,
		},
		{
			// **ADR 0429**'s no-return shape, which is the void and not an unread body: this is the half only
			// the VOID table can answer, and the reason the round records it beside the carried one.
			name: "a call forwarded to a method with no return at all", wantWord: answerWordNone,
			src:   "class C():\n    def m(self):\n        self.x = 1\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m", wantVoid: true,
		},
		{
			// **ADR 0428**'s arm, class-qualified: the word is the callee's run-time fact, read at the return.
			name: "a call forwarded to a method with two arms", wantWord: answerWordVaried, wantDyn: true,
			src:   "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\n",
			class: "C", method: "n", callee: "m", wantCarry: true,
		},
		{
			// The container's kind travels with the forwarded word, because the doors that WALK an answer
			// (`len`, an index, `in`, a loop) choose their road from the kind and not from the spelling.
			name: "a call forwarded to a method answering a list", wantWord: answerWordVaried, wantDyn: true,
			src:   "class C():\n    def m(self):\n        return [1, 2]\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m", wantCarry: true,
		},
		// The declines. Each is a callee the round describes in NO table, so the arm says nothing.
		{
			// A method answering ONE immediate kind is **ADR 0430**'s exemption from the round — and the
			// exemption has no answer for the body that FORWARDS it. Pinned in the owed table below as
			// roadmap **Gap R.462**.
			name: "a call forwarded to a method answering one text", declines: true, wantNeither: true,
			src:   "class C():\n    def hi(self):\n        return \"hi\"\n    def n(self):\n        return self.hi()\n",
			class: "C", method: "n", callee: "hi",
		},
		{
			// The exclusion **ADR 0273** wrote for the name road, kept: the inner frame would write the word
			// after the outer caller read it.
			name: "a call forwarded to the forwarding body ITSELF", declines: true,
			src:   "class C():\n    def n(self, x):\n        if x:\n            return 7\n        return self.n(0)\n",
			class: "C", method: "n", callee: "n",
		},
		{
			// Two methods forwarding to each other is what the tables, and not a reading of a body, exist
			// for: neither is ever named, and the walk never recurses.
			name: "two methods forwarding to each other", declines: true,
			src:   "class C():\n    def m(self):\n        return self.n()\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m",
		},
		{
			name: "a call forwarded to a method the class does not declare", declines: true,
			src:   "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.nope()\n",
			class: "C", method: "n", callee: "nope",
		},
		{
			// **ADR 0299**'s rule, applied by the walk that fills the table: no arm resolves through a class
			// name the program used twice, because the compiler would have to CHOOSE a body to read.
			name: "a call forwarded inside a class declared twice", declines: true,
			src:   "class C():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nclass C():\n    def m(self):\n        return 7\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m",
		},
		{
			// A method declared twice in one class is the class's own ambiguity, and the base walk does not
			// un-ambiguate a name the class itself spells twice.
			name: "a call forwarded to a method declared twice in one class", declines: true,
			src:   "class C():\n    def m(self):\n        return None\n    def m(self):\n        return 7\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m",
		},
		{
			// A generator, a decorated body and a coroutine are not ordinary bodies (**ADR 0368**'s
			// exclusion, read the same way by both rounds), so neither table holds them and the arm says
			// nothing — and the refusal the program meets is **Gap R.455**'s, not this row's.
			name: "a call forwarded to a generator method", declines: true,
			src:   "class C():\n    def m(self):\n        yield 1\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m",
		},
		{
			name: "a call forwarded to a decorated method", declines: true,
			src:   "class C():\n    @property\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m",
		},
		// The inheritance walk, in both directions.
		{
			name: "a call forwarded to a base class's void method", wantWord: answerWordNone,
			src:   "class B():\n    def m(self):\n        return None\nclass C(B):\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m", owner: "B", wantVoid: true,
		},
		{
			name: "a call forwarded to a base class's carried method", wantWord: answerWordVaried, wantDyn: true,
			src:   "class B():\n    def m(self, x):\n        if x:\n            return 7\n        return None\nclass C(B):\n    def n(self, x):\n        return self.m(x)\n",
			class: "C", method: "n", callee: "m", owner: "B", wantCarry: true,
		},
		{
			// The shadow: the dispatch runs the DERIVED body, so that is the body the arm answers for, and the
			// base's void body has no say in the word.
			name: "a call forwarded to a method the class shadows", wantWord: answerWordVaried, wantDyn: true,
			src:   "class B():\n    def m(self):\n        return None\nclass C(B):\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\n",
			class: "C", method: "n", callee: "m", owner: "C", wantCarry: true,
		},
		{
			// A hierarchy that names itself is not walked twice: the visit set is what makes this TERMINATE, and
			// it terminates on the body the dispatch would run — `B.m`, which `resolveMethod` finds through the
			// same base walk — rather than on a stack overflow or a guess.
			name: "a base class that names its own derived class", wantWord: answerWordNone,
			src:   "class B(C):\n    def m(self):\n        return None\nclass C(B):\n    def n(self):\n        return self.m()\n",
			class: "C", method: "n", callee: "m", owner: "B", wantVoid: true,
		},
		// Two classes, one method name: the word belongs to the body, not to the spelling.
		{
			name: "one method name, two classes — the void body", wantWord: answerWordNone,
			src:   "class A():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nclass B():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\n",
			class: "A", method: "n", callee: "m", owner: "A", wantVoid: true,
		},
		{
			name: "one method name, two classes — the carried body", wantWord: answerWordVaried, wantDyn: true,
			src:   "class A():\n    def m(self):\n        return None\n    def n(self):\n        return self.m()\nclass B():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n    def n(self, x):\n        return self.m(x)\n",
			class: "B", method: "n", callee: "m", owner: "B", wantCarry: true,
		},
		{
			// A module-level `def` has no `self`, so an attribute call written in one is not the arm's to read
			// (roadmap **Gap R.463**'s half).
			name: "a module def is no class's body at all", declines: true,
			src:    "def f(o):\n    return o.m()\n",
			class:  "",
			method: "f", callee: "m",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			p := answerKindPlanOf(prog, map[string]bool{})
			var fd *FuncDef
			if tc.class != "" {
				fd = classMethodOf(prog, tc.class, tc.method)
			} else {
				for _, st := range prog.Stmts {
					if d, ok := st.(*FuncDef); ok && d.Name == tc.method {
						fd = d
					}
				}
			}
			if fd == nil {
				t.Fatalf("the program declares no %s — the row is written against a body that is not there:\n%s", tc.method, tc.src)
			}
			owner := tc.owner
			if owner == "" {
				owner = tc.class
			}
			key := methodAnswerKey(owner, tc.callee)
			inVoid, inCarry := p.methodVoid[key], p.carriesMethod(key)
			arm, ok := answerForwardedMethodArm(fd, selfCall(tc.callee), p)
			if tc.declines {
				if ok {
					t.Fatalf("the arm CLAIMED a kind it was never given — word %q, forwarded %q (callee in "+
						"methodVoid=%v, methodCarry=%v):\n%s", arm.word, arm.dyn, inVoid, inCarry, tc.src)
				}
				if tc.wantNeither && (inVoid || inCarry) {
					t.Fatalf("the arm declined a callee the round DOES describe (methodVoid=%v, methodCarry=%v): "+
						"that is a hole in the arm, not a refusal:\n%s", inVoid, inCarry, tc.src)
				}
				return
			}
			if !ok {
				t.Fatalf("the arm declined a callee the round describes (want %q):\n%s", tc.wantWord, tc.src)
			}
			if arm.word != tc.wantWord {
				t.Fatalf("the arm answers word %q, want %q — the word IS the answer's kind, and a wrong one prints "+
					"a payload wearing another kind's bits (**ADR 0187**):\n%s", arm.word, tc.wantWord, tc.src)
			}
			if (arm.dyn != "") != tc.wantDyn {
				t.Fatalf("forwarded = %q, want a forwarded arm = %v:\n%s", arm.dyn, tc.wantDyn, tc.src)
			}
			// The arm answers out of exactly one table, and the table it answers out of is the callee's own
			// verdict — the same one the print door and the `is` consult (**ADR 0232**).
			if inVoid == inCarry {
				t.Fatalf("the callee is in %d tables and the arm answered anyway (void=%v, carry=%v):\n%s",
					map[bool]int{true: 2, false: 0}[inVoid], inVoid, inCarry, tc.src)
			}
			if inVoid != tc.wantVoid || inCarry != tc.wantCarry {
				t.Fatalf("the round says (void=%v, carry=%v) and the row expected (%v, %v):\n%s",
					inVoid, inCarry, tc.wantVoid, tc.wantCarry, tc.src)
			}
		})
	}
}

// TestTheForwardingArmIsNotAWayToReadAReceiverTheBodyCannotName pins the receiver rule on its own: `self` is
// the one receiver whose class is written in the body that wrote the arm, and the census's reading of a
// module variable bound to one class is not the plan's to lean on — it would hand one class's answer word to
// a call the dispatch may run against another (**ADR 0299**).
func TestTheForwardingArmIsNotAWayToReadAReceiverTheBodyCannotName(t *testing.T) {
	t.Parallel()
	const src = "class C():\n    def m(self):\n        return None\n    def n(self, o):\n        return o.m()\n    def o(self):\n        return C().m()\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	p := answerKindPlanOf(prog, map[string]bool{})
	callee := methodAnswerKey("C", "m")
	if !p.methodVoid[callee] {
		t.Fatalf("the callee is not on the void table at all, so the rows below prove nothing:\n%s", src)
	}
	for _, m := range []string{"n", "o"} {
		fd := classMethodOf(prog, "C", m)
		if fd == nil {
			t.Fatalf("C declares no %s", m)
		}
		if _, ok := answerForwardedMethodArm(fd, &Call{Fn: &Attr{Obj: &Name{Value: "other"}, Name: &Name{Value: "m"}}}, p); ok {
			t.Fatalf("C.%s forwarded a call through a receiver the body cannot place, and read %s's word anyway:\n%s",
				m, callee, src)
		}
	}
	// The same call written as `self.` resolves, so the decline above is about the receiver and not about the
	// name, the class or the table.
	if _, ok := answerForwardedMethodArm(classMethodOf(prog, "C", "n"), selfCall("m"), p); !ok {
		t.Fatalf("the arm declined the one receiver its own body writes:\n%s", src)
	}
}

// TestWhatTheForwardingMethodEmitsIsTheCalleesWordAndNotAPayload reads the module: a void-forwarding body
// declares no word (**ADR 0429**'s fold), a carried-forwarding body declares one and writes into it the word
// it read out of the callee, the call runs exactly once, and no forwarding body hands `llc` a container
// GLOBAL in its return slot (**ADR 0166**'s compiler-bug exit code).
func TestWhatTheForwardingMethodEmitsIsTheCalleesWordAndNotAPayload(t *testing.T) {
	t.Parallel()
	t.Run("a body forwarding the void declares no word", func(t *testing.T) {
		t.Parallel()
		src := methodForwardVoidBase + "print(c.n() is None, c.n())\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		if strings.Contains(res.IR, "@gy_C_n.anst") {
			t.Fatalf("a body that forwards the void was given a word no road reads — the fold answers it "+
				"statically (**ADR 0429**):\n%s", res.IR)
		}
		body := runtimeFnBody(res.IR, "define i32 @gy_C_n(")
		if body == "" {
			t.Fatalf("the module carries no @gy_C_n:\n%s", res.IR)
		}
		if got := callOnce(body, "gy_C_m"); got != 1 {
			t.Fatalf("@gy_C_n calls @gy_C_m %d time(s), want exactly 1: the arm is a KIND, not a substitution, "+
				"and the callee still runs (**ADR 0172**'s one-evaluation rule)\n%s\n%s", got, body, src)
		}
		if res.Output != "True None\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "True None\n", src)
		}
	})

	t.Run("the callee's side effect runs when the answer is folded away", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        print(\"side\")\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		if res.Output != "side\nNone\n" {
			t.Fatalf("printed %q, want %q — the forwarded arm states a kind and never skips the call", res.Output, "side\nNone\n")
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if got := callOnce(main, "gy_C_n"); got != 1 {
			t.Fatalf("@main runs the forwarding method %d times, want 1\n%s\n%s", got, main, src)
		}
		nBody := runtimeFnBody(res.IR, "define i32 @gy_C_n(")
		if got := callOnce(nBody, "gy_C_m"); got != 1 {
			t.Fatalf("@gy_C_n runs the callee %d times, want 1\n%s\n%s", got, nBody, src)
		}
	})

	t.Run("a body forwarding a carried answer copies its word", func(t *testing.T) {
		t.Parallel()
		src := methodForwardCarriedBase + "print(c.n(1), c.n(0), c.n(0) is None)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		if !strings.Contains(res.IR, "@gy_C_n.anst = internal global i32 0") {
			t.Fatalf("the forwarding body declares no word of its own, so its caller reads whichever arm the "+
				"last call left:\n%s", res.IR)
		}
		body := runtimeFnBody(res.IR, "define i32 @gy_C_n(")
		if !strings.Contains(body, "load i32, i32* @gy_C_m.anst") {
			t.Fatalf("@gy_C_n never asks the callee which arm ran:\n%s\n%s", body, src)
		}
		if !strings.Contains(body, "store i32 %") || !strings.Contains(body, "@gy_C_n.anst") {
			t.Fatalf("@gy_C_n read the callee's word and never stored it:\n%s\n%s", body, src)
		}
		if got := strings.Count(body, "i32* @gy_C_n.anst"); got < 3 {
			// one store of the forwarded word, two of the void's (the epilogue and the unwind target)
			t.Fatalf("@gy_C_n writes its own word %d time(s), want at least 3:\n%s\n%s", got, body, src)
		}
		// The two fall-off-the-end roads (the ordinary epilogue and the unwind target) answer the void, and
		// say so, so a caller can never read a number out of a body that returned nothing.
		if got := strings.Count(body, "store i32 3, i32* @gy_C_n.anst"); got != 2 {
			t.Fatalf("@gy_C_n stores the void's word %d time(s), want 2 (the epilogue and the unwind target):"+
				"\n%s\n%s", got, body, src)
		}
		if strings.Contains(body, "ret i32 @") {
			t.Fatalf("@gy_C_n returns a GLOBAL — that is the operand `llc` refuses (**ADR 0166**):\n%s\n%s", body, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if !strings.Contains(main, "@gy_C_n.anst") {
			t.Fatalf("@main prints the forwarded answer without asking its word:\n%s\n%s", main, src)
		}
		if res.Output != "7 None True\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "7 None True\n", src)
		}
	})

	t.Run("a forwarded container answer is an object, not a global", func(t *testing.T) {
		t.Parallel()
		src := methodForwardListBase + "print(c.n(), len(c.n()))\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		if body := runtimeFnBody(res.IR, "define i32 @gy_C_n("); strings.Contains(body, "ret i32 @") {
			t.Fatalf("@gy_C_n returns a GLOBAL in the i32 slot `llc` refuses (**ADR 0166**, **Gap R.67**):\n%s\n%s",
				body, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if !strings.Contains(main, "@rt_print_mixed_value") {
			t.Fatalf("the forwarded container was not printed by the one printer that reads a tag:\n%s\n%s", main, src)
		}
		// `rt_heap_len` is the door a `len` asks of an object it was handed (**ADR 0430**'s container answer for
		// a module `def` calls the same one); the forwarded arm has to carry the KIND for this call to be emitted
		// at all, because a body the plan reads as a number is measured by the number road.
		if !strings.Contains(main, "@rt_heap_len") {
			t.Fatalf("`len` of the forwarded answer never asked the object — the container kind did not travel "+
				"through the forwarding arm:\n%s\n%s", main, src)
		}
		if res.Output != "[1, 2] 2\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "[1, 2] 2\n", src)
		}
	})
}

// methodForwardFamilyStillOwes is the filed half, factored out so the same rows are read by the compiled pin
// below and by the reference harness in `integration/method_forward_test.go`.
func methodForwardFamilyStillOwes() []struct {
	name       string
	src        string
	wantExit   int
	wantOut    string
	wantStderr string
	dossier    string
} {
	return []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		{
			// The callee answers ONE immediate kind, which is the one case **ADR 0430** exempts from the round
			// — and the exemption has no answer for the body that FORWARDS it: `hi` keeps the table keyed by its
			// own name, and `greet`, whose only road is a call, is described by nothing. The payload reaches the
			// number printer.
			name:     "a call forwarded to a method answering one text",
			src:      "class C():\n    def hi(self):\n        return \"hi\"\n    def greet(self):\n        return self.hi()\nc = C()\nprint(c.greet())\n",
			wantExit: 0, wantOut: "0\n",
			dossier: "**Gap R.462** — the reference prints `hi`; `print(c.hi())` prints it too, so the missing half " +
				"is the forwarding body's kind, which no table describes",
		},
		{
			// Measured on the way to this row and NOT this row's: the DIRECT call to the same one-float method
			// prints `1` too, on this build and on its parent, where the module `def` twin prints `1.5`.
			name:     "a call forwarded to a method answering one float",
			src:      "class C():\n    def half(self):\n        return 1.5\n    def n(self):\n        return self.half()\nc = C()\nprint(c.n())\n",
			wantExit: 0, wantOut: "1\n",
			dossier: "**Gap R.466** — the reference prints `1.5`, and this backend's DIRECT call `print(c.half())` " +
				"prints `1` too, on this build and on its parent, where the module `def` twin prints `1.5`: the " +
				"class's print door asks a table for a TEXT-answering method (**ADR 0366**'s `strMethodFuncs`) and has " +
				"none for a FLOAT-answering one, so the number road prints the double's low word (**ADR 0187**'s " +
				"shape, one door further in) — which is the measurement that moves this out of **Gap R.462**'s " +
				"forwarding story",
		},
		{
			// The arm reads `self`, because `self` is the one receiver whose class is written in the body that
			// wrote the arm. A parameter is not, and the census's reading of a module variable bound to one
			// class is not the plan's to lean on.
			name:     "a module def forwarding a call through a parameter",
			src:      "class C():\n    def m(self):\n        return None\nc = C()\ndef f(o):\n    return o.m()\nprint(f(c) is None)\n",
			wantExit: 0, wantOut: "False\n",
			dossier: "**Gap R.463** — the reference prints `True`; the callee is on the void table, and the arm " +
				"walks past it because the receiver is spelled `o` and not `self`",
		},
		{
			// The container kind travels through the arm for the doors that MEASURE and WALK an answer (`len`,
			// `in`, a loop, all answered above); the doors that read a SLOT still resolve the answer through the
			// notebooks a literal fills, and a forwarded answer fills none.
			name:     "a forwarded list, subscripted",
			src:      methodForwardListBase + "print(c.n()[0])\n",
			wantExit: 1, wantStderr: "index requires an inline list/dict/set literal",
			dossier: "**Gap R.464** — the reference prints `1`; `c.m()[0]` of the SAME body prints it (**ADR 0430**), " +
				"so the object exists and only the forwarded road's slot notebook is missing",
		},
		{
			name:     "a forwarded dict, read by key",
			src:      methodForwardDictBase + "print(c.n()[\"a\"])\n",
			wantExit: 1, wantStderr: "index must be a constant",
			dossier: "**Gap R.464** — the reference prints `1`; `c.m()[\"a\"]` prints it, and `print(c.n())` on the " +
				"line above the refusal prints the dict, which is the two-door disagreement **ADR 0232** forbids",
		},
		{
			// The reductions, already filed for a DIRECT call and pinned here for the forwarded one: the same
			// door, one call deeper, refusing with the same sentence.
			name:     "a forwarded container answer, summed",
			src:      "class C():\n    def m(self):\n        return [1, 2, 3]\n    def n(self):\n        return self.m()\nc = C()\nprint(sum(c.n()))\n",
			wantExit: 1, wantStderr: "sum requires an inline list/set/dict literal",
			dossier: "**Gap R.458** (standing) — the reference prints `6`; `sum(build())` of a module `def` refuses " +
				"with this sentence too, so it is one door and one row, asked here through a forwarding body",
		},
		{
			// **ADR 0368**'s one word per body, not per call, read through two forwarding bodies. The answer
			// beside it (`c.n(1) == c.n(0)`, in the answered family) is right because the comparison leaves two
			// carried answers to `comparePair`; the truth road reads the one word both calls left.
			name:     "two calls of one forwarding body, asked for their truth",
			src:      "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return None\n    def n(self, x):\n        return self.m(x)\nc = C()\nprint(bool(c.n(1)), bool(c.n(0)))\n",
			wantExit: 0, wantOut: "False False\n",
			dossier: "**Gap R.460** (standing) — the reference prints `True False`; each call copies the callee's " +
				"word into one global, and the second copy is the one both positions read",
		},
	}
}

// TestWhatTheMethodForwardingStillOwesIsFiledNotFixed is the honest half: what the arm leaves wrong or
// refuses, pinned with the reference's answer beside this backend's, so the day a door opens the row fails and
// the row in `roadmap.md` can be closed. A refusal stays a refusal at exit 1 — **ADR 0166** reserves exit 2 —
// and a wrong answer stays a wrong answer at the exit code of success.
func TestWhatTheMethodForwardingStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range methodForwardFamilyStillOwes() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if tc.wantExit != 1 || !strings.Contains(err.Error(), tc.wantStderr) {
					t.Fatalf("%s (%s): failed in a way this pin does not describe: %v", tc.name, tc.dossier, err)
				}
				return
			}
			if tc.wantExit != 0 {
				t.Fatalf("%s (%s): exited %d where this row pins a refusal (%q)", tc.name, tc.dossier, res.Code, tc.wantStderr)
			}
			if res.Code != 0 {
				t.Fatalf("%s (%s): exited %d with stderr %q, want 0 with %q", tc.name, tc.dossier, res.Code, res.Stderr, tc.wantOut)
			}
			if res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, this row pins %q — the shape moved, and the row in roadmap.md is "+
					"ready to close", tc.name, tc.dossier, res.Output, tc.wantOut)
			}
		})
	}
}
