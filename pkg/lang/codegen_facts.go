// The static questions the roads ask one another: is this name bound, is this expression a text/an int/None,
// what is this callee's name, is this builtin shadowed. Predicates only — no road emits IR from here.
package lang

import (
	"fmt"
	"strings"
)

// iterableIsRuntimeString reports whether a `for … in` right-hand side is text that codegen
// cannot walk: an interpolated or concatenated string, a folded string expression, a variable
// holding a @str_tab index, or a call to a function known to return one. A *literal* is not
// included: the loop unrolls it above (ADR 0208).
func (g *irGen) iterableIsRuntimeString(e Expr) bool {
	if e == nil {
		return false
	}
	if _, ok := e.(*StrLit); ok {
		return false
	}
	if _, ok := e.(*ListLit); ok {
		return false
	}
	if isStringExpr(e) {
		return true
	}
	if _, ok := g.stringVal(e); ok {
		return true
	}
	if nm, ok := e.(*Name); ok {
		if g.internedVars[nm.Value] {
			return true
		}
		if g.strVals != nil {
			if _, isStr := g.strVals[nm.Value]; isStr {
				return true
			}
		}
		return false
	}
	if c, ok := e.(*Call); ok {
		return g.callReturnsStr(c)
	}
	return false
}
func isScalarConst(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit, *StrLit:
		return true
	case *BinOp:
		// `x = 1 + 2` is a scalar constant too: codegen folds it, so the variable
		// cannot hold a heap handle and does not need a root entry (which also keeps
		// the root runtime out of purely scalar modules).
		return isScalarConst(n.L) && isScalarConst(n.R)
	case *UnOp:
		return isScalarConst(n.X)
	}
	return false
}

// intLiteralOf reads an int literal, including a negated one, which is how the parser stores `-3`.
func intLiteralOf(e Expr) (int64, bool) {
	switch v := e.(type) {
	case *IntLit:
		return v.Value, true
	case *UnOp:
		if v.Op == "-" {
			if x, ok := intLiteralOf(v.X); ok {
				return -x, true
			}
		}
		if v.Op == "+" {
			return intLiteralOf(v.X)
		}
	}
	return 0, false
}

// staticHandleName is the question the comprehension road asks before it walks a container and the slice
// road asks before it calls `rt_slice`: does this expression lower to a COMPILE-TIME GLOBAL (`@.lstN`)
// rather than a heap handle? If so the caller must build a real heap object before handing it to a
// runtime helper, because those walk `@heap` by index and a global address is not one.
func (g *irGen) staticHandleName(e Expr) string {
	switch v := e.(type) {
	case *Name:
		if g.builtinShadowed(v.Value) {
			return ""
		}
		return v.Value
	}
	return ""
}

// comparisonOpForTextGuard reports that the BinOp now being lowered into the float arms compares
// rather than computes. `==`, `!=` and the orderings answer a verdict for a pair of unlike kinds —
// CPython answers `1.0 == "a"` with False, and this backend answers 0 — so their operands may be
// texts and the equality road lowers both sides through the double lift. Only arithmetic may refuse
// a text (roadmap L11.2, Gap R.165, ADR 0282).
func comparisonOpForTextGuard(n *BinOp) bool {
	if n == nil {
		return false
	}
	switch n.Op {
	case "==", "!=", "<", "<=", ">", ">=", "in", "not in", "is", "is not":
		return true
	}
	return false
}

// exprTyName returns the checker's inferred type name for an expression, for the
// node kinds whose binding a container registration depends on.
func exprTyName(e Expr) string {
	switch n := e.(type) {
	case *FloatLit:
		// Naming matters in a refusal: this helper filled the blank in "an element slot is an i32
		// word and <blank> has no representation in one", which told the reader nothing about
		// what to change (ADR 0226).
		return "a float value"
	case *IntLit:
		return "an integer"
	case *StrLit:
		return "a string"
	case *BoolLit:
		return "a bool"
	case *NoneLit:
		return "None"
	case *Call:
		return n.Ty
	case *Name:
		return n.Ty
	case *Index:
		return n.Ty
	case *Attr:
		return n.Ty
	case *CondExpr:
		return n.Ty
	case *ListLit:
		return n.Ty
	case *DictLit:
		return n.Ty
	case *SetLit:
		return n.Ty
	}
	return ""
}

// containerKindFromTy maps an inferred type name ("dict[any, any]", "list[int]",
// "set[int]") to the runtime container kind, or "" when it is not a container.
func containerKindFromTy(name string) string {
	switch {
	case strings.HasPrefix(name, "dict["):
		return "dict"
	case strings.HasPrefix(name, "list["):
		return "list"
	case strings.HasPrefix(name, "set["):
		return "set"
	}
	return ""
}

// nameShadowed applies a (possibly nil) shadow predicate.
func nameShadowed(shadowed func(string) bool, name string) bool {
	return shadowed != nil && shadowed(name)
}

// builtinShadowed reports whether the program defines a function of this name itself —
// in which case the name belongs to the program, and codegen must not read a call to it
// as the built-in of that name.
//
// Both the record and CPython let a `def` shadow a built-in, and the record
// already resolves it that way; the compiled path read the call by name instead, so
//
//	def float(x):
//	    return x + 7
//	print(float(1))
//
// printed 8 interpreted and 1.0 compiled — the float-shape helpers saw the *name* `float`
// and folded the call as the conversion, never asking whether the program had defined it.
// `str` and `chr` were worse still: the call was emitted as the user's function and then
// *used* as the builtin's string result, which llc rejected (roadmap Gap R.6, ADR 0199).
//
// The rule is not "refuse the name" — `float`, `str`, `len`, `sum` are names people choose
// deliberately — it is that the program's definition wins, the way every other scope rule
// in the language says the innermost declaration wins.
func (g *irGen) builtinShadowed(name string) bool {
	if name == "" {
		return false
	}
	if g.funcs[name] {
		return true
	}
	if fd, ok := g.fds[name]; ok && fd != nil {
		return true
	}
	return false
}

// builtinCallAs reports whether a call may still be read as the built-in `name`: the callee
// has to be that bare name, and the program must not have defined it. Every shape decision
// keyed on a built-in name goes through this, so there is one place that decides.
func (g *irGen) builtinCallAs(c *Call, name string) bool {
	n, ok := c.Fn.(*Name)
	return ok && n.Value == name && !g.builtinShadowed(name)
}

// paramNameOf asks whether an expression is a plain name the enclosing function received as a
// parameter — a value whose kind this road cannot see, because the caller writes it.
func (g *irGen) paramNameOf(e Expr) bool {
	nm, ok := e.(*Name)
	if !ok || g.curFunc == "" {
		return false
	}
	fd, known := g.fds[g.curFunc]
	if !known {
		return false
	}
	for _, p := range fd.Params {
		if p.Name == nm.Value {
			return true
		}
	}
	return false
}

// paramNameByName is nameHoldsContainer's question for the enclosing function's own parameter list.
func (g *irGen) paramNameByName(nm string) bool {
	if g.curFunc == "" {
		return false
	}
	fd, known := g.fds[g.curFunc]
	if !known {
		return false
	}
	for _, p := range fd.Params {
		if p.Name == nm {
			return true
		}
	}
	return false
}

// chainOperandIsCheap reports whether an operand can be lowered at each of its uses without changing
// the program: a literal or a plain name reads a value and does nothing else. A call, an index or an
// attribute has to be slotted, because reading it twice is the very bug this exists to fix.
func chainOperandIsCheap(e Expr) bool {
	switch e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit, *Name:
		return true
	}
	return false
}

// isCallExpr reports whether an expression is a call — the shape a comprehension has to walk at runtime,
// because nothing here knows the container's length until the program computes it.
func isCallExpr(e Expr) bool {
	_, ok := e.(*Call)
	return ok
}

// constIntVal resolves a literal expression to a compile-time integer value
// (IntLit, BoolLit, FloatLit truncation, NoneLit -> 0). Used by builtins that
// fold keyword/positional truthiness (e.g. sorted's reverse=True) at codegen.
func (g *irGen) constIntVal(e Expr) (int64, error) {
	switch n := e.(type) {
	case *IntLit:
		return n.Value, nil
	case *BoolLit:
		if n.Value {
			return 1, nil
		}
		return 0, nil
	case *FloatLit:
		return int64(n.Value), nil
	case *NoneLit:
		return 0, nil
	default:
		return 0, fmt.Errorf("codegen: reverse flag must be a literal")
	}
}

// isNoneExpr reports whether an expression evaluates to the None singleton. The AOT
// representation of a value is an untagged i32, so unlike the record this cannot be
// decided at run time: None is recognised where the source says so — the literal, a variable
// whose latest assignment was None, and a call to a function whose body never returns a value
// (ADR 0172).
// forgetVarBool retires a name's boolness where the binding is not an expression the
// front end can read — a loop variable, a comprehension counter, a parameter. The name
// may once have been assigned a comparison, and a program must not print True for the
// number the iterable bound into it afterwards (ADR 0172's latest-binding rule, carried
// to bools by ADR 0257).
func (g *irGen) forgetVarBool(name string) {
	delete(g.boolVars, name)
}

// printsAsBool answers whether print and str render this expression as True/False. The
// value itself is the 0/1 the comparison produced — a bool has no word of its own to carry
// a kind, and the tagged value word that would give it one is the L11.1 destination, not
// this rung — so the answer comes from the AST, where it has always been. the record's
// print, the CLI's --json type field and this backend all ask the one predicate in
// pkg/lang/boolvalue.go, so both legs cannot disagree about what is a bool
// (roadmap L11.1 step 2, ADR 0257).
func (g *irGen) printsAsBool(e Expr) bool {
	return IsBoolExpr(e, BoolEnv{Vars: g.boolVars, Lookup: g.lookupFuncDef, Shadowed: g.builtinShadowed, Instance: g.instanceOperand, NumericCandidate: g.foldableCandidate, CalleeVars: g.calleeBoolVars, MethodLookup: g.methodAnswer, SelfLookup: g.selfAnswer, FieldVerdict: g.fieldAnswer})
}
func (g *irGen) isNoneExpr(e Expr) bool {
	switch v := e.(type) {
	case *NoneLit:
		return true
	case *Name:
		return g.noneVars[v.Value]
	case *Call:
		// A method that mutates its receiver in place answers the VOID, not the container: `append`,
		// `add`, `discard`, `remove`, `clear` all hand back None in the reference. The mutation roads
		// lower to no value at all in this backend -- which is correct for the statement `xs.append(2)`
		// and left the PRINT door emitting `printf(i8* @.fmt1, i32 )`, a call with a missing operand
		// that llc rejects as an invalid module, so the program spent exit 2 (ADR 0166's forbidden
		// class). The answer is asked of the CALL SHAPE here, in the one predicate print, str() and
		// the REPL already consult, so the void cannot be lowered without being recognised.
		// Roadmap Gap R.187, ADR 0300 -- the same rule as a builtin that hands back void (ADR 0291).
		if g.callIsInPlaceMutation(v) {
			return true
		}
		if nm, ok := v.Fn.(*Name); ok {
			if g.answer.carries(nm.Value) && !g.answer.answersOnlyTheVoid(nm.Value) {
				// Which arm ran is a run-time fact, and the word it stores says so: the print road that
				// claims the void here would print `None` for the arm that answered `7` (roadmap Gap R.219,
				// Gap R.228, ADR 0368).
				return false
			}
			if fd, ok2 := g.fds[nm.Value]; ok2 && !fdReturnsValue(fd) {
				return true
			}
			// min/max of all-None candidates choose a None. Their untagged representation is
			// 0, and without this answer print said 0 rather than None (roadmap Gap R.73).
			if (nm.Value == "min" || nm.Value == "max") && !g.builtinShadowed(nm.Value) && g.minMaxCallReturnsNone(v) {
				return true
			}
		}
		// `d.get(key)` over a literal dict with the key absent lowers to the void word, because
		// that is what a void is in this backend. The fold KNOWS the key is absent — it is the
		// same fact that let it answer at all — so the print road has to be told, or it renders
		// the word: `print({"a": 1}.get("z"))` said `0` where the reference says `None` (Gap R.174,
		// the print-side half of Gap R.171). Asked of the fold's own condition, not of a guess.
		//
		// The same lookup answers for the slot itself and for an explicit default: `{"k": None}.get("k")`
		// hits a slot holding a void and printed `0`, and `{"k": None}.get("zz", None)` defaults to one
		// (Gap R.180 — the text half of that row is in stringVal beside this).
		if attr, isAttr := v.Fn.(*Attr); isAttr && attr.Name.Value == "get" && (len(v.Args) == 1 || len(v.Args) == 2) {
			if dl, ok := attr.Obj.(*DictLit); ok {
				if g.dictFoldMisses(dl, v.Args[0]) {
					if len(v.Args) == 2 {
						return g.isNoneExpr(v.Args[1])
					}
					return true
				}
				if slot, found := g.dictFoldSlot(dl, v.Args[0]); found {
					return g.isNoneExpr(slot)
				}
			}
		}
	}
	return false
}

// calleeName renders a call's callee as the source text the user wrote, so diagnostics
// name `list(...)` rather than dumping the AST node (`&{list {2 7} fn() -> list[any]}`).
func calleeName(c *Call) string {
	if n, ok := c.Fn.(*Name); ok {
		return n.Value
	}
	return "<expression>"
}

// nameIsBound reports whether nm names something codegen has actually allocated or
// predeclared, so a reference to it can be lowered. Everything here is a name that can
// legally appear as a value without a visible alloca: parameters, captured env slots,
// container handles, folded constants, module globals, classes, and predeclared names.
func (g *irGen) nameIsBound(nm string) bool {
	if g.allocd[nm] || g.funcs[nm] {
		return true
	}
	if g.boundFlags[nm] {
		// A name carrying a written-flag has a slot: emitBoundAllocas gave it one, precisely because
		// no store had to have run for it to be readable. Refusing here would be codegen blaming the
		// program for a name the body itself binds -- `def f(): print(v); v = 2` is a program, and
		// CPython runs it (raising UnboundLocalError at the read, which is what the flag does too).
		return true
	}
	if _, ok := g.params[nm]; ok {
		return true
	}
	if _, ok := g.funcBind[nm]; ok {
		return true
	}
	if _, ok := g.externs[nm]; ok {
		return true
	}
	if g.listVars[nm] || g.runtimeDicts[nm] || g.runtimeSets[nm] {
		return true
	}
	if g.strVals != nil {
		if _, ok := g.strVals[nm]; ok {
			return true
		}
	}
	if g.floatVars != nil && g.floatVars[nm] {
		return true
	}
	if g.unionVars[nm] || g.classIDs[nm] != 0 || g.classInfos[nm] != nil {
		return true
	}
	if _, ok := g.constBindings[nm]; ok {
		return true
	}
	if g.envCaptures != nil {
		if _, ok := g.envCaptures[nm]; ok {
			return true
		}
	}
	if g.curModGlobals != nil {
		if _, ok := g.curModGlobals[nm]; ok {
			return true
		}
	}
	if g.curModParams != nil {
		if _, ok := g.curModParams[nm]; ok {
			return true
		}
	}
	if g.imports != nil {
		// `import mod` binds `mod` as a value-ish name (mod.var / mod.fn()).
		for mod := range g.imports.Globals {
			if mod == nm {
				return true
			}
		}
		for mod := range g.imports.Funcs {
			if mod == nm {
				return true
			}
		}
	}
	if isExnClass(nm) || isPredeclaredName(nm) {
		return true
	}
	return false
}
