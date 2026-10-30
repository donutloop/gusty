package lang

// unary_plus.go is the unary plus's own road — the reference's `__pos__` — and it exists because the
// grammar had no prefix `+` rule at all (roadmap **Gap R.244**): `print(+1)` died in the parser with
// `parse error at 1:7: unexpected token` at exit 1, where the reference answers `1`, and `print(+1.5)`,
// `print(+True)`, `print(-+1)` and `x = 5` / `print(x-+1)` died with it.
//
// Why this is a door and not a desugaring. The obvious lowering — `+x` is `x + 0`, the additive identity —
// was built first and measured against the reference, and it is wrong in three ways the reference is not:
//
//   - `print(+1.5)` printed `1`, and `x = 5.5` / `print(+x)` printed `0`, because the `0` that was handed
//     to the binary road is an INT literal and that road chooses its domain from the operand spellings;
//   - `print(+None)` printed `0` at exit 0, where the reference raises;
//   - `+[1, 2]` raised `can only concatenate list (not "int" to list)` and `+"a"` was refused outright —
//     sentences about a program with a `0` the author never wrote, where the reference says
//     `bad operand type for unary +: 'list'` and `… : 'str'`.
//
// A neutral element is a guess about the domain, and the domain is exactly what this road exists to ask,
// which is why CPython has `__pos__` rather than `__add__` with a zero. The road therefore has the unary
// minus's SHAPE — one question, asked of the door that already answers it (ADR 0266's kind table,
// ADR 0271's one-sentence-owner table) — and differs from it in the two places the reference differs: the
// sentence it writes, and the value it hands back.
//
// What the road refuses is as deliberate as what it answers. `-x` can afford to hand its operand to the
// numeric road and trust that road's own refusals, because a subtraction of a handle is a number the
// printer will happily show; `+x` hands back the operand ITSELF, so an operand whose kind the compiler
// cannot name would print a text, a container or an exception message as the answer to a program the
// reference stops on. The road therefore answers only where the operand's number-ness is a fact the
// records state (`unaryPosOperandIsANumber`), raises where the operand's kind is a signless one the door
// names, and refuses in words everywhere else — a refusal is exit 1 and a roadmap row, and the
// wrong-answer-at-exit-0 family is what this loop has never been able to close.

import (
	"fmt"
	"strings"
)

// unaryPosOperandIsANumber is the road's positive proof: the compiler can state, from the records it
// already keeps, that this operand holds a number (an int, a float, or a bool, which the reference's
// `__pos__` answers as an int). It is NOT the same question as the raise door's: `negationOperandKind`
// names the kinds that have no sign so the sentence can quote them, and answers "no kind" for everything
// it cannot see, which is the wrong shape of answer for a road that hands the operand back. Here the
// burden is the other way round — silence is a refusal, not a permission.
func (g *irGen) unaryPosOperandIsANumber(e Expr) bool {
	if g == nil || e == nil {
		return false
	}
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *UnOp:
		if n.Op == "not" {
			// `not` answers a boolean VALUE whatever stands behind it, and the reference's `__pos__` of a
			// bool is an int, so `print(+not 1)` is `0` on both legs without asking what `1` was.
			return true
		}
		// `-(-x)` is a number too: the operand of a `+` may itself be a prefix the road already answers,
		// which is how `print(-+1)` and `print(+-1)` reach the reference's `-1` and `-1`.
		return (n.Op == "-" || n.Op == "+") && g.unaryPosOperandIsANumber(n.X)
	case *BinOp:
		// A verdict is a boolean, which `__pos__` answers as an int: `print(+(1 < 2))` is `1`.
		switch n.Op {
		case "<", "<=", ">", ">=", "==", "!=", "in", "is":
			return true
		}
		// Or an arithmetic operator whose every arm is proven: `"a" + "b"` is a TEXT and the reference
		// stops on `+("a" + "b")`, and `and`/`or` answer one of their operands, whose kind is the arms'
		// business rather than this road's.
		switch n.Op {
		case "+", "-", "*", "/", "//", "%", "**":
			return g.unaryPosOperandIsANumber(n.L) && g.unaryPosOperandIsANumber(n.R)
		}
		return false
	case *ChainCompare:
		// A comparison chain answers a verdict, as each of its comparisons does.
		return true
	case *CondExpr:
		// `+x if c else y` hands back one of the arms, so both must be numbers; which one runs is a
		// run-time question, but neither answer can be a text.
		return g.unaryPosOperandIsANumber(n.If) && g.unaryPosOperandIsANumber(n.Else)
	case *Name:
		return g.unaryPosNameIsANumber(n.Value)
	case *Call:
		return g.unaryPosCallIsANumber(n)
	case *Index:
		return g.unaryPosSlotIsANumber(n)
	case *Attr:
		// `mod.CONST` for a data import spells its kind in the module rather than in the importing
		// program; the fold that carries the literal is the same door the raise asks.
		if lit, folded := g.foldedModuleAttr(n); folded {
			return g.unaryPosOperandIsANumber(lit)
		}
		return false
	}
	return false
}

// unaryPosNameIsANumber proves a name. What is wanted is not "nothing bad is recorded" — the raise door's
// list of records is the kinds it can NAME, and it has never had a reason to look at the exception
// message, the lambda binding, or the alias a container was copied through. So the proof walks the whole
// set of records that say this name holds something with no sign, and accepts a float or a bool binding
// (the reference's `+True` is the int 1, and the word it prints in is the int word because the node the
// printer is standing on is this `+`, not a `BoolLit`).
func (g *irGen) unaryPosNameIsANumber(name string) bool {
	if name == "" || g == nil {
		return false
	}
	// A parameter's kind is whatever the call sites hand it: `def f(v): return +v` / `f("a")` is a program
	// whose operand the body cannot name, and the road refuses rather than trusts (roadmap Gap R.564).
	if _, isParam := g.params[name]; isParam {
		return false
	}
	// Its kind lives in the object beside the word, not in the source.
	if g.taggedVars != nil && g.taggedVars[name] {
		return false
	}
	if g.unionVars != nil && g.unionVars[name] {
		return false
	}
	if g.strVals[name] != "" || g.internedVars[name] {
		return false
	}
	if g.noneVars[name] {
		return false
	}
	if g.caughtVars[name] {
		return false
	}
	if g.listVars[name] || g.mixedLists[name] || g.runtimeDicts[name] || g.runtimeSets[name] {
		return false
	}
	if g.mixedDicts[name] || g.mixedSets[name] || g.tupleVars[name] {
		return false
	}
	if g.varClasses[name] != "" || g.lambdas[name] != "" || g.funcBind[name] != "" {
		return false
	}
	if g.containerAlias != nil && g.containerAlias[name] != "" {
		return false
	}
	if g.floatVars[name] || g.boolVars[name] {
		return true
	}
	if g.floatTemps != nil && g.floatTemps[name] {
		return true
	}
	// A name the program never bound is the front end's business, not this road's: it refuses `zzz`
	// before codegen, so the walk ends here on names that were assigned something none of the records
	// above describe — which is the number case, and the same negative space the unary minus answers on.
	return g.allocd != nil && g.allocd[name]
}

// unaryPosSlotIsANumber proves a slot read. Three records can state it: the folded element of a literal
// the compiler can read (`[1, 2][0]`), the element-kind table the assignment walk keeps for the container
// a name was last spelled as, and the literal kind tree that proves a container built rather than spelled
// holds nothing but numbers — the same proof ADR 0265's tagged door requires before it will answer a `+`
// or a `*`, for the same reason: what a slot can hold that is not a number is what makes the answer wrong.
func (g *irGen) unaryPosSlotIsANumber(ix *Index) bool {
	if kind, ok := g.constantSubscriptKind(ix); ok {
		switch kind {
		case "int", "float", "bool":
			return true
		}
		return false
	}
	if fam, ok := g.slotNumberFamily(ix.Obj); ok {
		// "int", "float" and "mixed" say every element the literal spells is a number; "none" says none is,
		// and that shape never reaches here — the raise door names it first.
		switch fam {
		case "int", "float", "mixed":
			return true
		}
		return false
	}
	nm, depth := chainDepth(ix)
	if nm == nil || depth < 1 || g.numChains == nil {
		return false
	}
	return g.numChains.numeric(nm.Value, depth)
}

// unaryPosCallIsANumber proves a callee's answer. A builtin whose contract is "a number or a stop" is
// listed by name — `min`/`max` are NOT, because they answer whichever operand wins and that operand may
// be a text. A function the program wrote is proven by walking the `return`s its body holds, which is the
// evidence `answerBodyReturns` already collects for the answer-kind road: every returned value must be a
// number, and a body that can walk off its end answers None and is not proven, because the reference stops
// on `+None` rather than answering it.
func (g *irGen) unaryPosCallIsANumber(c *Call) bool {
	if nm, isName := c.Fn.(*Name); isName {
		if !g.builtinShadowed(nm.Value) {
			switch nm.Value {
			case "int", "float", "len", "ord", "abs", "round", "pow", "sum":
				// Each answers a number or stops the program; none answers a value of another kind, so
				// handing the operand back cannot print a text. `bytes`/`complex`/`type` are absent because
				// they answer something the reference's `__pos__` would stop on.
				return true
			}
		}
		if fd, ok := g.fds[nm.Value]; ok && fd != nil {
			return g.funcBodyAnswersNumbers(fd)
		}
		if g.pairRetDone != nil && g.pairRetDone[nm.Value] {
			// A callee the pair road proved answers a number beside its tag (ADR 0273's answer direction).
			return true
		}
	}
	if lam, isLambda := c.Fn.(*Lambda); isLambda {
		if fd := lambdaFuncDef(lam); fd != nil {
			return g.funcBodyAnswersNumbers(fd)
		}
	}
	return false
}

// funcBodyAnswersNumbers walks a body for the one thing this road needs: every value the function can hand
// back is a number. A body that falls off its end returns None, and a bare `return` does too, so neither is
// proven — the reference stops on those, and a road that answered them would print the operand's word for
// a program that raises.
func (g *irGen) funcBodyAnswersNumbers(fd *FuncDef) bool {
	if fd == nil {
		return false
	}
	returns, fallsOff := answerBodyReturns(fd.Body)
	if fallsOff || len(returns) == 0 {
		return false
	}
	for _, r := range returns {
		if r == nil || r.Expr == nil || !g.unaryPosOperandIsANumber(r.Expr) {
			return false
		}
	}
	return true
}

// unaryPosRaise asks the raise door — the one the unary minus owns — what kind the operand is, and writes
// the reference's `bad operand type for unary +` sentence for it. It answers true when the road is
// terminated by a raise, in which case the caller's value is dead and only its type matters.
func (g *irGen) unaryPosRaise(b *strings.Builder, n *UnOp) (string, bool) {
	kind, named := g.negationOperandKind(n.X)
	if !named {
		return "", false
	}
	if kind != "" {
		return g.emitBadSignless(b, "pos", kind, n.Span()), true
	}
	// The slot reports more than one kind, so the raise is the table the object's tag walks.
	if ix, isIdx := n.X.(*Index); isIdx {
		if v, ok := g.emitBadSignlessOfSlot(b, ix, "pos", n.Span()); ok {
			return v, true
		}
	}
	return "", false
}

// valueUnaryPos lowers `+x` in the integer domain. The reference's `__pos__` on an int, a bool or a float
// IS the value, so the road is: raise if the operand has no sign, and otherwise lower the operand itself.
// There is no instruction here to get wrong, which is the point — the instruction the negation writes is
// the one place a `+` could disagree with the number the source wrote.
func (g *irGen) valueUnaryPos(b *strings.Builder, n *UnOp) (string, error) {
	if n == nil || n.X == nil {
		return "", fmt.Errorf("codegen: unary + with no operand")
	}
	if v, raised := g.unaryPosRaise(b, n); raised {
		return v, nil
	}
	if !g.unaryPosOperandIsANumber(n.X) {
		return "", g.unaryPosRefusal(n)
	}
	if g.isFloat(n.X) {
		if d := g.floatValue(b, n.X); d != "" {
			return d, nil
		}
		g.floatUnlowerable = fmt.Sprintf("the operand %s of the unary +", g.exprSummary(n.X))
		return "", fmt.Errorf("codegen: %s", g.floatUnlowerable)
	}
	return g.value(b, n.X)
}

// unaryPosDouble lowers `+x` in the double domain, for the caller that already knows the answer is a
// float: the print dispatch after isFloat named it, and a float arm that found the `+` beside a double.
// It asks the raise door first for the reason ADR 0266 wrote for the negation — a text lifted through
// `sitofp` prints the interned index as a number, which is the wrong answer this road must never reach.
func (g *irGen) unaryPosDouble(b *strings.Builder, n *UnOp) string {
	if n == nil || n.X == nil {
		return ""
	}
	if _, raised := g.unaryPosRaise(b, n); raised {
		return "0.000000e+00"
	}
	if !g.unaryPosOperandIsANumber(n.X) {
		g.floatUnlowerable = fmt.Sprintf("the operand %s of the unary +", g.exprSummary(n.X))
		return ""
	}
	d := g.floatValue(b, n.X)
	if d == "" {
		g.floatUnlowerable = fmt.Sprintf("the operand %s of the unary +", g.exprSummary(n.X))
	}
	return d
}

// unaryPosRefusal is the road declining to guess. It names what it could not do and why, in the words an
// agent acts on: which operand, which question, and the row that owes the work.
func (g *irGen) unaryPosRefusal(n *UnOp) error {
	return fmt.Errorf("codegen: cannot answer +%s: the operand's kind is not a number this backend can name, "+
		"and unary + hands the operand back unchanged (roadmap Gap R.564)", g.exprSummary(n.X))
}

// unaryPosIsTheRoad reports that an `*UnOp` node belongs to this file. The predicate sites across the
// numeric roads ask "is this the negation?" and answer "no" for anything else; a `+` is not a negation,
// but it IS the same operand underneath, and each of those roads decides whether to look through the
// prefix. This is the one question they can ask rather than each re-spelling `n.Op == "+"`.
func unaryPosIsTheRoad(n *UnOp) bool {
	return n != nil && n.Op == "+"
}

// unaryPosOperandOf returns what a prefix node hides: the operand a `+` stands in front of, for a road
// that has already decided to look through it. The negation has no such accessor because it must NOT look
// through its own prefix — the subtraction is its answer — while for `+` the operand is the whole answer.
func unaryPosOperandOf(n *UnOp) Expr {
	if !unaryPosIsTheRoad(n) {
		return nil
	}
	return n.X
}
