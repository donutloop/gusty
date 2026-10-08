package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// ValueForm is the question a value is asked before it is written down: "show me what you
// say" (str) or "show me how you are written" (repr). The two answers differ for exactly one
// kind of value — a text, which writes its characters under str and its quoted source form
// under repr — and are the same answer for every other kind (roadmap L11.2, ADR 0258).
//
// The pair exists as one type rather than two functions because the bug it closes (Gap L.2)
// was caused by the two halves being written twice: print had a printer per backend and
// str() had a number-formatter beside it, so every form the second one had not been told
// about came back as the number underneath the value. A form is added to one table here, or
// it is added to neither.
type ValueForm int

const (
	// FormStr is the value's own text: what print writes and str() returns.
	FormStr ValueForm = iota
	// FormRepr is the value's written form: what repr() returns, and what a container
	// writes for each of its elements.
	FormRepr
)

// QuotesText reports whether a top-level text writes its quoted form. Container elements are
// rendered by reprNested / the `%quote` flag on the runtime printers and do not consult this:
// a text inside a container is quoted under both halves of the pair, which is why `print` of
// a list and `str` of that list agree.
func (f ValueForm) QuotesText() bool { return f == FormRepr }

// renderPair lowers str(x) and repr(x) in the compiled backend onto the printers the module
// already has, by pointing those printers at a capture buffer instead of at stdout
// (rt_str_of_value / rt_str_of_container, heapRuntimeIR). handled=false means "this form is
// not named here" — the caller then either reaches its number path or, for repr, asks
// renderPairRefusal to say which half is missing.
//
// The order of the questions below is the order the forms are distinguished, and it is the
// order the pair table test walks. A container is asked first because a container is the form
// the number formatter used to swallow: its slots answer for themselves (@estr flags, per-slot
// tags), which no static guess can improve on (ADR 0232's rule, applied to the pair).
// pairForEcho is the pair door's own question, asked by the prompt: the (payload, tag) words a slot
// read or a slot-bound name lives in, when the value's kind is something only the tag can say. The
// echo asks this before the form table so the ANNOUNCED kind comes from the tag rather than from what
// the compiler could guess — `xs = [True, 1]` / `xs[0]` is a bool, and a prompt that says `object` is
// reporting its own blind spot instead of the program's value (roadmap L13.1, ADR 0259's rule for what
// a slot is, applied to the prompt).
func (g *irGen) pairForEcho(b *strings.Builder, e Expr) (payload, tag string, ok bool) {
	if nm, isName := e.(*Name); isName {
		if g.numericPairVar(nm.Value) {
			p, t := g.numericPairRegs(b, nm.Value)
			return p, t, true
		}
		if p, t, okTag := g.taggedLoopVarRead(b, nm.Value); okTag {
			return p, t, true
		}
	}
	return g.runtimeSlotPairDeep(b, e)
}

func (g *irGen) renderPair(b *strings.Builder, e Expr, form ValueForm, sp Span) (string, bool, error) {
	quote := 0
	if form.QuotesText() {
		quote = 1
	}

	// The answer of a fold the run time chose is a pair before it is a number: `min` and `max` hand back
	// one of the values they were given, so which form this renders is the winner's fact and not the
	// source's. It is asked of the same door print and the binder ask, which is the only reason
	// `str(min(n, 3))`, `print(min(n, 3))` and `f"{min(n, 3)}"` can be relied on to agree (ADR 0303's
	// one printer, ADR 0316; roadmap L11.1, Gap R.146).
	if p, t, okFold, ferr := g.foldPairOf(b, e); ferr != nil {
		return "", true, ferr
	} else if okFold {
		return g.rtStrCall(b, "rt_str_of_value", "i32 "+p, "i32 "+t, "i32 "+strconv.Itoa(quote)), true, nil
	}

	// A name the arithmetic door bound is a number whose family the objects chose, and it is asked
	// first — ahead of the container question, whose probe lowers the name as a number and would
	// refuse before this arm could speak. It is rendered by the printer every other form goes
	// through: the (payload, tag) pair is what that printer reads, so `str(n)`, `repr(n)` and
	// `print(n)` cannot disagree about `14` versus `14.0` the way the two number formatters used to
	// disagree about a container (ADR 0258's rule, one statement later; roadmap L11.1, Gap R.143).
	if nm, isName := e.(*Name); isName && g.numericPairVar(nm.Value) {
		p, t := g.numericPairRegs(b, nm.Value)
		return g.rtStrCall(b, "rt_str_of_value", "i32 "+p, "i32 "+t, "i32 "+strconv.Itoa(quote)), true, nil
	}

	// A name whose kind the OBJECT chose — the loop variable of a container that mixes kinds, or a name
	// bound from a slot read — carries its tag in its own alloca, and that pair is the whole answer.
	// It goes to the module's one tag-reading printer rather than to a guess, which is the rule this
	// file exists to enforce: `str(x)`, `repr(x)` and `print(x)` over the same value agree because all
	// three ask the same door (ADR 0258, extended to the slot-bound name this road used to refuse;
	// roadmap L11.1, Gap R.171).
	if nm, isName := e.(*Name); isName {
		if p, t, ok := g.taggedLoopVarRead(b, nm.Value); ok {
			return g.rtStrCall(b, "rt_str_of_value", "i32 "+p, "i32 "+t, "i32 "+strconv.Itoa(quote)), true, nil
		}
	}

	// A slot read out of a container the program built: the (payload, tag) pair the tagged-read door
	// already produces for `print`, the comparisons and `len` is exactly what the printer reads, so
	// `print(xs[i])` and `str(xs[i])` are the same question asked twice rather than two rules that can
	// drift. A slot holding a container is answered by that container's own object, which is why the
	// tag has to travel with the payload here as well.
	if p, t, ok := g.runtimeSlotPairDeep(b, e); ok {
		return g.rtStrCall(b, "rt_str_of_value", "i32 "+p, "i32 "+t, "i32 "+strconv.Itoa(quote)), true, nil
	}

	// A container asks its own object to render itself.
	h, isContainer, cerr := g.renderPairContainer(b, e)
	if isContainer {
		if cerr != nil {
			return "", true, cerr
		}
		g.heapUsed = true
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_of_container(i32 %s, i32 %d)\n", t, h, quote))
		return t, true, nil
	}

	// The void writes its one name, and it is the same name under both halves. The argument
	// is still evaluated: a None that arrived from a call must have run.
	if g.isNoneExpr(e) {
		if _, isLit := e.(*NoneLit); !isLit {
			if _, err := g.value(b, e); err != nil {
				return "", true, err
			}
		}
		return g.internStr(b, "None"), true, nil
	}

	// A verdict writes the name the expression gave it, which is a question about the AST
	// rather than the slot (ADR 0257); repr of a verdict is the same word Python's str gives.
	if g.printsAsBool(e) {
		v, err := g.value(b, e)
		if err != nil {
			return "", true, err
		}
		tt := g.internStr(b, "True")
		ff := g.internStr(b, "False")
		cmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", cmp, v))
		sel := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", sel, cmp, tt, ff))
		return sel, true, nil
	}

	// A text is the one form the two halves disagree on: str hands the value straight back,
	// repr runs the same capture with the quoting flag on so the answer is interned text that
	// nests and case-folds like any other runtime-built string.
	if g.exprIsString(e) {
		v, err := g.value(b, e)
		if err != nil {
			return "", true, err
		}
		if quote == 0 {
			return v, true, nil
		}
		return g.rtStrCall(b, "rt_str_of_value", "i32 "+v, "i32 4", "i32 1"), true, nil
	}

	// A float takes the round-tripping text print already uses (rt_fmt_double), so the pair
	// and print cannot disagree about how many digits a float has.
	if g.isFloat(e) {
		if fv, ok := g.floatEval(e); ok {
			return g.internStr(b, pyFloatRepr(fv)), true, nil
		}
		fv := g.floatValue(b, e)
		if fv == "" {
			return "", true, g.floatOperandRefusal(e)
		}
		g.floatFmtUsed = true
		fs := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
		return g.rtStrCall(b, "rt_str_intern", "i8* "+fs), true, nil
	}

	// A number writes its digits — the one form every value besides a text agrees on between
	// the two halves. A number the compiler can read is interned from the digits it already
	// knows; one it cannot read has them written at run time by rt_str_of_int, the same call
	// str() has always made. Answering that form for repr too is what keeps `n = 42` followed
	// by repr(n) from refusing while str(n) answers: the pair cannot disagree about digits
	// any more than it can disagree about a list (roadmap L11.2, ADR 0258).
	if n, ok := g.foldConstInt(e); ok {
		return g.internStr(b, strconv.FormatInt(n, 10)), true, nil
	}
	// ... but only for a number the compiler can SEE is one. This road used to take anything that
	// was neither a text nor a float, which made `def f(v): print(str(v))` called with `None` intern
	// the digits of the None handle and print `0` at exit 0, and left a name whose kind the object
	// carries one guess away from the same. `str(None)` written literally answers `None` through the
	// arms above; the parameter form now falls through to the renderer's own refusal, because a
	// missing rendering must not become the number underneath the value (roadmap L11.2, Gap R.171,
	// ADR 0258's rule for a container — extended to the void and to any name of unknown kind).
	if g.strArgIsNumberish(e) && !g.exprIsString(e) && !g.isFloat(e) && !strings.Contains(exprTyName(e), "float") {
		v, err := g.value(b, e)
		if err != nil {
			return "", true, err
		}
		return g.rtStrCall(b, "rt_str_of_int", "i32 "+v), true, nil
	}

	return "", false, nil
}

// renderPairContainer answers whether the expression is a container, and hands back the heap
// handle its object lives at. The three kinds are told apart by the runtime, not here:
// rt_str_of_container reads the object's own kind record, so one entry point renders list,
// dict and set and this scope only has to say "this operand names a container".
func (g *irGen) renderPairContainer(b *strings.Builder, e Expr) (string, bool, error) {
	if isContainerLiteral(e) {
		// A list that mixes kinds, or whose elements are other containers, is built with
		// per-element tags — the same build print chooses, so the pair and print construct
		// the same object and cannot render two different things from one source line
		// (ADR 0184's rule, applied to str()/repr() by roadmap L11.2, ADR 0258). Without it
		// a nested list is a pair of heap handles printed as numbers: str([[1, 2], [3]])
		// answered [1, 2], which is the handles, not the contents.
		if lit, ok := e.(*ListLit); ok && (literalMixedKinds(e) || g.literalNeedsTags(e)) && g.taggableMixedList(lit) {
			g.heapUsed = true
			h, err := g.heapListFromTagged(b, lit)
			return h, true, err
		}
		if (literalMixedKinds(e) || g.literalNeedsTags(e)) && !g.literalMixedIsTaggable(e) {
			noun := "list"
			switch e.(type) {
			case *SetLit:
				noun = "set"
			case *DictLit:
				noun = "dict"
			}
			return "", true, mixedKindErr(noun)
		}
		g.heapUsed = true
		switch lit := e.(type) {
		case *ListLit:
			h, err := g.heapListFrom(b, lit, "")
			return h, true, err
		case *SetLit:
			h, err := g.heapSetFrom(b, lit, "")
			return h, true, err
		case *DictLit:
			h, err := g.heapDictFrom(b, lit, "")
			return h, true, err
		}
	}
	if nm, ok := e.(*Name); ok {
		if g.listVars[nm.Value] || g.mixedLists[nm.Value] || g.taggedVars[nm.Value] ||
			g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value] ||
			g.runtimeSets[nm.Value] || g.mixedSets[nm.Value] {
			v, err := g.value(b, e)
			return v, true, err
		}
	}
	// The checker's inferred type is a second witness that an operand names a container, for
	// the shapes no builder registered: `str(set())` asks a constructor, whose handle nothing
	// in this scope had recorded, and answering it with the digits of the heap index is
	// exactly the failure Gap L.2 was (roadmap L11.2, ADR 0258).
	if kind := containerKindFromTy(exprTyName(e)); kind != "" {
		v, err := g.value(b, e)
		return v, true, err
	}
	v, err := g.value(b, e)
	if err != nil {
		return "", false, err
	}
	if g.listOperands[v] {
		return v, true, nil
	}
	return "", false, nil
}

// renderPairRefusal is the refusal the pair is allowed to make, and it names the half that is
// missing rather than answering with the number underneath the value. Reaching it means the
// expression's form is not visible from here: the compiled backend stores values as untagged
// words, so a form can only be rendered when the expression says which one it is.
func (g *irGen) renderPairRefusal(e Expr, form ValueForm) error {
	half := "str"
	if form.QuotesText() {
		half = "repr"
	}
	return fmt.Errorf("codegen: %s of %s is not implemented: the compiled backend renders a value by the form the expression names, and this one names none — a number, a float, a verdict, None, a text or a container all say which printer they want, and this would have to guess one. Guessing is how str([1, 2]) came to answer 0 (roadmap L11.2, ADR 0258)", half, g.exprSummary(e))
}

// formOfName picks the half of the pair a call spells.
func formOfName(name string) ValueForm {
	if name == "repr" {
		return FormRepr
	}
	return FormStr
}
