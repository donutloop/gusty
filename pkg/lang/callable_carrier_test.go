// Roadmap L11.7 · Gap R.383 (closed by **ADR 0403**), read again one cycle later through the exception
// flag it exposed (**Gap R.241**, closed by **ADR 0404**), plus **Gap R.390** (an interpolation's field over
// a folded container, which answers the payload word) and **Gap R.391** (a `match` on a text subject).
//
// The AST stores the expressions of three constructs in values that are NOT nodes — a type with no
// `Span()` to answer, so nothing that filters a field by "can this hold a node?" ever looks inside it:
//
//	MatchCase      — a `match` arm: its pattern, its guard, its BODY
//	ExceptClause   — an `except` handler: the exception name and its BODY
//	FStringPart    — an interpolation's field: the expression between the braces
//
// `callableWalk` filtered exactly that way, so every question the defunctionalisation pass asks — which
// calls exist, which names are read, which key a hook calls — was answered from a program with those
// three regions cut out. The witness was one statement written twice: `sorted([3, 1, 2], key=lambda v:
// v + base)` compiled outside the `match` and was refused inside it, at exit 1, with the sentence for a
// callable that has no word to travel in.
//
// This file holds four claims:
//
//  1. the reader reads all three carriers, and the programs they hide ANSWER — compiled, on the record,
//     and (in `integration/lambda_carrier_test.go`) against CPython, each beside its un-carried twin;
//  2. the account says so: `carriers_read` counts the calls it met in each carrier, so "the reader never
//     met this call" is a claim a script can falsify rather than one it must trust;
//  3. the handler is REWRITTEN too, now that an accepting arm clears the frame's exception flag on entry
//     — the position ADR 0403 declined was declined for a fault that was never the pass's (roadmap
//     Gap R.241), and a carrier the reader can read and the roads can answer has no veto over it;
//  4. the carrier set is closed, and it is closed by reading `ast.go`: a fourth carrier reaches the reader
//     automatically and fails this case, because a construct worth reading is a decision someone has to
//     make against a witness, not a default a reflection inherits.
package lang

import (
	"encoding/json"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// 1. The carrier family that answers.
// ---------------------------------------------------------------------------

// callableCarrierAnswered are the programs the missing walk refused and the reader now hands to the
// roads that answer. Each is the SAME statement its `def`-twin or its un-carried twin is, so a row that
// stops being right cannot be a silent regression: it is the carrier and nothing else that differs.
func callableCarrierAnswered() []struct{ name, src, want, twin string } {
	return []struct{ name, src, want, twin string }{
		{
			name: "a key lambda in a `match` arm, module level",
			src:  "base = 1\nmatch 1:\n    case 1:\n        print(sorted([3, 1, 2], key=lambda v: v + base))\n",
			want: "[1, 2, 3]\n",
			twin: "base = 1\nprint(sorted([3, 1, 2], key=lambda v: v + base))\n",
		},
		{
			name: "a key lambda in a `match` arm inside a `def`",
			src:  "def outer():\n    base = 1\n    match 1:\n        case 1:\n            print(sorted([3, 1, 2], key=lambda v: v + base))\n\nouter()\n",
			want: "[1, 2, 3]\n",
			twin: "def outer():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda v: v + base))\n\nouter()\n",
		},
		{
			name: "a key lambda in a `match` arm at module level, no capture at all",
			src:  "match 1:\n    case 1:\n        print(sorted([3, 1, 2], key=lambda v: v + 1))\n",
			want: "[1, 2, 3]\n",
			twin: "print(sorted([3, 1, 2], key=lambda v: v + 1))\n",
		},
		{
			name: "a higher-order argument in a `match` arm",
			src:  "def use(f):\n    return f(2)\n\nmatch 1:\n    case 1:\n        print(use(lambda v: v + 1))\n",
			want: "3\n",
			twin: "def use(f):\n    return f(2)\n\nprint(use(lambda v: v + 1))\n",
		},
		{
			name: "a higher-order argument in an interpolation's field",
			src:  "def use(f):\n    return f(2)\n\nprint(f\"{use(lambda v: v * 3)}\")\n",
			want: "6\n",
			twin: "def use(f):\n    return f(2)\n\nprint(use(lambda v: v * 3))\n",
		},
		{
			name: "a higher-order argument in a second `match` arm",
			src:  "def use(f):\n    return f(2)\n\nmatch 2:\n    case 1:\n        print(\"never\")\n    case 2:\n        print(use(lambda v: v - 1))\n",
			want: "1\n",
			twin: "def use(f):\n    return f(2)\n\nprint(use(lambda v: v - 1))\n",
		},
	}
}

// TestWhatTheWholeProgramReaderSeesNowAnswers is the compiled leg of the answered family, and the
// `def`/un-carried twin beside every row: a row that answers differently from its twin is the carrier
// being asked a question the twin was never asked, which is the defect this row is made of.
func TestWhatTheWholeProgramReaderSeesNowAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range callableCarrierAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("Gap R.383: the carrier's copy answers differently from the reference.\n got: %q\nwant: %q\n%s", got, tc.want, tc.src)
			}
			if got := compiledOut(t, tc.twin); got != tc.want {
				t.Fatalf("Gap R.383: the TWIN stopped answering (%q), so the pair no longer localises anything.\n got: %q\nwant: %q\n%s", got, got, tc.want, tc.twin)
			}
			// The record leg: the retired engine never answered these (they did not compile), so the
			// entry is the reference's, written by tools/recmerge and cross-checked here (ADR 0309).
			if got, err := runGoldenStdout(t, tc.src); err != nil {
				t.Fatalf("the record has no answer for a program this row closes: %v", err)
			} else if got != tc.want {
				t.Fatalf("the record and the compiled leg disagree.\nrecord: %q\n  compiled: %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. The handler: read, counted in the account, and — since the frame clears its flag on the accepting
//    edge — rewritten like every other position.
// ---------------------------------------------------------------------------

// callableCarrierHandlerAnswered is the family the reader used to be able to see in a `match` arm and an
// interpolation's field but not in an `except` handler, and which ADR 0403 could read but not rewrite:
// cloning a call out of a handler used to move it out of the frame whose trap catch the handler is
// registered on, so the program that caught its own error died with it unhandled at exit 3. That was the
// exception flag's fault, not the pass's: an accepted exception was cleared only on the edge OUT of the
// arm, so the post-call check of any call the arm wrote saw a flag still saying "in flight" and took the
// run to the handler outside this one (roadmap Gap R.241, closed by ADR 0404's clear on the accepting edge).
// Every row is the SAME statement its un-carried twin is, so a regression cannot hide as "the twin is fine".
func callableCarrierHandlerAnswered() []struct{ name, src, want, twin string } {
	return []struct{ name, src, want, twin string }{
		{
			name: "a key lambda in a handler that the program REACHES",
			src:  "base = 1\ntry:\n    print([1, 2][5])\nexcept IndexError:\n    print(sorted([3, 1, 2], key=lambda v: v + base))\n",
			want: "[1, 2, 3]\n",
			twin: "base = 1\nprint(sorted([3, 1, 2], key=lambda v: v + base))\n",
		},
		{
			name: "a higher-order argument in a handler that the program reaches",
			src:  "def apply(f, v):\n    return f(v)\n\ntry:\n    print([1, 2][5])\nexcept IndexError:\n    print(apply(lambda x: x * 2, 3))\n",
			want: "6\n",
			twin: "def apply(f, v):\n    return f(v)\n\nprint(apply(lambda x: x * 2, 3))\n",
		},
		{
			name: "a callable passed by name into a handler's argument",
			src:  "def use(f):\n    return f(2)\n\ntry:\n    print([1, 2][5])\nexcept IndexError:\n    print(use(lambda v: v + 1))\n",
			want: "3\n",
			twin: "def use(f):\n    return f(2)\n\nprint(use(lambda v: v + 1))\n",
		},
		{
			name: "an applied lambda standing in the handler",
			src:  "try:\n    print([1, 2][5])\nexcept IndexError:\n    print((lambda x: x * 2)(3))\n",
			want: "6\n",
			twin: "print((lambda x: x * 2)(3))\n",
		},
		{
			name: "a user call in the handler, over a raise from the body",
			src:  "def answer():\n    return 7\n\ntry:\n    z = 1 / 0\nexcept ZeroDivisionError:\n    print(answer())\nprint(\"after\")\n",
			want: "7\nafter\n",
			twin: "def answer():\n    return 7\n\nprint(answer())\nprint(\"after\")\n",
		},
	}
}

// TestWhatTheReaderSeesInAHandlerNowAnswers is the compiled leg and the record leg of the handler family,
// with the account beside it: the pass reports the calls it met in the handler (`carriers_read`), rewrote
// the program, and refuses nothing on the ground of where the call was written — because nothing does any
// more. A row here that stops answering is roadmap Gap R.241 coming back.
func TestWhatTheReaderSeesInAHandlerNowAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range callableCarrierHandlerAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("Gap R.241: the handler's copy answers differently from the reference (the flag clear on the\n"+
					"accepting edge is what makes this answer).\n got: %q\nwant: %q\n%s", got, tc.want, tc.src)
			}
			if got := compiledOut(t, tc.twin); got != tc.want {
				t.Fatalf("Gap R.241: the TWIN stopped answering (%q), so the pair no longer localises anything.\n got: %q\nwant: %q\n%s",
					got, got, tc.want, tc.twin)
			}
			if got, err := runGoldenStdout(t, tc.src); err != nil {
				t.Fatalf("the record has no answer for a program this row answers: %v", err)
			} else if got != tc.want {
				t.Fatalf("the record and the compiled leg disagree.\nrecord: %q\n  compiled: %q\n%s", got, tc.want, tc.src)
			}
			doc, err := CallablesJSON(tc.src)
			if err != nil {
				t.Fatalf("the account is a document even for a handler program: %v", err)
			}
			var rep struct {
				CarriersRead map[string]int `json:"carriers_read"`
			}
			if err := json.Unmarshal([]byte(doc), &rep); err != nil {
				t.Fatalf("the account is not JSON: %v\n%s", err, doc)
			}
			if rep.CarriersRead[callableCarrierExcept] == 0 {
				t.Fatalf("Gap R.383: the account does not say it read the call out of the handler (carriers_read=%v):\n%s\n%s",
					rep.CarriersRead, doc, tc.src)
			}
		})
	}
}

// TestEveryCallTheAccountNamesIsADefinitionTheModuleCarries is the soundness invariant the carrier
// descent has to keep: a call the reader reached in a carrier is a call the roads may point at a minted
// declaration, and a `call` to a `define` nobody emits is ADR 0345's forbidden half-rewrite — an
// `llc`/linker mystery rather than a compiler sentence. The mixed program is the witness: the same hook,
// one call in an arm, one in a handler and one in an interpolation's field, so the pass takes some sites
// and leaves others for reasons of shape rather than position.
func TestEveryCallTheAccountNamesIsADefinitionTheModuleCarries(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def use(f):\n    return f(2)\n\nmatch 1:\n    case 1:\n        print(use(lambda v: v + 1))\ntry:\n    z = 1\nexcept ZeroDivisionError:\n    print(use(lambda v: v * 3))\n",
		"def apply(f, v):\n    return f(v)\n\nmatch 1:\n    case 1:\n        print(apply(lambda x: x * 2, 3))\nprint(f\"{apply(lambda x: x * 3, 4)}\")\n",
	} {
		res, err := Compile(src)
		if err != nil {
			if strings.Contains(err.Error(), "llc") || strings.Contains(err.Error(), "undefined value") {
				t.Fatalf("Gap R.383: a carrier program reached the assembler and was rejected — a half-rewrite got that far\n%v\n%s", err, src)
			}
			continue // an honest refusal at the front end is the safe answer
		}
		for _, line := range strings.Split(res.IR, "\n") {
			if !strings.Contains(line, "call ") || !strings.Contains(line, "@gy_") {
				continue
			}
			start := strings.Index(line, "@gy_")
			if start < 0 {
				continue
			}
			rest := line[start+1:]
			end := strings.IndexAny(rest, "( ")
			if end < 0 {
				continue
			}
			name := rest[:end]
			if !strings.Contains(res.IR, "define i32 @"+name+"(") {
				t.Fatalf("Gap R.383: the module calls @%s, which it never defines — a carrier's call was left pointing at a\n"+
					"declaration that was not emitted\n%s\n%s", name, line, src)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// 3. The pins: what the carrier family still answers wrongly, with the row that owns each.
// ---------------------------------------------------------------------------

// callableCarrierPinned are the shapes this cycle MEASURED and does not own. Each pins what the compiled
// leg gives today beside the reference's, so the day a door opens this case fails and the row is deleted
// rather than left certifying a leg that no longer exists. `mode` says how the row is witnessed:
//
//	stdout — the program runs, and the byte is the one pinned (a wrong answer at the success exit, which
//	         is exactly why it is pinned rather than accepted);
//	trap   — the program runs and dies with the sentence pinned, where the reference answers;
//	ir     — the module never reaches the run time at all: the emitted text carries a module GLOBAL in a
//	         word position, which is what llc rejects at ADR 0166's exit-2 class.
type callableCarrierPinnedRow struct {
	name, src, today, want, gap, mode string
}

func callableCarrierPinned() []callableCarrierPinnedRow {
	return []callableCarrierPinnedRow{
		{
			// The answer of `sorted(...)` handed back by a `def` is the folded container read as a number —
			// the same byte its CONTROL prints, which is why this is Gap R.328's door and not Gap R.383's
			// reader. The reader half is closed; this is the answer half the row does not claim.
			name:  "a `def` in a `match` arm answering a folded container prints the handle",
			src:   "def outer():\n    base = 1\n    match 1:\n        case 1:\n            return sorted([3, 1, 2], key=lambda v: v + base)\n\nprint(outer())\n",
			today: "0\n",
			want:  "[1, 2, 3]\n",
			gap:   "Gap R.328",
			mode:  "stdout",
		},
		{
			name:  "an interpolation's field over a folded container prints the handle",
			src:   "print(f\"{sorted([3, 1, 2], key=lambda v: v + 1)}\")\n",
			today: "0\n",
			want:  "[1, 2, 3]\n",
			gap:   "Gap R.390",
			mode:  "stdout",
		},
		{
			// The same field, no key at all: the folded global reaches the printf slot and the assembler
			// rejects the module. Pre-existing on both spellings — the `def`-key twin answers the same way
			// on the parent commit — which is what makes both of them Gap R.390's and not this cycle's.
			name:  "an interpolation's field over a folded container hands the assembler the global",
			src:   "print(f\"{sorted([3, 1, 2])}\")\n",
			today: "i32 @.lst1",
			want:  "[1, 2, 3]\n",
			gap:   "Gap R.390",
			mode:  "ir",
		},
		{
			// The carrier the reader now reaches, on a subject the `match` road cannot compare: an arm that
			// the reference takes is skipped here, at the success exit, with nothing printed. Written with
			// the key lambda this cycle's reader made visible, because that is where it was measured.
			name:  "a `match` on a text subject takes no arm at all",
			src:   "def use(f):\n    return f(2)\n\nmatch \"hi\":\n    case \"hi\":\n        print(use(lambda v: v + 1))\n",
			today: "",
			want:  "3\n",
			gap:   "Gap R.391",
			mode:  "stdout",
		},
	}
}

// TestWhatTheCarrierFamilyStillAnswersWrongIsFiledNotFixed pins each divergence with what the compiled
// leg gives today. A row that stops being true fails: that is the whole point of the table, and the
// reason an answer this cycle does not own is written down instead of quietly accepted.
func TestWhatTheCarrierFamilyStillAnswersWrongIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range callableCarrierPinned() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			switch tc.mode {
			case "ir":
				res, err := Compile(tc.src)
				if err != nil {
					t.Fatalf("%s: the module stopped being emitted at all (%v) — the row pinned a module that reaches the assembler", tc.gap, err)
				}
				if !strings.Contains(res.IR, tc.today) {
					t.Fatalf("%s: the module no longer carries %q — the global-in-a-word-position is gone, and this case is the\n"+
						"one that has to be DELETED, not edited.\n%s", tc.gap, tc.today, tc.src)
				}
			case "trap":
				res, err := Compile(tc.src)
				if err != nil {
					t.Fatalf("%s: the compiled leg refused a program the reference answers (%v) — the row pinned a run-time death, not a refusal", tc.gap, err)
				}
				if out := runIRMayTrap(t, res.IR); !strings.Contains(out, tc.today) {
					t.Fatalf("%s: the program no longer dies with %q.\n got: %q\nwant: %q\n%s", tc.gap, tc.today, out, tc.want, tc.src)
				}
			default:
				out, refused := compiledOutOrRefusal(t, tc.src)
				if refused {
					if !strings.Contains(out, tc.today) {
						t.Fatalf("%s: refused with a different sentence than the row pinned.\n got: %q\n%s", tc.gap, out, tc.src)
					}
					return
				}
				if out != tc.today {
					t.Fatalf("%s: the compiled leg answers %q where the row pinned %q — the reference answers %q, and the day\n"+
						"it answers that is the day this case is DELETED, not edited.\n%s", tc.gap, out, tc.today, tc.want, tc.src)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. The carrier set, closed by reading the AST.
// ---------------------------------------------------------------------------

// TestTheASTCarriesExpressionsInExactlyTheseThreeNonNodeTypes is the guard that keeps this row's claim
// from rotting. `callableWalk` now descends any non-node struct that can hold a node, so a FOURTH
// carrier becomes readable on the day it is added to the AST — and nothing decides whether a REWRITE may
// cross it. This case reads `ast.go`, finds every struct that carries nodes without being one, and names
// it if the set grew: a new carrier is a decision to take, not a default to inherit.
func TestTheASTCarriesExpressionsInExactlyTheseThreeNonNodeTypes(t *testing.T) {
	t.Parallel()
	file, err := goparser.ParseFile(gotoken.NewFileSet(), "ast.go", nil, goparser.ParseComments)
	if err != nil {
		t.Fatalf("the test cannot read ast.go: %v", err)
	}
	// The reflection the PASS asks with, on the three types it names: none of them is a node — which is
	// the whole defect, and the reason a field filter on "can this hold a node?" had to go. If one of them
	// ever grows a Span(), the carrier table shrinks and this case says so.
	nodeIface := reflect.TypeOf((*Node)(nil)).Elem()
	for _, probe := range []any{&MatchCase{}, &ExceptClause{}, &FStringPart{}} {
		if reflect.TypeOf(probe).Implements(nodeIface) {
			t.Fatalf("%s implements Node — it is not a carrier any more, and the reader's claim is stale",
				reflect.TypeOf(probe).Elem().Name())
		}
	}
	nodeMethod := map[string]bool{}
	fields := map[string][]string{}
	goast.Inspect(file, func(n goast.Node) bool {
		switch d := n.(type) {
		case *goast.FuncDecl:
			if d.Recv != nil && len(d.Recv.List) == 1 && d.Name.Name == "Span" {
				nodeMethod[receiverName(d.Recv.List[0].Type)] = true
			}
		case *goast.TypeSpec:
			st, ok := d.Type.(*goast.StructType)
			if !ok {
				break
			}
			names := []string{}
			for _, f := range st.Fields.List {
				names = append(names, exprTypeName(f.Type))
			}
			fields[d.Name.Name] = names
		}
		return true
	})
	got := []string{}
	for name, fieldTypes := range fields {
		if nodeMethod[name] {
			continue // it IS a node: the walk already visited it
		}
		if !anyFieldHoldsNode(fieldTypes, fields, map[string]bool{}) {
			continue
		}
		got = append(got, name)
	}
	// The closed set, asserted as a SET so a fifth carrier and a deleted one both fail.
	want := map[string]bool{"MatchCase": true, "ExceptClause": true, "FStringPart": true}
	if len(got) != len(want) {
		t.Fatalf("the AST carries nodes in %d non-node types, and the pass names %d:\n got: %v\nwant: %v\n\n"+
			"A new carrier is readable the day it exists (that is the point of the reflective descent) and is\n"+
			"NOT yet a position the rewrite has an opinion about: add it to callableCarrierKindOf, decide\n"+
			"whether the rewrite may cross it, and take that decision in callableCarrierKindOf and the roads\n"+
			"before this case goes green (roadmap Gap R.383, ADR 0403; ADR 0404 removed the only veto that\n"+
			"ever keyed on the reading).",
			len(got), len(want), got, wantKeys(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("unexpected carrier %q — see the message beside the count assertion above", name)
		}
	}
	for _, name := range []string{"MatchCase", "ExceptClause", "FStringPart"} {
		if nodeMethod[name] {
			t.Fatalf("%s answers Span() — it is a node now, and the carrier table has to shrink with it", name)
		}
		if !anyFieldHoldsNode(fields[name], fields, map[string]bool{}) {
			t.Fatalf("%s no longer carries a node, so naming it a carrier is a claim with no witness", name)
		}
	}
	// And the pass must name exactly the three, by the strings it compares.
	named := map[string]bool{}
	for _, probe := range []reflect.Type{reflect.TypeOf(&MatchCase{}).Elem(), reflect.TypeOf(&ExceptClause{}).Elem(), reflect.TypeOf(&FStringPart{}).Elem()} {
		named[callableCarrierKindOf(probe)] = true
	}
	if len(named) != 3 || !named[callableCarrierMatch] || !named[callableCarrierExcept] || !named[callableCarrierInterp] {
		t.Fatalf("callableCarrierKindOf does not name the three carriers distinctly: %v", named)
	}
	// Nothing is declined any more, and the emptiness is a measurement: the handler veto went with **ADR
	// 0404**, when the fault behind it turned out to be the frame's exception flag (roadmap Gap R.241)
	// rather than anything about a clone. The witness that a handler's call is therefore REWRITTEN, not
	// merely read, is the account of a handler program: rewritten, with a minted declaration named in it.
	if doc, err := CallablesJSON("def use(f):\n    return f(2)\n\ntry:\n    print([1, 2][5])\nexcept IndexError:\n    print(use(lambda v: v + 1))\n"); err != nil {
		t.Fatalf("the handler's account could not be printed: %v", err)
	} else {
		var rep struct {
			Rewritten bool     `json:"rewritten"`
			Symbols   []string `json:"symbols"`
			Refused   []string `json:"refused"`
		}
		if err := json.Unmarshal([]byte(doc), &rep); err != nil {
			t.Fatalf("the handler's account is not JSON: %v\n%s", err, doc)
		}
		if !rep.Rewritten || len(rep.Symbols) == 0 {
			t.Fatalf("Gap R.241: the pass still will not rewrite a call written in a handler — the veto is back, and with it\n"+
				"the five programs of `callableCarrierHandlerAnswered` refusing:\n%s", doc)
		}
		for _, r := range rep.Refused {
			if strings.Contains(r, "handler") || strings.Contains(r, "carri") {
				t.Fatalf("Gap R.241: the account declines a position by name again: %q\n%s", r, doc)
			}
		}
	}
	// And each carrier name has to be REACHED, by a program the reader really read: a key nobody can
	// produce is an account field that lies.
	reached := map[string]bool{}
	for _, probe := range []string{
		"def use(f):\n    return f(2)\n\nmatch 1:\n    case 1:\n        print(use(lambda v: v + 1))\n",
		"def use(f):\n    return f(2)\n\nprint(f\"{use(lambda v: v + 1)}\")\n",
		"def use(f):\n    return f(2)\n\ntry:\n    print([1, 2][5])\nexcept IndexError:\n    print(use(lambda v: v + 1))\n",
	} {
		prog, perr := parseProgram(probe)
		if perr != nil {
			t.Fatalf("the probe does not parse: %v\n%s", perr, probe)
		}
		for kind, n := range callableCarrierSites(prog) {
			if n > 0 {
				reached[kind] = true
			}
		}
	}
	for _, kind := range []string{callableCarrierMatch, callableCarrierInterp, callableCarrierExcept} {
		if !reached[kind] {
			t.Fatalf("Gap R.383: the carrier %q is named by the pass and reached by no probe — the account's key set has "+
				"a liar in it", kind)
		}
	}
}

func wantKeys(m map[string]bool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func receiverName(e goast.Expr) string {
	switch t := e.(type) {
	case *goast.StarExpr:
		if id, ok := t.X.(*goast.Ident); ok {
			return id.Name
		}
	case *goast.Ident:
		return t.Name
	}
	return ""
}

// exprTypeName renders a field's type the way this question needs it: the element type of a slice, the
// pointee of a pointer, the name of anything else. It is the reader-side twin of the reflection the walk
// itself does, which is the point — the test asks the same question the walk asks.
func exprTypeName(e goast.Expr) string {
	switch t := e.(type) {
	case *goast.ArrayType:
		return exprTypeName(t.Elt)
	case *goast.StarExpr:
		return exprTypeName(t.X)
	case *goast.MapType:
		return exprTypeName(t.Value)
	case *goast.Ident:
		return t.Name
	case *goast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}

// nodeCarrierTypes are the AST's node interfaces by name — the field types whose values the walk visits.
var nodeCarrierTypes = map[string]bool{
	"Node": true, "Stmt": true, "Expr": true,
}

func anyFieldHoldsNode(fieldTypes []string, all map[string][]string, seen map[string]bool) bool {
	for _, f := range fieldTypes {
		if f == "" || seen[f] {
			continue
		}
		if nodeCarrierTypes[f] {
			return true
		}
		inner, ok := all[f]
		if !ok {
			continue
		}
		seen[f] = true
		if anyFieldHoldsNode(inner, all, seen) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 5. The account and the module, one claim.
// ---------------------------------------------------------------------------

// TestTheCarrierAccountNamesOnlySymbolsTheModuleCarries is the schema-to-module witness for the reader
// half (ADR 0166, ADR 0345 §3): the pass may mint a name for a `lambda` it can now SEE in a carrier, and
// every name it mints has to be a `define` in the emitted module — the arm's copy compiles, so the
// account is read on a leg that really ran.
func TestTheCarrierAccountNamesOnlySymbolsTheModuleCarries(t *testing.T) {
	t.Parallel()
	const src = "base = 1\nmatch 1:\n    case 1:\n        print(sorted([3, 1, 2], key=lambda v: v + base))\n"
	doc, err := CallablesJSON(src)
	if err != nil {
		t.Fatalf("the pass could not answer for a program the toolchain compiles: %v", err)
	}
	var rep struct {
		Rewritten         bool           `json:"rewritten"`
		HoistedLambdas    int            `json:"hoisted_lambdas"`
		CaptureParameters int            `json:"capture_parameters"`
		CarriersRead      map[string]int `json:"carriers_read"`
		Symbols           []string       `json:"symbols"`
		Refused           []string       `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, doc)
	}
	if !rep.Rewritten || rep.HoistedLambdas != 1 {
		t.Fatalf("Gap R.383: the reader still cannot see the `lambda` in the arm — the pass minted nothing for it:\n%s\n%s", doc, src)
	}
	if n := rep.CarriersRead[callableCarrierExcept]; n != 0 {
		t.Fatalf("Gap R.383: a `match` arm's call is counted under the handler carrier (%d): %s", n, doc)
	}
	if n := rep.CarriersRead[callableCarrierMatch]; n != 2 {
		t.Fatalf("Gap R.383: the account does not count the arm's two calls — `print(...)` and the `sorted(...)` "+
			"whose key was hoisted (carriers_read=%v):\n%s", rep.CarriersRead, doc)
	}
	if len(rep.Symbols) == 0 {
		t.Fatalf("the pass rewrote the program and named no symbol: %s", doc)
	}
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference answers: %v", err)
	}
	for _, sym := range rep.Symbols {
		if !strings.Contains(res.IR, "define i32 @gy_"+sym+"(") {
			t.Fatalf("the account names %s, which the module does not define\n%s\n%s", sym, doc, src)
		}
	}

	// The same arm one frame deeper, where the name the body reads belongs to the frame rather than to the
	// module: there the minted declaration gains a capture parameter per name, which is the witness that
	// the arm's `lambda` was carried out of a frame and not merely copied (ADR 0395's minting, read at the
	// carrier this pass newly sees — roadmap Gap R.383).
	const nested = "def outer():\n    base = 1\n    match 1:\n        case 1:\n            print(sorted([3, 1, 2], key=lambda v: v + base))\n\nouter()\n"
	nestedDoc, err := CallablesJSON(nested)
	if err != nil {
		t.Fatalf("the pass could not answer for a program the toolchain compiles: %v", err)
	}
	var nestedRep struct {
		Rewritten         bool     `json:"rewritten"`
		HoistedLambdas    int      `json:"hoisted_lambdas"`
		CaptureParameters int      `json:"capture_parameters"`
		Symbols           []string `json:"symbols"`
	}
	if err := json.Unmarshal([]byte(nestedDoc), &nestedRep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, nestedDoc)
	}
	if !nestedRep.Rewritten || nestedRep.HoistedLambdas != 1 || nestedRep.CaptureParameters != 1 {
		t.Fatalf("Gap R.383: the arm's capturing `lambda` was not carried out of its frame:\n%s\n%s", nestedDoc, nested)
	}
	nestedIR, err := Compile(nested)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference answers: %v", err)
	}
	for _, sym := range nestedRep.Symbols {
		if !strings.Contains(nestedIR.IR, "define i32 @gy_"+sym+"(") {
			t.Fatalf("the account names %s, which the module does not define\n%s\n%s", sym, nestedDoc, nested)
		}
	}
}
