package lang

import (
	"fmt"
	"strings"
	"testing"
)

// Coverage for roadmap Gap R.68 / ADR 0358: a `match` capture is a BINDING, so it happens where the
// arm that bound it runs, in the frame that owns the name, and nowhere else.
//
// The compiled backend gives each capture a slot, and until now the store into that slot was written
// where the PATTERN was lowered. `matchPattern` is called to decide WHICH arm runs — every arm's
// pattern, in order, before any arm's body — so a store placed there is performed by the arms that do
// not match as much as by the one that does, along with the `store i8 1` that makes the slot look
// written. `v = 5; match v: case [y]: pass; case 5: pass; print(y)` printed `0` at the exit code of a
// handled program, where the reference raises NameError (and UnboundLocalError inside a function): the
// class a handler matches on, the exit code a script branches on, and the answer itself all wrong,
// with nothing printed to say so.
//
// The written-flag machinery was in place and correct (ADR 0228); the flag was being set by an act that
// is not a binding. So the cases below are about where a store sits and who may set a bit:
//
//   - the binding, and the flag store beside it, land in the block the branch enters (IR);
//   - an or-pattern binds the alternative that matched and not the ones that did not (behaviour);
//   - the frame owns a capture whether or not the match sits in a function (the checker);
//   - the census of capture positions is one function three roads ask (unit);
//   - the shape and the class are the record's and the reference's (both witness legs);
//   - what is still not answered stays filed, measured, and refused in words (the filed rows).

const (
	// unboundMsg is what the reference says for a name the frame owns and has no value for.
	unboundMsg = "cannot access local variable '%s' where it is not associated with a value"
	// notDefinedMsg is what it says for the same event in the module's own statements.
	notDefinedMsg = "name '%s' is not defined"
	// pointDecl is a class whose one attribute a pattern can capture. `case Point(x)` on an actual
	// Point is not reference-legal (CPython wants __match_args__, which is roadmap Gap R.304), so the
	// programs here that the record holds are the ones where the class arm does not match.
	pointDecl = "class Point:\n    def __init__(self, x):\n        self.x = x\n\n"
)

// compiledMatchIR compiles src and asserts the module verifies, as every codegen case must (ADR 0166:
// a module the toolchain rejects is a source-level bug blamed on the linker).
func compiledMatchIR(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := VerifyModuleIR(res.IR, 0); err != nil {
		t.Fatalf("the emitted module does not verify: %v\n%s", err, res.IR)
	}
	return res.IR
}

// TestACaptureTheSkippedArmComputedStillTraps is the measured bug. Each row is a program whose match
// has an arm that would bind the name and an arm that actually runs; the compiled answer used to be a
// word from the frame at exit 0. The wanted class and message are the reference's own, measured with
// `python3` beside each program, not a format this file invents.
func TestACaptureTheSkippedArmComputedStillTraps(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, wantClass, wantMsg string }{
		{
			name:      "sequence capture the subject is not",
			src:       "v = 5\nmatch v:\n    case [y]:\n        pass\n    case 5:\n        pass\nprint(y)\n",
			wantClass: "NameError", wantMsg: fmt.Sprintf(notDefinedMsg, "y"),
		},
		{
			name:      "mapping capture the subject has no entry for",
			src:       "v = {}\nmatch v:\n    case {\"k\": got}:\n        pass\n    case _:\n        pass\nprint(got)\n",
			wantClass: "NameError", wantMsg: fmt.Sprintf(notDefinedMsg, "got"),
		},
		{
			name:      "class capture over a subject that is not an instance",
			src:       pointDecl + "match 5:\n    case Point(x):\n        pass\n    case 5:\n        pass\nprint(x)\n",
			wantClass: "NameError", wantMsg: fmt.Sprintf(notDefinedMsg, "x"),
		},
		{
			name:      "the same shape inside a function takes the other class",
			src:       "def go(v):\n    match v:\n        case [a]:\n            pass\n        case 5:\n            pass\n    return a\n\nprint(go(5))\n",
			wantClass: "UnboundLocalError", wantMsg: fmt.Sprintf(unboundMsg, "a"),
		},
		{
			name:      "a mapping capture skipped inside a function",
			src:       "def go(v):\n    match v:\n        case {\"k\": got}:\n            pass\n        case 5:\n            pass\n    return got\n\nprint(go(5))\n",
			wantClass: "UnboundLocalError", wantMsg: fmt.Sprintf(unboundMsg, "got"),
		},
		{
			name:      "a capture skipped by a failed class check is not bound either",
			src:       pointDecl + "def go(v):\n    match v:\n        case Point(x):\n            pass\n        case 5:\n            pass\n    return x\n\nprint(go(5))\n",
			wantClass: "UnboundLocalError", wantMsg: fmt.Sprintf(unboundMsg, "x"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Code == 0 {
				t.Fatalf("the program answered %q at exit 0; a capture no arm bound must trap with %s: %s\n%s",
					res.Output, tc.wantClass, tc.wantMsg, res.IR)
			}
			if strings.TrimSpace(res.Output) != "" {
				t.Fatalf("the program printed %q before trapping; the capture reached a print that must not run", res.Output)
			}
			if !strings.Contains(res.Stderr, tc.wantClass+": "+tc.wantMsg) {
				t.Fatalf("trap was\n%s\nwant %s: %s", res.Stderr, tc.wantClass, tc.wantMsg)
			}
		})
	}
}

// TestACaptureStillAnswersOnTheArmThatBoundIt is the half that must not regress: deferring a binding may
// delay it, never lose one. Every row's wanted output is what `python3` prints for the same source, and
// each source is in the record (tools/recmerge refuses to file one whose compiled leg disagrees).
//
// The guard rows ask the question a guard exists to answer: the reference KEEPS a capture whose pattern
// matched and whose guard then failed (measured: `case z if False` leaves `z` bound), which is why the
// store goes in front of the guard rather than after it.
func TestACaptureStillAnswersOnTheArmThatBoundIt(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"bare name capture", "match 5:\n    case y:\n        print(y)\n", "5\n"},
		{"sequence capture", "xs = [4]\nmatch xs:\n    case [y]:\n        print(y)\n", "4\n"},
		{"mapping capture", "d = {\"k\": 7}\nmatch d:\n    case {\"k\": got}:\n        print(got)\n", "7\n"},
		{"capture shadows an earlier binding", "x = 99\nxs = [4]\nmatch xs:\n    case [x]:\n        print(x)\n", "4\n"},
		{"the arm that binds reads the capture itself", "xs = [4]\nmatch xs:\n    case [y]:\n        print(y + 1)\n", "5\n"},
		{"later statements see the binding", "xs = [4]\nmatch xs:\n    case [y]:\n        pass\nprint(y)\n", "4\n"},
		{"a guard reads the capture its own pattern bound", "match 5:\n    case z if z == 5:\n        print(z)\n", "5\n"},
		{"a failed guard does not unbind what matched (CPython keeps it)", "match 5:\n    case z if False:\n        pass\n    case _:\n        pass\nprint(z)\n", "5\n"},
		{"the wildcard arm binds nothing", "x = 5\nmatch x:\n    case _:\n        print(\"wild\")\n", "wild\n"},
		{"an or-arm whose first alternative matches", "match 9:\n    case 1 | 2:\n        print(\"small\")\n    case y:\n        print(y)\n", "9\n"},
		{"an or-arm whose second alternative matches", "match 5:\n    case 1 | 2:\n        print(\"small\")\n    case y:\n        print(y)\n", "5\n"},
		{"arithmetic over two captures of an all-number container", "xs = [10, 20]\nmatch xs:\n    case [a, b]:\n        print(a + b)\n", "30\n"},
		// The capture that IS bound by an earlier statement keeps that value when no arm rebinds it:
		// `case [x]` does not fit 5, `case 5` binds nothing, and `x` is still the 5 the frame started
		// with. Deferring a binding may delay one, never cancel one that already happened.
		{"a name the match never rebinds keeps what it was", "def go():\n    x = 5\n    match x:\n        case [x]:\n            pass\n        case 5:\n            pass\n    return x\n\nprint(go())\n", "5\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := captureStdout(t, tc.src)
			if got != tc.want {
				t.Fatalf("printed %q, want %q", got, tc.want)
			}
		})
	}
}

// TestACaptureTrapIsCatchableByClassOnEitherFrame: the trap is an ordinary typed raise, so `except
// NameError:` catches it at module level and `except UnboundLocalError:` inside a function — ADR 0212's
// rule for a built-in trap, and CPython's own split of the two events.
func TestACaptureTrapIsCatchableByClassOnEitherFrame(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			"module frame, caught on NameError",
			"match 5:\n    case [y]:\n        pass\n    case _:\n        pass\ntry:\n    print(y)\nexcept NameError:\n    print(\"caught\")\n",
			"caught\n",
		},
		{
			"function frame, caught on UnboundLocalError",
			"def go(v):\n    match v:\n        case [a]:\n            pass\n    try:\n        print(a)\n    except UnboundLocalError:\n        print(\"caught\")\n    return 0\ngo(5)\n",
			"caught\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := captureStdout(t, tc.src); got != tc.want {
				t.Fatalf("printed %q, want %q — the handler must match on the class the reference raises", got, tc.want)
			}
		})
	}
}

// TestAnOrPatternBindsOnlyTheAlternativeThatMatched is the same defect one level down, and the reason
// the arm is not one block. Folding an arm's alternatives into a single `or i1` and writing every
// alternative's captures made a name bound by the alternative that did NOT match answer the value the
// other one computed: `case {"k": a} | [a]:` over {"k": 8} printed `0` (the sequence alternative's
// `rt_get_elem` ran last and won) where the arm that matched is the mapping one. The reference refuses
// this program outright (`alternative patterns bind different names`, roadmap Gap R.307), so the values
// below are this backend's own contract rather than a parity claim — but they are the values the arm
// that matched selected, which is the claim the placement has to hold.
func TestAnOrPatternBindsOnlyTheAlternativeThatMatched(t *testing.T) {
	t.Parallel()
	src := "def f(v):\n    match v:\n        case {\"k\": a} | [a]:\n            print(a)\n            return 0\n    return 0\n\nf({\"k\": 8})\nf([3])\n"
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("jit: %v", err)
	}
	if res.Code != 0 {
		t.Fatalf("the program trapped (%s) where both arms bind\n%s", res.Stderr, res.IR)
	}
	if res.Output != "8\n3\n" {
		t.Fatalf("printed %q, want \"8\\n3\\n\" — each alternative must bind only its own value\n%s", res.Output, res.IR)
	}

	// The same arm written the other way round, where the FIRST alternative is the one that must not
	// decide the value. Before the arm stopped being one block this printed `0 8`: the sequence
	// alternative was evaluated first, its `rt_get_elem` answered the dict's slot 0, and the mapping
	// alternative's store — the arm that actually matched — was overwritten by nothing, because both
	// stores sat in the same block and the last one won by position, not by which alternative matched.
	rev := "def f(v):\n    match v:\n        case [a] | {\"k\": a}:\n            print(a)\n            return 0\n    return 0\n\nf([3])\nf({\"k\": 8})\nf(5)\n"
	if got := captureStdout(t, rev); got != "3\n8\n" {
		t.Fatalf("the reversed arm printed %q, want \"3\\n8\\n\"", got)
	}
	if got, err := runGoldenStdout(t, rev); err != nil {
		t.Fatalf("record leg: %v", err)
	} else if got != "3\n8\n" {
		t.Fatalf("the record says %q for the reversed arm", got)
	}
}

// TestTheCaptureStoreSitsInTheArmTheBranchEntered is the artifact-level half, and the reason the fix is
// a placement rather than a new check. For
//
//	class Point: …
//	match 5:
//	    case Point(x):
//	        pass
//	    case 5:
//	        pass
//	print(x)
//
// it asserts over the emitted module that the only stores into `%_x` are the entry-block zeroing and
// ONE store inside an arm block; that store, and the `store i8 1` that sets the written-flag, both come
// AFTER the `br i1 <pattern>` that decides the arm — so the arm that did not match never performed
// them; and that the flag is cleared on entry. Before the fix both stores were emitted while lowering
// `case Point(x)`, which is above the branch: the flag was set on the way to deciding the subject is
// not a Point.
func TestTheCaptureStoreSitsInTheArmTheBranchEntered(t *testing.T) {
	t.Parallel()
	ir := compiledMatchIR(t, pointDecl+"match 5:\n    case Point(x):\n        pass\n    case 5:\n        pass\nprint(x)\n")

	lines := strings.Split(ir, "\n")
	// The match's own branch is the first `br i1` whose targets are the arm labels the lowering mints:
	// nothing above it may store into a capture, because everything above it runs whichever arm the
	// branch picks. `br i1` alone does not find it — the module preamble contains branches of its own
	// (`rt_die`'s frame walk) that sit above every function, including the one under test.
	firstBr := -1
	for i, ln := range lines {
		text := strings.TrimSpace(ln)
		if strings.HasPrefix(text, "br i1 ") && strings.Contains(text, "label %match.") {
			firstBr = i
			break
		}
	}
	if firstBr < 0 {
		t.Fatalf("no `br i1 <pattern>` in the module — the match did not lower as a branch\n%s", ir)
	}

	var valueStores, flagStores, flagSets []int
	for i, ln := range lines {
		text := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(text, "store i32 ") && strings.HasSuffix(text, "i32* %_x"):
			if strings.HasPrefix(text, "store i32 0,") {
				continue // the entry-block zeroing (ADR 0228's hoisted init), not a binding
			}
			valueStores = append(valueStores, i)
		case text == "store i8 0, i8* %bnd_x":
			flagStores = append(flagStores, i)
		case text == "store i8 1, i8* %bnd_x":
			flagSets = append(flagSets, i)
		}
	}
	if len(valueStores) != 1 {
		t.Fatalf("the capture %%_x must have exactly one binding store (the arm's), found %d at %v\n%s",
			len(valueStores), valueStores, ir)
	}
	if valueStores[0] < firstBr {
		t.Fatalf("the store into %%_x is at line %d, above the `br i1 <pattern>` at line %d — a skipped arm would perform it\n%s",
			valueStores[0], firstBr, ir)
	}
	if len(flagSets) != 1 {
		t.Fatalf("the capture must carry exactly one written-flag set, found %d at %v\n%s", len(flagSets), flagSets, ir)
	}
	if flagSets[0] < firstBr {
		t.Fatalf("the written-flag store is above the branch at line %d: the flag would be set by a pattern evaluation, which is what made the read answer a slot (Gap R.68)\n%s", firstBr, ir)
	}
	if flagSets[0] < valueStores[0] {
		t.Fatalf("the flag is set before the value is stored — a read between the two sees a bound, unwritten slot\n%s", ir)
	}
	if len(flagStores) != 1 {
		t.Fatalf("the flag must be cleared on entry exactly once, found %d\n%s", len(flagStores), ir)
	}
	if flagStores[0] > firstBr {
		t.Fatalf("the flag's zeroing must dominate the branch (line %d > %d)\n%s", flagStores[0], firstBr, ir)
	}
	if !strings.Contains(ir, "%bnd_x = alloca i8") {
		t.Fatalf("the capture needs a written-flag of its own\n%s", ir)
	}
}

// TestMatchCaptureIsALocalOfTheFrameItSitsIn is the checker's half, and the reason the module-level rows
// needed two changes and not one: `boundEligible` hands a flag to a name only once the checker has
// recorded it as a local of the frame whose read is uncertain, and the old code asked `inFunc` — so a
// module-level capture was never a local, `isDefinite` answered "certain", no flag was attached and the
// read printed the top-level alloca's word.
func TestMatchCaptureIsALocalOfTheFrameItSitsIn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		src      string
		inFunc   bool
		capture  string
		wantFlag bool
	}{
		{name: "function frame, sequence capture", src: "def go(v):\n    match v:\n        case [a]:\n            pass\n        case 5:\n            pass\n    return a\n", inFunc: true, capture: "a", wantFlag: true},
		{name: "function frame, mapping capture", src: "def go(v):\n    match v:\n        case {\"k\": got}:\n            pass\n        case 5:\n            pass\n    return got\n", inFunc: true, capture: "got", wantFlag: true},
		{name: "function frame, class capture", src: pointDecl + "def go(v):\n    match v:\n        case Point(x):\n            pass\n        case 5:\n            pass\n    return x\n", inFunc: true, capture: "x", wantFlag: true},
		{name: "function frame, capture bound by every arm pays nothing", src: "def go(v):\n    match v:\n        case y:\n            pass\n    return y\n", inFunc: true, capture: "y", wantFlag: false},
		{name: "module frame, sequence capture", src: "v = 5\nmatch v:\n    case [y]:\n        pass\n    case 5:\n        pass\nprint(y)\n", capture: "y", wantFlag: true},
		{name: "module frame, mapping capture", src: "v = {}\nmatch v:\n    case {\"k\": got}:\n        pass\n    case _:\n        pass\nprint(got)\n", capture: "got", wantFlag: true},
		{name: "module frame, class capture", src: pointDecl + "match 5:\n    case Point(x):\n        pass\n    case 5:\n        pass\nprint(x)\n", capture: "x", wantFlag: true},
		{name: "the wildcard arm is not a capture", src: "x = 5\nmatch x:\n    case _:\n        pass\nprint(x)\n", capture: "_", wantFlag: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := parseProgram(tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			table := UnwrittenReads(prog)
			uncertain := map[string]bool{}
			for _, set := range table { // the module's entry is the nil key; a function's is its FuncDef
				for nm := range set {
					uncertain[nm] = true
				}
			}
			ir := compiledMatchIR(t, tc.src)
			if tc.capture == "_" {
				if strings.Contains(ir, "bnd__") {
					t.Fatalf("the wildcard arm binds nothing, so nothing may carry a flag for it\n%s", ir)
				}
				return
			}
			flagged := strings.Contains(ir, "%bnd_"+tc.capture+" = alloca i8")
			if flagged != tc.wantFlag {
				t.Fatalf("written-flag for %q present=%v, want %v (checker uncertain=%v)\n%s",
					tc.capture, flagged, tc.wantFlag, uncertain[tc.capture], ir)
			}
			if !tc.wantFlag {
				return
			}
			// The checker's answer and the emitter's must agree: the flag exists exactly where the
			// every-path rule could not prove the binding.
			if !uncertain[tc.capture] {
				t.Fatalf("a flag was attached for %q, which the checker did not record as possibly-unbound — the two roads disagree\n%s", tc.capture, ir)
			}
		})
	}
}

// TestMatchCaptureNamesIsTheOneCensus is the unit test for the rule three roads now share: the checker
// (semantic.go's matchPatternNames), the written-flag eligibility rule (boundflags.go) and the emitter
// (matchPattern through bindPat). Each row is one arm of matchPattern, and the answer is read off the
// emitter's own behaviour: a name bindPat is handed is a capture, and a name it is not handed is not.
// The rows that used to be wrong are what the fix is: the class pattern and the mapping pattern were
// missing from the checker's census entirely, and the nested sub-patterns it promised are binds the
// emitter never performs.
func TestMatchCaptureNamesIsTheOneCensus(t *testing.T) {
	t.Parallel()
	n := func(s string) Expr { return &Name{Value: s} }
	cases := []struct {
		name string
		pat  Expr
		want []string
	}{
		{"bare name capture", n("x"), []string{"x"}},
		{"the wildcard is a wildcard", n("_"), nil},
		{"literal pattern binds nothing", &IntLit{Value: 5}, nil},
		{"sequence: name elements capture", &ListLit{Elems: []Expr{n("a"), &IntLit{Value: 1}}}, []string{"a"}},
		{"sequence: a wildcard element does not", &ListLit{Elems: []Expr{n("_"), n("b")}}, []string{"b"}},
		{"sequence: a literal element does not", &ListLit{Elems: []Expr{&IntLit{Value: 1}, &IntLit{Value: 2}}}, nil},
		{"sequence: a nested sub-pattern binds nothing", &ListLit{Elems: []Expr{&ListLit{Elems: []Expr{n("deep")}}}}, nil},
		{"mapping: name values capture", &DictLit{Keys: []Expr{&StrLit{Value: "k"}}, Vals: []Expr{n("got")}}, []string{"got"}},
		{"mapping: a wildcard value does not", &DictLit{Keys: []Expr{&StrLit{Value: "k"}}, Vals: []Expr{n("_")}}, nil},
		{"mapping: a literal value does not", &DictLit{Keys: []Expr{&StrLit{Value: "k"}}, Vals: []Expr{&IntLit{Value: 3}}}, nil},
		{"class: name arguments capture", &Call{Fn: n("Point"), Args: []Expr{n("x"), n("y")}}, []string{"x", "y"}},
		{"class: a wildcard argument does not", &Call{Fn: n("Point"), Args: []Expr{n("_")}}, nil},
		{"class: a non-name argument is not a capture", &Call{Fn: n("Point"), Args: []Expr{&IntLit{Value: 1}}}, nil},
		{"a parenthesised tuple is not a sequence pattern", &Tuple{Elems: []Expr{n("a"), n("b")}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := matchCaptureNames(tc.pat)
			if len(got) != len(tc.want) {
				t.Fatalf("census is %v, want %v", captureNamesOf(got), tc.want)
			}
			for _, nm := range tc.want {
				if !got[nm] {
					t.Fatalf("census is %v, missing %q (want %v)", captureNamesOf(got), nm, tc.want)
				}
			}
			// The checker asks THIS function; if it ever keeps its own copy again, this is the case
			// that says so.
			if !sameCaptureNameSet(matchPatternNames(tc.pat), got) {
				t.Fatalf("the checker's census %v and the emitter's %v disagree for %T", captureNamesOf(matchPatternNames(tc.pat)), captureNamesOf(got), tc.pat)
			}
		})
	}
}

// TestMatchCaptureCensusIsWhatTheCheckerBinds walks the source forms through the parser and asks both
// the census and the eligibility rule, so a change to the parser's pattern shapes (a new arm of `case`)
// cannot silently add a position that no census knows about, and a name the census lists cannot end up
// below the eligibility rule — which is the state that leaves a capture with no flag and its read
// answering the slot.
func TestMatchCaptureCensusIsWhatTheCheckerBinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src     string
		capture string
	}{
		{"match v:\n    case x:\n        pass\n", "x"},
		{"match v:\n    case [a, 1]:\n        pass\n", "a"},
		{"match v:\n    case {\"k\": got}:\n        pass\n", "got"},
		{"match v:\n    case Point(x, y):\n        pass\n", "x"},
		{"match v:\n    case 1 | z:\n        pass\n", "z"},
	}
	for _, tc := range cases {
		t.Run(strings.ReplaceAll(tc.src, "\n", " ⏎ "), func(t *testing.T) {
			t.Parallel()
			prog, err := parseProgram("v = 5\n" + tc.src)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			ms, ok := prog.Stmts[1].(*MatchStmt)
			if !ok || len(ms.Cases) != 1 {
				t.Fatalf("not the single-arm match this case describes: %#v", prog.Stmts[1])
			}
			listed := false
			for _, p := range append([]Expr{ms.Cases[0].Pattern}, ms.Cases[0].Or...) {
				if matchCaptureNames(p)[tc.capture] {
					listed = true
				}
			}
			if !listed {
				t.Fatalf("no pattern of the arm lists %q as a capture", tc.capture)
			}
			ok2, bad := boundEligible(prog.Stmts[1:])
			if bad[tc.capture] || !ok2[tc.capture] {
				t.Fatalf("the eligibility rule will not give %q a flag, so its read answers the slot (Gap R.68): ok=%v bad=%v", tc.capture, ok2[tc.capture], bad[tc.capture])
			}
		})
	}
}

// TestMatchCaptureRecordLeg checks the shape and the class against the record — the 46 sources this
// cycle put through tools/recmerge, each recorded from the reference with the compiled leg
// cross-checked. A missing record fails the case, so this coverage cannot be deleted by deleting an
// entry (ADR 0302's rule).
func TestMatchCaptureRecordLeg(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		src    string
		trap   string // the `<Class>: <message>` the record holds
		stdout string // what the record holds for a program that prints
	}{
		{"sequence capture, subject not a list", "v = 5\nmatch v:\n    case [y]:\n        pass\n    case 5:\n        pass\nprint(y)\n", "NameError: name 'y' is not defined", ""},
		{"mapping capture, subject has no entry", "v = {}\nmatch v:\n    case {\"k\": got}:\n        pass\n    case _:\n        pass\nprint(got)\n", "NameError: name 'got' is not defined", ""},
		{"class capture, subject not an instance", pointDecl + "match 5:\n    case Point(x):\n        pass\n    case 5:\n        pass\nprint(x)\n", "NameError: name 'x' is not defined", ""},
		{"function frame takes UnboundLocalError", "def go(v):\n    match v:\n        case [a]:\n            pass\n        case 5:\n            pass\n    return a\n\nprint(go(5))\n", "UnboundLocalError: cannot access local variable 'a' where it is not associated with a value", ""},
		{"mapping capture inside a function", "def go(v):\n    match v:\n        case {\"k\": got}:\n            pass\n        case 5:\n            pass\n    return got\n\nprint(go(5))\n", "UnboundLocalError: cannot access local variable 'got' where it is not associated with a value", ""},
		{"a skipped arm does not overwrite a real binding", "def go(x, v):\n    match v:\n        case [x]:\n            pass\n    print(x)\n    return 0\ngo(9, 5)\n", "", "9\n"},
		{"the arm that binds reads back what bound it", "def go(x, v):\n    match v:\n        case [x]:\n            pass\n    print(x)\n    return 0\ngo(9, [4])\n", "", "4\n"},
		{"the trap is catchable by class", "def go(v):\n    match v:\n        case [a]:\n            pass\n    try:\n        print(a)\n    except UnboundLocalError:\n        print(\"caught\")\n    return 0\ngo(5)\n", "", "caught\n"},
		{"an unwritten sequence capture in a function", "def go(v):\n    match v:\n        case [a]:\n            pass\n    print(a)\n    return 0\ngo(5)\n", "UnboundLocalError: cannot access local variable 'a' where it is not associated with a value", ""},
		{"a capture the guard's arm did not bind", "def go(v):\n    match v:\n        case 1:\n            print(\"one\")\n        case [a]:\n            print(\"seq\", a)\n    print(\"after\", a)\n    return 0\ngo(5)\n", "UnboundLocalError: cannot access local variable 'a' where it is not associated with a value", ""},
		{"the arm that binds prints the value", "xs = [4]\nmatch xs:\n    case [y]:\n        print(y)\n", "", "4\n"},
		{"a mapping capture prints the entry", "d = {\"k\": 7}\nmatch d:\n    case {\"k\": got}:\n        print(got)\n", "", "7\n"},
		{"a capture arithmetic can use", "xs = [10, 20]\nmatch xs:\n    case [a, b]:\n        print(a + b)\n", "", "30\n"},
		{"a failed guard keeps the binding the pattern made", "match 5:\n    case z if False:\n        pass\n    case _:\n        pass\nprint(z)\n", "", "5\n"},
		{"a capture an earlier statement already bound survives an arm that did not run", "def go():\n    x = 5\n    match x:\n        case [x]:\n            pass\n        case 5:\n            pass\n    return x\n\nprint(go())\n", "", "5\n"},
		// Gap R.306's program, asked of the record: a sequence pattern is a kind test, so the dict arm
		// fails and the wildcard answers.
		{"a sequence pattern does not match a dict", "d = {\"k\": 1}\nmatch d:\n    case [a]:\n        print(\"list\", a)\n    case _:\n        print(\"no\")\n", "", "no\n"},
		{"each alternative binds its own capture", "def f(v):\n    match v:\n        case {\"k\": a} | [a]:\n            print(a)\n            return 0\n    return 0\n\nf({\"k\": 8})\nf([3])\n", "", "8\n3\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := goldenLookup(t, tc.src)
			if tc.trap != "" {
				got := want.ExnType + ": " + want.ExnMsg
				if got != tc.trap {
					t.Fatalf("the record holds %q, want %q", got, tc.trap)
				}
				if _, err := runGoldenStdout(t, tc.src); err == nil {
					t.Fatalf("the compiled leg answered where the record traps: %q", tc.src)
				}
				return
			}
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("compiled leg: %v", err)
			}
			if got != tc.stdout {
				t.Fatalf("the record says %q, the compiled leg printed %q", tc.stdout, got)
			}
		})
	}
}

// TestMatchCaptureShapesStillNotAnswered files what this cycle did NOT fix, measured rather than
// assumed. The first group is refused in words (ADR 0166's contract) with a roadmap row each; the
// second group still answers where the reference does something else, and is asserted as the wrong
// answer it is so a future cycle cannot lose the measurement. None of them is a silent zero produced by
// the code this commit changed: the nested ones in particular are honest refusals BECAUSE the census
// does not list a name bindPat is never handed — listing it would have made the checker vouch for a
// program codegen cannot build.
func TestMatchCaptureShapesStillNotAnswered(t *testing.T) {
	t.Parallel()
	refused := []struct{ name, src, wantNeedle, roadmap string }{
		{
			"nested sequence sub-pattern binds nothing (Gap R.301)",
			"match [[2, 3]]:\n    case [[a]]:\n        print(a)\n",
			`undefined name "a"`, "roadmap Gap R.301",
		},
		{
			"nested mapping value binds nothing (Gap R.301)",
			"match {2: {\"b\": 7}}:\n    case {2: {\"b\": v}}:\n        print(v)\n",
			`undefined name "v"`, "roadmap Gap R.301",
		},
		{
			"class pattern with a keyword attribute is refused (Gap R.303)",
			pointDecl + "match Point(5):\n    case Point(x=1):\n        print(1)\n",
			"expected ')' in class pattern", "roadmap Gap R.303",
		},
		{
			"class pattern with a non-name argument is refused (Gap R.304)",
			pointDecl + "match Point(5):\n    case Point(x, 1):\n        print(1)\n",
			"class pattern attribute must be a name", "roadmap Gap R.304",
		},
		{
			"a parenthesised tuple pattern is refused (Gap R.302)",
			// `case (a,)` is a Tuple pattern, which matchPattern has no arm for. The checker refuses it
			// one stage earlier (`undefined name "a"`, because the census has no Tuple arm and so never
			// heard of `a`) and the emitter refuses it here, by naming the AST type it cannot lower.
			// Either way nothing is silently bound — but the message blames the name, not the missing
			// pattern form, which is the row's second half.
			"match (5,):\n    case (a,):\n        print(a)\n",
			"unsupported expression *lang.Tuple", "roadmap Gap R.302",
		},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s: this compiles — the roadmap row %s says it is not answered, and a case that answers nothing must not answer silently", tc.src, tc.roadmap)
			}
			if !strings.Contains(err.Error(), tc.wantNeedle) {
				t.Fatalf("%s: the refusal does not name what is missing\n got:  %v\n want: %q\n the row this files is %s", tc.src, err, tc.wantNeedle, tc.roadmap)
			}
		})
	}

	// Still wrong, measured, and owned by a row. Each is the compiled answer today beside the
	// reference's, so the case fails the day the answer changes for the better without a row being
	// edited — the same shape as the debt ledger's "a paid debt fails the build" rule.
	stillWrong := []struct{ name, src, compiledAsIs, reference, roadmap string }{
		{
			"a text element capture answers its @str_tab index (Gap R.305)",
			"xs = [\"eight\"]\nmatch xs:\n    case [a]:\n        print(a)\n",
			"0\n", "eight\n", "roadmap Gap R.305",
		},
		{
			"a text mapping value capture answers its index (Gap R.305)",
			"d = {\"k\": \"nine\"}\nmatch d:\n    case {\"k\": w}:\n        print(w)\n",
			"1\n", "nine\n", "roadmap Gap R.305",
		},
	}
	for _, tc := range stillWrong {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Output != tc.compiledAsIs {
				t.Fatalf("%s: the compiled answer changed from %q to %q — %s has been fixed, so delete this row and move the program into the record (the reference prints %q)",
					tc.src, tc.compiledAsIs, res.Output, tc.roadmap, tc.reference)
			}
		})
	}
}

// TestMatchCaptureShapesTheReferenceItselfRefuses pins two forms this backend accepts and CPython
// rejects outright. They are not parity claims; they are the reason those forms have rows.
func TestMatchCaptureShapesTheReferenceItselfRefuses(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want, roadmap string }{
		{
			"an or-pattern whose alternatives bind different names (Gap R.307)",
			"match 5:\n    case 1 | a:\n        print(a)\n",
			"5\n", "roadmap Gap R.307",
		},
		{
			"a positional class pattern needs no __match_args__ (Gap R.304)",
			pointDecl + "match Point(5):\n    case Point(x):\n        print(x)\n",
			"5\n", "roadmap Gap R.304",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Output != tc.want {
				t.Fatalf("printed %q, want the answer this backend gives today (%q) — if it changed, %s needs re-measuring", res.Output, tc.want, tc.roadmap)
			}
		})
	}
}

// captureNamesOf renders a name set for a failure message, sorted so the message is reproducible.
func captureNamesOf(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// sameCaptureNameSet compares two censuses.
func sameCaptureNameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
