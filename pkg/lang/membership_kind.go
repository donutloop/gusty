package lang

// A name's kind, and a call's answer kind, where the PROGRAM — not the expression — promises it.
//
// roadmap **Gap R.522** · **ADR 0453** (the residue **ADR 0452** filed)
//
// ADR 0452 gave the membership door the kinds the source spells AT the question: `print(1 in 5)` raises
// `argument of type 'int' is not iterable`, because the `5` is written there, it is one of the four
// `notIterableKinds`, and it has no members. What that row left standing — pinned with the bytes of the day in
// a residue table that failed the moment this file landed, and was DELETED rather than edited — was the same
// question asked of a value the source NAMES rather than writes:
//
//	x = 5                          def f():
//	print(1 in x)             →        return 5
//	                                print(1 in f())
//
// both `False` at the exit code of success, while `print(1 in xs[0])` over the very same number raised,
// because there the value arrives with a tag beside its payload and the OBJECT is asked.
//
// The reason ADR 0452 stopped is written in its own file and repeated in the row: a name's kind, read out of
// the fourteen name-keyed notebooks the compiler keeps, is a SUMMARY of the assignments a pass has seen, not
// a PROMISE about the value the read receives. The counterexample is one statement long:
//
//	x = [1]
//	if cond():
//	    x = 5
//	print(1 in x)
//
// The last binding emitted says `int`; the run time may be standing in the other arm holding a list; and a
// door that read the summary would trade the working `True` for a raise. That trade is forbidden in this
// house in as many words — "a fold that raises too eagerly turns a working language feature into a trap,
// which is worse than the bug this row filed" (`membership_fold_test.go`), "a gate that guessed is worse
// than a gate that declines" (`answer_kind.go`); **ADR 0299** measured the same cliff for the verdict table
// and rejected it.
//
// So this file asks a DIFFERENT question, and the question is about the program rather than about a name:
//
//	did every road that could ever have written this name write THIS LITERAL?
//
// One binder, in the frame that owns the name, and that binder is a plain assignment of a spelled no-member
// literal: then the value standing at the read IS that literal whatever the program does, and the raise is
// sound. Anything else retires the name and the door stays out — a second assignment; an assignment inside
// an `if`/`for`/`while`/`try` arm, where "it ran" is a run-time fact; an augmented assignment; a loop or
// comprehension target; an `except … as`; a `with … as`; a `match` capture; an `import`; a name some other
// frame writes through `global` (**ADR 0448**'s road, the newest one in the tree and the one a name-keyed
// summary would never have noticed).
//
// A call's answer is asked the same way, of the BODY instead of of a binder list: every `return` road spells
// a literal of one no-member kind, control cannot fall off the end (or the fall-off spells the void, which
// is the same kind), and the body is an ordinary one — not `async`, not decorated, not a generator. Those
// are the answer direction's own four questions (`answerBodyArms`), asked of the ONE body walk
// (`answerBodyReturns`) and the ONE table of kinds (`notIterableKinds`) — **ADR 0232** again — and the walk
// reaches an answer the direction cannot: a `return 1.5` is un-nameable to `answerArmOf`, and that shape
// was an `llc` rejection at **exit 2** — `call i32 @rt_contains(i32 %t6, …)` with `%t6` a `double`,
// **ADR 0166**'s compiler-bug code spent on a program CPython stops with a sentence.
//
// What stays owed, and is pinned as owed by `TestWhatTheMembershipDoorStillDeclines`: a parameter's kind
// (only the call sites know it), a name bound inside an arm, a name bound by an expression rather than
// spelled (`y = x`, `n = a + 1`), a builtin whose answer kind no door names (`bool`), and any value that
// arrives as a (payload, tag) pair — the last is **L11.1**'s tagged word at a one-word position
// (**Gap R.146**), not this door's.

// spelledScalar is one name's promise about its own kind: the reference's name for the kind its single
// binding spells, and the line that binding stands on. The line is what makes the promise cover the read
// rather than the whole program: a read written ABOVE its only binding is a read of a name that has no
// value yet — CPython answers that `NameError`, and this front end refuses it before codegen — so the door
// declines instead of raising a TypeError for a program whose real complaint is different.
type spelledScalar struct {
	kind string
	line int
}

// spelledScalarFrame is one frame's answer to "which of my names carry a kind promise". Frames chain,
// because a read standing inside an `if` arm that binds nothing still reads the module's `x`; a name the
// inner frame binds at all hides the outer frame's entry for it, which is the shadowing rule read off the
// binder counts rather than written beside them.
type spelledScalarFrame struct {
	parent *spelledScalarFrame
	// kindOf holds only the names whose WHOLE story is one top-level assignment of a spelled no-member
	// literal. A name absent from it has no promise; a name present in `bound` without being present here
	// was bound by some other road.
	kindOf map[string]spelledScalar
	// containerOf holds the names whose WHOLE story is one or more top-level assignments of a spelled
	// CONTAINER literal that all spell the SAME family (roadmap Gap R.586 — `programPromisesASet`). The rule
	// is kindOf's, widened by exactly one measure: kindOf may demand a single binder because its candidates
	// are scalars of one kind, while a container name rebound to a second literal of the family it already
	// had is still that family whatever the run does — so this table counts the binders it spelled and lets
	// the prune below compare the two counts. Any binder this pass did not spell — one inside an `if` arm, a
	// loop target, an augmented assignment, a `with … as` — leaves the counts unequal and retires the name.
	containerOf map[string]spelledContainer
	// containerBinders counts the container literals pass 2 spelled per name. It is the numerator of the
	// promise; `bound` is the denominator.
	containerBinders map[string]int
	// bound counts every binder the frame's own statements run — every road, at every depth of control
	// flow, including the ones that hide inside expressions (a comprehension's target, a walrus). A nested
	// `def` or `class` is NOT descended: its binders belong to its own frame.
	bound map[string]int
}

// spelledContainer is a name's promise about its own container family, and the last line a binding of it
// stands on, for the same reason a spelledScalar carries one: a read written ABOVE its bindings reads names
// that have no value yet, and a door that answered for it would be answering a program whose complaint is a
// NameError. `dead` marks a name that spelled two different families — the promise is off, and the name may
// not be re-granted by a later literal of either one.
type spelledContainer struct {
	family string
	line   int
	dead   bool
}

// beginSpelledScalars installs the table for the frame about to be emitted and returns the closure that puts
// the previous frame's table back — the same save/restore shape `enterBody` gives the float table, the slot
// table and the frame-target table, for the same reason: what one body bound is not a fact about the next.
//
// `cross` names the names ANOTHER frame writes — the program's `global` declarations for the module, this
// body's own for a frame — because such a name is written where this scan cannot see it, which is exactly
// how a promise becomes a lie.
func (g *irGen) beginSpelledScalars(body []Stmt, cross map[string]bool) func() {
	saved := g.spelledScalars
	g.spelledScalars = scanSpelledScalars(g, body, g.spelledScalars, cross)
	return func() { g.spelledScalars = saved }
}

// scanSpelledScalars reads a body once and answers, per name, whether one assignment is its whole life
// story. It is called at the frame's entry, before any of it is lowered, for the reason every other
// program-wide reading gives: the answer must not depend on which statement the emitter happens to be
// standing in (**ADR 0343**'s rule about a run-scoped fact).
func scanSpelledScalars(g *irGen, body []Stmt, parent *spelledScalarFrame, cross map[string]bool) *spelledScalarFrame {
	f := &spelledScalarFrame{parent: parent, kindOf: map[string]spelledScalar{}, containerOf: map[string]spelledContainer{}, containerBinders: map[string]int{}, bound: map[string]int{}}
	// Pass 1: every binder road anywhere in this frame.
	countStmtBinders(body, f.bound)
	// Pass 2: the candidates — a top-level `x = <no-member literal>`. The candidate table is only trusted
	// when pass 1 counted exactly one binder for the name, which is then this very statement.
	for _, st := range body {
		as, ok := st.(*AssignStmt)
		if !ok || as == nil || as.Annot != nil {
			continue
		}
		nm, ok := as.Target.(*Name)
		if !ok || nm == nil || nm.Value == "" {
			continue
		}
		// The container promise, recorded beside the scalar one from the same statement and pruned by the same
		// two rules below: a name whose whole story is `s = {1, 2}` is a set whatever the program does next
		// (roadmap Gap R.586).
		if fam := containerLiteralFamily(g, as.Value); fam != "" {
			line := 0
			if sp := as.Span(); !sp.IsZero() {
				line = sp.Line
			}
			had, seen := f.containerOf[nm.Value]
			if seen && had.dead {
				f.containerBinders[nm.Value]++
				continue
			}
			if seen && had.family != fam {
				// Two spelled bindings, two families: no family is this name's promise.
				f.containerOf[nm.Value] = spelledContainer{dead: true}
				f.containerBinders[nm.Value]++
				continue
			}
			if seen && line < had.line {
				line = had.line
			}
			f.containerOf[nm.Value] = spelledContainer{family: fam, line: line}
			f.containerBinders[nm.Value]++
		}
		tag, spelled := spelledNoMemberKindLiteral(as.Value)
		if !spelled {
			// One wider arm: the value is a CALL every road of which answers a spelled no-member literal
			// (`x = f()`), which is the same promise one statement further out — the name carries what the
			// callee's every-road answers, and the call door's own guards (a name the program assigned to, a
			// parameter standing in front of the `def`) are asked before the kind is taken.
			if c, isCall := as.Value.(*Call); isCall && g.canBindSettledCall() {
				if k, answerable := g.callAnswersOnlyNoMemberKind(c); answerable {
					line := 0
					if sp := as.Span(); !sp.IsZero() {
						line = sp.Line
					}
					if _, seen := f.kindOf[nm.Value]; seen {
						delete(f.kindOf, nm.Value)
						continue
					}
					f.kindOf[nm.Value] = spelledScalar{kind: k, line: line}
					continue
				}
			}
			continue
		}
		kind, known := notIterableKindName(tag)
		if !known {
			continue
		}
		if _, seen := f.kindOf[nm.Value]; seen {
			delete(f.kindOf, nm.Value) // two spelled bindings, one name: the later one wins at run time
			continue
		}
		line := 0
		if sp := as.Span(); !sp.IsZero() {
			line = sp.Line
		}
		f.kindOf[nm.Value] = spelledScalar{kind: kind, line: line}
	}
	// A name another frame writes is not this frame's promise, and a name this frame binds by any road other
	// than the candidate assignment is not one either.
	for nm := range cross {
		delete(f.kindOf, nm)
		delete(f.containerOf, nm)
		f.bound[nm]++
	}
	for nm, n := range f.bound {
		if n != 1 {
			delete(f.kindOf, nm)
		}
		// The container promise survives several binders only when this pass spelled every one of them, and
		// spelled them all into one family (`dead` above). Anything else — a binder in an arm, a loop target,
		// a `with … as`, a value that is not a literal — is a run-time fact, and this file does not trade an
		// answer for a run-time fact (**ADR 0452**, **ADR 0453**).
		if f.containerBinders[nm] != n {
			delete(f.containerOf, nm)
			continue
		}
		if c, seen := f.containerOf[nm]; seen && c.dead {
			delete(f.containerOf, nm)
		}
	}
	return f
}

// containerLiteralFamily names the family of a spelled container literal — the one question
// `containerShapeOf` already answers, asked here so the promise table and the operator doors cannot
// disagree about what `{1, 2}` is (**ADR 0232**). `set()` is the empty set spelled by its call, and it is
// the same literal: `emptyContainerCall` is the door that already recognises it.
func containerLiteralFamily(g *irGen, e Expr) string {
	switch e.(type) {
	case *ListLit, *DictLit, *SetLit, *Tuple:
		return containerShapeOf(e)
	}
	if c, isCall := e.(*Call); isCall && g != nil {
		if lit, isEmpty := g.emptyContainerCall(c); isEmpty {
			return containerShapeOf(lit)
		}
	}
	return ""
}

// programPromisesASet is the answer a door may build a TRAP-FREE decision on: does this expression denote a
// SET, and is that a fact about the PROGRAM rather than a summary of what a pass happened to see?
//
// roadmap **Gap R.586** · **ADR 0453** (the discipline this copies) · **ADR 0232**
//
// The membership door needs the set's kind to decide whether the haystack converts the needle to the
// frozenset it hashes as, and it cannot get that from the name-keyed claims. Those are a SUMMARY of the
// assignments a pass has seen, and one statement breaks them:
//
//	s = {"a": 1}
//	if no():
//	    s = {1, 2}
//	print({1} in s)      # the run-time haystack is the DICT; the reference raises TypeError
//
// A door that read the last-emitted claim would skip the raise and answer `False` — a wrong answer at the
// exit code of success, which is the trade **ADR 0452** and **ADR 0453** both refuse to make. So a NAME is
// trusted only on the promise this file's frame already gives a scalar: exactly one binder, in the frame
// that owns the name, that binds a spelled set literal, standing BEHIND the read, and not a name another
// frame writes through `global`.
//
// One reading is left, and it is the strongest of the three: a name the source never binds at all. Those
// names are the compiler's own — a container answer materialised under the rooted slot a container variable
// occupies (`__cansN`, container_return.go) — and the only road that can write one is the line that minted
// it, so its recorded kind IS a promise. Every other shape declines, and the door keeps the answer it always
// gave: a missing exemption is a filed gap, a wrong one is an exit 0.
func (g *irGen) programPromisesASet(e Expr, readLine int) bool {
	if e == nil || g == nil {
		return false
	}
	if containerLiteralFamily(g, e) == "set" {
		return true
	}
	nm, isName := e.(*Name)
	if !isName || nm.Value == "" {
		return false
	}
	if g.spelledScalars == nil {
		// No frame: this read stands somewhere the binder walk never ran, and a claim read from here is a
		// summary again. Decline, which is the safe direction — the gate keeps raising what it raises today.
		return false
	}
	for f := g.spelledScalars; f != nil; f = f.parent {
		if n := f.bound[nm.Value]; n > 0 {
			// This frame owns the name; whatever an outer frame said about the same spelling is another variable.
			if n != f.containerBinders[nm.Value] {
				return false
			}
			c, ok := f.containerOf[nm.Value]
			if !ok || c.dead || c.family != "set" {
				return false
			}
			return c.line > 0 && readLine > c.line
		}
	}
	// No frame binds it: a compiler-minted slot, whose kind the minting road wrote and nothing else can
	// change. Ask the one door that knows what a minted name holds.
	if _, declared := g.allocd[nm.Value]; !declared {
		return false
	}
	if g.runtimeSets[nm.Value] {
		return true
	}
	if lit, has := g.containerLits[nm.Value]; has {
		_, isSet := lit.(*SetLit)
		return isSet
	}
	return false
}

// spelledScalarKindName answers the membership door: does this NAME stand for a value whose kind the program
// promises, and which kind? The walk stops at the first frame that knows the name at all — that frame owns
// it, and whatever an outer frame said about the same spelling describes a different variable.
func (g *irGen) spelledScalarKindName(name string, readLine int) (string, bool) {
	if g == nil || name == "" {
		return "", false
	}
	for f := g.spelledScalars; f != nil; f = f.parent {
		if sp, ok := f.kindOf[name]; ok {
			if f.bound[name] != 1 {
				return "", false
			}
			// The binding must be BEHIND the read. A read with no line, or a binding with none, is declined:
			// a wrong line number is worse than no verdict, which is the rule `atLocation` states.
			if sp.line <= 0 || readLine <= sp.line {
				return "", false
			}
			return sp.kind, true
		}
		if f.bound[name] > 0 {
			return "", false
		}
	}
	return "", false
}

// callLiteralAnswerKinds reads the program's module-level `def`s and records the ONE no-member kind every
// road of each answers. A name declared twice at top level is dropped: two bodies, one name, is no body —
// **ADR 0299**'s conservatism about a duplicated name, read off the walk that fills the table rather than
// written beside it, the way `answerKindPlanOf` drops a duplicated class.
func callLiteralAnswerKinds(prog []Stmt) map[string]string {
	out := map[string]string{}
	seen := map[string]bool{}
	for _, st := range prog {
		fd, ok := st.(*FuncDef)
		if !ok || fd == nil || fd.Name == "" {
			continue
		}
		if seen[fd.Name] {
			delete(out, fd.Name)
			continue
		}
		seen[fd.Name] = true
		if k, answerable := bodySpellsOneNoMemberKind(fd); answerable {
			out[fd.Name] = k
		}
	}
	return out
}

// bodySpellsOneNoMemberKind asks a body whether every road that can answer answers a literal of one and the
// same no-member kind — the answer direction's question, asked of its ONE body walk.
func bodySpellsOneNoMemberKind(fd *FuncDef) (string, bool) {
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || containsYield(fd.Body) {
		return "", false
	}
	returns, fallsOff := answerBodyReturns(fd.Body)
	kinds := map[string]bool{}
	if len(returns) == 0 {
		// No `return` road at all: every road answers the void, which is one kind. `answerBodyArms` declines
		// this body and leaves the verdict to `bodyAnswersTheVoid`; the membership door needs the KIND, so it
		// asks the walk rather than a second table.
		kinds["NoneType"] = true
	}
	for _, r := range returns {
		if r == nil {
			return "", false
		}
		if r.Expr == nil {
			kinds["NoneType"] = true // a bare `return` answers the void
			continue
		}
		tag, spelled := spelledNoMemberKindLiteral(r.Expr)
		if !spelled {
			return "", false // one road the source does not spell: the answer is a run-time fact
		}
		k, known := notIterableKindName(tag)
		if !known {
			return "", false
		}
		kinds[k] = true
	}
	if fallsOff {
		kinds["NoneType"] = true
	}
	if len(kinds) != 1 {
		return "", false
	}
	for k := range kinds {
		return k, true
	}
	return "", false
}

// assignedNamesEverywhere is every name the program writes a VALUE into, anywhere, by any assignment road —
// an assignment, an augmented one, a loop or comprehension target, an `except … as`, a `with … as`, a
// capture, a walrus. It is the call door's guard: `f = lambda: [1]` under a `def f(): return 5` makes `f()` a
// call of the LAMBDA, and answering the module `def`'s kind for that call raises over a value the program
// prints. A `def`/`class`/`import`/`type` name is NOT in this table — declaring is not assigning — which is
// why the walk exposes `declares` (roadmap **Gap R.522**, **ADR 0453**).
func assignedNamesEverywhere(prog []Stmt) map[string]bool {
	count := map[string]int{}
	countStmtBindersDecl(prog, count, false)
	out := make(map[string]bool, len(count))
	for nm := range count {
		out[nm] = true
	}
	return out
}

// programGlobalWrites is every name ANY frame of the program declares `global`. The MODULE's own table has
// to drop them: such a name is written by the function that declared it, which is a body this scan does not
// read. `globalDeclsIn` answers the question for one body and stops at a nested `def`; walking those bodies
// too is the whole addition here (**ADR 0448**'s road seen from the side that must not trust a name).
func programGlobalWrites(prog []Stmt) map[string]bool {
	out := map[string]bool{}
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for nm := range globalDeclsIn(sts) {
			out[nm] = true
		}
		for _, st := range sts {
			switch s := st.(type) {
			case *FuncDef:
				if s != nil {
					walk(s.Body)
				}
			case *ClassDef:
				if s != nil {
					walk(s.Body)
				}
			}
		}
	}
	walk(prog)
	return out
}

// canBindSettledCall is the frame's permission to let an assignment of a CALL count as a kind promise: the
// program-wide call table has to exist (the module's kind scan runs before any frame is entered) and the
// calling frame must be the module's or a body whose parameters are known — the same two guards
// `callAnswersOnlyNoMemberKind` asks, asked here so a BINDING never takes a kind the READ would decline.
func (g *irGen) canBindSettledCall() bool {
	return g != nil && g.callKinds != nil
}

// callAnswersOnlyNoMemberKind answers the membership door for a call: does every road of this callee answer
// a literal of one no-member kind? A builtin is asked the ONE door that names a builtin's answer kind
// (`orderKindOfBuiltin`, the table **ADR 0450**'s comparison door reads); a user `def` is asked the
// program-wide table above — and only while nothing in the program has assigned to its name (an `f = lambda:
// [1]` under a `def f(): return 5` makes `f()` a call of the LAMBDA, and answering the module def's kind for
// that call would raise over a value the program prints) and while the name is not one of the calling
// frame's own parameters (there the callable road runs the argument's body, not this one).
func (g *irGen) callAnswersOnlyNoMemberKind(c *Call) (string, bool) {
	if c == nil {
		return "", false
	}
	nm, isName := c.Fn.(*Name)
	if !isName || nm == nil || nm.Value == "" {
		return "", false
	}
	name := nm.Value
	if g.assignedNames[name] {
		return "", false
	}
	if g.curFD != nil {
		for _, p := range g.curFD.Params {
			if p != nil && p.Name == name {
				return "", false
			}
		}
	}
	if k, known := g.orderKindOfBuiltin(c, name); known {
		if notIterableKind(k) {
			return k, true
		}
		return "", false
	}
	k, ok := g.callKinds[name]
	if !ok || !notIterableKind(k) {
		return "", false
	}
	return k, true
}

// notIterableKind is the set test asked of a kind NAME rather than a tag. The four kinds are the ones
// `notIterableKinds` already lists, so a fifth cannot join this door without joining the table the runtime
// road walks as well (**ADR 0232**, and `TestTheFoldAndTheRuntimeObjectDoorReadOneTable`).
func notIterableKind(kind string) bool {
	for _, k := range notIterableKinds {
		if k.Name == kind {
			return true
		}
	}
	return false
}

// spelledNoMemberKindLiteral names, as a tag, the kind an expression SPELLS itself as, where that kind has
// no members. It is the arm list `notIterableHaystackKind` has always had, taken out so the name behind
// `print(1 in x)` and the answer behind `print(1 in f())` can be asked the same way the literal is — and so
// a kind added here cannot be missing there.
func spelledNoMemberKindLiteral(e Expr) (int32, bool) {
	switch e.(type) {
	case *IntLit:
		return int32(TagInt), true
	case *FloatLit:
		return int32(TagFloat), true
	case *BoolLit:
		return int32(TagBool), true
	case *NoneLit:
		return int32(TagNone), true
	}
	return 0, false
}

// notIterableKindName reads a tag back out of the one table.
func notIterableKindName(tag int32) (string, bool) {
	for _, k := range notIterableKinds {
		if k.Tag == tag {
			return k.Name, true
		}
	}
	return "", false
}

// countStmtBinders counts every road that BINDS a name in the statement lists it is given, at every depth of
// control flow, and stops at a nested `def` or `class`: those bodies are separate frames, counted when the
// frame itself is scanned. The list is the AST's own, read statement by statement the way `answer_kind.go`'s
// `walkStmt` reads it, because a scan that FORGETS a binder road is a door that raises where the reference
// answers — the one failure mode this file is not allowed to have.
func countStmtBinders(sts []Stmt, bound map[string]int) {
	countStmtBindersDecl(sts, bound, true)
}

// countStmtBindersDecl is the walk with one decision exposed: whether a `def`, a `class`, an `import` or a
// `type` counts as binding its name. A frame's own binder count must include them — `x = 5` beside a `class
// x:` is not a promise about `x` — while the CALL door's guard asks about assignments only, because a
// `def f()` is not a program writing a VALUE into `f` (roadmap Gap R.522, ADR 0453).
func countStmtBindersDecl(sts []Stmt, bound map[string]int, declares bool) {
	for _, st := range sts {
		countStmtBinderDecl(st, bound, declares)
	}
}

func countStmtBinderDecl(st Stmt, bound map[string]int, declares bool) {
	decl := func(name string) {
		if declares && name != "" {
			bound[name]++
		}
	}
	var markTargets func(e Expr)
	markTargets = func(e Expr) {
		switch t := e.(type) {
		case *Name:
			if t != nil && t.Value != "" {
				bound[t.Value]++
			}
		case *Tuple:
			if t != nil {
				for _, el := range t.Elems {
					markTargets(el)
				}
			}
		case *ListLit:
			if t != nil {
				for _, el := range t.Elems {
					markTargets(el)
				}
			}
		}
	}
	switch s := st.(type) {
	case nil:
	case *AssignStmt:
		if s == nil {
			return
		}
		markTargets(s.Target)
		countExprBinders(s.Value, bound)
	case *AugAssignStmt:
		if s == nil {
			return
		}
		markTargets(s.Target)
		countExprBinders(s.Value, bound)
	case *ExprStmt:
		if s != nil {
			countExprBinders(s.Expr, bound)
		}
	case *ReturnStmt:
		if s != nil {
			countExprBinders(s.Expr, bound)
		}
	case *RaiseStmt:
		if s != nil {
			countExprBinders(s.Expr, bound)
		}
	case *YieldStmt:
		if s != nil {
			countExprBinders(s.Expr, bound)
		}
	case *YieldFromStmt:
		if s != nil {
			countExprBinders(s.Expr, bound)
		}
	case *IfStmt:
		if s == nil {
			return
		}
		countExprBinders(s.Cond, bound)
		countStmtBindersDecl(s.Then, bound, declares)
		countStmtBindersDecl(s.Else, bound, declares)
		for _, e := range s.Elifs {
			countStmtBinderDecl(e, bound, declares)
		}
	case *WhileStmt:
		if s == nil {
			return
		}
		countExprBinders(s.Cond, bound)
		countStmtBindersDecl(s.Body, bound, declares)
		countStmtBindersDecl(s.Else, bound, declares)
	case *ForStmt:
		if s == nil {
			return
		}
		for _, nm := range loopVarNames(s.Var) {
			bound[nm]++
		}
		countExprBinders(s.Iter, bound)
		countStmtBindersDecl(s.Body, bound, declares)
		countStmtBindersDecl(s.Else, bound, declares)
	case *TryStmt:
		if s == nil {
			return
		}
		countStmtBindersDecl(s.Body, bound, declares)
		countStmtBindersDecl(s.Finally, bound, declares)
		for _, cl := range s.Excepts {
			if cl == nil {
				continue
			}
			if cl.As != "" {
				bound[cl.As]++
			}
			countStmtBindersDecl(cl.Body, bound, declares)
		}
	case *WithStmt:
		if s == nil {
			return
		}
		countExprBinders(s.Expr, bound)
		if s.As != nil && s.As.Value != "" {
			bound[s.As.Value]++
		}
		countStmtBindersDecl(s.Body, bound, declares)
	case *MatchStmt:
		if s == nil {
			return
		}
		countExprBinders(s.Subject, bound)
		for _, cs := range s.Cases {
			if cs == nil {
				continue
			}
			// A pattern is not an expression that READS names: a bare name in one BINDS, which is why the
			// pattern and its alternatives are walked with namesBind.
			countExprPatternBinders(cs.Pattern, bound)
			for _, alt := range cs.Or {
				countExprPatternBinders(alt, bound)
			}
			countExprBinders(cs.Guard, bound)
			countStmtBindersDecl(cs.Body, bound, declares)
		}
	case *ImportStmt:
		if s != nil {
			decl(s.Module)
		}
	case *TypeAliasStmt:
		if s != nil {
			decl(s.Name)
		}
	case *GlobalStmt:
		// A declaration is not a binding — but it does make the name ANOTHER frame's, which is the `cross`
		// table this scan is handed rather than something counted here.
	case *FuncDef:
		if s == nil {
			return
		}
		decl(s.Name) // the name the `def` gives itself; its body is another frame's
		for _, p := range s.Params {
			if p != nil {
				countExprBinders(p.Default, bound)
			}
		}
		for _, d := range s.Decorators {
			countExprBinders(d, bound)
		}
	case *ClassDef:
		if s != nil {
			decl(s.Name)
		}
	case *ExternDecl:
		if s != nil {
			decl(s.Name)
		}
	}
}

// countExprBinders walks an expression for the binder roads that hide INSIDE expressions — a comprehension or
// generator clause target, which this backend binds in the frame that HOSTS it (**ADR 0381**), and a walrus —
// and stops at a `lambda`, whose parameters and body belong to the lambda's own frame. A plain `*Name` is a
// READ here, not a binder; that distinction is what `countExprPatternBinders` exists to invert.
func countExprBinders(e Expr, bound map[string]int) { countExprBinder(e, bound, false) }

// countExprPatternBinders is the same walk for a `match` pattern, where a name is a CAPTURE.
func countExprPatternBinders(e Expr, bound map[string]int) { countExprBinder(e, bound, true) }

func countExprBinder(e Expr, bound map[string]int, namesBind bool) {
	if e == nil {
		return
	}
	mark := func(n *Name) {
		if namesBind && n != nil && n.Value != "" {
			bound[n.Value]++
		}
	}
	switch n := e.(type) {
	case *Name:
		mark(n)
	case *AssignExpr:
		if n == nil {
			return
		}
		if n.Name != nil && n.Name.Value != "" {
			bound[n.Name.Value]++
		}
		countExprBinder(n.Value, bound, namesBind)
	case *Attr:
		if n != nil {
			countExprBinder(n.Obj, bound, namesBind)
		}
	case *BinOp:
		if n != nil {
			countExprBinder(n.L, bound, namesBind)
			countExprBinder(n.R, bound, namesBind)
		}
	case *UnOp:
		if n != nil {
			countExprBinder(n.X, bound, namesBind)
		}
	case *CondExpr:
		if n != nil {
			countExprBinder(n.Cond, bound, namesBind)
			countExprBinder(n.If, bound, namesBind)
			countExprBinder(n.Else, bound, namesBind)
		}
	case *ChainCompare:
		if n != nil {
			for _, o := range n.Operands {
				countExprBinder(o, bound, namesBind)
			}
		}
	case *Call:
		if n == nil {
			return
		}
		countExprBinder(n.Fn, bound, namesBind)
		for _, a := range n.Args {
			countExprBinder(a, bound, namesBind)
		}
	case *Index:
		if n != nil {
			countExprBinder(n.Obj, bound, namesBind)
			countExprBinder(n.Idx, bound, namesBind)
		}
	case *Slice:
		if n != nil {
			countExprBinder(n.Obj, bound, namesBind)
			countExprBinder(n.Low, bound, namesBind)
			countExprBinder(n.High, bound, namesBind)
			countExprBinder(n.Step, bound, namesBind)
		}
	case *Tuple:
		if n != nil {
			for _, el := range n.Elems {
				countExprBinder(el, bound, namesBind)
			}
		}
	case *ListLit:
		if n != nil {
			for _, el := range n.Elems {
				countExprBinder(el, bound, namesBind)
			}
		}
	case *SetLit:
		if n != nil {
			for _, el := range n.Elems {
				countExprBinder(el, bound, namesBind)
			}
		}
	case *DictLit:
		if n != nil {
			for _, k := range n.Keys {
				countExprBinder(k, bound, namesBind)
			}
			for _, v := range n.Vals {
				countExprBinder(v, bound, namesBind)
			}
		}
	case *FString:
		if n != nil {
			for _, p := range n.Parts {
				countExprBinder(p.Expr, bound, namesBind)
			}
		}
	case *AwaitExpr:
		if n != nil {
			countExprBinder(n.Expr, bound, namesBind)
		}
	case *Comp:
		if n == nil {
			return
		}
		for _, el := range n.Elems {
			countExprBinder(el, bound, namesBind)
		}
		for _, k := range n.Keys {
			countExprBinder(k, bound, namesBind)
		}
		for _, v := range n.Vals {
			countExprBinder(v, bound, namesBind)
		}
		for _, cl := range n.compClauses() {
			if cl == nil {
				continue
			}
			if cl.Target != nil && cl.Target.Value != "" {
				bound[cl.Target.Value]++
			}
			countExprBinder(cl.Iter, bound, namesBind)
			countExprBinder(cl.Cond, bound, namesBind)
		}
	case *Generator:
		if n == nil {
			return
		}
		for _, el := range n.Elems {
			countExprBinder(el, bound, namesBind)
		}
		for _, cl := range n.compClauses() {
			if cl == nil {
				continue
			}
			if cl.Target != nil && cl.Target.Value != "" {
				bound[cl.Target.Value]++
			}
			countExprBinder(cl.Iter, bound, namesBind)
			countExprBinder(cl.Cond, bound, namesBind)
		}
	case *Lambda:
		return // its own frame: its parameters bind there, not here
	}
}
