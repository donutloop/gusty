package lang

import (
	"fmt"
	"sort"
	"strings"
)

// boolargs.go — roadmap Gap R.111 (L11.1's remaining bool item, ADR 0257's unfinished edge).
//
// A verdict has always been a value to this language and never a value to its value word: a
// bool is stored as the number a comparison produced, and what makes it print as `True` is
// the front end's answer to "is this expression a verdict?" (ADR 0257). That question is
// asked of an AST, and it works everywhere the *expression* is visible — `print(True)`,
// `print(1 == 1)`, `print([True, 1])` (ADR 0259), `print(max([True, 0]))` (ADR 0261).
//
// What it could not see was a verdict crossing a call boundary:
//
//	def show(f):
//	    print(f)      # the print road asks IsBoolExpr(Name{"f"}) and the answer is not there
//
//	show(True)        # gusty: 1     CPython: True
//
// Inside `show`, `f` is a `*Name`, and `BoolEnv.Vars` records the names whose *assignment*
// produced a bool — a parameter is never assigned, so the print road fell back to the number
// the slot holds. The information is not lost, only unfetched: the call site wrote `True`, and
// the compiler read it.
//
// So this file fetches it. `boolArgKinds` walks the whole program and answers, for each
// parameter of each function and each uniquely-named method, whether *every* argument the
// program ever passes into that position is a verdict. It is the same question, asked the same
// way, that `strArgKinds` asks for interned text (Gap J.5) and `stringFillingParams` asks for
// containers — a parameter's kind is a fact about the call sites, and the alternative is the
// runtime guessing from a word that does not carry a kind.
//
// Three rules keep it honest, and they are the rules the sibling analyses learned:
//
//   - **A parameter is marked only when no call site contradicts it.** One `show(1)` anywhere
//     unmarks it, and the print road keeps printing the number it always printed. An answer
//     the compiler is unsure of degrades to yesterday's behaviour, never to a new wrong one.
//   - **A variable forwards a verdict only while nothing has stored a non-verdict into it.**
//     `flag = True; show(flag)` marks; `flag = 3` anywhere in that scope drops the candidate,
//     because the compiler is reading a *flow*, not a declaration.
//   - **A method is matched by name only when one class owns that name.** Two classes with a
//     `run` method mean two parameter lists, and marking one from the other's call site would
//     print `True` for a value that was never a verdict.

// boolArgKinds maps a function (or uniquely-named method) to the parameter indices that every
// call site hands a verdict to. The key is the callable's name as written; `boolArgNames` is the
// same answer keyed by parameter name, which is what the print road and the bool predicate read.
func boolArgKinds(prog *Program) map[string]map[int]bool {
	return verdictFacts(prog).kinds
}

// boolLocalsOf answers the sibling question: which names, inside each callable's own body, hold
// a verdict. The print road inside a body needs the parameter table; the print road at the CALL
// needs to know what a call hands back, and `def isit(): b = True; return b` answers `True` in
// CPython and `1` compiled because bodyReturnsOnlyBools asked the question with the caller's
// names, where `b` had never been heard of (roadmap Gap R.111).
func boolLocalsOf(prog *Program) map[string]map[string]bool {
	return verdictFacts(prog).locals
}

// verdictFacts is the one walk that answers both questions, because they are the same walk: the
// call sites decide a parameter, and the assignments plus those parameters decide a name.
type verdictFactsT struct {
	kinds map[string]map[int]bool
	// locals maps a callable to the names whose own bindings inside its body hold a verdict.
	locals map[string]map[string]bool
}

// of is the BoolEnv.CalleeVars shape: a nil map means "this function has nothing to report",
// which the predicate reads as "ask with the caller's names", the pre-existing behaviour.
func (v verdictFactsT) of(fn string) map[string]bool { return v.locals[fn] }

func verdictFacts(prog *Program) verdictFactsT {
	var none verdictFactsT
	if prog == nil {
		return none
	}
	callables := map[string]*FuncDef{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if _, dup := callables[fd.Name]; dup {
				// Two functions with one name: the last definition wins at call time and the
				// first may still be reachable through a closure, so neither is safe to mark.
				callables[fd.Name] = nil
				continue
			}
			callables[fd.Name] = fd
		}
	}
	for name, fd := range callables {
		if fd == nil {
			delete(callables, name)
		}
	}
	// Methods, but only the ones a call could resolve unambiguously.
	for _, st := range prog.Stmts {
		cd, ok := st.(*ClassDef)
		if !ok {
			continue
		}
		for _, st2 := range cd.Body {
			m, ok := st2.(*FuncDef)
			if !ok || m == nil {
				continue
			}
			if _, seen := callables[m.Name]; seen {
				callables[m.Name] = nil // ambiguous: two callables, one name
				continue
			}
			callables[m.Name] = m
		}
	}
	for name, fd := range callables {
		if fd == nil {
			delete(callables, name)
		}
	}
	if len(callables) == 0 {
		return none
	}

	// The environment the verdict question asks with. Lookup resolves a called name so that a
	// forwarding call (`def outer(f): inner(f)`) can be read, and Shadowed keeps a program's own
	// `def any(...)` from being answered out of the builtin table.
	// The method-side reading: which method one callable owns, and which class a receiver hands
	// back. The verdict question asks it so `print(C().yes())` is a verdict too (roadmap Gap R.316).
	mcs := methodCensus(prog, nil)
	env := func(vars map[string]bool) BoolEnv {
		return BoolEnv{
			Vars:         vars,
			Lookup:       func(n string) *FuncDef { return callables[n] },
			Shadowed:     func(n string) bool { _, ok := callables[n]; return ok },
			MethodLookup: mcs.lookup, SelfLookup: mcs.lookupClass, FieldVerdict: mcs.fieldIsVerdict,
		}
	}

	// Which plain variables hold a verdict, per scope. A name ever assigned a non-verdict is
	// dropped from propagation — the same rule scopeHasString enforces for text, for the same
	// reason: a parameter called `flag` in one function must not inherit another function's flag.
	boolVars := map[string]bool{}
	notBool := map[string]bool{}

	// Evidence against a position: the same callable reached with a number somewhere else.
	// Without it `show(True)` alone would mark the parameter and `show(1)` would then print
	// True for a number — a new wrong answer, which the rules above forbid.
	notBoolArg := map[string]map[int]bool{}
	out := map[string]map[int]bool{}
	mark := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		m := out[fn]
		if m == nil {
			m = map[int]bool{}
			out[fn] = m
		}
		m[idx] = true
	}
	negate := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		m := notBoolArg[fn]
		if m == nil {
			m = map[int]bool{}
			notBoolArg[fn] = m
		}
		m[idx] = true
	}

	// ask is the whole question in one place: is this argument a verdict, as far as the
	// compiler can tell from the AST and what it has learned about names so far?
	ask := func(scope string, a Expr, vars map[string]bool) bool {
		if a == nil {
			return false
		}
		if kw, ok := a.(*KeywordArg); ok {
			a = kw.Value
		}
		v := env(vars)
		if IsBoolExpr(a, v) {
			return true
		}
		if nm, ok := a.(*Name); ok {
			return vars[scopeKey(scope, nm.Value)] || vars[scopeKey("", nm.Value)]
		}
		return false
	}
	// refuses is the answer's mirror: an argument that provably is NOT a verdict, which unmarks
	// the position no matter how many verdicts arrive at it elsewhere.
	refuses := func(a Expr) bool {
		if a == nil {
			return false
		}
		if kw, ok := a.(*KeywordArg); ok {
			a = kw.Value
		}
		return definitelyNotABool(a)
	}

	w := heapASTWalker{}
	w.assign = func(scope, target string, value Expr, annot *Type) {
		key := scopeKey(scope, target)
		if annot.IsBool() || IsBoolExpr(value, env(boolVars)) || value == nil {
			boolVars[key] = true
			return
		}
		notBool[key] = true
		delete(boolVars, key)
	}
	// oneCall reads one call site. `viaMethod` is true for `obj.m(x)`, where the receiver fills
	// the callable's first parameter: `def m(self, f)` called as `z.m(True)` puts True in slot 1,
	// and reading it as slot 0 would mark `self` a verdict and leave `f` a number.
	oneCall := func(scope, fn string, args []Expr, viaMethod bool) {
		fd, ok := callables[fn]
		if !ok || fd == nil {
			return
		}
		pos := 0
		if viaMethod && len(fd.Params) > 0 && (fd.Params[0] == nil || fd.Params[0].Name == "self" || fd.Params[0].Name == "cls") {
			pos = 1
		}
		for _, a := range args {
			idx := pos
			if kw, isKw := a.(*KeywordArg); isKw {
				idx = -1
				for i, p := range fd.Params {
					if p != nil && p.Name == kw.Name {
						idx = i
						break
					}
				}
			}
			if ask(scope, a, boolVars) {
				mark(fn, idx)
			} else if refuses(a) {
				negate(fn, idx)
			}
			if _, isKw := a.(*KeywordArg); !isKw {
				pos++
			}
		}
	}
	w.call = func(scope, fn string, args []Expr) { oneCall(scope, fn, args, false) }
	w.attrCall = func(scope, fn string, args []Expr) { oneCall(scope, fn, args, true) }

	seed := func() {
		for _, fn := range sortedCallableNames(callables) {
			fd := callables[fn]
			for i, p := range fd.Params {
				if p == nil {
					continue
				}
				if p.Annot.IsBool() || IsBoolExpr(p.Default, env(boolVars)) {
					mark(fn, i)
				}
			}
		}
		w.scope = ""
		w.stmts(prog.Stmts)
	}

	seed()
	// Fixed point: a parameter known to receive a verdict makes the body's own name a verdict,
	// so a forwarding call discovered on a later pass (`def outer(f): inner(f)`) marks inner too.
	for pass := 0; pass < 8; pass++ {
		before := snapshotBoolKinds(out)
		for _, fn := range sortedCallableNames(callables) {
			fd := callables[fn]
			for i := range out[fn] {
				if i >= len(fd.Params) || fd.Params[i] == nil {
					continue
				}
				name := fd.Params[i].Name
				if notBool[scopeKey(fn, name)] || notBool[scopeKey("", name)] {
					continue
				}
				boolVars[scopeKey(fn, name)] = true
			}
		}
		seed()
		if snapshotBoolKinds(out) == before {
			break
		}
	}

	// Contradiction wins: a position any call site filled with a non-verdict is not a verdict
	// position, whatever the majority did.
	for fn, neg := range notBoolArg {
		for i := range neg {
			delete(out[fn], i)
		}
		if len(out[fn]) == 0 {
			delete(out, fn)
		}
	}
	// A parameter the body itself rebinds to a non-verdict is not a verdict parameter either:
	// `def show(f): print(f); f = 3` prints f before the rebind, and the print road's own
	// last-assignment-wins rule already answered 1 there.
	for fn, m := range out {
		fd := callables[fn]
		for i := range m {
			if i >= len(fd.Params) || fd.Params[i] == nil {
				delete(m, i)
				continue
			}
			if notBool[scopeKey(fn, fd.Params[i].Name)] {
				delete(m, i)
			}
		}
		if len(m) == 0 {
			delete(out, fn)
		}
	}
	locals := map[string]map[string]bool{}
	for key := range boolVars {
		fn, name := splitScopeKey(key)
		if notBool[key] || (fn != "" && notBool[scopeKey("", name)]) {
			continue
		}
		m := locals[fn]
		if m == nil {
			m = map[string]bool{}
			locals[fn] = m
		}
		m[name] = true
	}
	// A parameter the call sites all filled with a verdict is a verdict inside the body even
	// when the body never assigns it — that is the whole of Gap R.111 — so the locals table
	// carries it too, and a forwarding `return f` is read correctly by the caller.
	for fn, m := range out {
		fd := callables[fn]
		for i := range m {
			if i < len(fd.Params) && fd.Params[i] != nil {
				if locals[fn] == nil {
					locals[fn] = map[string]bool{}
				}
				locals[fn][fd.Params[i].Name] = true
			}
		}
	}
	return verdictFactsT{kinds: out, locals: locals}
}

// definitelyNotABool is the evidence that a position is not filled by a verdict. It is a
// closed list of shapes, not the negation of IsBoolExpr: an expression this file cannot judge
// (a name it has not learned, a call it cannot resolve, an attribute it cannot see through) is
// neither marked nor negated, which is what keeps an unresolved program printing exactly what
// it printed before this file existed.
func definitelyNotABool(e Expr) bool {
	switch t := e.(type) {
	case *BoolLit:
		return false
	case *IntLit, *FloatLit, *StrLit, *NoneLit, *ListLit, *DictLit, *SetLit, *Tuple, *Comp, *Lambda:
		return true
	case *UnOp:
		// `-x`, `~x` are numbers about x; only `not x` is a verdict.
		return t.Op != "not"
	case *BinOp:
		switch t.Op {
		case "+", "-", "*", "/", "//", "%", "**", "&", "|", "^", "<<", ">>":
			return true
		}
		return false
	case *Index:
		return true
	default:
		return false
	}
}

// splitScopeKey is scopeKey's inverse. The two are kept here together so a change to the
// separator cannot silently break the split.
func splitScopeKey(key string) (scope, name string) {
	if i := strings.IndexByte(key, 0x1f); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

func sortedCallableNames(m map[string]*FuncDef) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func snapshotBoolKinds(m map[string]map[int]bool) string {
	fns := make([]string, 0, len(m))
	for k := range m {
		fns = append(fns, k)
	}
	sort.Strings(fns)
	var sb strings.Builder
	for _, fn := range fns {
		idxs := make([]int, 0, len(m[fn]))
		for i := range m[fn] {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		fmt.Fprintf(&sb, "%s:", fn)
		for _, i := range idxs {
			fmt.Fprintf(&sb, "%d,", i)
		}
		sb.WriteString(";")
	}
	return sb.String()
}
