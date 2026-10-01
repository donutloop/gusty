package lang

import (
	"regexp"
	"strings"
	"testing"
)

// Gap B (ADR 0235): a class pattern is a question about a class, and it was asked twice — once by
// the evaluator, once by the generator — and got it wrong in three unrelated ways.
//
//	case Point(a, b):      # the instance has x and y, not a and b
//
// The compiled arm matched. @heap's data words cannot tell an attribute that was never written from
// a stored 0, so the arm bound `pt 0 0` while the interpreter and docs/language.md both say the
// pattern fails and the next case is tried.
//
//	case Alias(x, y):      # Alias = Point, inside a function body
//
// The generator's "runtime alias" branch read the name with g.value and DISCARDED the error, emitting
// `%t6 = icmp eq i32 %t5, ` — nothing after the comma — which llc rejects: exit 2, a compiler bug
// under the exit-code contract. The evaluator lost the module's `Alias` inside the function's frame
// and fell through to *calling* the class: `TypeError: 'type' object is not callable`.
//
//	case f():              # a call compared to the subject
//
// Same branch, same discarded error: `%_f.ld1 = load i32, i32* %_f` for a variable that does not
// exist, because `f` is a function. The interpreter prints `call`.
//
// The two guards at the bottom of this file are the ones that matter most: they are regexes over a
// set of programs, because three strings written by the person who fixed the three known shapes
// would certify only the three shapes that person knew about.

const classPointSrc = `class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y
`

func TestClassPatternTableResolvesAliasesAndChains(t *testing.T) {
	src := classPointSrc + "class Special:\n    pass\nAlias = Point\nSecond = Alias\nnotAClass = 5\n"
	info := classPatternsOf(mustParse(t, src))
	for name, want := range map[string]string{
		"Point":   "Point",   // a class names itself
		"Special": "Special", // including one nobody instantiates
		"Alias":   "Point",
		"Second":  "Point", // a chain resolves to the class at its end
	} {
		if got := info.classOfName(name); got != want {
			t.Errorf("classOfName(%q) = %q, want %q", name, got, want)
		}
	}
	if got := info.classOfName("notAClass"); got != "" {
		t.Errorf("a plain variable must not name a class, got %q", got)
	}
	if got := info.classOfName("nowhere"); got != "" {
		t.Errorf("an unknown name must not name a class, got %q", got)
	}
}

func TestClassPatternTableTerminatesOnACycle(t *testing.T) {
	// `a = b; b = a` has no class at the end of it. The walk is bounded, not recursive, so the
	// answer is "no class" rather than a hang — this test is the difference.
	info := classPatternsOf(mustParse(t, "a = b\nb = a\nc = c\n"))
	for _, n := range []string{"a", "b", "c"} {
		if info.classOfName(n) != "" {
			t.Fatalf("a cycle must resolve to no class, %q resolved to %q", n, info.classOfName(n))
		}
	}
}

func TestClassPatternAttributeUniverseCoversEveryName(t *testing.T) {
	// The presence bitmap is cleared over this list at instantiation; a name missing from the list
	// is a slot whose stale bit survives a heap-slot reuse. Capture names have no dot in front of
	// them, so they can only be collected by knowing they are attribute names.
	src := classPointSrc + `
c = Point(1, 2)
c.extra = 7
def m(self):
    self.inner = 1
match c:
    case Point(captured, other):
        print(captured)
`
	have := map[string]bool{}
	for _, n := range classPatternsOf(mustParse(t, src)).attrs {
		have[n] = true
	}
	for _, want := range []string{"x", "y", "extra", "inner", "captured", "other"} {
		if !have[want] {
			t.Errorf("attribute %q is missing from the universe %v", want, have)
		}
	}
}

func TestClassPatternAsksWhetherTheInstanceHasTheAttribute(t *testing.T) {
	src := classPointSrc + "c = Point(1, 2)\nmatch c:\n    case Point(x, y):\n        print(x)\n    case _:\n        print(0)\n"
	mod := compileSrcIR(t, src)
	if n := strings.Count(mod, "call i32 @rt_inst_has("); n < 2 {
		t.Errorf("each capture must be asked of the instance, got %d questions:\n%s", n, mod)
	}
	if !strings.Contains(mod, "call void @rt_inst_clear(") {
		t.Errorf("instantiating a class must clear the presence row of its heap slot:\n%s", mod)
	}
	// The clear is what makes "this instance has no such attribute" true for a REUSED slot; if it
	// ran after the class id was written it would erase the instance's own identity instead. The
	// search is anchored on the emitted call (the runtime's own definitions of these helpers sit at
	// the top of the module, and a bare Index would find those instead).
	alloc := strings.Index(mod, "= call i32 @rt_alloc(i32 4)") // HeapKindInstance
	if alloc < 0 {
		t.Fatalf("no instance instantiation in the module:\n%s", mod)
	}
	region := mod[alloc:]
	clear := strings.Index(region, "call void @rt_inst_clear(")
	put := strings.Index(region, "call void @rt_inst_put(")
	if clear < 0 || put < 0 || clear > put {
		t.Errorf("instantiation must clear presence and then write the class id (clear=%d put=%d):\n%s", clear, put, region[:min(len(region), 600)])
	}
	assertModuleVerifies(t, mod)
}

func TestAliasClassPatternInAFunctionIsNotAnEmptyOperand(t *testing.T) {
	// The program that exited 2. What it must produce instead is a chain check against the class
	// the alias denotes — the same lowering `case Point(x, y):` uses.
	src := classPointSrc + "Alias = Point\ndef go(p):\n    match p:\n        case Alias(x, y):\n            print(x)\n        case _:\n            print(0)\ngo(Point(2, 3))\n"
	mod := compileSrcIR(t, src)
	if strings.Contains(mod, "icmp eq i32 %t") && regexp.MustCompile(`icmp [^\n]*,\s*\n`).MatchString(mod) {
		t.Errorf("an instruction with a missing operand reached the module:\n%s", mod)
	}
	if !strings.Contains(mod, "call i32 @rt_inst_has(") {
		t.Errorf("the aliased form must ask the same questions as the named one:\n%s", mod)
	}
	assertModuleVerifies(t, mod)
}

func TestCasePatternForACallIsNotReadAsAVariable(t *testing.T) {
	// `case f():` is an expression-equality pattern. The old branch read `f` as a variable because
	// it had already decided any non-class name must be one.
	src := "def f():\n    return 3\nmatch 3:\n    case f():\n        print(1)\n    case _:\n        print(0)\n"
	mod := compileSrcIR(t, src)
	if strings.Contains(mod, "i32* %_f") {
		t.Errorf("a function name was loaded as a variable:\n%s", mod)
	}
	assertModuleVerifies(t, mod)
}

func TestMatchPatternOperandsAreNeverFoldedToAnInteger(t *testing.T) {
	// A pattern that always matches used to return the string "1", which is not an i1. It is legal
	// only because the combinators fold it; a `br i1 1` or `or i1 %t, 0` would be a rejected module.
	for _, src := range []string{
		"match 3:\n    case x:\n        print(1)\n",
		"match 3:\n    case 1 | 3:\n        print(1)\n    case _:\n        print(0)\n",
		"match 3:\n    case x:\n        print(1)\n    case 1 | 2:\n        print(2)\n",
		classPointSrc + "match Point(1, 2):\n    case Point(_, y):\n        print(y)\n    case _:\n        print(0)\n",
	} {
		mod := compileSrcIR(t, src)
		if bad := regexp.MustCompile(`(?:br i1|and i1|or i1) [^\n]*[^%\w i1tfals][01]\b`).MatchString(mod); bad {
			t.Errorf("an integer reached an i1 operand position:\n%s", mod)
		}
	}
}

func TestMissingAttributeFailsTheClassPattern(t *testing.T) {
	// The interpreter's half of the parity the compiled half now keeps: the arm is not taken, and
	// the capture variables are never bound.
	src := classPointSrc + "p = Point(2, 3)\nmatch p:\n    case Point(a, b):\n        99\n    case _:\n        7\n"
	if v := evalStr(t, src); v != 7 {
		t.Fatalf("a class pattern matched attributes the instance does not have: got %d, want 7", v)
	}
}

func TestAliasClassPatternMatchesInsideAFunction(t *testing.T) {
	src := classPointSrc + "Alias = Point\ndef go(p):\n    match p:\n        case Alias(x, y):\n            return x + y\n    return -1\n"
	if v := evalStr(t, src+"go(Point(2, 3))\n"); v != 5 {
		// The important part is that the arm ran at all: reaching -1 means the pattern was lost.
		t.Fatalf("the alias pattern did not match inside a function body: got %d, want 5", v)
	}
}

func TestClassInstantiationDoesNotInheritPresence(t *testing.T) {
	// A heap slot is reused; an instance must not inherit which attributes existed last time. The
	// `ghost` attribute is never written anywhere in the program, so no slot may claim it — and 40
	// instantiations are what hand a later instance somebody else's slot.
	src := classPointSrc + `
def churn(n):
    total = 0
    for i in range(n):
        p = Point(i, i)
        match p:
            case Point(x, y):
                total = total + x
            case _:
                total = total - 1
    return total

v = churn(40)
q = Point(1, 2)
match q:
    case Point(ghost):
        v = v + 999
    case _:
        v = v + 0
v
`
	if v := evalStr(t, src); v != 780 {
		t.Fatalf("churn + ghost probe = %d, want 780 (a reused slot claimed an attribute nobody wrote)", v)
	}
}

// --- helpers ---------------------------------------------------------------------------

// mustParse lives in incremental_test.go; assertModuleVerifies is this file's, because the
// verifier's verdict is the point of these tests rather than an afterthought.

func assertModuleVerifies(t *testing.T, mod string) {
	t.Helper()
	ver, err := VerifyModuleIR(mod, 0)
	if err != nil {
		t.Fatalf("the module does not verify: %v\n%s", err, mod)
	}
	if ver == nil {
		t.Fatalf("no verdict for the module\n%s", mod)
	}
	if ver.Skipped {
		t.Skip("no LLVM verification tool available")
	}
	if !ver.OK {
		t.Fatalf("the module verifier reported problems: %v\n%s", ver.Errors, mod)
	}
}
