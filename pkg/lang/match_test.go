package lang

import "testing"

// TestMatchClassPattern verifies `case Point(x, y):` binds an instance's
// attributes when the subject is an instance of Point.
func TestMatchClassPattern(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\np = Point(2, 3)\nmatch p:\n    case Point(x, y):\n        x + y\n    case _:\n        0\n"
	v := evalStr(t, src)
	if v != 5 {
		t.Fatalf("class pattern bound x+y=%d, want 5", v)
	}
}

// TestMatchClassPatternSubclass verifies a subclass instance matches a
// superclass class pattern.
func TestMatchClassPatternSubclass(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\nclass CPoint(Point):\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\np = CPoint(3, 4)\nmatch p:\n    case Point(x, y):\n        x + y\n    case _:\n        0\n"
	v := evalStr(t, src)
	if v != 7 {
		t.Fatalf("subclass pattern x+y=%d, want 7", v)
	}
}

// TestMatchClassPatternNoMatch verifies a non-matching instance falls through
// to the wildcard case.
func TestMatchClassPatternNoMatch(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\nclass Circle:\n    def __init__(self, r):\n        self.r = r\np = Circle(5)\nmatch p:\n    case Point(x, y):\n        x + y\n    case _:\n        99\n"
	v := evalStr(t, src)
	if v != 99 {
		t.Fatalf("non-instance fell to wildcard=%d, want 99", v)
	}
}

// TestMatchClassPatternMissingAttr verifies a missing attribute fails the
// pattern (falls through to the next case).
func TestMatchClassPatternMissingAttr(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\np = Point(1, 2)\nmatch p:\n    case Point(x, z):\n        x + z\n    case _:\n        77\n"
	v := evalStr(t, src)
	if v != 77 {
		t.Fatalf("missing-attr pattern fell to wildcard=%d, want 77", v)
	}
}

// TestMatchClassPatternAlias verifies a class stored in a variable can be used
// as a class pattern.
func TestMatchClassPatternAlias(t *testing.T) {
	src := "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\nAlias = Point\np = Point(5, 6)\nmatch p:\n    case Alias(x, y):\n        x * y\n    case _:\n        0\n"
	v := evalStr(t, src)
	if v != 30 {
		t.Fatalf("alias class pattern x*y=%d, want 30", v)
	}
}

func TestMatchListDestructure(t *testing.T) {
	// match [10, 20]: case [a, b]: a + b  -> 30
	src := "x = [10, 20]\nmatch x:\n    case [a, b]:\n        a + b\n"
	v := evalStr(t, src)
	if v != 30 {
		t.Fatalf("destructure sum = %d, want 30", v)
	}
}

func TestMatchListDestructureMismatch(t *testing.T) {
	// subject not a list of matching arity -> falls through to wildcard
	src := "x = [1, 2, 3]\nmatch x:\n    case [a, b]:\n        a + b\n    case _:\n        99\n"
	v := evalStr(t, src)
	if v != 99 {
		t.Fatalf("mismatch fell to wildcard = %d, want 99", v)
	}
}

func TestMatchOrPatterns(t *testing.T) {
	src := "x = 3\nmatch x:\n    case 1 | 2:\n        111\n    case 3 | 4:\n        333\n    case _:\n        999"
	if got := evalStr(t, src); got != 333 {
		t.Fatalf("or-patterns got %d, want 333", got)
	}
}

func TestMatchGuardFallsThrough(t *testing.T) {
	src := "x = 5\nmatch x:\n    case 5 if x > 10:\n        100\n    case 5:\n        555\n    case _:\n        999"
	if got := evalStr(t, src); got != 555 {
		t.Fatalf("guard got %d, want 555", got)
	}
}

func TestMatchGuardPasses(t *testing.T) {
	src := "x = 20\nmatch x:\n    case 20 if x > 10:\n        200\n    case _:\n        999"
	if got := evalStr(t, src); got != 200 {
		t.Fatalf("guard got %d, want 200", got)
	}
}

func TestMatchDictPattern(t *testing.T) {
	src := "d = {\"a\": 10}\nmatch d:\n    case {\"a\": v}:\n        v\n    case _:\n        0"
	if got := evalStr(t, src); got != 10 {
		t.Fatalf("dict pattern got %d, want 10", got)
	}
}

func TestMatchDictPatternMissingKey(t *testing.T) {
	src := "d = {\"b\": 5}\nmatch d:\n    case {\"a\": v}:\n        v\n    case _:\n        0"
	if got := evalStr(t, src); got != 0 {
		t.Fatalf("dict pattern missing key got %d, want 0", got)
	}
}

// TestMatchBareNameCapture verifies a bare-name capture pattern (`case x:`)
// binds the subject to a fresh variable and always matches (Python semantics).
func TestMatchBareNameCapture(t *testing.T) {
	src := "def f(v):\n    match v:\n        case 1:\n            return 10\n        case x:\n            return x\nf(1) + f(5) + f(99)"
	got := evalStr(t, src)
	if got != 10+5+99 {
		t.Fatalf("bare-name capture sum = %d, want 114", got)
	}
}

// TestMatchBareNameCaptureString verifies the captured name works in a string
// context inside the matched branch.
func TestMatchBareNameCaptureString(t *testing.T) {
	src := "def classify(v):\n    match v:\n        case 1:\n            return \"one\"\n        case x:\n            return \"other:\" + str(x)\nclassify(1) == \"one\" and classify(5) == \"other:5\""
	if got := evalStr(t, src); got != 1 {
		t.Fatalf("bare-name capture string parity = %d, want 1", got)
	}
}
