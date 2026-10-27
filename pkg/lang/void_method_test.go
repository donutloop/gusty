// pkg/lang/void_method_test.go — ADR 0429 · roadmap Gap R.452: a METHOD whose body answers nothing but the
// void answers the void, and the verdict is asked of the BODY.
//
// **ADR 0427** connected the comparison fold to **ADR 0368**'s answer plan, and **ADR 0428** carried that
// verdict through a forwarding body. Both stopped at the callee's own NAME, because both ask tables keyed by
// a module-level `def` — `g.fds`, `answerKindPlan.void`. A method's body lives in the class's own table, so
// the same question about it was answered by nobody:
//
//	class C():
//	    def m(self):
//	        return None
//	c = C()
//	print(c.m() is None)   # the reference: True    this backend, before: False  — at exit 0
//	print(c.m())           # the reference: None    this backend, before: 0      — at exit 0
//
// Two doors, one value (**ADR 0232**): `print(f())` had always written `None` for a `def`, and the fold
// beside it compared the void's payload — the word `0`, which an int slot legally carries (**ADR 0187**).
// For a method BOTH doors were reading the payload, because the one predicate that owns the question
// (`isNoneExpr`) had never been told a method has a body at all.
//
// The fix is one more table to look in, not a new door: `methodAnswersOnlyTheVoid` asks the same
// `answerBodyPlan` the module road asks — the walk that stops at a nested `def` and refuses a body with any
// arm it cannot name — and `methodBodyOf` resolves the callee the way the dispatch does, by the RECEIVER'S
// CLASS and the name (**Gap R.66**: the compiled backend walks every base, so an inherited method is not an
// ambiguous one).
//
// Rows are judged on BOTH witness legs (**ADR 0302**): this compiler's module, and the answer
// `testdata/interpreter-golden.json` holds for that exact source, where a MISSING record fails the case. The
// controls are as load-bearing as the fixes: a method answering a number, a text or a verdict keeps its own
// answer, and the container-method refusal this row sits beside is not silently widened.

package lang

import (
	"regexp"
	"strings"
	"testing"
)

// voidMethodRow is the row's own module: one class, one method, one body that answers nothing but the void.
const voidMethodRow = "class C():\n    def m(self):\n        return None\nc = C()\n"

// voidMethodFalloffRow is the same body spelled `pass`: no `return` road at all, which is the shape the
// module road has always answered (`fdReturnsValue`) and the method road never did.
const voidMethodFalloffRow = "class C():\n    def m(self):\n        pass\nc = C()\n"

// voidMethodFamily is the answered family.
func voidMethodFamily() []struct {
	name string
	src  string
	want string
} {
	return []struct {
		name string
		src  string
		want string
	}{
		// The row's own program, and the four spellings of the question.
		{"the identity question", voidMethodRow + "print(c.m() is None)\n", "True\n"},
		{"its equality twin", voidMethodRow + "print(c.m() == None)\n", "True\n"},
		{"the negated equality", voidMethodRow + "print(c.m() != None)\n", "False\n"},
		{"the negated identity", voidMethodRow + "print(c.m() is not None)\n", "False\n"},
		// The void written on either side of the question.
		{"the void written first (equality)", voidMethodRow + "print(None == c.m())\n", "True\n"},
		{"the void written first (identity)", voidMethodRow + "print(None is c.m())\n", "True\n"},
		// The positions the answer is read at.
		{"printed", voidMethodRow + "print(c.m())\n", "None\n"},
		{"printed twice", voidMethodRow + "print(c.m(), c.m())\n", "None None\n"},
		{"rendered by str and repr", voidMethodRow + "print(str(c.m()), repr(c.m()))\n", "None None\n"},
		{"in a list, a dict and a truth test", voidMethodRow + "print([c.m()], {1: c.m()}, bool(c.m()))\n", "[None] {1: None} False\n"},
		{"bound, then asked", voidMethodRow + "r = c.m()\nprint(r is None, r)\n", "True None\n"},
		{"membership in the list that holds it", voidMethodRow + "print(c.m() in [None])\n", "True\n"},
		{"printed and compared on one line", voidMethodRow + "print(c.m(), c.m() == None, c.m() is None)\n", "None True True\n"},
		// The collisions this row exists for: the void's word is `0`, and the fold used to call it a number.
		{"the void against zero, False and one", voidMethodRow + "print(c.m() == 0, c.m() == False, c.m() == 1)\n", "False False False\n"},
		// The body that falls off its end, with and without a side effect.
		{"a body that falls off its end, printed", voidMethodFalloffRow + "print(c.m())\n", "None\n"},
		{"a body that falls off its end, asked", voidMethodFalloffRow + "print(c.m() is None)\n", "True\n"},
		{
			"a body that prints and falls off its end",
			"class C():\n    def m(self):\n        print(\"side\")\nc = C()\nprint(c.m(), c.m() is None)\n",
			"side\nside\nNone True\n",
		},
		{
			"a body that prints and returns None",
			"class C():\n    def m(self):\n        print(\"side\")\n        return None\nc = C()\nprint(c.m())\n",
			"side\nNone\n",
		},
		// The receiver, the parameters, and the shape of the call.
		{"a method that takes a parameter", "class C():\n    def m(self, x):\n        return None\nc = C()\nprint(c.m(3) is None, c.m(3))\n", "True None\n"},
		{"a fresh instance as the receiver", "class C():\n    def m(self):\n        return None\nprint(C().m() is None)\n", "True\n"},
		{"two instances of one class", "class C():\n    def m(self):\n        return None\na = C()\nb = C()\nprint(a.m() is None, b.m() is None)\n", "True True\n"},
		// The dunder spellings the row's Definition of done asks for beside the ordinary name.
		{
			"a constructor asked as an ordinary call",
			"class C():\n    def __init__(self):\n        self.x = 1\nc = C()\nprint(c.__init__() is None, c.__init__())\n",
			"True None\n",
		},
		{
			"a setter body with no return at all",
			"class C():\n    def reset(self):\n        self.x = 0\nc = C()\nprint(c.reset(), c.reset() is None)\n",
			"None True\n",
		},
		{
			"two classes, each with its own constructor body",
			"class A():\n    def __init__(self):\n        self.x = 1\nclass B():\n    def __init__(self):\n        self.y = 2\na = A()\nb = B()\nprint(a.__init__() is None, b.__init__() is None)\n",
			"True True\n",
		},
		// One method NAME, two classes, two different bodies: the verdict is keyed by the class, so a
		// name two classes share does not make it ambiguous — the receiver does.
		{
			"one method name, two classes, two answers",
			"class A():\n    def m(self):\n        return None\nclass B():\n    def m(self):\n        return 7\nprint(A().m(), B().m())\n",
			"None 7\n",
		},
		{
			"a module def sharing the method's name",
			"def m():\n    return 7\nclass C():\n    def m(self):\n        return None\nc = C()\nprint(m(), c.m())\n",
			"7 None\n",
		},
		// The dispatch walks every base (**Gap R.66**), so the verdict may too.
		{
			"an inherited method",
			"class A():\n    def m(self):\n        return None\nclass B(A):\n    pass\nb = B()\nprint(b.m() is None, b.m())\n",
			"True None\n",
		},
		// The void reached through a call the method forwards (**ADR 0428**'s arm, one class later).
		{
			"a method forwarding a module def's void answer",
			"def f():\n    return None\nclass C():\n    def m(self):\n        return f()\nc = C()\nprint(c.m() is None)\n",
			"True\n",
		},
		{
			"a method forwarding a void call, printed and compared",
			"def f():\n    return None\nclass C():\n    def m(self):\n        return f()\nc = C()\nprint(c.m(), c.m() == None, bool(c.m()))\n",
			"None True False\n",
		},
		// The controls. A method that answers something keeps it: reading any of these as the void would
		// print `None` for the value the program answers.
		{
			"a method answering a number keeps it",
			"class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m(), c.m() == None, c.m() is None)\n",
			"7 False False\n",
		},
		{
			"a method answering a text keeps it",
			"class C():\n    def m(self):\n        return \"s\"\nc = C()\nprint(c.m(), c.m() == None, c.m() is None)\n",
			"s False False\n",
		},
		{
			"a method answering a verdict keeps it",
			"class C():\n    def m(self):\n        return True\nc = C()\nprint(c.m(), c.m() is None, bool(c.m()))\n",
			"True False True\n",
		},
		{
			"a method answering the void beside a method answering a number",
			"class C():\n    def m(self):\n        return None\n    def n(self):\n        return 7\nc = C()\nprint(c.m(), c.n(), c.m() is None, c.n() is None)\n",
			"None 7 True False\n",
		},
	}
}

// TestTheVoidAnsweringMethodAnswersEveryQuestion is the family on both witness legs.
func TestTheVoidAnsweringMethodAnswersEveryQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range voidMethodFamily() {
		tc := tc
		if tc.want == "" {
			t.Fatalf("%q has no expectation, so it certifies nothing", tc.name)
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
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
					"stale against the reference\n%s", recorded, got, tc.src)
			}
		})
	}
}

// voidMethodPositions walks the places a condition may stand: each has a road of its own, and **Gap
// R.449** is the proof that two roads can disagree about one question inside one body.
func voidMethodPositions() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"an if head", voidMethodRow + "if c.m() is None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "y\n"},
		{"a negated if head", voidMethodRow + "if c.m() != None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a while head", voidMethodRow + "while c.m() is None:\n    print(\"once\")\n    break\n", "once\n"},
		{"a short-circuit's right operand", voidMethodRow + "print(c.m() is None and \"t\")\n", "t\n"},
		{"a short-circuit that chooses the void", voidMethodRow + "print(c.m() is None or \"t\")\n", "True\n"},
		{"a ternary test", voidMethodRow + "print(1 if c.m() is None else 0)\n", "1\n"},
		{"a negated test", voidMethodRow + "print(not c.m())\n", "True\n"},
		{"a loop element", voidMethodRow + "for v in [c.m()]:\n    print(v is None, v)\n", "True None\n"},
		{"asked twice in one statement", voidMethodRow + "print(c.m() == None, c.m() == None)\n", "True True\n"},
		{"a void method beside a module function", voidMethodRow + "def f():\n    return None\nprint(f() is None, c.m() is None)\n", "True True\n"},
		// The controls, in the same positions: a method that answers something keeps its own road.
		{"a number method in an if head", "class C():\n    def m(self):\n        return 7\nc = C()\nif c.m() == None:\n    print(\"y\")\nelse:\n    print(\"n\")\n", "n\n"},
		{"a verdict method asked for its truth", "class C():\n    def m(self):\n        return True\nc = C()\nif c.m():\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
		{"a number method's truth", "class C():\n    def m(self):\n        return 0\nc = C()\nif c.m():\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "no\n"},
	}
}

func TestTheVoidAnsweringMethodHoldsInEveryPosition(t *testing.T) {
	t.Parallel()
	for _, tc := range voidMethodPositions() {
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
		})
	}
}

// attrCall mints the AST for `recv.name()`, the shape `isNoneExpr`'s new arm reads.
func attrCall(recv Expr, name string) *Call {
	return &Call{Fn: &Attr{Obj: recv, Name: &Name{Value: name}}}
}

// TestTheVoidAnsweringMethodIsTheBodyAndNotTheName asks the predicate DIRECTLY, over a parsed program, in
// both directions. The end-to-end rows cannot tell "the body was read" from "the call happened to lower to
// zero"; these can, and they are where a widening past a void-only body fails.
func TestTheVoidAnsweringMethodIsTheBodyAndNotTheName(t *testing.T) {
	t.Parallel()
	const src = `class C():
    def voidReturn(self):
        return None
    def voidFalloff(self):
        pass
    def voidBare(self):
        return
    def forwardsVoid(self):
        return nothing()
    def number(self):
        return 7
    def text(self):
        return "s"
    def verdict(self):
        return True
    def container(self):
        return [1, 2]
    def varied(self, x):
        if x:
            return None
        return 7
    def readsASelfCall(self):
        return self.voidReturn()
    def readsACarriedCall(self):
        return self.varied()
    def readsItself(self):
        return self.readsItself()
    def callsAModuleName(self):
        return number2()
    def generator(self):
        yield 1
    async def coroutine(self):
        return None
    @property
    def decorated(self):
        return None
class Base():
    def inherited(self):
        return None
class Sub(Base):
    pass
def nothing():
    return None
def number2():
    return 3
c = C()
sub = Sub()
s = "abc"
xs = [1, 2]
print(1)
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	g := &irGen{answer: answerKindPlanOf(prog, nil), methodCensus: methodCensus(prog, nil)}

	for _, tc := range []struct {
		name string
		recv Expr
		meth string
		want bool
	}{
		{"a body that writes return None", &Name{Value: "c"}, "voidReturn", true},
		{"a body that falls off its end", &Name{Value: "c"}, "voidFalloff", true},
		{"a body that returns bare", &Name{Value: "c"}, "voidBare", true},
		{"a body forwarding a void-answering def", &Name{Value: "c"}, "forwardsVoid", true},
		{"an inherited void body", &Name{Value: "sub"}, "inherited", true},
		{"a body answering a number", &Name{Value: "c"}, "number", false},
		{"a body answering a text", &Name{Value: "c"}, "text", false},
		{"a body answering a verdict", &Name{Value: "c"}, "verdict", false},
		{"a body answering a container", &Name{Value: "c"}, "container", false},
		{"a body whose other arm answers a number", &Name{Value: "c"}, "varied", false},
		// `readsASelfCall` FORWARDS the void — `return self.voidReturn()` answers nothing but the void once the
		// arm can read the callee — so the verdict is now TRUE, which is what this case's own sentence demands
		// ("the verdict comes from the BODY"). It read `false` while `answerArmOf` could not name an attribute
		// callee at all, which is the hole **ADR 0431** / roadmap **Gap R.454** fills.
		{"a body that forwards a call to another method", &Name{Value: "c"}, "readsASelfCall", true},
		// The two directions the forwarding arm still declines, each read as "I have not read this body": a
		// call to a method that answers a VALUE may never be read as the void, and a call to the body ITSELF is
		// excluded the way **ADR 0273** excludes the name road's self-call.
		{"a body forwarding a call to a value-answering method", &Name{Value: "c"}, "readsACarriedCall", false},
		{"a body forwarding a call to itself", &Name{Value: "c"}, "readsItself", false},
		{"a body that calls a module def by name", &Name{Value: "c"}, "callsAModuleName", false},
		{"a generator body", &Name{Value: "c"}, "generator", false},
		{"an async body", &Name{Value: "c"}, "coroutine", false},
		{"a decorated body", &Name{Value: "c"}, "decorated", false},
		{"a method the class does not declare", &Name{Value: "c"}, "nowhere", false},
		{"a receiver the program never made", &Name{Value: "nowhere"}, "voidReturn", false},
		{"a text receiver", &Name{Value: "s"}, "upper", false},
		{"a list receiver", &Name{Value: "xs"}, "append", false},
		{"a fresh instance of a known class", &Call{Fn: &Name{Value: "C"}}, "voidReturn", true},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			call := attrCall(tc.recv, tc.meth)
			gotMethod := g.methodCallAnswersOnlyTheVoid(call)
			if gotMethod != tc.want {
				t.Fatalf("methodCallAnswersOnlyTheVoid(%s.%s()) = %v, want %v — the verdict comes from the BODY "+
					"and a body that answers a value may never be read as the void (ADR 0368's rule, one table "+
					"later)", "c", tc.meth, gotMethod, tc.want)
			}
			// The predicate the whole toolchain asks has to agree with the door this row added: if the
			// method door answers, `isNoneExpr` must answer, or one road prints `None` and the next
			// compares a zero (**ADR 0232**).
			if got := g.isNoneExpr(call); got != tc.want {
				t.Fatalf("isNoneExpr(%s.%s()) = %v, want %v", "c", tc.meth, got, tc.want)
			}
		})
	}

	t.Run("a self receiver reads the class being lowered", func(t *testing.T) {
		t.Parallel()
		g2 := &irGen{answer: g.answer, methodCensus: g.methodCensus, curClassSrc: "C"}
		if !g2.methodCallAnswersOnlyTheVoid(attrCall(&Name{Value: "self"}, "voidReturn")) {
			t.Fatal("self.voidReturn() inside C was not read as the void — a body cannot ask about its own helper")
		}
		if g2.methodCallAnswersOnlyTheVoid(attrCall(&Name{Value: "self"}, "number")) {
			t.Fatal("self.number() inside C was read as the void")
		}
		g3 := &irGen{answer: g.answer, methodCensus: g.methodCensus, curClassSrc: "Sub"}
		if !g3.methodCallAnswersOnlyTheVoid(attrCall(&Name{Value: "self"}, "inherited")) {
			t.Fatal("self.inherited() inside a subclass was not read as the void, though the dispatch finds it")
		}
	})

	t.Run("an unknown class is no body at all", func(t *testing.T) {
		t.Parallel()
		if g.methodCallAnswersOnlyTheVoid(attrCall(&Name{Value: "z"}, "voidReturn")) {
			t.Fatal("a receiver of an unknown class was read through some class's body")
		}
		if g.methodCallAnswersOnlyTheVoid(nil) || g.methodCallAnswersOnlyTheVoid(&Call{Fn: &Name{Value: "m"}}) {
			t.Fatal("the method door answered a call that is not an attribute call — the NAME road owns those")
		}
	})

	t.Run("a duplicated class name is no class", func(t *testing.T) {
		t.Parallel()
		dup, err := Parse("class C():\n    def m(self):\n        return None\nclass C():\n    def m(self):\n        return 7\nprint(1)\n")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		gd := &irGen{answer: answerKindPlanOf(dup, nil), methodCensus: methodCensus(dup, nil)}
		if gd.methodCallAnswersOnlyTheVoid(attrCall(&Call{Fn: &Name{Value: "C"}}, "m")) {
			t.Fatal("two classes, one name: the compiler chose a body to read — one callable, one name is no " +
				"name at all (ADR 0299)")
		}
	})
}

// callOnce counts `call i32 @name(` in a body.
func callOnce(body, name string) int {
	return len(regexp.MustCompile(`call i32 @`+name+`\(`).FindAllString(body, -1))
}

// TestWhatTheVoidAnsweringMethodEmitsIsTheAnswerAndTheCall is the module half. The fold answers these
// questions with a CONSTANT, so the witness is threefold: the call is still emitted (a fold that dropped it
// would print `True` for a body whose `return` prints), no run-time comparison door is reached for a question
// the source can name, and the print road still asks its printer. The control rows are the other direction:
// a method answering a number stores no answer word and never calls the void printer.
func TestWhatTheVoidAnsweringMethodEmitsIsTheAnswerAndTheCall(t *testing.T) {
	t.Parallel()
	t.Run("a folded comparison keeps the method call", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        return None\nc = C()\nr = c.m() == None\nprint(r)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if got := callOnce(main, "gy_C_m"); got == 0 {
			t.Fatalf("@main never calls @gy_C_m; the fold may answer from the body but it may not delete the "+
				"call (**ADR 0172**)\n%s\n%s", main, src)
		}
		for _, banned := range []string{`@rt_payload_eq`, `@rt_pair_truth`, `@rt_icmp_values`} {
			if strings.Contains(main, banned) {
				t.Fatalf("%s is in @main — a void method answer is a question the compiler can name, and it went "+
					"to a run-time door anyway\n%s\n%s", banned, main, src)
			}
		}
		if res.Output != "True\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "True\n", src)
		}
	})

	t.Run("the print road asks the void printer and still runs the method", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        print(\"side\")\n        return None\nc = C()\nprint(c.m())\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		main := runtimeFnBody(res.IR, "define i32 @main()")
		if !strings.Contains(main, "call void @rt_print_none(i32 0)") {
			t.Fatalf("@main does not ask the void printer — the predicate answered and the print road did not:\n%s\n%s", main, src)
		}
		if got := callOnce(main, "gy_C_m"); got != 1 {
			t.Fatalf("@main calls @gy_C_m %d time(s), want exactly 1: the argument is evaluated for its effects "+
				"before None is written\n%s\n%s", got, main, src)
		}
		if res.Output != "side\nNone\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "side\nNone\n", src)
		}
	})

	t.Run("a forwarding method calls its callee once", func(t *testing.T) {
		t.Parallel()
		src := "def f():\n    print(\"inner\")\n    return None\nclass C():\n    def m(self):\n        return f()\nc = C()\nprint(c.m() is None)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		body := runtimeFnBody(res.IR, "define i32 @gy_C_m(")
		if body == "" {
			t.Fatalf("the module carries no @gy_C_m:\n%s", res.IR)
		}
		if got := callOnce(body, "gy_f"); got != 1 {
			t.Fatalf("@gy_C_m calls @gy_f %d time(s), want exactly 1 — the arm is a kind, not a substitution, and "+
				"a forwarding body that stopped calling would drop the callee's effects\n%s\n%s", got, body, src)
		}
		if res.Output != "inner\nTrue\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "inner\nTrue\n", src)
		}
	})

	t.Run("a method answering a number keeps its own roads", func(t *testing.T) {
		t.Parallel()
		src := "class C():\n    def m(self):\n        return 7\nc = C()\nprint(c.m(), c.m() is None)\n"
		res, err := JIT(src, 0)
		if err != nil {
			t.Fatalf("the module would not build: %v\n%s", err, src)
		}
		body := runtimeFnBody(res.IR, "define i32 @gy_C_m(")
		if body == "" {
			t.Fatalf("the module carries no @gy_C_m:\n%s", res.IR)
		}
		if strings.Contains(body, `@gy_C_m.anst`) {
			t.Fatalf("a method answering a NUMBER stores an answer kind — the void arm reached past a body that "+
				"answers a value:\n%s\n%s", body, src)
		}
		if strings.Contains(runtimeFnBody(res.IR, "define i32 @main()"), "call void @rt_print_none") {
			// The RUNTIME module defines the void printer, so the claim is about this program's `main`.
			t.Fatalf("@main asks the void printer for a body that answers 7:\n%s\n%s",
				runtimeFnBody(res.IR, "define i32 @main()"), src)
		}
		if res.Output != "7 False\n" {
			t.Fatalf("printed %q, want %q\n%s", res.Output, "7 False\n", src)
		}
	})
}

// TestWhatTheVoidAnsweringMethodStillOwesIsFiledNotFixed pins what this row leaves owed, with today's bytes
// beside the reference's. Every one is byte-identical on this row's parent, measured with a `git worktree`
// build of it — the sentence that makes them other rows' lesions rather than this commit's damage. An answer
// that arrives here FAILS the case.
func TestWhatTheVoidAnsweringMethodStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		// The two pins that stood here are DELETED rather than edited. `class C():` /
		// `    def m(self, x):` / `        if x:` / `            return 7` / `        return None` /
		// `c = C()` / `print(c.m(1), c.m(0), c.m(0) is None)`, held at `7 0 False` beside the reference's
		// `7 None True`, and `class C():` / `    def m(self):` / `        return [1, 2]` / `c = C()` /
		// `print(c.m())`, held at a module `llc` REFUSES (**ADR 0166**'s compiler-bug exit code) — both are
		// closed by **ADR 0430**, which gives the answer direction the METHOD round it never had: the plan
		// walks a class body, a carried method keeps the arm that ran in its own word, and a body answering
		// a container builds it before the word is written. The programs print the reference's bytes and
		// stand in the answered family of `pkg/lang/method_answer_test.go` and
		// `integration/method_answer_test.go`, on the compiled leg, the record leg AND the reference.
		// The pin that stood here is DELETED rather than edited. `class C():` / `    def m(self):` /
		// `        return None` / `    def n(self):` / `        return self.m()` / `c = C()` /
		// `print(c.n() is None, c.n())`, held at `False 0` beside the reference's `True None`, is closed by
		// **ADR 0431**, which gives `answerArmOf` the ATTRIBUTE arm beside its `*Name` one: a body that
		// forwards a call to another method of its class answers that method's OWN word — the void's constant,
		// or the carried body's run-time tag — and the program now prints the reference's bytes. It stands in
		// the answered family of `pkg/lang/method_forward_test.go` and `integration/method_forward_test.go`,
		// on the compiled leg, the record leg and the reference leg.
		{
			// A generator METHOD: the body is a generator the front end never recognised because it lives
			// in a class, so `yield` is refused as being outside a generator function.
			name:       "a generator method",
			src:        "class C():\n    def m(self):\n        yield 1\nc = C()\nprint(c.m() is None)\n",
			wantExit:   1,
			wantStderr: "yield outside a generator function",
			dossier:    "**Gap R.455** — the reference prints `False`; a `def` in a class body is a method by position and never gets the generator reading a module `def` gets",
		},
		{
			// A method writing into its own class's FIELD container: the mutation door reads a receiver it
			// cannot name as a text and refuses, long before any answer is asked.
			name:       "a method appending to a field container",
			src:        "class C():\n    def __init__(self):\n        self.items = []\n    def add(self, x):\n        self.items.append(x)\nc = C()\nprint(c.add(1))\n",
			wantExit:   1,
			wantStderr: "string-method append on a receiver that is not a text the compiler can read",
			dossier:    "**Gap R.456** — the reference prints `None`; `self.items` is an attribute the string-method door was never taught to ask about",
		},
		{
			// A nested `def` inside a method emits the GC frame prologue twice.
			name:       "a method containing a nested def",
			src:        "class C():\n    def m(self):\n        def inner():\n            return 7\n        return None\nc = C()\nprint(c.m())\n",
			wantExit:   2,
			wantStderr: "multiple definition of local value named 'gc.frame.base'",
			dossier:    "**Gap R.457** — the reference prints `None`; the nested body re-emits the frame prologue its enclosing method already wrote",
		},
		{
			// A refusal, pinned AS one: exit 1, exit 2 forbidden (**ADR 0166**). The row's neighbour, and
			// the thing this change must not silently widen.
			name:       "a container pop over a name, asked against the void",
			src:        "d = {\"a\": 1}\nprint(d.pop(\"a\") is None)\n",
			wantExit:   1,
			wantStderr: "the compiled backend folds container methods only over a literal written at the call",
			dossier:    "**Gap R.174** (standing) — the same refusal this toolchain has always spent here, and an attribute call this row's door must not read",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if tc.wantExit != 1 && tc.wantExit != 2 {
					t.Fatalf("%s (%s): failed in a way this pin does not describe: %v", tc.name, tc.dossier, err)
				}
				if !strings.Contains(err.Error(), tc.wantStderr) {
					t.Fatalf("%s (%s): failed with %q, which is not the sentence this pin holds (%q)",
						tc.name, tc.dossier, err.Error(), tc.wantStderr)
				}
				return
			}
			if tc.wantExit == 1 {
				t.Fatalf("%s (%s): the program compiled — this pin holds a REFUSAL, and a refusal that stopped "+
					"refusing is a row that was closed without deleting its pin\n%s", tc.name, tc.dossier, tc.src)
			}
			if tc.wantExit == 2 {
				t.Fatalf("%s (%s): exited %d with stdout %q — this pin holds the module `llc` rejects (**ADR 0166**), "+
					"and a module that started assembling is a row that moved\n%s", tc.name, tc.dossier, res.Code, res.Output, tc.src)
			}
			if res.Code != tc.wantExit {
				t.Fatalf("%s (%s): exited %d with stdout %q stderr %q — an answer that arrived here means the "+
					"row was closed and this table did not follow\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.src)
			}
			if tc.wantOut != "" && res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, want %q\n%s", tc.name, tc.dossier, res.Output, tc.wantOut, tc.src)
			}
		})
	}
}
