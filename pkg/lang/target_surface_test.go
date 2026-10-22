package lang

import (
	"strings"
	"testing"
)

// target_surface_test.go — the three doors **Gap R.379** files, asked in the program's own words
// (roadmap L11.7 · **ADR 0400**).
//
// ADR 0398 closed the family at the STATEMENT door: a payload that is already a refusal is handed over
// verbatim, and a raw one gets a wrapper whose subject comes from `stmtSurface`. Three doors were left
// untouched by that row because they never went through `noteUnlowered` at all — they each held their own
// `fmt.Errorf` with a `%T` in it:
//
//	[1, 2][0] = 3   → codegen: item assignment needs a container variable on the left (d[k] = v), got *lang.ListLit; CPython accepts more forms
//	(1).x = 2       → codegen: unsupported assignment target *lang.Attr
//	3 += x          → parser: augmented assignment requires a name or attribute target (got *lang.IntLit)
//
// Each of the three is already the right VERDICT at the right exit code — `unsupported`, exit 1, and the
// reference agrees that these programs fail (a SyntaxError for the `+=` on a literal, an AttributeError for
// the attribute store, an accepted-but-pointless write for the inline list). So the wording is the whole
// defect, and a Go type name is the worst possible subject for a sentence: `[1, 2][0] = 3` never mentions
// `ListLit`, and the reader has no table to look it up in — nor does the `--json` consumer, which is shown
// the identical string.
//
// The fix is the one ADR 0398 used, moved from statements to expressions: one shared door, `nodeSurface`,
// that names an expression the way the program wrote it AND says what kind of thing it is in the
// language's own words ("the list literal `[1, 2]`", "the attribute `x` of a number", "the number `3`").
// It is `exprSurface`'s sibling with one difference that matters: `exprSurface` falls back to `a %T` for a
// node the formatter cannot write, which would put the type name straight back into the sentence, so
// `nodeSurface` says "an expression the compiler cannot name" instead — an honest gap in the reader's
// language rather than a fact about this compiler's AST.
//
// What this row does NOT touch is pinned below in `TestWhatTheAttrReadDoorStillSpeaksTheLedgersSentence`:
// `unsupported attr expression` is quoted VERBATIM by four rows of
// `pkg/lang/testdata/interpreter-golden-drift.json`, by `dead_binding_test.go` and by the CLI's exit-code
// table, so rewording it there is a divergence silently disappearing — ADR 0398's most-feared failure,
// caught here before it happened. That sentence's own fix belongs to **Gap R.272**, whose definition of
// done already asks for it ("name the base's origin in the sentence rather than `unsupported attr
// expression`") and which must move those ledger rows in the same commit.

// targetSurfaceDoors is the family: the three doors, over the node kinds a program can actually put in the
// position. `said` is the substring that names WHAT THE PROGRAM WROTE, `cites` the row the sentence must
// point at, and `ref` is what CPython does with the same source — so a reader can see that a refusal here is
// the reference's own verdict, not this backend's limitation.
var targetSurfaceDoors = []struct {
	name  string
	src   string
	said  string
	cites string
	ref   string
	door  string
}{
	{
		name:  "an item store whose container is a list literal",
		src:   "[1, 2][0] = 3\nprint(1)\n",
		said:  "the list literal `[1, 2]`",
		cites: "Gap R.381",
		ref:   "accepted — the write lands in a temporary no one can read",
		door:  "item store",
	},
	{
		name:  "an item store whose container is a dict literal",
		src:   "{\"a\": 1}[\"a\"] = 2\nprint(1)\n",
		said:  "the dict literal",
		cites: "Gap R.381",
		ref:   "accepted, same temporary",
		door:  "item store",
	},
	{
		name:  "an item store whose container is a call's answer",
		src:   "def f():\n    return [1, 2]\n\nf()[0] = 3\nprint(1)\n",
		said:  "the call `f(…)`",
		cites: "Gap R.381",
		ref:   "accepted, same temporary",
		door:  "item store",
	},
	{
		// the shape the reference answers with a real mutation, and this backend still refuses —
		// pinned as a decline (exit 1, exit 2 forbidden), not as an answer
		name:  "an item store whose container is an instance attribute",
		src:   "class C:\n    def __init__(self):\n        self.a = [1]\n\nc = C()\nc.a[0] = 3\nprint(c.a)\n",
		said:  "the attribute `a` of the name `c`",
		cites: "Gap R.381",
		ref:   "[3]",
		door:  "item store",
	},
	{
		name:  "an attribute store on a number",
		src:   "(1).x = 2\nprint(1)\n",
		said:  "the attribute `x` of a number",
		cites: "Gap R.272",
		ref:   "raises AttributeError: 'int' object has no attribute 'x'",
		door:  "attribute store",
	},
	{
		name:  "an attribute store on a text",
		src:   "\"ab\".x = 2\nprint(1)\n",
		said:  "the attribute `x` of a text",
		cites: "Gap R.272",
		ref:   "raises AttributeError: 'str' object has no attribute 'x'",
		door:  "attribute store",
	},
	{
		name:  "an attribute store on a list literal",
		src:   "[1].x = 2\nprint(1)\n",
		said:  "the attribute `x` of a list literal",
		cites: "Gap R.272",
		ref:   "raises AttributeError: 'list' object has no attribute 'x'",
		door:  "attribute store",
	},
	{
		name:  "an augmented assignment to a number",
		src:   "x = 1\n3 += x\nprint(x)\n",
		said:  "the number `3`",
		cites: "Gap R.380",
		ref:   "raises SyntaxError: cannot assign to literal here",
		door:  "parser",
	},
	{
		name:  "an augmented assignment to a call",
		src:   "def f():\n    return 1\n\nf() += 1\nprint(1)\n",
		said:  "the call `f(…)`",
		cites: "Gap R.380",
		ref:   "raises SyntaxError: cannot assign to function call here",
		door:  "parser",
	},
	{
		name:  "an augmented assignment to a list literal",
		src:   "x = 1\n[1] += x\nprint(x)\n",
		said:  "the list literal `[1]`",
		cites: "Gap R.380",
		ref:   "raises SyntaxError: cannot assign to literal here",
		door:  "parser",
	},
	// the four that actually carried the Go type name before this row (measured, not remembered: the parent
	// build printed `got *ir.InstCall`, `*ir.InstIndex`, `*lang.Attr` for these)
	{
		name:  "an item store on a set literal",
		src:   "{1, 2}[0] = 9\nprint(1)\n",
		said:  "the set literal `{1, 2}`",
		cites: "Gap R.381",
		ref:   "raises TypeError: 'set' object does not support item assignment",
		door:  "item store",
	},
	{
		name:  "an item store on a text",
		src:   "\"abc\"[0] = 9\nprint(1)\n",
		said:  "the text `\"abc\"`",
		cites: "Gap R.381",
		ref:   "raises TypeError: 'str' object does not support item assignment",
		door:  "item store",
	},
	{
		name:  "an item store through a subscript, one container deep",
		src:   "d = {}\nd[\"x\"] = {}\nd[\"x\"][0] = 1\nprint(1)\n",
		said:  "the subscript `d[\"x\"]`",
		cites: "Gap R.381",
		ref:   "accepted — CPython mutates the stored dict, this backend refuses the write",
		door:  "item store",
	},
	{
		name:  "an attribute store whose base is what a call returns",
		src:   "def f():\n    return [1]\n\nf().pop = 9\nprint(1)\n",
		said:  "the attribute `pop` of a call's answer",
		cites: "Gap R.272",
		ref:   "raises AttributeError: 'list' object attribute 'pop' is unreadable",
		door:  "attribute store",
	},
	{
		name:  "an augmented assignment to a subscript",
		src:   "xs = [1]\nxs[0] += 5\nprint(xs[0])\n",
		said:  "the subscript `xs[0]`",
		cites: "Gap R.380",
		ref:   "6",
		door:  "parser",
	},
	{
		name:  "an augmented assignment to a call",
		src:   "print(1) += 1\nprint(1)\n",
		said:  "the call `print(…)`",
		cites: "Gap R.380",
		ref:   "raises SyntaxError: cannot assign to function call here",
		door:  "parser",
	},
}

// TestWhatTheTargetDoorsSay asks the three doors the question ADR 0166 asks of every refusal: does the
// sentence name what the PROGRAM wrote, does it name a roadmap row, and does it stay out of the compiler's
// own vocabulary?
func TestWhatTheTargetDoorsSay(t *testing.T) {
	t.Parallel()
	for _, tc := range targetSurfaceDoors {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := compiledRefusalOf(tc.src)
			if said == "" {
				t.Fatalf("the program compiled; this shape is a refusal on every engine — re-check it against the reference (%s)\n%s", tc.ref, tc.src)
			}
			if !strings.Contains(said, tc.said) {
				t.Fatalf("the %s door does not name what the program wrote (%q):\n%s", tc.door, tc.said, said)
			}
			if !strings.Contains(said, tc.cites) {
				t.Fatalf("the %s door names neither %q nor any row:\n%s", tc.door, tc.cites, said)
			}
			// the leak list this row exists to keep empty
			for _, bad := range []string{"*lang.", "0x", "%!s(", "%!d(", "%v", "lang.", "got %"} {
				if strings.Contains(said, bad) {
					t.Fatalf("the %s door leaks the compiler's own machinery (%q):\n%s", tc.door, bad, said)
				}
			}
			// and the sentence still reads as ONE sentence: the program's node comes before the advice
			if i := strings.Index(said, tc.said); i >= 0 && strings.Contains(said[:i], "CPython accepts more forms") {
				t.Fatalf("the door buried the program's node behind its own advice:\n%s", said)
			}
		})
	}
}

// TestWhatTheTargetDoorsSayIsOneDoorForAllThree is the row's own economy claim: the three sites share ONE
// answer, so they cannot drift into three wordings the way `stmtSurface` replaced three `%T`s. The test
// asks `nodeSurface` directly and pins the wording of each arm, including the fallback a door reaches for a
// node the formatter cannot write — the arm that would otherwise print `a *lang.X`.
func TestWhatTheTargetDoorsSayIsOneDoorForAllThree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		e    Expr
		want string
	}{
		{name: "a name", e: &Name{Value: "xs"}, want: "the name `xs`"},
		{name: "a number", e: &IntLit{Value: 3}, want: "the number `3`"},
		{name: "a float keeps its spelling", e: &FloatLit{Value: 2.5, Text: "2.5"}, want: "the number `2.5`"},
		{name: "a text", e: &StrLit{Value: "ab"}, want: "the text `\"ab\"`"},
		{name: "None", e: &NoneLit{}, want: "None"},
		{name: "a bool", e: &BoolLit{Value: true}, want: "a bool"},
		{name: "nothing at all", e: nil, want: "nothing"},
		{
			name: "a list literal",
			e:    &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}},
			want: "the list literal `[1, 2]`",
		},
		{
			name: "an attribute of a number names the KIND of its base",
			e:    &Attr{Obj: &IntLit{Value: 1}, Name: &Name{Value: "x"}},
			want: "the attribute `x` of a number",
		},
		{
			name: "a subscript quotes the whole thing",
			e:    &Index{Obj: &Name{Value: "xs"}, Idx: &IntLit{Value: 0}},
			want: "the subscript `xs[0]`",
		},
		{
			name: "a tuple literal is a tuple in the sentence, not a list",
			e:    &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}, Tuple: true},
			want: "the tuple literal `(1, 2)`",
		},
		{
			// the honest gap: a node the formatter cannot write is still not a Go type name
			name: "a node the formatter cannot write",
			e:    oddExpr{},
			want: "an expression the compiler cannot name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := nodeSurface(tc.e); got != tc.want {
				t.Fatalf("nodeSurface = %q, want %q", got, tc.want)
			}
			if strings.Contains(nodeSurface(tc.e), "*lang.") {
				t.Fatalf("nodeSurface leaked a Go type name: %q", nodeSurface(tc.e))
			}
		})
	}
}

// oddExpr is an expression the canonical formatter has no arm for, which is what `nodeSurface`'s fallback
// is FOR: the same reasoning ADR 0398 applied to `stmtSurface`'s default — a kind the language cannot write
// still needs a witness, or the arm rots into the leak it replaced.
type oddExpr struct{}

func (oddExpr) exprNode()    {}
func (oddExpr) Span() Span   { return Span{Line: 3, Col: 1} }
func (oddExpr) SetSpan(Span) {}

// TestWhatTheTargetDoorsLeaveStandingIsFiledNotFixed holds what this row MEASURED and did not take.
// Each is a program the reference ANSWERS — not one it refuses — so the door's sentence is the whole
// defect at the wrong altitude: the shape needs a lowering, and the row that owns it must be named in the
// sentence AND here. `TestWhatTheTargetDoorsSay` cannot tell these from the twelve shapes where CPython
// agrees with the verdict (its `ref` cell is documentation, not an assertion); this table is the assertion,
// and the live reference leg is `integration`'s test of the same name.
func TestWhatTheTargetDoorsLeaveStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		row   string // the roadmap row that owns the missing half
		ref   string // what CPython does, asserted live in the integration leg
		owner string // the gate that must close it
	}{
		{
			name:  "an augmented assignment to a subscript",
			src:   "xs = [1]\nxs[0] += 5\nprint(xs[0])\n",
			row:   "Gap R.380",
			ref:   "6",
			owner: "the parser's `Name`/`Attr` rule for `op=`, which declines a target `genItemStoreFromExpr` already lowers",
		},
		{
			name:  "an item store whose container is what a call returns",
			src:   "def f():\n    return [1, 2]\n\nf()[0] = 3\nprint(1)\n",
			row:   "Gap R.381",
			ref:   "1 (CPython writes into the temporary the call handed back)",
			owner: "assignIndex's `*Name` rule",
		},
		{
			name:  "an item store into a list literal",
			src:   "[1, 2][0] = 3\nprint(1)\n",
			row:   "Gap R.381",
			ref:   "1 (CPython writes, and nothing can read the temporary back)",
			owner: "assignIndex's `*Name` rule",
		},
		{
			name:  "an item store through a subscript, one container deep",
			src:   "d = {}\nd[\"x\"] = {}\nd[\"x\"][0] = 1\nprint(1)\n",
			row:   "Gap R.381",
			ref:   "1 (CPython mutates the stored dict)",
			owner: "assignIndex's `*Name` rule",
		},
		{
			name:  "an item store through an instance attribute",
			src:   "class C:\n    def __init__(self):\n        self.a = [1]\n\nc = C()\nc.a[0] = 3\nprint(c.a)\n",
			row:   "Gap R.381",
			ref:   "[3]",
			owner: "assignIndex's `*Name` rule, and **Gap R.272**'s instance-tagged base",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := compiledRefusalOf(tc.src)
			if said == "" {
				t.Fatalf("this shape now COMPILES — %s has been closed: move its row to DONE and delete this pin\n%s", tc.row, tc.src)
			}
			if !strings.Contains(said, tc.row) {
				t.Fatalf("the refusal does not name the row that owns the missing half (%s):\n%s", tc.row, said)
			}
			if HasGoldenAnswer(tc.src) {
				t.Fatalf("a refused shape is on the record, which the recorder refuses to file:\n%s", tc.src)
			}
		})
	}
}

// TestTheTargetDoorsAnswerWhatTheyAlwaysAnswered is the control side: the forms that WORK must keep working
// byte for byte, so a wording change cannot have quietly turned an answer into a refusal — or, worse, a
// refusal into an answer. Every `want` is what CPython prints for the same source.
func TestTheTargetDoorsAnswerWhatTheyAlwaysAnswered(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "the name target",
			src:  "x = 1\nx = 2\nprint(x)\n",
			want: "2",
		},
		{
			name: "the subscript target",
			src:  "xs = [1, 2]\nxs[0] = 9\nprint(xs)\n",
			want: "[9, 2]",
		},
		{
			name: "the attribute target of an instance",
			src:  "class C:\n    def __init__(self):\n        self.v = 0\n\nc = C()\nc.v = 7\nprint(c.v)\n",
			want: "7",
		},
		{
			name: "augmented assignment to a name",
			src:  "x = 1\nx += 2\nprint(x)\n",
			want: "3",
		},
		{
			name: "the tuple target",
			src:  "a, b = [1, 2]\nprint(a, b)\n",
			want: "1 2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("the control is on the record — record it from the reference with tools/recmerge:\n%s", tc.src)
			}
			rec, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record could not answer: %v", err)
			}
			if strings.TrimSpace(rec) != tc.want {
				t.Fatalf("the record said %q, this table says %q", strings.TrimSpace(rec), tc.want)
			}
			got, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				t.Fatalf("a target form that used to compile is now refused (%v): %s", tc.name, got)
			}
			if strings.TrimSpace(got) != tc.want {
				t.Fatalf("compiled printed %q, the reference prints %q", strings.TrimSpace(got), tc.want)
			}
		})
	}
}

// TestWhatTheAttrReadDoorStillSpeaksTheLedgersSentence is the reason this row stopped at three doors.
// `unsupported attr expression` is the same disease — a sentence that names nothing but a door — and the
// fix is one line away. It is NOT taken here, because four rows of
// `pkg/lang/testdata/interpreter-golden-drift.json` quote that exact string as the compiled leg's `actual`,
// and `dead_binding_test.go` and the CLI's exit-code table quote it too: rewording it in this commit would
// have made four recorded divergences silently disappear, which is the one failure the drift ratchet exists
// to make loud (ADR 0398's lesson, applied one cycle later). Its owner is **Gap R.272**, whose definition of
// done already demands the better sentence — and which must move those ledger rows in the same commit.
//
// The second case is a divergence this row FOUND while writing its own controls: an augmented store
// through an attribute of a value handed in as a PARAMETER is the reference's `3` and this backend's
// `unsupported attr expression`. It is not on the record (the recorder refuses an entry the compiled leg
// will not agree to) and it was not pinned anywhere, so it is pinned here with the reference's answer
// beside it rather than dropped from the control table and lost.
func TestWhatTheAttrReadDoorStillSpeaksTheLedgersSentence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		ref  string
	}{
		{
			name: "an attribute read off a container element",
			src:  "class Node:\n    def __init__(self, v):\n        self.v = v\n\nobjs = []\nobjs.append(Node(1))\nprint(objs[0].v)\n",
			ref:  "1",
		},
		{
			name: "an augmented attribute store through a parameter",
			src:  "class C:\n    def __init__(self):\n        self.v = 0\n\ndef bump(c):\n    c.v += 3\n    return c.v\n\nprint(bump(C()))\n",
			ref:  "3",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			said := compiledRefusalOf(tc.src)
			if said == "" {
				t.Fatalf("this shape now COMPILES — Gap R.272 has been closed, and the four drift rows that quote this sentence must be re-recorded in the same commit\n%s", tc.src)
			}
			if !strings.Contains(said, "unsupported attr expression") {
				t.Fatalf("the attr READ door changed wording (roadmap Gap R.272 owns it, and four drift-ledger rows quote the old sentence verbatim):\n%s", said)
			}
			if HasGoldenAnswer(tc.src) {
				t.Fatalf("a refused shape is on the record, which the recorder refuses to file:\n%s", tc.src)
			}
		})
	}
}
