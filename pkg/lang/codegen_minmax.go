// min/max: the fold, the verdict, the trap and the print road all read the same candidate rule —
// numericWinner and minMaxCandidateValue in boolvalue.go (Gap R.124).
package lang

import (
	"fmt"
	"strings"
)

// minMaxReturnsFloat answers whether min/max of these elements denotes a float — which is a question
// about the *winning* element, not about whether any element is a float. When the winner is an
// integer, printing the call must print an integer: CPython's min([2.5, 1]) is 1, not 1.0.
func (g *irGen) minMaxReturnsFloat(elems []Expr, wantMin bool) (bool, bool) {
	vals, floats, ok := g.numericFoldElems(elems)
	if !ok || len(vals) == 0 {
		return false, false
	}
	return floats[numericWinner(vals, wantMin)], true
}

// minMaxFoldedWinner answers the varargs spelling of min/max (“min(a, b, ...)“) when every
// argument is a number the compiler can evaluate here. The winner is an *element*, not a
// comparison result, so the answer's kind is the winning element's own kind: CPython's
// “min(2.5, 1)“ is “1“, not “1.0“. That rule is numericFoldElems' whole reason for
// returning the parallel “isFloatElem“ list, and using it is what distinguishes a fold from a
// float promotion (roadmap Gap R.73, Gap R.104, ADR 0256).
func (g *irGen) minMaxFoldedWinner(args []Expr, wantMin bool) (float64, bool, bool) {
	vals, isFloatElem, ok := g.numericFoldElems(args)
	if !ok || len(vals) == 0 {
		return 0, false, false
	}
	best := numericWinner(vals, wantMin)
	return vals[best], isFloatElem[best], true
}

// minMaxVarargsAreText reports every argument being text. Text min/max is not a numeric fold
// dressed up as a string question: an @str_tab index compared with “icmp“ orders the intern
// table, not the texts, so the lowering has to ask rt_str_order (ADR 0248) instead.
func (g *irGen) minMaxVarargsAreText(args []Expr) bool {
	for _, a := range args {
		if !g.exprIsString(a) && !g.printsAsInternedStr(a) {
			return false
		}
	}
	return true
}

// minMaxStaticKind names an argument whose kind the compiler can see in the source: text, a
// number, None, or one of the three container literals. A Name whose type has not settled, or a
// call this backend cannot classify, is deliberately not named — then the fold refuses rather
// than invents a comparison.
func (g *irGen) minMaxStaticKind(e Expr) (string, bool) {
	if g.exprIsString(e) || g.printsAsInternedStr(e) {
		return "str", true
	}
	if g.isNoneExpr(e) {
		return "NoneType", true
	}
	switch e.(type) {
	case *ListLit:
		return "list", true
	case *SetLit:
		return "set", true
	case *DictLit:
		return "dict", true
	case *IntLit, *BoolLit:
		return "int", true
	case *FloatLit:
		return "float", true
	}
	if g.isFloat(e) {
		return "float", true
	}
	return "", false
}

// minMaxStaticOrderTrap emits the TypeError that the reference implementation raises when a fold
// reaches a candidate whose kind has no ordering against the incumbent's. The fold is source-order
// left to right, so the message names the candidate first and the incumbent second — exactly the
// two operands the failing `<` or `>` had. The branch ends the block, the handler (when there is
// one) takes it, and the label after the raise is where a caught program resumes.
func (g *irGen) minMaxStaticOrderTrap(b *strings.Builder, op, candidateKind, incumbentKind string, sp Span) (string, error) {
	bad := g.newLabel("minmaxbad")
	ok := g.newLabel("minmaxok")
	fmt.Fprintf(b, "  br label %%%s\n", bad)
	fmt.Fprintf(b, "%s:\n", bad)
	msg := fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, candidateKind, incumbentKind)
	g.raiseTo(b, exnCode("TypeError"), "TypeError", msg, sp)
	fmt.Fprintf(b, "%s:\n", ok)
	return "0", nil
}

// minMaxKindTrap scans values side by side (or a container's elements) and answers the first
// comparison the reference implementation could not make. It returns the empty string when every
// adjacent pair is either comparable in this backend or a kind the compiler cannot name; the
// caller then chooses between its numeric fold, its text fold, and its honest refusal.
func (g *irGen) minMaxKindTrap(b *strings.Builder, args []Expr, wantMin bool, sp Span) (string, bool, error) {
	if len(args) == 0 {
		return "", false, nil
	}
	bestKind, known := g.minMaxStaticKind(args[0])
	if !known {
		return "", false, nil
	}
	op := "<"
	if !wantMin {
		op = ">"
	}
	for _, a := range args[1:] {
		k, ok := g.minMaxStaticKind(a)
		if !ok {
			return "", false, nil
		}
		// The sentence names the kinds the *candidates* have, and a verdict's kind is bool: the
		// family says “compare it as a number“, the name says what to print, and ADR 0259 settled
		// that a slot holding a verdict raises a sentence that reads 'bool' (roadmap Gap R.117).
		winnerName := g.minMaxKindName(args[0], bestKind)
		candidateName := g.minMaxKindName(a, k)
		if bestKind == "int" || bestKind == "float" {
			if k == "int" || k == "float" {
				continue
			}
			v, err := g.minMaxStaticOrderTrap(b, op, candidateName, winnerName, sp)
			if err != nil {
				return "", true, err
			}
			return v, true, nil
		}
		if bestKind == "str" {
			if k == "str" {
				continue
			}
			v, err := g.minMaxStaticOrderTrap(b, op, candidateName, winnerName, sp)
			if err != nil {
				return "", true, err
			}
			return v, true, nil
		}
		if k == bestKind {
			continue // two values of the same kind need the helper that kind has, not this trap
		}
		v, err := g.minMaxStaticOrderTrap(b, op, candidateName, winnerName, sp)
		if err != nil {
			return "", true, err
		}
		return v, true, nil
	}
	return "", false, nil
}

// minMaxKindName is the word the TypeError sentence writes for a candidate whose comparison the
// fold settles statically. The family is what the trap compares; the name is what the program
// reads, and a verdict reads `bool` — the same word the record's compareOrder and ADR 0259's
// element tag use for the very same operand.
func (g *irGen) minMaxKindName(e Expr, family string) string {
	if family == "int" && g.printsAsBool(e) {
		return "bool"
	}
	return family
}

// emitScalarMinMax lowers “min(a, b, ...)“ / “max(a, b, ...)“ when the arguments are values
// side by side rather than one container. It answers three homogeneous worlds — the compile-time
// numeric fold, all-double arguments, and all-int arguments — plus text ordered by its content.
// A runtime mix of ints and doubles is deliberately not answered: the comparison can be decided
// at run time, but the *winner's kind* cannot cross out of the call without the tagged value word
// (L11.1), and emitting the double anyway is how “min(2.5, 1)“ came to print “1.0“.
func (g *irGen) emitScalarMinMax(b *strings.Builder, args []Expr, wantMin bool, sp Span) (string, error) {
	if len(args) < 2 {
		return "", fmt.Errorf("min/max expect at least one argument")
	}
	fnName := "min"
	if !wantMin {
		fnName = "max"
	}
	if trap, raised, err := g.minMaxKindTrap(b, args, wantMin, sp); raised || err != nil {
		return trap, err
	}
	if isContainerLiteral(args[0]) || isContainerLiteral(args[1]) {
		return "", fmt.Errorf("%s takes values side by side or one container, not a container among values; comparing two containers needs the tagged value word (roadmap L11.1, Gap R.73)", fnName)
	}
	if g.minMaxVarargsAreText(args) {
		return g.emitTextMinMax(b, args, wantMin)
	}
	if v, isF, ok := g.minMaxFoldedWinner(args, wantMin); ok {
		if isF {
			t := g.newTmp()
			materialiseFloat(b, t, v)

			return t, nil
		}
		return fmt.Sprintf("%d", int64(v)), nil
	}
	anyFloat := false
	for _, a := range args {
		if g.isFloat(a) {
			anyFloat = true
			break
		}
	}
	if anyFloat {
		allFloat := true
		for _, a := range args {
			if !g.isFloat(a) {
				allFloat = false
				break
			}
		}
		if !allFloat {
			return "", fmt.Errorf("%s of these arguments mixes a double and an int whose winner is not known until run time; the winner's own kind needs the tagged value word (roadmap L11.1, Gap R.73)", fnName)
		}
		best := g.floatValue(b, args[0])
		for _, a := range args[1:] {
			cand := g.floatValue(b, a)
			cmp := g.newTmp()
			if wantMin {
				fmt.Fprintf(b, "  %s = fcmp olt double %s, %s\n", cmp, cand, best)
			} else {
				fmt.Fprintf(b, "  %s = fcmp ogt double %s, %s\n", cmp, cand, best)
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", t, cmp, cand, best)
			best = t
		}
		return best, nil
	}
	best, err := g.value(b, args[0])
	if err != nil {
		return "", err
	}
	for _, a := range args[1:] {
		cand, err := g.value(b, a)
		if err != nil {
			return "", err
		}
		cmp := g.newTmp()
		if wantMin {
			fmt.Fprintf(b, "  %s = icmp slt i32 %s, %s\n", cmp, cand, best)
		} else {
			fmt.Fprintf(b, "  %s = icmp sgt i32 %s, %s\n", cmp, cand, best)
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, cand, best)
		best = t
	}
	return best, nil
}
func (g *irGen) emitTextMinMax(b *strings.Builder, args []Expr, wantMin bool) (string, error) {
	g.heapUsed = true // @str_tab and rt_str_order live in the heap runtime block
	best, err := g.value(b, args[0])
	if err != nil {
		return "", err
	}
	for _, a := range args[1:] {
		cand, err := g.value(b, a)
		if err != nil {
			return "", err
		}
		ord := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @rt_str_order(i32 %s, i32 %s)\n", ord, cand, best)
		cmp := g.newTmp()
		if wantMin {
			fmt.Fprintf(b, "  %s = icmp slt i32 %s, 0\n", cmp, ord)
		} else {
			fmt.Fprintf(b, "  %s = icmp sgt i32 %s, 0\n", cmp, ord)
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, cand, best)
		best = t
	}
	return best, nil
}

// minMaxCallReturnsText answers whether the value min/max hands back is an @str_tab index. The
// call's type comes from the candidates, not from a builtin table: text in, the winning text out;
// number in, number out. Printing the returned index through %d was how min(["b", "a"]) answered
// the intern table rather than "a" (roadmap Gap R.73).
func (g *irGen) minMaxCallReturnsText(c *Call) bool {
	// The question is about the candidates, never about the options: `min(["a", "bb"], key=len)` hands
	// back one of the texts, and reading `key=` as a second candidate made this answer "not text" and
	// printed the intern table's position instead (Gap R.249, ADR 0340).
	pos, _, _, _ := g.callOptions(c)
	if len(pos) == 0 {
		return false
	}
	if len(pos) > 1 {
		return g.minMaxVarargsAreText(pos)
	}
	var elems []Expr
	switch n := pos[0].(type) {
	case *ListLit:
		elems = n.Elems
	case *SetLit:
		elems = n.Elems
	case *DictLit:
		elems = n.Keys
	default:
		return g.exprIsString(pos[0]) || g.printsAsInternedStr(pos[0])
	}
	return len(elems) > 0 && g.minMaxVarargsAreText(elems)
}

// minMaxVarargsReturnsFloat answers whether the varargs spelling of min/max denotes a float.
// Like the container fold, the question is about the **winner**, not about whether any operand
// is a float; unlike it, a mixed int/double pair whose winner cannot be folded is not claimed to
// be a float, because returning a double here would silently turn an integer winner into “1.0“.
func (g *irGen) minMaxVarargsReturnsFloat(args []Expr, wantMin bool) bool {
	if _, isFloatWinner, ok := g.minMaxFoldedWinner(args, wantMin); ok {
		return isFloatWinner
	}
	if g.minMaxVarargsAreText(args) {
		return false
	}
	for _, a := range args {
		if !g.isFloat(a) {
			return false
		}
	}
	return true
}

// minMaxCallReturnsNone is the None counterpart of the same call-kind rule: a fold returns a
// candidate, so if every candidate is None the answer is None.
func (g *irGen) minMaxCallReturnsNone(c *Call) bool {
	if len(c.Args) == 0 {
		return false
	}
	if len(c.Args) > 1 {
		for _, a := range c.Args {
			if !g.isNoneExpr(a) {
				return false
			}
		}
		return true
	}
	var elems []Expr
	switch n := c.Args[0].(type) {
	case *ListLit:
		elems = n.Elems
	case *SetLit:
		elems = n.Elems
	case *DictLit:
		elems = n.Keys
	default:
		return g.isNoneExpr(c.Args[0])
	}
	for _, e := range elems {
		if !g.isNoneExpr(e) {
			return false
		}
	}
	return len(elems) > 0
}
