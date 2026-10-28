package lang

import "fmt"

// ─── Gap R.48: the `global` statement, and the module storage a body is allowed to write ───────────────
//
// The reference executes a function body with a set of names bound OUTSIDE it, and this toolchain had no way to
// say so: `global` lexed as an ordinary name, the resolver answered `undefined name "global"`, and the whole
// family of programs that share module state through a declaration — a counter a helper bumps, a flag a setup
// function sets, a registry a `register` call fills — could not be written at all (roadmap **Gap R.48**).
//
// The READ half already existed. **ADR 0227**/`moduleSlotNames` mint a `@gy_mod_<name>` global for a module-bound
// name that some body reads without binding it itself, and `storeModuleScalar` keeps main's bindings and those
// slots the same object (**ADR 0351**'s lesson: a read of a slot no store visits answers a value no statement
// produced). What was missing was the write, and the declaration that licenses it — which is why this file holds
// both halves of one question rather than a statement emitter:
//
//   - WHICH names a body declares (the scope rule below, mirroring `collectLocals` and `moduleBodyReads` rather
//     than inventing a third one);
//   - WHETHER a name still belongs to the frame at all (a declared name is deleted from `funcLocals`, which is
//     what makes the read road in `value()` take the module slot without a new arm there);
//   - and the two refusals the checker owes, which are the mirror of **ADR 0228**'s local rule: the reference
//     refuses a name that is BOTH a parameter and a global, and one that is read before its own declaration —
//     a gate that guessed at either would compile a program the reference rejects.

// globalDeclsIn answers which names a body declares module-level with `global a, b`.
//
// The walk is the body's OWN: a nested `def`'s declaration belongs to that body, which enters this table again
// when it is emitted. (A nested `global` still names the MODULE's binding — the reference has exactly one
// module scope — and it gets it, because the storage asked for is `@gy_mod_<name>` either way; what the
// per-body split decides is only which frame loses the name.) A class body is not a frame in this backend, so a
// `global` written there is accepted and inert: the member table owns class-body assignments (**ADR 0418**, and
// their re-binding is **Gap R.506**), and the reference's behaviour there is filed with the rest of that family.
func globalDeclsIn(body []Stmt) map[string]bool {
	out := map[string]bool{}
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch s := st.(type) {
			case *GlobalStmt:
				for _, nm := range s.Names {
					if nm != nil && nm.Value != "" {
						out[nm.Value] = true
					}
				}
			case *FuncDef:
				// A nested `def` is its own body: its declarations are its own.
			case *ClassDef:
				// As above — a class body is the class road's, not a frame's.
			case *IfStmt:
				if s != nil {
					walk(s.Then)
					walk(s.Else)
					for _, e := range s.Elifs {
						if e != nil {
							walk(e.Then)
							walk(e.Else)
						}
					}
				}
			case *WhileStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *ForStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *TryStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Finally)
					for _, ex := range s.Excepts {
						if ex != nil {
							walk(ex.Body)
						}
					}
				}
			case *WithStmt:
				if s != nil {
					walk(s.Body)
				}
			case *MatchStmt:
				if s != nil {
					for _, c := range s.Cases {
						if c != nil {
							walk(c.Body)
						}
					}
				}
			}
		}
	}
	walk(body)
	if len(out) == 0 {
		return nil
	}
	return out
}

// programGlobalDeclNames is the program-wide view the module tables ask: a name ANY body declares is a name the
// module owns, so it needs a `@gy_mod_<name>` slot even when no module-level statement mentions it — the shape
// `def f():` / `    global y` / `        y = 5` / `f()` / `print(y)`, where the reference prints `5` and this
// backend had no slot to write the 5 into.
func programGlobalDeclNames(prog *Program) map[string]bool {
	out := map[string]bool{}
	if prog == nil {
		return out
	}
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch s := st.(type) {
			case *GlobalStmt:
				// A module-level `global` is legal and means nothing (the module is already module scope),
				// and it must not mint a slot on its own: with no writer anywhere the reference raises
				// NameError, and a slot answered 0. Only a body's declaration claims module storage.
			case *FuncDef:
				if s == nil {
					continue
				}
				for nm := range globalDeclsIn(s.Body) {
					out[nm] = true
				}
				walk(s.Body)
			case *ClassDef:
				if s == nil {
					continue
				}
				walk(s.Body)
			case *IfStmt:
				if s != nil {
					walk(s.Then)
					walk(s.Else)
					for _, e := range s.Elifs {
						if e != nil {
							walk(e.Then)
							walk(e.Else)
						}
					}
				}
			case *WhileStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *ForStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *TryStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Finally)
					for _, ex := range s.Excepts {
						if ex != nil {
							walk(ex.Body)
						}
					}
				}
			case *WithStmt:
				if s != nil {
					walk(s.Body)
				}
			case *MatchStmt:
				if s != nil {
					for _, c := range s.Cases {
						if c != nil {
							walk(c.Body)
						}
					}
				}
			}
		}
	}
	walk(prog.Stmts)
	return out
}

// globalDeclRefusals is the checker's half, and it is the mirror of **ADR 0228**'s local rule: that ADR made an
// unwritten LOCAL slot raise rather than answer; this one refuses the two shapes the reference refuses, because
// a body whose name means two things has no answer to compile.
//
//   - a PARAMETER and a global: `def f(x):` / `    global x` — CPython says `name 'x' is parameter and global`.
//     The call writes the frame slot and the declaration says the name is the module's; whichever road won, the
//     other would be a slot no read consults (**ADR 0351**'s defect, arriving from the declaration side).
//   - used PRIOR to the declaration: `def f():` / `    print(x)` / `    global x` — CPython says `name 'x' is
//     used prior to global declaration`. The read before the line asks a question the declaration then answers
//     differently, which is a program whose meaning depends on a line order the compiler has already flattened.
func (g *irGen) globalDeclRefusals(fd *FuncDef) error {
	if fd == nil {
		return nil
	}
	declared := globalDeclsIn(fd.Body)
	if len(declared) == 0 {
		return nil
	}
	for _, p := range fd.Params {
		if p != nil && declared[p.Name] {
			return nameIsParameterAndGlobal(fd.Name, p.Name)
		}
	}
	if bad, kind, line := globalUsedPriorToDecl(fd); bad != "" {
		return globalUsedPrior(fd.Name, bad, kind, line)
	}
	return nil
}

// globalUsedPriorToDecl reports the first use of a declared name that sits BEFORE the `global` line that
// claims it, and which of the reference's two refusals it is. The walk is in DOCUMENT ORDER, which is the
// rule the reference actually compiles: the body is scanned statement by statement, a `global` line moves
// its names into the "declared from here" set, and a name already mentioned when it is bound or read is the
// program's error. Line numbers are not the input here — this parser leaves many statement spans at zero,
// and a check that compared them refused the ordinary `global count` / `count = count + 1` body (measured:
// it read "line 0" for every statement and refused `while`, `main-like`, `read-only` and `global-then-local`,
// four programs the reference answers).
//
// The reference distinguishes the two, and so does this:
//
//	def f():                      def f():
//	    x = 1                         print(x)
//	    global x                        global x
//	                                  x = 2
//	name 'x' is assigned to       name 'x' is used prior to
//	before global declaration     global declaration
func globalUsedPriorToDecl(fd *FuncDef) (string, string, int) {
	if fd == nil {
		return "", "", 0
	}
	declared := globalDeclsIn(fd.Body)
	if len(declared) == 0 {
		return "", "", 0
	}
	// A parameter is the OTHER refusal, and it wins: a name that is both is refused before any question
	// about order is asked (globalDeclRefusals asks it first).
	params := map[string]bool{}
	for _, p := range fd.Params {
		if p != nil {
			params[p.Name] = true
		}
	}
	seen := map[string]bool{} // names the body has already mentioned
	var badName, badKind string
	var badLine int
	note := func(name, kind string, line int) {
		if badName != "" || !declared[name] || params[name] || seen[name] {
			return
		}
		badName, badKind, badLine = name, kind, line
	}
	var walk func(sts []Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			if badName != "" {
				return
			}
			line := int(st.Span().Line)
			switch s := st.(type) {
			case *GlobalStmt:
				// The declaration itself: everything it names is declared FROM HERE, so a use above this
				// line is already an offence and a use below it is the program's meaning.
				for _, nm := range s.Names {
					if nm != nil {
						seen[nm.Value] = true
					}
				}
			case *AssignStmt:
				// A binding BEFORE the value's reads, because that is the sentence the reference reads
				// first for `x = x + 1` written above the declaration.
				noteNames(s.Target, "assigned", line, note)
				if badName == "" {
					scanExprForPriorRead(s.Value, note)
				}
				walk(classChainStmtChildren(st))
			case *ForStmt:
				if s != nil {
					// The loop VARIABLE is bound by the statement, which is the sentence the reference
					// reads first for `for x in xs:` written above the declaration; the iterable is read.
					noteNames(s.Var, "assigned", line, note)
					if badName == "" {
						scanExprForPriorRead(s.Iter, note)
					}
				}
				walk(classChainStmtChildren(st))
			case *FuncDef, *ClassDef:
				// A nested body is its own scope for this question: its uses are not this body's, and a
				// `global` written there belongs to that body's own check.
			default:
				scanExprForPriorRead(st, note)
				walk(classChainStmtChildren(st))
			}
		}
	}
	walk(fd.Body)
	return badName, badKind, badLine
}

// noteNames records the names an assignment/loop TARGET binds. A subscript or attribute target is not a
// name being bound — `x[0] = 1` reads x — so it contributes a read, which is what the reference compiles.
func noteNames(target Expr, kind string, line int, note func(string, string, int)) {
	if nm, ok := target.(*Name); ok && nm != nil {
		note(nm.Value, kind, line)
	}
}

// scanExprForPriorRead records the names a statement's EXPRESSIONS mention: a read, in the reference's
// language. What it cannot reach is a read it cannot accuse, which errs toward compiling.
func scanExprForPriorRead(node interface{}, note func(string, string, int)) {
	names := map[string]bool{}
	collectExprNames(node, names)
	for nm := range names {
		note(nm, "used", 0)
	}
}

// collectExprNames gathers the names an expression tree mentions. It walks the shapes a body's statements
// actually hold; a name it cannot reach is a read it cannot accuse, which errs toward compiling.
func collectExprNames(node interface{}, out map[string]bool) {
	var walkE func(Expr)
	walkE = func(e Expr) {
		if e == nil {
			return
		}
		switch x := e.(type) {
		case *Name:
			if x != nil {
				out[x.Value] = true
			}
		case *BinOp:
			if x != nil {
				walkE(x.L)
				walkE(x.R)
			}
		case *UnOp:
			if x != nil {
				walkE(x.X)
			}
		case *Call:
			if x != nil {
				walkE(x.Fn)
				for _, a := range x.Args {
					walkE(a)
				}
			}
		case *Index:
			if x != nil {
				walkE(x.Obj)
				walkE(x.Idx)
			}
		case *Attr:
			if x != nil {
				walkE(x.Obj)
			}
		case *ListLit:
			if x != nil {
				for _, e := range x.Elems {
					walkE(e)
				}
			}
		case *Tuple:
			if x != nil {
				for _, e := range x.Elems {
					walkE(e)
				}
			}
		case *CondExpr:
			if x != nil {
				walkE(x.Cond)
				walkE(x.If)
				walkE(x.Else)
			}
		}
	}
	switch n := node.(type) {
	case Expr:
		walkE(n)
	case *ReturnStmt:
		if n != nil {
			walkE(n.Expr)
		}
	case *ExprStmt:
		if n != nil {
			walkE(n.Expr)
		}
	case *AssignStmt:
		if n != nil {
			walkE(n.Value)
		}
	case *IfStmt:
		if n != nil {
			walkE(n.Cond)
		}
	case *WhileStmt:
		if n != nil {
			walkE(n.Cond)
		}
	case *ForStmt:
		if n != nil {
			walkE(n.Iter)
		}
	case *RaiseStmt:
		if n != nil {
			walkE(n.Expr)
		}
	}
}

// nameIsParameterAndGlobal and globalUsedPrior are the two sentences the checker owes a `global` declaration
// that the reference refuses. Both name the program's shape and the line it was written on, and neither blames a
// node (the rule is roadmap Gap R.116).
func nameIsParameterAndGlobal(funcName, name string) error {
	return fmt.Errorf("codegen: %s declares `global %s` and also takes %s as a parameter, which the reference refuses as "+
		"`name '%s' is parameter and global`: the call writes the frame's slot for it and the declaration says the name is the "+
		"module's, so one of the two would be a slot no read consults (roadmap Gap R.48)", funcName, name, name, name)
}

// globalUsedPrior says the reference's OWN sentence for the shape, of the two it keeps apart: a name BOUND
// above its declaration is `assigned to before global declaration`, a name merely READ above it is `used
// prior to global declaration`. Both are a SyntaxError there, which is what this backend says in words at the
// refusal exit; the line is named when the parser knew it (many statement spans here are zero, and a sentence
// that printed "line 0" would be a diagnostic that lies, which roadmap Gap R.116 forbids).
func globalUsedPrior(funcName, name, kind string, line int) error {
	sentence, verb := "used prior to global declaration", "reads"
	if kind == "assigned" {
		sentence, verb = "assigned to before global declaration", "binds"
	}
	at := "above"
	if line > 0 {
		at = fmt.Sprintf("at line %d, above", line)
	}
	return fmt.Errorf("codegen: %s %s %s %s the `global %s` declaration that claims it, which the reference refuses as "+
		"`name '%s' is %s`: the use above the line asks a question the declaration answers differently for the rest of the "+
		"body, and no answer to it is in the program (roadmap Gap R.48)",
		funcName, verb, name, at, name, name, sentence)
}

// checkGlobalDecls asks the checker's question of EVERY function in the program — the module's own, a
// method, a nested `def` — before any of it is lowered, because the reference's refusal is a COMPILE-time
// one: `def f(x):` / `    global x` / `    return x` is a SyntaxError even when nothing ever calls `f`,
// and a check that waited for the emission would answer with whichever slot the body happened to read
// first (roadmap **Gap R.48**, **ADR 0448**).
//
// A class body is NOT asked: `global` there is inert in this backend (the member table owns class-body
// assignments — **ADR 0418**), and the reference's behaviour for it is filed with that family rather than
// guessed at here.
func (g *irGen) checkGlobalDecls(prog *Program) error {
	if prog == nil {
		return nil
	}
	var walk func(sts []Stmt) error
	walk = func(sts []Stmt) error {
		for _, st := range sts {
			switch s := st.(type) {
			case *FuncDef:
				if s == nil {
					continue
				}
				if err := g.globalDeclRefusals(s); err != nil {
					return err
				}
				if err := walk(s.Body); err != nil {
					return err
				}
			case *ClassDef:
				if s == nil {
					continue
				}
				if err := walk(s.Body); err != nil { // the METHODS of a class are asked
					return err
				}
			case *IfStmt:
				if s == nil {
					continue
				}
				if err := walk(s.Then); err != nil {
					return err
				}
				if err := walk(s.Else); err != nil {
					return err
				}
				for _, e := range s.Elifs {
					if e != nil {
						if err := walk(e.Then); err != nil {
							return err
						}
					}
				}
			case *WhileStmt:
				if s == nil {
					continue
				}
				if err := walk(s.Body); err != nil {
					return err
				}
			case *ForStmt:
				if s == nil {
					continue
				}
				if err := walk(s.Body); err != nil {
					return err
				}
			case *TryStmt:
				if s == nil {
					continue
				}
				if err := walk(s.Body); err != nil {
					return err
				}
				for _, ex := range s.Excepts {
					if ex != nil {
						if err := walk(ex.Body); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}
	return walk(prog.Stmts)
}

// ─── The three refusals the declaration owes, beyond the two the reference itself raises ─────────
//
// A `global` declaration is a promise about STORAGE, and this backend's module storage is a scalar word
// with no kind beside it (**L11.1**'s tagged value word is the row that owns the difference). Three shapes
// break that promise, and all three currently answer at the exit code of success — the answer class this
// repo ranks worst — so the checker says so instead:
//
//	count = 0                        reg = 0                      def f():
//	def bump():                          def f():                     global z
//	    global count                         global reg                     pass
//	    count = count + 1                    global q                   f()
//	                                       reg = [1, 2]                 print(z)
//	print(count) → 2 ✓                   print(reg) → 0 ✗             → NameError, we answered 0
//	   (every write spells one kind)      (an int slot holding a list)
//
// The rule the answer obeys is therefore: a declared global whose every written value SPELLS one kind is
// answered (that is the counter, the flag, the accumulator, the registry handle), and one whose writes
// spell two kinds, or which nothing ever writes, is refused in words that name the missing word.
type globalKindFact struct {
	kinds map[int32]string // the spelled kind of each write, and the first name that wrote it… kept simple
	any   bool             // something wrote it, spelled or not
}

// globalFacts walks the program's writes to every declared-global name, in the same document-order way the
// reference's own scope rule reads a body: which names a body DECLARES, which names the MODULE binds, and
// what each write spells.
func globalFacts(prog *Program) map[string]*globalKindFact {
	declared := map[string]bool{}
	for nm := range programGlobalDeclNames(prog) {
		declared[nm] = true
	}
	out := map[string]*globalKindFact{}
	fact := func(nm string) *globalKindFact {
		f := out[nm]
		if f == nil {
			f = &globalKindFact{kinds: map[int32]string{}}
			out[nm] = f
		}
		return f
	}
	var walk func(sts []Stmt, decl map[string]bool)
	walk = func(sts []Stmt, decl map[string]bool) {
		for _, st := range sts {
			switch s := st.(type) {
			case *GlobalStmt:
				// A module-level `global` declares nothing that needs storage; a body's declaration is taken
				// from globalDeclsIn when the body is entered.
				_ = s
			case *FuncDef:
				if s == nil {
					continue
				}
				walk(s.Body, globalDeclsIn(s.Body))
			case *ClassDef:
				if s == nil {
					continue
				}
				// A method is a body; the class body itself is not (and a `global` written there is refused).
				walk(s.Body, nil)
			case *AssignStmt:
				if s == nil {
					continue
				}
				if nm, ok := s.Target.(*Name); ok && nm != nil && declared[nm.Value] {
					f := fact(nm.Value)
					f.any = true
					if k, ok := globalSpelledKind(s.Value); ok {
						f.kinds[k] = nm.Value
					} else {
						f.kinds[-999] = nm.Value // an unwritten-down kind: still a write, still no claim
					}
				}
				walk(classChainStmtChildren(st), decl)
			case *ForStmt:
				if s != nil {
					if nm, ok := s.Var.(*Name); ok && nm != nil && declared[nm.Value] {
						f := fact(nm.Value)
						f.any = true
						f.kinds[-999] = nm.Value
					}
				}
				walk(classChainStmtChildren(st), decl)
			default:
				walk(classChainStmtChildren(st), decl)
			}
		}
	}
	walk(prog.Stmts, nil)
	return out
}

// globalSpelledKind is the literal-kind door the render road already owns (**ADR 0447**'s
// `renderLiteralKind`), asked of a module write: what does this value say it is, when it says at all.
func globalSpelledKind(e Expr) (int32, bool) {
	return renderLiteralKind(e)
}

// globalDeclChecks is the checker's second half: the class body, the two-kind slot and the never-written
// name. It runs with `checkGlobalDecls`, so a program that would answer wrongly never reaches the printer.
func (g *irGen) globalDeclChecks(prog *Program) error {
	if prog == nil {
		return nil
	}
	// (1) `global` written in a CLASS body. The class road keeps what a body binds (**ADR 0418**), and the
	// reference routes the write to the module instead — so the member table and the declaration would each
	// own one read and no write of the other. The reference's answer (`x` reads 4, not the module's 0) is
	// owed to the class-body family, filed from this row; what it must not be is a silent 0.
	if bad := globalInClassBody(prog); bad != "" {
		return globalInClassBodyRefusal(bad)
	}
	declared := programGlobalDeclNames(prog)
	if len(declared) == 0 {
		return nil
	}
	facts := globalFacts(prog)
	for nm := range declared {
		f := facts[nm]
		// (2) Nothing ever writes it. The reference raises NameError at the read; a slot answered 0, which is
		// **Gap R.22**'s signature arriving through a declaration.
		if f == nil || !f.any {
			return globalNeverWrittenRefusal(nm)
		}
		// (2b) Nothing at module level binds it, and a body READS it before any statement has written it.
		// `def f():` / `    global q` / `    q = q + 1` is the shape: the right-hand side asks for q before
		// the statement that answers for it finishes, and the reference dies there with NameError while an
		// unwritten slot answers 0 — the same silent-wrong the never-written rule above keeps out, one
		// statement earlier in the program.
		if !moduleBindsName(prog, nm) && globalReadBeforeWrite(prog, nm) {
			return globalReadBeforeWriteRefusal(nm)
		}
		// (2c) The kind the module's scalar word cannot carry. The slot is an int-sized word: a number,
		// a verdict and the void answer out of it, and a text does too when nothing froze its value. A
		// FRACTIONAL number, a container, or a text whose value the module also froze is the tagged value
		// word **L11.1** owns, and answering one here printed the handle — `0` for `[1, 2]`, `1` for a bool
		// with no claim behind it, an empty line for a text (roadmap **Gap R.48**, **Gap R.22**).
		if kind, bad := globalKindTheWordCannotCarry(f, moduleBindsName(prog, nm)); bad != "" {
			return uncarryableGlobalKindRefusal(nm, kind, bad)
		}
		// (3) Its writes spell two kinds. The slot is one word with no kind beside it: whichever value won,
		// the other would print as the first one's kind — `reg = 0` then `reg = [1, 2]` printed `0`.
		if kind, other, mixed := mixedGlobalKind(f); mixed {
			return mixedGlobalKindRefusal(nm, kind, other)
		}
	}
	return nil
}

// mixedGlobalKind reports the two spelled kinds a declared global's writes claim, when they disagree. The
// "unspelled" marker (-999) is not a kind: a write the door cannot read makes no claim and no conflict.
func mixedGlobalKind(f *globalKindFact) (string, string, bool) {
	seen := map[string]bool{}
	var first, second string
	for k := range f.kinds {
		if k == -999 {
			continue
		}
		name := globalKindWord(k)
		if seen[name] {
			continue
		}
		if len(seen) == 0 {
			first = name
		} else if second == "" && name != first {
			second = name
		}
		seen[name] = true
	}
	if len(seen) > 0 && second != "" {
		// print's word for the kinds, so the sentence names what the reader sees rather than a tag number.
		return first, second, true
	}
	return "", "", false
}

func globalKindWord(tag int32) string {
	switch tag {
	case int32(TagInt):
		return "a number"
	case int32(TagFloat):
		return "a fractional number"
	case int32(TagStr):
		return "a text"
	case int32(TagBool):
		return "a verdict"
	case int32(TagNone):
		return "nothing (None)"
	case int32(TagList), int32(TagTuple):
		return "a list"
	case int32(TagDict):
		return "a dict"
	case int32(TagSet):
		return "a set"
	default:
		return "a value"
	}
}

// globalInClassBody names the first `global` written directly in a class body, if the program writes one.
// The walk is the CLASS road's own: a method's body is a function body (asked by checkGlobalDecls), and a
// `global` written there is the ordinary case.
func globalInClassBody(prog *Program) string {
	if prog == nil {
		return ""
	}
	var find func(sts []Stmt, inClass bool) string
	find = func(sts []Stmt, inClass bool) string {
		for _, st := range sts {
			switch s := st.(type) {
			case *GlobalStmt:
				if inClass && s != nil {
					for _, nm := range s.Names {
						if nm != nil && nm.Value != "" {
							return nm.Value
						}
					}
				}
			case *FuncDef:
				// A method body is a function body, and its declarations are the ordinary case.
			case *ClassDef:
				if s == nil {
					continue
				}
				if bad := find(s.Body, true); bad != "" {
					return bad
				}
			default:
				if inClass {
					if bad := find(classChainStmtChildren(st), true); bad != "" {
						return bad
					}
				}
			}
		}
		return ""
	}
	return find(prog.Stmts, false)
}

func globalInClassBodyRefusal(name string) error {
	return fmt.Errorf("codegen: a class body declares `global %s`, which this backend cannot honour: the class road keeps what a "+
		"class body binds as a MEMBER of the class (roadmap ADR 0418), while the reference routes that write to the module, so the two "+
		"readers of the name would see two different values and the module would print the literal no statement wrote. The reference "+
		"answers the module's value there; the class-body declaration is owed to that family (roadmap Gap R.48)", name)
}

func globalNeverWrittenRefusal(name string) error {
	return fmt.Errorf("codegen: `global %s` declares a module binding that no statement ever writes: the reference raises "+
		"`NameError: name '%s' is not defined` at the read, and this backend's module slot would answer 0 for it — a handle rendered as a "+
		"number, which the exit-code contract ranks worse than the refusal (roadmap Gap R.48, Gap R.22)", name, name)
}

func mixedGlobalKindRefusal(name, kindA, kindB string) error {
	return fmt.Errorf("codegen: `global %s` is written as both %s and %s, and the module's slot for it is one word with no kind beside "+
		"it: whichever value the read asked, the other would print as the first one's kind (`reg = 0` then `reg = [1, 2]` prints `0`). The "+
		"tagged value word that would carry both is roadmap row L11.1's, and the shape is filed from there (roadmap Gap R.48)", name, kindA, kindB)
}

// moduleBindsName asks whether the MODULE's own statements bind the name at all — the difference between a
// counter the program initialises (`count = 0`) and one a body alone ever writes.
func moduleBindsName(prog *Program, name string) bool {
	if prog == nil {
		return false
	}
	found := false
	var walk func(sts []Stmt, inBody bool)
	walk = func(sts []Stmt, inBody bool) {
		for _, st := range sts {
			if found {
				return
			}
			switch s := st.(type) {
			case *FuncDef:
				if s == nil {
					continue
				}
				walk(s.Body, true)
			case *ClassDef:
				if s == nil {
					continue
				}
				walk(s.Body, true)
			case *AssignStmt:
				if s == nil {
					continue
				}
				if !inBody {
					if nm, ok := s.Target.(*Name); ok && nm != nil && nm.Value == name {
						found = true
						return
					}
				}
				walk(classChainStmtChildren(st), inBody)
			default:
				walk(classChainStmtChildren(st), inBody)
			}
		}
	}
	walk(prog.Stmts, false)
	return found
}

// globalReadBeforeWrite reports the first body that reads a declared global before a statement of that body
// has written it, with no module-level binding behind it. Document order is the rule again — the same input
// the reference's compiler reads, and the reason `count = count + 1` under a `global count` is the
// program's own counter and `q = q + 1` with nothing behind it is a NameError.
func globalReadBeforeWrite(prog *Program, name string) bool {
	if prog == nil {
		return false
	}
	hit := false
	var walkBody func(sts []Stmt)
	walkBody = func(sts []Stmt) {
		// Only a statement at the body's OWN level licenses a later read: a binding the program guards
		// (`if 0:` / `while False:` / a loop that runs zero times / a try that never reaches it) is a write
		// that may never happen, and the reference's NameError is decided by whether it DID happen. The
		// probe this rule was written for is exactly that shape — `global gz` / `if 0:` / `gz = 1` /
		// `return gz` — where a walk that credited the guarded write answered 0 for a name the reference
		// has never bound.
		written := false
		var walk func([]Stmt, bool)
		walk = func(ss []Stmt, topLevel bool) {
			for _, st := range ss {
				if hit {
					return
				}
				switch s := st.(type) {
				case *GlobalStmt:
					// the declaration itself mentions nothing
				case *AssignStmt:
					if s == nil {
						continue
					}
					if nm, ok := s.Target.(*Name); ok && nm != nil && nm.Value == name {
						// The right-hand side is read BEFORE the target is bound, which is the
						// reference's order for `q = q + 1` exactly.
						names := map[string]bool{}
						collectExprNames(s.Value, names)
						if names[name] && !written {
							hit = true
							return
						}
						if topLevel {
							written = true
						}
						return
					}
					names := map[string]bool{}
					collectExprNames(s, names)
					if names[name] && !written {
						hit = true
						return
					}
					walk(classChainStmtChildren(st), false)
				case *FuncDef, *ClassDef:
					// a nested body is its own frame; its own declarations are asked on its own pass
				default:
					names := map[string]bool{}
					collectExprNames(st, names)
					if names[name] && !written {
						hit = true
						return
					}
					walk(classChainStmtChildren(st), false)
				}
			}
		}
		walk(sts, true)
	}
	var find func(sts []Stmt)
	find = func(sts []Stmt) {
		for _, st := range sts {
			if hit {
				return
			}
			switch s := st.(type) {
			case *FuncDef:
				if s == nil {
					continue
				}
				if globalDeclsIn(s.Body)[name] {
					walkBody(s.Body)
				}
				find(s.Body)
			case *ClassDef:
				if s == nil {
					continue
				}
				find(s.Body)
			default:
				find(classChainStmtChildren(st))
			}
		}
	}
	find(prog.Stmts)
	return hit
}

func globalReadBeforeWriteRefusal(name string) error {
	return fmt.Errorf("codegen: a body reads `global %s` before any statement has written it and nothing at module level binds it: the "+
		"reference raises `NameError: name '%s' is not defined` there, and this backend's unwritten module slot would answer 0 for it — the "+
		"raise, with its traceback, belongs to the module's bound-flag discipline (roadmap Gap R.48, Gap R.22)", name, name)
}

// globalKindTheWordCannotCarry names the first value the module's scalar slot cannot hold for this name,
// and the word the reader would have seen. The module's own binding widens what is answerable — with a
// binding behind it the record knows how to FORMAT the value (True, None, 2), while with none the body's
// literal is the only evidence and a fractional number or a container has no claim to print from.
func globalKindTheWordCannotCarry(f *globalKindFact, moduleBound bool) (string, string) {
	for k := range f.kinds {
		if k == -999 {
			continue
		}
		switch int32(k) {
		case int32(TagInt), int32(TagBool), int32(TagNone):
			continue
		case int32(TagStr):
			if moduleBound {
				return "a text", "the module's interned literal rather than the slot the body wrote"
			}
			// With nothing behind the name there is not even a claim to print from: the word in the slot
			// is an @str_tab index and the printer, asked what to write, wrote `(null)`.
			return "a text", "no interned claim beside an index the printer cannot read"
		}
		return globalKindWord(int32(k)), "the module's scalar word, which is an int-sized handle"
	}
	return "", ""
}

func uncarryableGlobalKindRefusal(name, kind, why string) error {
	return fmt.Errorf("codegen: `global %s` names a module binding written as %s, and the module's slot for it is one word — %s. The "+
		"tagged value word that carries a number, a text, a verdict, the void and a container together is roadmap row L11.1's, and this "+
		"backend refuses to print the handle for it (roadmap Gap R.48, Gap R.22)", name, kind, why)
}
