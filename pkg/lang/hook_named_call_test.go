package lang

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// hook_named_call_test.go — a `key=lambda` hook that CALLS a function the module declares gets that call
// BY NAME (roadmap L11.7 · **Gap R.378**, ADR 0399).
//
// The shape the filing puts at the top of the row:
//
//	def helper(v):
//	    return 0 - v
//
//	print(sorted([3, 1, 2], key=lambda x: helper(x)))   # the reference's [3, 2, 1]
//
// The hook is hoisted to a `define` of its own, and the copied body calls the declared function the way
// every other body in the module does — `call i32 @gy_helper(i32 %p0)` against the symbol the emitter
// declares (see `TestTheHookCallsTheDeclaredFunctionBySymbol` below for the bytes). No word is minted for
// `helper`, because a function in this backend is a symbol rather than a value (ADR 0345 §1).
//
// What the row changed is ONE question. `callableLambdaCaptures` asks what a hoisted body reads out of the
// frame it was written in, and a hook road asked it and then refused the hoist if any answer named a
// declared `def` or `class`. That sentence was about the wrong thing: a body may not hold a function as a
// VALUE — the `i32`-word world of ADR 0345 §1 has no bits for one — but a body may certainly CALL one,
// because a hoisted `def` is itself a top-level `define` and calls `helper` through the same symbol an
// ordinary call in any other function body resolves through. `callableAppliedLambda` and `entryLambda` had
// the same words and no such gate, which is why the applied road called declared functions all along.
//
// The gate now asks three questions per free name, all of them about the PROGRAM:
//
//   - is the name a top-level `def`/`class` the module DECLARES (`callableModuleDeclarations`)?
//   - does the name appear anywhere in the body OUTSIDE a call position (`callableFreeNameSet` with the
//     callee nodes skipped) — read bare, printed, stored, handed to another call? then it is being read as
//     a value and the road declines, because that is the half ADR 0345 has no word for;
//   - does the frame the call is written in OWN the name (`callableFrameOwns`)? a frame binding shadows the
//     module symbol, and the frame's value — which may be an `int` the reference would call and get a
//     TypeError from — is the one that answers.
//
// The ordering rule the front end already enforces of an ordinary call is kept, and kept CONSERVATIVE: a
// declaration appearing at or below the hook's own line, or a module that rebinds the name anywhere, keeps
// the refusal. That direction matters — a hook hoisted above its `def`, or beside a rebind, would print the
// declared function's answer where the reference raises (`helper` is a *slot* by then, not a function), so
// the conservative answer is the only one that cannot fabricate. `TestWhatTheNamedCallHookRefusesToAnswer`
// pins each of those with the reference's answer beside it, including the three where the reference answers
// and we still decline.
//
// Every `want` below is CPython's own text — generated from `python3` for these exact sources when this file
// was written, not transcribed — and every source is on the record leg (`tools/recmerge`, which files an
// entry only where the reference and the compiled leg agree). `integration/hook_named_call_test.go` re-asks
// the reference at run time, and `integration/programs/hook_named_call.gy` is the conformance program.

// hookNamedCallAnswered is the family: a hook that CALLS something the module declares, across the frames
// the call can be written in and the values the callee can take and return.
var hookNamedCallAnswered = []struct{ name, src, want string }{
	{
		// the filing's own shape
		name: "a hook that calls a function the module declares",
		src:  "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the same hook on the other side of the order, as `min`",
		src:  "def helper(v):\n    return 0 - v\n\nprint(min([3, 1, 2], key=lambda x: helper(x)))\n",
		want: "3",
	},
	{
		name: "and as `max`",
		src:  "def helper(v):\n    return 0 - v\n\nprint(max([3, 1, 2], key=lambda x: helper(x)))\n",
		want: "1",
	},
	{
		// the two stages in one body: the callee goes by symbol, `base` is a word — ADR 0396's fix holds
		name: "a hook that calls a function and reads a local of its own frame",
		src:  "def helper(v):\n    return 0 - v\n\ndef use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: helper(x) + base))\n\nuse()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a callee that reads a module name of its own",
		src:  "OFF = 1\n\ndef helper(v):\n    return 0 - v + OFF\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a callee whose second argument comes from the body",
		src:  "def helper(v, k):\n    return 0 - v + k\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x, 1)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a callee with a default the hook does not supply",
		src:  "def helper(v, k=1):\n    return 0 - v + k\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a callee called with a NAMED argument",
		src:  "def helper(v, k=1):\n    return 0 - v + k\n\nprint(sorted([3, 1, 2], key=lambda x: helper(v=x)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the same call over texts",
		src:  "def helper(s):\n    return 0 - len(s)\n\nprint(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: helper(s)))\n",
		want: "['bbb', 'cc', 'a']",
	},
	{
		name: "a callee that is a declared CLASS",
		src:  "class Box:\n    def __init__(self, v):\n        self.v = v\n\nprint(sorted([3, 1, 2], key=lambda x: Box(x).v))\n",
		want: "[1, 2, 3]",
	},
	{
		name: "a callee whose answer comes from a branch",
		src:  "def sign(v):\n    if v > 0:\n        return 1\n    return 0 - 1\n\nprint(sorted([3, -1, 2], key=lambda x: sign(x)))\n",
		want: "[-1, 3, 2]",
	},
	{
		name: "a hook written in a method's frame",
		src:  "def helper(v):\n    return 0 - v\n\nclass C:\n    def m(self):\n        print(sorted([3, 1, 2], key=lambda x: helper(x)))\n\nC().m()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "two hooks of one frame, both calling",
		src:  "def helper(v):\n    return 0 - v\n\ndef use():\n    print(sorted([3, 1, 2], key=lambda x: helper(x)))\n    print(sorted([5, 4], key=lambda y: helper(y)))\n\nuse()\n",
		want: "[3, 2, 1]\n[5, 4]",
	},
	{
		name: "one hook called twice over two containers",
		src:  "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\nprint(sorted([7, 8], key=lambda x: helper(x)))\n",
		want: "[3, 2, 1]\n[8, 7]",
	},
	{
		name: "two calls of one function from one hook",
		src:  "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x) + helper(0)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a hook in a while body",
		src:  "def helper(v):\n    return 0 - v\n\nn = 0\nwhile n < 2:\n    print(sorted([3, 1, 2], key=lambda x: helper(x) + n))\n    n = n + 1\n",
		want: "[3, 2, 1]\n[3, 2, 1]",
	},
	{
		name: "the answer bound, measured and walked",
		src:  "def helper(v):\n    return 0 - v\n\nxs = sorted([3, 1, 2], key=lambda x: helper(x))\nprint(xs, len(xs), xs[0], xs[2])\n",
		want: "[3, 2, 1] 3 3 1",
	},
	{
		// the callee is a declared function too, and the chain resolves through symbols
		name: "a hook that calls a function which calls a function",
		src:  "def twice(v):\n    return helper(v) + helper(v)\n\ndef helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: twice(x)))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a builtin called beside the declared one",
		src:  "def helper(v):\n    return 0 - len(v)\n\nprint(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: helper(s) + len(s)))\n",
		want: "['bbb', 'a', 'cc']",
	},
	{
		name: "a comprehension inside the hook beside the call",
		src:  "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x) + len([q for q in [1, 2]])))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the comparison of two keyed calls",
		src:  "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)) == sorted([9, 8], key=lambda y: helper(y)))\n",
		want: "False",
	},
	// the controls: the self-contained hook, a frame-reading hook, a module-name hook, a `def` handed to
	// the door as the key itself, a builtin key, and the applied/entry roads. These answered BEFORE the row
	// and had better still answer.
	{
		name: "the self-contained hook",
		src:  "print(sorted([3, 1, 2], key=lambda x: 0 - x))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the hook that only reads its frame",
		src:  "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the hook that only reads a module name",
		src:  "base = 1\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the named key — a `def` handed to the door as the key itself",
		src:  "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=helper))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the builtin key",
		src:  "print(sorted([\"bbb\", \"a\"], key=len))\n",
		want: "['a', 'bbb']",
	},
	{
		name: "the applied body calling a declared function",
		src:  "def helper(v):\n    return 0 - v\n\nprint((lambda x: helper(x) + 5)(3))\n",
		want: "2",
	},
	{
		name: "the higher-order entry calling a declared function",
		src:  "def helper(v):\n    return 0 - v\n\ndef use(fn):\n    return fn(1)\n\nprint(use(lambda v: helper(v)))\n",
		want: "-1",
	},
}

// TestAHookThatCallsADeclaredFunctionAnswersWhatTheReferenceAnswers is the compiled leg of the family: the
// program COMPILES, the hoisted hook runs, and the bytes are the reference's. A refusal here is a failure —
// the row's whole claim is that this shape is no longer refused.
func TestAHookThatCallsADeclaredFunctionAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range hookNamedCallAnswered {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("the compiled leg refused a program the reference answers (%q): %s", tt.want, got)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("compiled leg printed\n%s\nwant\n%s", strings.TrimSpace(got), tt.want)
			}
		})
	}
}

// TestAHookThatCallsADeclaredFunctionIsWhatTheRecordSaid is the record leg. An absent entry FAILS: the
// family's answers were recorded from the reference with the compiled leg cross-checked (`tools/recmerge`
// files an entry only where the two agree), so a family member that quietly left the record is coverage
// deleted rather than coverage that was never there.
func TestAHookThatCallsADeclaredFunctionIsWhatTheRecordSaid(t *testing.T) {
	t.Parallel()
	for _, tt := range hookNamedCallAnswered {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("the family is on the record — `go run -tags=llvm20 ./tools/recmerge -sources <file>` this source\n%s", tt.src)
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record could not answer: %v", err)
			}
			if strings.TrimSpace(rec) != tt.want {
				t.Fatalf("the record said\n%s\nthis table says\n%s", strings.TrimSpace(rec), tt.want)
			}
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("the compiled leg refused a program the record answers (%q): %s", tt.want, got)
			}
			if strings.TrimSpace(got) != strings.TrimSpace(rec) {
				t.Fatalf("the compiled leg printed\n%s\nthe record said\n%s", strings.TrimSpace(got), strings.TrimSpace(rec))
			}
		})
	}
}

// TestWhatTheNamedCallHookRefusesToAnswer is the gate's other half, and the half that keeps the row honest:
// a program that still refuses is NOT a wrong answer. Each row states what the reference does — including
// the three where the reference answers a number and we still decline — and the roadmap row that owns the
// missing half.
func TestWhatTheNamedCallHookRefusesToAnswer(t *testing.T) {
	t.Parallel()
	for _, tc := range hookNamedCallDeclineTable {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := compiledRefusalOf(tc.src)
			if said == "" {
				t.Fatalf("the program compiled clean; this shape is expected to refuse\n%s", tc.src)
			}
			if !strings.Contains(said, tc.names) {
				t.Fatalf("the refusal does not say %q:\n%s", tc.names, said)
			}
			if !strings.Contains(said, tc.owns) && !strings.Contains(said, "roadmap") && !strings.Contains(said, "ADR ") {
				t.Fatalf("the refusal names neither %q nor a roadmap row:\n%s", tc.owns, said)
			}
			// a shape the language refuses has no answer to record — the recorder's own `-check` rule,
			// asked of the table so a refusal cannot quietly acquire an entry and become self-certifying
			if HasGoldenAnswer(tc.src) {
				t.Fatalf("a refused shape is on the record, which the recorder refuses to file:\n%s", tc.src)
			}
			// the machine path. The key road's own sentence is NOT in the capability table today — the rows
			// below carry whatever code the table DOES answer, and an empty `code` means the table answers
			// nothing. Pinning that costs nothing and catches the drift that matters: a refusal an agent
			// branches on cannot move, gain or lose a code without this table being read (ADR 0346).
			kind, ok := ClassifyCapabilityRefusal(said)
			if kind == nil {
				ok = false
			}
			got := ""
			if ok {
				got = kind.Code
			}
			if got != tc.code {
				t.Fatalf("the refusal classifies as %q, want %q:\n%s", got, tc.code, said)
			}
			// and it stays the program's own sentence
			for _, bad := range []string{"*lang.", "0x", "%!s(", "%c4", "statement cannot be compiled"} {
				if strings.Contains(said, bad) {
					t.Fatalf("the refusal carries the compiler's own machinery (%q):\n%s", bad, said)
				}
			}
		})
	}
}

// TestTheHookCallsTheDeclaredFunctionBySymbol is the IR witness for the row's claim: the hoisted hook is a
// `define` of its own, it takes ONE parameter — the element — and no word for the callee, and its body calls
// the declared function through the symbol the emitter declares. `fnptr`/`ptrtoint`/`inttoptr`/`indirect`
// are absent because they have been absent since ADR 0345.
func TestTheHookCallsTheDeclaredFunctionBySymbol(t *testing.T) {
	t.Parallel()
	const src = "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled leg refused (%v): %s", err, src)
	}
	ir := res.IR

	hook := regexp.MustCompile(`(?m)^define [^(]*@gy_[^(]*\$lambda\$[^(]*\(([^)]*)\)`)
	m := hook.FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("no hoisted `$lambda$` define in the module:\n%s", firstDefines(ir))
	}
	if strings.Contains(m[1], ",") {
		t.Fatalf("the hoisted hook takes a word beside its element — %q — which is ADR 0345 §1's bits coming back", m[1])
	}

	if !regexp.MustCompile(`(?m)^define i32 @gy_helper\(i32 %\w+\)`).MatchString(ir) {
		t.Fatalf("the module's `def helper` is not declared as its own define:\n%s", firstDefines(ir))
	}
	if !strings.Contains(ir, "call i32 @gy_helper(") {
		t.Fatalf("nothing calls the declared function by symbol:\n%s", firstDefines(ir))
	}
	for _, bad := range []string{"fnptr", "ptrtoint", "inttoptr", "indirect call", "call <null>"} {
		if strings.Contains(ir, bad) {
			t.Fatalf("the module contains %q — a function value came back (ADR 0345 §1):\n%s", bad, firstDefines(ir))
		}
	}
}

// TestTheNamedCallHookMintsNoWordForTheCallee is `--callables`' account of the same fact: the pass hoists
// the hook and writes NO capture parameter for a callee it can reach by symbol — while the hook that also
// reads a name of its frame still writes exactly one. That distinction is the row: a call mints nothing, a
// read mints a word.
func TestTheNamedCallHookMintsNoWordForTheCallee(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		hoisted  int
		captures int
	}{
		{
			name:    "a hook that only calls a declared function mints no word",
			src:     "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
			hoisted: 1, captures: 0,
		},
		{
			name:    "a hook that calls a declared CLASS mints no word",
			src:     "class Box:\n    def __init__(self, v):\n        self.v = v\n\nprint(sorted([3, 1, 2], key=lambda x: Box(x).v))\n",
			hoisted: 1, captures: 0,
		},
		{
			name:    "a hook that calls AND reads one name of its frame mints exactly one word",
			src:     "def helper(v):\n    return 0 - v\n\ndef use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: helper(x) + base))\n\nuse()\n",
			hoisted: 1, captures: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := CallablesJSON(tc.src)
			if err != nil {
				t.Fatalf("--callables: %v", err)
			}
			var rep struct {
				Rewritten         bool     `json:"rewritten"`
				HoistedLambdas    int      `json:"hoisted_lambdas"`
				CaptureParameters int      `json:"capture_parameters"`
				Refused           []string `json:"refused"`
			}
			if err := json.Unmarshal([]byte(out), &rep); err != nil {
				t.Fatalf("the account is not JSON: %v\n%s", err, out)
			}
			if !rep.Rewritten {
				t.Fatalf("the pass reports no rewrite; refused: %v", rep.Refused)
			}
			if rep.HoistedLambdas != tc.hoisted {
				t.Fatalf("hoisted_lambdas = %d, want %d (%s)", rep.HoistedLambdas, tc.hoisted, out)
			}
			if rep.CaptureParameters != tc.captures {
				t.Fatalf("capture_parameters = %d, want %d (%s)", rep.CaptureParameters, tc.captures, out)
			}
		})
	}
}

// TestTheNamedCallHookFamilyIsHonestAboutItsOwnCounts is this file's own accounting: the three tables have
// the sizes the row's evidence quotes, they do not overlap, and every family member is on the record.
func TestTheNamedCallHookFamilyIsHonestAboutItsOwnCounts(t *testing.T) {
	t.Parallel()
	if len(hookNamedCallAnswered) != 28 {
		t.Errorf("the family has %d entries, want 28 — the row's evidence and this table are read together; change both or neither", len(hookNamedCallAnswered))
	}
	seen := map[string]string{}
	for _, tt := range hookNamedCallAnswered {
		if prev, ok := seen[tt.src]; ok {
			t.Errorf("%q and %q are the same program", prev, tt.name)
		}
		seen[tt.src] = tt.name
	}
	refused := 0
	for _, tc := range hookNamedCallDeclineTable {
		if _, ok := seen[tc.src]; ok {
			t.Errorf("%q is in BOTH the family and the refusal table", tc.name)
		}
		seen[tc.src] = tc.name
		if strings.HasPrefix(tc.ref, "raises") {
			refused++
		}
	}
	if refused != 6 {
		t.Errorf("the refusal table holds %d programs the reference itself refuses, want 6", refused)
	}
}

// hookNamedCallDeclineTable is the refusal table, in one place so the count guard here and the integration
// file's CLI leg read the same rows rather than a copy of them. `ref` is what CPython does with the program
// — its bytes, or how it raises — recorded from `python3` for these exact sources.
var hookNamedCallDeclineTable = []struct{ name, src, ref, names, owns, code string }{
	// the ordering rule the front end already enforces of an ordinary call, kept CONSERVATIVE: a hook
	// hoisted above its `def` would call a symbol whose slot is not the function yet
	{
		name:  "a hook that calls a function declared FURTHER DOWN the module than the hook",
		src:   "print(sorted([3, 1, 2], key=lambda x: helper(x)))\n\ndef helper(v):\n    return 0 - v\n",
		ref:   "raises NameError: name 'helper' is not defined",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	// the frame owns the name: `helper` is a slot holding a number, and the reference's TypeError is the
	// answer that shape has
	{
		name:  "a frame that shadows the declared function with a local of the same name",
		src:   "def helper(v):\n    return 0 - v\n\ndef use():\n    helper = 3\n    print(sorted([3, 1, 2], key=lambda x: helper(x)))\n\nuse()\n",
		ref:   "raises TypeError: 'int' object is not callable",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	{
		name:  "a module that rebinds the declared function before the call",
		src:   "def helper(v):\n    return 0 - v\n\nhelper = 3\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		ref:   "raises TypeError: 'int' object is not callable",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	// the CONSERVATIVE direction of the ordering rule: the rebind is BELOW the call, where the reference is
	// past it by the time the hook runs — declining is the only answer that cannot fabricate a number
	{
		name:  "a module that rebinds the declared function AFTER the call",
		src:   "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\nhelper = 3\n",
		ref:   "[3, 2, 1]",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	// a name assigned inside a module-level block is still the module's slot
	{
		name:  "a rebind of the declared function inside a module-level `if`",
		src:   "def helper(v):\n    return 0 - v\n\nflag = 0\nif flag == 0:\n    helper = 3\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		ref:   "raises TypeError: 'int' object is not callable",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	{
		name:  "a rebind of the declared function inside a module-level `for`",
		src:   "def helper(v):\n    return 0 - v\n\nfor t in [3]:\n    helper = t\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		ref:   "raises TypeError: 'int' object is not callable",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	// the value half: an ALIAS holds the function, and the only thing that could travel to the hoisted hook
	// is a word for it — which ADR 0345 §1 has no bits for (the row that owns it is Gap R.297)
	{
		name:  "a hook that calls through a name the module bound to the function",
		src:   "def helper(v):\n    return 0 - v\n\nalias = helper\nprint(sorted([3, 1, 2], key=lambda x: alias(x)))\n",
		ref:   "[3, 2, 1]",
		names: "is called but is not a function this backend can build a call to",
		owns:  "Gap R.297",
		code:  "lower.unsupported.indirect-call",
	},
	// the value half, said by the hook road itself: the body READS the name, in `str(helper)`
	{
		name:  "a hook that reads the function as a VALUE beside its call",
		src:   "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x) + len(str(helper))))\n",
		ref:   "[3, 2, 1]",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	// the value half, handed to another call
	{
		name:  "a hook that hands the function to another function",
		src:   "def helper(v):\n    return 0 - v\n\ndef apply1(fn, v):\n    return fn(v)\n\nprint(sorted([3, 1, 2], key=lambda x: apply1(helper, x)))\n",
		ref:   "[3, 2, 1]",
		names: "a lambda is a function VALUE",
		owns:  "L11.7",
	},
	// the applied road keeps its own verdict: reading the function as the RESULT of an applied body is a
	// value read, and the reference prints an address
	{
		name:  "an applied body that reads the function as a value",
		src:   "def helper(v):\n    return v + 1\n\nprint((lambda x: helper)(3))\n",
		ref:   "<function helper at 0x…>",
		names: "names function, which the compiled backend has no value for",
		owns:  "L11.7",
		code:  "lower.unsupported.function-read-as-value",
	},
	// the callee is a NAME the module never declared: not a symbol, so no call to build
	{
		name:  "a hook that calls a name the module never declares",
		src:   "print(sorted([3, 1, 2], key=lambda x: not_here(x)))\n",
		ref:   "raises NameError: name 'not_here' is not defined",
		names: "is called but is not a function this backend can build a call to",
		owns:  "L11.7",
		code:  "lower.unsupported.indirect-call",
	},
	// a `def` in a conditional block is not a top-level declaration, so there is no symbol whose definition
	// dominates every use
	{
		name:  "a hook that calls a function declared inside a module-level `if`",
		src:   "flag = 1\nif flag == 1:\n    def helper(v):\n        return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
		ref:   "[3, 2, 1]",
		names: "is called but is not a function this backend can build a call to",
		owns:  "L11.7",
		code:  "lower.unsupported.indirect-call",
	},
}

// TestWhatTheRowLeavesStandingIsFiledNotFixed is the mission's rule about known limitations: the shapes the
// row did NOT take are named here with the answer the reference gives, so a reader of this file can tell a
// refusal from a wrong answer without running anything. Two of them are the emitter's doors elsewhere in
// the module — the same family of raw sentences Gap R.377 closed for statements, which Gap R.379 owns.
func TestWhatTheNamedCallRowLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		says  string // what the refusal must at least say
		owned string // the row that owns it
	}{
		{
			// an attribute of a module-bound object, read by the hook: the expression road's door, with
			// the same two defects Gap R.377 closed for statements — no line, no roadmap row
			name:  "a hook that reads an attribute of a module-bound object",
			src:   "class C:\n    def __init__(self):\n        self.k = 1\n\nc = C()\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + c.k))\n",
			says:  "unsupported attr expression",
			owned: "Gap R.379",
		},
		{
			// the callee reads module state that changes; the refusal names the name and the rule, and it
			// is the SAME sentence on the build before this row
			name:  "a callee that mutates a module-level container",
			src:   "TRIED = [0]\n\ndef note(v):\n    TRIED[0] = TRIED[0] + 1\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: note(x)))\nprint(TRIED[0])\n",
			says:  "is bound at module level",
			owned: "Gap R.379",
		},
		{
			// a method of a module-bound object as the callee: the hook's call target is not a NAME, so
			// there is no symbol to call and the module-state door answers
			name:  "a hook that calls a method of a module-bound object",
			src:   "class C:\n    def __init__(self):\n        self.k = 1\n\n    def m(self, v):\n        return 0 - v\n\nc = C()\nprint(sorted([3, 1, 2], key=lambda x: c.m(x)))\n",
			says:  "is bound at module level",
			owned: "Gap R.379",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := compiledRefusalOf(tc.src)
			if said == "" {
				t.Fatalf("the program compiled; this shape is left standing on purpose and its answer must be re-checked against the reference\n%s", tc.src)
			}
			if !strings.Contains(said, tc.says) {
				t.Fatalf("the refusal no longer says %q — the row it is filed under changed, update this table and %s\n%s", tc.says, tc.owned, said)
			}
		})
	}
}

// firstDefines quotes the module's function definitions, which is the part of the IR a reader needs to
// judge a "it is a define, not a value" claim; the runtime's own helpers would bury it.
func firstDefines(ir string) string {
	var b strings.Builder
	for _, line := range strings.Split(ir, "\n") {
		if strings.HasPrefix(line, "define ") || strings.HasPrefix(line, "declare ") {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	if b.Len() == 0 {
		return ir
	}
	return b.String()
}
