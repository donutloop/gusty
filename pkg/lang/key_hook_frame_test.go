package lang

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// key_hook_frame_test.go — the ordering door's hook lambda is minted WITH the words it reads out of the
// frame its call is written in (roadmap L11.7 · Gap R.376; ADR 0396).
//
// ADR 0340 gave `sorted`/`min`/`max` a key door that calls the key once per element through the ordinary
// call road; ADR 0345 gave a `key=lambda …` hook a declaration of its own so that door had a name to call,
// on the condition — stated in the road's own comment — that "the lambda must read nothing but its own
// parameters". A hook that read a name of the frame around the call therefore refused:
//
//	def use():
//	    base = 1
//	    return sorted([3, 1, 2], key=lambda x: 0 - x + base)   # the reference's [3, 2, 1]
//
//	print(use())
//
// while the ENTRY road — the callable the CALLEE calls, ADR 0395 — had just been paid for exactly that
// shape. The two roads were built a cycle apart and never met: `callableLambdaCaptures` (ADR 0370) is the
// one question "what does this body read out of the frame it was written in", and the application road and
// the entry road asked it while the hook road asked a spelling test instead.
//
// The fix is to ask the door, not to write a third spelling test (ADR 0232, and ADR 0391/0392/0394's found
// disease): the hook is hoisted with one capture parameter per name the frame owns, carrying `Captures` so
// the EXISTING call road fills it from the caller's own reading — the caller of a hook being the frame the
// `sorted` call is written in, which is the frame that owns the name. ADR 0345's rule holds all the way
// down: the hook stays a named `define`, and every module here is free of `fnptr`, `ptrtoint`, `inttoptr`
// and an indirect `call`.
//
// One arity census had to learn the difference between an argument and a word: `keyDoorCallsOneArgument`
// and the key door's own arity verdict counted EVERY parameter of the minted declaration, so a key the
// reference calls with one element was declared to be missing one required positional argument. Both now
// count `callableSuppliedParams` — the parameters the source supplies — which is one question with one
// asking place rather than a condition bolted onto each door.
//
// What the road declines is pinned below with today's answer beside the reference's, and each names the row
// that owns the missing half: a name of a frame two steps out (ADR 0245's nested-`def` lift), a capture the
// one-word value cannot carry (Gap R.332), a name the MODULE binds that the module rebinds (Gap R.35), a
// hook that CALLS a declared function, and a hook written inside a `lambda` (the closure road's frame).
//
// Every `want` below is CPython's own text — generated from `python3` when this file was written, not
// transcribed — and `integration/key_hook_frame_test.go` re-asks the reference at run time.

// hookFrameAnswered is the answer table: a hook lambda that reads the frame its keyed call is written in.
var hookFrameAnswered = []struct{ name, src, want string }{
	{
		name: "a hook reads a local of the frame the call is written in",
		src:  "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the same hook spelled at module level over a module name",
		src:  "base = 1\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a hook reads a constant the module declares",
		src:  "LIMIT = 10\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a hook reads a constant from inside a function frame",
		src:  "LIMIT = 10\ndef use():\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))\n\nuse()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the frame's own parameter, and the frame runs twice",
		src:  "def use(seed):\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + seed))\n\nuse(1)\nuse(4)\n",
		want: "[3, 2, 1]\n[3, 2, 1]",
	},
	{
		name: "min and max ask the one hook the same question",
		src:  "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base), max([1, 2, 3], key=lambda v: 0 - v + base))\n\nuse()\n",
		want: "[3, 2, 1] 1",
	},
	{
		name: "min and max over written-out candidates",
		src:  "def use():\n    base = 1\n    print(min([3, 1, 2], key=lambda x: 0 - x + base), max([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
		want: "3 1",
	},
	{
		name: "reverse=True counts the other side of the same key",
		src:  "def use():\n    base = 1\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base, reverse=True))\n\nuse()\n",
		want: "['a', 'cc', 'bbb']",
	},
	{
		name: "the answer is a list like any other: bound, measured, indexed",
		src:  "def use():\n    base = 1\n    xs = sorted([3, 1, 2], key=lambda x: 0 - x + base)\n    print(xs, len(xs), xs[0], xs[2])\n\nuse()\n",
		want: "[3, 2, 1] 3 3 1",
	},
	{
		name: "the keyed answer is walkable",
		src:  "def use(step):\n    for v in sorted([3, 1, 2], key=lambda x: 0 - x + step):\n        print(v)\n\nuse(1)\n",
		want: "3\n2\n1",
	},
	{
		name: "two hooks of one frame",
		src:  "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n    print(sorted([5, 4], key=lambda y: 0 - y + base))\n\nuse()\n",
		want: "[3, 2, 1]\n[5, 4]",
	},
	{
		name: "a hook in a while body, and the frame changes under it",
		src:  "def use():\n    base = 1\n    n = 0\n    while n < 2:\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base + n))\n        n = n + 1\n\nuse()\n",
		want: "[3, 2, 1]\n[3, 2, 1]",
	},
	{
		name: "a rebinding between the two hooks moves both answers",
		src:  "def use():\n    base = 1\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base))\n    base = 5\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base))\n\nuse()\n",
		want: "['bbb', 'cc', 'a']\n['bbb', 'cc', 'a']",
	},
	{
		name: "a hook that reads two names of its frame",
		src:  "def use():\n    base = 1\n    other = 2\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base + other))\n\nuse()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a hook inside a method's frame",
		src:  "class C:\n    def method(self):\n        base = 1\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nC().method()\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the lambda's own parameter is the frame's name: no word is minted",
		src:  "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda base: 0 - base + base))\n\nuse()\n",
		want: "[3, 1, 2]",
	},
	{
		name: "a hook in a for body inside a def",
		src:  "def use():\n    for step in [1, 2]:\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + step))\n\nuse()\n",
		want: "[3, 2, 1]\n[3, 2, 1]",
	},
	{
		name: "the self-contained hook keeps the one word it always had",
		src:  "print(sorted([3, 1, 2], key=lambda x: 0 - x))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "a builtin key",
		src:  "print(sorted([\"bbb\", \"a\"], key=len))\n",
		want: "['a', 'bbb']",
	},
	{
		name: "a named key",
		src:  "def neg(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=neg))\n",
		want: "[3, 2, 1]",
	},
	{
		name: "the program's own sorted is not the builtin",
		src:  "def sorted(xs):\n    return [1]\nprint(sorted([3, 2]))\n",
		want: "[1]",
	},
	{
		name: "the key's body answers a plain call too",
		src:  "def k(s, base):\n    return 0 - len(s) + base\n\nprint(k(\"ab\", 1), k(\"abcd\", 1))\n",
		want: "-1 -3",
	},
	{
		name: "an int key that reads the frame orders numbers",
		src:  "def use():\n    base = 1\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base))\n\nuse()\n",
		want: "['bbb', 'cc', 'a']",
	},
}

// TestAHookThatReadsItsFrameAnswersWhatTheReferenceAnswers is the compiled leg: the module the compiler
// writes runs the hook, and the JIT prints what CPython prints.
func TestAHookThatReadsItsFrameAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range hookFrameAnswered {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference prints %q", tt.src, got, tt.want)
			}
			if strings.TrimSpace(got) != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, strings.TrimSpace(got), tt.want)
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestAHookThatReadsItsFrameIsWhatTheRecordSaid is the record leg over the same sources. A source this row
// first asked was recorded from the reference by tools/recmerge; a record that is missing fails the row
// rather than passing by having nothing to compare against (AGENTS.md's record leg).
func TestAHookThatReadsItsFrameIsWhatTheRecordSaid(t *testing.T) {
	t.Parallel()
	asked := 0
	for _, tt := range hookFrameAnswered {
		t.Run(tt.name, func(t *testing.T) {
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tt.src)
			}
			asked++
			want := strings.TrimSpace(goldenStdout(silentReporter{}, tt.src))
			if want != tt.want {
				t.Fatalf("the record says %q, this table says %q — one of them is wrong", want, tt.want)
			}
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the record prints %q", tt.src, got, want)
			}
			if strings.TrimSpace(got) != want {
				t.Fatalf("%q printed %q, the record printed %q", tt.src, strings.TrimSpace(got), want)
			}
		})
	}
	if asked == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestWhatTheHookRoadDeclinesIsDeclinedOutLoud pins the shapes this row leaves where they were, with the
// sentence the program hears and the row that owns the missing half. None of them became a wrong answer at
// the exit code of success, and none of them reaches exit 2 (ADR 0166).
func TestWhatTheHookRoadDeclinesIsDeclinedOutLoud(t *testing.T) {
	t.Parallel()
	// Three of these refusals MOVED under this row: the hook is hoisted now, so the program reaches the door
	// that actually owns the missing half and hears its sentence (a text/container capture hears Gap R.332's
	// one-word capture rule, float elements hear Gap R.248's, and a module-rebound name hears Gap R.35's) instead
	// of the key door's generic "a lambda is a function VALUE". Every one stays in the refusal class at exit 1 —
	// which is what is asserted — and none of them became an answer, because the reference's program needs the
	// word those rows owe. The rest were, and stay, the door's own sentence.
	for _, tc := range []struct {
		name  string
		src   string
		names string
		owns  string
	}{
		{
			// a name two frames out: the program's `base` belongs to `outer`, and the only lift that
			// reaches a frame a call does not name is ADR 0245's nested-`def` one
			name:  "a hook that reads a frame two steps out",
			src:   "def outer():\n    base = 2\n    def use():\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\n    use()\n\nouter()\n",
			names: "a lambda is a function VALUE",
			owns:  "L11.7",
		},
		{
			// the door's own one-word capture rule (Gap R.332), reached now that the hook is hoisted:
			// a text cannot cross as one i32, and the refusal says so instead of printing a table index
			name:  "a capture whose value is a text",
			src:   "def use():\n    label = \"x\"\n    print(sorted([\"ab\", \"b\"], key=lambda s: s + label))\n\nuse()\n",
			names: "holds a text",
			owns:  "L11.1",
		},
		{
			// the same rule for a container, asked of the hook road for the first time
			name:  "a capture whose value is a container",
			src:   "def use():\n    bag = [1, 2]\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + bag))\n\nuse()\n",
			names: "holds a container",
			owns:  "L11.1",
		},
		{
			// a module-level `for` target is module state the module rebinds: the hoisted hook reads it
			// as a parameter, and the CALLER'S reading is the module's own changing slot (Gap R.35). The
			// applied-lambda road has always refused this shape with the same sentence — measured.
			name:  "a module frame whose name the module rebinds",
			src:   "for step in [1, 2]:\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + step))\n",
			names: "bound at module level",
			owns:  "Gap R.35",
		},
		{
			// the hook CALLS a declared function: `helper` is a symbol, not a value, and this backend
			// has no word for a function value (Gap R.377 — the road declines rather than read `%_helper`)
			name:  "a hook that calls a function the module declares",
			src:   "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
			names: "a lambda is a function VALUE",
			owns:  "L11.7",
		},
		{
			// a hook written inside a `lambda` is the closure road's frame, and this pass does not
			// rewrite through that boundary (ADR 0345)
			name:  "a hook written inside a lambda body",
			src:   "def use():\n    base = 1\n    print((lambda xs: sorted(xs, key=lambda x: 0 - x + base))([3, 1, 2]))\n\nuse()\n",
			names: "a lambda is a function VALUE",
			owns:  "L11.7",
		},
		{
			// the option the door does NOT call: minting a word for a `reverse=` lambda would mint a
			// word no call fills
			name:  "a lambda in the option slot the door never calls",
			src:   "def use():\n    base = 1\n    print(sorted([3, 1, 2], reverse=lambda x: x + base))\n\nuse()\n",
			names: "needs a constant flag",
			owns:  "L11.7",
		},
		{
			// float ELEMENTS are the door's own refusal (Gap R.248), reached for the first time by a
			// hook that got hoisted: the answer is the payload or a module `llc` rejects
			name:  "float elements under a hook that reads its frame",
			src:   "def use():\n    base = 1\n    print(sorted([3.5, 1.5, 2.0], key=lambda x: 0 - x + base))\n\nuse()\n",
			names: "Gap R.248",
			owns:  "L11.1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tc.names)
			}
			if !strings.Contains(msg, tc.names) {
				t.Fatalf("refusal = %q, want it to mention %q", msg, tc.names)
			}
			if !strings.Contains(msg, tc.owns) && !strings.Contains(msg, "roadmap") {
				t.Fatalf("refusal %q does not point at a row (%s)", msg, tc.owns)
			}
			// No compiler's own machinery leaking into the sentence the door writes (ADR 0166). Until
			// **Gap R.377** / **ADR 0398** this list had to carve out the statement wrapper — the test stripped
			// `a *lang.ExprStmt statement cannot be compiled:` off the front so that its own leak-forbidden
			// check could pass, which is a leak becoming the test's problem instead of the compiler's. The
			// carve-out is gone and the assertion is the other way round: the wrapper may NOT appear.
			said := msg
			for _, leak := range []string{"*lang.", "0x", "%!s(", "%c4"} {
				if strings.Contains(said, leak) {
					t.Fatalf("the refusal leaks %q: %s", leak, msg)
				}
			}
			if strings.Contains(said, "statement cannot be compiled") {
				t.Fatalf("the refusal still arrives inside the statement wrapper (Gap R.377): %s", msg)
			}
		})
	}
}

// TestTheHookIsMintedWithTheWordsItReads is the pass's own account, asked of the plan rather than of the
// printed bytes: `capture_parameters` counts the words the hook road wrote onto the declaration it minted,
// and a hook that reads nothing writes none (the account a reader of `--callables` is owed).
func TestTheHookIsMintedWithTheWordsItReads(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		captures int
		hoisted  int
	}{
		{
			name:     "a hook that reads one name of its frame gains one word",
			src:      "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
			captures: 1, hoisted: 1,
		},
		{
			name:     "a hook that reads two names gains two words",
			src:      "def use():\n    base = 1\n    other = 2\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base + other))\n\nuse()\n",
			captures: 2, hoisted: 1,
		},
		{
			name:     "the frame's parameter is a word like any other",
			src:      "def use(seed):\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + seed))\n\nuse(1)\n",
			captures: 1, hoisted: 1,
		},
		{
			// the module's name is read by the hoisted `def` directly — ADR 0395's rule, asked of this
			// road now: a hoisted def reads a global as well as the lambda did, so no word is minted
			name:     "a module name mints no word at all",
			src:      "LIMIT = 10\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))\n",
			captures: 0, hoisted: 1,
		},
		{
			name:     "the self-contained hook still mints nothing beside its parameter",
			src:      "print(sorted([3, 1, 2], key=lambda x: 0 - x))\n",
			captures: 0, hoisted: 1,
		},
		{
			// Two stages, two claims (ADR 0395's lesson): the pass DID mint the word — the frame owns
			// `label`, so the door says one word is needed — and the EMITTER is the stage that refuses,
			// because the value behind the name is a text and one i32 cannot say so (Gap R.332). The
			// account reports the pass; only the second verdict reaches `--file`.
			name:     "the account reports the minting the emitter later refuses",
			src:      "def use():\n    label = \"x\"\n    print(sorted([\"ab\", \"b\"], key=lambda s: s + label))\n\nuse()\n",
			captures: 1, hoisted: 1,
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

// TestTheHookDoorCountsOnlyTheArgumentsTheSourceSupplies asks the arity census DIRECTLY, which is the shape
// that fails when someone widens or narrows it: the minted hook has two parameters and supplies ONE
// argument, and a door that counts the minted word answers "that key is never called" — which the pass then
// reads as a function value left in the program and refuses the whole program for.
func TestTheHookDoorCountsOnlyTheArgumentsTheSourceSupplies(t *testing.T) {
	t.Parallel()
	const src = "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	prog = defunctionalizeCallables(prog)
	hook := 0
	for _, st := range prog.Stmts {
		fd, isDef := st.(*FuncDef)
		if !isDef || !strings.Contains(fd.Name, callableLambdaInfix) {
			continue
		}
		hook++
		if len(fd.Captures) != 1 || fd.Captures[0] != "base" {
			t.Fatalf("the hook carries %v, want the one word \"base\" the frame owns", fd.Captures)
		}
		if len(fd.Params) != 2 {
			t.Fatalf("the hook was minted with %d parameters, want the lambda's own beside one capture word", len(fd.Params))
		}
		if got := len(callableSuppliedParams(fd)); got != 1 {
			t.Fatalf("the source supplies %d arguments to the hook, want 1 — the key door calls it with one element", got)
		}
		if !keyDoorCallsOneArgument(fd) {
			t.Fatal("keyDoorCallsOneArgument says the ordering door never calls this key, so the pass would refuse the program for keeping a function value")
		}
		// and the census is still a door, not a broom: a key that really does need two arguments is
		// still the reference's TypeError, capture words or no capture words
		two := &FuncDef{Name: fd.Name + "2", Params: []*Param{{Name: "a"}, {Name: "b"}}, Captures: fd.Captures}
		if keyDoorCallsOneArgument(two) {
			t.Fatal("a key that cannot take the one element the door passes is a call, which is what the reference raises for")
		}
	}
	if hook == 0 {
		t.Fatal("no hook lambda was minted for this program: the road regressed and the census is not being asked")
	}
	// a self-contained hook has no words to exclude, and the census answers the ordinary way
	plain, err := Parse("print(sorted([3, 1, 2], key=lambda x: 0 - x))\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	plain = defunctionalizeCallables(plain)
	for _, st := range plain.Stmts {
		fd, isDef := st.(*FuncDef)
		if !isDef || !strings.Contains(fd.Name, callableLambdaInfix) {
			continue
		}
		if len(fd.Captures) != 0 {
			t.Fatalf("the self-contained hook gained capture parameters: %v", fd.Captures)
		}
		if !keyDoorCallsOneArgument(fd) {
			t.Fatal("the self-contained hook the door has always called is no longer called")
		}
	}
}

// TestTheHookIsCalledByNameAndNotByAValue reads the module: the hook is a `define` with the element and the
// words it reads, the call carries the HOST'S own reading of the captured name, and the residue table the
// whole L11.7 row lives by stays empty. The IR claims say whose body they read — the caller's `@gy_use`
// holds the call, the minted hook holds the parameters (ADR 0394's lesson).
func TestTheHookIsCalledByNameAndNotByAValue(t *testing.T) {
	t.Parallel()
	const src = "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n"
	mod := compileSrc(t, src)
	hookHead := regexp.MustCompile(`(?m)^define i32 @gy_sorted` + regexp.QuoteMeta(callableLambdaInfix) + `[^(]*\(i32 %p0, i32 %p1\) \{$`)
	if !hookHead.MatchString(mod) {
		t.Fatalf("the hook is not a two-word define:\n%s", mod)
	}
	caller := functionBodyOf(mod, "gy_use")
	if !strings.Contains(caller, "call i32 @gy_sorted"+callableLambdaInfix) {
		t.Fatalf("the caller does not call the hook by name:\n%s", caller)
	}
	if !regexp.MustCompile(`call i32 @gy_sorted` + regexp.QuoteMeta(callableLambdaInfix) + `[^(]*\(i32 [^,]+, i32 %_base\.ld[0-9]+\)`).MatchString(caller) {
		t.Fatalf("the hook call does not carry the host's own reading of %q:\n%s", "base", caller)
	}
	hook := regexp.MustCompile(`(?ms)^define i32 @gy_sorted\$lambda\$[^{]*\{.*?^}`)
	bodies := hook.FindAllString(mod, -1)
	if len(bodies) == 0 {
		t.Fatal("no minted hook define in the module")
	}
	if !strings.Contains(bodies[0], "store i32 %p1, i32* %_param1") {
		t.Fatalf("the forwarded word is not stored into the callee's slot:\n%s", bodies[0])
	}
	for _, forbidden := range []string{"fnptr", "ptrtoint", "inttoptr", "indirect"} {
		if strings.Contains(mod, forbidden) {
			t.Fatalf("the module contains %q — ADR 0345's rule (a function is declared, not a value):\n%s", forbidden, mod)
		}
	}
	if regexp.MustCompile(`call [^\n]*\(i32 %[^)]*\) to i32`).MatchString(mod) {
		t.Fatalf("the module makes an indirect call:\n%s", mod)
	}
	if again := compileSrc(t, src); again != mod {
		t.Fatal("two compiles of one source disagree — a minted name is not a function of the program")
	}
}
