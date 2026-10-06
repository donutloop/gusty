package lang

import "fmt"
import "os"
import "strings"
import "testing"

func TestEvalUserFunc(t *testing.T) {
	v, _, err := evalGolden(t, "def double(x):\n    return x * 2\ndouble(5)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
}

func TestEvalUserFuncTwoParams(t *testing.T) {
	v, _, err := evalGolden(t, "def add(a, b):\n    return a + b\nadd(3, 4)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalIfElse(t *testing.T) {
	v, _, err := evalGolden(t, "x = 1\nif x < 2:\n    print(10)\nelse:\n    print(20)\nx")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
}

func TestEvalElifChain(t *testing.T) {
	// elif taken
	v, _, err := evalGolden(t, "x = 3\nif x < 2:\n    y = 10\nelif x < 4:\n    y = 20\nelse:\n    y = 30\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 20 {
		t.Fatalf("got %d, want 20", v)
	}
	// elif false, else taken
	v, _, err = evalGolden(t, "x = 9\nif x < 2:\n    y = 10\nelif x < 4:\n    y = 20\nelse:\n    y = 30\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 30 {
		t.Fatalf("got %d, want 30", v)
	}
}

func TestEvalElifMultiple(t *testing.T) {
	// second of three elifs taken
	v, _, err := evalGolden(t, "x = 6\nif x < 2:\n    y = 10\nelif x < 5:\n    y = 20\nelif x < 8:\n    y = 30\nelse:\n    y = 40\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 30 {
		t.Fatalf("got %d, want 30", v)
	}
	// no elif, no else -> nothing taken
	v, _, err = evalGolden(t, "x = 9\nif x < 2:\n    y = 10\nelif x < 5:\n    y = 20\ny = 99\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 99 {
		t.Fatalf("got %d, want 99", v)
	}
}

func TestEvalWhileSum(t *testing.T) {
	v, _, err := evalGolden(t, "i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
}

func TestEvalForSum(t *testing.T) {
	v, _, err := evalGolden(t, "s = 0\nfor i in range(5):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
}

func TestEvalMatch(t *testing.T) {
	v, _, err := evalGolden(t, "x = 2\nmatch x:\n    case 1:\n        print(1)\n    case 2:\n        print(2)\nx")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalBreakContinue(t *testing.T) {
	// break out of while
	v, _, err := evalGolden(t, "i = 0\nwhile i < 100:\n    i = i + 1\n    if i == 3:\n        break\ni")
	if err != nil {
		t.Fatalf("break err: %v", err)
	}
	if v != 3 {
		t.Fatalf("break got %d, want 3", v)
	}
	// continue skips increment in for loop
	v2, _, err := evalGolden(t, "s = 0\nfor i in range(5):\n    if i == 2:\n        continue\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("continue err: %v", err)
	}
	if v2 != 8 { // 0+1+3+4 (skip 2)
		t.Fatalf("continue got %d, want 8", v2)
	}
}

func TestEvalRangeTwoArg(t *testing.T) {
	// sum range(2, 5) = 2+3+4 = 9
	v, _, err := evalGolden(t, "s = 0\nfor i in range(2, 5):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("range(a,b) err: %v", err)
	}
	if v != 9 {
		t.Fatalf("range(2,5) sum got %d, want 9", v)
	}
}

func TestEvalForRangeStep(t *testing.T) {
	// positive step range(1, 5, 2) = 1+3 = 4
	v, _, err := evalGolden(t, "s = 0\nfor i in range(1, 5, 2):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("range step err: %v", err)
	}
	if v != 4 {
		t.Fatalf("range(1,5,2) sum got %d, want 4", v)
	}
	// negative step range(5, 0, -1) = 5+4+3+2+1 = 15
	v, _, err = evalGolden(t, "s = 0\nfor i in range(5, 0, -1):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("range neg step err: %v", err)
	}
	if v != 15 {
		t.Fatalf("range(5,0,-1) sum got %d, want 15", v)
	}
}

func TestEvalMatchWildcard(t *testing.T) {
	// match with _ wildcard catches unmatched value
	v, _, err := evalGolden(t, "x = 42\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(42)\nx")
	if err != nil {
		t.Fatalf("match wildcard err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalForElse(t *testing.T) {
	// for without break: else runs
	v, _, err := evalGolden(t, "s = 0\nfor i in range(3):\n    s = s + i\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("for-else err: %v", err)
	}
	if v != 103 { // 0+1+2 + 100
		t.Fatalf("got %d, want 103", v)
	}
}

func TestEvalForElseBreak(t *testing.T) {
	// break skips else
	v, _, err := evalGolden(t, "s = 0\nfor i in range(3):\n    if i == 1:\n        break\n    s = s + i\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("for-else break err: %v", err)
	}
	if v != 0 { // else skipped; only i=0 added before break
		t.Fatalf("got %d, want 0", v)
	}
}

func TestEvalWhileElse(t *testing.T) {
	v, _, err := evalGolden(t, "i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\nelse:\n    s = s + 10\ns")
	if err != nil {
		t.Fatalf("while-else err: %v", err)
	}
	if v != 13 { // 0+1+2 + 10
		t.Fatalf("got %d, want 13", v)
	}
}

func TestEvalClassMethod(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n    def sum(self):\n        return self.x + self.y\np = Point(2, 3)\np.sum()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("class err: %v", err)
	}
	if v != 5 {
		t.Fatalf("got %d, want 5", v)
	}
}

func TestEvalClassAttrSet(t *testing.T) {
	src := "class C:\n    def __init__(self):\n        self.n = 0\n    def bump(self):\n        self.n = self.n + 1\n        return self.n\nc = C()\nc.bump()\nc.bump()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("class err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalTryExcept(t *testing.T) {
	src := "x = 0\ntry:\n    x = 1 // 0\nexcept Exception:\n    x = 42\nx"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("try err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalRaiseCaught(t *testing.T) {
	src := "x = 0\ntry:\n    raise Exception\n    x = 1\nexcept Exception:\n    x = 42\nx"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("raise-catch err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalDefaultArg(t *testing.T) {
	v, _, err := evalGolden(t, "def f(a, b=10):\n    return a + b\nf(5)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 15 {
		t.Fatalf("got %d, want 15", v)
	}
}

func TestEvalKeywordArg(t *testing.T) {
	v, _, err := evalGolden(t, "def f(a, b):\n    return a * b\nf(a=3, b=4)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 12 {
		t.Fatalf("got %d, want 12", v)
	}
}

func TestEvalKeywordArgOutOfOrder(t *testing.T) {
	v, _, err := evalGolden(t, "def f(a, b):\n    return a - b\nf(b=3, a=10)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalKeywordAndDefault(t *testing.T) {
	v, _, err := evalGolden(t, "def f(a, b=5):\n    return a + b\nf(b=100, a=2)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 102 {
		t.Fatalf("got %d, want 102", v)
	}
}

func TestEvalDictSetIndexLen(t *testing.T) {
	// dict constant-key lookup.
	v, _, err := evalGolden(t, "{1: 10, 2: 20}[1]")
	if err != nil {
		t.Fatalf("dict index err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
	v, _, err = evalGolden(t, "{1: 10, 2: 20}[2]")
	if err != nil {
		t.Fatalf("dict index err: %v", err)
	}
	if v != 20 {
		t.Fatalf("got %d, want 20", v)
	}
	// len over dict and set.
	v, _, err = evalGolden(t, "len({1: 10, 2: 20})")
	if err != nil {
		t.Fatalf("len dict err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
	v, _, err = evalGolden(t, "len({1, 2, 3})")
	if err != nil {
		t.Fatalf("len set err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
	// A set subscript is the documented gusty extension: the subscript is a member the set is asked
	// about, and the answer is that member. Pinned on both engines because the compiled tag arm below a
	// slot reads a set slot the same way (roadmap L11.1, ADR 0251; docs/language.md § Dicts & sets).
	v, _, err = evalGolden(t, "{1, 2, 3}[2]")
	if err != nil {
		t.Fatalf("set index err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalDictLiteral(t *testing.T) {
	// dict literal then iterate keys and sum via comprehension
	v, _, err := evalGolden(t, "def f(d):\n    return len(d)\nf({1: 10, 2: 20})")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalListComprehension(t *testing.T) {
	v, _, err := evalGolden(t, "def g(ys):\n    s = 0\n    for y in ys:\n        s = s + y\n    return s\nv = [1, 2, 3]\ng([x * 2 for x in v])")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 12 {
		t.Fatalf("got %d, want 12", v)
	}
}

func TestEvalForOverList(t *testing.T) {
	// for-over-list sums the elements of a boxed list.
	v, _, err := evalGolden(t, "s = 0\nfor x in [1, 2, 3]:\n    s = s + x\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
	// continue skips to the next element.
	v, _, err = evalGolden(t, "s = 0\nfor x in [1, 2, 3, 4]:\n    if x == 2:\n        continue\n    s = s + x\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 8 {
		t.Fatalf("got %d, want 8", v)
	}
	// break stops; else runs only on normal completion.
	v, _, err = evalGolden(t, "s = 0\nfor x in [1, 2, 3]:\n    if x == 2:\n        break\n    s = s + x\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
	v, _, err = evalGolden(t, "s = 0\nfor x in [1, 2, 3]:\n    s = s + x\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 106 {
		t.Fatalf("got %d, want 106", v)
	}
}

func TestEvalComprehensionAssignment(t *testing.T) {
	// A comprehension assigned to a variable must bind its loop variable so
	// the body/condition can reference it. Previously the semantic analyzer
	// reported "undefined name x", short-circuiting evaluation to 0.
	v, _, err := evalGolden(t, "c = [x * 2 for x in [1, 2, 3]]\nlen(c)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}

	// With a condition referencing the comprehension variable too.
	v, _, err = evalGolden(t, "c = [x for x in [1, 2, 3, 4] if x % 2 == 0]\nlen(c)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalTernary(t *testing.T) {
	// ternary `then if cond else otherwise` picks the branch by truthiness.
	for _, tc := range []struct {
		src  string
		want int64
	}{
		{"5 if 1 else 3", 5},
		{"10 if 0 else 42", 42},
		{"7 if 2 > 1 else 99", 7},
		// right-associative: the else branch is a full ternary.
		{"1 if 0 else 2 if 1 else 3", 2},
		// used inside a larger expression.
		{"(5 if 1 else 6) + 1", 6},
	} {
		v, _, err := evalGolden(t, tc.src+"\n")
		if err != nil {
			t.Fatalf("%s: err: %v", tc.src, err)
		}
		if v != tc.want {
			t.Fatalf("%s: got %d, want %d", tc.src, v, tc.want)
		}
	}
}

func TestClosureCapturesEnclosingScope(t *testing.T) {
	src := "def make_adder(x):\n    def add(y):\n        return x + y\n    return add\nw = make_adder(5)\nw(3)"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 8 {
		t.Fatalf("got %d, want 8", v)
	}
}

func TestClosureNestedFunctionCall(t *testing.T) {
	src := "def outer(a):\n    def inner(b):\n        return a * b\n    return inner\nf = outer(6)\nf(7)"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestClosureInClosure(t *testing.T) {
	// a closure that itself returns a closure capturing both layers
	src := "def add(x):\n    def mid(y):\n        def inner(z):\n            return x + y + z\n        return inner\n    return mid\nm = add(1)\nm2 = m(2)\nm2(3)"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
}

func TestDecoratorAppliesToFunction(t *testing.T) {
	// @dec def f -> f = dec(f); dec wraps the function value.
	src := "def dec(g):\n    return g\n@dec\ndef f():\n    return 42\nf()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestDecoratorTransformsFunction(t *testing.T) {
	// dec returns a new closure that adds 1 to the decorated function's result.
	src := "def add1(g):\n    def wrap():\n        return g() + 1\n    return wrap\n@add1\ndef f():\n    return 40\nf()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 41 {
		t.Fatalf("got %d, want 41", v)
	}
}

func TestMultipleDecorators(t *testing.T) {
	// decorators apply bottom-up: f = dec2(dec1(f)).
	src := "def dec1(g):\n    def w():\n        return g() + 1\n    return w\ndef dec2(g):\n    def w():\n        return g() * 2\n    return w\n@dec1\n@dec2\ndef f():\n    return 10\nf()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 22 {
		t.Fatalf("got %d, want 22", v)
	}
}

func TestAnnotAssignOK(t *testing.T) {
	// a matching annotation is accepted and the value flows through.
	src := "x: int = 5\nx"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 5 {
		t.Fatalf("got %d, want 5", v)
	}
}

func TestAnnotAssignMismatch(t *testing.T) {
	// a list value under an int annotation is a runtime type error.
	src := "x: int = [1, 2]"
	_, _, err := evalGolden(t, src)
	if err == nil {
		t.Fatalf("expected a type mismatch error")
	}
	if !strings.Contains(err.Error(), "type mismatch") || strings.Contains(err.Error(), "argument") || strings.Contains(err.Error(), "return type mismatch") {
		t.Fatalf("got %v, want type mismatch", err)
	}
}

func TestAnnotDynAcceptsAnything(t *testing.T) {
	// any (dynamic) annotation accepts a list under an int context.
	src := "x: any = [1, 2]\nlen(x)"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestAnnotParamMismatch(t *testing.T) {
	// an annotated parameter rejects a wrong-typed argument.
	src := "def f(x: int):\n    return x\nf([1, 2])"
	_, _, err := evalGolden(t, src)
	if err == nil || (!strings.Contains(err.Error(), "type mismatch") && !strings.Contains(err.Error(), "argument") && !strings.Contains(err.Error(), "return type mismatch")) {
		t.Fatalf("got %v, want type mismatch", err)
	}
}

func TestAnnotReturnMismatch(t *testing.T) {
	// an annotated return rejects a wrong-typed returned value.
	src := "def f() -> int:\n    return [1, 2]\nf()"
	_, _, err := evalGolden(t, src)
	if err == nil || (!strings.Contains(err.Error(), "type mismatch") && !strings.Contains(err.Error(), "argument") && !strings.Contains(err.Error(), "return type mismatch")) {
		t.Fatalf("got %v, want type mismatch", err)
	}
}

func TestEvalClassInheritanceMethodResolution(t *testing.T) {
	// Child inherits a method defined only on Base.
	src := "class Base:\n    def greet(self):\n        return 41\nclass Child(Base):\n    def hi(self):\n        return 1\nc = Child()\nc.greet()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("inherit err: %v", err)
	}
	if v != 41 {
		t.Fatalf("got %d, want 41", v)
	}
}

func TestEvalClassInheritanceOverride(t *testing.T) {
	// A subclass overriding a base method uses the subclass's version.
	src := "class Base:\n    def val(self):\n        return 1\nclass Child(Base):\n    def val(self):\n        return 2\nc = Child()\nc.val()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("override err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalClassInheritanceInit(t *testing.T) {
	// __init__ inherited from Base runs when instantiating Child.
	src := "class Base:\n    def __init__(self):\n        self.n = 5\nclass Child(Base):\n    def get(self):\n        return self.n\nc = Child()\nc.get()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("init err: %v", err)
	}
	if v != 5 {
		t.Fatalf("got %d, want 5", v)
	}
}

func TestEvalClassSuperDelegation(t *testing.T) {
	// super() lets an overridden method delegate to the base implementation.
	src := "class Base:\n    def val(self):\n        return 10\nclass Child(Base):\n    def val(self):\n        return super().val() + 5\nc = Child()\nc.val()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("super err: %v", err)
	}
	if v != 15 {
		t.Fatalf("got %d, want 15", v)
	}
}

func TestEvalClassGrandChildResolution(t *testing.T) {
	// method resolution walks the whole base chain (grandparent).
	src := "class A:\n    def f(self):\n        return 7\nclass B(A):\n    def b(self):\n        return 1\nclass C(B):\n    def g(self):\n        return 1\nc = C()\nc.f()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("grandchild err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalDynamicDispatch(t *testing.T) {
	// Dynamic dispatch: the receiver's class is only known at runtime because
	// `make` returns an instance of a different class per argument. Method
	// resolution must follow the runtime instance, not a compile-time guess.
	src := "class Animal:\n    def __init__(self):\n        self.x = 1\n    def speak(self):\n        return self.x\nclass Dog(Animal):\n    def __init__(self):\n        super().__init__()\n    def speak(self):\n        return 42\ndef make(kind):\n    if kind == 1:\n        return Animal()\n    return Dog()\na = make(1)\nb = make(2)\na.speak() + b.speak()"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("dyn dispatch err: %v", err)
	}
	// a=Animal -> a.speak()=1 ; b=Dog -> b.speak()=42 ; sum=43.
	if v != 43 {
		t.Fatalf("got %d, want 43", v)
	}
}

func TestEvalImportModuleFunction(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/mylib.gy", []byte("def double(x):\n    return x * 2\n"), 0o600)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	src := "import mylib\nmylib.double(4)"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("import fn err: %v", err)
	}
	if v != 8 {
		t.Fatalf("got %d, want 8", v)
	}
}

func TestEvalImportModuleConst(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/constlib.gy", []byte("base = 21\n"), 0o600)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	src := "import constlib\nconstlib.base + 1"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("import const err: %v", err)
	}
	if v != 22 {
		t.Fatalf("got %d, want 22", v)
	}
}

func TestEvalImportMissingModule(t *testing.T) {
	dir := t.TempDir()
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	src := "import nope\nnope.x"
	_, _, err := evalGolden(t, src)
	if err == nil {
		t.Fatalf("expected import error, got nil")
	}
}

func TestEvalListIndex(t *testing.T) {
	src := "lst = [10, 20, 30]\nlst[1]"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("index err: %v", err)
	}
	if v != 20 {
		t.Fatalf("got %d, want 20", v)
	}
}

func TestEvalListIndexOutOfRange(t *testing.T) {
	src := "lst = [1, 2]\nlst[5]"
	_, _, err := evalGolden(t, src)
	if err == nil {
		t.Fatalf("expected out-of-range error")
	}
}

func TestEvalDictIndex(t *testing.T) {
	src := "d = {1: 100, 2: 200}\nd[2]"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("dict index err: %v", err)
	}
	if v != 200 {
		t.Fatalf("got %d, want 200", v)
	}
}

func TestEvalPassStatement(t *testing.T) {
	// pass inside a loop body is a no-op; iteration proceeds normally.
	v, _, err := evalGolden(t, "x = 0\nfor i in range(3):\n    pass\n    x = x + i\nx")
	if err != nil {
		t.Fatalf("loop pass err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
	// pass inside an if body: no branch taken, else still runs.
	v, _, err = evalGolden(t, "x = 0\nif 0:\n    pass\nelse:\n    x = 1\nx")
	if err != nil {
		t.Fatalf("if pass err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
	// bare pass statement at top level.
	v, _, err = evalGolden(t, "pass\nx = 42\nx")
	if err != nil {
		t.Fatalf("bare pass err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalStringConcatLen(t *testing.T) {
	// len over a string concatenation counts the joined characters.
	v, _, err := evalGolden(t, "len(\"ab\" + \"cd\")")
	if err != nil {
		t.Fatalf("concat len err: %v", err)
	}
	if v != 4 {
		t.Fatalf("got %d, want 4", v)
	}
	// empty string.
	v, _, err = evalGolden(t, "len(\"\")")
	if err != nil {
		t.Fatalf("empty len err: %v", err)
	}
	if v != 0 {
		t.Fatalf("got %d, want 0", v)
	}
}

func TestEvalStringLiteral(t *testing.T) {
	// string literals evaluate to boxed strings (last stmt is an assignment)
	v, _, err := evalGolden(t, "s = \"hello\"\ns")
	if err != nil {
		t.Fatalf("str literal err: %v", err)
	}
	if v == 0 {
		t.Fatalf("str literal returned 0, want a heap handle")
	}
	// concatenation with +
	v, _, err = evalGolden(t, "x = \"a\" + \"b\"\nx")
	if err != nil {
		t.Fatalf("concat err: %v", err)
	}
	if v == 0 {
		t.Fatalf("concat returned 0, want a heap handle")
	}
	// len of a string
	v, _, err = evalGolden(t, "len(\"hello\")")
	if err != nil {
		t.Fatalf("len str err: %v", err)
	}
	if v != 5 {
		t.Fatalf("len(str) got %d, want 5", v)
	}
}

func TestEvalDictVariableIndex(t *testing.T) {
	// d[key] on a dict variable resolves at codegen time.
	v, _, err := evalGolden(t, "d = {1: 10, 2: 20}\nd[1]")
	if err != nil {
		t.Fatalf("d[1] err: %v", err)
	}
	if v != 10 {
		t.Fatalf("d[1] got %d, want 10", v)
	}
}

func TestEvalLenStringVariable(t *testing.T) {
	// len(s) on a string variable resolves at codegen time.
	v, _, err := evalGolden(t, "s = \"abc\"\nlen(s)")
	if err != nil {
		t.Fatalf("len(s) err: %v", err)
	}
	if v != 3 {
		t.Fatalf("len(s) got %d, want 3", v)
	}

	v, _, err = evalGolden(t, "a = \"hello\"\nlen(a)")
	if err != nil {
		t.Fatalf("len(a) err: %v", err)
	}
	if v != 5 {
		t.Fatalf("len(a) got %d, want 5", v)
	}
}

func TestEvalUnionVarIntStr(t *testing.T) {
	// A union-annotated variable accepts an int member (regression: previously
	// rejected at runtime with "expected value but got int").
	v, _, err := evalGolden(t, "x: int | str = 42\nx")
	if err != nil {
		t.Fatalf("union int|str int member: %v", err)
	}
	if v != 42 {
		t.Fatalf("union int|str int member got %d, want 42", v)
	}
}

func TestEvalUnionVarStrMember(t *testing.T) {
	// A union-annotated variable accepts a string member, and the value that comes back is the
	// string — which is what a snippet answers, in the end.
	goldenReprIs(t, "x: int | str = \"hello\"\nx", "hello")
}

func TestEvalUnionVarIntFloat(t *testing.T) {
	// int member of an int | float union.
	v, _, err := evalGolden(t, "x: int | float = 7\nx")
	if err != nil {
		t.Fatalf("union int|float int member: %v", err)
	}
	if v != 7 {
		t.Fatalf("union int|float int member got %d, want 7", v)
	}
	// float member of an int | float union.
	goldenReprIs(t, "x: int | float = 2.5\nx", "2.5")
}

func TestEvalFloatLiteral(t *testing.T) {
	// float literals evaluate to boxed floats
	v, _, err := evalGolden(t, "x = 1.5\nx")
	if err != nil {
		t.Fatalf("float literal err: %v", err)
	}
	if v == 0 {
		t.Fatalf("float literal returned 0, want a heap handle")
	}
	// float + int arithmetic
	v, _, err = evalGolden(t, "x = 2.0 + 3\nx")
	if err != nil {
		t.Fatalf("float add err: %v", err)
	}
	if v == 0 {
		t.Fatalf("float add returned 0, want a heap handle")
	}
	// float division
	v, _, err = evalGolden(t, "x = 7.0 / 2.0\nx")
	if err != nil {
		t.Fatalf("float div err: %v", err)
	}
	if v == 0 {
		t.Fatalf("float div returned 0, want a heap handle")
	}
	// float comparison returns int 1/0
	v, _, err = evalGolden(t, "1.5 > 1\n1.5 < 1")
	if err != nil {
		t.Fatalf("float cmp err: %v", err)
	}
	if v != 0 {
		t.Fatalf("1.5 < 1 got %d, want 0", v)
	}
}

func TestEvalAndOrFloorDiv(t *testing.T) {
	// `and`/`or` hand back an **operand**, not a verdict (roadmap Gap R.147, ADR 0269): the reference
	// evaluates the left, tests it, and returns whichever operand the test chose. The verdict these used
	// to answer — `1 and 2` as the 1 — is the exit-0 wrong answer the row was filed for.
	v, _, err := evalGolden(t, "1 and 0")
	if err != nil {
		t.Fatalf("and err: %v", err)
	}
	if v != 0 {
		t.Fatalf("1 and 0 got %d, want 0", v)
	}
	v, _, err = evalGolden(t, "1 and 2")
	if err != nil {
		t.Fatalf("and err: %v", err)
	}
	if v != 2 {
		t.Fatalf("1 and 2 got %d, want 2 (the operand the test chose, not the verdict)", v)
	}
	v, _, err = evalGolden(t, "0 or 7")
	if err != nil {
		t.Fatalf("or err: %v", err)
	}
	if v != 7 {
		t.Fatalf("0 or 7 got %d, want 7", v)
	}
	v, _, err = evalGolden(t, "7 or 0")
	if err != nil {
		t.Fatalf("or err: %v", err)
	}
	if v != 7 {
		t.Fatalf("7 or 0 got %d, want 7 — the operand the test keeps is the answer", v)
	}
	v, _, err = evalGolden(t, "0 and 7")
	if err != nil {
		t.Fatalf("and err: %v", err)
	}
	if v != 0 {
		t.Fatalf("0 and 7 got %d, want 0", v)
	}
	// floor division mirrors integer division in the interpreter.
	v, _, err = evalGolden(t, "9 // 2")
	if err != nil {
		t.Fatalf("floor div err: %v", err)
	}
	if v != 4 {
		t.Fatalf("9 // 2 got %d, want 4", v)
	}
	// modulo.
	v, _, err = evalGolden(t, "17 % 5")
	if err != nil {
		t.Fatalf("modulo err: %v", err)
	}
	if v != 2 {
		t.Fatalf("17 %% 5 got %d, want 2", v)
	}
	v, _, err = evalGolden(t, "20 % 7")
	if err != nil {
		t.Fatalf("modulo err: %v", err)
	}
	if v != 6 {
		t.Fatalf("20 %% 7 got %d, want 6", v)
	}
}

func TestEvalComprehension(t *testing.T) {
	// list comprehension over range
	v, _, err := evalGolden(t, "xs = [x * 2 for x in range(3)]\nxs")
	if err != nil {
		t.Fatalf("list comp err: %v", err)
	}
	if v == 0 {
		t.Fatalf("list comp returned 0, want a heap handle")
	}
	// comprehension over a list with a filter
	v, _, err = evalGolden(t, "ys = [1, 2, 3]\nzs = [y for y in ys if y > 1]\nzs")
	if err != nil {
		t.Fatalf("filtered comp err: %v", err)
	}
	if v == 0 {
		t.Fatalf("filtered comp returned 0, want a heap handle")
	}
}

func TestEvalMinMaxAbs(t *testing.T) {
	// min/max over a list
	v, _, err := evalGolden(t, "min([3, 1, 2])")
	if err != nil {
		t.Fatalf("min err: %v", err)
	}
	if v != 1 {
		t.Fatalf("min got %d, want 1", v)
	}
	v, _, err = evalGolden(t, "max([3, 1, 2])")
	if err != nil {
		t.Fatalf("max err: %v", err)
	}
	if v != 3 {
		t.Fatalf("max got %d, want 3", v)
	}
	// abs
	v, _, err = evalGolden(t, "abs(-5)")
	if err != nil {
		t.Fatalf("abs err: %v", err)
	}
	if v != 5 {
		t.Fatalf("abs got %d, want 5", v)
	}
}

func TestStrMethods(t *testing.T) {
	goldenReprIs(t, `"heLLo".upper()`, "HELLO")
	// Python quotes strings inside containers, and so does the answer for split.
	goldenReprIs(t, `"a b c".split(" ")`, "['a', 'b', 'c']")
}

func TestListAppend(t *testing.T) {
	goldenReprIs(t, "xs = [1, 2]\nxs.append(3)\nxs", "[1, 2, 3]")
}

func TestDictMethods(t *testing.T) {
	// This pin read "[1, 2]", which is what this backend used to print. CPython prints
	// dict_values([1, 2]) — a dict view is not a list and says so in its own rendering — so the
	// pin moved rather than being deleted (roadmap Gap R.182, ADR 0296).
	goldenReprIs(t, `{"a": 1, "b": 2}.values()`, "dict_values([1, 2])")
}

func TestSumBuiltin(t *testing.T) {
	v, _, err := evalGolden(t, `sum([1, 2, 3])`)
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if v != 6 {
		t.Fatalf("sum got %d, want 6", v)
	}
}

func TestEvalMultiArgPrint(t *testing.T) {
	// print(*args, sep=" ", end="\n") — Python semantics, matched by the AOT
	// backend (ADR 0165).
	out := captureStdout(t, "print(1, 2)")
	if out != "1 2\n" {
		t.Fatalf("print(1, 2) stdout %q, want \"1 2\\n\"", out)
	}
	out = captureStdout(t, "x = 7\nprint(x, x + 1)")
	if out != "7 8\n" {
		t.Fatalf("print(x, x+1) stdout %q, want \"7 8\\n\"", out)
	}
	out = captureStdout(t, "print(\"a =\", 42)")
	if out != "a = 42\n" {
		t.Fatalf("print(\"a =\", 42) stdout %q, want \"a = 42\\n\"", out)
	}
	out = captureStdout(t, "print()")
	if out != "\n" {
		t.Fatalf("print() stdout %q, want a blank line", out)
	}
	out = captureStdout(t, "print(1, 2, 3, sep=\", \")")
	if out != "1, 2, 3\n" {
		t.Fatalf("sep stdout %q, want \"1, 2, 3\\n\"", out)
	}
	out = captureStdout(t, "print(\"a\", end=\":\")\nprint(\"b\")")
	if out != "a:b\n" {
		t.Fatalf("end stdout %q, want \"a:b\\n\"", out)
	}
	out = captureStdout(t, "print(\"hi\")")
	if out != "hi\n" {
		t.Fatalf("print(\"hi\") stdout %q, want hi\\n", out)
	}
	out = captureStdout(t, "print(1, \"hi\", 2)")
	if out != "1 hi 2\n" {
		t.Fatalf("mixed print stdout %q, want \"1 hi 2\\n\"", out)
	}
	// zero-argument print() writes only the terminator.
	out = captureStdout(t, "print()")
	if out != "\n" {
		t.Fatalf("print() stdout %q, want a blank line", out)
	}
	// a container argument prints via the same Repr the AOT renderer produces.
	out = captureStdout(t, "xs = [1, 2]\nprint(\"xs =\", xs)")
	if out != "xs = [1, 2]\n" {
		t.Fatalf("container print stdout %q, want \"xs = [1, 2]\\n\"", out)
	}
}

// captureStdout runs src and returns everything the program wrote to stdout.
//
// It used to capture os.Stdout around an interpreter call. The compiled program writes to the
// descriptor itself and RunSource hands back what came through, so there is nothing to capture and
// nothing to redirect — and the capture was the weaker arrangement anyway, since it could not tell
// the program's bytes from anything else written to the terminal.
//
// A source the record knows goes through the golden funnel, so the case is checked against the
// reference-adjacent answer and not merely against the compiler; a source it does not know (one a
// case invented after the recording) is simply run, and the case's own expectation judges it.
func captureStdout(t *testing.T, src string) string {
	t.Helper()
	if _, ok := loadGolden(t)[src]; ok {
		return goldenStdout(t, src)
	}
	out, err := RunSource(src)
	if err != nil {
		t.Fatalf("compiled run of %q: %v", src, err)
	}
	return out
}

func TestEvalFloatSubMul(t *testing.T) {
	// Float `-` and `*` on float variables must operate on the float64
	// payload, not the raw heap handles (which are small ints). Regression:
	// a*b previously multiplied the boxed handles and produced garbage.
	// The six operations, printed. Reading the answer off the program is the only way there is to
	// check a payload now that no engine hands a test its variable table — and it is the better way:
	// it checks what a user sees rather than what a Go field held.
	goldenStdoutIs(t, "a = 1.5\nb = 2.0\nc = a - b\nd = a * b\ne = b - a\nf = b * a\ng = a + 1\nh = 2 * a\nprint(c, d, e, f, g, h)",
		"-0.5 3.0 0.5 3.0 2.5 3.0")
	// Their sum, for the case where the six answers have to add up rather than line up.
	goldenReprIs(t, "a = 1.5\nb = 2.0\nc = a - b\nd = a * b\ne = b - a\nf = b * a\ng = a + 1\nh = 2 * a\nc + d + e + f + g + h", "11.5")
}

func TestEvalFloatFloorModNeg(t *testing.T) {
	// Float `//` floor division, `%` remainder, and unary `-` must operate on
	// the float64 payload (not raw heap handles). 5.5//2.0 == 2, 5.5%2.0 == 1.5,
	// -5.5 == -5.5, 7.0//2 == 3.
	goldenStdoutIs(t, "a = 5.5\nb = 2.0\nc = a // b\nd = a % b\ne = -a\nf = 7.0 // 2\nprint(c, d, e, f)",
		"2.0 1.5 -5.5 3.0")
}

func TestEvalRound(t *testing.T) {
	// round(float) goes to the nearest value, ties to EVEN — the IEEE rule CPython uses, matching the
	// AOT codegen's fold and `llvm.roundeven.f64`, and not truncating toward zero either. The comment
	// here used to say "must round half-away-from-zero (math.Round), matching the AOT codegen's
	// constant-folded math.Round" — an accurate description of two backends agreeing on the wrong rule.
	// CPython's answers, not our own: a tie goes to the nearest EVEN value. This table used to read
	// {3, 4, 2, -3} — the away-from-zero rule, asserted by the only test that looked, and agreed with
	// itself across both backends so the parity matrix never noticed (roadmap Gap R.50, ADR 0236).
	// Each answer is now its own compiled run, checked against what the engine recorded.
	for i, src := range []string{"round(2.5)", "round(3.9)", "round(2.4)", "round(-2.5)"} {
		want := []string{"2", "4", "2", "-2"}
		goldenStdoutIs(t, "x = "+src+"\nprint(x)", want[i])
	}
	goldenStdoutIs(t, "print(round(2.5))\nprint(round(3.9))\nprint(round(2.4))\nprint(round(-2.5))", "2\n4\n2\n-2")
}

func TestEvalAbsFloat(t *testing.T) {
	// abs(float) must negate a negative float64 payload, not return the boxed
	// heap handle unchanged.
	goldenStdoutIs(t, "a = -3.5\nb = 3.5\nc = abs(a)\nd = abs(b)\ne = abs(-1.5 * 2.0)\nprint(c, d, e)", "3.5 3.5 3.0")
}

func TestEvalMembership(t *testing.T) {
	tests := []struct {
		src  string
		want int64
	}{
		{"1 in [1, 2, 3]", 1},
		{"4 in [1, 2, 3]", 0},
		{"1 not in [1, 2, 3]", 0},
		{"4 not in [1, 2, 3]", 1},
		{"2 in {1, 2, 3}", 1},
		{"9 in {1, 2, 3}", 0},
		{`"b" in "abc"`, 1},
		{`"z" in "abc"`, 0},
		{"2 in {1: 10, 2: 20}", 1},
		{"3 in {1: 10, 2: 20}", 0},
		{"1 is 1", 1},
		{"1 is 2", 0},
		{"1 is not 1", 0},
		{"1 is not 2", 1},
	}
	for _, tc := range tests {
		got, diags, err := evalGolden(t, tc.src)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if len(diags) > 0 {
			t.Fatalf("%s: diags: %v", tc.src, diags)
		}
		if got != tc.want {
			t.Fatalf("%s got %d, want %d", tc.src, got, tc.want)
		}
	}
}

func TestEvalFString(t *testing.T) {
	// a plain f-string is just a string
	if v, _, err := evalGolden(t, `f"hello"`); err != nil {
		t.Fatalf("plain fstring err: %v", err)
	} else if v == 0 {
		t.Fatalf("plain fstring returned 0")
	}
	// literal with {{ }} escapes
	if v, _, err := evalGolden(t, `f"a{{b}}"`); err != nil {
		t.Fatalf("escape fstring err: %v", err)
	} else if v == 0 {
		t.Fatalf("escape fstring returned 0")
	}
	// print interpolates runtime values
	out := captureStdout(t, `x = 42
print(f"x={x}")`)
	if out != "x=42\n" {
		t.Fatalf("print f-string stdout %q, want x=42\\n", out)
	}
	out = captureStdout(t, `print(f"{1 + 2}")`)
	if out != "3\n" {
		t.Fatalf("print expr f-string stdout %q, want 3\\n", out)
	}
	// f-string with multiple parts and a format spec is supported
	// A format spec is HONOURED, not stripped. This pin used to assert `val=7` — the padding the
	// spec asked for simply never happened, on both engines, at exit 0, which is the whole defect
	// Gap R.186 files (ADR 0299). CPython answers `val=  7` for `f"val={n:>3}"`, and a pinned wrong
	// answer is an acceptance test that moves when the road lifts; it does not get deleted.
	out = captureStdout(t, `n = 7
print(f"val={n:>3}")`)
	if out != "val=  7\n" {
		t.Fatalf("print fmt f-string stdout %q, want val=  7\\n", out)
	}
}

func TestFStringConstantFold(t *testing.T) {
	// constant-only f-strings should fold through the optimizer to a StrLit
	if v, _, err := evalGolden(t, `f"a{1}b"`); err != nil {
		t.Fatalf("const fstring err: %v", err)
	} else if v == 0 {
		t.Fatalf("const fstring returned 0")
	}
}

func TestEvalFStringEdgeCases(t *testing.T) {
	// multiple parts and string interpolation
	out := captureStdout(t, `s = "world"
print(f"hello {s}!")`)
	if out != "hello world!\n" {
		t.Fatalf("multi-part stdout %q, want hello world!\\n", out)
	}
	// float interpolation uses the same formatting as print
	out = captureStdout(t, `print(f"{1.5}")`)
	if out != "1.5\n" {
		t.Fatalf("float f-string stdout %q, want 1.5\\n", out)
	}
	// bool interpolation
	out = captureStdout(t, `print(f"{True}")`)
	if out != "True\n" {
		t.Fatalf("bool f-string stdout %q, want True\\n", out)
	}
	// a bare f-string returns a non-zero string handle
	if v, _, err := evalGolden(t, `f"plain"`); err != nil {
		t.Fatalf("plain fstring err: %v", err)
	} else if v == 0 {
		t.Fatalf("plain fstring returned 0")
	}
}

func TestEvalAugmentedAssignment(t *testing.T) {
	cases := []struct {
		src  string
		want int64
	}{
		{"x = 1\nx += 2\nx", 3},
		{"x = 5\nx -= 1\nx", 4},
		{"x = 3\nx *= 4\nx", 12},
		{"x = 8\nx //= 2\nx", 4},
		{"x = 10\nx %= 3\nx", 1},
		{"x = 2\nx += 1\nx *= 3\nx", 9},
	}
	for _, tc := range cases {
		if v, _, err := evalGolden(t, tc.src); err != nil {
			t.Fatalf("%q: err: %v", tc.src, err)
		} else if v != tc.want {
			t.Fatalf("%q: got %d, want %d", tc.src, v, tc.want)
		}
	}
}

func TestEvalAugmentedAttr(t *testing.T) {
	src := "class C:\n    def set(self, v):\n        self.x = v\n    def bump(self):\n        self.x += 5\nc = C()\nc.set(2)\nc.bump()\nc.x"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestTupleUnpack(t *testing.T) {
	tests := []struct {
		src  string
		want int64
	}{
		{"a, b = 1, 2\na", 1},
		{"a, b = 1, 2\nb", 2},
		{"a = 1\nb = 2\na, b = b, a\na", 2},
		{"a = 1\nb = 2\na, b = b, a\nb", 1},
		{"a, b = [1, 2]\na", 1},
		{"a, b = (3, 4)\nb", 4},
		{"s = 0\nfor a, b in [(1, 2), (3, 4)]:\n  s = s + a + b\ns", 10},
		{"a, b = 5, 6\na + b", 11},
	}
	for _, tt := range tests {
		v, _, err := evalGolden(t, tt.src)
		if err != nil {
			t.Fatalf("evalGolden(t, %q): %v", tt.src, err)
		}
		if v != tt.want {
			t.Errorf("evalGolden(t, %q) = %d, want %d", tt.src, v, tt.want)
		}
	}
}

func TestEvalPower(t *testing.T) {
	// integer power: 2 ** 3 == 8
	v, diags, err := evalGolden(t, "2 ** 3")
	if err != nil {
		t.Fatalf("2 ** 3: %v", err)
	}
	if v != 8 {
		t.Fatalf("2 ** 3 = %d, want 8", v)
	}
	_ = diags

	// right-associative: 2 ** 3 ** 2 == 2 ** (3 ** 2) == 2 ** 9 == 512
	v, _, err = evalGolden(t, "2 ** 3 ** 2")
	if err != nil || v != 512 {
		t.Fatalf("2 ** 3 ** 2 = %d err %v, want 512", v, err)
	}

	// the lexer folds `-2` into a single negative literal, so -2 ** 2 == (-2) ** 2 == 4
	// (unary-minus-on-literal is a lexer-level choice in this language)
	v, _, err = evalGolden(t, "-2 ** 2")
	if err != nil || v != 4 {
		t.Fatalf("-2 ** 2 = %d err %v, want 4", v, err)
	}

	// power with variable base/exponent
	v, _, err = evalGolden(t, "a = 2\nb = 10\na ** b")
	if err != nil || v != 1024 {
		t.Fatalf("2 ** 10 = %d err %v, want 1024", v, err)
	}

	// A NEGATIVE integer exponent answers a FLOAT, not 0. `2 ** -1` is `0.5` in the reference; the pin
	// here used to be `want 0` with the comment "yields 0 (int result), like Python's 2 ** -1 -> int
	// floor", which describes an operation Python does not have — both backends agreed with the test and
	// disagreed with CPython, so only the oracle leg could see it (roadmap Gap R.176, ADR 0293).
	progNeg, perr := Parse("2 ** -1")
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	_ = progNeg
	goldenReprIs(t, "2 ** -1", "0.5")

	// float power: 2.0 ** 3.0 == 8.0, and the mixed form 2.0 ** 3 answers the same float.
	goldenReprIs(t, "2.0 ** 3.0", "8.0")
	goldenReprIs(t, "2.0 ** 3", "8.0")
}

func TestEvalOperatorOverloading(t *testing.T) {
	// Left-operand dunder dispatch: Vec.__add__ combines two vectors' x.
	src := "class Vec:\n    def __init__(self, x):\n        self.x = x\n    def __add__(self, other):\n        return self.x + other.x\n    def __mul__(self, n):\n        return self.x * n\na = Vec(2)\nb = Vec(3)\na + b"
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("overload +: %v", err)
	}
	if v != 5 {
		t.Fatalf("got %d, want 5", v)
	}
	// Right-operand reflected dispatch: 10 * Vec(4) uses Vec.__rmul__.
	src = "class Vec:\n    def __init__(self, x):\n        self.x = x\n    def __rmul__(self, n):\n        return self.x * n\nv = Vec(4)\n10 * v"
	v, _, err = evalGolden(t, src)
	if err != nil {
		t.Fatalf("overload rmul: %v", err)
	}
	if v != 40 {
		t.Fatalf("got %d, want 40", v)
	}
	// Comparison dunder: Vec.__lt__ compares by x.
	src = "class Vec:\n    def __init__(self, x):\n        self.x = x\n    def __lt__(self, other):\n        return 1 if self.x < other.x else 0\na = Vec(2)\nb = Vec(9)\na < b"
	v, _, err = evalGolden(t, src)
	if err != nil {
		t.Fatalf("overload lt: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
}

func TestEvalDocstringFunc(t *testing.T) {
	// A leading bare string literal in a def body becomes __doc__.
	out := captureStdout(t, `def greet():
    "returns a greeting"
    return "hi"
print(greet.__doc__)`)
	if out != "returns a greeting\n" {
		t.Fatalf("greet.__doc__ = %q, want %q", out, "returns a greeting\n")
	}
}

func TestEvalDocstringFuncNone(t *testing.T) {
	// A function without a docstring yields the empty string.
	out := captureStdout(t, `def f():
    return 1
print(f.__doc__)`)
	if out != "\n" {
		t.Fatalf("f.__doc__ = %q, want empty string", out)
	}
}

func TestEvalDocstringClass(t *testing.T) {
	// A leading string literal in a class body becomes cls.__doc__.
	out := captureStdout(t, `class Animal:
    "an animal class"
    def speak(self):
        return self
a = Animal()
print(Animal.__doc__)`)
	if out != "an animal class\n" {
		t.Fatalf("Animal.__doc__ = %q, want %q", out, "an animal class\n")
	}
}

func TestEvalDocstringClosure(t *testing.T) {
	// A nested def (closure value) carries __doc__ too.
	out := captureStdout(t, `def outer():
    def inner():
        "inner helper"
        return 1
    return inner
f = outer()
print(f.__doc__)`)
	if out != "inner helper\n" {
		t.Fatalf("closure.__doc__ = %q, want %q", out, "inner helper\n")
	}
}

func TestEvalDocstringNotFirst(t *testing.T) {
	// A string literal that is NOT the first statement is a normal expression,
	// not a docstring.
	out := captureStdout(t, `def g():
    x = 1
    "not a docstring"
    return x
print(g.__doc__)`)
	if out != "\n" {
		t.Fatalf("g.__doc__ = %q, want empty string", out)
	}
}

// Where a trap came from is what a traceback is for, and the language owes one.
//
// The retired engine built the frame stack as it walked: every call pushed a frame, and a raise
// collected them, so these three cases could read `bar` at line 2 out of the error. The compiled
// backend has no such stack — the target raises with the class and the message the runtime was
// given, and the frames are Gap K.8's to emit (its metadata design landed with ADR 0231, and the
// CLI already reads a `traceback` member of the JSON result, so the seam is in place).
//
// What is testable now is the two halves that exist, and they are the ones a reader judges: the
// trap arrives named — the class an `except IndexError:` would match, the message the raise
// carried — through every shape a program can fail in; and the host's renderer, given a frame
// stack, writes Python's shape rather than a paraphrase of it. Both are asserted below, per shape.
// When Gap K.8 fills in, the frames join the same three cases without rewriting them.
func TestTrapReportNamesTheFailureInEveryShape(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantClass, wantMsg string
		frame                         Frame
	}{
		{
			name:  "a call nested two deep",
			src:   "def bar(x):\n  return x // 0\ndef foo(x):\n  return bar(x)\nfoo(10)\n",
			frame: Frame{Name: "bar", Line: 2, Col: 10},
		},
		{
			name:  "a method body",
			src:   "class C:\n  def m(self):\n    return 1 // 0\nc = C()\nc.m()\n",
			frame: Frame{Name: "m", Line: 3, Col: 12},
		},
		{
			name:  "the module's own statement",
			src:   "x = 1 // 0\n",
			frame: Frame{Name: "<module>", Line: 1, Col: 5},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := goldenRunError(t, tc.src)
			if err == nil {
				t.Fatalf("%q answered; a division by zero must fail the program", tc.src)
			}
			ee, ok := err.(*TrapError)
			if !ok {
				t.Fatalf("want a *TrapError, got %T (%v)", err, err)
			}
			if ee.ExnType == "" {
				t.Errorf("the trap reached the host without a class — the reader cannot tell a ZeroDivisionError from a TypeError: %+v", ee)
			}
			if strings.TrimSpace(ee.ExnMsg) == "" {
				t.Errorf("the trap reached the host without the message the raise carried: %+v", ee)
			}
			if tc.wantClass != "" && ee.ExnType != tc.wantClass {
				t.Errorf("class = %q, want %q", ee.ExnType, tc.wantClass)
			}
			// No frames may be invented: a stack the compiler cannot name is worse than none,
			// because it reads as evidence. Gap K.8 will supply real ones.
			if len(ee.Traceback) != 0 {
				t.Errorf("a frame stack arrived from a backend that does not emit one yet (Gap K.8): %v", ee.Traceback)
			}
			// The host's rendering, given a stack, keeps Python's shape: header, one line per
			// frame, and the class and message last — the line that tells a reader what to grep.
			ee.Traceback = []Frame{tc.frame}
			rendered := ee.RenderTraceback()
			for _, want := range []string{
				"Traceback (most recent call last):",
				fmt.Sprintf("line %d, in %s", tc.frame.Line, tc.frame.Name),
				ee.ExnType + ": " + ee.ExnMsg,
			} {
				if !strings.Contains(rendered, want) {
					t.Errorf("rendered traceback\n%s\nis missing %q", rendered, want)
				}
			}
		})
	}
}

func TestTrapReportNamesTheCallThatFailed(t *testing.T) {
	// What a trap owes the reader: its class, its message, and the frames it travelled through. The
	// engine used to build that frame stack itself, and this case read it back out of Go. The
	// compiled backend reports the class and the message on the tool channel and not yet the frames
	// — that half is Gap K.8, whose metadata design landed with ADR 0231, and the CLI already reads
	// a `traceback` member of the JSON result, so the seam is here and waiting for it.
	//
	// What this case holds today is the half the target does deliver, plus the two things the host
	// must never do: invent a class the program did not raise, or invent a frame it cannot name.
	err := goldenRunError(t, "def foo(x):\n  return x // 0\nfoo(1)\n")
	if err == nil {
		t.Fatal("the division by zero should have propagated")
	}
	ee, ok := err.(*TrapError)
	if !ok {
		t.Fatalf("want *TrapError, got %T (%v)", err, err)
	}
	if ee.ExnType == "" || ee.ExnMsg == "" {
		t.Fatalf("the trap arrived without a class or a message: %+v", ee)
	}
	if len(ee.Traceback) != 0 {
		t.Fatalf("a frame stack came from a backend that does not build one yet (Gap K.8): %v", ee.Traceback)
	}
	// The host's rendering, once a stack exists: Python's shape, with the class on the last line —
	// the line that tells a reader what an `except IndexError:` would have matched.
	rendered := (&TrapError{Msg: ee.ExnMsg, ExnType: ee.ExnType, ExnMsg: ee.ExnMsg,
		Traceback: []Frame{{Name: "foo", Line: 2, Col: 1}}}).RenderTraceback()
	for _, want := range []string{"Traceback (most recent call last):", "line 2, in foo", ee.ExnType + ": " + ee.ExnMsg} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered traceback %q is missing %q", rendered, want)
		}
	}
}
