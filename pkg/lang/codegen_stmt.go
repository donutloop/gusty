// The statement roads. g.stmt is the dispatcher: a statement is handed to the road that lowers it,
// and the road lives with the family it belongs to — the bindings in codegen_assign.go, the loops in
// codegen_for.go, a `match` in codegen_match.go, a `return`/`yield` in codegen_func.go. What stays
// here is the switch itself, the `if`/`with`/`break`/`continue` arms that are nothing but a label and
// a jump, and the block-terminator question they all ask.
package lang

import (
	"fmt"
	"strings"
)

type loopInfo struct {
	breakLabel    string
	continueLabel string
}

// blockEndsInTerminator reports whether the block being built already ended in a terminator,
// in which case another instruction would be dead weight: an arm whose last statement is
// `return` or `raise` has already left, and it did so with the exception's own state.
func blockEndsInTerminator(b *strings.Builder) bool {
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		if strings.HasSuffix(ln, ":") {
			return false // a label: a fresh block, nothing emitted in it yet
		}
		return strings.HasPrefix(ln, "ret ") || strings.HasPrefix(ln, "br ") || ln == "unreachable"
	}
	return false
}

// loopVarName returns the name of a Name loop variable, or "" for a Tuple.
func loopVarName(v Expr) string {
	if n, ok := v.(*Name); ok {
		return n.Value
	}
	return ""
}

// stmt lowers one statement: the switch is a dispatch table, and every arm with anything in it is
// a method in the file that owns that shape.
func (g *irGen) stmt(b *strings.Builder, st Stmt) error {
	// Every statement is a position a debugger, a profiler or (Gap K.8) a traceback can be
	// asked about, so record where its IR starts before anything writes a line (L8.5, ADR 0231).
	g.dbgMark(b, st.Span())
	switch n := st.(type) {
	case *ImportStmt:
	case *ExternDecl:
		// extern declarations are handled at the module level

		// `import mod` resolves module globals at compile time (see resolveImports);
		// the statement itself emits no IR.
		return nil
	case *TryStmt:
		return g.tryStmt(b, n)
	case *RaiseStmt:
		return g.raiseStmt(b, n)
	case *ExprStmt:
		// A bare string expression as a statement has no effect, and evaluating it would now
		// intern it -- which keeps the literal's globals alive and unprunable for a program that
		// only mentions the text (the `--opt-level=1` dead-global assertion). Skipping it is the
		// same no-op it always was.
		if _, isLit := n.Expr.(*StrLit); !isLit {
			// A call whose answer nobody reads is still a call: the statement asked for its effects, and
			// the kind word the callee stored beside its payload is simply never read. The refusal that
			// guards a position which READS one word would stop `main()` — a program the reference runs —
			// at the compile door (roadmap L11.1, Gap R.146's rule, ADR 0368's statement position).
			undo := g.grantDiscardedCallAnswer(n.Expr)
			_, err := g.value(b, n.Expr)
			undo()
			if err != nil {
				return err
			}
		}
	case *AssignStmt:
		return g.stmtAssign(b, st, n)
	case *AugAssignStmt:
		return g.stmtAugAssign(b, st, n)
	case *IfStmt:
		return g.stmtIf(b, st, n)
	case *MatchStmt:
		return g.stmtMatch(b, st, n)
	case *WhileStmt:
		return g.stmtWhile(b, st, n)
	case *ForStmt:
		return g.stmtFor(b, st, n)
	case *ClassDef:
		g.registerClass(n)
		return nil
	case *FuncDef:
		ci := g.closures[n.Name]
		if ci == nil {
			if err := g.funcDef(b, n); err != nil {
				return err
			}
			return nil
		}
		env := g.emitNewEnv(b)
		for i, c := range ci.captured {
			val := ""
			if pn, ok := g.params[c]; ok && !g.paramSlot[c] {
				val = pn
			} else {
				val = g.newTmp()
				fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", val, c)
			}
			g.emitEnvStore(b, env, i, val)
		}
		fmt.Fprintf(b, "  store i32 %s, i32* @%s_slot\n", env, n.Name)
	case *ReturnStmt:
		return g.stmtReturn(b, st, n)
	case *BreakStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: break outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		if g.handledArms > 0 {
			g.clearExn(b) // leaving an arm we already accepted the exception in
		}
		if err := g.runDeferred(b); err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.breakLabel))
	case *ContinueStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: continue outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		if g.handledArms > 0 {
			g.clearExn(b)
		}
		if err := g.runDeferred(b); err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.continueLabel))
	case *PassStmt, *TypeAliasStmt:
		// no-op statement: type aliases are compile-time only (L5.7); emit nothing
	case *YieldStmt:
		return g.stmtYield(b, st, n)
	case *YieldFromStmt:
		return g.stmtYieldFrom(b, st, n)
	case *WithStmt:
		mh, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		_ = mh // manager handle kept for __exit__ dispatch
		enterCall := &Call{Fn: &Attr{Obj: n.Expr, Name: &Name{Value: "__enter__"}}}
		eh, err := g.value(b, enterCall)
		if err != nil {
			return err
		}
		if n.As != nil {
			fmt.Fprintf(b, "%%_%s = alloca i32\n", n.As.Value)
			fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", eh, n.As.Value)
			// Record the slot like every other binding does: the unbound-name guard
			// (nameIsBound) consults allocd, and `with M() as m: … m.n` was rejected as
			// "undefined name m" because this path allocated without registering.
			g.allocd[n.As.Value] = true
			// Record the `as` binding's class so instance field access (m.n) works.
			// `__enter__` returns self, so the binding is an instance of the manager's class.
			cls := ""
			if call, ok := n.Expr.(*Call); ok {
				if fn, ok2 := call.Fn.(*Name); ok2 {
					if _, isClass := g.classInfos[fn.Value]; isClass {
						cls = fn.Value
					}
				}
			} else if name, ok := n.Expr.(*Name); ok {
				cls = g.varClasses[name.Value]
			}
			if cls != "" {
				if g.varClasses == nil {
					g.varClasses = map[string]string{}
				}
				g.varClasses[n.As.Value] = cls
			}
		}
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		exitCall := &Call{Fn: &Attr{Obj: n.Expr, Name: &Name{Value: "__exit__"}},
			Args: []Expr{&IntLit{Value: 0}, &IntLit{Value: 0}, &IntLit{Value: 0}}}
		_, err = g.value(b, exitCall)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("codegen: unsupported statement %T", st)
	}
	return nil
}

// stmtIf is the road an `if`/`elif`/`else` takes: one label per arm, and every test asked through
// the one truthy road.
func (g *irGen) stmtIf(b *strings.Builder, st Stmt, n *IfStmt) error {
	cond, cerr := g.truthyValue(b, n.Cond)
	if cerr != nil {
		return cerr
	}
	thenL := g.newLabel("if.then")
	endL := g.newLabel("if.end")
	var elseL string
	// build elif chain: each elif gets a cond label and a then label.
	type el struct {
		condL, thenL string
		e            *IfStmt
	}
	els := []el{}
	for _, e := range n.Elifs {
		els = append(els, el{g.newLabel("if.elif"), g.newLabel("if.elif.then"), e})
	}
	elseL = g.newLabel("if.else")
	// dispatch from top: cond -> then, else -> first elif cond (or else).
	firstTarget := elseL
	if len(els) > 0 {
		firstTarget = els[0].condL
	}
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, thenL, firstTarget))
	b.WriteString(fmt.Sprintf("%s:\n", thenL))
	for _, s := range n.Then {
		if err := g.stmt(b, s); err != nil {
			return err
		}
	}
	b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
	// elif branches
	for i, e := range els {
		b.WriteString(fmt.Sprintf("%s:\n", e.condL))
		ec, err := g.truthOperandErr(b, e.e.Cond)
		if err != nil {
			return err
		}
		nextTarget := elseL
		if i+1 < len(els) {
			nextTarget = els[i+1].condL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", ec, e.thenL, nextTarget))
		b.WriteString(fmt.Sprintf("%s:\n", e.thenL))
		for _, s := range e.e.Then {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
	}
	b.WriteString(fmt.Sprintf("%s:\n", elseL))
	for _, s := range n.Else {
		if err := g.stmt(b, s); err != nil {
			return err
		}
	}
	b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
	b.WriteString(fmt.Sprintf("%s:\n", endL))
	return nil
}
