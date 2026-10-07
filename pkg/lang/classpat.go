package lang

// classpat.go — the two questions a class pattern asks, answered once (roadmap Gap B, ADR 0235).
//
//	case Alias(x, y):
//
// The first question is *which class does this name name?*. `Alias = Point` is an assignment, not a
// class definition, and until now each backend guessed at it separately: the evaluator consulted the
// scope it happened to be executing (so a pattern inside a function lost the module's `Alias` and
// fell through to calling a class as a constructor — `TypeError: 'type' object is not callable`), and
// codegen consulted a "runtime alias" branch that read the name through `g.value` and threw the error
// away when that refused — emitting `%t6 = icmp eq i32 %t5, ` with nothing after the comma, which
// `llc` rejects (exit 2, ADR 0211).
//
// The second question is *which attribute names can this program ever write?*. A class pattern binds
// its capture names to instance attributes and, per the documented rule, a missing attribute fails
// the pattern. The compiled instance model stores attributes in a fixed slot array and had no notion
// of "never written": `case Point(a, b):` read slots nothing had written and matched, answering
// `pt 0 0` where the record answered `no`. The answer is an instance-presence bitmap in the
// runtime, and a bitmap must be cleared when a heap slot is reused — so codegen needs the whole
// universe of attribute names *before* it emits, which is what `attrs` is for.
//
// Both are AST questions, so they are answered here and read by the checker and by the
// generator's matcher (codegen.go). One question, one table — the rule ADR 0161 set for constant
// folding and ADR 0211 set for error classes. (The AST interpreter that used to be the third reader
// was retired by ADR 0302; its record is still produced from these same tables.)

// classPatternInfo is the answer to those two questions for one program.
type classPatternInfo struct {
	// classOf maps a name used in class-pattern position to the declared class it denotes:
	// identity for `case Point(...)` itself, and the resolved target for an alias
	// (`Alias = Point`, including chains of them). Names that are not classes are absent.
	classOf map[string]string
	// attrs is every attribute name the program can name — reads (`p.x`), writes (`p.x = 1`) and
	// class-pattern capture names — in first-seen order. It is the slot universe the instance
	// presence bitmap is cleared over at instantiation.
	attrs []string
}

// classPatternsOf answers both questions for a program. The walk is a pre-pass, run before any
// emission, so `attrs` is complete when the first instance-allocation site needs its length.
func classPatternsOf(prog *Program) *classPatternInfo {
	if prog == nil {
		return &classPatternInfo{classOf: map[string]string{}}
	}
	declared := map[string]bool{}
	// `Alias = Point` recorded as target -> right-hand name, in source order. A later assignment to
	// the same target wins, which is what executing the program would do.
	chains := map[string]string{}
	walk := astWalker{
		stmt: func(s Stmt) {
			switch st := s.(type) {
			case *ClassDef:
				declared[st.Name] = true
			case *AssignStmt:
				tgt, ok := st.Target.(*Name)
				if !ok {
					return
				}
				src, ok := st.Value.(*Name)
				if !ok {
					return
				}
				chains[tgt.Value] = src.Value
			}
		},
	}
	walk.stmts(prog.Stmts)

	info := &classPatternInfo{classOf: map[string]string{}}
	// Resolve each chain by walking it. A cycle (`a = b; b = a`) has no class at the end of it, and
	// an unbroken walk of more than len(chains)+1 links is such a cycle — bounded rather than hung.
	resolve := func(name string) string {
		seen := 0
		for {
			if declared[name] {
				return name
			}
			next, ok := chains[name]
			if !ok || seen > len(chains) {
				return ""
			}
			name = next
			seen++
		}
	}
	for name := range chains {
		if c := resolve(name); c != "" {
			info.classOf[name] = c
		}
	}
	for name := range declared {
		info.classOf[name] = name
	}

	// Attribute names: everything a `.name` can name, plus the capture names of a class pattern,
	// which are attribute names without a dot in front of them.
	seenAttr := map[string]bool{}
	addAttr := func(n string) {
		if n == "" || seenAttr[n] {
			return
		}
		seenAttr[n] = true
		info.attrs = append(info.attrs, n)
	}
	walk.expr = func(e Expr) {
		switch ex := e.(type) {
		case *Attr:
			if ex.Name != nil {
				addAttr(ex.Name.Value)
			}
		case *Call:
			fn, ok := ex.Fn.(*Name)
			if !ok {
				return
			}
			if info.classOf[fn.Value] == "" {
				return
			}
			for _, arg := range ex.Args {
				if nm, ok := arg.(*Name); ok {
					addAttr(nm.Value)
				}
			}
		}
	}
	walk.stmts(prog.Stmts)
	return info
}

// classOf answers the pattern-position question: which declared class does `name` denote? An empty
// answer means the name is not a class at all, which is the answer a pattern must not paper over.
func (c *classPatternInfo) classOfName(name string) string {
	if c == nil {
		return ""
	}
	return c.classOf[name]
}

// --- a small AST walk ---------------------------------------------------------------
//
// The language has no generic visitor and each pass has carried its own switch. This one covers
// every node in ast.go; a node that gains a child must be added here or the answer above goes
// silently incomplete, so the walk is written as one exhaustive switch rather than a set of
// per-pass copies.

type astWalker struct {
	stmt func(Stmt)
	expr func(Expr)
}

func (w astWalker) stmts(list []Stmt) {
	for _, s := range list {
		if s == nil {
			continue
		}
		if w.stmt != nil {
			w.stmt(s)
		}
		w.descend(s)
	}
}

func (w astWalker) descends(list ...Expr) {
	for _, e := range list {
		w.exprOf(e)
	}
}

func (w astWalker) exprOf(e Expr) {
	if e == nil {
		return
	}
	if w.expr != nil {
		w.expr(e)
	}
	switch ex := e.(type) {
	case *Tuple:
		w.descends(ex.Elems...)
	case *FString:
		for _, p := range ex.Parts {
			w.exprOf(p.Expr)
		}
	case *ListLit:
		w.descends(ex.Elems...)
	case *DictLit:
		w.descends(ex.Keys...)
		w.descends(ex.Vals...)
	case *SetLit:
		w.descends(ex.Elems...)
	case *BinOp:
		w.exprOf(ex.L)
		w.exprOf(ex.R)
	case *UnOp:
		w.exprOf(ex.X)
	case *AwaitExpr:
		w.exprOf(ex.Expr)
	case *CondExpr:
		w.exprOf(ex.If)
		w.exprOf(ex.Cond)
		w.exprOf(ex.Else)
	case *AssignExpr:
		w.exprOf(ex.Value)
	case *Call:
		w.exprOf(ex.Fn)
		w.descends(ex.Args...)
	case *KeywordArg:
		w.exprOf(ex.Value)
	case *Index:
		w.exprOf(ex.Obj)
		w.exprOf(ex.Idx)
	case *Slice:
		w.exprOf(ex.Obj)
		w.exprOf(ex.Low)
		w.exprOf(ex.High)
		w.exprOf(ex.Step)
	case *Attr:
		w.exprOf(ex.Obj)
	case *Lambda:
		for _, p := range ex.Params {
			if p != nil {
				w.exprOf(p.Default)
			}
		}
		w.exprOf(ex.Body)
	case *Comp:
		w.descends(ex.Elems...)
		w.descends(ex.Keys...)
		w.descends(ex.Vals...)
		w.exprOf(ex.Iter)
		w.exprOf(ex.Cond)
	case *Generator:
		w.descends(ex.Elems...)
		w.exprOf(ex.Iter)
		w.exprOf(ex.Cond)
	}
}

func (w astWalker) descend(s Stmt) {
	switch st := s.(type) {
	case *ReturnStmt:
		w.exprOf(st.Expr)
	case *ExprStmt:
		w.exprOf(st.Expr)
	case *AssignStmt:
		w.exprOf(st.Target)
		w.exprOf(st.Value)
	case *AugAssignStmt:
		w.exprOf(st.Target)
		w.exprOf(st.Value)
	case *RaiseStmt:
		w.exprOf(st.Expr)
	case *IfStmt:
		w.exprOf(st.Cond)
		w.stmts(st.Then)
		for _, e := range st.Elifs {
			if e != nil {
				w.descend(e)
			}
		}
		w.stmts(st.Else)
	case *WhileStmt:
		w.exprOf(st.Cond)
		w.stmts(st.Body)
		w.stmts(st.Else)
	case *ForStmt:
		w.exprOf(st.Var)
		w.exprOf(st.Iter)
		w.stmts(st.Body)
		w.stmts(st.Else)
	case *FuncDef:
		for _, p := range st.Params {
			if p != nil {
				w.exprOf(p.Default)
			}
		}
		w.descends(st.Decorators...)
		w.stmts(st.Body)
	case *ExternDecl:
		for _, p := range st.Params {
			if p != nil {
				w.exprOf(p.Default)
			}
		}
	case *ClassDef:
		w.descendsBase(st.Bases)
		w.stmts(st.Body)
	case *MatchStmt:
		w.exprOf(st.Subject)
		for _, c := range st.Cases {
			if c == nil {
				continue
			}
			w.exprOf(c.Pattern)
			w.descends(c.Or...)
			w.exprOf(c.Guard)
			w.stmts(c.Body)
		}
	case *TryStmt:
		w.stmts(st.Body)
		for _, e := range st.Excepts {
			if e != nil {
				w.stmts(e.Body)
			}
		}
		w.stmts(st.Finally)
	case *YieldStmt:
		w.exprOf(st.Expr)
	case *YieldFromStmt:
		w.exprOf(st.Expr)
	case *WithStmt:
		w.exprOf(st.Expr)
		w.stmts(st.Body)
	}
}

func (w astWalker) descendsBase(bases []*Name) {
	for _, b := range bases {
		if b != nil {
			w.exprOf(b)
		}
	}
}
