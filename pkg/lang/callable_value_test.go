// L11.7 (b): a function used as a value is a value this backend answers.
//
// The pass in callable_value.go decides that question before the doors that
// decide a call are asked. These tests walk the shapes the old refusal named —
// "def f(): return 1 / print(f(2) + (lambda x: x)(3))" among them — and judge
// each on BOTH witness legs, plus the shape of the IR the rewrite must produce.
package lang

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// callablePrograms is the L11.7 probe set. Every row's `want` is what CPython 3
// prints for the same source, written down rather than read from the record, so
// a row cannot pass by agreeing with the thing it is being checked against; the
// `record` field is filled in only for the rows the retired engine left an
// answer for, and the record-leg test asks the table for those.
func callablePrograms() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"lambda called immediately", "print((lambda x: x)(3))\n", "3\n"},
		{"lambda nested in a lambda", "print((lambda x: x)((lambda y: y * 2)(3)))\n", "6\n"},
		{"lambda bound to a name", "f = lambda x: x * 3\nprint(f(2))\n", "6\n"},
		{"lambda through a parameter", "def twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(twice(lambda x: x * 3, 2))\n", "12\n"},
		{"lambda in one of three arguments", "def call3(f, a, b, c):\n    return f(a) + f(b) + f(c)\n\nprint(call3(lambda x: x * 2, 1, 2, 3))\n", "12\n"},
		{"lambda as a predicate", "def pick(fn, xs):\n    out = []\n    for x in xs:\n        if fn(x):\n            out.append(x)\n    return out\n\nprint(pick(lambda x: x > 1, [1, 2, 3]))\n", "[2, 3]\n"},
		{"declared function through a parameter", "def inc(x: int) -> int:\n    return x + 1\n\ndef apply1(fn, v):\n    return fn(v)\n\nprint(apply1(inc, 41))\n", "42\n"},
		{"declared function of one argument", "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x: int) -> int:\n    return x * 2\n\nprint(twice(d2, 3))\n", "12\n"},
		{"same entry at two arguments", "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x: int) -> int:\n    return x * 2\n\nprint(twice(d2, 3), twice(d2, 5))\n", "12 20\n"},
		{"function called through an alias", "def d2(x: int) -> int:\n    return x * 2\n\ng = d2\nprint(g(4))\n", "8\n"},
		{"alias called twice", "def d2(x: int) -> int:\n    return x * 2\n\ng = d2\nprint(g(1), g(2))\n", "2 4\n"},
		{"alias of an annotated function", "def inc(x: int) -> int:\n    return x + 1\n\nk = inc\nprint(k(41))\n", "42\n"},
		{"module-level lambda called by its name", "h = lambda x: x + 1\nprint(h(2))\n", "3\n"},
		{"builtin hook argument", "print(sorted([3, 1, 2], key=lambda x: -x))\n", "[3, 2, 1]\n"},
		{"builtin hook argument for max", "print(max([1, 3, 2], key=lambda x: -x))\n", "1\n"},
		{"builtin hook argument for min", "print(min([1, 3, 2], key=lambda x: -x))\n", "3\n"},
		{"builtin hook given a declared function", "def neg(x):\n    return -x\n\nprint(sorted([3, 1, 2], key=neg))\n", "[3, 2, 1]\n"},
	}
}

// compiledRunIsTheBackend runs a program on the one backend there is. It is not
// captureStdout: that helper answers from the record when the record has the
// source, which would let a probe pass without the compiler being asked
// anything (ADR 0302's trap, and the reason the record is a witness and not an
// engine).
func compiledRunIsTheBackend(t *testing.T, src string) string {
	t.Helper()
	out, err := RunSource(src)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the pass claims to answer (%v):\n%s", err, src)
	}
	return out
}

// TestThePassAnswerssWhatTheReferencePrints is the reference leg, with CPython's
// text written into the table above.
func TestThePassAnswerssWhatTheReferencePrints(t *testing.T) {
	for _, tc := range callablePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if got := compiledRunIsTheBackend(t, tc.src); got != tc.want {
				t.Fatalf("compiled=%q reference=%q", got, tc.want)
			}
		})
	}
}

// TestThePassAnswerssWhatTheRecordRecorded is the record leg. Sources the retired
// engine never saw are skipped, not deleted: the reference leg above is what
// judges them.
func TestThePassAnswerssWhatTheRecordRecorded(t *testing.T) {
	asked := 0
	for _, tc := range callablePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Skipf("the retired engine left no record of this program")
			}
			asked++
			want := goldenStdout(silentReporter{}, tc.src)
			if got := compiledRunIsTheBackend(t, tc.src); got != want {
				t.Fatalf("compiled=%q record=%q", got, want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("the probe set matches no record row at all: the record leg is not being walked")
	}
}

// TestThePassRefusessNothingItClaimsToAnswer is ADR 0273's rule applied to this
// file's own table: a row that answers on the reference leg may not come back as
// a refusal.
func TestThePassRefusessNothingItClaimsToAnswer(t *testing.T) {
	for _, tc := range callablePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("refused a program the table claims: %s", msg)
			}
		})
	}
}

// compiledRefusalOf asks whether the backend refuses a program, without failing
// the test either way: the two refusal tables need the sentence, the answer
// tables need its absence, and neither may decide the other's outcome.
func compiledRefusalOf(src string) string {
	_, err := Compile(src)
	if err != nil {
		return err.Error()
	}
	return ""
}

// ---------------------------------------------------------------------------
// What the pass cannot prove is handed back untouched, so the older honest
// refusal stays the answer (ADR 0273, ADR 0211) — never a number.
// ---------------------------------------------------------------------------

func TestThePassLeavesWhatItCannotProveToTheOlderRefusals(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"declared function printed", "def d2(x: int) -> int:\n    return x * 2\n\nprint(d2)\n",
			"names function, which the compiled backend has no value for"},
		{"alias read as a value", "def d2(x: int) -> int:\n    return x * 2\n\ng = d2\nprint(g)\n",
			"names function, which the compiled backend has no value for"},
		{"alias rebound to a number", "def d2(x: int) -> int:\n    return x * 2\n\ng = d2\nprint(g(1))\ng = 3\nprint(g)\n",
			"names function, which the compiled backend has no value for"},
		{"function stored in a container", "def d2(x: int) -> int:\n    return x * 2\n\nxs = [d2]\nprint(len(xs))\n",
			"names function, which the compiled backend has no value for"},
		{"lambda that reads its enclosing variable", "def outer(v):\n    f = lambda x: x + v\n    return f(1)\n\nprint(outer(1))\n",
			`undefined name "v"`},
		{"lambda that calls its own parameter", "print((lambda f: f(3))(lambda x: x * 2))\n",
			"is called but is not a function this backend can build a call to"},
		{"lambda returned from a function", "def picker():\n    return lambda x: x + 1\n\nprint(picker()(2))\n",
			"a lambda used as a value has no compiled representation"},
		{"entry of the wrong arity", "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef add2(a, b):\n    return a + b\n\nprint(twice(add2, 1))\n",
			"is called but is not a function this backend can build a call to"},
		{"higher-order function as an entry", "def apply1(fn, v):\n    return fn(v)\n\ndef twice2(fn, v):\n    return fn(fn(v))\n\nprint(twice2(apply1, 3))\n",
			"is called but is not a function this backend can build a call to"},
		{"zero-argument call with an argument", "def f():\n    return 1\n\nprint(f(2) + (lambda x: x)(3))\n",
			"too many arguments for f"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal = %q, want it to mention %q", msg, tc.want)
			}
			// A refusal must never be dressed up as an answer: the row is refused, so the
			// program prints nothing (ADR 0273).
			if strings.Contains(msg, "compiled clean") {
				t.Fatalf("contradiction in the harness: %q", msg)
			}
		})
	}
}

// Where the pass declines, the sentence the backend already had is still the answer,
// and it still names the parameter the program's own body used (ADR 0345 §3).
func TestThePassNamesWhatTheProgramNamedInRefusalsItLeaves(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"indirect call through a param", "def apply1(fn, v):\n    return fn(v)\n\nclass Foo:\n    def bar(self, x):\n        return x\n\nprint(apply1(Foo().bar, 1))\n", `"fn"`},
		{"bound method argument", "def call_it(fn, v):\n    return fn(v)\n\nclass Foo:\n    def bar(self, x):\n        return x\n\nprint(call_it(Foo().bar, 5))\n", `"fn"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := compiledRefusalOf(tc.src)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal = %q, want it to name %q", msg, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The IR shape the rewrite owes.
// ---------------------------------------------------------------------------

func TestThePassRewritesValuesIntoNamedDefsAndRetiresTheOriginals(t *testing.T) {
	src := "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x: int) -> int:\n    return x * 2\n\nprint(twice(d2, 3))\n"
	mod := compileSrc(t, src)
	if !strings.Contains(mod, "define i32 @gy_twice"+callableCloneInfix) {
		t.Fatalf("no specialised def was emitted:\n%s", mod)
	}
	if strings.Contains(mod, "define i32 @gy_twice(") {
		t.Fatalf("the unspecialised def is still emitted:\n%s", mod)
	}
	if !regexp.MustCompile(`call i32 @gy_d2\(i32 %`).MatchString(mod) {
		t.Fatalf("the clone does not call the entry directly:\n%s", mod)
	}
	for _, junk := range []string{"ptrtoint", "inttoptr"} {
		if strings.Contains(mod, junk) {
			t.Fatalf("module contains the junk shape %q:\n%s", junk, mod)
		}
	}
}

func TestThePassEmitsARealDefForAHoistedHookArgument(t *testing.T) {
	src := "print(sorted([3, 1, 2], key=lambda x: -x))\n"
	mod := compileSrc(t, src)
	if !strings.Contains(mod, "define i32 @gy_sorted"+callableLambdaInfix) {
		t.Fatalf("the hook lambda was not hoisted into a def:\n%s", mod)
	}
	if !regexp.MustCompile(`call i32 @gy_sorted\$lambda\$[0-9a-f]+_\d+\(i32 `).MatchString(mod) {
		t.Fatalf("no call into the hoisted hook def:\n%s", mod)
	}
}

// A clone is the function the program wrote, seen twice: the debug table reports the
// source name and the symbol table reports the minted one, so a traceback from a clone
// reads as `twice` and not as a name no programmer typed (ADR 0345 §3).
func TestThePassNamesGeneratedDefsByWhatWasWritten(t *testing.T) {
	src := "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x: int) -> int:\n    return x * 2\n\nprint(twice(d2, 3))\n"
	_, dbg := debugModuleOf(t, src, &DebugOptions{})
	var names, symbols []string
	for _, f := range dbg.Functions {
		names = append(names, f.Name)
		symbols = append(symbols, f.Symbol)
	}
	sawClone := false
	for i, sym := range symbols {
		if !strings.Contains(sym, callableCloneInfix) {
			continue
		}
		sawClone = true
		if names[i] != "twice" {
			t.Fatalf("clone %q is reported as %q, want the source name %q", sym, names[i], "twice")
		}
	}
	if !sawClone {
		t.Fatalf("no clone reached the debug table: symbols=%v", symbols)
	}
}

// A rewrite is a compile-time function of the source: two runs must hand back the
// same bytes, or every cached artifact, lit test and golden row becomes a coin.
func TestThePassRewritessTwiceToTheSameBytes(t *testing.T) {
	for _, tc := range callablePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if first, second := compileSrc(t, tc.src), compileSrc(t, tc.src); first != second {
				t.Fatalf("the rewrite is not deterministic for %q", tc.name)
			}
		})
	}
}

// Nothing in the pass may answer a call by handing a heap handle to a function: that
// cast is the shape the bug had before the rewrite.
func TestThePassNeverHandsAHeapHandleToACall(t *testing.T) {
	for _, tc := range callablePrograms() {
		t.Run(tc.name, func(t *testing.T) {
			if mod := compileSrc(t, tc.src); strings.Contains(mod, "ptrtoint") || strings.Contains(mod, "inttoptr") {
				t.Fatalf("%q reaches a call through a cast handle:\n%s", tc.name, mod)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The machine path: what the pass decided has to be readable without reading
// the pass (ADR 0345's agentic rule, ADR 0006's exit-code contract).
// ---------------------------------------------------------------------------

func TestThePassAccountssForItselfInJSON(t *testing.T) {
	rewritten := "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x: int) -> int:\n    return x * 2\n\nprint(twice(d2, 3), twice(d2, 5))\nprint(sorted([3, 1, 2], key=lambda x: 0 - x))\n"
	doc, err := CallablesJSON(rewritten)
	if err != nil {
		t.Fatalf("the account of a valid program must print, not fail: %v", err)
	}
	var rep struct {
		Tool             string   `json:"tool"`
		Pass             string   `json:"pass"`
		Hash             string   `json:"hash"`
		Rewritten        bool     `json:"rewritten"`
		Clones           int      `json:"clones"`
		HoistedLambdas   int      `json:"hoisted_lambdas"`
		RewrittenCallNum int      `json:"rewritten_call_sites"`
		HigherOrderFunc  []string `json:"higher_order_functions"`
		Symbols          []string `json:"symbols"`
		Refused          []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, doc)
	}
	if rep.Tool != callableToolName || rep.Pass != "defunctionalize-callables" {
		t.Fatalf("the document does not name its tool and pass: %q / %q", rep.Tool, rep.Pass)
	}
	if !rep.Rewritten || rep.Clones == 0 || rep.HoistedLambdas == 0 || rep.RewrittenCallNum == 0 {
		t.Fatalf("the rewrite is not accounted for: %+v", rep)
	}
	if len(rep.HigherOrderFunc) != 1 || rep.HigherOrderFunc[0] != "twice" {
		t.Fatalf("higher_order_functions = %v, want [twice]", rep.HigherOrderFunc)
	}
	if len(rep.Symbols) != rep.Clones+rep.HoistedLambdas {
		t.Fatalf("%d symbols for %d clones and %d hoisted lambdas", len(rep.Symbols), rep.Clones, rep.HoistedLambdas)
	}
	for _, sym := range rep.Symbols {
		if !strings.Contains(sym, callableCloneInfix) && !strings.Contains(sym, callableLambdaInfix) {
			t.Fatalf("symbol %q is not one this pass minted", sym)
		}
	}
	if len(rep.Refused) != 0 {
		t.Fatalf("a rewritten program reports refusals: %v", rep.Refused)
	}
	if rep.Hash == "none" || rep.Hash == "" {
		t.Fatalf("a rewritten program has no hash: %q", rep.Hash)
	}

	// The same program twice: the hash is the pass's own determinism claim.
	again, err := CallablesJSON(rewritten)
	if err != nil || again != doc {
		t.Fatalf("the account is not reproducible: %v", err)
	}

	// A program with nothing to rewrite declines, and says so in the document rather than failing.
	declined, err := CallablesJSON("print(1)\n")
	if err != nil {
		t.Fatalf("a decline is an answer, not a tool failure: %v", err)
	}
	if !strings.Contains(declined, `"rewritten": false`) || !strings.Contains(declined, "roadmap L11.7") {
		t.Fatalf("the decline does not say why, with its row:\n%s", declined)
	}

	// A source that is not gusty is the tool being handed junk: an error, not a document.
	if doc, err := CallablesJSON("def ("); err == nil {
		t.Fatalf("an unparseable source printed a document instead of failing: %s", doc)
	}
}
