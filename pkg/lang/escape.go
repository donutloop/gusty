package lang

import "reflect"

// deadListAssignments returns the set of top-level variable names whose every
// top-level assignment is a list-literal and which are never read at top level.
// For such variables the heap list allocation is dead (the list is never
// observed), so the codegen skips the rt_alloc entirely.
//
// The analysis walks only top-level statements (descending into compound
// bodies like if/for/while/try/match) and deliberately does NOT descend into
// function bodies: in this codegen function bodies use their own shadowed locals and cannot read
// top-level globals (roadmap Gap R.35 is the row that refuses such a read out loud).
//
// # Why the walk is generic
//
// The answer of this analysis is not a report, it is a PERMISSION. A name that comes back unread has its
// binding's heap allocation deleted by the `deadLists` road in codegen, and the program then reads a slot
// nothing ever wrote. So a construct the walk cannot see is not a name missed in a listing — it is a
// program whose binding is gone from under it. That is how, with a `switch` of the node types someone had
// thought to list:
//
//	xs = [1.5, "a"]
//	print(f"len={len(xs)}")   # compiled said: len of a non-string variable
//	xs = [1.5, "a"]           # (and CPython prints len=2)
//
//	xs = [1.5, "a"]
//	def show(k): print(k)
//	show(k=xs)               # compiled said: undefined name "xs" (no binding for it; assign it
//	                         # before use) — for a program that assigned xs on the line above
//
// refused at exit 1 with sentences that are false about the program (roadmap Gap R.289). The node types
// that had no arm, and now cannot be forgotten one at a time: FString (every field of an interpolation),
// Tuple, KeywordArg, ChainCompare, AssignExpr, AwaitExpr, Comp.Iter/.Cond and Generator.Cond,
// MatchCase.Guard/.Or, and at statement level RaiseStmt, WithStmt, YieldFromStmt, ClassDef's body and
// bases, and a `def`'s decorators and parameter defaults — all of which are evaluated where the module
// is, and none of which counted as a read.
//
// The walk therefore follows the node tree through reflection — every exported field of every node,
// descending into anything that is or holds an Expr, and handing anything that is or holds a Stmt back to
// the statement rules — so the AST can grow a node and this analysis cannot silently fall behind it.
//
// Two policies are stated as policy rather than left to the fall of the switch:
//
//   - a `lambda` IS walked into, body and parameter defaults included. A lambda is an expression the
//     module evaluates *here*, and the free names in its body are reads of this scope: that is what a
//     closure is. (`xs = [10, 20]` / `f = lambda: xs[1]` / `print(f())` answers 20.)
//   - a `def`'s body is NOT. Its reads are compiled into another function, and this backend refuses to
//     read module state from one at all (Gap R.35's own sentence), so there is no live read here to
//     protect and the allocation stays dead. Its DECORATORS and parameter DEFAULTS, by contrast, are
//     module-level expressions — they run when the `def` statement runs — so they are walked.
func deadListAssignments(prog []Stmt) map[string]bool {
	reads := map[string]bool{}
	nonListAssign := map[string]bool{}

	var walkStmt func(s Stmt)

	mark := func(name string) { reads[name] = true }

	// walkStmt processes a top-level statement: it descends into compound
	// bodies but not into function definitions.
	walkStmt = func(s Stmt) {
		switch n := s.(type) {
		case nil:
		case *AssignStmt:
			if name, ok := n.Target.(*Name); ok {
				if _, isList := n.Value.(*ListLit); !isList {
					nonListAssign[name.Value] = true
				}
			}
			// a plain Name target is a write, not a read; only walk Attr/Index
			// targets (their object sub-expressions are reads).
			if _, ok := n.Target.(*Name); !ok {
				walkExpr2(n.Target, mark, walkStmt)
			}
			walkExpr2(n.Value, mark, walkStmt)
		case *AugAssignStmt:
			// augmented assignment reads and writes the target.
			walkExpr2(n.Target, mark, walkStmt)
			walkExpr2(n.Value, mark, walkStmt)
		case *ExprStmt:
			walkExpr2(n.Expr, mark, walkStmt)
		case *ReturnStmt:
			walkExpr2(n.Expr, mark, walkStmt)
		case *YieldStmt:
			walkExpr2(n.Expr, mark, walkStmt)
		case *YieldFromStmt:
			walkExpr2(n.Expr, mark, walkStmt)
		case *RaiseStmt:
			// `raise ValueError(msg)` reads msg. A statement the walk did not visit made the program
			// that raises from a container's slot read a slot that had been allocated to nobody.
			walkExpr2(n.Expr, mark, walkStmt)
		case *IfStmt:
			walkExpr2(n.Cond, mark, walkStmt)
			for _, st := range n.Then {
				walkStmt(st)
			}
			for _, e := range n.Elifs {
				walkExpr2(e.Cond, mark, walkStmt)
				for _, st := range e.Then {
					walkStmt(st)
				}
			}
			for _, st := range n.Else {
				walkStmt(st)
			}
		case *WhileStmt:
			walkExpr2(n.Cond, mark, walkStmt)
			for _, st := range n.Body {
				walkStmt(st)
			}
			for _, st := range n.Else {
				walkStmt(st)
			}
		case *ForStmt:
			if n.Var != nil {
				// loop var is an assignment target (value is an element,
				// not a list literal) so the variable must keep its slot.
				for _, nm := range loopVarNames(n.Var) {
					nonListAssign[nm] = true
				}
			}
			walkExpr2(n.Iter, mark, walkStmt)
			for _, st := range n.Body {
				walkStmt(st)
			}
			for _, st := range n.Else {
				walkStmt(st)
			}
		case *TryStmt:
			for _, st := range n.Body {
				walkStmt(st)
			}
			for _, ec := range n.Excepts {
				if ec.Exn != nil {
					nonListAssign[ec.Exn.Value] = true
				}
				for _, st := range ec.Body {
					walkStmt(st)
				}
			}
			for _, st := range n.Finally {
				walkStmt(st)
			}
		case *MatchStmt:
			walkExpr2(n.Subject, mark, walkStmt)
			for _, c := range n.Cases {
				// A pattern binds names; the alternative patterns and the guard READ them, and the
				// guard is an ordinary expression evaluated where the match statement is.
				for _, alt := range c.Or {
					walkExpr2(alt, mark, walkStmt)
				}
				walkExpr2(c.Guard, mark, walkStmt)
				for _, st := range c.Body {
					walkStmt(st)
				}
			}
		case *WithStmt:
			// `with open(path) as fh:` evaluates its expression here, and the body runs here too.
			walkExpr2(n.Expr, mark, walkStmt)
			if n.As != nil {
				nonListAssign[n.As.Value] = true
			}
			for _, st := range n.Body {
				walkStmt(st)
			}
		case *ClassDef:
			// A class body's statements run where the class statement is, and read the names around
			// them (`class C:` / `first = xs[0]` reads xs). The bases are expressions too.
			for _, b := range n.Bases {
				if b != nil {
					mark(b.Value)
				}
			}
			for _, st := range n.Body {
				walkStmt(st)
			}
		case *FuncDef:
			// Body: not descended (the policy, above). Decorators and parameter defaults: walked,
			// because they are module-level expressions.
			for _, d := range n.Decorators {
				walkExpr2(d, mark, walkStmt)
			}
			for _, p := range n.Params {
				if p != nil && p.Default != nil {
					walkExpr2(p.Default, mark, walkStmt)
				}
			}
		case *ImportStmt, *BreakStmt, *PassStmt, *ContinueStmt, *ExternDecl, *TypeAliasStmt:
			// no reads
		default:
			// A statement this analysis has no case for is walked generically rather than ignored:
			// the safe direction for an analysis that deletes allocations is to assume the name is
			// read, and reflection is what makes "assume" the default instead of the exception.
			markValueReads(reflect.ValueOf(s), mark, walkStmt)
		}
	}

	for _, st := range prog {
		walkStmt(st)
	}

	// A variable is a dead-list candidate iff every top-level assignment to it
	// is a list-literal and it is never read at top level.
	dead := map[string]bool{}
	for _, st := range prog {
		if as, ok := st.(*AssignStmt); ok {
			if name, ok2 := as.Target.(*Name); ok2 {
				if _, isList := as.Value.(*ListLit); isList && !reads[name.Value] && !nonListAssign[name.Value] {
					dead[name.Value] = true
				}
			}
		}
	}
	return dead
}

// walkExpr2 is the entry to the generic expression walk: an Expr, or nothing.
func walkExpr2(e Expr, mark func(string), onStmt func(Stmt)) {
	if e == nil {
		return
	}
	markValueReads(reflect.ValueOf(e), mark, onStmt)
}

// markExprReads keeps the name the analysis reads best at the call site.
func markExprReads(e Expr, mark func(string), onStmt func(Stmt)) { walkExpr2(e, mark, onStmt) }

// markValueReads walks any AST value, marking every *Name it reaches as a potential read and handing
// every Stmt it reaches to the statement rules. Node types are NOT enumerated here — that enumeration
// is what this file's history is (see the comment above deadListAssignments). What IS enumerated is the
// two policy nodes, `*Lambda` (walked into) and `*FuncDef` (walked up to, but never into its body).
func markValueReads(v reflect.Value, mark func(string), onStmt func(Stmt)) {
	for v.IsValid() && (v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer) {
		if v.IsNil() {
			return
		}
		if nm, ok := v.Interface().(*Name); ok {
			// The singleton name node: mark the read and stop. A Name's own fields are its spelling
			// and span, and a `Name` is also how a BINDING is spelled (`for v in xs`, `except E as v`)
			// — which is why the statement rules above, and not this walk, decide what a Name means.
			mark(nm.Value)
			return
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			markValueReads(v.Index(i), mark, onStmt)
		}
	case reflect.Map:
		// The AST carries no maps today; the direction is the same — an unknown container of nodes is
		// walked, because forgetting to look is the failure mode this file is about.
		iter := v.MapKeys()
		for _, k := range iter {
			markValueReads(v.MapIndex(k), mark, onStmt)
		}
	case reflect.Struct:
		// The policy nodes, matched in both spellings: a value reached through a pointer has been
		// dereferenced above, so `FuncDef` and `*FuncDef` are the same node arriving two ways, and a
		// policy that only matches one of them is a policy that quietly does not apply.
		node := v.Interface()
		if v.CanAddr() {
			node = v.Addr().Interface()
		}
		switch n := node.(type) {
		case Lambda:
			markLambdaReads(&n, mark, onStmt)
			return
		case *Lambda:
			markLambdaReads(n, mark, onStmt)
			return
		case FuncDef:
			markFuncDefReads(&n, mark, onStmt)
			return
		case *FuncDef:
			markFuncDefReads(n, mark, onStmt)
			return
		}
		// Every struct's exported fields are walked, whether or not the struct is itself an Expr:
		// `FStringPart` is a plain struct holding one, and a rule that only descends into node types
		// stops at the wrapper and loses the field — which is precisely the failure this file exists
		// to end. A Span or a Type costs a few reflection calls and yields nothing.
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			markValueReads(v.Field(i), mark, onStmt)
		}
	default:
		// Strings, ints, spans, kind enums: no names inside them.
	}
}

// markLambdaReads walks a lambda: an expression the module evaluates now, whose body's free names are
// reads of this scope and whose defaults are module-level expressions too.
func markLambdaReads(n *Lambda, mark func(string), onStmt func(Stmt)) {
	if n == nil {
		return
	}
	for _, p := range n.Params {
		if p != nil && p.Default != nil {
			walkExpr2(p.Default, mark, onStmt)
		}
	}
	walkExpr2(n.Body, mark, onStmt)
}

// markFuncDefReads walks the parts of a `def` statement that run where the statement is: its decorators
// and its parameter defaults. The BODY is another function's program, and this backend refuses to read
// module state from one at all (roadmap Gap R.35 is the row that says so out loud), so a name whose only
// read lives in a body has no live read to protect and its allocation stays dead. That asymmetry is
// stated, tested (TestTheWalksPositionOnBodiesIsPolicyAndNotAnAccident) and filed (roadmap Gap R.290)
// rather than left to the fall of a switch.
func markFuncDefReads(n *FuncDef, mark func(string), onStmt func(Stmt)) {
	if n == nil {
		return
	}
	for _, d := range n.Decorators {
		walkExpr2(d, mark, onStmt)
	}
	for _, p := range n.Params {
		if p != nil && p.Default != nil {
			walkExpr2(p.Default, mark, onStmt)
		}
	}
}
