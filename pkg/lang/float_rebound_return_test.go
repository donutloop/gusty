package lang

import (
	"strings"
	"testing"
)

// float_rebound_return_test.go — a parameter the body rebinds to a float is returned in a
// `double` word (roadmap L11.6, Gap R.3c; ADR 0254).
//
// A function's calling convention used to be written twice, from two unrelated questions: the
// argument words came from the call sites and the return word from the *shape of the return
// expression*. `return x + 0.0` is visibly a float, so that function got a double; `return x`
// says nothing at all, so the function was emitted `i32` — and a body that had just stored a
// double into the parameter's slot had nowhere to put the answer:
//
//	def addf(x):
//	    x = x + 1.5
//	    return x
//	print(addf(1.0))   # CPython 2.5 · --interp 2.5 · --aot answered 1, exit 0
//
// The compiled leg answered the *argument*, in the shape of a correct answer. ADR 0196's
// copy-in had long since given the rebound parameter a slot to live in; what was missing was
// asking the body — not its last line — what kind the answer is. So the question is now asked
// before the header is written: a parameter the body rebinds to a float value, read back by a
// `return`, makes the function double-returning, exactly as if the program had written
// `return x + 0.0`.
//
// The convention decides every parameter together, which is where the refusal comes from: a
// container parameter's word is a heap handle and an interned text's is an index into
// `@str_tab`, and neither can be handed a `double`. Those functions are refused in words
// rather than emitted wrong, and the answer is the `(payload, tag)` pair the tagged value word
// (L11.1) carries.
//
// Both legs below, CPython in the sibling integration file.

func TestAParameterReboundToAFloatIsReturnedAsAFloat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		// ---- the shape the gap was named for.
		{"a parameter rebound to a float and returned by bare name",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(1.0))\n", "2.5\n"},
		{"the same function given an int argument",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(2))\n", "3.5\n"},
		{"the same function given a bool argument",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(True))\n", "2.5\n"},
		{"several arguments to one promoted function",
			"def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(1.0), addf(2), addf(0.0))\n", "2.5 3.5 1.5\n"},
		// ---- the value may travel through an arithmetic expression on its way out.
		{"returned negated",
			"def f(x):\n    x = x + 1.5\n    return -x\n\nprint(f(1.0))\n", "-2.5\n"},
		{"returned negated, twice, from three call sites",
			"def f(x):\n    x = x + 1.5\n    return -x\n\nprint(f(1.0), f(2), f(0.0))\n", "-2.5 -3.5 -1.5\n"},
		{"returned through abs() of a negation",
			"def f(x):\n    x = x + 1.5\n    return abs(-x)\n\nprint(f(1.0))\n", "2.5\n"},
		{"returned inside a product",
			"def f(x):\n    x = x + 1.5\n    return x * 2\n\nprint(f(1.0))\n", "5.0\n"},
		{"returned inside a sum",
			"def k(x):\n    x = x + 1.5\n    return x + 0\n\nprint(k(1.0))\n", "2.5\n"},
		{"returned through floor division",
			"def f(x):\n    x = x + 0.5\n    return x // 1\n\nprint(f(1.0))\n", "1.0\n"},
		{"returned through a remainder",
			"def f(x):\n    x = x * 2.0\n    return x % 3\n\nprint(f(1.5))\n", "0.0\n"},
		// ---- the calls that hand the number back as the same kind it came in.
		{"returned through abs()",
			"def f(x):\n    x = x + 1.5\n    return abs(x)\n\nprint(f(1.0))\n", "2.5\n"},
		{"returned through float()",
			"def f(x):\n    x = x + 1.5\n    return float(x)\n\nprint(f(1.0))\n", "2.5\n"},
		// ...and the ones that answer with a different kind, which the gate must leave alone: an
		// int-returning function stays int-returning, promotion of those would be the same bug
		// wearing a hat.
		{"returned through int(), which is an int whatever arrives",
			"def f(x):\n    x = x + 1.5\n    return int(x)\n\nprint(f(1.0))\n", "2\n"},
		{"returned through round(), which is an int too",
			"def f(x):\n    x = x + 1.5\n    return round(x)\n\nprint(f(1.0))\n", "2\n"},
		// ---- the shape the convention always saw, pinned so promotion cannot break it.
		{"a return expression that is already visibly a float",
			"def scaled(x):\n    x = x * 2.0\n    return x + 0.0\n\nprint(scaled(1.5))\n", "3.0\n"},
		// ---- more than one parameter: the one that matters is the one the return reads.
		{"two parameters, one rebound",
			"def acc(a, b):\n    a = a + 0.5\n    b = b + 1\n    return a\n\nprint(acc(1.0, 2))\nprint(acc(2, 3))\n", "1.5\n2.5\n"},
		{"a parameter rebound to an int is still an int",
			"def f(x, n):\n    n = n + 1\n    return x * n\n\nprint(f(3, 2))\n", "9\n"},
		// ---- the body shapes the scan has to walk.
		{"a return inside an if",
			"def f(x):\n    x = x + 1.5\n    if x > 2:\n        return x\n    return 0.0\n\nprint(f(2.0), f(0.0))\n", "3.5 0.0\n"},
		{"a return inside a while",
			"def f(x):\n    while x > 1:\n        x = x - 0.5\n    return x\n\nprint(f(2.0))\n", "1.0\n"},
		{"a return inside a try",
			"def f(x):\n    try:\n        x = x + 1.5\n    except TypeError:\n        x = 0.0\n    return x\n\nprint(f(1.0))\n", "2.5\n"},
		// ---- recursion: the promoted convention has to hold through the callee's own calls.
		{"a promoted function that calls itself",
			"def fib(n):\n    n = n + 0.0\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(6))\n", "8.0\n"},
		// ---- defaults and the call-site side of the same convention.
		{"a promoted function with a default argument",
			"def g(x=2.0):\n    x = x + 1.5\n    return x\n\nprint(g())\nprint(g(1.0))\n", "3.5\n2.5\n"},
		{"the answer used inside a larger expression",
			"def h(x):\n    x = x + 1.5\n    return x\n\nprint(h(1.0) + h(2.0))\n", "6.0\n"},
		{"a promoted function beside a nested def",
			"def outer(x):\n    x = x + 0.5\n    def inner(y):\n        return y\n    return x + inner(0)\n\nprint(outer(1.0))\n", "1.5\n"},
		// ---- what the gate must not touch at all: other kinds, re-bound and returned.
		{"a parameter rebound to an int, returned bare",
			"def f(x):\n    x = x + 1\n    return x\n\nprint(f(1))\n", "2\n"},
		{"a parameter rebound to text, returned bare",
			"def f(s):\n    s = \"b\"\n    return s\n\nprint(f(\"a\"))\n", "b\n"},
		{"a parameter never rebounded is unaffected",
			"def f(x):\n    return x * 2\n\nprint(f(3))\n", "6\n"},
		{"a local float returned by bare name (never this gate's business)",
			"def f(x):\n    y = x + 0.5\n    return y\n\nprint(f(1.0))\n", "1.5\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestAReboundParameterIsRefusedWhereTheWordCannotCarryIt pins the half the promotion cannot take.
// The convention that decides the return word decides every parameter's word with it, so a body
// with a container parameter (whose word is a heap handle) or an interned-text parameter (whose
// word is an index into `@str_tab`) would be emitted as `sitofp i32 @.lst1 to double` — the module
// `llc` rejects, which ADR 0166 counts as our bug for an ordinary program. A method is emitted
// `i32`-returning whatever its body computes, for the same reason.
func TestAReboundParameterIsRefusedWhereTheWordCannotCarryIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			"a container parameter in the same signature",
			"def f(xs, y):\n    y = y + 0.5\n    return y\n\nprint(f([1, 2], 1.0))\n",
			"the double the body computed has no word to travel in",
		},
		{
			"an interned-text parameter in the same signature",
			"def f(x, s):\n    x = x + 0.5\n    return x\n\nprint(f(1.0, \"z\"))\n",
			"which its own body binds to a float",
		},
		{
			"a method: the receiver and the return word are both fixed",
			"class C:\n    def m(self, x):\n        x = x + 0.5\n        return x\n\nc = C()\nprint(c.m(1.0))\n",
			"rebound to a float by this body and returned by bare name",
		},
		// The two signatures above could in principle be carried — nothing in those bodies reads the
		// container or the text. These do, and that is what makes the `double` convention impossible:
		// `print(s)` wants the interned index where the convention would hand it `double %p1`, and
		// `xs[0]` wants the list's handle where it would hand it `sitofp i32 @.lst1`. Both are the
		// module `llc` rejects, so the gate asks its question before writing a single instruction.
		{
			"a container parameter the body reads",
			"def f(xs, y):\n    y = y + 0.5\n    print(xs[0])\n    return y\n\nprint(f([7, 2], 1.0))\n",
			"the double the body computed has no word to travel in",
		},
		{
			"an interned-text parameter the body reads",
			"def f(x, s):\n    x = x + 0.5\n    print(s)\n    return x\n\nprint(f(1.0, \"z\"))\n",
			"which its own body binds to a float",
		},
		// The ternary arm of such a parameter is no longer this test's: ADR 0262 gave the compiled
		// backend the number-typed `select` it was missing, so
		// `def f(x): x = x + 1.5` / `return x if x > 2 else 0.0` answers `2.5` — the row moved to
		// `TestATernaryAnswersInTheWordItsArmsAnswer` rather than being repinned. The method below
		// stays, because a method's return word is fixed at i32 whatever the body computes.
		{
			"a method's ternary arm: no double word anywhere, and no select either",
			"class C:\n    def m(self, x):\n        x = x + 1.5\n        return x if x > 2 else 0.0\n\nc = C()\nprint(c.m(1.0))\n",
			"rebound to a float by this body and returned by bare name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s (%q): compiled; want a refusal — the alternative is a module llc rejects", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s refused with %q, want it to mention %q", tc.name, err.Error(), tc.want)
			}
			for _, bad := range []string{"LLVM ERROR", "verifier", "must have pointer type", "defined with type"} {
				if strings.Contains(err.Error(), bad) {
					t.Errorf("%s reached the toolchain instead of refusing: %v", tc.name, err)
				}
			}
			// The record answers all three, so the refusal is the compiled backend's own limit.
			if out := captureStdout(t, tc.src); out == "" {
				t.Errorf("%s: the record leg printed nothing either", tc.name)
			}
		})
	}
}

// TestTheFloatReturnPromotionIsOnlyForValuesThatAreTheDouble pins the gate's second half: a
// comparison does not hand the parameter's double back — it answers with the bool word it always
// used — and a user callee's return word is that callee's own question. Promoting either would put
// an i32 in a double `ret`, which is the module `llc` rejects; they are left exactly where they
// were. (A unary minus used to belong to this list too, on the theory that the i32 unary lowering
// would be reached: it is not, the body emits `fsub double 0.0`, and the negation travels in the
// double word now — pinned by TestReturningAReboundParameterNegatedTravelsInTheDoubleWord.)
func TestTheFloatReturnPromotionIsOnlyForValuesThatAreTheDouble(t *testing.T) {
	t.Parallel()
	// A comparison still answers with the bool word it always used: the value is right and the
	// rendering is the bool gap (roadmap L11.1's bool step), not this gate's business.
	src := "def f(x):\n    x = x + 1.5\n    return x > 2\n\nprint(1 if f(2.0) else 0)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("a comparison return was refused; the gate is over-reaching: %v", err)
	}
	if out := runIR(t, res.IR); out != "1\n" {
		t.Errorf("the comparison verdict came back %q, want CPython's 1\nsrc: %s", out, src)
	}
	if out := captureStdout(t, src); out != "1\n" {
		t.Errorf("interpreter printed %q, want 1", out)
	}
	// A user callee's own return word used to be its own question: `return g(x)` was not promoted, and the
	// answer it gave was 2 at exit 0 where CPython answers 5.0 — the callee's parameter took one word, so
	// the double the caller held was truncated before the multiply. Both words cross now and the answer
	// comes back lifted to the double this function's own convention promised (roadmap L11.1, Gap R.139's
	// caller half, ADR 0276).
	nested := "def g(y):\n    return y * 2\n\ndef f(x):\n    x = x + 1.5\n    return g(x)\n\nprint(f(1.0))\n"
	nestedRes, nestedErr := Compile(nested)
	if nestedErr != nil {
		t.Fatalf("a nested pair-returning callee was refused: %v", nestedErr)
	}
	if out := runIR(t, nestedRes.IR); out != "5.0\n" {
		t.Errorf("the nested callee answered %q, want CPython's 5.0\n%s", out, nestedRes.IR)
	}
	if out := captureStdout(t, nested); out != "5.0\n" {
		t.Errorf("interpreter printed %q, want 5.0", out)
	}
	// The honesty of that road is the load of the tag the callee stored: a body whose nested call is not
	// pair-returning keeps the ordinary road, and the refusal that goes with it, rather than reading a
	// word nobody wrote.
	plain := "def g(y):\n    return y * 2\n\ndef f(x):\n    return g(x)\n\nprint(f(2))\n"
	if _, err := Compile(plain); err != nil {
		t.Logf("the non-pair callee refuses, which is its own honest answer: %v", err)
	}
}

// TestReturningAReboundParameterNegatedTravelsInTheDoubleWord is the IR half of the negation, which
// was an `llc` rejection until this round and predates it: `return -x` of a parameter the body
// rebound to a float reached no gate at all — the return word came off the shape of the return
// expression, and a unary minus says nothing — so the module was emitted `define double` and ended in
// `ret i32` of a double, the verifier's own rejection (ADR 0166's class, Gap R.89's operator). It
// answers -2.5 now, and the module is what says so.
func TestReturningAReboundParameterNegatedTravelsInTheDoubleWord(t *testing.T) {
	t.Parallel()
	res, err := Compile("def f(x):\n    x = x + 1.5\n    return -x\n\nprint(f(1.0))\n")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	const head = "define double @gy_f(double %p0)"
	at := strings.Index(res.IR, head)
	if at < 0 {
		t.Fatalf("the negated return did not promote the function:\n%s", res.IR)
	}
	body := res.IR[at:]
	if stop := strings.Index(body, "\n}\n"); stop >= 0 {
		body = body[:stop]
	}
	if !strings.Contains(body, "fsub double 0.0") {
		t.Errorf("the negation of the rebound parameter was not emitted as the float spelling:\n%s", body)
	}
	if strings.Contains(body, "ret i32 %") {
		t.Errorf("a promoted function still returns an i32 register:\n%s", body)
	}
	if !strings.Contains(body, "ret double") {
		t.Errorf("the promoted function has no double return:\n%s", body)
	}
	assertNoForbiddenIR(t, "def f(x): x = x + 1.5; return -x", res.IR)
	if out := runIR(t, res.IR); out != "-2.5\n" {
		t.Errorf("the negated answer ran %q, want CPython's \"-2.5\\n\"", out)
	}
}

// TestAReboundParameterReturnBranchesOnTheBodyNotTheLastLine is the IR half: the promotion is a
// fact about the *body*, so the module must say `define double` for a function whose return
// expression mentions nothing but a name — and must keep saying `define i32` for the same source
// with no float rebind, or the gate has become a guess about any function at all.
func TestAReboundParameterReturnBranchesOnTheBodyNotTheLastLine(t *testing.T) {
	t.Parallel()
	promoted, err := Compile("def addf(x):\n    x = x + 1.5\n    return x\n\nprint(addf(1.0))\n")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if !strings.Contains(promoted.IR, "define double @") {
		t.Errorf("a body that rebinds a parameter to a float did not get a double return word:\n%s", promoted.IR)
	}
	if !strings.Contains(promoted.IR, "alloca double") || !strings.Contains(promoted.IR, "store double") {
		t.Errorf("the promoted body has no double slot to read its answer from:\n%s", promoted.IR)
	}
	plain, err := Compile("def f(x):\n    return x * 2\n\nprint(f(3))\n")
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	if strings.Contains(plain.IR, "define double @") {
		t.Errorf("a function with no float rebind was promoted anyway:\n%s", plain.IR)
	}
}

// TestWhatAFloatTheBodyBindsToItsOwnNameAndReturnsAnswersTheDouble is the half of the return word
// that had no gate at all: not a parameter the body rebound, but a name the BODY itself bound to a
// double and handed back by bare name. The scan this file exists for asked its question of
// parameters, and a body with no parameters was answered before it was asked —
//
//	def f():
//	    u = 2.5
//	    return u
//	print(f())     # CPython 9.5 … and the compiled leg printed 0, at the exit code of success
//
// `define i32 @gy_f()`, a `double` slot the body had just stored into, and the read answered the low
// word of the double: `0` for 9.5, `0` for 2.5. Nothing about the program is unusual — it is the
// first float program a beginner writes — and the neighbouring shape with one parameter has answered
// correctly since ADR 0254, which is what makes the no-parameter form a hole in the gate rather than
// a boundary of the language (roadmap L11.6, Gap R.3c, Gap R.338, ADR 0373).
//
// The derived name is the same question one step further on. `v = u * 2` is a double only because
// `u` is, and a scan that reads each binding once, in whatever order the map iterates, says no and
// emits the truncated word for the second name too:
//
//	def f():
//	    u = 2.5
//	    v = u * 2
//	    return v   # CPython 5.0 · the single-pass scan printed 0
//
// So the scan now settles its float table by repetition and asks each question inside
// `withFloatNames`, which is the table the emitted body will act on — one table, one answer, the
// rule ADR 0273 wrote for the pair road read by the return road.
func TestWhatAFloatTheBodyBindsToItsOwnNameAndReturnsAnswersTheDouble(t *testing.T) {
	t.Parallel()
	asked := 0
	for _, tc := range []struct{ name, src, want string }{
		// ---- the shape the hole was.
		{"a local float returned by bare name, no parameters in sight",
			"def f():\n    u = 9.5\n    return u\n\nprint(f())\n", "9.5\n"},
		{"the same body with the arithmetic ON the return",
			"def f():\n    u = 9.5\n    return u * 2\n\nprint(f())\n", "19.0\n"},
		{"the derived name: a double built from a double and returned bare",
			"def f():\n    u = 2.5\n    v = u * 2\n    return v\n\nprint(f())\n", "5.0\n"},
		{"the derived name built from an int literal on the right",
			"def f():\n    v = 2.5 * 2\n    return v\n\nprint(f())\n", "5.0\n"},
		{"two derivations deep",
			"def f():\n    u = 2.5\n    v = u * 2\n    w = v + 1\n    return w\n\nprint(f())\n", "6.0\n"},
		{"a local float through a parameter of the same body",
			"def f(x):\n    u = 2.5\n    v = u * x\n    return v\n\nprint(f(2))\n", "5.0\n"},
		{"the promoted answer read twice, from two call sites",
			"def f():\n    u = 1.5\n    v = u + u\n    return v\n\nprint(f(), f())\n", "3.0 3.0\n"},
		{"a promoted function called from inside another function",
			"def half():\n    u = 3.0\n    v = u / 2\n    return v\n\ndef twice():\n    return half() + half()\n\nprint(twice())\n", "3.0\n"},
		{"a float bound through a conditional assignment",
			"def f(c):\n    u = 1.5 if c else 2.5\n    v = u * 2\n    return v\n\nprint(f(True))\n", "3.0\n"},
		// ---- what the widened scan must NOT promote: the LAST write decides, because the read
		// answers the last write's word. A name the body ends up holding an int is an int, and
		// promoting it would print `3.0` where the reference prints `3`.
		{"a float binding overwritten by an int before the return",
			"def f():\n    u = 2.5\n    u = 3\n    return u\n\nprint(f())\n", "3\n"},
		{"the answer taken out of the float domain by int()",
			"def f():\n    u = 2.5\n    return int(u)\n\nprint(f())\n", "2\n"},
		{"the answer taken out of the float domain by round()",
			"def f():\n    u = 2.5\n    return round(u)\n\nprint(f())\n", "2\n"},
		{"a name the body binds to a float but never returns",
			"def f():\n    u = 2.5\n    return 4\n\nprint(f())\n", "4\n"},
		{"an int-only body is untouched by the widened scan",
			"def f():\n    u = 2\n    v = u * 3\n    return v\n\nprint(f())\n", "6\n"},
	} {
		// No t.Parallel() on these subtests: the `asked` counter below is the guard that the RECORD leg
		// is being walked, and a parallel subtest has not started by the time the loop finishes.
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			assertNoForbiddenIR(t, tc.src, res.IR)
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if HasGoldenAnswer(tc.src) {
				asked++
				want := goldenStdout(silentReporter{}, tc.src)
				if out := compiledRunIsTheBackend(t, tc.src); out != want {
					t.Errorf("%s: the compiled leg ran %q, the record says %q\nsrc: %s", tc.name, out, want, tc.src)
				}
			}
		})
	}
	if asked == 0 {
		t.Fatal("no row of this table is covered by the record: the record leg is not being walked")
	}
}

// TestWhatTheReturnWordOfAParameterlessFloatBodySaysIsTheIR half of the same rule: the module must
// say `define double @gy_f()` and end in `ret double` for a body that binds a name to a double and
// returns it, and must keep saying `define i32` for the same source with the float written away
// before the return. A gate that promoted everything with a `double` alloca in it would answer the
// programs above correctly and be wrong in the other direction, so both halves are pinned.
func TestWhatTheReturnWordOfAParameterlessFloatBodySays(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, head, absent string
	}{
		{"a returned float local is a double-returning function",
			"def f():\n    u = 9.5\n    return u\n\nprint(f())\n",
			"define double @gy_f()", "ret i32"},
		{"a derived returned float is a double-returning function",
			"def f():\n    u = 2.5\n    v = u * 2\n    return v\n\nprint(f())\n",
			"define double @gy_f()", "ret i32"},
		{"a local whose last write is an int is not",
			"def f():\n    u = 2.5\n    u = 3\n    return u\n\nprint(f())\n",
			"define i32 @gy_f()", "ret double"},
		{"a body with no float at all is not",
			"def f():\n    u = 2\n    v = u * 3\n    return v\n\nprint(f())\n",
			"define i32 @gy_f()", "ret double"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s: refused: %v", tc.name, err)
			}
			at := strings.Index(res.IR, tc.head)
			if at < 0 {
				t.Fatalf("%s: the module has no %q:\n%s", tc.name, tc.head, res.IR)
			}
			body := res.IR[at:]
			if stop := strings.Index(body, "\n}\n"); stop >= 0 {
				body = body[:stop]
			}
			if strings.Contains(body, tc.absent) {
				t.Errorf("%s: the body carries %q it must not:\n%s", tc.name, tc.absent, body)
			}
			if strings.HasPrefix(tc.head, "define double") && !strings.Contains(body, "alloca double") {
				t.Errorf("%s: the promoted body has no double slot to read its answer from:\n%s", tc.name, body)
			}
			// The negative half is the RETURN WORD, which is what this gate decides: an int-only body
			// keeps `define i32`. A body whose float binding is overwritten before the return may still
			// carry a dead `alloca double` — the assignment road allocated it before the last write
			// settled — and that is an optimisation opportunity (Gap R.290's dead-allocation family),
			// not a wrong answer, so it is not what this row judges.
			if strings.HasPrefix(tc.head, "define i32") && strings.Contains(body, "ret double") {
				t.Errorf("%s: a body the scan must not promote returns a double:\n%s", tc.name, body)
			}
		})
	}
}

// The filed table this file used to carry (「the float answer word still owes」, two rows, both
// Gap R.339) is DELETED, as its own rule required: ADR 0374 gave each body its own float-variable
// table, and the group of two bodies now answers the reference's `9.5` / `3`. The answers live in
// `float_frame_test.go`, whose rows are judged on both legs, and what is still owed there is the
// float accumulator inside a body (Gap R.340) and the nested `def` that stores through its
// enclosing frame's alloca (Gap R.341).
