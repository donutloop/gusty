package lang

// ADR 0430 · roadmap Gap R.453 — the answer direction has a METHOD round.
//
// **ADR 0368** taught the compiler to keep the kind a `def`'s answer denotes in a word of its own, and the
// reading that decided it walked `prog.Stmts` and stopped at the first `def`. A method's body was therefore
// read by nobody, and one question — "which kind does this body answer?" — got two answers depending on
// where the `def` was written (**ADR 0232**'s complaint, verbatim, one scope in):
//
//	def f(x):
//	    if x:
//	        return 7
//	    return None
//	class C():
//	    def m(self, x):
//	        if x:
//	            return 7
//	        return None
//
//	c = C()
//	print(f(1), f(0), f(0) is None)   # the reference: 7 None True   this backend: 7 None True
//	print(c.m(1), c.m(0), c.m(0) is None)   # the reference: 7 None True   this backend, before: 7 0 False
//
// …and the arm that answers a CONTAINER had no builder at a method return at all, so the module reached
// `llc` with `ret i32 @.lst1` — a global in an i32 slot, which is **ADR 0166**'s compiler-bug exit code:
//
//	class C():
//	    def m(self):
//	        return [1, 2]
//
//	c = C()
//	print(c.m())   # the reference: [1, 2]   this backend, before: exit 2
//
// The round is additive and keyed by the emitted symbol (`gy_C_m`, the name `resolveMethod` already mints),
// so a one-kind method — which is every method this toolchain answered correctly before — keeps the road it
// had, declares no kind word, and stores nothing. A method whose body answers nothing but the void stays
// **ADR 0429**'s static fold: carrying it would store a word no road ever reads.
//
// Rows are judged on BOTH witness legs (**ADR 0302**): this compiler's module, and the answer
// `testdata/interpreter-golden.json` holds for that exact source, where a MISSING record fails the case. The
// reference leg — a live CPython beside the linked compiler — is `integration/method_answer_test.go`.

import (
	"strings"
	"testing"
)

// methodAnswerRow is the module the family is written against: one class, one method whose two arms answer
// two different kinds, and one instance.
const methodAnswerRow = "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\nc = C()\n"

// methodAnswerFamily is the answered half. `want` is what the reference prints AND what this compiler must
// print; the same list is the reference leg in `integration/method_answer_test.go`, where every `want` is
// read off a live CPython rather than from this file.
func methodAnswerFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The row's own program: the arm that ran is a run-time fact, and now it is a run-time WORD.
		{"the row's program", methodAnswerRow + "print(c.m(1), c.m(0), c.m(0) is None)\n", "7 None True\n"},
		{"its equality twin", methodAnswerRow + "print(c.m(1) == 7, c.m(0) == None)\n", "True True\n"},
		{"the negated identity", methodAnswerRow + "print(c.m(0) is not None, c.m(1) is not None)\n", "False True\n"},
		{"asked for its truth", methodAnswerRow + "print(bool(c.m(1)), bool(c.m(0)))\n", "True False\n"},
		{"bound, then asked", methodAnswerRow + "r = c.m(0)\nprint(r is None, r)\n", "True None\n"},
		{"rendered by str", methodAnswerRow + "print(str(c.m(1)), str(c.m(0)))\n", "7 None\n"},
		{"written into an f-string", methodAnswerRow + "print(f\"{c.m(1)}|{c.m(0)}\")\n", "7|None\n"},
		{"an element of a list literal", methodAnswerRow + "print([c.m(1), c.m(0)])\n", "[7, None]\n"},
		{"a condition head", methodAnswerRow + "if c.m(0):\n    print(\"t\")\nelse:\n    print(\"f\")\n", "f\n"},
		// Two arms of two DIFFERENT non-void kinds: the fold used to answer the second from the first.
		{
			name: "a text arm and a number arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return \"x\"\n        return 0\nc = C()\nprint(c.m(1))\nprint(c.m(0))\n",
			want: "x\n0\n",
		},
		{
			name: "three arms — a number, a text and the void",
			src:  "class C():\n    def m(self, x):\n        if x == 1:\n            return 1\n        elif x == 2:\n            return \"a\"\n        return None\nc = C()\nprint(c.m(1), c.m(2), c.m(3))\n",
			want: "1 a None\n",
		},
		{
			// The void's payload is a LEGAL integer (**ADR 0187**), which is why the equality against a number
			// was answered by coincidence rather than by a tag.
			name: "the void arm against the number the other arm answers",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return None\n        return 0\nc = C()\nprint(c.m(1) == None, c.m(0) == None, c.m(1) == 0, c.m(0) == 0)\n",
			want: "True False False True\n",
		},
		{
			name: "a text arm compared against a number and a text",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return 1\nc = C()\nprint(c.m(1) == \"s\", c.m(0) == \"s\", c.m(1) == 1, c.m(0) == 1)\n",
			want: "True False False True\n",
		},
		// The container arms — the half that reached the assembler.
		{
			name: "a body that answers a list",
			src:  "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m())\n",
			want: "[1, 2]\n",
		},
		{
			name: "a body that answers a dict",
			src:  "class C():\n    def m(self):\n        return {\"a\": 1}\nc = C()\nprint(c.m())\n",
			want: "{'a': 1}\n",
		},
		{
			name: "a body whose container slot holds another container",
			src:  "class C():\n    def m(self):\n        return {\"k\": [1, 2]}\nc = C()\nprint(c.m())\n",
			want: "{'k': [1, 2]}\n",
		},
		{
			name: "a body whose answer is a list of lists",
			src:  "class C():\n    def m(self):\n        return [[1, 2], [3]]\nc = C()\nprint(c.m())\n",
			want: "[[1, 2], [3]]\n",
		},
		{
			name: "a container arm beside a void arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return [1, 2]\n        return None\nc = C()\nprint(c.m(1), c.m(0))\n",
			want: "[1, 2] None\n",
		},
		{
			name: "a list arm and a dict arm",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return [1, 2]\n        return {\"a\": 1}\nc = C()\nprint(c.m(1), c.m(0))\n",
			want: "[1, 2] {'a': 1}\n",
		},
		{
			name: "the container answer measured, subscripted and walked",
			src:  "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(len(c.m()), c.m()[0])\nfor v in c.m():\n    print(v)\n",
			want: "2 1\n1\n2\n",
		},
		{
			name: "the dict answer measured and asked for a key",
			src:  "class C():\n    def m(self):\n        return {\"a\": 1}\nc = C()\nprint(len(c.m()), c.m()[\"a\"], \"a\" in c.m())\n",
			want: "1 1 True\n",
		},
		{
			name: "a container answer compared with the literal it answers",
			src:  "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m() == [1, 2])\n",
			want: "True\n",
		},
		{
			name: "a fresh instance's container answer",
			src:  "class C():\n    def m(self):\n        return [1, 2]\nprint(len(C().m()), C().m()[0])\n",
			want: "2 1\n",
		},
		// Inheritance: the answer belongs to the class that wrote the body.
		{
			name: "an inherited mixed-arm method",
			src:  "class Base():\n    def m(self, x):\n        if x:\n            return 5\n        return None\nclass Child(Base):\n    pass\nprint(Child().m(1), Child().m(0), Child().m(0) is None)\n",
			want: "5 None True\n",
		},
		{
			name: "one method name, two classes, two bodies",
			src:  "class A():\n    def m(self, x):\n        if x:\n            return 1\n        return None\nclass B():\n    def m(self, x):\n        if x:\n            return 2\n        return None\nprint(A().m(1), A().m(0), B().m(1), B().m(0))\n",
			want: "1 None 2 None\n",
		},
		// The arm still runs: an answer is a kind, never a substitution (**ADR 0172**'s rule).
		{
			name: "a fall-off-the-end arm that prints",
			src:  "class C():\n    def m(self, x):\n        if x:\n            return 7\n        print(\"fall\")\nc = C()\nprint(c.m(1), c.m(0), c.m(0) is None)\n",
			want: "fall\nfall\n7 None True\n",
		},
		// The module `def` twins of the two comparison rows: one question, one answer (**ADR 0232**).
		{
			name: "a module body whose void arm is compared with None",
			src:  "def f(x):\n    if x:\n        return None\n    return 1\nprint(f(True) is None)\n",
			want: "True\n",
		},
		{
			name: "a module body forwarding one arm of a mixed body",
			src:  "def f(x):\n    if x:\n        return None\n    return 1\ndef g():\n    return f(True)\nprint(g(), g() is None)\n",
			want: "None True\n",
		},
		{
			name: "a module body's void arm compared with the number the other arm answers",
			src:  "def f(x):\n    if x:\n            return None\n    return 0\nprint(f(True) == 0)\n",
			want: "False\n",
		},
		// The controls: a method that answers ONE kind is not carried and keeps every road it had.
		{
			name: "a method answering a number keeps its road",
			src:  "class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m(), c.m() is None)\n",
			want: "7 False\n",
		},
		{
			name: "a method answering a text keeps its road",
			src:  "class C():\n    def m(self):\n        return \"s\"\nc = C()\nprint(c.m(), c.m() == None)\n",
			want: "s False\n",
		},
	}
}

// TestTheMethodAnswerAnswersEveryQuestion is the family on the compiled leg and the record leg.
func TestTheMethodAnswerAnswersEveryQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range methodAnswerFamily() {
		tc := tc
		if tc.want == "" {
			t.Fatalf("%q has no expectation, so it certifies nothing", tc.name)
		}
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

// classMethodOf picks a class's method body out of a parsed program, the way the row asks about it — by the
// class it is written in and the name the call spells. It is the test's own reading, deliberately NOT
// `methodBodyOf`: this table must be able to say "the plan and the fold disagree" without borrowing the
// machinery under test to find the body.
func classMethodOf(prog *Program, class, name string) *FuncDef {
	for _, st := range prog.Stmts {
		cd, ok := st.(*ClassDef)
		if !ok || cd.Name != class {
			continue
		}
		for _, m := range cd.Body {
			if fd, ok := m.(*FuncDef); ok && fd.Name == name {
				return fd
			}
		}
	}
	return nil
}

// TestTheAnswerPlanCarriesTheMethodRound asks the READING directly, in both directions. The end-to-end rows
// cannot tell "the plan carries the method and a road happens to print the right bytes" from "the method's
// body was read", and this is where a widening past the round's own remit shows up: a one-kind method must
// NOT be carried, and a void-only body must stay **ADR 0429**'s static fold.
func TestTheAnswerPlanCarriesTheMethodRound(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		symbol   string
		class    string // the class whose body the row asks about, for the VOID verdict control below
		method   string
		carried  bool
		kind     containerAnswerKind
		oneKind  bool
		voidOnly bool
	}{
		{
			name: "a method with a void arm and a number arm", symbol: "gy_C_m", class: "C", method: "m", carried: true,
			src: "class C():\n    def m(self, x):\n        if x:\n            return 7\n        return None\n",
		},
		{
			name: "a method with a text arm and a number arm", symbol: "gy_C_m", class: "C", method: "m", carried: true,
			src: "class C():\n    def m(self, x):\n        if x:\n            return \"x\"\n        return 0\n",
		},
		{
			// The control that keeps the round from being a tax: this body answers ONE immediate kind, needs no
			// word, and must not be given one.
			name: "a method answering one number", symbol: "gy_C_m", class: "C", method: "m", carried: false,
			src: "class C():\n    def m(self):\n        return 7\n",
		},
		{
			// **ADR 0429**'s remit, unchanged: a void-only body is answered by the fold, statically — which is
			// the pair of facts this row has to keep, because "not carried" alone would also be true of a body
			// the plan simply failed to read.
			name: "a method answering only the void", symbol: "gy_C_m", class: "C", method: "m", carried: false, voidOnly: true,
			src: "class C():\n    def m(self):\n        return None\n",
		},
		{
			name: "a method answering only the void by falling off its end", symbol: "gy_C_m", class: "C", method: "m",
			carried: false, voidOnly: true,
			src: "class C():\n    def m(self):\n        pass\n",
		},
		{
			// The container half: a method has no `containerRet`, so a body answering one container is carried
			// for the container to be built at all — and the plan is what says which kind.
			name: "a method answering a list", symbol: "gy_C_m", class: "C", method: "m", carried: true, kind: listAnswer, oneKind: true,
			src: "class C():\n    def m(self):\n        return [1, 2]\n",
		},
		{
			name: "a method answering a dict", symbol: "gy_C_m", class: "C", method: "m", carried: true, kind: dictAnswer, oneKind: true,
			src: "class C():\n    def m(self):\n        return {\"a\": 1}\n",
		},
		{
			// The key: two classes, one method name. The word belongs to the body, not to the name.
			name: "one name, two classes — the second body", symbol: "gy_B_m", class: "B", method: "m", carried: true,
			src: "class A():\n    def m(self):\n        return 7\nclass B():\n    def m(self, x):\n        if x:\n            return 1\n        return None\n",
		},
		{
			name: "one name, two classes — the first body is not carried", symbol: "gy_A_m", class: "A", method: "m", carried: false,
			src: "class A():\n    def m(self):\n        return 7\nclass B():\n    def m(self, x):\n        if x:\n            return 1\n        return None\n",
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
			if got := p.carriesMethod(tc.symbol); got != tc.carried {
				t.Fatalf("carriesMethod(%q) = %v, want %v for:\n%s", tc.symbol, got, tc.carried, tc.src)
			}
			if !tc.carried {
				if _, kinded := p.methodContainerKind(tc.symbol); kinded {
					t.Fatalf("%q is not carried and so has no container kind to report, but the plan says %v", tc.symbol, kinded)
				}
				// "Not carried" is also what a body the plan FAILED to read looks like, so the row says
				// WHICH of the two exclusions it is: a void-only body is **ADR 0429**'s static fold, and a
				// one-kind body answers with a payload that can speak for itself. A body that is neither is
				// not an exclusion, it is a hole.
				g := &irGen{answer: p, methodCensus: methodCensus(prog, nil)}
				fd := classMethodOf(prog, tc.class, tc.method)
				if fd == nil {
					t.Fatalf("the program declares no %s.%s — the row is written against a body that is not there:\n%s",
						tc.class, tc.method, tc.src)
				}
				if got := g.methodAnswersOnlyTheVoid(fd); got != tc.voidOnly {
					t.Fatalf("methodAnswersOnlyTheVoid(%s.%s) = %v, want %v: an un-carried method must be either "+
						"**ADR 0429**'s static void fold or a body whose single kind its payload states — anything else "+
						"is a body this round silently stopped reading\n%s", tc.class, tc.method, got, tc.voidOnly, tc.src)
				}
				return
			}
			if tc.voidOnly {
				t.Fatalf("%q is carried and answers nothing but the void — the fold already knows that answer, and a "+
					"word nobody reads is the tax this round rejects (**ADR 0429**, **ADR 0430**):\n%s", tc.symbol, tc.src)
			}
			if tc.oneKind {
				kind, ok := p.methodContainerKind(tc.symbol)
				if !ok || kind != tc.kind {
					t.Fatalf("%q answers one container: the plan reports (%v, %v), want (%v, true)", tc.symbol, kind, ok, tc.kind)
				}
			}
			if _, _, ok := p.methodReading(tc.symbol); !ok {
				t.Fatalf("%q is carried but the plan kept no reading of its body, so no door can ask what it answers", tc.symbol)
			}
		})
	}
}

// TestWhatTheMethodAnswerEmitsIsTheTagWordAndNotAGlobal pins the IR the round writes: the method's own word,
// one store on the arm that ran, and — the half that used to reach the assembler — a `ret` whose operand is
// an i32 rather than a container GLOBAL (**ADR 0166**'s compiler-bug exit code, **ADR 0188**/**0189**'s
// global-in-a-word rule).
func TestWhatTheMethodAnswerEmitsIsTheTagWordAndNotAGlobal(t *testing.T) {
	t.Parallel()
	t.Run("a mixed-arm method stores the arm that ran", func(t *testing.T) {
		t.Parallel()
		src := methodAnswerRow + "print(c.m(1), c.m(0))\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		body := runtimeFnBody(res.IR, "define i32 @gy_C_m(")
		if body == "" {
			t.Fatalf("the module carries no @gy_C_m:\n%s", res.IR)
		}
		if !strings.Contains(res.IR, "@gy_C_m.anst = internal global i32 0") {
			t.Fatalf("the module declares no word for the method's answer — the arm that ran has nowhere to be "+
				"written, which is the same @<symbol>.anst the module `def` round writes (**ADR 0368**):\n%s", res.IR)
		}
		for _, want := range []string{
			// The number arm's word (**TagInt**) and the void arm's (**TagNone**), each beside its own `ret`.
			"store i32 0, i32* @gy_C_m.anst",
			"store i32 3, i32* @gy_C_m.anst",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("@gy_C_m never writes %q — one arm's kind is missing, so the caller reads whichever "+
					"word the other left:\n%s\n%s", want, body, src)
			}
		}
		if res.Output != "7 None\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "7 None\n", src)
		}
	})

	t.Run("a container method builds the object before it answers", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(c.m())\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		body := runtimeFnBody(res.IR, "define i32 @gy_C_m(")
		if body == "" {
			t.Fatalf("the module carries no @gy_C_m:\n%s", res.IR)
		}
		if strings.Contains(body, "ret i32 @") {
			t.Fatalf("@gy_C_m returns a GLOBAL — that is the operand `llc` refuses (**ADR 0166**):\n%s\n%s", body, src)
		}
		if !strings.Contains(body, "@rt_alloc") || !strings.Contains(body, "@rt_set_elem") {
			t.Fatalf("@gy_C_m never builds the list it answers — the arm's object has no handle to return:\n%s\n%s", body, src)
		}
		if !strings.Contains(body, "store i32 5, i32* @gy_C_m.anst") {
			t.Fatalf("@gy_C_m built a list but did not say so: the word it stores is not the list's:\n%s\n%s", body, src)
		}
		if res.Output != "[1, 2]\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "[1, 2]\n", src)
		}
	})

	t.Run("a one-kind method declares no word", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m())\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		if strings.Contains(res.IR, "@gy_C_m.anst") {
			t.Fatalf("a method answering ONE kind was given a word no road reads:\n%s", res.IR)
		}
		if !strings.Contains(runtimeFnBody(res.IR, "define i32 @gy_C_m("), "ret i32 7") {
			t.Fatalf("@gy_C_m no longer answers the number directly:\n%s\n%s",
				runtimeFnBody(res.IR, "define i32 @gy_C_m("), src)
		}
	})

	t.Run("a void-only method declares no word either", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        return None\nc = C()\nprint(c.m() is None)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		if strings.Contains(res.IR, "@gy_C_m.anst") {
			t.Fatalf("**ADR 0429** answers a void-only body statically; the word it stored would be write-only:\n%s",
				res.IR)
		}
		if res.Output != "True\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "True\n", src)
		}
	})

	t.Run("the caller reads the word once and runs the method", func(t *testing.T) {
		t.Parallel()
		src := methodAnswerRow + "print(c.m(0))\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if got := callOnce(main, "gy_C_m"); got != 1 {
			t.Fatalf("@main calls @gy_C_m %d time(s), want exactly 1 — the answer is read from the word the call "+
				"wrote, never by re-running the body (**ADR 0172**)\n%s\n%s", got, main, src)
		}
		if !strings.Contains(main, "@gy_C_m.anst") {
			t.Fatalf("@main prints the void arm without asking the word, which is the fold answering from the "+
				"source again\n%s\n%s", main, src)
		}
	})
}

// methodAnswerFamilyStillOwes is the filed half, factored out so the same rows are read by the compiled pin
// below and by the reference harness while the row is being written.
func methodAnswerFamilyStillOwes() []struct {
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
			// The number roads measure a container the plan hands back by NAME, which is the one thing an
			// answer built in a method's own frame is not. `len` asks the object; these never learned to.
			name:     "a container the method answers, asked of sum",
			src:      "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nprint(sum(c.m()))\n",
			wantExit: 1, wantStderr: "sum requires an inline list/set/dict literal",
			dossier: "**Gap R.458** — the reference prints `3`, and the module `def` twin refuses with the same " +
				"sentence: `sum(build())` has never been answered, only `len(build())` has",
		},
		{
			// The binding road records the kind of a container a `def` built; for a method the record is
			// written and the answer still reads as one word to the roads that walk the name.
			name:     "a bound method answer, measured",
			src:      "class C():\n    def m(self):\n        return [1, 2]\nc = C()\nys = c.m()\nprint(len(ys))\n",
			wantExit: 1, wantStderr: "len of a non-string variable",
			dossier: "**Gap R.459** — the reference prints `2`; `ys = build()` measures, `ys = c.m()` does not, " +
				"which is **Gap R.67**'s record asked of a class's answer",
		},
		{
			// One word per ANSWERABLE BODY, not per call (**ADR 0368**'s choice, inherited by this round): two
			// calls of one carried body in one expression leave the word holding whichever ran last, and the
			// comparison reads one tag for both sides.
			name:     "two calls of one carried body, compared",
			src:      "def f(x):\n    if x:\n        return \"s\"\n    return None\n\nprint(f(1) == f(0))\n",
			wantExit: 0, wantOut: "True\n",
			dossier: "**Gap R.460** — the reference prints `False`; `f(1) == g(1)` across TWO bodies prints the " +
				"reference's bytes, so the collision is one word shared by two calls of the SAME body, and each call " +
				"bound to its own name is right too",
		},
		{
			name:     "two calls of one carried body, asked for their truth",
			src:      "def f(x):\n    if x:\n        return \"s\"\n    return None\n\nprint(bool(f(1)), bool(f(0)))\n",
			wantExit: 0, wantOut: "False False\n",
			dossier: "**Gap R.460** — the reference prints `True False`; the second call's word is the one both " +
				"positions read, and `print(f(1), f(0))` — where the printer takes the pair per argument — is right",
		},
		{
			// The same collision at a class's door, and the one item in this cycle that moved AWAY from a right
			// answer: the parent's `True` for the text arm's truth came from `strMethodFuncs` claiming a carried
			// body (ADR 0368's one guess, **Gap R.219**'s class), which **ADR 0430**'s `answerOwnsMethod` gate
			// removes. The right answer for BOTH arms needs the per-call word **Gap R.460** owns.
			name:     "two calls of one carried method, asked for their truth",
			src:      "class C():\n    def m(self, x):\n        if x:\n            return \"s\"\n        return None\n\nc = C()\nprint(bool(c.m(1)), bool(c.m(0)))\n",
			wantExit: 0, wantOut: "False False\n",
			dossier: "**Gap R.460** — the reference prints `True False`; the parent printed `True True`, whose " +
				"first half was right by the WRONG road (the text-method table), and whose second half was the " +
				"void arm rendered as a text",
		},
		{
			// The comparison door still asks a CONTAINER-vs-container question of the source, and a carried
			// method answer is neither side's kind as written.
			name:     "a mixed container answer, compared with the literal one arm answers",
			src:      "class C():\n    def m(self, x):\n        if x:\n            return [1, 2]\n        return None\n\nc = C()\nprint(c.m(1) == [1, 2])\n",
			wantExit: 1, wantStderr: "comparing a container with",
			dossier: "**Gap R.461** — the reference prints `True`; `c.m() == [1, 2]` of a body answering ONE " +
				"container prints it, and this refusal also names `*lang.Call` to the program (**Gap R.116**)",
		},
	}
}

// TestWhatTheMethodAnswerStillOwesIsFiledNotFixed is the honest half: what the round leaves wrong, pinned
// with the reference's answer beside this backend's, so the day a door opens the row fails and the row in
// `roadmap.md` can be closed. A refusal stays a refusal (exit 1 — **ADR 0166** forbids exit 2), and a wrong
// answer stays a wrong answer at the exit code of success.
func TestWhatTheMethodAnswerRoundStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range methodAnswerFamilyStillOwes() {

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
				t.Fatalf("%s (%s): exited %d where this row pins a refusal", tc.name, tc.dossier, res.Code)
			}
			if res.Code != 0 {
				t.Fatalf("%s (%s): exited %d, want 0 with %q (stderr %q)", tc.name, tc.dossier, res.Code, tc.wantOut, res.Stderr)
			}
			if res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, this row pins %q", tc.name, tc.dossier, res.Output, tc.wantOut)
			}
		})
	}
}
