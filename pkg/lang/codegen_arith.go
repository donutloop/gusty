// Arithmetic and comparison: BinOp's roads (numeric, container, mixed-tagged), the dunder door, the chained
// comparison, and the trap sentence each pair of operands gets.
package lang

import (
	"fmt"
	"strings"
)

// emitDunderBinOp emits an AOT lowering of operator overloading for a binary
// operator. When a statically-known operand is a class instance that defines a
// dunder (or reflected) method for the operator, it emits a direct call to that
// method and returns the result register. It returns ("", false) when no
// overloading applies, so the caller falls back to builtin arithmetic.
func (g *irGen) emitDunderBinOp(b *strings.Builder, n *BinOp) (string, bool) {
	dunder := dunderForBinOp(n.Op)
	reflected := reflectedDunder(dunder)
	if dunder == "" {
		return "", false
	}
	// The reference dispatches the left operand's dunder first, then a
	// reflected method on the right operand (the rule the retired interpreter reached
	// by asking the value; ADR 0302).
	clsL := g.receiverClass(n.L)
	if clsL != "" {
		if fn, ok := g.resolveMethod(clsL, dunder); ok {
			ret := g.newTmp()
			lv, err := g.value(b, n.L)
			if err != nil {
				return "", false
			}
			rv, err := g.value(b, n.R)
			if err != nil {
				return "", false
			}
			fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s, i32 %s)\n", ret, fn, lv, rv)
			return ret, true
		}
	}
	clsR := g.receiverClass(n.R)
	if clsR != "" {
		if fn, ok := g.resolveMethod(clsR, reflected); ok {
			ret := g.newTmp()
			rv, err := g.value(b, n.R)
			if err != nil {
				return "", false
			}
			lv, err := g.value(b, n.L)
			if err != nil {
				return "", false
			}
			fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s, i32 %s)\n", ret, fn, rv, lv)
			return ret, true
		}
	}
	return "", false
}

// chainValue lowers Python's comparison chain (`a < b < c`, `1 <= x < 10`) into the compiled module.
// Each operand is STORED ONCE into a fresh local slot and every link reads the slots, which is what
// makes "the middle operand is evaluated once" a property of the emitted IR rather than a hope about
// road order (roadmap L12.1 / Gap R.53, ADR 0288). The links are then combined with `and`, and that is
// safe here for a reason the rejected parser-level desugaring did not have: by the time the `and` runs,
// every operand has already been evaluated into its slot, so there is no side effect left for
// short-circuiting to skip. Rewriting the source expressions instead — `a < b and b < c` over the
// ORIGINAL nodes — is what would have evaluated a call in the middle of a chain twice.
//
// Before this the grammar's left-associativity made the chain `(a < b) < c`, an int compared against a
// boolean, which this backend answers rather than refusing: `print(1 > 2 < 3)` printed True where
// CPython prints False and `print(1 < 2 > 1)` printed False where CPython prints True.
func (g *irGen) chainValue(b *strings.Builder, n *ChainCompare) (string, error) {
	if len(n.Operands) != len(n.Ops)+1 || len(n.Ops) == 0 {
		return "", fmt.Errorf("codegen: malformed comparison chain")
	}
	words := make([]string, len(n.Operands))
	operandName := make([]string, len(n.Operands))
	for i, o := range n.Operands {
		if chainOperandIsCheap(o) {
			// A literal or a plain name reads a value and does nothing else, so it may be lowered
			// at each use; giving it a slot would add a store, and a store is a chance to be
			// reordered relative to the statement the chain sits in.
			w, err := g.value(b, o)
			if err != nil {
				return "", err
			}
			words[i] = w
			continue
		}
		// A non-cheap operand is bound to an ordinary LOCAL NAME and read back as one, rather than to a
		// bare alloca handed to the comparison roads. The roads ask a name about the container records
		// beside it (`dictVals`, `listVars`, the tagged-slot tables); a synthetic alloca with no name
		// behind it answered `index of a non-literal variable` for `0 < xs[1] < 3`, which the baseline
		// printed `True` for — a regression my own first version shipped (roadmap L12.1 / Gap R.53,
		// ADR 0288). Going through a name keeps every one of those records reachable.
		g.ldN++
		// A CONTAINER operand cannot be slotted at all in this backend: a list/set/dict literal lowers
		// to the address of a compile-time global (`@.lst1`) and an alloca'd local is an `i32` slot, so
		// the store becomes `store i32 @.lst1, i32* %_chain1` — the shape llc rejects with "global
		// variable reference must have pointer type", which ADR 0234 already named a compiler bug for an
		// ordinary program. Refusing in words is the honest answer; the interpreter chains over
		// containers fine, and L11.1's tagged value word is what lets the compiled leg do the same
		// (roadmap L12.1 / Gap R.53, ADR 0288).
		if g.chainOperandIsContainer(o) {
			return "", fmt.Errorf("codegen: a container operand of a comparison chain would store a container global into an i32 slot; the reference chains over containers and the compiled leg waits for the tagged value word (roadmap L11.1, Gap R.53, ADR 0166)")
		}
		slot := fmt.Sprintf("chain%d", g.ldN)
		if !g.allocd[slot] {
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", slot))
			g.gcReg(b, slot)
			g.allocd[slot] = true
		}
		w, err := g.value(b, o)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", w, slot))
		ld := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ld, slot))
		words[i] = ld
		// The name this slot answers for, so a link's operand is a plain name read — and the records
		// the comparison roads consult for a NAME are copied across, because `0 < xs[1] < 3` asks the
		// middle operand the same questions `0 < xs[1]` does and a slot with no records behind it
		// answered `index of a non-literal variable` where the baseline answered True (a regression my
		// first version shipped; roadmap L12.1 / Gap R.53, ADR 0288).
		g.carryOperandRecords(o, slot)
		operandName[i] = slot
	}
	// Each link is lowered through the ordinary comparison road, so a chain gets the int, text,
	// float, container and tagged-slot answers of an ordinary comparison rather than a second set
	// that can disagree with the first.
	// Each link is lowered through the ordinary comparison road, so a chain gets the int, text,
	// float, container and tagged-slot answers of an ordinary comparison rather than a second set
	// that can disagree with the first.
	//
	// The links are built over PLACEHOLDER nodes carrying the already-computed word, NOT over the
	// original operand expressions. Lowering `n.Operands[i]` again would re-run it: my first version
	// did exactly that and `print(1 < g() < 10)` printed the call's output THREE times at exit 0 —
	// the precise bug L12.1 exists to prevent, re-imported by the code that was written to fix it
	// (roadmap L12.1 / Gap R.53, ADR 0288). The comparison roads only ever ask a placeholder for its
	// word, which the slot above has already produced.
	linkExprs := make([]Expr, len(n.Ops))
	for i, op := range n.Ops {
		// A middle operand that was slotted is read as the NAME of its slot, so the comparison road
		// asks it the same questions it asks any local; a cheap operand keeps its original node.
		lh, rh := linkOperand(n.Operands[i], words[i], operandName[i], n.Src), linkOperand(n.Operands[i+1], words[i+1], operandName[i+1], n.Src)

		linkExprs[i] = &BinOp{Op: op, L: lh, R: rh, Src: n.Src}
	}
	var acc string
	for i := range linkExprs {
		lw, err := g.value(b, linkExprs[i])
		if err != nil {
			return "", err
		}
		if acc == "" {
			acc = lw
			continue
		}
		// `and` of two verdicts, both of whose operands are already computed: the conjunction
		// cannot skip work the reference would have done.
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = and i32 %s, %s\n", t, acc, lw))
		acc = t
	}
	return acc, nil
}

// carryOperandRecords copies the per-name facts codegen's comparison roads look up from an operand to
// the slot that now stands for it. Only the records the comparison road actually consults, and only
// when the operand is a name: the point is that reading the slot asks exactly the questions reading the
// original operand asked, so a chain cannot lose an answer the single comparison had.
func (g *irGen) carryOperandRecords(from Expr, to string) {
	nm, isName := from.(*Name)
	if !isName || nm.Value == to {
		return
	}
	if g.listVars[nm.Value] {
		g.listVars[to] = true
	}
	if g.mixedLists[nm.Value] {
		g.mixedLists[to] = true
	}
	if g.runtimeDicts[nm.Value] {
		g.runtimeDicts[to] = true
	}
	if g.mixedDicts[nm.Value] {
		g.mixedDicts[to] = true
	}
	if g.runtimeSets[nm.Value] {
		g.runtimeSets[to] = true
	}
	if g.mixedSets[nm.Value] {
		g.mixedSets[to] = true
	}
	if g.tupleVars[nm.Value] {
		// `u = t` binds the same value, so it binds the same promise: the alias prints with parentheses
		// too (roadmap L11.3, ADR 0361).
		g.tupleVars[to] = true
	}
	if g.strVals != nil {
		if v, ok := g.strVals[nm.Value]; ok {
			g.strVals[to] = v
		}
	}
	if g.dictVals != nil {
		if dv, ok := g.dictVals[nm.Value]; ok {
			g.dictVals[to] = dv
		}
	}
	// The literal-backed container records are the ones the Index road's fold reads
	// (`staticElemExpr` walks a name through containerLits/staticLists/staticDicts/staticSets), and
	// without them `0 < xs[1] < 3` refused with `index of a non-literal variable` where the baseline
	// answered True — a regression my own first version shipped (Gap R.53, ADR 0288).
	if lit, ok := g.containerLits[nm.Value]; ok {
		g.containerLits[to] = lit
	}
	if lit, ok := g.staticLists[nm.Value]; ok {
		g.staticLists[to] = lit
	}
	if lit, ok := g.staticDicts[nm.Value]; ok {
		g.staticDicts[to] = lit
	}
	if lit, ok := g.staticSets[nm.Value]; ok {
		g.staticSets[to] = lit
	}
	if g.taggedVars[nm.Value] {
		g.taggedVars[to] = true
		if g.taggedOrigin != nil {
			g.taggedOrigin[to] = g.taggedOrigin[nm.Value]
		}
	}
	if g.boolVars[nm.Value] {
		g.boolVars[to] = true
	}
	if g.noneVars[nm.Value] {
		g.noneVars[to] = true
	}
}

// orFallback keeps a diagnostic from naming an empty type.
func orFallback(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// otherOperandOfNumeric is otherOperand for an operand already known to be one side of the BinOp.
func otherOperandOfNumeric(n *BinOp, e Expr) Expr {
	if n == nil {
		return e
	}
	if n.L == e {
		return n.R
	}
	return n.L
}

// isCompareOpForContainer is the ordering operators, spelled for the guard above.
func isCompareOpForContainer(op string) bool {
	switch op {
	case "<", "<=", ">", ">=":
		return true
	}
	return false
}

// containerKindOf names a container for the raise/answer split: a literal by its node, and a NAME by
// asking the caller-supplied reader. A comparison over two variables is an ordinary program the reference
// orders happily, so a predicate that could only read literals would raise where the reference answers.
func containerKindOf(e Expr, ofName func(string) string) string {
	if k := containerShapeOf(e); k != "" {
		return k
	}
	nm, ok := e.(*Name)
	if !ok || ofName == nil {
		return ""
	}
	return ofName(nm.Value)
}

// arithOperandIsContainer asks the numeric road's question: would this operand lower to a CONTAINER
// rather than to a number? Only an operand whose kind this pass can SEE is claimed — a parameter's kind
// belongs to whoever calls, and a subscript's to the slot roads beside this one, which already answer or
// refuse with a message naming the slot. Claiming either turned a working answer into a refusal
// (`def half(xs): return xs[0] / 2` with `half([1.5])` = 0.75) and a precise diagnostic into a vague one.
func (g *irGen) arithOperandIsContainer(e Expr) bool {
	switch e.(type) {
	case *ListLit, *DictLit, *SetLit, *Tuple:
		return true
	}
	nm, ok := e.(*Name)
	if !ok {
		return false
	}
	if g.builtinShadowed(nm.Value) || g.paramNameOf(nm) {
		return false
	}
	switch nm.Value {
	case "list", "dict", "set", "tuple":
		// spelled builtins, not a container the program bound
		return false
	}
	return g.nameHoldsContainer(nm.Value)
}

// containerShapeOf names a container literal's family, so `+` can ask whether the two sides are the SAME
// kind: the reference concatenates list+list and tuple+tuple and refuses list+dict.
// isContainerRepeatOp is the `*` test the raise's wording needs.
func isContainerRepeatOp(op string) bool { return op == "*" }

// multiplierKindNameOf asks the generator when it is in reach and falls back to the node alone.
func multiplierKindNameOf(g *irGen, e Expr) string {
	if g == nil {
		if _, ok := e.(*FloatLit); ok {
			return "float"
		}
		if _, ok := e.(*StrLit); ok {
			return "str"
		}
		return "int"
	}
	return g.multiplierKindName(e)
}

// multiplierIsPlainInt says whether the non-container side of a `*` is an int literal (or a name the
// records call an int), which is the ONLY multiplier the reference lets a sequence or mapping see without
// raising. Written as its own predicate because the answer separates a refusal (`[0] * 3`, which the
// reference answers and this road cannot build) from a raise (`[1] * 2.0`, which the reference raises for)
// and getting it backwards turns a working `[0] * 3` into a TypeError.
func multiplierIsPlainInt(l, r Expr) bool {
	other := l
	if containerShapeOf(l) != "" {
		other = r
	}
	switch v := other.(type) {
	case *IntLit:
		return true
	case *UnOp:
		// `-3` is still an int multiplier: `[0] * -3` is the empty list in the reference.
		return (v.Op == "-" || v.Op == "+") && multiplierIsPlainInt(v.X, v.X)
	}
	return false
}

// multiplierIsNotAnInt is the reference's test, spelled for `*`: the sequence's own __mul__ complains
// "non-int" for anything that is not an int — a float, a text, a container, None. `other` is the NAME the
// raise quotes, so a name the road could not resolve arrives as "int" and stays on the generic road.
func multiplierIsNotAnInt(other string) bool {
	switch other {
	case "int", "":
		return false
	}
	return true
}
func containerShapeOf(e Expr) string {
	switch e.(type) {
	case *ListLit:
		return "list"
	case *DictLit:
		return "dict"
	case *SetLit:
		return "set"
	case *Tuple:
		return "tuple"
	}
	return ""
}
func exprIsContainerShape(e Expr) bool {
	return containerShapeOf(e) != ""
}

// containerRaisesForPair is the reference's own judgement, asked once: does CPython stop with a TypeError
// for THIS operator on THIS pair? Two nested gates inverted this and turned `{1: 2} * 2`'s TypeError into a
// sentence about our own missing helper, which puts our words where the reference has words of its own.
func (g *irGen) containerRaisesForPair(op string, l, r Expr) bool {
	ofName := func(n string) string { return g.nameContainerKind(n) }
	switch op {
	case "-", "/", "//", "%", "**":
		// No container meaning whatever the other side is: `[1] - [2]` and `[1] / 2` both raise.
		return true
	case "*":
		// Only the SEQUENCES repeat: `[0] * 3` answers `[0, 0, 0]` (refused here, not answered), while
		// `{1: 2} * 2` and `{1} * 2` raise `unsupported operand type(s) for *: 'dict' and 'int'`.
		lk, rk := containerKindOf(l, ofName), containerKindOf(r, ofName)
		if lk == rk && lk != "" {
			// Two containers on both sides is not the reference's repeat at all: a sequence times a
			// sequence is `can't multiply sequence by non-int of type 'list'`, which this road raises.
			return lk == "list" || lk == "tuple" || lk == "dict" || lk == "set"
		}
		// Exactly one side is a container. Whether the reference RAISES depends on what the OTHER side
		// is, and `[0] * 3` — which the reference answers — must not be swept into the raise: an int
		// multiplier is the sequence repeat (refused, no helper), anything else is a raise.
		if multiplierIsPlainInt(l, r) {
			k := lk
			if k == "" {
				k = rk
			}
			return k == "dict" || k == "set"
		}
		// The multiplier is a float, a text, a container, or something whose kind this pass cannot pin
		// — the reference raises for every family here (`can't multiply sequence by non-int of type
		// 'float'`, and the mapping types have no __mul__ at all).
		return true
	// [multiplier kind recorded for the raise's sentence]
	case "+":
		// A container with a non-container, or two different container kinds, is `can only
		// concatenate`; the same kind concatenates and the reference answers.
		lk, rk := containerKindOf(l, ofName), containerKindOf(r, ofName)
		if lk == "" || rk == "" {
			return true
		}
		return lk != rk
	case "<", ">", "<=", ">=":
		// Only a MIXED pair raises: container against number, or `[] < {}` across kinds. Two
		// same-kind containers ORDER, and the tagged door above answers that for names
		// (`a = [1]` / `b = [2]` / `print(a < b)` is True, as it was before this row).
		lk, rk := containerKindOf(l, ofName), containerKindOf(r, ofName)
		if lk == "" && rk == "" {
			// Neither side is a container at all: not this road's question.
			return false
		}
		if lk == "" || rk == "" {
			// Exactly one side is a container — the MIXED pair the reference stops with
			// `'<' not supported between instances of 'list' and 'int'`.
			return true
		}
		return lk != rk
	}
	// NOT a comparison the tagged order road can answer. `a = [1]` / `b = [2]` / `print(a < b)` is an
	// ordinary program this backend ordered before the row existed, and its own door is asked three
	// lines above this guard; claiming everything from here would turn that answer into a raise. Only a
	// MIXED pair — container against number, or across kinds — is CPython's TypeError.
	return false
}

// containerNumberOp spells CPython's own complaint. Which sentence it is, is a fact about the OPERATOR:
// `+` reads `can only concatenate list (not "dict") to list`, the comparisons read `'<' not supported
// between instances of 'list' and 'dict'`, and everything else reads `unsupported operand type(s) for
// -: 'list' and 'list'` (ADR 0215's rule that the reference's wording is the wording).
func containerNumberOp(op, other, leftKind, rightKind string) (string, string) {
	if op == "*" && (leftKind == "list" || leftKind == "tuple") && multiplierIsNotAnInt(other) {
		// A sequence DOES define __mul__, so it refuses a non-int multiplier with its own sentence
		// rather than the generic one — measured for a float (`can't multiply sequence by non-int of
		// type 'float'`), for a dict, and for a text. The type it quotes is the MULTIPLIER's, which is
		// why `other` is asked rather than inferred from the container's own kind.
		return "TypeError", fmt.Sprintf("can't multiply sequence by non-int of type '%s'", other)
	}
	if op == "*" && (leftKind == "dict" || leftKind == "set") && rightKind != "" && multiplierIsNotAnInt(rightKind) {
		// `{} * []` says `'dict'`, not `'list'`: the reference names the type whose __mul__ was tried
		// and failed, which for a mapping-times-something pair is the MAPPING on the left. Measured,
		// because the obvious reading (name the other operand) gets this row backwards.
		return "TypeError", fmt.Sprintf("can't multiply sequence by non-int of type '%s'", leftKind)
	}
	if op == "+" && leftKind == "list" {
		// The reference spells `+` two ways, and which one it uses is a fact about the LEFT operand's
		// type, not the operator alone: a list says `can only concatenate list (not "dict") to list`
		// because list defines __add__ and refuses the other type, while a dict or set does not define
		// concatenation at all and says the generic `unsupported operand type(s) for +: 'dict' and
		// 'list'`. Measured, not remembered (ADR 0215).
		self := leftKind
		if self == "" {
			self = other
		}
		return "TypeError", fmt.Sprintf("can only concatenate %s (not %q) to %s", self, other, self)
	}
	a, b := leftKind, rightKind
	if a == "" {
		a = other
	}
	if b == "" {
		b = other
	}
	switch op {
	case "<", ">", "<=", ">=":
		return "TypeError", fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, a, b)
	}
	return "TypeError", fmt.Sprintf("unsupported operand type(s) for %s: '%s' and '%s'", op, a, b)
}

// containerKindName spells a container the way the reference names its type in a TypeError.
// multiplierKindName names the multiplier's type for a raise's sentence: a container family, or the
// primitive the reference would name (`float`, `str`). `containerKindName` alone answers "" for `2.0`,
// and the fallback `'int'` would put the wrong type in the message.
func (g *irGen) multiplierKindName(e Expr) string {
	switch e.(type) {
	case *FloatLit:
		return "float"
	case *StrLit:
		return "str"
	case *BoolLit:
		return "bool"
	case *NoneLit:
		return "NoneType"
	}
	if k := g.containerKindName(e); k != "" {
		return k
	}
	if nm, ok := e.(*Name); ok && !g.builtinShadowed(nm.Value) {
		if g.floatVars[nm.Value] {
			return "float"
		}
		if g.exprIsString(nm) {
			return "str"
		}
	}
	return "int"
}
func (g *irGen) containerKindName(e Expr) string {
	if k := containerShapeOf(e); k != "" {
		return k
	}
	nm, ok := e.(*Name)
	if !ok {
		return ""
	}
	if _, is := g.staticDicts[nm.Value]; is {
		return "dict"
	}
	if g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value] {
		return "dict"
	}
	if _, is := g.staticSets[nm.Value]; is {
		return "set"
	}
	if g.runtimeSets[nm.Value] || g.mixedSets[nm.Value] {
		return "set"
	}
	return "list"
}

// nameContainerKind names what a NAME holds, from the same records the iteration, subscript and length
// roads read, or "" when this pass cannot say. It is the reader the raise/answer split asks about a
// variable, so `a = [1]` / `b = [2]` / `print(a < b)` is not mistaken for a pair that raises.
func (g *irGen) nameContainerKind(nm string) string {
	if g.builtinShadowed(nm) || g.paramNameByName(nm) {
		return ""
	}
	switch nm {
	case "list", "dict", "set", "tuple":
		return ""
	}
	if !g.nameHoldsContainer(nm) {
		return ""
	}
	if g.runtimeDicts[nm] || g.mixedDicts[nm] {
		return "dict"
	}
	if _, is := g.staticDicts[nm]; is {
		return "dict"
	}
	if g.runtimeSets[nm] || g.mixedSets[nm] {
		return "set"
	}
	if _, is := g.staticSets[nm]; is {
		return "set"
	}
	return "list"
}

// nameHoldsContainer reports whether the program bound a name to a list, dict or set — the same
// records the iteration, subscript and length roads consult, asked here so a diagnostic cannot call a
// dict a string.
func (g *irGen) nameHoldsContainer(nm string) bool {
	if g.runtimeDicts[nm] || g.mixedDicts[nm] || g.runtimeSets[nm] || g.mixedSets[nm] ||
		g.listVars[nm] || g.mixedLists[nm] {
		return true
	}
	if _, ok := g.containerLits[nm]; ok {
		return true
	}
	if _, ok := g.staticLists[nm]; ok {
		return true
	}
	if _, ok := g.staticDicts[nm]; ok {
		return true
	}
	if _, ok := g.staticSets[nm]; ok {
		return true
	}
	return false
}

// chainOperandIsContainer recognises the operand shapes whose compiled value is a container GLOBAL
// rather than a word: the literals themselves, and a name the program bound to one of those kinds.
func (g *irGen) chainOperandIsContainer(e Expr) bool {
	switch e.(type) {
	case *ListLit, *DictLit, *SetLit, *Tuple:
		return true
	}
	nm, ok := e.(*Name)
	if !ok {
		return false
	}
	if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] ||
		g.mixedLists[nm.Value] || g.mixedDicts[nm.Value] || g.mixedSets[nm.Value] {
		return true
	}
	if _, has := g.containerLits[nm.Value]; has {
		return true
	}
	if _, has := g.staticLists[nm.Value]; has {
		return true
	}
	if _, has := g.staticDicts[nm.Value]; has {
		return true
	}
	if _, has := g.staticSets[nm.Value]; has {
		return true
	}
	return false
}

// linkOperand picks the expression a chain link reads: the ORIGINAL node when the operand was cheap
// (a literal or a name — re-lowering it is free and keeps every record the comparison roads consult),
// and the slot's NAME when the operand had to be evaluated once into a slot. Returning the original node
// in the second case is what re-ran a call for every link that read it.
func linkOperand(orig Expr, word, name string, src Span) Expr {
	if name != "" {
		return &Name{Value: name, Src: src}
	}
	return orig
}

// mixedTaggedCompare answers `a == b` (and `!=`) when either side is a value whose kind lives in
// the object rather than in the compiler's notebook. Two shapes reach it:
//
//   - a variable that carries a runtime tag — a loop variable over a mixed container, or one
//     bound from a mixed read (ADR 0185, ADR 0187); and
//   - a *slot read*: an element of a container whose slots describe themselves, or a slot reached
//     through an index the program computes (roadmap L11.1, Gap R.79). Until now `print(out[1])`
//     asked the slot and rendered `a`, while `out[1] == "a"` refused — the tag was carried to the
//     printer and dropped at the comparison.
//
// Both sides become (payload, tag) pairs and `rt_payload_eq` answers, which is the one equality
// that knows what a payload means: within a tag by payload, across the numeric tags numerically,
// container slots by content. The other side must still be an expression whose kind the compiler
// can prove; when it cannot, this declines and the ordinary path reports whatever it reports
// (ADR 0232).
func (g *irGen) mixedTaggedCompare(b *strings.Builder, n *BinOp) (string, bool, error) {
	if n.Op != "==" && n.Op != "!=" {
		return "", false, nil
	}
	if !g.isTaggedCompareSide(n.L) && !g.isTaggedCompareSide(n.R) {
		return "", false, nil
	}
	// The read emits its own bounds check, whose IndexError branch terminates a block. A door that
	// changes its mind halfway through would leave the caller's block with instructions after a
	// terminator, and llc would report the compiler's mistake as the program's (ADR 0166). So the
	// whole door is built in a scratch buffer and committed only once both sides answer.
	var scratch strings.Builder
	lv, lt, lok := g.comparePair(&scratch, n.L)
	rv, rt, rok := g.comparePair(&scratch, n.R)
	if !lok || !rok {
		// One side's kind lives in the object, and the other side's cannot be proven at all. The
		// untagged compare that used to answer this compared two bare words, so `xs[0] == f()` came
		// out true whenever f handed back the text whose interned index happened to be the number in
		// the slot — an answer by coincidence (ADR 0232's collision, at a new site). Refuse by naming
		// the missing half instead. When *both* sides are reads, decline and let the ordinary path
		// report whatever it reports.
		if g.isTaggedCompareSide(n.L) && !rok && !g.isTaggedCompareSide(n.R) {
			return "", true, g.taggedCompareOperandErr(n.R, n.L)
		}
		if g.isTaggedCompareSide(n.R) && !lok && !g.isTaggedCompareSide(n.L) {
			return "", true, g.taggedCompareOperandErr(n.L, n.R)
		}
		return "", false, nil
	}
	cmp := g.newTmp()
	fmt.Fprintf(&scratch, "  %s = call i32 @rt_payload_eq(i32 %s, i32 %s, i32 %s, i32 %s)\n", cmp, lv, lt, rv, rt)
	res := g.newTmp()
	fmt.Fprintf(&scratch, "  %s = icmp ne i32 %s, 0\n", res, cmp)
	g.markI1(res)
	if n.Op == "!=" {
		inv := g.newTmp()
		fmt.Fprintf(&scratch, "  %s = xor i1 %s, true\n", inv, res)
		g.markI1(inv)
		res = inv
	}
	g.heapUsed = true
	// rt_payload_eq unboxes a float slot, so the float block has to be in the module even for a
	// program whose own literals are all integers.
	g.floatFmtUsed = true
	b.WriteString(scratch.String())
	return g.asBoolI32(b, res), true, nil
}

// mixedMembership lowers `x in s` / `k in d` where the container's slots carry tags. It answers
// (handled, error): handled is false when the container is uniform (or not a named container at
// all), and the untagged rt_contains path stays in charge — that path is sound for a container
// whose one kind the compiler has proved, and only for one (ADR 0232).
func (g *irGen) mixedMembership(b *strings.Builder, n *BinOp) (string, bool, error) {
	nm, ok := n.R.(*Name)
	if !ok || !g.allocd[nm.Value] {
		return "", false, nil // not a container variable the codegen built a slot for
	}
	// Which container, and which lookup agrees with its slot layout. A uniform container takes
	// the tagged call too whenever the needle's kind is provable: `1 in ["1"]` is false, and a
	// payload-only scan answers it true because "1" interned to the index 1 (ADR 0232).
	fn := ""
	switch {
	case g.mixedSets[nm.Value] || g.runtimeSets[nm.Value]:
		fn = "rt_set_contains_tagged"
	case g.mixedDicts[nm.Value] || g.runtimeDicts[nm.Value]:
		fn = "rt_dict_has_tagged"
	case g.mixedLists[nm.Value] || g.listVars[nm.Value]:
		fn = "rt_contains_tagged"
	default:
		return "", false, nil
	}
	mixed := g.mixedSets[nm.Value] || g.mixedDicts[nm.Value] || g.mixedLists[nm.Value]
	needle, tag, tok := g.taggedOperand(b, n.L)
	if !tok {
		if !mixed {
			return "", false, nil // the untagged scan keeps the answer it always gave
		}
		return "", false, fmt.Errorf("codegen: testing %s against the container %q whose slots describe themselves needs a needle whose kind the compiler can prove; a bool, float or container needle needs the tagged value word (roadmap L11.1, ADR 0232)", n.L, nm.Value)
	}
	h := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", h, "_"+nm.Value))
	hit := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s, i32 %s, i32 %s)\n", hit, fn, h, needle, tag))
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", cmp, hit))
	g.markI1(cmp)
	if n.Op == "not in" {
		inv := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", inv, cmp))
		g.markI1(inv)
		cmp = inv
	}
	return g.asBoolI32(b, cmp), true, nil
}
