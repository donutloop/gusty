// sequence_ops.go is the door a sequence operation passes through on its way to the module: the `+`
// between two lists (or two tuples), the `*` between a sequence and a whole number, and the `*` between
// a text and a whole number. It is the codegen half of roadmap Gap R.33; the runtime half is
// `pkg/lang/codegen_ir_sequence.go`.
//
// Why a door and not three cases in `valueBinOp`. The refusal this row retires was written when a
// container lowered to the address of a compile-time global — `mul i32 @.lst1, 3` is the module `llc`
// rejects, and exit 2 is the contract's compiler-bug class (ADR 0166) — and it was kept after the heap
// moved, so the sentence stayed true to its original reason long after that reason left. The three
// operations are one shape: an operand whose kind must be *proven* a sequence, a builder that copies
// slots with their tags (ADR 0187), and an object that owns its own attributes (ADR 0251). Asking them
// in one place is what keeps the answers to the three questions each road asks — print, the binding,
// the membership test, the loop, the comparison — from disagreeing, which is the rule ADR 0232 states
// and every row since has enforced.
//
// What the door will not do is answer a kind it cannot prove. A sequence operand is claimed only where
// `staticContainerKind`/`isTupleExpr` can name it; a multiplier is claimed only where the kind
// classifier in `list_index_kind.go` does not accuse it of being a float, a text, a container or
// `None`. A multiplier whose kind is only a run-time fact travels the road the program wrote — see
// `seqMultiplierTravels` for what that costs and which row owns it.
package lang

import (
	"fmt"
	"strings"
)

// seqOpKind is the operation the door chose.
type seqOpKind int

const (
	seqNoOp seqOpKind = iota
	seqConcatOp
	seqRepeatOp
	seqTextRepeatOp
)

// seqOp is the door's decision: which operation, over which operands, in which spelling, with what the
// compiler could read of the answer's size.
type seqOp struct {
	kind seqOpKind
	// sequence is the operand that is the sequence: both operands for a concatenation, the repeated
	// one for a repeat. multiplier is the other side of a repeat (nil for a concatenation).
	sequence Expr
	other    Expr
	// tuple says the answer is the immutable spelling — the object's own attribute word carries it, and
	// the door is the only place the parentheses of `(1, 2) + (3,)` are knowable (L11.3, ADR 0361).
	tuple bool
	// count is the answer's element count (a text repeat: its CODE POINTS — here, its bytes, which is
	// what rt_str_repeat copies) when the compiler read it without running anything; okCount says it
	// could. It is the guard against building a sequence past the one object's element row.
	count    int64
	okCount  bool
	isRepeat bool // the count that needs the row guard is a repeat's, not a concatenation's
}

// seqElementRow is how many slots one container object owns: `@heap` is
// `[HEAPSLOTS@ x {i32, i32, [256 x i32]}]`, so 256 is where one object's element row ends. A repeat
// the compiler can see would pass the row has no honest answer until the row that owns walking past it
// lands — Gap R.271, which `xs.append` in a loop already reaches.
const seqElementRow = 256

// seqPlanOf is the one question: is this BinOp a sequence operation this backend builds? It emits
// nothing, so every road that has to decide before it lowers asks it (ADR 0232's rule that the roads
// that build and the roads that read ask one predicate, or they disagree and one is quietly wrong).
func (g *irGen) seqPlanOf(n *BinOp) (seqOp, bool) {
	if n == nil {
		return seqOp{}, false
	}
	switch n.Op {
	case "+":
		lk, lok := g.seqSideIs(n.L)
		rk, rok := g.seqSideIs(n.R)
		if !lok || !rok || lk != rk {
			return seqOp{}, false
		}
		op := seqOp{kind: seqConcatOp, sequence: n.L, other: n.R, tuple: lk == "tuple"}
		lc, lcOK := g.seqElementCount(n.L)
		rc, rcOK := g.seqElementCount(n.R)
		if lcOK && rcOK {
			op.count, op.okCount, op.isRepeat = lc+rc, true, false
		}
		return op, true
	case "*":
		// A text repeated by a whole number is a text, and the number may be on either side —
		// `"ab" * 2` and `2 * "ab"` are one program to the reference.
		if g.seqSideIsText(n.L) && !g.seqSideIsContainer(n.R) {
			if _, bad := g.nonIntIndexTypeName(n.R); bad {
				return seqOp{}, false
			}
			return g.seqTextPlan(n.L, n.R), true
		}
		if g.seqSideIsText(n.R) && !g.seqSideIsContainer(n.L) {
			if _, bad := g.nonIntIndexTypeName(n.L); bad {
				return seqOp{}, false
			}
			return g.seqTextPlan(n.R, n.L), true
		}
		lk, lok := g.seqSideIs(n.L)
		rk, rok := g.seqSideIs(n.R)
		if lok && rok {
			// A sequence times a sequence is not a repeat at all: the reference raises, and the
			// raise is the other door's (`containerRaisesForPair`), not this one's answer.
			return seqOp{}, false
		}
		var base, mult Expr
		var kind string
		switch {
		case lok:
			base, mult, kind = n.L, n.R, lk
		case rok:
			base, mult, kind = n.R, n.L, rk
		default:
			return seqOp{}, false
		}
		// A multiplier the kind classifier accuses — a float, a text, a container, `None` — is not this
		// door's answer: the reference raises, and `seqRaises` words it. Claiming the shape here would
		// lower a double into the integer domain and hand `rt_seq_repeat` a float box handle as a count.
		if _, bad := g.nonIntIndexTypeName(mult); bad {
			return seqOp{}, false
		}
		op := seqOp{kind: seqRepeatOp, sequence: base, other: mult, tuple: kind == "tuple", isRepeat: true}
		if bc, bok := g.seqElementCount(base); bok {
			if mc, mok := g.foldConstInt(mult); mok {
				if mc < 0 {
					mc = 0
				}
				op.count, op.okCount = bc*mc, true
			}
		}
		return op, true
	}
	return seqOp{}, false
}

// seqTextPlan is the text half of `*` with the count the row guard asks about.
func (g *irGen) seqTextPlan(text, mult Expr) seqOp {
	op := seqOp{kind: seqTextRepeatOp, sequence: text, other: mult}
	if tc, tok := g.textByteCount(text); tok {
		if mc, mok := g.foldConstInt(mult); mok {
			if mc < 0 {
				mc = 0
			}
			op.count, op.okCount = tc*mc, true
		}
	}
	return op
}

// seqSideIs names the sequence family of one operand: "list" or "tuple", or nothing when the operand is
// not a sequence this pass can prove. A dict and a set answer "" — the reference concatenates and
// repeats neither, and their TypeError is the raise door's, spelled by `containerNumberOp`.
func (g *irGen) seqSideIs(e Expr) (string, bool) {
	if e == nil || g.seqSideIsText(e) {
		return "", false
	}
	if g.isTupleExpr(e) {
		return "tuple", true
	}
	kind, ok := g.staticContainerKind(e)
	if !ok || kind != containerKindList {
		return "", false
	}
	// A name the last binding filled with a tuple is the immutable container wearing the list layout;
	// asking that question here rather than at the printer is what makes `t + (3,)` a tuple without a
	// second reader for the attribute word (ADR 0361).
	if g.isTupleExpr(e) {
		return "tuple", true
	}
	return "list", true
}

// seqSideIsText is the text side of `*`: a literal, an interpolation, a formatting, or a name the
// printer renders out of @str_tab. It is the same question the print road and the ordering road ask
// (`textOrderSide`), because a value that prints as text is a value that repeats as text.
func (g *irGen) seqSideIsText(e Expr) bool {
	if e == nil {
		return false
	}
	if _, ok := g.stringVal(e); ok {
		return true
	}
	return g.exprIsString(e) || g.printsAsInternedStr(e)
}

// seqSideIsContainer is the `*` guard beside the text test: `"ab" * [2]` is not a repeat, it is the
// reference's `can't multiply sequence by non-int of type 'list'`, and the raise door words it.
func (g *irGen) seqSideIsContainer(e Expr) bool {
	if e == nil {
		return false
	}
	if _, ok := g.staticContainerKind(e); ok {
		return true
	}
	return g.isTupleExpr(e)
}

// seqElementCount reads an operand's length without running anything: a literal knows its own element
// count and a name whose last binding was a literal knows that one.
func (g *irGen) seqElementCount(e Expr) (int64, bool) {
	switch n := e.(type) {
	case *ListLit:
		return int64(len(n.Elems)), true
	case *Tuple:
		return int64(len(n.Elems)), true
	case *Name:
		if lit, ok := g.containerLits[n.Value]; ok {
			return g.seqElementCount(lit)
		}
	}
	return 0, false
}

// textByteCount reads a text operand's length in the bytes rt_str_repeat will copy. The row guard is
// the only reader: a text has no element row to overflow, so an answer above the row is allowed for a
// text and refused for a container.
func (g *irGen) textByteCount(e Expr) (int64, bool) {
	if s, ok := g.stringVal(e); ok {
		return int64(len(s)), true
	}
	if s, ok := stringConst(e, g.builtinShadowed); ok {
		return int64(len(s)), true
	}
	return 0, false
}

// seqRaises is the reference's own judgement, asked of the door before it declines: is this pair one the
// reference stops with a TypeError? A text times a float and a list times a text both are, and the
// sentence is the reference's rather than this backend's (`can't multiply sequence by non-int of type
// 'float'`), because ADR 0214 made the reference's wording the wording and ADR 0215 made the raise the
// answer that beats a refusal.
//
// ok is false when the door has nothing to say — an operand whose kind is a run-time fact keeps the road
// it always had (list_index_kind.go's rule, kept whole).
func (g *irGen) seqRaises(n *BinOp) (class, msg string, ok bool) {
	if n == nil {
		return "", "", false
	}
	switch n.Op {
	case "*":
		text := false
		switch {
		case g.seqSideIsText(n.L) && !g.seqSideIsContainer(n.R):
			text = true
			if t, bad := g.nonIntIndexTypeName(n.R); bad {
				return "TypeError", seqNonIntMultiplierMessage(t), true
			}
		case g.seqSideIsText(n.R) && !g.seqSideIsContainer(n.L):
			text = true
			if t, bad := g.nonIntIndexTypeName(n.L); bad {
				return "TypeError", seqNonIntMultiplierMessage(t), true
			}
		}
		if text {
			return "", "", false
		}
		// A sequence times something that is not a whole number: the sequence has a __mul__, so the
		// reference names the MULTIPLIER's type, whatever the sequence's own kind is.
		var mult Expr
		_, lok := g.seqSideIs(n.L)
		_, rok := g.seqSideIs(n.R)
		switch {
		case lok && !rok && !g.seqSideIsText(n.R):
			mult = n.R
		case rok && !lok && !g.seqSideIsText(n.L):
			mult = n.L
		default:
			return "", "", false
		}
		if t, bad := g.nonIntIndexTypeName(mult); bad {
			return "TypeError", seqNonIntMultiplierMessage(t), true
		}
		return "", "", false
	}
	// `+` between two operands the door cannot both claim is the raise door's question, and it is
	// already answered there: `containerRaisesForPair` asks which pair the reference stops on and
	// `containerNumberOp` words the two sentences (`can only concatenate list (not "dict") to list`,
	// `unsupported operand type(s) for +: 'dict' and 'dict'`). This door claims answers, never somebody
	// else's raise (ADR 0273) — EXCEPT for the pair the old road only ever refused, because the refusal it
	// refused with claimed the backend builds no list-concatenate helper at all, and that sentence is no
	// longer true of a compiler that has one. `[1] + (1, 2)` is CPython's
	// `can only concatenate list (not "tuple") to list`, `{1: 1} + {1: 1}` is its
	// `unsupported operand type(s) for +: 'dict' and 'dict'`, and both are raises the reference answers
	// with, not refusals (roadmap Gap R.33, ADR 0365; ADR 0215's rule that a raise beats a refusal).
	if n.Op == "+" {
		lk, rk := g.seqConcatKind(n.L), g.seqConcatKind(n.R)
		if lk == "" || rk == "" {
			return "", "", false
		}
		if lk == rk && (lk == "list" || lk == "tuple") {
			return "", "", false // the plan claims this pair and builds it
		}
		if (lk == "list" || lk == "tuple") && lk != rk {
			return "TypeError", fmt.Sprintf("can only concatenate %s (not %q) to %s", lk, rk, lk), true
		}
		// A text has `__add__` and refuses everything that is not a text, naming the OTHER side — the same
		// sentence shape as a list's, and the reason the old road's `unsupported operand type(s)` for a
		// text left side was wrong (Gap R.38's rule that a message must be true about the program).
		if lk == "str" {
			if rk == "str" {
				return "", "", false // the fold road already answers two texts
			}
			return "TypeError", fmt.Sprintf("can only concatenate str (not %q) to str", rk), true
		}
		// A dict and a set have no `__add__` at all: the reference names both sides.
		return "TypeError", fmt.Sprintf("unsupported operand type(s) for +: '%s' and '%s'", lk, rk), true
	}
	return "", "", false
}

// seqConcatKind names what one `+` operand is, in the words the reference's own TypeError uses: list,
// tuple, dict, set, str, or "" for anything this pass cannot prove. A number answers "" so the numeric
// road keeps its own `unsupported operand type(s) for +: 'int' and 'str'`.
func (g *irGen) seqConcatKind(e Expr) string {
	if e == nil {
		return ""
	}
	if g.seqSideIsText(e) {
		return "str"
	}
	if g.isTupleExpr(e) {
		return "tuple"
	}
	if kind, ok := g.staticContainerKind(e); ok {
		switch kind {
		case containerKindList:
			return "list"
		case containerKindDict:
			return "dict"
		case containerKindSet:
			return "set"
		}
		return ""
	}
	if _, isBin := e.(*BinOp); isBin {
		// `[1] + [2] + [3]`: the left side of a chain is itself a sequence the door builds.
		if _, applies := g.seqBuildsContainer(e); applies {
			return "list"
		}
	}
	return ""
}

// seqBuiltOrderingRefusal is the door's honest no about an ORDERING, asked by BOTH roads that read a
// comparison: the value door (`print([1, 2] < [2])`) and the condition door (`if [1, 2] < [2]:`, `while`,
// a ternary test, an `assert`). It emits nothing and claims nothing — it only says which pairs the numeric
// road would corrupt.
//
// The condition road needs it as much as the value road does, and for the same reason: an `if` condition
// is lowered by `codegen_truth.go`, not by `valueBinOp`, so a refusal written only in the value door left
// the condition to fall through to the generic comparison arm — which rendered both operands and emitted
// `%t1 = icmp slt i32 @.lst1, @.lst2`, the exact invalid IR this row's own bug was, reported by `llc` at
// the compiler-bug exit code (ADR 0166's class, Gap R.175's rule that an emitted module must verify).
// One predicate, two roads, is ADR 0232's rule restated: a question the roads answer separately gets
// answered separately, and one of them is quietly wrong.
//
// The guard is the ladder: `a = [1]` / `b = [2]` / `if a < b:` printed True long before this door existed,
// and the tagged order door reads those names. Claiming a pair the door already answers would turn an
// answer into a refusal, which `TestTheArithmeticGuardLeavesTheOrderRoadItsQuestion` exists to catch
// (Gap R.175's lesson, ADR 0365).
func (g *irGen) seqBuiltOrderingRefusal(n *BinOp) (string, bool) {
	if n == nil {
		return "", false
	}
	switch n.Op {
	case "<", "<=", ">", ">=":
	default:
		return "", false
	}
	lk, rk := g.seqConcatKind(n.L), g.seqConcatKind(n.R)
	if (lk == "list" || lk == "tuple") && lk == rk && g.seqBuiltInPlace(n.L) && g.seqBuiltInPlace(n.R) {
		return fmt.Sprintf("`%s` has no compiled lowering: the order of two %ss the compiler built in place is the tagged order door's question and it reads a name the program bound, not a handle this door assembled — CPython answers this program, and the arm that hands it the built object is owed (roadmap Gap R.314, ADR 0365, beside Gap R.175 and L11.1)", exprSurface(n), lk), true
	}
	return "", false
}

// seqShapeIsText is the PRE-SCAN's version of `seqIsTextExpr`: the passes that register "this function
// hands back a text" and "this parameter holds a text" run before any per-generator record exists, so they
// read only the shape the source wrote — a `*` with a text literal or an interpolation on one side is a
// repeat whatever the multiplier turns out to be. It is `percentFormatShapeIsText`'s rule, one operator
// over, and for the same reason: without the arm the census never saw the text cross the boundary and the
// print leg printed the @str_tab POSITION under it (roadmap Gap R.33, ADR 0365; ADR 0360 wrote the pattern).
func seqShapeIsText(e Expr) bool {
	n, ok := e.(*BinOp)
	if !ok || n.Op != "*" {
		return false
	}
	lText, rText := isStringExpr(n.L), isStringExpr(n.R)
	if !lText && !rText {
		return false
	}
	// A container on the other side is not a repeat at all — `"ab" * [2]` is the reference's TypeError —
	// and two texts is `"a" * "b"`, which also raises. Neither answers a value, so neither is claimed here:
	// the raise road owns them, and a shape claim that routed a raising pair through the text printer would
	// be a wrong answer wearing the right words.
	if lText && containerShapeOf(n.R) != "" {
		return false
	}
	if rText && containerShapeOf(n.L) != "" {
		return false
	}
	return !(isStringExpr(n.L) && isStringExpr(n.R))
}

// seqBuiltInPlace is the half of an ordering pair this door assembled: a container literal it can see, or a
// build another `+`/`*` of the same door produced. A NAME the program bound is not built in place — the
// tagged order door reads names and answers them, and claiming one is how an answer becomes a refusal
// (Gap R.175's lesson). Neither is a SLOT READ (`xs[0]`): an element's kind is a run-time fact the element
// door owns, and its refusal — "more than one kind" — is a truer sentence than this one (Gap R.86).
func (g *irGen) seqBuiltInPlace(e Expr) bool {
	if containerShapeOf(e) != "" {
		return true
	}
	_, builds := g.seqBuildsContainer(e)
	return builds
}

// seqOwnsBinOp is the question the FLOAT road asks before it claims a `*` or a `+`: does the sequence
// door own this expression — as an answer it builds, or as the TypeError the reference raises for a
// multiplier that is not a whole number? A sequence operation never answers a double, so leaving these
// to the double lift is how `"ab" * 2.5` was refused for want of a digit in a text where CPython quotes
// the multiplier's type (roadmap Gap R.33, ADR 0365; ADR 0360's `%` is the same shape one operator over).
func (g *irGen) seqOwnsBinOp(n *BinOp) bool {
	if n == nil {
		return false
	}
	switch n.Op {
	case "+", "*":
	default:
		return false
	}
	if _, applies := g.seqPlanOf(n); applies {
		return true
	}
	_, _, raises := g.seqRaises(n)
	return raises
}

// seqNonIntMultiplierMessage is the sequence's own complaint: a sequence has a __mul__ and refuses a
// multiplier that is not a whole number by naming the multiplier's type (measured, because the obvious
// reading — name the sequence — gets the row backwards; see containerNumberOp's note).
func seqNonIntMultiplierMessage(typeName string) string {
	return "can't multiply sequence by non-int of type '" + typeName + "'"
}

// seqMultiplierTravels reports that the door is taking a repeat whose multiplier it could not read as a
// constant. It is a counter, not a permission: the row's IR assertion counts these so the shape the
// compiler cannot kind stays visible (a multiplier that is a float at run time only would answer the
// repeat rather than raise, and that residue is Gap R.314's).
func (g *irGen) seqMultiplierTravels() { g.seqRuntimeMultipliers++ }

// emitSeqOp builds the answer: the operands through the heap door, the helper call beside them, and the
// handle or interned index that comes back. The caller that has a NAME to bind asks recordSeqBinding to
// write what the roads will look up for it.
func (g *irGen) emitSeqOp(b *strings.Builder, op seqOp, sp Span) (string, error) {
	g.heapUsed = true
	g.seqUsed = true
	switch op.kind {
	case seqConcatOp:
		lh, err := g.containerOperand(b, op.sequence)
		if err != nil {
			return "", err
		}
		rh, err := g.containerOperand(b, op.other)
		if err != nil {
			return "", err
		}
		res := g.newTmp()
		g.emitHeapCall(b, res, fmt.Sprintf("call i32 @rt_seq_concat(i32 %s, i32 %s, i32 %d)", lh, rh, op.estrBits()), sp)
		return res, nil
	case seqRepeatOp:
		h, err := g.containerOperand(b, op.sequence)
		if err != nil {
			return "", err
		}
		n, err := g.seqMultiplier(b, op)
		if err != nil {
			return "", err
		}
		res := g.newTmp()
		g.emitHeapCall(b, res, fmt.Sprintf("call i32 @rt_seq_repeat(i32 %s, i32 %s, i32 %d)", h, n, op.estrBits()), sp)
		return res, nil
	case seqTextRepeatOp:
		s, err := g.value(b, op.sequence)
		if err != nil {
			return "", err
		}
		n, err := g.seqMultiplier(b, op)
		if err != nil {
			return "", err
		}
		// The text door is rt_str_cat's door: one interned index, built through the same call shape,
		// so every text road takes the answer without knowing what built it (ADR 0229).
		return g.rtStrCall(b, "rt_str_repeat", "i32 "+s, "i32 "+n), nil
	}
	return "", fmt.Errorf("codegen: %s is not a sequence operation the door can build (roadmap Gap R.33)", exprSnippet(op.sequence))
}

// estrBits is the attribute word the new object is born with: self-describing slots (ADR 0187's bit,
// because a copied slot is the pair whatever the operand was built as) and, for the immutable spelling,
// the tuple bit the door read off the source (ADR 0361). The heap block never sees either constant.
func (op seqOp) estrBits() int {
	bits := 8
	if op.tuple {
		bits |= estrBitTuple
	}
	return bits
}

// seqMultiplier lowers the `*`'s other side to the i32 the helper counts with: a constant the compiler
// read is folded into the call, anything else travels through the ordinary value road.
func (g *irGen) seqMultiplier(b *strings.Builder, op seqOp) (string, error) {
	if c, ok := g.foldConstInt(op.other); ok {
		if c < 0 {
			c = 0
		}
		return fmt.Sprintf("%d", c), nil
	}
	g.seqMultiplierTravels()
	return g.value(b, op.other)
}

// seqRefusal is the door's honest no: a repeat the compiler can see would build past the element row one
// container object owns. The sentence names the row that owns the walk rather than answering a corrupted
// container, because a fabricated container outranks any refusal (Gap R.271).
func (op seqOp) refusal(surface string) error {
	if op.okCount && op.isRepeat && op.count > seqElementRow {
		return fmt.Errorf("`%s` would build a sequence of %d slots, past the %d slots one container object owns; this backend neither walks past the row nor truncates the answer — CPython answers this program, and the row that owns walking past it is roadmap Gap R.271 (ADR 0365)", surface, op.count, seqElementRow)
	}
	return nil
}

// recordSeqBinding writes what the roads look up for a name bound to a sequence the door built: the kind
// claims through the one door that owns them (ADR 0341), the self-describing bit the copied tags justify
// (ADR 0232), and the immutable spelling (ADR 0361).
//
// `mixedLists` is claimed from the operands rather than guessed: the copy is born self-describing in the
// runtime, so the printer must ask each slot, and the one thing that must not happen is a reader sent to
// a tag array nobody wrote. Here every slot was written — rt_seq_concat copies payload and tag in the same
// breath (ADR 0187) — so the claim is true whatever the two operands were.
func (g *irGen) recordSeqBinding(name string, op seqOp) {
	g.settleContainerKind(name, containerKindList)
	g.mixedLists[name] = true
	delete(g.containerLits, name)
	delete(g.noneVars, name)
	delete(g.boolVars, name)
	if g.strVals != nil {
		delete(g.strVals, name)
	}
	if g.dictVals != nil {
		delete(g.dictVals, name)
	}
	if op.tuple {
		g.recordTupleClaim(name)
	} else if g.tupleVars != nil {
		delete(g.tupleVars, name)
	}
}

// seqValue is the road an expression takes when the door can build it: the decision, the guard, the
// emission. It answers false for every expression that is not a sequence operation, leaving the caller's
// road exactly as it was (ADR 0273's rule: an extra answer, never a substitute for one).
func (g *irGen) seqValue(b *strings.Builder, e Expr) (string, bool, error) {
	n, ok := e.(*BinOp)
	if !ok {
		return "", false, nil
	}
	op, ok := g.seqPlanOf(n)
	if !ok {
		return "", false, nil
	}
	if err := op.refusal(exprSurface(n)); err != nil {
		return "", true, err
	}
	h, err := g.emitSeqOp(b, op, n.Span())
	if err != nil {
		return "", true, err
	}
	return h, true, nil
}

// seqOperationRoad is the door as `valueBinOp` asks it. The two questions it can answer about a pair the
// door will not build are the reference's raise (a multiplier that is not a whole number, which the
// classifier can name) and silence (a pair whose kinds this pass cannot prove, which keeps the road and
// the refusal it has always had).
func (g *irGen) seqOperationRoad(b *strings.Builder, n *BinOp) (string, bool, error) {
	op, applies := g.seqPlanOf(n)
	if !applies {
		if class, msg, raises := g.seqRaises(n); raises {
			g.heapUsed = true
			g.raiseTo(b, exnCode(class), class, msg, n.Span())
			return "0", true, nil
		}
		// An ORDERING of two containers the compiler built in place is refused **here**, before either side
		// is lowered, because the lowering is what made this shape a compiler bug rather than a refusal:
		// the numeric road put the two container globals in an `icmp slt` and `llc` rejected the module
		// (Gap R.175's exit-2 class, reopened by the door's own placement and closed again by this line;
		// the ordering itself is roadmap Gap R.314, ADR 0365's rule that a refusal is reserved for a fact).
		if msg, refuses := g.seqBuiltOrderingRefusal(n); refuses {
			return "", true, fmt.Errorf("%s", msg)
		}
		return "", false, nil
	}
	if err := op.refusal(exprSurface(n)); err != nil {
		return "", true, err
	}
	h, err := g.emitSeqOp(b, op, n.Span())
	if err != nil {
		return "", true, err
	}
	return h, true, nil
}

// seqHandleOf is `containerOperand`'s and `containerHandleOf`'s arm: the handle of the object the door
// builds, asked by the roads that read a container — the printer, the membership test, the loop, the
// comparison, the length — so one program written two ways takes one road (Gap R.33, ADR 0365).
func (g *irGen) seqHandleOf(b *strings.Builder, e Expr) (string, bool, error) {
	if _, builds := g.seqBuildsContainer(e); !builds {
		// A text repeat builds NO object: its answer is an @str_tab index, and handing that to a container
		// renderer printed an empty line for `str("ab" * 2)` — the index of a text is not a heap handle, and
		// the container printer asked of one writes nothing (Gap R.42's family, ADR 0224's slot-is-the-truth,
		// roadmap Gap R.33, ADR 0365).
		return "", false, nil
	}
	h, ok, err := g.seqValue(b, e)
	if !ok || err != nil {
		return "", false, err
	}
	return h, true, nil
}

// membershipOfBuiltSequence is `2 in [1] + [3]`: the haystack is an object the door builds, so its handle
// comes out of the door and the object's own slots answer the question. The needle arrives as a
// (payload, tag) pair for the same reason the container's elements do — membership is the payload-and-tag
// question, one level out (ADR 0241's rule for a read haystack, applied to a built one; roadmap Gap R.33,
// ADR 0365). A TEXT haystack is left where it was: the substring test already has its road.
func (g *irGen) membershipOfBuiltSequence(b *strings.Builder, n *BinOp) (string, bool, error) {
	if n.Op != "in" && n.Op != "not in" {
		return "", false, nil
	}
	if _, builds := g.seqBuildsContainer(n.R); !builds {
		return "", false, nil
	}
	h, applies, err := g.seqHandleOf(b, n.R)
	if !applies || err != nil {
		return "", false, err
	}
	needle, needleTag, okPair := g.taggedOperand(b, n.L)
	if !okPair {
		// The haystack has already been built by this point, which costs the program a dead object and
		// nothing else: declining must not undo the effects the operand itself had (the same rule the
		// static-answer comparisons keep when they evaluate both sides anyway).
		return "", false, nil
	}
	g.heapUsed = true
	g.seqUsed = true
	found := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_contains_tagged(i32 %s, i32 %s, i32 %s)\n", found, h, needle, needleTag)
	cmp := g.newTmp()
	if n.Op == "in" {
		fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", cmp, found)
	} else {
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, 0\n", cmp, found)
	}
	g.markI1(cmp)
	out := g.newTmp()
	fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", out, cmp)
	return out, true, nil
}

// seqBuildsContainer is the decide-before-building question the same roads ask: does this expression
// denote a sequence object the door will build? A text repeat answers no — its answer is an @str_tab
// index, which every text road already takes.
func (g *irGen) seqBuildsContainer(e Expr) (string, bool) {
	n, isBin := e.(*BinOp)
	if !isBin {
		return "", false
	}
	op, ok := g.seqPlanOf(n)
	if !ok || op.kind == seqTextRepeatOp {
		return "", false
	}
	return containerKindList, true
}

// seqIsTextExpr is the answer the two text predicates — `exprIsString` and `printsAsInternedStr` — are
// given for the repeat of a text. It is the same arrangement ADR 0360 built for the formatting: the door
// that decides the shape also answers "is the answer a text", so the printer, the length, the subscript,
// the method road and the binding cannot disagree about a value they each receive from `*`.
func (g *irGen) seqIsTextExpr(e Expr) bool {
	n, isBin := e.(*BinOp)
	if !isBin {
		return false
	}
	op, ok := g.seqPlanOf(n)
	return ok && op.kind == seqTextRepeatOp
}
