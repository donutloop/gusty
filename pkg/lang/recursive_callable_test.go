package lang

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// recursive_callable_test.go — a higher-order body that calls itself is cloned, not refused.
// roadmap L11.7 · Gap R.267 (and the entry-tuple ordinal it exposed, Gap R.313); ADR 0364.
//
// ADR 0345 clones a function whose body calls one of its own parameters: one clone per distinct
// function the call sites hand that parameter, each clone calling the entry by name. That account
// silently assumed the recursion never happens. Write the most ordinary higher-order recursion —
//
//	def fact(n, f):
//	    if n <= 1:
//	        return 1
//	    return f(n) * fact(n - 1, f)
//	print(fact(4, lambda x: x))                    # the reference: 24
//
// — and the backend answered:
//
//	codegen: "f" is called but is not a function this backend can build a call to
//
// The reason was structural, not a missing case in the clone road. `fact(n - 1, f)` mentions `f`
// anywhere but in the callee position, `onlyCalledInBody` said "a parameter that is also read", the
// function was never registered as higher-order at all, and the whole rewrite declined. Meanwhile a
// clone that *did* get built still held the call to `fact` — a `define` ADR 0345 had marked
// unemittable — so the invariant pulled the rewrite back and the program kept the refusal.
//
// Two answers, both in `pkg/lang/callable_value.go`:
//
//   - The recursion is credited as a use, not read as a value: a parameter may appear, in addition to
//     being called, as the argument that hands itself back to its OWN position of the enclosing
//     function's own call. The clone answers that call by calling itself, and fills the slot with the
//     same plain integer an outside call site gets, because a clone reads nothing there.
//   - The clone's entry tuple is indexed by the ordinal of the called parameter, not by its position in
//     the parameter list (Gap R.313). The two coincide exactly when the callable comes first, which is
//     the only shape the suite had ever written, so `def run(v, fn): return fn(v)` had been handing its
//     own rewrite back and answering with the pre-pass refusal.
//
// What stays refused is the recursion whose clone would need an entry the plan cannot name — a callable
// swapped for another mid-recursion, a recursion between two higher-order bodies, a recursion written
// inside a `lambda`, a self-call that does not fill every position. Each declines the pass whole, in a
// sentence that names the recursion as the reason, and the program keeps the honest refusal it had.
//
// Every `want` below is what CPython prints for the same source; `integration/recursive_callable_test.go`
// runs the same programs through `python3` and derives its expectation from the interpreter rather than
// from this file.

// recursiveAnswered is the answer table: a higher-order body that calls itself, and the ordinary
// higher-order shapes the ordinal fix reaches.
var recursiveAnswered = []struct {
	name string
	src  string
	want string
}{
	{
		"the row's own program",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(4, lambda x: x))",
		"24",
	},
	{
		"a declared function enters the recursion",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\ndef dbl(x):\n    return x * 2\n\nprint(fact(3, dbl))",
		"24",
	},
	{
		// one body, two entries: two clones, each calling itself and its own entry
		"two entries ask for two clones",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\ndef dbl(x):\n    return x * 2\n\nprint(fact(3, dbl), fact(3, lambda v: v))",
		"24 6",
	},
	{
		"a recursion that sums",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nprint(down(3, lambda v: v))",
		"6",
	},
	{
		"the same recursion, two different callables",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\ndef sq(x):\n    return x * x\n\nprint(down(3, sq), down(2, lambda v: 0 - v))",
		"14 -3",
	},
	{
		// Gap R.313's program: the called parameter is not the first one the source wrote
		"a callable that is not the first parameter",
		"def run(v, fn):\n    return fn(v)\n\ndef inc(y):\n    return y + 1\n\nprint(run(21, inc))",
		"22",
	},
	{
		"a callable that is not the first parameter, recursively",
		"def total(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + total(n - 1, f)\n\nprint(total(4, lambda v: 0 - v))",
		"-10",
	},
	{
		"two called parameters, neither first",
		"def pick(a, f, g, b):\n    if a <= 0:\n        return b\n\n    return f(a) + g(a) + pick(a - 1, f, g, b)\n\nprint(pick(2, lambda v: v, lambda v: v * 2, 100))",
		"109",
	},
	{
		"a parameter the body calls twice, no recursion",
		"def apply2(v, f):\n    return f(f(v))\n\nprint(apply2(3, lambda x: x * 3))",
		"27",
	},
	{
		"the recursion prints on the way down",
		"def walk(n, f):\n    if n <= 0:\n        return 0\n\n    print(f(n))\n    return walk(n - 1, f)\n\nprint(walk(3, lambda v: v * 10))",
		"30\n20\n10\n0",
	},
	{
		"the recursion is entered from another body",
		"def total(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + total(n - 1, f)\n\ndef outer(k):\n    return total(k, lambda v: v * 2)\n\nprint(outer(3))",
		"12",
	},
	{
		"five deep",
		"def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(5, lambda v: v))",
		"120",
	},
	{
		"the base case asks the callable too",
		"def walk(n, f):\n    if n <= 0:\n        return f(0)\n\n    return f(n) + walk(n - 1, f)\n\nprint(walk(3, lambda v: v))",
		"6",
	},
	{
		"the answer is used as a number",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nprint(1 + down(3, lambda v: v), down(3, lambda v: v) * 2)",
		"7 12",
	},
	{
		"a loop calls the parameter and the recursion closes the body",
		"def each(n, f):\n    total = 0\n    for k in range(1, n + 1):\n        total = total + f(k)\n\n    if n <= 0:\n        return total\n\n    return total + each(n - 1, f)\n\nprint(each(3, lambda v: v * 2))",
		"20",
	},
	{
		// the answer is a text: the clone's answer travels to the concatenation with its kind intact
		"a text answer",
		"def join(n, f):\n    if n <= 0:\n        return \"\"\n\n    return f(n) + join(n - 1, f)\n\nprint(join(3, lambda v: \"ab\"))",
		"ababab",
	},
	{
		"a callable that answers a verdict",
		"def count(n, f):\n    if n <= 0:\n        return 0\n\n    if f(n):\n        return 1 + count(n - 1, f)\n\n    return count(n - 1, f)\n\nprint(count(5, lambda v: v > 2))",
		"3",
	},
	{
		"two recursive higher-order bodies in one program",
		"def up(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + up(n - 1, f)\n\ndef dn(n, g):\n    if n <= 0:\n        return 1\n\n    return g(n) * dn(n - 1, g)\n\nprint(up(3, lambda v: v), dn(3, lambda v: v))",
		"6 6",
	},
	{
		"the recursion is a condition's operand",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nprint(1 if down(3, lambda v: v) > 5 else 0)",
		"1",
	},
	{
		// the entry arrives by an alias: `k = treble` is retargeted, and the recursion still clones
		"the entry arrives by a name the program gave it",
		"def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\ndef treble(v):\n    return v * 3\n\nk = treble\nprint(down(3, k))",
		"18",
	},
	{
		"a recursive higher-order body beside a plain one",
		"def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\ndef d2(x):\n    return x * 2\n\nprint(twice(d2, 3), down(3, d2))",
		"12 12",
	},
	{
		"the recursion feeds a builtin's argument",
		"def step(n, f):\n    if n <= 0:\n        return 0 - 6\n\n    return abs(f(step(n - 1, f)))\n\nprint(step(3, lambda v: v))",
		"6",
	},
	{
		"the negative control: a plain recursion, no callable parameter",
		"def fact(n):\n    if n <= 1:\n        return 1\n\n    return n * fact(n - 1)\n\nprint(fact(5))",
		"120",
	},
	{
		"the negative control: a higher-order body with no recursion",
		"def apply1(fn, v):\n    return fn(v)\n\ndef inc(x):\n    return x + 1\n\nprint(apply1(inc, 41))",
		"42",
	},
}

// recursiveRefused is the refusal table. Each row names the shape the pass declines with, and the
// row that owns what the program still cannot do.
var recursiveRefused = []struct {
	name     string
	src      string
	names    string
	decline  string
	owns     string
	frontEnd bool // the refusal is the front end's, which cites no row
}{
	{
		// the clone would need an entry for the SECOND function as well, chosen mid-recursion
		name:    "a recursion that swaps the callable",
		src:     "def run(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1, other)\n\ndef other(x):\n    return x\n\nprint(run(2, lambda v: v))",
		names:   `is called but is not a function this backend can build a call to`,
		decline: "hands \"f\" something other than itself",
		owns:    "Gap R.267",
	},
	{
		// two bodies that call the callable and hand it to each other: neither clone owns the call
		name:    "a recursion between two higher-order bodies",
		src:     "def a(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + b(n - 1, f)\n\ndef b(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + a(n - 1, f)\n\ndef inc(x):\n    return x\n\nprint(a(3, inc))",
		names:   `is called but is not a function this backend can build a call to`,
		decline: "a recursion between two higher-order functions",
		owns:    "Gap R.267",
	},
	{
		// the recursion is a use of the function's own name, and that name has been taken by a
		// parameter — the call belongs to the frame, and the frame's answer is the reference's TypeError
		name:    "a recursion that hands back the wrong word",
		src:     "def run(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1, \"x\")\n\nprint(run(2, lambda v: v))",
		names:   `is called but is not a function this backend can build a call to`,
		decline: "hands \"f\" something other than itself",
		owns:    "Gap R.267",
	},
	{
		// a self-call that omits a parameter is a call the plan cannot audit its arity against
		name:    "a recursion that does not fill the parameters",
		src:     "def run(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + run(n - 1)\n\nprint(run(2, lambda v: v))",
		names:   `is called but is not a function this backend can build a call to`,
		decline: "without filling every parameter positionally",
		owns:    "Gap R.267",
	},
	{
		// the recursion is written where no clone's frame reaches
		name:     "a recursion written inside a lambda",
		src:      "def run(n, f):\n    if n <= 0:\n        return 0\n\n    h = lambda k: run(k, f)\n    return f(n) + h(n - 1)\n\nprint(run(2, lambda v: v))",
		names:    `undefined name "f"`,
		decline:  "",
		owns:     "Gap R.265",
		frontEnd: true,
	},
	{
		// the name of a higher-order function that reaches a container is a function VALUE, and this
		// pass makes names, not values: the invariant pulls the whole rewrite back
		name:    "a recursive higher-order function stored in a container",
		src:     "def down(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + down(n - 1, f)\n\nxs = [down]\nprint(len(xs))",
		names:   `is called but is not a function this backend can build a call to`,
		decline: "",
		owns:    "Gap R.150",
	},
	{
		// a method's self-call is an attribute read, not a call of a name: the recursion is the
		// method road's, and the method road takes a receiver, not a clone
		name:    "a method that recurses through its callable parameter",
		src:     "class C:\n    def down(self, n, f):\n        if n <= 0:\n            return 0\n\n        return f(n) + self.down(n - 1, f)\n\ndef dbl(v):\n    return v * 2\n\nc = C()\nprint(c.down(3, dbl))",
		names:   `names function, which the compiled backend has no value for`,
		decline: "",
		owns:    "Gap R.268",
	},
}

// TestARecursiveHigherOrderBodyAnswersWhatTheReferenceAnswers is the compiled leg: the module the
// compiler writes and the JIT that runs it. Every `want` is CPython's text.
func TestARecursiveHigherOrderBodyAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range recursiveAnswered {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := tc.src + "\n"
			got, refused := compiledOutOrRefusal(t, src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tc.name, got, tc.want)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Fatalf("%q printed %q, want %q", tc.name, strings.TrimSpace(got), tc.want)
			}
			out, trap := runForParity(t, src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tc.name, trap, tc.want)
			}
			if out != tc.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tc.name, out, tc.want)
			}
		})
	}
}

// TestARecursiveHigherOrderBodyIsWhatTheRecordSaid is the record leg. A source the record does not
// hold skips this leg rather than passing it silently; the sources this row first asked were recorded
// from the reference, and the record's own meta.added_after_0302 says so.
func TestARecursiveHigherOrderBodyIsWhatTheRecordSaid(t *testing.T) {
	asked := 0
	for _, tc := range recursiveAnswered {
		// No t.Parallel() on the child: the parent's "did any row hit the record" guard runs when
		// this body returns, and a parallel child has not run by then (ADR 0343's lesson, learned the
		// hard way in the cycle before this one).
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src + "\n"
			if !HasGoldenAnswer(src) {
				t.Skipf("the record holds no answer for this source")
			}
			asked++
			want := strings.TrimSpace(goldenStdout(silentReporter{}, src))
			if want != tc.want {
				t.Fatalf("the record says %q, this table says %q — one of them is wrong", want, tc.want)
			}
			got, refused := compiledOutOrRefusal(t, src)
			if refused {
				t.Fatalf("%q refused (%s); the record prints %q", tc.name, got, want)
			}
			if strings.TrimSpace(got) != want {
				t.Fatalf("%q printed %q, the record printed %q", tc.name, strings.TrimSpace(got), want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("the answer table matches no record row at all: the record leg is not being walked")
	}
}

// TestWhatARecursiveHigherOrderBodyCannotAnswerStillRefusesInWords holds a decline to ADR 0166 and
// ADR 0273: the program keeps the refusal the ordinary road always gave, the pass's own account says
// which recursion it could not follow, and no refusal is dressed up as an answer.
func TestWhatARecursiveHigherOrderBodyCannotAnswerStillRefusesInWords(t *testing.T) {
	t.Parallel()
	for _, tc := range recursiveRefused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := tc.src + "\n"
			msg := compiledRefusalOf(src)
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tc.names)
			}
			if !strings.Contains(msg, tc.names) {
				t.Fatalf("refusal = %q, want it to mention %q", msg, tc.names)
			}
			if !strings.Contains(msg, "roadmap") && !tc.frontEnd {
				t.Fatalf("refusal %q does not point at a row", msg)
			}
			if tc.decline != "" {
				doc, aerr := CallablesJSON(src)
				if aerr != nil {
					t.Fatalf("the account of a declined program is an answer, not a tool failure: %v", aerr)
				}
				var rep callableReport
				if uerr := json.Unmarshal([]byte(doc), &rep); uerr != nil {
					t.Fatalf("the account is not the document the schema promises: %v\n%s", uerr, doc)
				}
				if rep.Rewritten {
					t.Fatalf("the pass rewrote a recursion it also says it declined:\n%s", doc)
				}
				joined := strings.Join(rep.Refused, "\n")
				if !strings.Contains(joined, tc.decline) {
					t.Fatalf("the pass does not say which recursion it declined (%s):\n%s", tc.decline, joined)
				}
				if !strings.Contains(joined, "roadmap L11.7") {
					t.Fatalf("the decline does not point at its row:\n%s", joined)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The IR the recursion owes: a clone that calls itself, and nothing else.
// ---------------------------------------------------------------------------

// TestTheRecursionIsAnsweredByACloneCallingItself reads the module the rewrite produced. The shape is
// the whole decision: a clone whose body calls the clone, an entry called by name, the callable's slot
// filled with the placeholder, and no unspecialised `define` left behind to be called by mistake.
func TestTheRecursionIsAnsweredByACloneCallingItself(t *testing.T) {
	t.Parallel()
	src := "def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(4, lambda x: x))\n"
	mod := compileSrc(t, src)
	clone := regexp.MustCompile(`define i32 @gy_fact` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]+_\d+\(i32 %p0, i32 %p1\)`)
	if !clone.MatchString(mod) {
		t.Fatalf("no clone of fact was emitted:\n%s", mod)
	}
	if strings.Contains(mod, "define i32 @gy_fact(") {
		t.Fatalf("the unspecialised definition is still emitted:\n%s", mod)
	}
	self := regexp.MustCompile(`call i32 @gy_fact` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]+_\d+\(i32 %t\d+, i32 0\)`)
	if !self.MatchString(mod) {
		t.Fatalf("the clone does not call itself with the slot filled:\n%s", mod)
	}
	hook := regexp.MustCompile(`call i32 @gy_module` + regexp.QuoteMeta(callableLambdaInfix) + `[0-9a-f]+_\d+\(i32 %\w+\)`)
	if !hook.MatchString(mod) {
		t.Fatalf("the clone does not call the entry directly:\n%s", mod)
	}
	for _, junk := range []string{"ptrtoint", "inttoptr"} {
		if strings.Contains(mod, junk) {
			t.Fatalf("the module reaches a call through a cast (%s):\n%s", junk, mod)
		}
	}
}

// TestTheRecursionIsAnsweredByEveryCloneSeparately is the multi-entry case: two call sites, two
// entries, and each clone calls ITSELF and its OWN entry — a clone that called the other's entry
// would answer one program with the other's function.
func TestTheRecursionIsAnsweredByEveryCloneSeparately(t *testing.T) {
	t.Parallel()
	src := "def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\ndef dbl(x):\n    return x * 2\n\nprint(fact(3, dbl), fact(3, lambda v: v))\n"
	mod := compileSrc(t, src)
	clones := regexp.MustCompile(`define i32 @gy_fact` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]+_\d+\(`)
	if got := len(clones.FindAllString(mod, -1)); got != 2 {
		t.Fatalf("want two clones of fact, the module defines %d:\n%s", got, mod)
	}
	selfs := regexp.MustCompile(`call i32 @gy_fact` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]+_\d+\(i32 %t\d+, i32 0\)`)
	if got := len(selfs.FindAllString(mod, -1)); got != 2 {
		t.Fatalf("want each clone to call itself once, found %d:\n%s", got, mod)
	}
	if !strings.Contains(mod, "call i32 @gy_dbl(i32 %") {
		t.Fatalf("the declared entry is not called by name:\n%s", mod)
	}
}

// TestTheCalledParameterReachesTheEntryAtItsOwnSlot is the Gap R.313 IR row: the entry tuple is
// indexed by the ordinal of the called parameter, so a callable written second is still called by
// name. Before the fix this program's clone bailed out of its own rewrite and the answer came from
// the pre-pass refusal.
func TestTheCalledParameterReachesTheEntryAtItsOwnSlot(t *testing.T) {
	t.Parallel()
	src := "def run(v, fn):\n    return fn(v)\n\ndef inc(y):\n    return y + 1\n\nprint(run(21, inc))\n"
	mod := compileSrc(t, src)
	if strings.Contains(mod, "define i32 @gy_run(") {
		t.Fatalf("the unspecialised definition is still emitted:\n%s", mod)
	}
	if !regexp.MustCompile(`call i32 @gy_inc\(i32 %`).MatchString(mod) {
		t.Fatalf("the clone does not call the entry by name:\n%s", mod)
	}
	clone := regexp.MustCompile(`define i32 @gy_run` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]+_\d+\(i32 %p0, i32 %p1\)`)
	if !clone.MatchString(mod) {
		t.Fatalf("the clone kept the source arity and slot order:\n%s", mod)
	}
}

// TestTheSlotIsTheOrdinalNotTheParameterPosition asks the plan directly, over the two shapes whose
// answers used to depend on where the callable sat in the parameter list: the inner call records BOTH
// the parameter it evaluates (`pos`) and the entry of the clone's tuple that parameter is asked to be
// (`slot`). Reading one as the other is what handed `def run(v, fn)` back to the pre-pass refusal
// (roadmap Gap R.313, ADR 0364).
func TestTheSlotIsTheOrdinalNotTheParameterPosition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		src       string
		fn        string
		calledPos []int
		slots     []int
		sites     int // how many call sites the plan read
		clones    int // how many distinct entry tuples those sites ask for
	}{
		{
			"the callable first, as every program this suite wrote has had it",
			"def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x):\n    return x * 2\n\nprint(twice(d2, 3))\n",
			"twice", []int{0}, []int{0, 0}, 1, 1,
		},
		{
			"the callable second, which no program in the suite had ever written",
			"def run(v, fn):\n    return fn(v)\n\ndef inc(y):\n    return y + 1\n\nprint(run(21, inc))\n",
			"run", []int{1}, []int{0}, 1, 1,
		},
		{
			// one called parameter, and the recursion hands it back to its own position
			"the callable second, with a recursion",
			"def total(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + total(n - 1, f)\n\nprint(total(3, lambda v: v))\n",
			"total", []int{1}, []int{0}, 1, 1,
		},
		{
			// two called parameters, each reading its own entry of the one tuple the site fills
			"two called parameters, and each keeps its own entry",
			"def pick(a, f, g, b):\n    if a <= 0:\n        return b\n\n    return f(a) + g(a) + pick(a - 1, f, g, b)\n\nprint(pick(2, lambda v: v, lambda v: v * 2, 100))\n",
			"pick", []int{1, 2}, []int{0, 1}, 1, 1,
		},
		{
			// the same body read twice with different callables: two sites, two tuples, two clones —
			// and the recursion inside each clone is that clone's own
			"two call sites asking two different entries",
			"def total(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + total(n - 1, f)\n\ndef neg(v):\n    return 0 - v\n\nprint(total(3, neg), total(2, lambda v: v))\n",
			"total", []int{1}, []int{0}, 2, 2,
		},
		{
			// two hooks that happen to be the same function coalesce into one clone: the tuple is the
			// identity that matters, not the number of times it was written
			"two called parameters that are asked to be the same pair of functions",
			"def pick(a, f, g, b):\n    if a <= 0:\n        return b\n\n    return f(a) + g(a) + pick(a - 1, f, g, b)\n\nprint(pick(2, lambda v: v, lambda v: v, 100))\n",
			"pick", []int{1, 2}, []int{0, 1}, 1, 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := callablePlanOf(t, tc.src)
			var cf *callableFn
			for _, c := range plan.order {
				if c.def.Name == tc.fn {
					cf = c
				}
			}
			if cf == nil {
				t.Fatalf("%q is not in the plan", tc.fn)
			}
			if !sameInts(cf.calledPos, tc.calledPos) {
				t.Fatalf("%q calls parameters %v, want %v", tc.fn, cf.calledPos, tc.calledPos)
			}
			if len(cf.inner) != len(tc.slots) {
				t.Fatalf("%q records %d calls of its parameters, want %d", tc.fn, len(cf.inner), len(tc.slots))
			}
			for k, in := range cf.inner {
				if in.slot != tc.slots[k] {
					t.Fatalf("%q: the %dth call of a parameter reads entry %d, want %d (pos %d)",
						tc.fn, k, in.slot, tc.slots[k], in.pos)
				}
				if in.slot >= len(cf.clones[0].key) {
					t.Fatalf("%q: entry %d is past the clone's tuple of %d", tc.fn, in.slot, len(cf.clones[0].key))
				}
			}
			if len(cf.sites) != tc.sites {
				t.Fatalf("%q reads %d call sites, want %d", tc.fn, len(cf.sites), tc.sites)
			}
			if len(cf.clones) != tc.clones {
				t.Fatalf("%q minted %d clones for its %d site(s), want %d", tc.fn, len(cf.clones), len(cf.sites), tc.clones)
			}
		})
	}
}

// TestThePlanRecordsTheRecursionAsASiteOfTheClone asks the plan the question the rewrite is written
// against: which calls of a body are the recursion, and does every clone answer each of them.
func TestThePlanRecordsTheRecursionAsASiteOfTheClone(t *testing.T) {
	t.Parallel()
	src := "def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(4, lambda x: x))\n"
	plan := callablePlanOf(t, src)
	var cf *callableFn
	for _, c := range plan.order {
		if c.def.Name == "fact" {
			cf = c
		}
	}
	if cf == nil {
		t.Fatal("fact is not in the plan at all")
	}
	if len(cf.self) != 1 {
		t.Fatalf("want one recursive call recorded, got %d", len(cf.self))
	}
	if len(cf.clones) != 1 {
		t.Fatalf("want one clone, got %d", len(cf.clones))
	}
	rep := callableReportOf(t, src)
	if rep.RecursiveSites != len(cf.clones)*len(cf.self) {
		t.Fatalf("the account reports %d recursive sites for %d clones x %d calls",
			rep.RecursiveSites, len(cf.clones), len(cf.self))
	}
}

// ---------------------------------------------------------------------------
// The machine path: what the pass followed, readable without reading the pass.
// ---------------------------------------------------------------------------

// TestThePassAccountsForTheRecursionInJSON is the agentic contract for this row: `--callables` says
// how many recursive calls the clones answer, and it says so for every program in the answer table.
func TestThePassAccountsForTheRecursionInJSON(t *testing.T) {
	t.Parallel()
	src := "def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(4, lambda x: x))\n"
	doc, err := CallablesJSON(src)
	if err != nil {
		t.Fatalf("the account of a valid program must print, not fail: %v", err)
	}
	var rep struct {
		Rewritten      bool     `json:"rewritten"`
		Clones         int      `json:"clones"`
		RecursiveSites int      `json:"recursive_sites"`
		Refused        []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, doc)
	}
	if !rep.Rewritten || len(rep.Refused) != 0 {
		t.Fatalf("the row's own program did not rewrite: %s", doc)
	}
	if rep.Clones != 1 || rep.RecursiveSites != 1 {
		t.Fatalf("want one clone answering one recursive call, got %d and %d:\n%s", rep.Clones, rep.RecursiveSites, doc)
	}

	// The two-entry program: each of its two clones answers the recursion once.
	two := "def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\ndef dbl(x):\n    return x * 2\n\nprint(fact(3, dbl), fact(3, lambda v: v))\n"
	if got := callableReportOf(t, two); got.RecursiveSites != 2 || got.Clones != 2 {
		t.Fatalf("two clones should answer the recursion twice, account = %+v", got)
	}

	// A program with no recursion says so, rather than staying silent on the field.
	plain := "def apply1(fn, v):\n    return fn(v)\n\ndef inc(x):\n    return x + 1\n\nprint(apply1(inc, 41))\n"
	if got := callableReportOf(t, plain); got.RecursiveSites != 0 {
		t.Fatalf("a higher-order body with no recursion reported recursive_sites = %d", got.RecursiveSites)
	}

	// A decline names the recursion, so a reader is told which shape stopped the rewrite.
	mutual := "def a(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + b(n - 1, f)\n\ndef b(n, f):\n    if n <= 0:\n        return 0\n\n    return f(n) + a(n - 1, f)\n\ndef inc(x):\n    return x\n\nprint(a(3, inc))\n"
	doc, err = CallablesJSON(mutual)
	if err != nil {
		t.Fatalf("a decline is an answer, not a tool failure: %v", err)
	}
	if !strings.Contains(doc, `"rewritten": false`) || !strings.Contains(doc, "recursion") || !strings.Contains(doc, "roadmap L11.7") {
		t.Fatalf("the decline does not say which recursion, with its row:\n%s", doc)
	}
}

// TestTheRecursionRewritesTwiceToTheSameBytes is the determinism claim: the record, the lit tests and
// every cached artifact read the IR the pass mints, and a recursion whose clone names moved between
// runs would invalidate all three.
func TestTheRecursionRewritesTwiceToTheSameBytes(t *testing.T) {
	t.Parallel()
	for _, tc := range recursiveAnswered {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := tc.src + "\n"
			if first, second := compileSrc(t, src), compileSrc(t, src); first != second {
				t.Fatalf("the rewrite of %q is not deterministic", tc.name)
			}
		})
	}
}

// TestNoRecursionReachesACallThroughACastHandle is ADR 0345's ban, re-asked of the new road: the
// recursion is a `call` to a `define`, never a handle turned into an integer and back.
func TestNoRecursionReachesACallThroughACastHandle(t *testing.T) {
	t.Parallel()
	for _, tc := range recursiveAnswered {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if mod := compileSrc(t, tc.src+"\n"); strings.Contains(mod, "ptrtoint") || strings.Contains(mod, "inttoptr") {
				t.Fatalf("%q reaches the recursion through a cast handle:\n%s", tc.name, mod)
			}
		})
	}
}

// TestTheRecursionNamesWhatTheProgramNamed keeps ADR 0345 §3's promise for a clone that recurses: a
// traceback and the line table read the name the programmer typed, not the symbol the pass minted.
func TestTheRecursionNamesWhatTheProgramNamed(t *testing.T) {
	t.Parallel()
	src := "def fact(n, f):\n    if n <= 1:\n        return 1\n\n    return f(n) * fact(n - 1, f)\n\nprint(fact(4, lambda x: x))\n"
	_, dbg := debugModuleOf(t, src, &DebugOptions{})
	sawClone := false
	for _, f := range dbg.Functions {
		if !strings.Contains(f.Symbol, callableCloneInfix) {
			continue
		}
		sawClone = true
		if f.Name != "fact" {
			t.Fatalf("the recursive clone %q is reported as %q, want the source name", f.Symbol, f.Name)
		}
	}
	if !sawClone {
		t.Fatal("no recursive clone reached the debug table")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// callablePlanOf asks the pass for its plan, failing the case rather than returning a nil the caller
// would have to reason about: these rows are about what the plan decided, before any IR exists.
func callablePlanOf(t *testing.T, src string) *callablePlan {
	t.Helper()
	prog, err := parseProgram(src)
	if err != nil {
		t.Fatalf("the program does not parse: %v", err)
	}
	plan, perr := planDefunctionalization(prog)
	if perr != nil {
		t.Fatalf("the pass declined a program the table answers: %v", perr)
	}
	return plan
}

func callableReportOf(t *testing.T, src string) callableReport {
	t.Helper()
	_, rep := applyDefunctionalization(mustParseForCallables(t, src))
	if rep == nil {
		t.Fatalf("the pass withdrew a rewrite the table answers")
	}
	return *rep
}

func callableAccount(t *testing.T, src string) string {
	t.Helper()
	doc, err := CallablesJSON(src)
	if err != nil {
		t.Fatalf("the account failed to print: %v", err)
	}
	return doc
}

func mustParseForCallables(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := parseProgram(src)
	if err != nil {
		t.Fatalf("the program does not parse: %v", err)
	}
	return prog
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
