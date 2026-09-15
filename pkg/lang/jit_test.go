package lang

import "os"
import "strings"
import "testing"

func TestEvalUserFunc(t *testing.T) {
	v, _, err := EvalExpr("def double(x):\n    return x * 2\ndouble(5)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
}

func TestEvalUserFuncTwoParams(t *testing.T) {
	v, _, err := EvalExpr("def add(a, b):\n    return a + b\nadd(3, 4)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalIfElse(t *testing.T) {
	v, _, err := EvalExpr("x = 1\nif x < 2:\n    print(10)\nelse:\n    print(20)\nx")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
}

func TestEvalElifChain(t *testing.T) {
	// elif taken
	v, _, err := EvalExpr("x = 3\nif x < 2:\n    y = 10\nelif x < 4:\n    y = 20\nelse:\n    y = 30\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 20 {
		t.Fatalf("got %d, want 20", v)
	}
	// elif false, else taken
	v, _, err = EvalExpr("x = 9\nif x < 2:\n    y = 10\nelif x < 4:\n    y = 20\nelse:\n    y = 30\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 30 {
		t.Fatalf("got %d, want 30", v)
	}
}

func TestEvalElifMultiple(t *testing.T) {
	// second of three elifs taken
	v, _, err := EvalExpr("x = 6\nif x < 2:\n    y = 10\nelif x < 5:\n    y = 20\nelif x < 8:\n    y = 30\nelse:\n    y = 40\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 30 {
		t.Fatalf("got %d, want 30", v)
	}
	// no elif, no else -> nothing taken
	v, _, err = EvalExpr("x = 9\nif x < 2:\n    y = 10\nelif x < 5:\n    y = 20\ny = 99\ny")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 99 {
		t.Fatalf("got %d, want 99", v)
	}
}

func TestEvalWhileSum(t *testing.T) {
	v, _, err := EvalExpr("i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
}

func TestEvalForSum(t *testing.T) {
	v, _, err := EvalExpr("s = 0\nfor i in range(5):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
}

func TestEvalMatch(t *testing.T) {
	v, _, err := EvalExpr("x = 2\nmatch x:\n    case 1:\n        print(1)\n    case 2:\n        print(2)\nx")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalBreakContinue(t *testing.T) {
	// break out of while
	v, _, err := EvalExpr("i = 0\nwhile i < 100:\n    i = i + 1\n    if i == 3:\n        break\ni")
	if err != nil {
		t.Fatalf("break err: %v", err)
	}
	if v != 3 {
		t.Fatalf("break got %d, want 3", v)
	}
	// continue skips increment in for loop
	v2, _, err := EvalExpr("s = 0\nfor i in range(5):\n    if i == 2:\n        continue\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("continue err: %v", err)
	}
	if v2 != 8 { // 0+1+3+4 (skip 2)
		t.Fatalf("continue got %d, want 8", v2)
	}
}

func TestEvalRangeTwoArg(t *testing.T) {
	// sum range(2, 5) = 2+3+4 = 9
	v, _, err := EvalExpr("s = 0\nfor i in range(2, 5):\n    s = s + i\ns")
	if err != nil {
		t.Fatalf("range(a,b) err: %v", err)
	}
	if v != 9 {
		t.Fatalf("range(2,5) sum got %d, want 9", v)
	}
}

func TestEvalMatchWildcard(t *testing.T) {
	// match with _ wildcard catches unmatched value
	v, _, err := EvalExpr("x = 42\nmatch x:\n    case 1:\n        print(1)\n    case _:\n        print(42)\nx")
	if err != nil {
		t.Fatalf("match wildcard err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalForElse(t *testing.T) {
	// for without break: else runs
	v, _, err := EvalExpr("s = 0\nfor i in range(3):\n    s = s + i\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("for-else err: %v", err)
	}
	if v != 103 { // 0+1+2 + 100
		t.Fatalf("got %d, want 103", v)
	}
}

func TestEvalForElseBreak(t *testing.T) {
	// break skips else
	v, _, err := EvalExpr("s = 0\nfor i in range(3):\n    if i == 1:\n        break\n    s = s + i\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("for-else break err: %v", err)
	}
	if v != 0 { // else skipped; only i=0 added before break
		t.Fatalf("got %d, want 0", v)
	}
}

func TestEvalWhileElse(t *testing.T) {
	v, _, err := EvalExpr("i = 0\ns = 0\nwhile i < 3:\n    s = s + i\n    i = i + 1\nelse:\n    s = s + 10\ns")
	if err != nil {
		t.Fatalf("while-else err: %v", err)
	}
	if v != 13 { // 0+1+2 + 10
		t.Fatalf("got %d, want 13", v)
	}
}

func TestEvalClassMethod(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n    def sum(self):\n        return self.x + self.y\np = Point(2, 3)\np.sum()"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("class err: %v", err)
	}
	if v != 5 {
		t.Fatalf("got %d, want 5", v)
	}
}

func TestEvalClassAttrSet(t *testing.T) {
	src := "class C:\n    def __init__(self):\n        self.n = 0\n    def bump(self):\n        self.n = self.n + 1\n        return self.n\nc = C()\nc.bump()\nc.bump()"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("class err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalTryExcept(t *testing.T) {
	src := "x = 0\ntry:\n    x = 1 // 0\nexcept Exception:\n    x = 42\nx"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("try err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalRaiseCaught(t *testing.T) {
	src := "x = 0\ntry:\n    raise Exception\n    x = 1\nexcept Exception:\n    x = 42\nx"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("raise-catch err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalDefaultArg(t *testing.T) {
	v, _, err := EvalExpr("def f(a, b=10):\n    return a + b\nf(5)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 15 {
		t.Fatalf("got %d, want 15", v)
	}
}

func TestEvalKeywordArg(t *testing.T) {
	v, _, err := EvalExpr("def f(a, b):\n    return a * b\nf(a=3, b=4)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 12 {
		t.Fatalf("got %d, want 12", v)
	}
}

func TestEvalKeywordArgOutOfOrder(t *testing.T) {
	v, _, err := EvalExpr("def f(a, b):\n    return a - b\nf(b=3, a=10)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalKeywordAndDefault(t *testing.T) {
	v, _, err := EvalExpr("def f(a, b=5):\n    return a + b\nf(b=100, a=2)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 102 {
		t.Fatalf("got %d, want 102", v)
	}
}

func TestEvalDictSetIndexLen(t *testing.T) {
	// dict constant-key lookup.
	v, _, err := EvalExpr("{1: 10, 2: 20}[1]")
	if err != nil {
		t.Fatalf("dict index err: %v", err)
	}
	if v != 10 {
		t.Fatalf("got %d, want 10", v)
	}
	v, _, err = EvalExpr("{1: 10, 2: 20}[2]")
	if err != nil {
		t.Fatalf("dict index err: %v", err)
	}
	if v != 20 {
		t.Fatalf("got %d, want 20", v)
	}
	// len over dict and set.
	v, _, err = EvalExpr("len({1: 10, 2: 20})")
	if err != nil {
		t.Fatalf("len dict err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
	v, _, err = EvalExpr("len({1, 2, 3})")
	if err != nil {
		t.Fatalf("len set err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
	// set membership lookup returns the element.
	v, _, err = EvalExpr("{1, 2, 3}[2]")
	if err != nil {
		t.Fatalf("set index err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalDictLiteral(t *testing.T) {
	// dict literal then iterate keys and sum via comprehension
	v, _, err := EvalExpr("def f(d):\n    return len(d)\nf({1: 10, 2: 20})")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalListComprehension(t *testing.T) {
	v, _, err := EvalExpr("def g(ys):\n    s = 0\n    for y in ys:\n        s = s + y\n    return s\nv = [1, 2, 3]\ng([x * 2 for x in v])")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 12 {
		t.Fatalf("got %d, want 12", v)
	}
}

func TestEvalForOverList(t *testing.T) {
	// for-over-list sums the elements of a boxed list.
	v, _, err := EvalExpr("s = 0\nfor x in [1, 2, 3]:\n    s = s + x\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 6 {
		t.Fatalf("got %d, want 6", v)
	}
	// continue skips to the next element.
	v, _, err = EvalExpr("s = 0\nfor x in [1, 2, 3, 4]:\n    if x == 2:\n        continue\n    s = s + x\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 8 {
		t.Fatalf("got %d, want 8", v)
	}
	// break stops; else runs only on normal completion.
	v, _, err = EvalExpr("s = 0\nfor x in [1, 2, 3]:\n    if x == 2:\n        break\n    s = s + x\nelse:\n    s = s + 100\ns")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
	v, _, err = EvalExpr("s = 0\nfor x in [1, 2, 3]:\n    s = s + x\nelse:\n    s = s + 100\ns")
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
	v, _, err := EvalExpr("c = [x * 2 for x in [1, 2, 3]]\nlen(c)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}

	// With a condition referencing the comprehension variable too.
	v, _, err = EvalExpr("c = [x for x in [1, 2, 3, 4] if x % 2 == 0]\nlen(c)")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 2 {
		t.Fatalf("got %d, want 2", v)
	}
}

func TestEvalTernary(t *testing.T) {
	// ternary `then if cond else otherwise` picks the branch by truthiness.
	for _, tc := range []struct{ src string; want int64 }{
		{"5 if 1 else 3", 5},
		{"10 if 0 else 42", 42},
		{"7 if 2 > 1 else 99", 7},
		// right-associative: the else branch is a full ternary.
		{"1 if 0 else 2 if 1 else 3", 2},
		// used inside a larger expression.
		{"(5 if 1 else 6) + 1", 6},
	} {
		v, _, err := EvalExpr(tc.src + "\n")
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
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if v != 8 {
		t.Fatalf("got %d, want 8", v)
	}
}

func TestClosureNestedFunctionCall(t *testing.T) {
	src := "def outer(a):\n    def inner(b):\n        return a * b\n    return inner\nf = outer(6)\nf(7)"
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	_, _, err := EvalExpr(src)
	if err == nil {
		t.Fatalf("expected a type mismatch error")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("got %v, want type mismatch", err)
	}
}

func TestAnnotDynAcceptsAnything(t *testing.T) {
	// any (dynamic) annotation accepts a list under an int context.
	src := "x: any = [1, 2]\nlen(x)"
	v, _, err := EvalExpr(src)
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
	_, _, err := EvalExpr(src)
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("got %v, want type mismatch", err)
	}
}

func TestAnnotReturnMismatch(t *testing.T) {
	// an annotated return rejects a wrong-typed returned value.
	src := "def f() -> int:\n    return [1, 2]\nf()"
	_, _, err := EvalExpr(src)
	if err == nil || !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("got %v, want type mismatch", err)
	}
}

func TestEvalClassInheritanceMethodResolution(t *testing.T) {
	// Child inherits a method defined only on Base.
	src := "class Base:\n    def greet(self):\n        return 41\nclass Child(Base):\n    def hi(self):\n        return 1\nc = Child()\nc.greet()"
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("grandchild err: %v", err)
	}
	if v != 7 {
		t.Fatalf("got %d, want 7", v)
	}
}

func TestEvalImportModuleFunction(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/mylib.gy", []byte("def double(x):\n    return x * 2\n"), 0o600)
	old, _ := os.Getwd()
	defer os.Chdir(old)
	os.Chdir(dir)
	src := "import mylib\nmylib.double(4)"
	v, _, err := EvalExpr(src)
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
	v, _, err := EvalExpr(src)
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
	_, _, err := EvalExpr(src)
	if err == nil {
		t.Fatalf("expected import error, got nil")
	}
}

func TestEvalListIndex(t *testing.T) {
	src := "lst = [10, 20, 30]\nlst[1]"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("index err: %v", err)
	}
	if v != 20 {
		t.Fatalf("got %d, want 20", v)
	}
}

func TestEvalListIndexOutOfRange(t *testing.T) {
	src := "lst = [1, 2]\nlst[5]"
	_, _, err := EvalExpr(src)
	if err == nil {
		t.Fatalf("expected out-of-range error")
	}
}

func TestEvalDictIndex(t *testing.T) {
	src := "d = {1: 100, 2: 200}\nd[2]"
	v, _, err := EvalExpr(src)
	if err != nil {
		t.Fatalf("dict index err: %v", err)
	}
	if v != 200 {
		t.Fatalf("got %d, want 200", v)
	}
}

func TestEvalPassStatement(t *testing.T) {
	// pass inside a loop body is a no-op; iteration proceeds normally.
	v, _, err := EvalExpr("x = 0\nfor i in range(3):\n    pass\n    x = x + i\nx")
	if err != nil {
		t.Fatalf("loop pass err: %v", err)
	}
	if v != 3 {
		t.Fatalf("got %d, want 3", v)
	}
	// pass inside an if body: no branch taken, else still runs.
	v, _, err = EvalExpr("x = 0\nif 0:\n    pass\nelse:\n    x = 1\nx")
	if err != nil {
		t.Fatalf("if pass err: %v", err)
	}
	if v != 1 {
		t.Fatalf("got %d, want 1", v)
	}
	// bare pass statement at top level.
	v, _, err = EvalExpr("pass\nx = 42\nx")
	if err != nil {
		t.Fatalf("bare pass err: %v", err)
	}
	if v != 42 {
		t.Fatalf("got %d, want 42", v)
	}
}

func TestEvalStringConcatLen(t *testing.T) {
	// len over a string concatenation counts the joined characters.
	v, _, err := EvalExpr("len(\"ab\" + \"cd\")")
	if err != nil {
		t.Fatalf("concat len err: %v", err)
	}
	if v != 4 {
		t.Fatalf("got %d, want 4", v)
	}
	// empty string.
	v, _, err = EvalExpr("len(\"\")")
	if err != nil {
		t.Fatalf("empty len err: %v", err)
	}
	if v != 0 {
		t.Fatalf("got %d, want 0", v)
	}
}

func TestEvalStringLiteral(t *testing.T) {
	// string literals evaluate to boxed strings (last stmt is an assignment)
	v, _, err := EvalExpr("s = \"hello\"\ns")
	if err != nil {
		t.Fatalf("str literal err: %v", err)
	}
	if v == 0 {
		t.Fatalf("str literal returned 0, want a heap handle")
	}
	// concatenation with +
	v, _, err = EvalExpr("x = \"a\" + \"b\"\nx")
	if err != nil {
		t.Fatalf("concat err: %v", err)
	}
	if v == 0 {
		t.Fatalf("concat returned 0, want a heap handle")
	}
	// len of a string
	v, _, err = EvalExpr("len(\"hello\")")
	if err != nil {
		t.Fatalf("len str err: %v", err)
	}
	if v != 5 {
		t.Fatalf("len(str) got %d, want 5", v)
	}
}

func TestEvalFloatLiteral(t *testing.T) {
	// float literals evaluate to boxed floats
	v, _, err := EvalExpr("x = 1.5\nx")
	if err != nil {
		t.Fatalf("float literal err: %v", err)
	}
	if v == 0 {
		t.Fatalf("float literal returned 0, want a heap handle")
	}
	// float + int arithmetic
	v, _, err = EvalExpr("x = 2.0 + 3\nx")
	if err != nil {
		t.Fatalf("float add err: %v", err)
	}
	if v == 0 {
		t.Fatalf("float add returned 0, want a heap handle")
	}
	// float division
	v, _, err = EvalExpr("x = 7.0 / 2.0\nx")
	if err != nil {
		t.Fatalf("float div err: %v", err)
	}
	if v == 0 {
		t.Fatalf("float div returned 0, want a heap handle")
	}
	// float comparison returns int 1/0
	v, _, err = EvalExpr("1.5 > 1\n1.5 < 1")
	if err != nil {
		t.Fatalf("float cmp err: %v", err)
	}
	if v != 0 {
		t.Fatalf("1.5 < 1 got %d, want 0", v)
	}
}

func TestEvalAndOrFloorDiv(t *testing.T) {
	// and/or return a boolean 0/1 (both operands evaluated).
	v, _, err := EvalExpr("1 and 0")
	if err != nil {
		t.Fatalf("and err: %v", err)
	}
	if v != 0 {
		t.Fatalf("1 and 0 got %d, want 0", v)
	}
	v, _, err = EvalExpr("1 and 2")
	if err != nil {
		t.Fatalf("and err: %v", err)
	}
	if v != 1 {
		t.Fatalf("1 and 2 got %d, want 1", v)
	}
	v, _, err = EvalExpr("0 or 7")
	if err != nil {
		t.Fatalf("or err: %v", err)
	}
	if v != 1 {
		t.Fatalf("0 or 7 got %d, want 1", v)
	}
	// floor division mirrors integer division in the interpreter.
	v, _, err = EvalExpr("9 // 2")
	if err != nil {
		t.Fatalf("floor div err: %v", err)
	}
	if v != 4 {
		t.Fatalf("9 // 2 got %d, want 4", v)
	}
}

func TestEvalComprehension(t *testing.T) {
	// list comprehension over range
	v, _, err := EvalExpr("xs = [x * 2 for x in range(3)]\nxs")
	if err != nil {
		t.Fatalf("list comp err: %v", err)
	}
	if v == 0 {
		t.Fatalf("list comp returned 0, want a heap handle")
	}
	// comprehension over a list with a filter
	v, _, err = EvalExpr("ys = [1, 2, 3]\nzs = [y for y in ys if y > 1]\nzs")
	if err != nil {
		t.Fatalf("filtered comp err: %v", err)
	}
	if v == 0 {
		t.Fatalf("filtered comp returned 0, want a heap handle")
	}
}

func TestEvalMinMaxAbs(t *testing.T) {
	// min/max over a list
	v, _, err := EvalExpr("min([3, 1, 2])")
	if err != nil {
		t.Fatalf("min err: %v", err)
	}
	if v != 1 {
		t.Fatalf("min got %d, want 1", v)
	}
	v, _, err = EvalExpr("max([3, 1, 2])")
	if err != nil {
		t.Fatalf("max err: %v", err)
	}
	if v != 3 {
		t.Fatalf("max got %d, want 3", v)
	}
	// abs
	v, _, err = EvalExpr("abs(-5)")
	if err != nil {
		t.Fatalf("abs err: %v", err)
	}
	if v != 5 {
		t.Fatalf("abs got %d, want 5", v)
	}
}

func TestStrMethods(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse(`"heLLo".upper()`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("upper: %v", err)
	}
	if s := ev.Repr(v); s != "HELLO" {
		t.Fatalf("upper repr %q", s)
	}

	ev = NewEvaluator()
	prog, err = Parse(`"a b c".split(" ")`)
	if err != nil {
		t.Fatalf("parse split: %v", err)
	}
	v, err = ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if s := ev.Repr(v); s != "[a, b, c]" {
		t.Fatalf("split repr %q", s)
	}
}

func TestListAppend(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse("xs = [1, 2]\nxs.append(3)\nxs")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if s := ev.Repr(v); s != "[1, 2, 3]" {
		t.Fatalf("append repr %q", s)
	}
}

func TestDictMethods(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse(`{"a": 1, "b": 2}.values()`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("values: %v", err)
	}
	if s := ev.Repr(v); s != "[1, 2]" {
		t.Fatalf("values repr %q", s)
	}
}

func TestSumBuiltin(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse(`sum([1, 2, 3])`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if v != 6 {
		t.Fatalf("sum got %d", v)
	}
}
