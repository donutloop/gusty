// L11.7 (Gap R.265, Gap R.276): a lambda is a function this backend can build a call to.
//
// Two shapes of that sentence, each judged on both witness legs — CPython's text written into the tables
// below, and the record where the record covers the source:
//
//   - an applied lambda whose body calls one of its own parameters — `(lambda f: f(3))(lambda x: x * 2)`
//     — is a higher-order body with no name in the source. The function-value pass gives it a
//     declaration, clones it once per tuple of functions its called parameters are asked to be, and
//     points the application at the clone (roadmap Gap R.265, ADR 0370);
//   - a lambda that reads a name from the frame that encloses it — `(lambda y: y * 2)(x)` written in a
//     function whose parameter is `x` — crosses that name into the generated function as a parameter the
//     call site fills (roadmap Gap R.276, ADR 0370).
//
// What the pass cannot prove it refuses in words, and the table below names the sentence: a value the
// backend cannot carry as one i32 word is refused, never answered with the word (ADR 0273's rule that a
// refusal says what is missing, ADR 0166's rule that a wrong module is a compiler bug).
package lang

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

// appliedLambdaPrograms is the Gap R.265 probe set. `want` is CPython's stdout for the same source,
// written down rather than read from the record, so a row cannot pass by agreeing with the thing it is
// being checked against; the record-leg test asks the same table for the rows the record covers.
func appliedLambdaPrograms() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"a lambda calls its own parameter", "print((lambda f: f(3))(lambda x: x * 2))\n", "6\n"},
		{"a lambda calls two of them, twice each", "print((lambda f, g, x: f(g(x)) + g(f(x)))(lambda a: a * 2, lambda b: b + 1, 3))\n", "15\n"},
		{"a lambda calls them three times", "print((lambda f: f(1) + f(2))(lambda x: x * 3))\n", "9\n"},
		{"a lambda returns what its parameter returns", "print((lambda f: f(3))(lambda x: x * 2.5))\n", "7.5\n"},
		{"a named lambda fills a lambda's parameter", "f = lambda x: x * 2\n\nprint((lambda g: g(3))(f))\n", "6\n"},
		{"a def hands its parameter to a lambda", "def apply1(fn, v):\n    return fn(v)\n\nprint(apply1(lambda x: x + 1, 2))\n", "3\n"},
		{"a lambda applied inside a higher-order def", "def apply1(fn, v):\n    return fn(v)\n\nprint(apply1(lambda x: (lambda y: y * 2)(x), 4))\n", "8\n"},
		{"a higher-order lambda through a def", "def twice(fn, v):\n    return fn(fn(v))\n\nprint(twice(lambda x: x * 3, 2))\n", "18\n"},
		{"a higher-order lambda of three parameters", "def calling(fn, a, b):\n    return fn(a) + fn(b)\n\nprint(calling(lambda x: x * 2, 3, 4))\n", "14\n"},
		{"one lambda cloned for two tuples", "def twice(fn, x):\n    return fn(fn(x))\n\nprint(twice(lambda n: n * 3, 2))\nprint(twice(lambda n: n * 4, 1))\n", "18\n16\n"},
		{"one lambda cloned at two sites", "def apply(f, x):\n    return f(x)\n\nprint(apply(lambda x: x + 1, 1))\nprint(apply(lambda x: x + 10, 10))\n", "2\n20\n"},
		{"two entries of the same shape answer each other", "def use(cb):\n    return cb(1)\n\nprint(use(lambda x: x + 1))\nprint(use(lambda x: x * 3))\n", "2\n3\n"},
		{"the same call at two tuples", "def calling(fn, a, b):\n    return fn(a) + fn(b)\n\nprint(calling(lambda x: x * 2, 3, 4))\nprint(calling(lambda x: x * 3, 1, 2))\n", "14\n9\n"},
		{"a key= hook whose body calls a builtin", "print(sorted([3, -1, -2], key=lambda v: abs(v)))\n", "[-1, -2, 3]\n"},
		{
			// roadmap L11.7 · Gap R.329 (ADR 0370): the application OMITS the parameter the body reads. The
			// entry tuple is the list of functions the BODY asks for, so the left-out slot arrives with its
			// default and the called one with the entry. The row above that passes BOTH arguments is the same
			// body — nothing about the program differs but how many words the application wrote.
			"an application that omits the lambda's defaulted read parameter",
			"print((lambda f, y=10: f(y))(lambda x: x + 1))\n",
			"11\n",
		},
		{
			// The twin, naming the slot: the road the row above had to reach.
			"an application that names the lambda's defaulted parameter",
			"print((lambda f, y=10: f(y))(lambda x: x * 2, 5))\n",
			"10\n",
		},
		{
			// A read slot left out BELOW the called one keeps its default's word: the clone reads nothing
			// at that position and the ordinary call road fills it the way it fills any omitted argument.
			"an application that omits between a called slot and a read slot",
			"print((lambda f, k=3, n=40: f(k) + n)(lambda x: x * 2))\n",
			"46\n",
		},
		{
			// The omitted parameter is itself CALLED, and its default names the function: the entry the
			// clone builds a call to is the one the default writes, which is what the body would have seen
			// had the application named nothing else (roadmap Gap R.329).
			"an application that omits a called parameter whose default is a lambda",
			"print((lambda f, g=lambda v: v + 4: f(g(2)))((lambda n: n * 10)))\n",
			"60\n",
		},
		{
			// The same omission on the `def` road — the roadmap's other sentence: a higher-order def, its
			// CALLED parameter named, its READ parameter left to its default.
			"a higher-order def called with its called slot named and its read slot defaulted",
			"def apply(f, y=10):\n    return f(y)\n\nprint(apply(lambda x: x + 1))\n",
			"11\n",
		},
		{
			// …and the def whose called slot is left to a lambda default.
			"a higher-order def called with a called slot left to a lambda default",
			"def use(f, g=lambda v: v + 4):\n    return f(g(2))\n\nprint(use(lambda n: n * 10))\n",
			"60\n",
		},
		{
			// roadmap L11.7 · Gap R.334 (ADR 0372): the union the two ADR-0370 roads never made. The body
			// calls one of its own parameters AND reads a name the frame around the application owns — each
			// half answered alone before; the clone now carries the entries its calls name beside one capture
			// parameter per free name, and the call road fills each from the caller's own reading.
			"a capture read by a body that calls its own parameter",
			"def f(x):\n    return (lambda h: h(1) + x)(lambda q: q * 10)\n\nprint(f(2))\n",
			"12\n",
		},
		{
			// All three rules in one body: an entry, a capture, and a slot the application left to its default.
			"a capture, a called parameter and an omitted default in one body",
			"def g(v):\n    return (lambda f, k=3: f(k) + v)(lambda a: a * 2)\n\nprint(g(5))\n",
			"11\n",
		},
		{
			"two captures beside one entry",
			"def h(a, b):\n    return (lambda f: f(a) + b)(lambda z: z * 2)\n\nprint(h(3, 4))\n",
			"10\n",
		},
		{
			// The captured name is a LOCAL of the frame, not its parameter: the list is the names the body is
			// free in, and the frame's own table decides whether that is a capture at all.
			"a local of the frame captured by a body that calls its parameter",
			"def k():\n    t = 7\n    return (lambda f: f(1) + t)(lambda z: z * 2)\n\nprint(k())\n",
			"9\n",
		},
		{
			"two entries and one capture",
			"def m(v):\n    return (lambda f, g: f(g(v)))(lambda a: a * 2, lambda b: b + 1)\n\nprint(m(3))\n",
			"8\n",
		},
		{
			"a capture read by a body that calls its parameter, applied in a comprehension",
			"print([(lambda h: h(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]])\n",
			"[4, 6, 8]\n",
		},
	}
}

// capturedNamePrograms is the Gap R.276 probe set: a lambda that reads a name bound outside itself. The
// same rule as above — `want` is the reference's own stdout.
func capturedNamePrograms() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"a lambda reads the parameter of the function it is applied in", "def f(x):\n    return (lambda y: y * 2)(x)\n\nprint(f(4))\n", "8\n"},
		{"a lambda reads a local of the function it is applied in", "def outer(x):\n    y = x + 1\n    return (lambda z: z + y)(2)\n\nprint(outer(3))\n", "6\n"},
		{"a lambda reads its own enclosing lambda's parameter", "print((lambda x: (lambda y: x + y)(1))(2))\n", "3\n"},
		{"a lambda reads a value the frame bound two lines up", "def f(v):\n    t = v * 2\n    return (lambda k: k + t)(1)\n\nprint(f(3))\n", "7\n"},
		{"a lambda bound to a name reads the enclosing parameter", "def outer(v):\n    f = lambda x: x + v\n    return f(1)\n\nprint(outer(1))\n", "2\n"},
		{"a lambda bound to a name reads the enclosing local", "def outer():\n    b = 42\n    f = lambda: b\n    return f()\n\nprint(outer())\n", "42\n"},
		{"a lambda bound to a name reads the enclosing parameter twice", "def outer(v):\n    f = lambda: v * 2\n    return f()\n\nprint(outer(3))\n", "6\n"},
		{
			// the frame that owns the name is a nested `def` inside another `def`: the closure lift
			// carries the name into the inner body, and the lambda reaches it from there — the shape
			// that needed the closure road to look inside a lambda body at all.
			"a lambda reads a name two def frames up",
			"def add(a):\n    def inner(b):\n        return (lambda c: c + a + b)(1)\n    return inner(10)\n\nprint(add(100))\n", "111\n"},
		{
			"a lambda reads a local of the function enclosing the one it is applied in",
			"def counter():\n    total = 0\n    def bump(n):\n        return (lambda m: m + total)(n)\n    return bump(5)\n\nprint(counter())\n", "5\n"},
		{
			"a lambda applied in a method body reads the method's parameter",
			"class Foo:\n    def bar(self, x):\n        return (lambda y: y + 1)(x)\n\nprint(Foo().bar(3))\n", "4\n"},
		{
			"a lambda applied in a comprehension reads the target",
			"print([(lambda k: k * 2)(x) for x in [1, 2, 3]])\n", "[2, 4, 6]\n"},
	}
}

// ---------------------------------------------------------------------------
// The two witness legs, over both tables.
// ---------------------------------------------------------------------------

// TestWhatAnAppliedLambdaAnswersWhatTheReferencePrints is the reference leg, with CPython's text written
// into the tables: the compiled backend is asked, and the record is not.
func TestWhatAnAppliedLambdaAnswersWhatTheReferencePrints(t *testing.T) {
	for _, tc := range append(appliedLambdaPrograms(), capturedNamePrograms()...) {
		t.Run(tc.name, func(t *testing.T) {
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q reference=%q", got, tc.want)
			}
		})
	}
}

// TestWhatAnAppliedLambdaAnswersWhatTheRecordRecorded is the record leg: the same programs, judged against
// the answer on record. A source with no record is skipped rather than deleted — the record cannot be
// widened from here, and the reference leg above is what judges a source it does not cover.
func TestWhatAnAppliedLambdaAnswersWhatTheRecordRecorded(t *testing.T) {
	asked := 0
	for _, tc := range append(appliedLambdaPrograms(), capturedNamePrograms()...) {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Skipf("no record covers this program")
			}
			asked++
			want := goldenStdout(silentReporter{}, tc.src)
			if got := compiledRunIsTheBackend(t, tc.src); got != want {
				t.Fatalf("compiled=%q record=%q", got, want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("neither table matches a record row: the record leg is not being walked")
	}
}

// TestWhatAnAppliedLambdaRefusesNothingItClaimsToAnswer is ADR 0273's rule over these two tables: a row
// that answers on the reference leg may not come back as a refusal.
func TestWhatAnAppliedLambdaRefusesNothingItClaimsToAnswer(t *testing.T) {
	for _, tc := range append(appliedLambdaPrograms(), capturedNamePrograms()...) {
		t.Run(tc.name, func(t *testing.T) {
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("refused a program the table claims answers: %s", msg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// What the pass cannot prove, it refuses in words — and the sentence names the
// shape, the row that owns it, and the door the program can use instead.
// ---------------------------------------------------------------------------

func TestWhatTheAppliedLambdaRowsLeaveStandingRefuseInWords(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			// The callable would have to exist as a value for the clone to bind: the pass clones the
			// program's functions, and a call of a predeclared name is not one it can clone, so the
			// call is the question the value word would have answered (roadmap L11.1).
			name: "a parameter asked to be a builtin",
			src:  "print((lambda f: f(3))(abs))\n",
			want: "is called but is not a function this backend can build a call to",
		},
		{
			// The body hands the parameter out rather than calling it: nothing is a call, so no tuple
			// of functions can be proven and the lambda would have to exist as a value.
			name: "a parameter passed on, never called",
			src:  "print((lambda f, g: f(g))(lambda x: x, lambda y: y))\n",
			want: "a lambda used as a value has no compiled representation",
		},
		{
			// The callable escapes the frame that would have carried it.
			name: "a lambda returned and applied later",
			src:  "def mk(n):\n    return lambda x: x + n\n\nprint(mk(1)(2))\n",
			want: "a lambda used as a value has no compiled representation",
		},
		{
			// A captured float: the position arrives as a (payload, tag) pair or a `double`, never as
			// the one word a capture travels in.
			name: "a captured float the call site handed a double",
			src:  "def f(v):\n    return (lambda k: k if v else k)(1)\n\nprint(f(2.5))\n",
			want: `reads "v" from the frame that encloses it, and that name holds a float`,
		},
		{
			name: "a captured float the body's own default hands it",
			src:  "def f(v=2.5):\n    return (lambda k: k if v else k)(1)\n\nprint(f())\n",
			want: `reads "v" from the frame that encloses it, and that name holds a float`,
		},
		{
			// The word would reach the print door as a heap handle.
			name: "a captured container",
			src:  "def f(b):\n    return (lambda k: k if b else k)(1)\n\nprint(f([1, 2]))\n",
			want: `that name holds a container`,
		},
		{
			// The word would reach the print door as an @str_tab position.
			name: "a captured text",
			src:  "def f(b):\n    return (lambda k: k if b else k)(1)\n\nprint(f(\"abc\"))\n",
			want: `that name holds a text`,
		},
		{
			name: "a captured bool",
			src:  "def f(b):\n    return (lambda k: k if b else k)(1)\n\nprint(f(True))\n",
			want: `that name holds a bool`,
		},
		{
			name: "a captured None",
			src:  "def f(b):\n    return (lambda k: k if b else k)(1)\n\nprint(f(None))\n",
			want: `that name holds the void value`,
		},
		{
			// The frame the lambda sits in answers one i32 word whatever its body does, so a lambda
			// that answers a double from inside it has nothing to return in.
			name: "a float answered from inside a method",
			src:  "class Foo:\n    def bar(self, x):\n        return (lambda y: y / 2)(x)\n\nprint(Foo().bar(3))\n",
			want: "answers a float, and it is applied inside a method",
		},
		{
			name: "a float answered from inside a closure",
			src:  "def outer():\n    total = 0\n    def bump(n):\n        return (lambda m: m / 2)(n)\n    return bump(5)\n\nprint(outer())\n",
			want: "answers a float, and it is applied inside a closure",
		},
		{
			// Three binding forms deep: the name belongs to a frame two lambdas above, and the
			// capture road reads the frame the application is written in.
			name: "a name two lambdas up",
			src:  "print((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1))\n",
			want: `reads "x" from the frame that encloses it`,
		},
		{
			// An application that omits a parameter the body CALLS has to leave the function somewhere,
			// and a number is not one: the entry tuple would carry "10" as the slot's function, which is
			// the shape ADR 0166's value word is the answer to. The reference raises TypeError here
			// (`<lambda>() missing 1 required positional argument: 'f'`), and this backend is the ordinary
			// road's refusal until the row that owns the value word closes (roadmap Gap R.329, Gap R.31).
			name: "an application that omits a CALLED parameter that has no default",
			src:  "print((lambda f, y=10: f(y))(10))\n",
			want: "is called but is not a function this backend can build a call to",
		},
		{
			// The omission lands on a parameter the body READS whose default is a list. Binding an
			// omitted default is Gap R.277's road — a container default has to arrive as (payload, tag)
			// and the entry tuple has one word to carry it in, so the pass declines rather than read a
			// heap handle as a count (roadmap Gap R.277, ADR 0273's rule over an unanswered shape).
			name: "an application whose omitted read parameter defaults to a container",
			src:  "print((lambda f, y=[]: f(y))(lambda x: len(x)))\n",
			want: "is called but is not a function this backend can build a call to",
		},
		{
			// A required parameter after a defaulted one is not an entry the pass can name a position
			// for, and the reference raises TypeError on the same program (ADR 0283's rule over the
			// unanswered shape — roadmap Gap R.329).
			name: "an application whose omitted slot is followed by a required one",
			src:  "print((lambda f, y: f(y))(lambda x: x))\n",
			want: "is called but is not a function this backend can build a call to",
		},
		{
			// The recursion road's own audit: an application OUTSIDE the body may leave a defaulted slot
			// to its default (the row above), but a call of the higher-order function from inside its own
			// body has to name every position, because the clone follows a recursion by handing each called
			// parameter back to its own position — a recursion that omits one has no tuple to follow
			// (roadmap Gap R.335, the half of the omission ADR 0364's road does not read).
			name: "a higher-order recursion that omits its own defaulted parameter",
			src:  "def run(n, f=lambda x: x):\n    return 0 if n == 0 else f(n) + run(n - 1)\n\nprint(run(3))\n",
			want: "is called but is not a function this backend can build a call to",
		},
		{
			// The capture reaches PAST the frame the application is written in. The nested-`def` lift (ADR 0245)
			// owns that frame's names, and the L11.7 dependency note says the lift does not compose with this
			// pass — the read of `a` two frames up stays the ordinary road's sentence (roadmap Gap R.330, and the
			// half of Gap R.334 the nested-`def` lift still owes).
			name: "a capture two frames up, read by a body that calls its own parameter",
			src:  "def outer(a):\n    def inner(b):\n        return (lambda f: f(1) + a + b)(lambda z: z * 3)\n    return inner(2)\n\nprint(outer(4))\n",
			want: "is called but is not a function",
		},
		{
			// The frame that owns the captured name is a METHOD frame: its parameter arrives from the method
			// call road, which is no call a capture's type can be asked of. The gate says so in ADR 0370's
			// sentence — and names the callable `<lambda>()`, the reader-facing name ADR 0345 §3 keeps in a
			// traceback and in a diagnostic alike, rather than the symbol the pass minted for the clone.
			name: "a capture read by a body that calls its own parameter, in a method frame",
			src:  "class Foo:\n    def bar(self, x):\n        return (lambda h: h(1) + x)(lambda q: q * 10)\n\nprint(Foo().bar(3))\n",
			want: `reads "x" from the frame that encloses it`,
		},
		{
			// A capture whose value is not a number is L11.1's, however the body uses its parameter: the gate
			// is asked before the body is emitted, so no clone of a container-reading body reaches the printer
			// as a heap handle (roadmap Gap R.332, ADR 0273).
			name: "a container capture read by a body that calls its own parameter",
			src:  "def f(b):\n    return (lambda h: h(1) + len(b))(lambda q: q * 10)\n\nprint(f([1, 2]))\n",
			want: "holds a container",
		},
		{
			// The ENTRY-side twin of the union Gap R.334 paid. Here the lambda that reads the caller's frame is
			// not the body the application applies but the ARGUMENT the callee calls — and an entry is a bare
			// symbol, so `base` has no word to travel in, the pass declines, and the ordinary road answers with
			// its generic sentence (roadmap Gap R.336). The account says the true thing:
			// TestWhatTheCallablesAccountSaysAnEntryReadsItsFrame pins the sentence the pass writes, and the
			// terminal will carry it too once Gap R.337 lets a decline reach the refusal.
			name: "an entry lambda that reads the frame its application is written in",
			src:  "def apply1(fn, v):\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
			want: "is called but is not a function",
		},
		{
			// The same decline with the callee's parameter CALLED TWICE, so the clone would have to forward the
			// entry's capture word through the callee's own clone — the shape that makes Gap R.336 more than the
			// one-line fix Gap R.334 was.
			name: "an entry lambda that reads its frame, called twice by a higher-order def",
			src:  "def twice(fn, seed):\n    return fn(fn(seed))\n\ndef use_b(seed):\n    base = 1\n    return twice(lambda x: x * 2 + base, seed)\n\nprint(use_b(2))\n",
			want: "is called but is not a function",
		},
		{
			// The capture is the LOOP TARGET of the frame the application is written in, on both sides at once:
			// the body reads `shift` and the entry reads `step`. The body side is Gap R.334's and answers; the
			// entry side is Gap R.336's, and the whole program is handed back to the ordinary road.
			name: "an entry lambda that reads the loop target of the frame around its application",
			src:  "def stepper():\n    total = 0\n    for step in [1, 2, 3]:\n        shift = step * 10\n        total = total + (lambda fn: fn(2) + shift)(lambda n: n * step)\n    return total\n\nprint(stepper())\n",
			want: "is called but is not a function",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal = %q, want it to mention %q", msg, tc.want)
			}
		})
	}
}

// TestWhatACaptureRefusalNamesTheRoadThatWouldCarryIt pins the part of the sentence a reader acts on: the
// refusal says which name, which generated function, why a word is not an answer, which door does answer,
// and which roadmap rows own the missing half.
func TestWhatACaptureRefusalNamesTheRoadThatWouldCarryIt(t *testing.T) {
	msg := compiledRefusalOf("def f(b):\n    return (lambda k: k if b else k)(1)\n\nprint(f([1, 2]))\n")
	for _, want := range []string{
		`reads "b"`,                        // the name, quoted as the program wrote it
		"lambda_0()",                       // the generated function the reader would have to name
		"as one i32 word",                  // why a word is not an answer
		"write the body as a nested `def`", // the door that does answer
		"roadmap L11.1",                    // the row that would carry the value
		"Gap R.265",                        // and the rows this cycle owns
		"Gap R.276",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("the capture refusal omits %q:\n%s", want, msg)
		}
	}
}

// ---------------------------------------------------------------------------
// The wrong answers this cycle measured and did NOT fix, today's bytes beside
// the reference's. Each case is DELETED — not edited — when its fix lands.
// ---------------------------------------------------------------------------

// TestWhatTheAppliedLambdaRowsLeaveStandingIsFiledNotFixed pins the half of the family that is worse than a
// refusal: a module-level name of a non-number kind, read by a lambda applied at module level, reaches the
// screen as the payload word — at the exit code that means the program ran.
//
//   - `t = "hey"` above `print((lambda: t)())` prints `0` where the reference prints `hey`, and `b = True`
//     above `print((lambda: b)())` prints `1` for `True` (roadmap **Gap R.331**). These are not this row's
//     regression: the pre-ADR-0370 binary prints the same two bytes, which is what localises the defect to the
//     return-kind registration rather than to the lambda road — `funcReturnsFloat` is the template, and the
//     text and bool arms of the same question are missing for every body. They cannot be recorded either:
//     `tools/recmerge` asks CPython and cross-checks the compiled leg, so a source whose legs disagree has no
//     answer to write, and the row keeps the reference's bytes in this table until the tag travels.
//   - The same two programs written INSIDE a `def` are refused by this row's gate with the sentence that names
//     L11.1 (**Gap R.332**'s shape), which is the control that says the gate is on the capture road and the
//     module-global road is the one with the hole.
func TestWhatTheAppliedLambdaRowsLeaveStandingIsFiledNotFixed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		src     string
		want    string // what the reference prints
		today   string // what this toolchain prints today
		rejects string // when one leg dies in `llc` instead, the message it dies with
		gap     string
	}{
		{
			// The CLI answers this program `0` at exit 0 on the linked leg and on the in-process one — and
			// did so on the pre-ADR-0370
			// build too, which is what makes the wrong value filed (Gap R.331) rather than a regression.
			// Asked through the leg every `pkg/lang` compiled case walks, the program goes further: the
			// module calls `@rt_str_intern2` with no definition anywhere, and `llc-20` rejects it — which is
			// Gap R.333, and ADR 0166's exit-2 shape arriving as an exit 0 instead.
			name:    "a module-level text read by an applied lambda: a word on one leg, a rejected module on the other",
			src:     "t = \"hey\"\nprint((lambda: t)())\n",
			want:    "hey\n",
			today:   "0\n",
			rejects: "use of undefined value '@rt_str_intern2'",
			gap:     "Gap R.331, Gap R.333",
		},
		{
			name:  "a module-level bool read by an applied lambda prints its payload",
			src:   "b = True\nprint((lambda: b)())\n",
			want:  "True\n",
			today: "1\n",
			gap:   "Gap R.331",
		},
		{
			// the control: the same body inside a `def` is refused in words, not answered wrongly
			name:  "the same read inside a def is a refusal, not a wrong answer",
			src:   "def f():\n    t = \"hey\"\n    return (lambda: t)()\n\nprint(f())\n",
			want:  "hey\n",
			today: "",
			gap:   "Gap R.332",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.rejects != "" {
				// The leg every `pkg/lang` compiled case walks: ask it, and take whichever answer comes back.
				got, err := RunSource(tc.src)
				if err == nil {
					t.Fatalf("%s: the compiled leg no longer dies in `llc` — it printed %q (the reference prints %q); "+
						"when those two agree, DELETE this case\n\t%s", tc.gap, got, tc.want, tc.src)
				}
				if !strings.Contains(err.Error(), tc.rejects) {
					t.Fatalf("%s: the module rejection changed from %q to:\n%v", tc.gap, tc.rejects, err)
				}
				return
			}
			if tc.today == "" {
				msg := compiledRefusalOf(tc.src)
				if msg == "" {
					t.Fatalf("%s: expected a refusal, the program compiled", tc.gap)
				}
				if !strings.Contains(msg, "L11.1") {
					t.Fatalf("%s: the refusal does not name the row that owns the width: %s", tc.gap, msg)
				}
				return
			}
			got := compiledRunIsTheBackend(t, tc.src)
			if got != tc.today {
				t.Fatalf("%s: today's answer moved from %q to %q — the reference prints %q; when it prints the "+
					"reference's bytes, DELETE this case\n\t%s", tc.gap, tc.today, got, tc.want, tc.src)
			}
			if got == tc.want {
				t.Fatalf("%s: the program now prints the reference's %q — DELETE this case", tc.gap, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The IR the rewrite owes: the application names a function, and no call is made
// through a value.
// ---------------------------------------------------------------------------

func TestWhatAClonedLambdaIsEmittedAsAFunctionAndCalledByName(t *testing.T) {
	mod := compileSrc(t, "def twice(fn, v):\n    return fn(fn(v))\n\nprint(twice(lambda x: x * 3, 2))\n")
	if !strings.Contains(mod, callableCloneInfix) {
		t.Fatalf("the pass minted no clone for the applied lambda:\n%s", headLines(mod, 40))
	}
	for _, line := range strings.Split(mod, "\n") {
		// Only a call INSTRUCTION: a string constant may contain the word, and reading it would put
		// words in the module's mouth that no instruction says.
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "= call ") && !strings.HasPrefix(trimmed, "call ") {
			continue
		}
		i := strings.Index(trimmed, "call ")
		fields := strings.Fields(trimmed[i+len("call "):])
		if len(fields) < 2 {
			t.Fatalf("a malformed call: %q", trimmed)
		}
		// `call <ty> @name(...)` — or `call <ty> (<param types>) @name(...)` for a variadic libc
		// helper, where the callee's type is spelled before its name. Past the type, the callee must
		// be a named function: anything else is a call through a value, which this pass exists to make
		// impossible.
		j := 1
		if j < len(fields) && strings.HasPrefix(fields[j], "(") {
			for j < len(fields) && !strings.Contains(fields[j], ")") {
				j++
			}
			j++
		}
		if j >= len(fields) || !strings.HasPrefix(fields[j], "@") {
			t.Fatalf("a call does not name a function: %q", trimmed)
		}
	}
}

// The module shape of an application that leaves a parameter out: the clone's own `define` carries every
// parameter the BODY asks for, the retargeted `call` names it with the placeholder word at the position the body
// calls, and the defaults the application did not write arrive from the prototype — the same road that fills
// them for a `def` called with too few arguments (ADR 0322). No `fnptr` operand, no indirect `call`, and the
// clone builds its call to the entry by name (roadmap L11.7 · Gap R.329, ADR 0371).
func TestWhatAnOmittedArgumentStillEntersTheClonePositionally(t *testing.T) {
	mod := compileSrc(t, "print((lambda f, k=3, n=40: f(k) + n)(lambda x: x * 2))\n")
	clone := ""
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "define ") && strings.Contains(line, callableCloneInfix) {
			clone = strings.TrimSpace(line)
			break
		}
	}
	if clone == "" {
		t.Fatalf("the pass emitted no clone for the applied lambda:\n%s", headLines(mod, 40))
	}
	// The body asks for three: the entry it calls, and the two slots it reads.
	if n := strings.Count(clone, "i32 %p"); n != 3 {
		t.Fatalf("the clone does not carry the parameters the body asks for: %q", clone)
	}
	// The application wrote ONE argument. The call the clone sees carries the placeholder where the entry
	// sits and the two defaults the application left out, read from the prototype.
	if !regexp.MustCompile(`call i32 @gy_[^(]*` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]*_\d+\(i32 0, i32 3, i32 40\)`).MatchString(mod) {
		t.Fatalf("the retargeted call does not carry the omitted defaults:\n%s", headLines(mod, 60))
	}
	// The clone calls the entry by name — the whole point of the tuple.
	if !regexp.MustCompile(`call i32 @gy_[^(]*\$lambda\$[0-9a-f]+_\d+\(i32 %p1\)`).MatchString(mod) {
		t.Fatalf("the clone does not call the entry directly:\n%s", headLines(mod, 60))
	}
	for _, junk := range []string{"ptrtoint", "inttoptr"} {
		if strings.Contains(mod, junk) {
			t.Fatalf("module contains the junk shape %q:\n%s", junk, headLines(mod, 40))
		}
	}
}

// The account's own witness: an omission is reported as an omission, so two programs whose clones are
// otherwise identical are told apart by the document rather than by a diff of IR (roadmap Gap R.329, ADR 0371).
func TestWhatTheCallablesAccountSaysWhichParametersTheApplicationsLeftOut(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want int
	}{{
		src:  "print((lambda f, y=10: f(y))(lambda x: x * 2, 5))\n",
		want: 0,
	}, {
		src:  "print((lambda f, y=10: f(y))(lambda x: x + 1))\n",
		want: 1,
	}, {
		src:  "print((lambda f, k=3, n=40: f(k) + n)(lambda x: x * 2))\n",
		want: 2,
	}, {
		src:  "print((lambda f, g=lambda v: v + 4: f(g(2)))((lambda n: n * 10)))\n",
		want: 1,
	}} {
		doc, err := CallablesJSON(tc.src)
		if err != nil {
			t.Fatalf("the account failed for %q: %v", tc.src, err)
		}
		var rep struct {
			Rewritten        bool `json:"rewritten"`
			OmittedArguments int  `json:"omitted_arguments"`
		}
		if err := json.Unmarshal([]byte(doc), &rep); err != nil {
			t.Fatalf("the account is not JSON: %v\n%s", err, doc)
		}
		if !rep.Rewritten {
			t.Fatalf("the pass handed this program back: %s", doc)
		}
		if rep.OmittedArguments != tc.want {
			t.Fatalf("omitted_arguments = %d, want %d for %s", rep.OmittedArguments, tc.want, tc.src)
		}
	}
}

// The recursion road's own audit speaks HERE, in the account, and not in the sentence the user is shown: a
// call of the higher-order function from inside its own body must name every position, because a clone follows a
// recursion by handing each called parameter back to its own place (roadmap Gap R.335, ADR 0364's audit — the
// half of the omission the recursion road does not yet read).
func TestWhatTheCallablesAccountSaysTheRecursionLeftASlotOut(t *testing.T) {
	doc, err := CallablesJSON("def run(n, f=lambda x: x):\n    return 0 if n == 0 else f(n) + run(n - 1)\n\nprint(run(3))\n")
	if err != nil {
		t.Fatalf("a decline is still a document: %v", err)
	}
	var rep struct {
		Rewritten        bool     `json:"rewritten"`
		OmittedArguments int      `json:"omitted_arguments"`
		Refused          []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, doc)
	}
	if rep.Rewritten || rep.OmittedArguments != 0 {
		t.Fatalf("a declined recursion reports a rewrite: %s", doc)
	}
	if len(rep.Refused) != 1 || !strings.Contains(rep.Refused[0], "recurses at line") {
		t.Fatalf("the decline does not name the recursion audit: %v", rep.Refused)
	}
}

// The account's witness for the entry-side twin Gap R.336 files: the pass knows WHICH name is the reason it
// declined — the one the entry lambda reads out of the frame its application is written in — and it says so in
// `refused`, naming the missing machinery and the row. The terminal cannot see that sentence yet (Gap R.337), so
// the account is where the honest wording is pinned (ADR 0372).
func TestWhatTheCallablesAccountSaysAnEntryReadsItsFrame(t *testing.T) {
	for _, tc := range []struct{ src, name string }{
		{
			src:  "def apply1(fn, v):\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
			name: "base",
		},
		{
			src:  "def twice(fn, seed):\n    return fn(fn(seed))\n\ndef use_b(seed):\n    base = 1\n    return twice(lambda x: x * 2 + base, seed)\n\nprint(use_b(2))\n",
			name: "base",
		},
		{
			src:  "def stepper():\n    total = 0\n    for step in [1, 2, 3]:\n        shift = step * 10\n        total = total + (lambda fn: fn(2) + shift)(lambda n: n * step)\n    return total\n\nprint(stepper())\n",
			name: "step",
		},
	} {
		doc, err := CallablesJSON(tc.src)
		if err != nil {
			t.Fatalf("a decline is still a document: %v", err)
		}
		var rep struct {
			Rewritten bool     `json:"rewritten"`
			Refused   []string `json:"refused"`
		}
		if err := json.Unmarshal([]byte(doc), &rep); err != nil {
			t.Fatalf("the account is not JSON: %v\n%s", err, doc)
		}
		if rep.Rewritten || len(rep.Refused) != 1 {
			t.Fatalf("the pass rewrote a program whose entry reads a frame: %s", doc)
		}
		for _, want := range []string{`reads "` + tc.name + `"`, "an entry is a bare symbol", "Gap R.336"} {
			if !strings.Contains(rep.Refused[0], want) {
				t.Fatalf("the entry refusal omits %q:\n%v", want, rep.Refused)
			}
		}
	}
}

// The module shape of the union Gap R.334 opened: a clone of an applied `lambda` that calls one of its own
// parameters AND reads the frame around its application carries BOTH lists — the entries its calls name, in
// `calledPos` order, and one capture parameter per free name — and the call the caller emits fills the capture
// from its OWN reading of that name (`i32 %p0`, the caller's parameter word), never a copy of the value. The
// clone builds its call to the entry by name; no `fnptr` operand, no indirect `call` (roadmap L11.7 · Gap R.334,
// ADR 0372).
func TestWhatAClonedLambdaCarriesItsEntriesAndItsCaptures(t *testing.T) {
	mod := compileSrc(t, "def f(x):\n    return (lambda h: h(1) + x)(lambda q: q * 10)\n\nprint(f(2))\n")
	clone := ""
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "define ") && strings.Contains(line, callableCloneInfix) {
			clone = strings.TrimSpace(line)
			break
		}
	}
	if clone == "" {
		t.Fatalf("the pass emitted no clone for the applied lambda:\n%s", headLines(mod, 40))
	}
	// One parameter for the entry the body calls, one for the capture it reads.
	if n := strings.Count(clone, "i32 %p"); n != 2 {
		t.Fatalf("the clone does not carry the entries AND the captures: %q", clone)
	}
	// The caller hands in the placeholder for the entry and its OWN word for the captured name.
	if !regexp.MustCompile(`call i32 @gy_[^(]*` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]*_\d+\(i32 0, i32 %p\d+\)`).MatchString(mod) {
		t.Fatalf("the call does not fill the capture from the caller's own reading:\n%s", headLines(mod, 60))
	}
	// The clone calls the entry directly — the tuple, not a value.
	if !regexp.MustCompile(`call i32 @gy_[^(]*\$lambda\$[0-9a-f]+_\d+\(i32 1\)`).MatchString(mod) {
		t.Fatalf("the clone does not call the entry directly:\n%s", headLines(mod, 60))
	}
	for _, junk := range []string{"ptrtoint", "inttoptr"} {
		if strings.Contains(mod, junk) {
			t.Fatalf("module contains the junk shape %q:\n%s", junk, headLines(mod, 40))
		}
	}
}

// The account's witness for the union: `capture_parameters` says how many capture parameters the declarations
// this pass minted carry, which is the number that separates a clone of Gap R.265's self-contained body from a
// clone of Gap R.334's capturing one (ADR 0372).
func TestWhatTheCallablesAccountCountsTheCapturesOnAClone(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want int
	}{{
		src:  "print((lambda f: f(3))(lambda x: x * 2))\n",
		want: 0,
	}, {
		src:  "def f(x):\n    return (lambda h: h(1) + x)(lambda q: q * 10)\n\nprint(f(2))\n",
		want: 1,
	}, {
		src:  "def h(a, b):\n    return (lambda f: f(a) + b)(lambda z: z * 2)\n\nprint(h(3, 4))\n",
		want: 2,
	}} {
		doc, err := CallablesJSON(tc.src)
		if err != nil {
			t.Fatalf("the account failed for %q: %v", tc.src, err)
		}
		var rep struct {
			Rewritten         bool `json:"rewritten"`
			CaptureParameters int  `json:"capture_parameters"`
		}
		if err := json.Unmarshal([]byte(doc), &rep); err != nil {
			t.Fatalf("the account is not JSON: %v\n%s", err, doc)
		}
		if !rep.Rewritten {
			t.Fatalf("the pass handed this program back: %s", doc)
		}
		if rep.CaptureParameters != tc.want {
			t.Fatalf("capture_parameters = %d, want %d for %s", rep.CaptureParameters, tc.want, tc.src)
		}
	}
}

func TestWhatACapturedNameArrivesAsAParameterTheCallSiteFills(t *testing.T) {
	// A capture, not an argument: the body reads `y`, which the frame above it owns.
	mod := compileSrc(t, "def outer(x):\n    y = x + 1\n    return (lambda z: z + y)(2)\n\nprint(outer(3))\n")
	line, ok := definitionOf(mod, "gy_lambda_")
	if !ok {
		t.Fatalf("the emitted module has no generated lambda definition:\n%s", headLines(mod, 40))
	}
	// The source spells a one-parameter lambda; the emitted one takes the captured name beside it.
	if n := strings.Count(line, "i32"); n < 3 {
		t.Fatalf("the generated lambda does not take the captured name as a parameter: %q", line)
	}
	if !strings.Contains(mod, "call i32 @gy_lambda_0(") {
		t.Fatalf("the application does not name the generated function:\n%s", headLines(mod, 60))
	}
}

// ---------------------------------------------------------------------------
// The machine path: the account names a cloned lambda, and names it the way the
// reference does.
// ---------------------------------------------------------------------------

func TestWhatTheCallablesAccountCountsAClonedLambda(t *testing.T) {
	doc, err := CallablesJSON("print((lambda f: f(3))(lambda x: x * 2))\n")
	if err != nil {
		t.Fatalf("the account failed: %v", err)
	}
	var rep struct {
		Rewritten          bool     `json:"rewritten"`
		HigherOrderLambdas int      `json:"higher_order_lambdas"`
		HigherOrderFuncs   []string `json:"higher_order_functions"`
		Symbols            []string `json:"symbols"`
		Refused            []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, doc)
	}
	if !rep.Rewritten || rep.HigherOrderLambdas != 1 {
		t.Fatalf("the cloned lambda is not accounted for: %s", doc)
	}
	if len(rep.Refused) != 0 {
		t.Fatalf("a rewritten program reports refusals: %v", rep.Refused)
	}
	// A reader meets the clone under the name the reference gives it, not the pass's symbol.
	if len(rep.HigherOrderFuncs) != 1 || !strings.Contains(rep.HigherOrderFuncs[0], "<lambda>") {
		t.Fatalf("the higher-order list does not name the cloned lambda as the reference does: %v", rep.HigherOrderFuncs)
	}
	for _, s := range rep.Symbols {
		if !strings.Contains(s, callableCloneInfix) && !strings.Contains(s, callableLambdaInfix) {
			t.Fatalf("symbol %q is not one this pass minted", s)
		}
	}
}

// A shape the pass declines says which shape, and keeps its row: the account is where a reader finds out
// that the refusal they are about to see was decided here and not at the doors.
func TestWhatTheCallablesAccountSaysWhichLambdaShapeItCouldNotProve(t *testing.T) {
	doc, err := CallablesJSON("print((lambda f: f(3))(abs))\n")
	if err != nil {
		t.Fatalf("a refusal is still a document: %v", err)
	}
	if !strings.Contains(doc, `"rewritten": false`) {
		t.Fatalf("the pass claims to have rewritten a program it left alone:\n%s", doc)
	}
	if !strings.Contains(doc, "roadmap L11.7") {
		t.Fatalf("the decline does not name the row that owns it:\n%s", doc)
	}
}

// headLines is the diagnostic a failing table wants: the head of a module, not all 400 lines of it.
func headLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// definitionOf finds the `define` line whose symbol carries the name (the module prefixes generated
// functions, so the test asks for the substring the reader would recognise), so a test can read the
// generated signature without hard-coding the clone suffix the pass happens to mint.
func definitionOf(mod, name string) (string, bool) {
	for _, line := range strings.Split(mod, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "define ") && strings.Contains(t, "@"+name) {
			return t, true
		}
	}
	return "", false
}

// ---------------------------------------------------------------------------
// The walk the capture road asks its question with must terminate on a tree the
// rewrites themselves produced.
// ---------------------------------------------------------------------------

// TestTheCallableWalkCutsACycleAndStillWalksASharedNodeTwice guards the traversal `captureParamWord`
// reaches for. `callableWalk` is reflection over the AST on purpose — a hand-written switch stops
// seeing a construct the day someone adds a node kind — but a rewrite can point a node back at
// something that encloses it (a self-call whose callee names the declaration that owns the body is that
// shape), and a walk that trusts the tree to be a tree dies in the runtime instead of in a sentence the
// program could be shown. The guard is a path set, so a node is descended into at most once per path:
// an acyclic program is walked exactly as it always was — which the second half of this test pins,
// because the pass rewrites calls by their ORDINAL and a visited-set would quietly renumber them — and
// a cycle is cut where it closes.
func TestTheCallableWalkCutsACycleAndStillWalksASharedNodeTwice(t *testing.T) {
	t.Parallel()

	// The cycle: a `lambda` whose body is a call whose callee is that same `lambda`.
	cyclic := &Lambda{}
	cyclic.Body = &Call{Fn: cyclic, Args: []Expr{&IntLit{Value: 1}}}
	visited := make(chan int, 1)
	go func() {
		n := 0
		w := callableWalker(func(Node, *FuncDef) { n++ })
		w.node(cyclic, nil)
		visited <- n
	}()
	select {
	case n := <-visited:
		if n < 2 {
			t.Fatalf("the walk visited %d nodes and stopped: the guard cut the walk down instead of the "+
				"cycle — the `lambda` and its body's call must both be seen", n)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the walk did not terminate: a cyclic AST reached the runtime's stack limit (ADR 0166's " +
			"shape — the compiler's own output is the bug, and a hang is worse)")
	}

	// The control, and the reason the guard is a path set and not a memo: one node reached by two
	// routes must be visited on BOTH. The pass rewrites the k-th call of a body by its ordinal, and a
	// walk that remembered nodes would answer the second route with nothing and renumber the calls.
	shared := &Name{Value: "f"}
	seen := 0
	w := callableWalker(func(n Node, _ *FuncDef) {
		if n == Node(shared) {
			seen++
		}
	})
	w.node(&Call{Fn: shared, Args: []Expr{shared}}, nil)
	if seen != 2 {
		t.Fatalf("a node reached by two routes was visited %d times; every call site the pass rewrites by "+
			"ordinal depends on the walk visiting a shared node on each route to it", seen)
	}
}
