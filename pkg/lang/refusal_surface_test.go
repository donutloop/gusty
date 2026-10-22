package lang

// What a refusal about a STATEMENT is allowed to say (roadmap L11.7 · **Gap R.377**, closed by
// **ADR 0398**).
//
// The shape that was refused is not a program with a bug in it:
//
//	def outer():
//	    base = 2
//	    def use():
//	        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))
//	    return use()
//
//	outer()                                  # CPython prints [3, 2, 1]
//
// Before this row the compiled leg answered:
//
//	codegen: a *lang.ExprStmt statement cannot be compiled: `key=lambda ...` is refused: a lambda is a
//	function VALUE, and this backend has none — … (roadmap L11.7)
//
// The half after the colon is the door's own sentence and says everything a reader can act on. The half
// before it names a Go type the reader does not have, and it is the reason the sibling row's two tests
// had to STRIP a prefix before they could assert the sentence was clean — which is what a leak looks like
// once a suite has learned to work around it. The two carve-outs (`pkg/lang/key_hook_frame_test.go`,
// `integration/key_hook_frame_test.go`) are deleted, and this file asserts the absence instead.
//
// Three claims, in order of how hard they were to get right:
//
//  1. a refusal the backend wrote on purpose reaches the reader UNWRAPPED, with the frame and the line
//     appended — the only facts the wrapper knew that the payload did not;
//  2. a payload that is a raw Go error still gets a sentence about where it came from, and that sentence
//     names the KEYWORD and the LINE, never the Go type — so the wrapper arm cannot regress either;
//  3. the `*FuncDef` arm is untouched: it is Gap R.38's, it names the function the program wrote, and the
//     drift ledger quotes its sentence, so a "cleanup" that folded it into the passthrough would be a
//     ledger row silently disappearing — the failure mode this suite fears most.

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// refusalSurfacePrograms is the family: every arm of `noteUnlowered`'s `Stmt` case that a program can
// actually reach, taken from a closure body (the road that funnels statement errors through it). `said`
// is the DOOR's own sentence — the substring that must survive — and `frame`/`line` are where the program
// wrote the statement that refused.
var refusalSurfacePrograms = []struct {
	name  string
	src   string
	said  string
	frame string
	line  int
	owns  string
}{
	{
		// The probe the sibling row filed this one for: the hook door's sentence, buried under a Go type.
		name:  "a key hook inside a nested def reads the frame two steps out",
		src:   "def outer():\n    base = 2\n    def use():\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n    return use()\n\nouter()\n",
		said:  "`key=lambda ...` is refused",
		frame: "use",
		line:  4,
		owns:  "L11.7",
	},
	{
		// The same door at the same arm, reached from a different statement kind: `print(helper)` is an
		// ExprStmt, and the road that refuses it is ADR 0283's function-is-no-value rule.
		name:  "a closure body reads a declared function as a value",
		src:   "def helper(v):\n    return v\n\ndef outer():\n    def use():\n        print(helper)\n    return use()\n\nouter()\n",
		said:  `"helper" names function, which the compiled backend has no value for`,
		frame: "use",
		line:  6,
		owns:  "Gap R.150",
	},
	{
		// A `return` whose call target is a number: the `ReturnStmt` arm.
		name:  "a closure body calls a number",
		src:   "def outer():\n    x = 3\n    def use():\n        return x(1)\n    return use()\n\nprint(outer())\n",
		said:  `"x" is called but is not a function this backend can build a call to`,
		frame: "use",
		line:  4,
		owns:  "L11.1",
	},
	{
		name:  "a closure body iterates a text in a comprehension",
		src:   "def outer():\n    def use():\n        return [c for c in 'abc']\n    return use()\n\nprint(outer())\n",
		said:  `a comprehension over "abc" is refused`,
		frame: "use",
		line:  3,
		owns:  "Gap I.2",
	},
	{
		// The `TryStmt` arm. The payload is a CAPABILITY refusal — one `--capabilities` classifies —
		// which is the registry's witness that a payload is a sentence the backend wrote on purpose.
		name:  "a try block in a closure body copies a container",
		src:   "def outer():\n    def use():\n        try:\n            print(list([1]))\n        except:\n            print(0)\n    return use()\n\nouter()\n",
		said:  "list(<container>) copies are not supported",
		frame: "use",
		line:  3,
		owns:  "L11.1",
	},
	{
		// The `WithStmt` arm.
		name:  "a with block on a number in a closure body",
		src:   "def outer():\n    def use():\n        with 3 as x:\n            print(x)\n    return use()\n\nouter()\n",
		said:  "string-method __enter__ on a receiver that is not a text the compiler can read",
		frame: "use",
		line:  3,
		owns:  "Gap I.2",
	},
	{
		// The `IfStmt` arm: the block's own line, not the emitter's type table.
		name:  "an if block in a closure body copies a container",
		src:   "def outer():\n    def use():\n        if 1 < 2:\n            print(list([1, 2]))\n    return use()\n\nouter()\n",
		said:  "list(<container>) copies are not supported",
		frame: "use",
		line:  3,
		owns:  "L11.1",
	},
	{
		// The `WhileStmt` arm, whose payload is classified by the capability registry.
		name:  "a while block in a closure body calls a number",
		src:   "def outer():\n    n = 0\n    def use():\n        while n < 1:\n            print(tuple(3))\n            n = 1\n    return use()\n\nouter()\n",
		said:  `"tuple" is called but is not a function this backend can build a call to`,
		frame: "use",
		line:  4,
		owns:  "L11.1",
	},
	{
		// The `MatchStmt` arm.
		name:  "a match arm in a closure body calls a number",
		src:   "def outer():\n    def use(v):\n        match v:\n            case 1:\n                print(tuple(2))\n    return use(1)\n\nouter()\n",
		said:  `"tuple" is called but is not a function this backend can build a call to`,
		frame: "use",
		line:  3,
		owns:  "L11.1",
	},
}

// TestWhatAStatementRefusalSays is the row's own claim, read off the compiled leg: the door's sentence
// arrives first and whole, the wrapper is gone, no Go type name appears anywhere in it, and the reader is
// told which frame and which line to go and look at.
func TestWhatAStatementRefusalSays(t *testing.T) {
	t.Parallel()
	for _, tc := range refusalSurfacePrograms {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the program compiled clean; want the refusal naming %q", tc.said)
			}
			if !strings.HasPrefix(msg, "codegen: "+tc.said) {
				t.Fatalf("the refusal does not OPEN with the door's own sentence %q:\n%s", tc.said, msg)
			}
			if strings.Contains(msg, "statement cannot be compiled") {
				t.Fatalf("the statement wrapper is back (Gap R.377):\n%s", msg)
			}
			for _, leak := range []string{"*lang.", "lang.", "0x", "%!s(", "%!d(", "%v"} {
				if strings.Contains(msg, leak) {
					t.Fatalf("the refusal leaks the compiler's own machinery (%q):\n%s", leak, msg)
				}
			}
			// Where, in the two words a reader acts on. The frame is the DISPLAY name: a closure the pass
			// minted is named by the host it came from, never by its IR symbol (ADR 0345 §3).
			want := "(in `" + tc.frame + "`, line " + strconv.Itoa(tc.line) + ")"
			if !strings.HasSuffix(msg, want) {
				t.Fatalf("refusal = %q, want it to end with the frame and line %q", msg, want)
			}
			// A refusal names the row that owns the missing half (ADR 0166) — in the prose, or, for a
			// CAPABILITY refusal, on the `capability` object `--json` carries (ADR 0346): the registry holds
			// `list(<container>) copies`'s rows, and its sentence is the same on every road precisely because
			// it is the registry's text rather than a per-site one.
			_, classified := ClassifyCapabilityRefusal(msg)
			if !classified && !strings.Contains(msg, "roadmap") && !strings.Contains(msg, "ADR ") && !strings.Contains(msg, "Gap ") {
				t.Fatalf("the refusal cites no row that owns the missing half, and the registry does not know it either (ADR 0166, ADR 0346):\n%s", msg)
			}
			if tc.owns != "" && !strings.Contains(msg, tc.owns) && !classified {
				t.Fatalf("refusal = %q, want it to point at %s", msg, tc.owns)
			}
		})
	}
}

// TestWhatTheClosureRoadPrintsForAPrimitivePayload is claim (2): when the payload is NOT one the backend
// wrote on purpose, the wrapper still speaks — and it speaks in the program's words. The probe is a list
// literal of functions, whose payload (`list literal elements must be integers, not fn(any)`) is an
// internal type-name sentence and correctly gets the location wrapper.
func TestWhatTheClosureRoadPrintsForAPrimitivePayload(t *testing.T) {
	t.Parallel()
	src := "def helper(v):\n    return v\n\ndef outer():\n    def use():\n        return [helper]\n    return use()\n\nprint(outer())\n"
	msg := compiledRefusalOf(src)
	if msg == "" {
		t.Fatal("the program compiled clean; want the wrapper's sentence about the `return`")
	}
	if !strings.Contains(msg, "the `return` at line 6 cannot be compiled:") {
		t.Fatalf("the wrapper does not name the statement the program wrote:\n%s", msg)
	}
	for _, leak := range []string{"*lang.", "lang."} {
		if strings.Contains(msg, leak) {
			t.Fatalf("the wrapper leaks the compiler's own machinery (%q):\n%s", leak, msg)
		}
	}
}

// TestTheFuncDefArmStillNamesTheFunction protects the arm this row did NOT touch. Its sentence is quoted
// by `pkg/lang/testdata/interpreter-golden-drift.json`, where a row that silently changed wording is a
// divergence that silently disappeared — the failure TestMain is built to catch.
func TestTheFuncDefArmStillNamesTheFunction(t *testing.T) {
	t.Parallel()
	src := "class C:\n    def m(self, x):\n        x = x + 0.5\n        return x\n\nc = C()\nprint(c.m(1.0))\n"
	msg := compiledRefusalOf(src)
	if !strings.HasPrefix(msg, `codegen: function "m" cannot be compiled: `) {
		t.Fatalf("the *FuncDef arm changed (Gap R.38's sentence, quoted by the drift ledger):\n%s", msg)
	}
	if strings.Contains(msg, "*lang.") || strings.Contains(msg, "statement cannot be compiled") {
		t.Fatalf("the *FuncDef arm leaks a Go type name:\n%s", msg)
	}
}

// TestWhatTheSurfaceDoorAnswersForEveryStatement is the unit leg: `stmtSurface` names each statement kind
// by the keyword the program wrote, with its line, and never with a Go type name — including the fallback,
// which is the arm most likely to regress because nothing in the language reaches it today.
func TestWhatTheSurfaceDoorAnswersForEveryStatement(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		st   Stmt
		want string
	}{
		{"the call statement", &ExprStmt{Expr: &Call{Fn: &Name{Value: "print"}}, Src: Span{Line: 7}}, "the `print(…)` call at line 7"},
		{"the method-call statement", &ExprStmt{Expr: &Call{Fn: &Attr{Obj: &Name{Value: "xs"}, Name: &Name{Value: "append"}}}, Src: Span{Line: 2}}, "the `.append(…)` call at line 2"},
		{"an assignment", &AssignStmt{Target: &Name{Value: "k"}, Value: &IntLit{Value: 1}, Src: Span{Line: 3}}, "the assignment to `k` at line 3"},
		{"an augmented assignment", &AugAssignStmt{Target: &Name{Value: "n"}, Op: "+", Src: Span{Line: 4}}, "the `n +=` assignment at line 4"},
		{"a return", &ReturnStmt{Expr: &IntLit{Value: 1}, Src: Span{Line: 5}}, "the `return` at line 5"},
		{"a raise", &RaiseStmt{Expr: &IntLit{Value: 1}, Src: Span{Line: 6}}, "the `raise` at line 6"},
		{"an if", &IfStmt{Src: Span{Line: 8}}, "the `if` at line 8"},
		{"a while", &WhileStmt{Src: Span{Line: 9}}, "the `while` at line 9"},
		{"a for", &ForStmt{Src: Span{Line: 10}}, "the `for` at line 10"},
		{"a try", &TryStmt{Src: Span{Line: 11}}, "the `try` at line 11"},
		{"a with", &WithStmt{Src: Span{Line: 12}}, "the `with` at line 12"},
		{"a match", &MatchStmt{Src: Span{Line: 13}}, "the `match` at line 13"},
		{"an import", &ImportStmt{Module: "math", Src: Span{Line: 14}}, "the `import` at line 14"},
		{"a def", &FuncDef{Name: "go", Src: Span{Line: 15}}, "the `def go` at line 15"},
		{"a class", &ClassDef{Name: "C", Src: Span{Line: 16}}, "the `class C` at line 16"},
		{"a break", &BreakStmt{Src: Span{Line: 17}}, "the `break` at line 17"},
		{"a continue", &ContinueStmt{Src: Span{Line: 18}}, "the `continue` at line 18"},
		{"a pass", &PassStmt{Src: Span{Line: 19}}, "the `pass` at line 19"},
		{"a yield", &YieldStmt{Src: Span{Line: 20}}, "the `yield` at line 20"},
		{"a yield from", &YieldFromStmt{Src: Span{Line: 21}}, "the `yield from` at line 21"},
		{"a type alias", &TypeAliasStmt{Name: "T", Src: Span{Line: 22}}, "the type alias at line 22"},
		{"an extern declaration", &ExternDecl{Name: "puts", Src: Span{Line: 23}}, "the `extern` declaration at line 23"},
		// The fallback, exercised by a statement kind the language has no spelling for: the answer is
		// still a line, because a line is actionable and `*lang.somethingStmt` is not.
		{"a statement kind the door has no arm for", oddStmt{Src: Span{Line: 41}}, "the statement at line 41"},
		// And the degenerate input, which must not panic and must not quote a type.
		{"no statement at all", nil, "the statement"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := stmtSurface(tc.st)
			if got != tc.want {
				t.Fatalf("stmtSurface = %q, want %q", got, tc.want)
			}
			if strings.Contains(got, "*lang.") || strings.Contains(got, "Stmt{") {
				t.Fatalf("the surface names the Go type: %q", got)
			}
		})
	}
}

// oddStmt is a statement kind the language cannot write, so that `stmtSurface`'s fallback arm has a test:
// a helper is only honest about its fallback if something exercises it.
type oddStmt struct{ Src Span }

func (oddStmt) stmtNode()    {}
func (n oddStmt) Span() Span { return n.Src }

// TestWhatRefusalIsSentenceAsks pins the door that decides whether a payload is wrapped at all. Yes
// answers the house rule's two marks — a roadmap citation, or a sentence the capability registry knows
// (the same classifier `--json` and `--capabilities` read, ADR 0346); no answers a raw error from a
// helper, which is precisely the payload the wrapper exists to introduce.
func TestWhatRefusalIsSentenceAsks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		msg  string
		want bool
	}{
		{"a door's sentence with its row", "`key=lambda ...` is refused: … (roadmap L11.7)", true},
		{"a door's sentence naming a gap", "x: the half that is missing (Gap R.332, ADR 0166)", true},
		{"a capability refusal the registry knows", "list(<container>) copies are not supported in the AOT backend yet; CPython answers them — build the container with list() and add elements", true},
		{"a helper's bare complaint", "list literal elements must be integers, not fn(any)", false},
		{"an empty error", "", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := refusalIsSentence(errors.New(tc.msg)); got != tc.want {
				t.Fatalf("refusalIsSentence(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
	if refusalIsSentence(nil) {
		t.Fatal("no error is never a sentence")
	}
	// The negative control the door is only worth having: a bare complaint DOES get the wrapper, and the
	// wrapper carries the statement's own name.
	g := &irGen{}
	g.noteUnlowered(&ReturnStmt{Expr: &IntLit{Value: 1}, Src: Span{Line: 4}}, errors.New("the elements of a list literal are not integers"))
	if g.emitErr == nil || !strings.Contains(g.emitErr.Error(), "the `return` at line 4 cannot be compiled: the elements of a list literal are not integers") {
		t.Fatalf("a raw payload lost its wrapper: %v", g.emitErr)
	}
}

// TestTheStatementRefusalFamilyIsOnTheRecord is the record leg. A program that refuses is a claim about
// the record too: the retired engine answered these, so a row that starts answering (or stops) is a
// divergence, and a MISSING record is this suite's favourite failure. The controls are the answered
// siblings of the family — the same roads, the same closures, with nothing refused — and each one is
// checked against the answer the record holds.
func TestTheStatementRefusalFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	controls := []struct {
		name string
		src  string
		std  string
	}{
		{
			"the nested def whose hook reads its OWN frame",
			"def use():\n    base = 2\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
			"[3, 2, 1]\n",
		},
		{
			"the nested def whose body calls a declared function",
			"def helper(v):\n    return v * 2\n\ndef outer():\n    def use():\n        print(helper(3))\n    return use()\n\nouter()\n",
			"6\n",
		},
		{
			"the closure body inside a with and a try",
			"def outer():\n    def use():\n        try:\n            print(len([1, 2, 3]))\n        except:\n            print(0)\n    return use()\n\nouter()\n",
			"3\n",
		},
		{
			"the match arm over a closure body",
			"def outer():\n    def use(v):\n        match v:\n            case 1:\n                print(len([1, 2]))\n            case _:\n                print(0)\n    return use(1)\n\nouter()\n",
			"2\n",
		},
	}
	for _, tc := range controls {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend\n%s", tc.name, tc.src)
			}
			std, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg refused the control: %v", err)
			}
			if std != tc.std {
				t.Fatalf("the record's stdout = %q, want %q", std, tc.std)
			}
		})
	}
}

// TestWhatTheRowLeavesStandingIsFiledNotFixed pins the sibling leaks this row does NOT close, with the row
// that owns each. They are the same disease at a different door — a Go type name in a sentence a human
// reads — and the list is here so that closing them is an edit to a table rather than a re-discovery.
//
// Three rows used to sit here — the item store's `got *lang.ListLit`, the assignment target's
// `unsupported assignment target *lang.Attr` and the parser's `(got *lang.IntLit)`. **Gap R.379** closed them
// with **ADR 0400**, and they now live in `pkg/lang/target_surface_test.go` as answered rows: the guard fired
// exactly as written ("move this row to DONE and delete the pin"), which is the whole point of pinning a leak
// rather than listing it.
func TestWhatTheRowLeavesStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		leak string
		row  string
	}{
		{
			// The attr road's own fallback, `unsupported attr expression`, names NEITHER the base nor the
			// attribute. It is not reworded here because four rows of the drift ledger quote it verbatim —
			// see TestWhatTheAttrReadDoorStillSpeaksTheLedgersSentence, and Gap R.272's definition of done.
			"an attribute read off a container element",
			"class Node:\n    def __init__(self, v):\n        self.v = v\n\nobjs = []\nobjs.append(Node(1))\nprint(objs[0].v)\n",
			"unsupported attr expression",
			"Gap R.272",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("the program compiled clean; want the leaked refusal containing %q", tc.leak)
			}
			if !strings.Contains(msg, tc.leak) {
				t.Fatalf("%s has been closed — the leaked %q no longer appears, so move this row to DONE and delete the pin:\n%s", tc.row, tc.leak, msg)
			}
		})
	}
}
