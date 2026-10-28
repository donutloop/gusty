package lang

import (
	"strings"
	"testing"
)

// The class OBJECT, and the walk an attribute read takes through it — roadmap row **L12.4**, owning
// **Gap R.56** ("class-level attributes do not exist") and **Gap R.66** ("attribute lookup stops at the first
// base"). The family this file answers was measured on `9605949` first: `print(C().X)` for `class C: X = 1`
// printed `0` at the exit code of success, `print(Sub.NAME)` for an inherited member was refused in words, and
// a method handing back `self.NAME` printed the intern table's index. Every row below is judged on the compiled
// leg AND the record leg (**ADR 0302**: a missing record fails the case, so an answer is not certified by the
// backend that earned it).

// classMemberFromInstance is the promise the row's first half makes: what a class body binds is an attribute of
// the CLASS, and an instance reads it.
var classMemberFromInstance = []struct{ name, src, want string }{
	{
		"the row's own program, read through an instance",
		"class C:\n    X = 1\n\nprint(C().X)\n", "1\n",
	},
	{
		"a text member read through an instance",
		"class C:\n    NAME = \"c\"\n\nc = C()\nprint(c.NAME)\n", "c\n",
	},
	{
		"the enum idiom, read through an instance",
		"class Color:\n    RED = 1\n\nprint(Color().RED)\n", "1\n",
	},
	{
		"a method reads its class's member",
		"class C:\n    X = 1\n\n    def get(self):\n        return self.X\n\nprint(C().get())\n", "1\n",
	},
	{
		"a member read twice is read the same way twice",
		"class C:\n    X = 4\n\nc = C()\nprint(c.X)\nprint(c.X)\n", "4\n4\n",
	},
	{
		"a member goes into a list",
		"class C:\n    X = 3\n\nprint([C.X, C.X])\n", "[3, 3]\n",
	},
	{
		"a member goes into a dict",
		"class C:\n    K = 5\n\nprint({C.K: C.K})\n", "{5: 5}\n",
	},
	{
		"a member is an argument",
		"class C:\n    X = 4\n\ndef f(a):\n    return a * 2\n\nprint(f(C.X))\n", "8\n",
	},
	{
		"a member is compared",
		"class C:\n    X = 4\n\nprint(C.X == 4, C.X < 5)\n", "True True\n",
	},
	{
		"a text member is concatenated",
		"class C:\n    NAME = \"n\"\n\nprint(C.NAME + \"!\")\n", "n!\n",
	},
	{
		"a text member is cased",
		"class C:\n    NAME = \"n\"\n\nc = C()\nprint(c.NAME.upper())\n", "N\n",
	},
	{
		"a text member is interpolated",
		"class C:\n    NAME = \"c\"\n\nprint(f\"hi {C.NAME}\")\n", "hi c\n",
	},
	{
		"a member stands in a condition",
		"class C:\n    FLAG = 1\n\nif C.FLAG:\n    print(\"yes\")\n", "yes\n",
	},
	{
		"two members of one class, looped",
		"class Color:\n    R = 1\n    G = 2\n\nfor v in [Color.R, Color.G]:\n    print(v)\n", "1\n2\n",
	},
}

// classMemberThroughTheChain is the row's second half: EVERY base is walked, in the order the reference uses.
var classMemberThroughTheChain = []struct{ name, src, want string }{
	{
		"a member inherited from one base",
		"class A:\n    NAME = \"base\"\n\nclass B(A):\n    pass\n\nprint(B().NAME, B.NAME)\n", "base base\n",
	},
	{
		"a member inherited through two bases deep",
		"class A:\n    NAME = \"base\"\n\nclass B(A):\n    pass\n\nclass C(B):\n    pass\n\nprint(C().NAME, C.NAME)\n", "base base\n",
	},
	{
		"a member found in the SECOND base",
		"class A:\n    pass\n\nclass B:\n    X = 7\n\nclass C(A, B):\n    pass\n\nprint(C().X, C.X)\n", "7 7\n",
	},
	{
		"three bases, one member each",
		"class A:\n    X = 1\n\nclass B:\n    Y = 2\n\nclass C:\n    Z = 3\n\nclass D(A, B, C):\n    pass\n\nd = D()\nprint(d.X, d.Y, d.Z)\n", "1 2 3\n",
	},
	{
		"the nearer binding wins",
		"class A:\n    X = 1\n\nclass B(A):\n    X = 2\n\nprint(B().X, B.X)\n", "2 2\n",
	},
	{
		"the first of two bases wins when both bind it",
		"class A:\n    X = 1\n\nclass B:\n    X = 2\n\nclass C(A, B):\n    pass\n\nprint(C().X)\n", "1\n",
	},
	{
		"a diamond resolves in the linearized order",
		"class A:\n    X = 1\n\nclass B(A):\n    X = 2\n\nclass C(A):\n    X = 3\n\nclass D(B, C):\n    pass\n\nprint(D().X, D.X)\n", "2 2\n",
	},
	{
		"a method inherited from a base reads the base's member",
		"class A:\n    NAME = \"a\"\n\nclass B(A):\n    def show(self):\n        return self.NAME\n\nprint(B().show())\n", "a\n",
	},
	{
		"a method a class declares reads its own member",
		"class A:\n    NAME = \"a\"\n\n    def show(self):\n        return self.NAME\n\nprint(A().show())\n", "a\n",
	},
}

// classMemberShadowedByTheInstance is the direction the reference goes the OTHER way: the object's own word
// wins over the class's, and the two co-occurring answers are a run-time fact about which object is read.
var classMemberShadowedByTheInstance = []struct{ name, src, want string }{
	{
		"the instance's own word wins",
		"class C:\n    X = 1\n\nc = C()\nc.X = 5\nprint(c.X)\n", "5\n",
	},
	{
		"two objects of one class, one shadowed",
		"class A:\n    X = 1\n\nclass B(A):\n    pass\n\nb = B()\nb.X = 9\nprint(b.X, B().X, A().X)\n", "9 1 1\n",
	},
	{
		"the class answer stands until the object is written",
		"class C:\n    X = 1\n\nc = C()\nprint(c.X)\nc.X = 7\nprint(c.X)\n", "1\n7\n",
	},
	{
		"__init__ writes what the class also binds",
		"class C:\n    X = 1\n\n    def __init__(self):\n        self.X = 8\n\nprint(C().X, C.X)\n", "8 1\n",
	},
	{
		"a shadowed text still prints as a text",
		"class C:\n    NAME = \"c\"\n\nc = C()\nc.NAME = \"z\"\nprint(c.NAME, C().NAME)\n", "z c\n",
	},
}

// TestAClassMemberReadThroughAnInstance is the row's answer on both witness legs.
func TestAClassMemberReadThroughAnInstance(t *testing.T) {
	t.Parallel()
	for _, tc := range append(append([]struct{ name, src, want string }{}, classMemberFromInstance...), classMemberThroughTheChain...) {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the class object refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheInstanceWordWinsOverTheClassMember judges the shadowing family the same way.
func TestTheInstanceWordWinsOverTheClassMember(t *testing.T) {
	t.Parallel()
	for _, tc := range classMemberShadowedByTheInstance {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the shadowed read refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheClassReadIsCompileTimeAndTheShadowedReadAsksPresence is the cost half of the contract, and the reason
// the door is a static question first: a read the class object can answer touches no heap at all — neither
// `@rt_inst_get` nor the presence row — while a read the program may have shadowed asks `@rt_inst_has`, the
// row `@rt_inst_put` already writes (**ADR 0235**) and a plain attribute read had never asked.
func TestTheClassReadIsCompileTimeAndTheShadowedReadAsksPresence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		needles []string
		absent  []string
	}{
		{
			"a member only the class binds is answered without the heap",
			"class C:\n    X = 1\n\nprint(C().X)\n",
			nil,
			[]string{"call i32 @rt_inst_has(", "call i32 @rt_inst_get("},
		},
		{
			"a member inherited through a base is answered without the heap too",
			"class A:\n    X = 1\n\nclass B(A):\n    pass\n\nprint(B().X)\n",
			nil,
			[]string{"call i32 @rt_inst_has(", "call i32 @rt_inst_get("},
		},
		{
			"a read the program may have shadowed asks the presence row",
			"class A:\n    X = 1\n\nclass B(A):\n    pass\n\nb = B()\nb.X = 9\nprint(b.X, B().X)\n",
			[]string{"call i32 @rt_inst_has(", "call i32 @rt_inst_get("},
			nil,
		},
		{
			"an instance-only attribute keeps the plain slot read",
			"class C:\n    def __init__(self):\n        self.x = 7\n\nprint(C().x)\n",
			[]string{"call i32 @rt_inst_get("},
			[]string{"call i32 @rt_inst_has("},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mod := mustCompileIR(t, tc.src)
			for _, n := range tc.needles {
				if !strings.Contains(mod, n) {
					t.Fatalf("the module does not carry %s\n%s", n, tc.src)
				}
			}
			for _, n := range tc.absent {
				if strings.Contains(mod, n) {
					t.Fatalf("the module emits %s, which this read must not: the class object answers at compile time\n%s", n, tc.src)
				}
			}
		})
	}
}

// TestWhatTheClassObjectStillOwesIsFiledNotFixed keeps the residue honest. Each row is today's bytes beside the
// reference's, and the row that owns the rest; none of them is answered wrong in place of a refusal.
func TestWhatTheClassObjectStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this toolchain does today: the refusal's words, or the answer it prints
		want     string // what CPython prints
		refusal  bool
		row      string
	}{
		{
			// A fractional, verdict, void or container member is the tagged-value-word debt: the read lowers
			// the bound expression where it stands, and 1.5 would come back as the payload word 1.0.
			"a fractional member is still refused, from an instance as from the class name (Gap R.431's, not this row's)",
			"class C:\n    ratio = 1.5\n\nprint(C().ratio)\n",
			"binds ratio to a fractional member", "1.5\n", true,
			"roadmap Gap R.431 (the read that needs the tagged value word)",
		},
		{
			"a container member is refused where the reference loops it",
			"class C:\n    items = [1, 2]\n\nfor x in C.items:\n    print(x)\n",
			"binds items to a member bound to a container or a callable", "1\n2\n", true,
			"roadmap Gap R.431 (a container member minted afresh per read)",
		},
		{
			// The reference raises `AttributeError`; this backend answers the digit 0 and says so (**Gap R.19**
			// has owned that since before the class object existed — the presence row is now asked where a
			// binding EXISTS, and a name nothing binds at all still has no raise).
			"a name neither the object nor the class binds answers the digit 0 (Gap R.19's, not this row's)",
			"class C:\n    pass\n\nc = C()\nprint(c.nope)\n",
			"0\n", "AttributeError: 'C' object has no attribute 'nope'\n", false,
			"roadmap Gap R.19 (a missing attribute answers 0 instead of raising)",
		},
		{
			// A class body is EXECUTED by the reference, so a later binding may read an earlier one. This
			// backend reads the body twice — once for the class's table, once as ordinary code — and the name
			// is not in scope for the second read.
			"a class body may not read its own earlier bindings (Gap R.505's, filed by this row)",
			"class A:\n    X = 1\n    Y = X + 1\n\nprint(A.Y)\n",
			"undefined name \"X\"", "2\n", true,
			"roadmap Gap R.505 (the class body's own scope)",
		},
		{
			"a class attribute may not be rebound outside the body",
			"class Base:\n    NAME = \"base\"\n\nclass Sub(Base):\n    pass\n\nSub.NAME = \"sub\"\nprint(Sub.NAME)\n",
			"cannot store Sub.NAME", "sub\n", true,
			"roadmap Gap R.506 (the class attribute store)",
		},
		{
			"a class attribute may not be incremented",
			"class C:\n    N = 1\n\nC.N = C.N + 1\nprint(C.N)\n",
			"cannot store C.N", "2\n", true,
			"roadmap Gap R.506 (the same store, seen through an augmented read)",
		},
		{
			// P2.13 says `__doc__` is DONE and it folds to a constant — which it does. What it folds for a
			// class with NO docstring is the empty string, where the reference's class object holds None.
			"a class with no docstring prints the empty string, not None (Gap R.507's, filed by this row)",
			"class C:\n    pass\n\nprint(C.__doc__)\n",
			"\n", "None\n", false,
			"roadmap Gap R.507 (`__doc__` folds to \"\" where the reference holds None)",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.refusal {
				msg := compileErr(t, tc.src)
				if !strings.Contains(msg, tc.compiled) {
					t.Fatalf("refused with %q, which does not carry %q\nthe row: %s\n%s", msg, tc.compiled, tc.row, tc.src)
				}
				return
			}
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the program was refused, and the row says this toolchain prints %q\nthe row: %s\n%s", tc.compiled, tc.row, tc.src)
			}
			if got != tc.compiled {
				t.Fatalf("the filed row says this toolchain prints %q, it printed %q\nthe row: %s", tc.compiled, got, tc.row)
			}
		})
	}
}
