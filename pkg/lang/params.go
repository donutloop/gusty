package lang

import "sort"

// Rebound parameters — a parameter is a local that starts out bound to an argument.
//
// Python-shaped programs rebind parameters constantly: an accumulator that
// decrements the number it was given, a clamp that overwrites its argument, a loop
// that reuses the parameter as its variable:
//
//	def acc(n):
//	    total = 0
//	    while n > 0:
//	        total = total + n
//	        n = n - 1
//	    return total
//
// The compiled backend registered each parameter as its incoming argument register
// and resolved every reference from that register. The assignment below such a read
// allocated a *second* slot that nothing ever read, so `acc(4)` never terminated —
// its condition compared the argument, forever — and the quieter form
//
//	def bump(n):
//	    n = n + 1
//	    return n
//
// answered 0: the store happened, the read ignored it. Functions, methods and nested
// defs were all affected; only parameters rebound to a float, a string or a container
// worked, because those reads go through the kind maps rather than the register.
//
// The fix is what an SSA-form compiler does with a variable assigned after its
// definition: copy the incoming register into a real stack slot at the top of the
// body and make that slot authoritative for the whole body. Every path then reads
// initialized storage — including a read *textually before* the first assignment,
// which is why the copy belongs at the entry and not lazily at the assignment.
//
// Two rules keep the change honest:
//
//   - A parameter is copied in only when every value bound to it in the body is a
//     plain scalar. A parameter rebound to a float, a string or a container already
//     reads through its kind map (floatVars / strVals / listVars), which loads the
//     slot; making the entry decide that slot's LLVM type would duplicate a decision
//     the per-kind paths make at the store, and those shapes are not broken today.
//   - Extra copies are harmless, missing ones are not: a copy-in for a parameter that
//     turns out never to be assigned just costs one alloca, so the scan is deliberately
//     generous about what counts as a binding (a `for` over a parameter name, a `with`
//     target, a `case y:` capture, a comprehension's loop variable).
//
// The single float question is asked of the caller's own predicate — in codegen,
// (*irGen).isFloat — so the decision here can never disagree with the store type the
// assignment path will choose.

// reboundParams returns the parameters of fd whose own body rebinds them to plain
// scalar values, so the caller gives them an entry slot instead of letting them stay
// as incoming argument registers. isFloat is the caller's float predicate; nil means
// "treat every binding as scalar".
//
// Nested `def`s, `lambda`s and class bodies are not walked: their parameters and
// bodies are a separate scope with their own registers, and each gets this treatment
// from its own prologue.
func reboundParams(fd *FuncDef, isFloat func(Expr) bool) map[string]bool {
	if fd == nil || len(fd.Params) == 0 {
		return nil
	}
	bindings := map[string][]Expr{}
	scanRebinds(fd.Body, bindings)
	out := map[string]bool{}
	for _, p := range fd.Params {
		bs, ok := bindings[p.Name]
		if !ok || len(bs) == 0 {
			continue
		}
		scalar := true
		for _, v := range bs {
			if v == nil {
				// A binding with no observable value (a loop variable, a `case`
				// capture): codegen decides the kind where it writes it, and an
				// entry copy of the argument is correct whatever that turns out to
				// be as long as it is not a float slot.
				continue
			}
			if isFloat != nil && isFloat(v) {
				scalar = false
				break
			}
		}
		if scalar {
			out[p.Name] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// returnedFloatRebindings names the variables of fd — parameters and body-local bindings alike —
// that its own body binds to a float value *and* whose `return` reads back as a bare name, an
// arithmetic expression over it, or a kind-preserving numeric builtin of it. That is the shape the
// compiled backend could not answer while the return word came from the last line's syntax.
//
// A function's calling convention is written twice, from two different questions: the argument words
// come from the call sites and the return word from the shape of the return *expression*
// (`return x + 0.0` is a double, `return x` says nothing and so gets an i32). A body that then stores
// a double into the parameter's slot has nowhere for that double to be returned from: the read of `x`
// keeps answering the incoming register, and
//
//	def addf(x):
//	    x = x + 1.5
//	    return x
//
//	print(addf(1.0))   # CPython 2.5 · --interp 2.5 · --aot answered 1
//
// answers the *argument*, with exit 0. Rebinding a parameter to a plain int is fine (ADR 0196's
// copy-in gives the slot a word to live in), reading such a parameter in the body is fine (the float
// variable table answers those reads), and returning a float *expression* is fine (the function is
// emitted double-returning). What was not fine was returning the name itself — whether the name came
// in through the parameter list or was bound inside the body, which is why the scan covers both: the
// local spelling (`y = x + 0.5` / `return y`) answers `0`, and a program cannot tell the two apart.
// It stays a statement-shape question rather than a "does the body mention a float" question, because
// the second would refuse the programs that answer correctly today.
//
// A refusal is what ADR 0166 asks for here: an answer that is a truncated word, printed happily, is
// the compiler's bug (roadmap L11.6, Gap R.3c, ADR 0196 — the tagged value word is what makes this
// shape answerable rather than refuseable).
func returnedFloatRebindings(fd *FuncDef, isFloat func(Expr) bool) []string {
	if fd == nil || len(fd.Params) == 0 {
		return nil
	}
	bindings := map[string][]Expr{}
	scanRebinds(fd.Body, bindings)
	returned := map[string]bool{}
	scanBareReturns(fd.Body, returned)
	var out []string
	// Every name the return reads back, whether it arrived as an argument or was bound inside the
	// body: `y = x + 0.5` / `return y` is the same lie as the parameter form, and answers 0.
	for nm, vals := range bindings {
		if !returned[nm] {
			continue
		}
		for _, v := range vals {
			if v != nil && isFloat != nil && isFloat(v) {
				out = append(out, nm)
				break
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Strings(out)
	return out
}

// namedNumericLeaves names the variables an arithmetic-shaped expression reads: straight through a
// parenthesised/ unary / binary / conditional wrapper, which is how far a return expression carries
// the identity of the number it is built from (roadmap Gap R.3c).
func namedNumericLeaves(e Expr) []string {
	var out []string
	var walk func(Expr)
	walk = func(e Expr) {
		switch n := e.(type) {
		case nil:
		case *Name:
			out = append(out, n.Value)
		case *UnOp:
			walk(n.X)
		case *BinOp:
			walk(n.L)
			walk(n.R)
		case *CondExpr:
			walk(n.If)
			walk(n.Else)
		}
	}
	walk(e)
	return out
}

// kindPreservingNumericBuiltins are the calls that return their numeric argument as the same kind it
// arrived, which is what lets a `return abs(x)` of a rebound parameter travel in the double word.
// `round` is in the set with a condition the scan applies: only the two-argument form, whose answer
// is the kind its first argument arrived as (Gap R.69, ADR 0263).
var kindPreservingNumericBuiltins = map[string]bool{"abs": true, "float": true, "round": true}

// isArithmeticOp is the operator set whose result is a number whatever the operands are, which is the
// set the double-return promotion may carry. Ordering, equality and `not` answer with a bool word and
// unary minus with a plain i32, so a promoted function returning one would store an i32 in a double `ret`.
func isArithmeticOp(op string) bool {
	switch op {
	case "+", "-", "*", "/", "//", "%", "**":
		return true
	}
	return false
}

// scanBareReturns records the name of every `return <name>` in a statement list, including the
// returns nested in compound statements. A nested def, lambda or class body is not walked: its
// returns belong to that function's own convention, and it gets this question asked of it when it is
// emitted.
func scanBareReturns(list []Stmt, into map[string]bool) {
	var walk func([]Stmt)
	walk = func(stmts []Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *ReturnStmt:
				switch r := s.Expr.(type) {
				case *Name:
					into[r.Value] = true
				case *Call:
					// Only the builtins that hand the number back as the same kind it
					// came in: `return abs(x)` and `return float(x)` of such a parameter
					// are the double wearing a function call. `int(x)`/`round(x)` answer
					// with an int whatever arrives and `str(x)` with text, so promoting
					// those would put an i32 (or a string index) in the double's word —
					// and a user callee's return word is its own question, not this one.
					if fn, ok := r.Fn.(*Name); ok {
						callArgs := r.Args
						if fn.Value == "round" {
							// `round(x, ndigits)` is kind-preserving in its *first* argument and
							// nothing else: the float whose point moved leaves a float, the int stays
							// an int, and the digit count is an integer whoever wrote it (roadmap
							// Gap R.69, ADR 0263). Walking it would promote a function because a name
							// in its digit count happens to hold a double.
							if len(callArgs) != 2 {
								continue
							}
							callArgs = callArgs[:1]
						} else if !kindPreservingNumericBuiltins[fn.Value] {
							continue
						}
						for _, a := range callArgs {
							// `return abs(-x)` is the same double wearing a call and a negation:
							// the shape under the call still names the parameter, and the abs
							// lowering reads it as a double. Missing that arm left the return
							// word with nothing to choose from and the module came out
							// `ret i32` under a `define double` (roadmap Gap R.3c, ADR 0166).
							for _, nm := range namedNumericLeaves(a) {
								into[nm] = true
							}
						}
					}
					continue
				case *UnOp:
					// `-x` is arithmetic too, and the body already knows how to say it:
					// the negation of a float-rebound parameter is emitted `fsub double
					// 0.0, %v`, so the promoted function's `ret double` has its double.
					// Before this arm, `return -x` reached no gate at all — the return
					// word came from the last line's syntax, which reads a unary minus
					// as nothing, and the module came out `ret i32` under a
					// `define double`: the verifier's rejection, exit 2 (roadmap
					// Gap R.3c, ADR 0166).
					if n, ok := r.X.(*Name); ok && r.Op == "-" {
						into[n.Value] = true
					}
				case *CondExpr:
					// `return x if x > 2 else 0.0` reads the rebound parameter on one of the
					// two arms, so the answer's word is the arm's, and the `select` that
					// chooses it chooses two doubles (roadmap Gap R.102, ADR 0262).
					for _, arm := range []Expr{r.If, r.Else} {
						for _, nm := range namedNumericLeaves(arm) {
							into[nm] = true
						}
					}
				case *BinOp:
					// Arithmetic only. `return x > 2` of such a parameter answers with a
					// bool word, and promoting the function to a double return would put
					// an i32 in the double's word. That shape stays a refusal (roadmap
					// Gap R.3c, ADR 0166).
					if !isArithmeticOp(r.Op) {
						continue
					}
					// A side of the product may itself be the ternary whose arm is the name
					// the body rebound, and the gate has to see through that wrapper:
					// `return (x if x > 2 else 0.0) * 2` carries the parameter's identity
					// through the conditional, and a gate that stops at the wrapper emits an
					// i32 `ret` for a program whose answer is a double (roadmap Gap R.102,
					// ADR 0262; the same lesson ADR 0254 learned for the bare name).
					for _, side := range []Expr{r.L, r.R} {
						for _, nm := range namedNumericLeaves(side) {
							into[nm] = true
						}
					}
				}
			case *IfStmt:
				walk(s.Then)
				for _, e := range s.Elifs {
					walk(e.Then)
				}
				walk(s.Else)
			case *WhileStmt:
				walk(s.Body)
				walk(s.Else)
			case *ForStmt:
				walk(s.Body)
				walk(s.Else)
			case *WithStmt:
				walk(s.Body)
			case *TryStmt:
				walk(s.Body)
				for _, e := range s.Excepts {
					walk(e.Body)
				}
				walk(s.Finally)
			case *MatchStmt:
				for _, c := range s.Cases {
					walk(c.Body)
				}
			}
		}
	}
	walk(list)
}

// scanRebinds records every name a statement list binds, without descending into a
// nested function or lambda, whose parameters shadow the outer ones.
func scanRebinds(list []Stmt, into map[string][]Expr) {
	var walk func([]Stmt)
	rec := func(e Expr, val Expr) {
		nm, ok := e.(*Name)
		if !ok {
			return
		}
		into[nm.Value] = append(into[nm.Value], val)
	}
	var bindPattern func(Expr)
	bindPattern = func(p Expr) {
		// `case y:` binds y; a destructuring pattern binds through its parts.
		switch n := p.(type) {
		case *Name:
			if n.Value != "_" {
				rec(n, nil)
			}
		case *Tuple:
			for _, el := range n.Elems {
				bindPattern(el)
			}
		case *ListLit:
			for _, el := range n.Elems {
				bindPattern(el)
			}
		case *Call: // `case Point(x, y):` — the arguments are patterns
			for _, a := range n.Args {
				bindPattern(a)
			}
		}
	}
	walk = func(stmts []Stmt) {
		for _, st := range stmts {
			switch s := st.(type) {
			case *AssignStmt:
				rec(s.Target, s.Value)
				// `[n for n in ...]` binds n inside the assignment's *value*, and
				// codegen reuses that name as a local of this function.
				scanCompBinds(s.Value, into)
				switch t := s.Target.(type) {
				case *ListLit:
					for _, el := range t.Elems {
						rec(el, nil)
					}
				case *Tuple:
					for _, el := range t.Elems {
						rec(el, nil)
					}
				}
			case *AugAssignStmt:
				if s.Op == "/" {
					// `/=` is true division: the answer is a float whatever the operands were (ADR 0253's rule
					// for `/`), so the name's newest value is a double and the function that returns it needs
					// the double's return word — a fact the operator states and the written value (`2`) does not
					// (roadmap L11.6, Gap P.1).
					rec(s.Target, &BinOp{Op: "/", L: s.Target, R: s.Value, Src: s.Src})
					continue
				}
				rec(s.Target, s.Value)
			case *ForStmt:
				rec(s.Var, nil)
				walk(s.Body)
				walk(s.Else)
			case *WithStmt:
				if s.As != nil {
					rec(s.As, nil)
				}
				walk(s.Body)
			case *IfStmt:
				walk(s.Then)
				for _, e := range s.Elifs {
					walk(e.Then)
				}
				walk(s.Else)
			case *WhileStmt:
				walk(s.Body)
				walk(s.Else)
			case *TryStmt:
				walk(s.Body)
				for _, e := range s.Excepts {
					if e.Exn != nil {
						// `except E as e` rebinds the name; recording the class name
						// too costs an unused alloca at worst, and missing a real
						// rebind is the expensive direction.
						into[e.Exn.Value] = append(into[e.Exn.Value], nil)
					}
					walk(e.Body)
				}
				walk(s.Finally)
			case *MatchStmt:
				for _, c := range s.Cases {
					bindPattern(c.Pattern)
					for _, o := range c.Or {
						bindPattern(o)
					}
					walk(c.Body)
				}
			case *ExprStmt:
				scanCompBinds(s.Expr, into)
			case *ReturnStmt:
				if s.Expr != nil {
					scanCompBinds(s.Expr, into)
				}
			}
			// *FuncDef, *Lambda and *ClassDef are deliberately not descended into:
			// that scope owns those names.
		}
	}
	walk(list)
}

// scanCompBinds records the loop variable of a comprehension inside an expression.
// Codegen reuses that name as a local of the enclosing function, so a parameter of
// the same name is overwritten just the same.
func scanCompBinds(e Expr, into map[string][]Expr) {
	var walk func(Expr)
	walk = func(x Expr) {
		switch n := x.(type) {
		case *Comp:
			if n.ForVar != nil {
				into[n.ForVar.Value] = append(into[n.ForVar.Value], nil)
			}
			for _, el := range n.Elems {
				walk(el)
			}
			for _, k := range n.Keys {
				walk(k)
			}
			for _, v := range n.Vals {
				walk(v)
			}
			walk(n.Iter)
			walk(n.Cond)
		case *BinOp:
			walk(n.L)
			walk(n.R)
		case *UnOp:
			walk(n.X)
		case *Call:
			walk(n.Fn)
			for _, a := range n.Args {
				walk(a)
			}
		case *Index:
			walk(n.Obj)
			walk(n.Idx)
		case *CondExpr:
			walk(n.Cond)
			walk(n.If)
			walk(n.Else)
		case *Attr:
			walk(n.Obj)
		case *Tuple:
			for _, el := range n.Elems {
				walk(el)
			}
		case *ListLit:
			for _, el := range n.Elems {
				walk(el)
			}
		}
	}
	walk(e)
}

// floatFromSyntax is the stateless half of the float question codegen asks: a float
// literal, a true division, a sum or product with one, or a node the checker inferred
// as float. (*irGen).isFloat adds the compiler's own knowledge — which locals are
// floats — and is what the compiled path passes to reboundParams; this is the version
// the analysis can be tested against, and the fallback when there is no compiler.
func floatFromSyntax(e Expr) bool {
	switch n := e.(type) {
	case *FloatLit:
		return true
	case *UnOp:
		return n.Op == "-" && floatFromSyntax(n.X)
	case *BinOp:
		if n.Op == "/" {
			return true
		}
		switch n.Op {
		case "+", "-", "*", "%", "//":
			return floatFromSyntax(n.L) || floatFromSyntax(n.R)
		}
		return false
	case *Name:
		return n.Ty == "float"
	case *Call:
		return n.Ty == "float"
	case *Index:
		return n.Ty == "float"
	case *CondExpr:
		return floatFromSyntax(n.If) || floatFromSyntax(n.Else)
	}
	return false
}
