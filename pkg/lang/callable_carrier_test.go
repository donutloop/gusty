// Roadmap L11.7 · Gap R.383 (closed by **ADR 0403**), with the two rows the closure measured and
// filed rather than swallowed: **Gap R.389** (an `except` handler's trap catch, which the rewrite must
// not cross) and **Gap R.390** (an interpolation's field over a folded container, which answers the
// payload word).
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
// This file holds three claims, each on its own leg:
//
//  1. the reader now reads all three carriers, and the programs they hide ANSWER — asserted against
//     the record and, in `integration/lambda_carrier_test.go`, against CPython;
//  2. the REWRITE is a separate decision, and the one carrier the measurement condemns (an `except`
//     handler, whose frame owns the trap catch the handler is registered on) is declined in words, with
//     the line and the row, by the pass's own account;
//  3. the carrier set is closed, and it is closed by reading `ast.go` — a fourth carrier reaches the
//     reader automatically and fails this case, because a rewrite's opinion about it has to be taken
//     deliberately rather than inherited.
package lang

import (
	"encoding/json"
	"fmt"
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
// 2. The carrier the rewrite declines, in words.
// ---------------------------------------------------------------------------

func callableCarrierDeclined() []struct {
	name   string
	src    string
	line   int // the line the CALL was written on, which the account quotes
	symbol string
} {
	return []struct {
		name   string
		src    string
		line   int
		symbol string
	}{
		{
			name:   "a key lambda in an `except` handler",
			src:    "def outer():\n    base = 1\n    try:\n        x = 1\n    except ZeroDivisionError:\n        return sorted([3, 1, 2], key=lambda v: v + base)\n    return 0\n\nprint(outer())\n",
			line:   6,
			symbol: "sorted",
		},
		{
			name:   "a key lambda in a second handler of the same `try`",
			src:    "def outer():\n    base = 1\n    try:\n        x = 1\n    except ValueError:\n        return sorted([3, 1, 2], key=lambda v: v + base)\n    except ZeroDivisionError:\n        return 0\n    return 0\n\nprint(outer())\n",
			line:   6,
			symbol: "sorted",
		},
		{
			name:   "a higher-order argument in an `except` handler",
			src:    "def use(f):\n    return f(2)\n\ntry:\n    z = 1\nexcept ZeroDivisionError:\n    print(use(lambda v: v + 1))\n",
			line:   7,
			symbol: "use",
		},
	}
}

// TestWhatTheGateRefusesNamesTheCarrierAndTheRow is the machine path of the restraint: the reader saw
// the call (that is what Gap R.383 bought), so the account has to SAY it declined it — the carrier by
// name, the line the call sits on, and the row that owns the missing road — rather than fall back to
// the generic "nothing for me to do here" sentence, which would describe a program the pass never read.
func TestWhatTheGateRefusesNamesTheCarrierAndTheRow(t *testing.T) {
	t.Parallel()
	for _, tc := range callableCarrierDeclined() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, err := CallablesJSON(tc.src)
			if err != nil {
				t.Fatalf("a decline is still a document: %v", err)
			}
			var rep struct {
				Rewritten        bool           `json:"rewritten"`
				CarriersDeclined map[string]int `json:"carriers_declined"`
				Refused          []string       `json:"refused"`
			}
			if err := json.Unmarshal([]byte(doc), &rep); err != nil {
				t.Fatalf("the account is not JSON: %v\n%s", err, doc)
			}
			if rep.Rewritten {
				t.Fatalf("Gap R.389: the pass rewrote a call written in an `except` handler — the program that catches\n" +
					"its own error today would die unhandled at exit 3. The account:\n" + doc + "\n" + tc.src)
			}
			if n := rep.CarriersDeclined[callableCarrierExcept]; n == 0 {
				t.Fatalf("Gap R.389: the account does not say it declined the handler position (carriers_declined=%v):\n%s\n%s",
					rep.CarriersDeclined, doc, tc.src)
			}
			var joined string
			for _, r := range rep.Refused {
				joined += r + "\n"
			}
			for _, want := range []string{"except", fmt.Sprintf("line %d", tc.line), "Gap R.389"} {
				if !strings.Contains(joined, want) {
					t.Fatalf("Gap R.389: the refusal does not name %q.\naccount:\n%s\n%s", want, joined, tc.src)
				}
			}
			// And the program is refused, not answered: exit 1 with the ordinary road's sentence, which is
			// what it got before the reader could see the call at all. A refusal beats the wrong answer —
			// ADR 0166's ordering, worn by the trap that escapes.
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("Gap R.389: the handler's call compiled — either the gate is gone or the trap crossing landed, "+
					"and this case is the one that has to be DELETED when that happens\n%s", tc.src)
			}
			if !strings.Contains(msg, "refused") && !strings.Contains(msg, "not a function") && !strings.Contains(msg, "lambda") {
				t.Fatalf("Gap R.389: the handler is not refused by a sentence about the call:\n%s", msg)
			}
		})
	}
}

// TestTheGateNeverLeavesACallAtADefinitionThatWasTakenAway is the soundness belt beside the gate. A
// declined site keeps the name the program wrote; a clone road that took that name's definition out of
// the module would leave a `call` to a `define` nobody emits, which is a link-time mystery and ADR 0345's
// forbidden half-rewrite. The mixed program is the witness: the same hook, one call in an arm and one in
// a handler.
func TestTheGateNeverLeavesACallAtADefinitionThatWasTakenAway(t *testing.T) {
	t.Parallel()
	const mixed = "def use(f):\n    return f(2)\n\nmatch 1:\n    case 1:\n        print(use(lambda v: v + 1))\ntry:\n    z = 1\nexcept ZeroDivisionError:\n    print(use(lambda v: v * 3))\n"
	res, err := Compile(mixed)
	if err != nil {
		// A refusal is the safe answer and the one the row expects. It must not be an `llc` rejection.
		if strings.Contains(err.Error(), "llc") || strings.Contains(err.Error(), "undefined value") {
			t.Fatalf("Gap R.389: the mixed program reached llc and was rejected — a half-rewrite reached the assembler\n%v\n%s", err, mixed)
		}
		return
	}
	// If it compiled, no call may name a symbol the module does not define.
	for _, line := range strings.Split(res.IR, "\n") {
		if !strings.Contains(line, "call i32 @gy_use") && !strings.Contains(line, "call i32 @gy_sorted") {
			continue
		}
		for _, sym := range []string{"gy_use", "gy_sorted"} {
			name := "@" + sym
			if strings.Contains(line, name) && !strings.Contains(res.IR, "define i32 "+name+"(") {
				t.Fatalf("Gap R.389: the module calls %s, which it never defines — the gate left a call at a clone it took away\n%s\n%s",
					name, line, mixed)
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
			// A call written in a handler loses the frame's trap catch. Pre-existing: the parent commit
			// dies the same way on a program with NO lambda in it (`def answer(): return 7`, called from
			// the handler) — the witness that this is the trap table's row and not the reader's, and the
			// reason the gate declines the position instead of shipping this byte.
			name:  "a call in an `except` handler lets the frame's own trap escape",
			src:   "def answer():\n    return 7\n\ntry:\n    z = 1 / 0\nexcept ZeroDivisionError:\n    print(answer())\n",
			today: "ZeroDivisionError",
			want:  "7\n",
			gap:   "Gap R.389",
			mode:  "trap",
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
			"whether a clone may leave it, and put that answer in callableRewriteDeclinedCarriers, before\n"+
			"this case goes green (roadmap Gap R.383, ADR 0403).",
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
	if !callableRewriteDeclinedCarriers[callableCarrierExcept] {
		t.Fatalf("Gap R.389: the handler is not declined — the gate that keeps a caught trap from escaping is gone")
	}
	if callableRewriteDeclinedCarriers[callableCarrierMatch] || callableRewriteDeclinedCarriers[callableCarrierInterp] {
		t.Fatalf("Gap R.383: a carrier the measurement ANSWERS is declined — the row is open again")
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
		CarriersDeclined  map[string]int `json:"carriers_declined"`
		Symbols           []string       `json:"symbols"`
		Refused           []string       `json:"refused"`
	}
	if err := json.Unmarshal([]byte(doc), &rep); err != nil {
		t.Fatalf("the account is not JSON: %v\n%s", err, doc)
	}
	if !rep.Rewritten || rep.HoistedLambdas != 1 {
		t.Fatalf("Gap R.383: the reader still cannot see the `lambda` in the arm — the pass minted nothing for it:\n%s\n%s", doc, src)
	}
	if n := rep.CarriersDeclined[callableCarrierExcept]; n != 0 {
		t.Fatalf("Gap R.383: a `match` arm is counted as a declined carrier (%d): %s", n, doc)
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
