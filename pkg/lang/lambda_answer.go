package lang

// lambda_answer.go — the answer of a `lambda` applied where it is written is a value with a kind.
//
// A `def` that hands back a container has a door: ADR 0333's scan reads its `return`, the `ret`
// materialises the object through the builders a binding already uses, and every position that reads
// the answer asks the door that knows the kind (`containerAnswerOperandRead`, ADR 0344). A `lambda`
// applied where it is written had no such door, because it has no declaration for a scan to read: the
// emitter mints one (`emitLambda`) in the middle of lowering the statement that contains it, and by
// then every whole-program table has long been filled. The answer fell through to the one-word road,
// which is not a road a container can travel:
//
//	print((lambda: [1, 2])())
//
// is the reference's `[1, 2]`, and here the hoisted `define` ended `ret i32 @.lst1` — a module constant
// used as an integer — and `llc-20` refused the module at **exit 2**, the code ADR 0166 reserves for a
// compiler bug. With a comprehension in the body instead of a literal the module built and the same
// answer printed the allocator's slot `0` at **exit 0** (roadmap **Gap R.331**, the row one rung
// closer to a refusal). Filed as roadmap **Gap R.382**.
//
// The fix is not a second answer door for lambdas. A door for every position, minted per call site,
// would have to be written eleven times — print, `str`, `repr`, an f-string field, a binding, a
// condition, `len`, a membership test, a loop's iterable, a subscript, a slice — and the divergence
// between the ones that were written and the ones that were not is exactly the family roadmap Gap
// R.261 documents. The fix is to give the `lambda` the one thing the scan needs to see: **a
// declaration**. This pass turns an applied `lambda` whose body provably answers a container into a
// module-level `FuncDef` and points the application's callee at it, so the answer is registered by
// the same scan, stored by the same `ret` road, and read by the same eleven doors as a `def`'s answer
// — including the doors that refuse, which refuse with the same sentence. There is no "lambda answer"
// road to fall out of step with, because there is only the answer road.
//
// # What the pass will hoist
//
// Only a shape it can prove, and the proof is the scan's own, not a new one:
//
//   - the body's free names are all predeclared builtins. A `lambda` reads the frame its APPLICATION is
//     written in; a `def` reads the module. A body that reads a name its own parameters do not bind
//     (`def f(x): print((lambda: [x])())`) would lose that name on the way to the hoisted declaration,
//     so it stays on the emitter's capture road, which carries it as a parameter the call fills. The
//     question is asked by `lambdaFreeNames` — the one door that decides what a body is free in — so
//     the two roads cannot disagree about it (ADR 0232).
//   - the minted declaration lands in ADR 0333's table, described or undescribed — OR the body spells one
//     of the three kinds the `def` roads register and the one-word road cannot render: a TEXT, a VERDICT,
//     or the VOID (`None`). Those three are answered by `textReturningCallables`, by `IsBoolExpr`'s
//     call arm and by `fdReturnsValue`, every one of which reads a module-level `FuncDef` and none of
//     which can see a callee that is a `Lambda` node — which is why `(lambda: True)()` printed `1` where
//     `def a(): return True` prints `True` (roadmap **Gap R.384**, ADR 0368's answer direction). A body
//     that answers a NUMBER or a FLOAT is not this pass's program: its one-word road already answers,
//     and a hoist that bought nothing but risk was not worth the exit-2 class. A body that answers a name
//     the module bound, a call this scan cannot follow, or a container of two different kinds is likewise
//     left where it is — the scans' gates are the rows, and this pass borrows the rows.
//
// The two answer questions are asked by `appliedLambdaProved`, and both are asked of the program AFTER
// the rewrite, which is why the pass runs rounds.
//
// The second gate is asked of the program AFTER the rewrite, which is why the pass runs in rounds:
// `(lambda v: [v, 1])(2.5)` hands its parameter a value that crosses as a (payload, tag) pair, and only
// the call site — visible as a call site once the callee is a name — says so. A declaration the scan
// declines is pulled back and the application is left exactly as the source wrote it. The rounds only
// ever remove a hoist, so they settle; a program that does not settle in four rounds is handed back
// untouched, with the emitter's road and the defect's row intact.
//
// # What the pass does not touch
//
// The tree the front end reports. The rewrite happens on `copyProgramAST`'s copy, exactly as
// defunctionalisation's does, so `--emit-ast`, `gusty fmt` and the checker still see the `lambda` the
// human typed (ADR 0320). The names it mints come from the same `lambda_%d` shape the emitter uses for
// the lambdas it leaves alone, and the emitter's counter is started above every name this pass took —
// two `define`s of one spelling is an exit 2 waiting to be found. A reader meets the declaration as
// `<lambda>`: `CloneOf` carries ADR 0345's anonymous display name, and `callableSurfaceName` already
// knows that a `lambda_`-prefixed symbol is not a name to blame a programmer with.

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

// appliedLambdaSite is one `(lambda …: …)(…)` the pass is considering: the CALL whose callee was the
// `lambda`, the `lambda` that was the callee, and the declaration minted for it once the hoist is
// committed. The index is the candidate's position in the walk, which is the same in every round —
// the rounds decide *which* candidates to hoist, never how many there are.
type appliedLambdaSite struct {
	site  *Call
	lam   *Lambda
	def   *FuncDef
	index int
}

// hoistAppliedLambdaAnswers gives every applied `lambda` whose body provably answers a container a
// declaration of its own, so that the answer is registered, materialised and read by the doors that
// already answer for a `def`. It returns the program the emitter is to lower — the caller's own tree
// when there is nothing to do — and how many `lambda_%d` spellings it consumed, which is where the
// emitter's own counter has to start to avoid a name this pass already took (roadmap L11.7 · Gap
// R.382, ADR 0333, ADR 0344).
func hoistAppliedLambdaAnswers(prog *Program) (*Program, int) {
	return hoistAppliedLambdaAnswersTelling(prog, nil)
}

// hoistAppliedLambdaAnswersTelling is the pass with its account filled in: the same rewrite, and the
// decisions an agent asked for through `gustyc --lambda-answers` (roadmap L11.7 · Gap R.382, ADR 0401).
// The account is written by the same code path the rewrite takes, so the document cannot describe an
// optimisation the compiler did not perform — which is the whole reason an account of a pass is trusted
// (ADR 0379's rule for the defunctionalisation report this one is modelled on).
func hoistAppliedLambdaAnswersTelling(prog *Program, rep *lambdaAnswerAccount) (*Program, int) {
	if prog == nil || len(prog.Stmts) == 0 {
		return prog, 0
	}
	// A body that is nothing but a read of a module-level name the program never rebinds IS the literal
	// the module bound, and the road that emits the read does not know it (roadmap **Gap R.331**). The
	// fold runs before a single whole-program read of this tree — before the candidate walk, before the
	// container-answer scan, before the text-return scan — because a rewrite that arrived after them
	// would be a second opinion about a program they had already answered (**ADR 0345**'s rule, the same
	// one this pass and the defunctionalisation pass above it obey).
	foldAppliedLambdaModuleReads(prog, rep)
	// The names the program itself spells. A `def lambda_0(): …` is legal source, and a `define` of the
	// same spelling is `llc`'s `duplicate symbol` at exit 2, so a collision is avoided rather than
	// compiled and hoped for.
	taken := programDefinedNames(prog)
	// One name per candidate, minted ONCE, before any round. The rounds decide which candidates are
	// hoisted, never what they are called: a name that drifted with the round count would make the
	// module's own text — and every IR row that quotes it — depend on how many rounds the fixpoint
	// needed, which is ADR 0273's "asked of the program, not of the order things were done in" read
	// back at this pass.
	candidates := appliedLambdaSites(prog)
	if len(candidates) == 0 {
		return prog, 0
	}
	names := make([]string, len(candidates))
	minted := 0
	for _, s := range candidates {
		for {
			names[s.index] = fmt.Sprintf("lambda_%d", minted)
			minted++
			if !taken[names[s.index]] {
				break
			}
		}
	}
	// The account, filled by the path the rewrite takes and read out at every exit — a document that
	// described a hoist the compiler did not perform would be worse than no document at all.
	say := func(keep map[int]bool, kinds map[int]containerAnswerKind, answers map[int]string, settled bool, minted int) {
		if rep == nil {
			return
		}
		rep.settled = settled
		rep.names = minted
		for _, s := range candidates {
			if keep[s.index] {
				line := 0
				if s.site != nil {
					line = s.site.Span().Line
				}
				rep.declared = append(rep.declared, lambdaAnswerDeclared{
					symbol: names[s.index], lam: s.lam, kind: kinds[s.index], answer: answers[s.index], line: line,
				})
				continue
			}
			rep.declined = append(rep.declined, lambdaAnswerDeclineFor(s))
		}
	}
	declined := func(minted int) (*Program, int) {
		say(nil, nil, nil, false, minted)
		return prog, 0
	}
	allow := map[int]bool{}
	askForAll := true
	for round := 0; round < 4; round++ {
		fresh := copyProgramAST(prog)
		if fresh == nil {
			return declined(minted)
		}
		sites := appliedLambdaSites(fresh)
		// The copy has to hold the same candidates in the same order, or the pairing between a name and
		// the call that earned it is a guess. Two walks of one tree differ only if the copy changed the
		// tree's shape, and a copy that changes the program is not a copy worth trusting.
		if len(sites) != len(candidates) {
			return declined(minted)
		}
		var defs []*FuncDef
		hoisted := []*appliedLambdaSite{}
		for _, s := range sites {
			if !askForAll && !allow[s.index] {
				continue
			}
			if !appliedLambdaBodyCarriable(s.lam) {
				// The frame half of the gate: this body reads a name the application's frame owns, and a
				// hoisted `lambda` reads the module. The capture road carries it; see Gap R.386.
				continue
			}
			name := names[s.index]
			def := appliedLambdaDeclaration(s.lam, name, s.site.Span())
			// The application keeps its arguments, its span and its identity; only the callee becomes a
			// name, which is the whole of what the emitter needs to lower it as an ordinary call.
			s.site.Fn = &Name{Value: name, Src: s.lam.Span()}
			s.def = def
			defs = append(defs, def)
			hoisted = append(hoisted, s)
		}
		if len(hoisted) == 0 {
			return declined(minted)
		}
		fresh.Stmts = append(fresh.Stmts, stmtsOfFuncDefs(defs)...)
		next, nextKinds, nextAnswers := appliedLambdaProved(fresh, hoisted)
		if askForAll {
			// The trial round: everything answerable went in, and the scan says which of it is an answer.
			askForAll = false
			allow = next
			if len(allow) == 0 {
				// Nothing the scan proved. The tree goes back as it came, and so does the emitter's
				// numbering: a program this pass did not touch must not even see a different `lambda_N`.
				return declined(0)
			}
			continue
		}
		if appliedLambdaSameSet(allow, next) {
			say(allow, nextKinds, nextAnswers, true, minted)
			return fresh, minted
		}
		allow = next
		if len(allow) == 0 {
			return declined(0)
		}
	}
	// The rounds did not settle. That needs one body whose proof depends on another body that a later
	// round withdrew, and the honest answer to a pass that cannot make up its mind is the program the
	// source wrote: the emitter's road still refuses, or still answers wrongly, and the row still owns
	// it. A rewrite that flickers between runs is worse than one that never happens (ADR 0273's rule
	// about a verdict that must not depend on the order things were done in).
	return declined(0)
}

// appliedLambdaFoldRow is one body this pass rewrote: the module-level name the source read, the
// literal the module had bound to it, and the line the read stood on. It exists for the account
// `gustyc --lambda-answers` prints — an agent reading the document has to be able to tell a body the
// pass hoisted from a body the pass rewrote and then left on the emitter's road (roadmap **Gap R.331**,
// **ADR 0401**'s rule that the account is written by the path the rewrite takes).
type appliedLambdaFoldRow struct {
	name    string
	literal string
	line    int
}

// foldAppliedLambdaModuleReads rewrites, in place, an applied `lambda` whose whole body is a read of a
// module-level name the program never rebinds: `(lambda: NAME)()` becomes `(lambda: <literal>)()` where
// `<literal>` is what the module bound to NAME.
//
// The row this closes is two lines of the most ordinary kind — `t = "hey"` above `print((lambda: t)())`
// — and the answer it gives today is the intern table's INDEX, the digit `0`, at the exit code of success
// (**roadmap Gap R.331**; **ADR 0368**'s answer direction, which registers a kind only out of a spelling
// and finds no spelling in a `Name`). The read is not the defect: the module road carries an interned text
// through an `i32` correctly (**ADR 0227**), and the same read written in a `def` answers `hey` because a
// `def`'s `return "hey"` is a spelling the text-return scan can read. Folding the read to the literal the
// module owns therefore does two things at once: the answer kind becomes something a scan can spell, and
// the road that emits it becomes the one that already requests the runtime block the intern call lives in
// (**Gap R.333**'s missing definition, which this program reaches today through a lambda body the request
// was never asked for).
//
// # What the fold will read
//
//   - a body that is EXACTLY one `*Name`. A body that puts the name inside a literal, an operation or a
//     call is a program whose answer is not the module's literal, and folding half of it would be a
//     rewrite that changes what the source said.
//   - a name `moduleEnvFor` still holds: bound once, at module level, to an `IntLit`, `StrLit`, `BoolLit`
//     or `NoneLit`, never rebound, and never declared `global` by any body. That is the same table the
//     `def` road already reads through (`codegen_value.go`), so the fold and the read cannot disagree
//     about which names are values (**ADR 0294**'s one gate) — and it excludes containers by construction,
//     which is what keeps this from minting a fresh object where CPython shares one.
//   - a name no binder owns: not one of the lambda's own parameters, and not one an enclosing `lambda`
//     binds. `lambda t: (lambda: t)()` reads the OUTER `t`, and folding it to the module's literal would
//     be the wrong answer wearing this pass's clothes.
//   - an application written at MODULE level. A body reads its own frame, so a `def`'s or a `class`'s
//     subtree is skipped outright: `(lambda: t)()` written inside a function that also binds `t` is the
//     capture road's, and the row it owns is **Gap R.386** / **Gap R.387**, not this one.
//
// Everything the fold does not touch keeps the road, the answer and the row it has today.
func foldAppliedLambdaModuleReads(p *Program, rep *lambdaAnswerAccount) int {
	if p == nil || len(p.Stmts) == 0 {
		return 0
	}
	consts, _ := moduleEnvFor(p)
	if len(consts) == 0 {
		return 0
	}
	rows := []appliedLambdaFoldRow{}
	shadow := map[string]int{}
	w := &appliedLambdaWalk{
		skip: func(n Node) bool {
			switch n.(type) {
			case *FuncDef, *ClassDef:
				// A body reads its own frame, and a name it binds there is not the module's.
				return true
			}
			return false
		},
		enter: func(n Node) func() {
			lam, ok := n.(*Lambda)
			if !ok || lam == nil {
				return nil
			}
			var bound []string
			for _, pr := range lam.Params {
				if pr == nil {
					continue
				}
				bound = append(bound, pr.Name)
				shadow[pr.Name]++
			}
			return func() {
				for _, nm := range bound {
					shadow[nm]--
				}
			}
		},
		visit: func(n Node) {
			c, isCall := n.(*Call)
			if !isCall || c == nil {
				return
			}
			lam, isLambda := c.Fn.(*Lambda)
			if !isLambda || lam == nil || lam.Body == nil {
				return
			}
			lit, nm, ok := appliedLambdaModuleReadFold(lam, consts, shadow)
			if !ok {
				return
			}
			lam.Body = lit
			rows = append(rows, appliedLambdaFoldRow{name: nm, literal: appliedLambdaFoldSpelling(lit), line: c.Span().Line})
		},
	}
	w.stmts(p.Stmts)
	if rep != nil {
		rep.folds = len(rows)
		rep.foldRows = rows
	}
	return len(rows)
}

// appliedLambdaModuleReadFold asks the three questions the fold may not guess at, and answers the
// literal to write. A nil expression is "no fold", and the body stays exactly as the source wrote it.
func appliedLambdaModuleReadFold(lam *Lambda, consts map[string]Expr, shadow map[string]int) (Expr, string, bool) {
	nm, isName := lam.Body.(*Name)
	if !isName || nm == nil || nm.Value == "" {
		return nil, "", false
	}
	for _, pr := range lam.Params {
		if pr != nil && pr.Name == nm.Value {
			return nil, "", false // the parameter owns the name inside this body
		}
	}
	if shadow[nm.Value] > 0 {
		return nil, "", false // an enclosing `lambda` binds it; this read is not the module's
	}
	lit, ok := consts[nm.Value]
	if !ok || lit == nil {
		return nil, "", false
	}
	cp := copyModuleLiteral(lit, nm.Span())
	if cp == nil {
		return nil, "", false // not one of the four kinds the table admits — no fold
	}
	return cp, nm.Value, true
}

// copyModuleLiteral mints a FRESH node for the folded read rather than sharing the module's own: the
// module's `AssignStmt` still holds the original, and two emitters walking one node is how a per-node
// cache (an interned-text slot, an inferred type, a span) starts answering two programs at once.
// The span is the READ's, not the binding's — the traceback and the line table point at the line the
// program wrote, not at the line the module happened to bind the name on.
func copyModuleLiteral(lit Expr, at Span) Expr {
	switch n := lit.(type) {
	case *IntLit:
		return &IntLit{Value: n.Value, Text: n.Text, Src: at}
	case *StrLit:
		return &StrLit{Value: n.Value, Raw: n.Raw, Triple: n.Triple, Src: at}
	case *BoolLit:
		return &BoolLit{Value: n.Value, Src: at}
	case *NoneLit:
		return &NoneLit{Src: at}
	}
	return nil
}

// appliedLambdaFoldSpelling spells a folded literal for the account — the document has to show an agent
// what the body became, and it is written from the node the pass actually installed.
func appliedLambdaFoldSpelling(lit Expr) string {
	switch n := lit.(type) {
	case *IntLit:
		if n.Text != "" {
			return n.Text
		}
		return strconv.FormatInt(n.Value, 10)
	case *StrLit:
		return strconv.Quote(n.Value)
	case *BoolLit:
		if n.Value {
			return "True"
		}
		return "False"
	case *NoneLit:
		return "None"
	}
	return "?"
}

// appliedLambdaSites collects the applications this pass may take a declaration for, in the order the
// walk meets them, in one order that every round agrees about.
//
// The walk is this pass's own (`appliedLambdaWalk`), not the defunctionalisation pass's `callableWalk`,
// and the reason is a hole that row walked straight into: `callableWalk` follows a field only when the
// slice's element type is a `Node`, and three carriers of expressions in this AST are NOT nodes —
// `FStringPart` (an interpolation's field), `MatchCase` (a `match` arm) and `ExceptClause` (an `except`
// arm). A `lambda` applied inside an f-string field was therefore invisible to the reader, and the
// program `print(f"{(lambda: [1, 2])()}")` kept the hoisted `ret i32 @.lst1` that `llc-20` refuses —
// ADR 0166's exit 2, in the one position ADR 0333's door most needed to answer (roadmap Gap R.382; the
// same hole in the defunctionalisation pass's own reading is filed as roadmap Gap R.383).
func appliedLambdaSites(p *Program) []*appliedLambdaSite {
	out := []*appliedLambdaSite{}
	if p == nil {
		return out
	}
	w := &appliedLambdaWalk{visit: func(n Node) {
		c, isCall := n.(*Call)
		if !isCall || c.Fn == nil {
			return
		}
		lam, isLambda := c.Fn.(*Lambda)
		if !isLambda || lam == nil || lam.Body == nil {
			return
		}
		// Every application is a CANDIDATE, including the ones the frame gate will refuse: the account this
		// pass prints has to say why an application was left alone (roadmap Gap R.382, ADR 0401 — a document
		// that lists only the successes cannot tell an agent which of the two gates closed on the rest).
		out = append(out, &appliedLambdaSite{site: c, lam: lam, index: len(out)})
	}}
	w.stmts(p.Stmts)
	return out
}

// appliedLambdaWalk is the whole-program reader this pass needs: every expression, wherever the AST
// hides it. It descends through struct fields, slice elements and interface values by reflection, and
// treats a value as a node when the AST says it is one — which is the difference from `callableWalk`
// in exactly the three places that matter, listed above. The descent is per-path rather than global
// (the same node reached twice on one path is a cycle and stops) and bounded, so a malformed tree
// costs a declined hoist rather than a hung compile.
const appliedLambdaDepthCap = 4000

type appliedLambdaWalk struct {
	// visit is asked of every node the walk reaches, in the order the tree is read.
	visit  func(Node)
	onPath map[uintptr]bool
	depth  int
	// skip, when set, names the nodes the walk must not descend into at all. It is asked after
	// `visit`, so a reader can still see the node and choose to stop below it.
	skip func(Node) bool
	// enter, when set, is asked of a node the walk is about to descend and what it returns is run
	// when the walk leaves it — the scope discipline a rewrite that binds names needs (roadmap
	// **Gap R.331**: `(lambda t: (lambda: t)())("hey")` reads the OUTER `t`).
	enter func(Node) func()
}

func (w *appliedLambdaWalk) stmts(ss []Stmt) {
	for _, s := range ss {
		w.value(reflect.ValueOf(s))
	}
}

func (w *appliedLambdaWalk) value(v reflect.Value) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return
		}
		// An interface carries a pointer; the walk asks the pointer below, so the identity check that
		// stops a shared node being walked twice on one path is the SAME check whichever way the AST
		// handed the node over.
		w.value(v.Elem())
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		p := v.Pointer()
		if w.onPath == nil {
			w.onPath = map[uintptr]bool{}
		}
		if w.onPath[p] || w.depth >= appliedLambdaDepthCap {
			return
		}
		w.onPath[p] = true
		w.depth++
		defer func() { delete(w.onPath, p); w.depth-- }()
		if n, ok := v.Interface().(Node); ok {
			w.visit(n)
			if w.skip != nil && w.skip(n) {
				return
			}
			if w.enter != nil {
				if leave := w.enter(n); leave != nil {
					defer leave()
				}
				w.value(v.Elem())
				return
			}
		}
		w.value(v.Elem())
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if f := v.Field(i); f.CanInterface() {
				w.value(f)
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			w.value(v.Index(i))
		}
	case reflect.Map:
		// No AST node lives in a map today; the case exists so that one that grows tomorrow is read
		// rather than silently skipped, which is the mistake this walker was written to stop.
		for _, k := range v.MapKeys() {
			w.value(v.MapIndex(k))
		}
	}
}

// appliedLambdaBodyCarriable is the frame half of the gate: a hoisted `lambda` becomes a `def`, and a
// `def` reads the module, not the frame its closure was written in. So the body may read nothing
// except the names its own parameters bind, the builtins, and nothing else — a name the enclosing
// frame owns (`x`, a loop variable, a comprehension target two nodes out) is the closure road's to
// carry, and a declaration that silently read a different `x` would be ADR 0166's bug wearing this
// pass's clothes.
func appliedLambdaBodyCarriable(lam *Lambda) bool {
	if lam == nil || lam.Body == nil {
		return false
	}
	free := map[string]bool{}
	lambdaFreeNames(lam, free)
	for nm := range free {
		if isPredeclaredName(nm) {
			continue
		}
		return false
	}
	return true
}

// appliedLambdaDeclaration is the `def` an applied `lambda` becomes: the same spelling `lambdaFuncDef`
// mints for the emitter's road — one `return` of the body, the parameters copied, nothing shared that
// would let the two roads edit one another's node — plus the span the application was written at, so
// the line table and the traceback point at the program's own line, and the display name ADR 0345
// gives an anonymous function.
func appliedLambdaDeclaration(lam *Lambda, name string, src Span) *FuncDef {
	fd := lambdaFuncDef(lam)
	fd.Name = name
	fd.Src = src
	fd.CloneOf = callableAnonymousName
	return fd
}

// appliedLambdaProved asks the only question that decides whether a hoist stays: does ADR 0333's scan read
// this declaration as answering a container — described, or proved-and-undescribed? Both answers are keeps,
// because the second is the one that turns a printed handle into a refusal (Gap R.261). The second question
// is asked of the body the pass itself wrote — `appliedLambdaBodyRegisters`, the text/verdict/void arm of
// roadmap **Gap R.384** — and a body that answers neither is pulled back: the emitter's road, which for
// these shapes dies in `llc` or prints the allocator's slot, is at least the road the rows describe. The
// kinds travel beside the keeps so the pass can say, in its account, what answer each declaration proved —
// asked of the same scan run, so the document and the rewrite cannot hold two versions of one verdict
// (ADR 0232, ADR 0379).
func appliedLambdaProved(p *Program, sites []*appliedLambdaSite) (map[int]bool, map[int]containerAnswerKind, map[int]string) {
	keep := map[int]bool{}
	kinds := map[int]containerAnswerKind{}
	answers := map[int]string{}
	if p == nil || len(sites) == 0 {
		return keep, kinds, answers
	}
	described, undescribed := containerAnswerScan(p)
	for _, s := range sites {
		if s.def == nil {
			continue
		}
		if ans, ok := described[s.def.Name]; ok {
			keep[s.index], kinds[s.index] = true, ans.kind
			continue
		}
		if ans, ok := undescribed[s.def.Name]; ok {
			keep[s.index], kinds[s.index] = true, ans.kind
			continue
		}
		if word := appliedLambdaBodyRegisters(s.lam); word != "" {
			keep[s.index], answers[s.index] = true, word
		}
	}
	return keep, kinds, answers
}

// The answer words the account publishes for a declaration whose answer is not a container. They are the
// three kinds the `def` roads register and the one-word road prints as its payload: the @str_tab position,
// the 0/1 of a verdict, and the void's word.
const (
	lambdaAnswerText = "text"
	lambdaAnswerBool = "bool"
	lambdaAnswerNone = "none"
)

// appliedLambdaBodyRegisters is the answer half's second question, and it is asked of the ONE spelling both
// roads share — `lambdaFuncDef`'s single `return` of the body — so the pass and the emitter can never agree
// about two different programs. It answers a kind only when the body's answer is WRITTEN in the source as
// that kind: a text, a verdict, or the void. Every one of those three is a fact the `def` side already knows
// how to publish — `textReturningCallables` registers the text, `IsBoolExpr`'s call arm reads the verdict off
// the declaration's own returns, `fdReturnsValue` says the void answers nothing — and none of the three can
// read a callee that is a `Lambda` node, which is the whole of why `(lambda: True)()` printed `1` and
// `(lambda: None)()` printed `0` at **exit 0** while their `def` twins printed the reference's bytes
// (roadmap **Gap R.384**, ADR 0368, ADR 0257).
//
// What it will not claim:
//
//   - a number or a float. Their one-word road answers correctly today (`print((lambda: 5)())` is `5`,
//     `print((lambda: 2.5)())` is `2.5`), so a hoist would be a rewrite with all of the risk — the pair
//     road, the double-domain `define`, ADR 0370's float-frame gate — and none of the gain.
//   - an answer the source does not spell. A body that returns a NAME, a CALL, a comprehension or a
//     container is answered by the scans, not by this predicate; a name the module bound and a call the
//     scan cannot follow stay on the emitter's road, where the door refuses in words instead of guessing
//     a kind from a payload (Gap R.331, Gap R.386).
//   - an attribute, a subscript, a binary operation: shapes whose kind is a run-time question, and a gate
//     that guesses is worse than a gate that declines.
func appliedLambdaBodyRegisters(lam *Lambda) string {
	if lam == nil || lam.Body == nil {
		return ""
	}
	switch lam.Body.(type) {
	case *StrLit:
		return lambdaAnswerText
	case *BoolLit:
		return lambdaAnswerBool
	case *NoneLit:
		return lambdaAnswerNone
	}
	return ""
}

// programDefinedNames is every spelling the program itself declares: a `def`, a `class`, an `extern`,
// a type alias, a module-level binding. A minted `define` that lands on one of them is `llc`'s
// duplicate-symbol complaint at exit 2 — ADR 0166's class, arriving from a pass that had the good
// fortune to choose a name the program was already using. `def lambda_0(): …` is legal source.
func programDefinedNames(p *Program) map[string]bool {
	taken := map[string]bool{}
	if p == nil {
		return taken
	}
	_, names := moduleEnvFor(p)
	for n := range names {
		taken[n] = true
	}
	w := &appliedLambdaWalk{visit: func(n Node) {
		switch x := n.(type) {
		case *FuncDef:
			if x.Name != "" {
				taken[x.Name] = true
			}
		case *ClassDef:
			if x.Name != "" {
				taken[x.Name] = true
			}
		case *ExternDecl:
			if x.Name != "" {
				taken[x.Name] = true
			}
		case *TypeAliasStmt:
			if x.Name != "" {
				taken[x.Name] = true
			}
		case *AssignStmt:
			if x.Target != nil {
				collectNames(x.Target, taken)
			}
		case *ForStmt:
			// A `for` target is a binding of the frame that runs the loop, and at module level that frame
			// is the module: `for lambda_0 in [1]: pass` binds the name as much as `lambda_0 = 1` does.
			collectNames(x.Var, taken)
		case *WithStmt:
			if x.As != nil {
				collectNames(x.As, taken)
			}
		case *MatchStmt:
			for _, c := range x.Cases {
				if c == nil {
					continue
				}
				collectNames(c.Pattern, taken)
			}
		}
	}}
	w.stmts(p.Stmts)
	return taken
}

// appliedLambdaSameSet is the settles? question: a round that withdraws nothing new is the last round.
func appliedLambdaSameSet(a, b map[int]bool) bool {
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

// stmtsOfFuncDefs is the one line a minted declaration needs to join the module's statement list: a
// FuncDef is both a declaration and a statement, and the two interface types do not convert.
func stmtsOfFuncDefs(defs []*FuncDef) []Stmt {
	out := make([]Stmt, 0, len(defs))
	for _, d := range defs {
		out = append(out, d)
	}
	return out
}

// ----------------------------------------------------------------------------
// The account: what the pass decided, as data.
// ----------------------------------------------------------------------------

// lambdaAnswerAccount is the pass's own record of one program: which applications became declarations
// (and under which symbol), which it left on the emitter's road (and why, in the program's words), how
// many `lambda_%d` spellings it consumed, and whether the rounds settled. It is written by the same code
// path the rewrite takes — never by a second reading of the tree — because a document that described a
// hoist the compiler did not perform is worse than no document: an agent would branch on it (ADR 0379's
// rule, asked of this pass by roadmap Gap R.382).
type lambdaAnswerAccount struct {
	declared []lambdaAnswerDeclared
	declined []lambdaAnswerDecline
	// names is how many `lambda_%d` spellings were consumed, including any the pass minted and then
	// withdrew: the emitter's counter has to start above those too, or the module gets two `define`s of
	// one spelling — `llc`'s duplicate-symbol complaint at ADR 0166's exit 2.
	names int
	// folds is how many applied-`lambda` bodies the pass rewrote from a read of a never-rebound
	// module-level name into the literal that name holds (roadmap **Gap R.331**), and foldRows is each
	// one of them, for `gustyc --lambda-answers`.
	folds    int
	foldRows []appliedLambdaFoldRow
	// settled says the rounds agreed; false is the path that hands the program back untouched.
	settled bool
}

// lambdaAnswerDeclared is one application that became a declaration: the symbol the call now names, the
// answer the scan proved, and the `lambda` the program wrote (the front end still reports that node —
// ADR 0320 — and the account quotes its line, not the minted declaration's).
type lambdaAnswerDeclared struct {
	symbol string
	lam    *Lambda
	kind   containerAnswerKind
	// answer is the spelled kind for the three answers that are not containers — "text", "bool", "none"
	// (roadmap Gap R.384). Empty means the answer came from the container scan and `kind` is the word.
	answer string
	line   int
}

// lambdaAnswerDecline is one application the pass left alone. `shape` is the stable name an agent
// branches on; `reason` is the sentence a human reads, naming the program's own name and the row that
// owns the missing road — never a Go type, never a minted symbol (ADR 0166, ADR 0345 §3).
type lambdaAnswerDecline struct {
	shape  string
	reason string
	line   int
}

const (
	// lambdaAnswerShapeCaptures is the frame half of the gate: the body reads a name the application's
	// frame owns, and a hoisted `lambda` reads the module. Roadmap Gap R.386 owns the widening.
	lambdaAnswerShapeCaptures = "captures_frame"
	// lambdaAnswerShapeUnproved is the answer half: no scan can prove one kind along every road through the
	// body. A number and a float answer here because their one-word road already answers; a name, a call and
	// an operation answer here because the scans cannot follow them (Gap R.331, Gap R.384).
	lambdaAnswerShapeUnproved = "answers_nothing_provable"
)

// lambdaAnswerDeclineFor says, for one application the pass did not hoist, which of the two gates closed
// and in what words. The name quoted is the program's own — the reason a reader can act on is the name
// they typed, not a category this compiler invented.
func lambdaAnswerDeclineFor(s *appliedLambdaSite) lambdaAnswerDecline {
	line := 0
	if s != nil && s.site != nil {
		line = s.site.Span().Line
	}
	if appliedLambdaBodyCarriable(s.lam) {
		return lambdaAnswerDecline{
			shape: lambdaAnswerShapeUnproved,
			reason: "the body does not answer one container the scan can prove, nor a text, a verdict or " +
				"None written as a literal, so its answer keeps the one-word road it has always travelled — a " +
				"number and a float answer correctly that way, and a name, a call or an operation is a kind no " +
				"scan can name for a `lambda` yet (roadmap Gap R.331, Gap R.386, ADR 0333)",
			line: line,
		}
	}
	free := map[string]bool{}
	lambdaFreeNames(s.lam, free)
	blocked := []string{}
	for nm := range free {
		if isPredeclaredName(nm) {
			continue
		}
		blocked = append(blocked, nm)
	}
	sort.Strings(blocked)
	who := "a name the frame it is applied in owns"
	if len(blocked) > 0 {
		who = fmt.Sprintf("%q, which the frame it is applied in owns", blocked[0])
	}
	return lambdaAnswerDecline{
		shape:  lambdaAnswerShapeCaptures,
		reason: fmt.Sprintf("the body reads %s: a hoisted `lambda` becomes a `def`, and a `def` reads the module rather than that frame, so this one stays on the capture road that carries the name as a parameter (roadmap Gap R.386, ADR 0370)", who),
		line:   line,
	}
}

// lambdaAnswerDeclJSON / lambdaAnswerDeclineJSON / lambdaAnswerReport are the machine path. The document
// is the account above, with the emitter's naming decision (names_consumed) and the pass's own verdict
// (rewritten, settled) beside it, so a script can ask in one read what the pass did and why (roadmap
// L11.7 · Gap R.382, ADR 0004's schema discipline, ADR 0379's account-of-a-pass shape).
type lambdaAnswerDeclJSON struct {
	Symbol   string   `json:"symbol"`
	Answer   string   `json:"answer"`
	Params   []string `json:"params"`
	Defaults []string `json:"defaults"`
	Line     int      `json:"line"`
}

type lambdaAnswerDeclineJSON struct {
	Shape  string `json:"shape"`
	Reason string `json:"reason"`
	Line   int    `json:"line"`
}

type lambdaAnswerReport struct {
	Tool          string                    `json:"tool"`
	Pass          string                    `json:"pass"`
	Schema        string                    `json:"schema"`
	Rewritten     bool                      `json:"rewritten"`
	Settled       bool                      `json:"settled"`
	Declarations  []lambdaAnswerDeclJSON    `json:"declarations"`
	Symbols       []string                  `json:"symbols"`
	Declined      []lambdaAnswerDeclineJSON `json:"declined"`
	NamesConsumed int                       `json:"names_consumed"`
	Refused       []string                  `json:"refused"`
	// ReadsFolded counts the applied-`lambda` bodies the pass rewrote from a read of a never-rebound
	// module-level name into the literal that name holds; FoldedReads names each one. It is a separate
	// claim from `rewritten`, because a fold does not have to buy a hoist: a body the answer table still
	// declines keeps the emitter's road and travels it as a literal, which is the answer the row owed
	// (roadmap **Gap R.331**).
	ReadsFolded int                    `json:"reads_folded"`
	FoldedReads []lambdaAnswerFoldJSON `json:"folded_reads"`
}

// lambdaAnswerFoldJSON is one folded read in the machine document: the name the program read, the
// literal the module had bound to it, and the line the application stood on.
type lambdaAnswerFoldJSON struct {
	Name    string `json:"name"`
	Literal string `json:"literal"`
	Line    int    `json:"line"`
}

// LambdaAnswersJSON asks the pass its own questions and prints the answers: which applied `lambda`s
// became declarations, under what symbol, with what answer kind, and which were left on the emitter's
// road with which reason. A source that is not gusty at all is the tool's compile-error case, not the
// pass's verdict — the same line `--callables` draws between the two (ADR 0006, ADR 0379).
func LambdaAnswersJSON(src string) (string, error) {
	doc := lambdaAnswerReport{
		Tool:   callableToolName,
		Pass:   "hoist-applied-lambda-answers",
		Schema: "gusty.lambda-answers.v1",
	}
	prog, err := parseProgram(src)
	if err != nil {
		return "", err
	}
	var acc lambdaAnswerAccount
	out, consumed := hoistAppliedLambdaAnswersTelling(prog, &acc)
	doc.Rewritten = out != prog
	doc.Settled = acc.settled
	doc.NamesConsumed = consumed
	for _, d := range acc.declared {
		entry := lambdaAnswerDeclJSON{Symbol: d.symbol, Answer: d.answer, Line: d.line}
		if entry.Answer == "" {
			entry.Answer = d.kind.String()
		}
		if entry.Answer == "" {
			// The scan proved a container answer without naming one kind — the undescribed half, which
			// is what turns a printed handle into a refusal (Gap R.261). Say that rather than nothing.
			entry.Answer = "container"
		}
		if d.lam != nil {
			for _, p := range d.lam.Params {
				if p == nil {
					continue
				}
				entry.Params = append(entry.Params, p.Name)
				if p.Default != nil {
					entry.Defaults = append(entry.Defaults, p.Name)
				}
			}
		}
		if entry.Params == nil {
			entry.Params = []string{}
		}
		if entry.Defaults == nil {
			entry.Defaults = []string{}
		}
		doc.Declarations = append(doc.Declarations, entry)
		doc.Symbols = append(doc.Symbols, d.symbol)
	}
	for _, d := range acc.declined {
		doc.Declined = append(doc.Declined, lambdaAnswerDeclineJSON{Shape: d.shape, Reason: d.reason, Line: d.line})
	}
	if doc.Declarations == nil {
		doc.Declarations = []lambdaAnswerDeclJSON{}
	}
	if doc.Symbols == nil {
		doc.Symbols = []string{}
	}
	if doc.Declined == nil {
		doc.Declined = []lambdaAnswerDeclineJSON{}
	}
	if doc.Refused == nil {
		doc.Refused = []string{}
	}
	doc.ReadsFolded = acc.folds
	for _, f := range acc.foldRows {
		doc.FoldedReads = append(doc.FoldedReads, lambdaAnswerFoldJSON{Name: f.name, Literal: f.literal, Line: f.line})
	}
	if doc.FoldedReads == nil {
		doc.FoldedReads = []lambdaAnswerFoldJSON{}
	}
	// A fold that bought no hoist still rewrote the program: the body the emitter lowers is not the body
	// the parser built. `rewritten` says what happened to the tree, not what happened to the hoist list.
	if acc.folds > 0 {
		doc.Rewritten = true
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
