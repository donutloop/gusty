package lang

// negation.go is the unary minus's own road, and the one question it asks: *what kind is the operand?*
//
// The reference stops on a negation of anything that has no sign. `-"hi"`, `-None`, `-[1, 2]`, `-C()` each
// answer `TypeError: bad operand type for unary -: '<kind>'`, and every one of them was answered here by a
// *number at exit 0*: the interpreter handed the interned index of the text to its int evaluator (which is
// why the digits looked like an address, -281474976710658 = -(2^48 + 2)), and the compiled backend emitted
// `sub i32 0, @.str1` and printed 0. `-[1, 2]` went one step further and had `llc` reject the module — exit
// 2, the contract's "the compiler is broken" code, spent on a program the reference merely stops on.
//
// The cause is the one ADR 0166 names: the numeric road accepted an operand it has no meaning for, and
// every instruction it then writes is a guess. ADR 0265 already fixed this *for a slot read* — the object
// carries a tag there, so the raise can branch on it — and this file is the same rule for the positions the
// tag door never reached, where the kind is written in the source and the compiler only has to read it.
//
// One sentence, one owner: the wording comes from `unsupportedNumberOp("neg", …)`, the table ADR 0265
// established, so the interpreter, the emitted raise, the printer and the tests can never disagree about
// what `-x` says when x holds a text.
//
// Both halves raise rather than refuse. That is the load-bearing choice: a front-end refusal (exit 1) would
// be a *different* verdict from the reference's, would escape `except TypeError:`, and would make a program
// that catches the error run its `except` arm on one engine and its `try` arm on the other (ADR 0211's
// misclassing, ADR 0228's catchability).

import (
	"fmt"
	"strings"
)

// negationOperandIsLiterallyNotANumber is the fold-path half of the same question: an expression that
// spells its own kind, and spells it as something without a sign. The fold paths (`foldConstInt`,
// `constIntMemberVal`) answer a negation of one with a constant — `-None` became the int 0 — which is the
// silent wrong answer in its purest form: no raise, no tag, no instruction that could have disagreed. The
// folds ask this first and leave the shape un-folded, so the lowering that names the kind is what runs
// (roadmap Gap R.137, ADR 0266).
func negationOperandIsLiterallyNotANumber(e Expr) bool {
	switch e.(type) {
	case *NoneLit, *StrLit, *FString, *ListLit, *DictLit, *SetLit, *Tuple:
		return true
	}
	return false
}

// signlessOperandKind is ADR 0266's question — *what kind is this operand?* — asked for the other door that
// has no meaning for a value without a sign: `abs`. `abs("hi")` answered `hi` interpreted (the interned index
// rode on as the value) and `0` compiled, both at exit 0, and `abs([1])` had `llc` reject the module — exit 2,
// spent on a program the reference merely stops on (roadmap Gap R.140, ADR 0271).
//
// It is the same question and the same table, not a copy: one predicate names a value for the unary minus,
// for `abs`, for the printer and for the raise's wording, which is what keeps `-x` and `abs(x)` from
// disagreeing about what `x` holds. `negationOperandKind` is the delegation, kept under its own name because
// ADR 0266's tests ask it directly.
func (g *irGen) signlessOperandKind(e Expr) (string, bool) { return g.negationOperandKind(e) }

// absOperandKind is the door `abs` asks.
func (g *irGen) absOperandKind(e Expr) (string, bool) { return g.signlessOperandKind(e) }

// negationOperandKind names the kind CPython would put inside the quotes of its `bad operand type for
// unary -` sentence — for the half the compiler can answer without asking the run time. A literal has a
// spelling, and a name is described by the same records the print dispatch reads (`strVals`,
// `internedVars`, `noneVars`, the container maps), which the assignment walk keeps current statement by
// statement and clears when the name is rebound to something else.
//
// ok=false means the compiler cannot name the kind — a parameter that receives a text on one path and a
// number on another, a call whose return kind is unset, a slot of a container the program built rather than
// spelled. The caller then leaves the operand on the road it was already answered on: ADR 0265's tag door
// for a slot the object describes, the ordinary numeric road for a number. Refusing those here would spend
// exit 1 on programs that answer today, which is the expensive direction to be wrong (ADR 0166).
func (g *irGen) negationOperandKind(e Expr) (string, bool) {
	if g == nil || e == nil {
		return "", false
	}
	// `mod.NAME` for a data import spells its kind in the module rather than in the importing program, so
	// the question is asked of the literal the fold carries: `abs(consts.NAME)` had `llc` handed a subtract
	// of a text's interned index and answered `0`, where the reference stops with
	// `bad operand type for abs(): 'str'` (roadmap L11.6's typed stdlib constants, through ADR 0271's door).
	if lit, folded := g.foldedModuleAttr(e); folded {
		return g.negationOperandKind(lit)
	}
	// The printer's own question, asked once: is this value an index into @str_tab? A text is a text for
	// the print door and for the numeric road, or `print(x)` and `print(-x)` disagree about what x holds
	// — ADR 0229's rule ("the print path and the operation path ask this question of the same predicate")
	// applied one operator further out. It answers the str()/repr() calls, the text methods, a slice of a
	// text and a character read out of one, all of which are texts the compiler can name.
	if g.printsAsInternedStr(e) {
		return kindStr, true
	}
	switch n := e.(type) {
	case *StrLit, *FString:
		return kindStr, true
	case *NoneLit:
		return "NoneType", true
	case *ListLit:
		return "list", true
	case *DictLit:
		return "dict", true
	case *SetLit:
		return "set", true
	case *Tuple:
		return "tuple", true
	case *Name:
		// The container maps first: a name the module built as a container is a container whatever the
		// scalar records say, and a name that was once a text and is now a list is exactly the case the
		// assignment walk deletes out of strVals/internedVars.
		if _, isText := g.strVals[n.Value]; isText || g.internedVars[n.Value] {
			return kindStr, true
		}
		if g.noneVars[n.Value] {
			return "NoneType", true
		}
		if g.runtimeDicts[n.Value] {
			return "dict", true
		}
		if g.runtimeSets[n.Value] || g.mixedSets[n.Value] {
			return "set", true
		}
		if g.mixedDicts[n.Value] {
			return "dict", true
		}
		if g.listVars[n.Value] || g.mixedLists[n.Value] {
			return "list", true
		}
		if cls := g.varClasses[n.Value]; cls != "" {
			return cls, true
		}
		// A function's *name* and an imported module are values with no sign: `abs(f)`, `-f`,
		// `abs(math)`. CPython stops on each — `bad operand type for abs(): 'function'`,
		// `'module'` — and until this line the compiled leg reached `llc` with a load of a slot nothing
		// allocated (`%_f`, `%_math`) and left through exit 2, the contract's unforgivable code
		// (roadmap Gap R.150, Gap R.151, ADR 0283).
		if kind, isValue := g.nameIsAValueWithNoSign(n.Value); isValue {
			return kind, true
		}
	case *Attr:
		// `self.w = "hi"` in the class, `-c.w` outside it: the slot holds an @str_tab index, which is the
		// same value the printer renders as text (Gap R.42, ADR 0224).
		if cls := g.receiverClass(n.Obj); cls != "" && g.strAttrs[cls+"."+n.Name.Value] {
			return kindStr, true
		}
	case *Lambda:
		// A `lambda` in a numeric position is the family ADR 0271 deleted for containers, under a
		// different name: `print(-(lambda x: x))` reached `llc` as `%t4 = sub i32 0, lambda_0` — the
		// closure's function *global* written into an arithmetic operand — and exit 2 was spent on a
		// program the reference merely stops on. The name CPython puts in the quotes is `function`,
		// which is what the interpreter's `operandKind` already answers for a `closure`, `method` or
		// `function` heap object, so both engines quote one word (roadmap Gap R.151, ADR 0283).
		return "function", true
	case *Call:
		// A call the module knows answers text: `def f(): return "hi"` / `-f()`.
		if g.callReturnsStr(n) {
			return kindStr, true
		}
		if nm, isName := n.Fn.(*Name); isName {
			// A construction: CPython names the class, not a generic object, so `-Token()` says
			// `'Token'`. The name the class-pattern front end resolves to (ADR 0235) is the one the
			// reference prints, because an alias (`Alias = Token`) denotes the class it was bound to.
			if _, isClass := g.classInfos[nm.Value]; isClass {
				if g.classPat != nil {
					if cls, ok := g.classPat.classOf[nm.Value]; ok && cls != "" {
						return cls, true
					}
				}
				return nm.Value, true
			}
		}
	case *Index:
		// A slot of a container the literal still describes, and describes as holding no number at all:
		// `xs = ["a"]` / `-xs[i]`. The tag door cannot take this shape — every arm it can build answers a
		// double, and an int slot must stay an int — so the raise is written where the read is.
		if g.negationIndexIsText(n) {
			return kindStr, true
		}
		return g.negationSlotKind(n)
	}
	return "", false
}

// nameIsAValueWithNoSign names the two kinds a program can put in a numeric position that have no
// payload, no slot and no sign: a function the program defined, and an imported module. Both are read
// today as if they were variables — `load i32, i32* %_f` for a slot nothing ever allocated, and
// `use of undefined value '%_math'` from `llc-20` — which spends the exit-code contract's "the compiler
// is broken" code on a program the reference answers or stops on in one line (roadmap Gap R.150,
// Gap R.151, ADR 0283).
//
// The question is asked of the *declaration*, not of any value: the interpreter's `operandKind` names
// the same two words (`function` for a closure/method/function heap object, `module` for the module it
// binds), so `-f`, `abs(f)`, `abs(math)` and their siblings quote one word on both engines.
func (g *irGen) nameIsAValueWithNoSign(name string) (string, bool) {
	if name == "" || g == nil {
		return "", false
	}
	// A parameter that holds a callable is the program's own value, not the function's name: a body
	// that receives functions keeps whatever meaning it gave them.
	if _, isParam := g.params[name]; isParam {
		return "", false
	}
	if g.funcs[name] || (g.fds != nil && g.fds[name] != nil) {
		return "function", true
	}
	if g.imports != nil {
		if _, isModule := g.imports.Globals[name]; isModule {
			return "module", true
		}
		if _, isModule := g.imports.Funcs[name]; isModule {
			return "module", true
		}
	}
	return "", false
}

// otherOperand is the operand opposite the one being named, for the binary sentence CPython writes in
// source order: `f + 1` quotes 'function' and 'int', `1 + f` quotes them the other way round.
func otherOperand(n *BinOp, side Expr) Expr {
	if n.L == side {
		return n.R
	}
	return n.L
}

// valueWithNoSign is the binary road's form of nameIsAValueWithNoSign: it also accepts a `lambda`
// written inline, which is the shape Gap R.151 measured (`(lambda x: x) + 1`).
func (g *irGen) valueWithNoSign(e Expr) (string, bool) {
	if _, isLambda := e.(*Lambda); isLambda {
		return "function", true
	}
	if nm, isName := e.(*Name); isName {
		return g.nameIsAValueWithNoSign(nm.Value)
	}
	return "", false
}

// negationIndexIsText asks whether a subscript denotes a text — a character read out of one (`s[1]` of a
// text, ADR 0225) or an element of a text container (`xs[0][1]` where xs holds texts). The printer's own
// predicate answers the shapes it knows; the strVals record is added here because a name bound to a text
// literal is remembered there rather than in the interned-name set, and a subscript of one is a text the
// print door renders as a character while the numeric road would negate its index.
func (g *irGen) negationIndexIsText(ix *Index) bool {
	if g.printsAsInternedStr(ix) {
		return true
	}
	base := ix.Obj
	if g.printsAsInternedStr(base) || g.exprIsString(base) {
		return true
	}
	if _, isText := g.stringVal(base); isText {
		return true
	}
	if nm, isName := base.(*Name); isName {
		if _, isText := g.strVals[nm.Value]; isText {
			return true
		}
	}
	if bix, isIdx := base.(*Index); isIdx {
		return g.negationIndexIsText(bix)
	}
	return false
}

// negationSlotKind is the slot half of the question above, and it is deliberately narrower than the door's:
// the literal has to say that *nothing* the slot can report is a number (`"none"`), and the program must
// never store anything else into that container, because an `append` is the one thing that can make the
// literal's promise untrue at run time. Both halves are needed: a container that can also hold a number
// would be raised on for a slot CPython answers.
func (g *irGen) negationSlotKind(ix *Index) (string, bool) {
	nm, isName := ix.Obj.(*Name)
	if !isName || !g.containerNamed(nm.Value) {
		return "", false
	}
	if g.numChains != nil && g.numChains.ok[nm.Value] {
		return "", false
	}
	kinds, ok := g.negationSlotKinds(ix)
	if !ok {
		return "", false
	}
	for _, k := range kinds[1:] {
		if k.name != kinds[0].name {
			// Two kinds, and which one the slot reports is a run-time question only the tag answers. The
			// caller asks for the table; one name means one raise.
			return "", true
		}
	}
	return kinds[0].name, true
}

// negationSlotKinds is the two-name form: the caller emits one branch per kind the literal can report, and
// the tags decide which arm runs. ok=false when the shape is not the door's and not this file's either.
func (g *irGen) negationSlotKinds(ix *Index) ([]struct {
	tg   int32
	name string
}, bool) {
	nm, isName := ix.Obj.(*Name)
	if !isName || !g.containerNamed(nm.Value) {
		return nil, false
	}
	if g.numChains != nil && g.numChains.ok[nm.Value] {
		return nil, false
	}
	fam, ok := g.slotNumberFamily(ix.Obj)
	if !ok || fam != "none" {
		return nil, false
	}
	others, kindsOk := g.slotNonNumericKinds(ix.Obj)
	if !kindsOk || len(others) == 0 {
		return nil, false
	}
	return others, true
}

// emitBadSignless is the raise for a signless operation on a value that has no sign: the reference's own
// sentence, named by the operation so `abs` and the unary minus cannot borrow each other's wording
// (`bad operand type for abs(): 'str'` is not `bad operand type for unary -: 'str'`).
func (g *irGen) emitBadSignless(b *strings.Builder, op, kindName string, sp Span) string {
	class, msg := unsupportedNumberOp(op, kindName, "")
	g.raiseTo(b, exnCode(class), class, msg, sp)
	cont := g.newLabel(op + "raise")
	b.WriteString(cont + ":\n")
	return "0"
}

// emitBadAbs is the raise `abs` writes when its operand denotes something without a number in it.
func (g *irGen) emitBadAbs(b *strings.Builder, kindName string, sp Span) string {
	return g.emitBadSignless(b, "abs", kindName, sp)
}

// emitBadSignlessOfSlot is the same raise read off the object, one branch per kind the literal says the slot
// can report, so every path out of the read raises.
func (g *irGen) emitBadSignlessOfSlot(b *strings.Builder, ix *Index, op string, sp Span) (string, bool) {
	kinds, ok := g.negationSlotKinds(ix)
	if !ok {
		return "", false
	}
	_, tag, pairOk := g.runtimeSlotPair(b, ix)
	if !pairOk || tag == "" {
		return "", false
	}
	g.heapUsed = true
	for i, k := range kinds {
		class, msg := unsupportedNumberOp(op, k.name, "")
		if i == len(kinds)-1 {
			g.raiseTo(b, exnCode(class), class, msg, sp)
			break
		}
		cmp := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", cmp, tag, k.tg)
		g.markI1(cmp)
		g.branchRaise(b, cmp, class, msg, sp, op+"slot")
	}
	cont := g.newLabel(op + "raise")
	b.WriteString(cont + ":\n")
	return "0", true
}

// emitBadAbsOfSlot is `abs` of a slot the literal says holds no number.
func (g *irGen) emitBadAbsOfSlot(b *strings.Builder, ix *Index, sp Span) (string, bool) {
	return g.emitBadSignlessOfSlot(b, ix, "abs", sp)
}

// emitBadNegation writes the raise the reference raises for `-x` when x denotes something without a sign,
// and returns the value the rest of the (unreachable) expression is to use.
//
// The raise is the ordinary one — the same store-and-branch every raise in this language does, through
// `raiseTo`, so an open `except TypeError:` catches it and an uncaught one leaves through the traceback
// door at the runtime-error exit class (ADR 0228, ADR 0166). After it the block is terminated, so a fresh
// label is opened for whatever the caller still has to emit: the instructions survive, the path never runs.
func (g *irGen) emitBadNegation(b *strings.Builder, kindName string, sp Span) string {
	class, msg := unsupportedNumberOp("neg", kindName, "")
	g.raiseTo(b, exnCode(class), class, msg, sp)
	cont := g.newLabel("negraise")
	b.WriteString(cont + ":\n")
	return "0"
}

// emitBadNegationOfSlot is the same raise read off the object: one branch per kind the literal says the
// slot can report, the last as the unconditional else, so every path out of the read raises (the shape
// taggedDoubleFromSlot uses for the same reason — never a merge with a predecessor that stored nothing).
func (g *irGen) emitBadNegationOfSlot(b *strings.Builder, ix *Index, sp Span) (string, bool) {
	kinds, ok := g.negationSlotKinds(ix)
	if !ok {
		return "", false
	}
	_, tag, pairOk := g.runtimeSlotPair(b, ix)
	if !pairOk || tag == "" {
		return "", false
	}
	g.heapUsed = true
	for i, k := range kinds {
		class, msg := unsupportedNumberOp("neg", k.name, "")
		if i == len(kinds)-1 {
			// The last kind is the unconditional else, so no tag escapes the table.
			g.raiseTo(b, exnCode(class), class, msg, sp)
			break
		}
		cmp := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", cmp, tag, k.tg)
		g.markI1(cmp)
		g.branchRaise(b, cmp, class, msg, sp, "negslot")
	}
	cont := g.newLabel("negraise")
	b.WriteString(cont + ":\n")
	return "0", true
}
