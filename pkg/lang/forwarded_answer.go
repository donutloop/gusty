package lang

// forwarded_answer.go — the kind a call answers when its body HANDS BACK one of its own parameters.
//
// A container slot is written as two words: the payload and the tag that says what the payload means
// (ADR 0187). Every door that writes one asks the same question — `elemKindTag` — and for most
// expressions the answer is spelled by the expression itself: `"a"` is text, `1.5` is a float box,
// `None` is the void, `[1, 2]` is a list. A call spells nothing: the kind belongs to the CALLEE's
// body, and the tables that read a body (`isFloat`, `isNoneExpr`, `printsAsInternedStr`, the answer
// plan's arms) all read what a `return` SPELLS. A body that returns a parameter spells nothing at
// all, and every one of those questions fell through to the tag table's default:
//
//	def g(v):
//	    return v
//	print([g(None), 1])      # [None, 1] in the reference; the program printed [0, 1]      — the void, tagged int
//	print([g([1, 2]), 3])    # [[1, 2], 3] in the reference; the program printed [1, 3]    — a handle, tagged int
//	for v in [g(x) for x in [1, "a", 2.5, None]]:   # 1, a, 2.5, None; the program printed 1, 0, 1, 0
//	    print(v)
//
// — the last shape being the comprehension half of roadmap **Gap R.502**: the builder wrote the payload
// through `rt_append_tagged` and the tag it wrote was the compiler's guess, so the loop the object was
// walked by (**ADR 0443**) printed each slot's number and called it success at exit 0.
//
// The reading is simple and it is the reference's own: a body whose EVERY `return` names ONE of its own
// parameters answers whatever the CALLER handed that parameter. The value the callee returns IS the word
// the caller passed, so the kind of the answer is the kind of the argument, and the argument is right
// there at the call site — asked of this same door, one expression earlier. Nothing about the callee has
// to be guessed, and nothing about the caller has to be assumed:
//
//   - an argument the compiler can read (`g(2.5)`, `g("a")`, `g(None)`, `g([1])`, the item an unrolled
//     loop bound for `x`) answers a CONSTANT tag, the same number the literal element answers;
//   - an argument whose kind only the objects can tell — a name the pair road bound (`n = xs[0]`) —
//     answers a REGISTER, the tag the objects wrote, and the element travels as the pair every other
//     pair element travels in (`pairElemPair`, ADR 0306's arrangement for a subscript);
//   - an argument whose kind neither can tell declines, and the call keeps the road it has always had.
//     This door answers; it never takes an answer away.
//
// What the door insists on, and why each is a wrong answer rather than a corner:
//
//   - every `return` names the SAME parameter — two arms naming two parameters answer two kinds;
//   - no road in the body can reach the end without a `return`, or the missing answer is the void
//     whatever the argument was (`answerBodyReturns` answers this conservatively, in the safe direction);
//   - the callee never REBINDS that parameter — `def g(v): v = 1; return v` answers an int whatever it
//     is handed, and the last-binding-wins rule (ADR 0172) is exactly what the argument's kind is not;
//   - the body is not a generator, not `async`, not decorated, and not the function being called: a
//     generator's answer is a list of its own, a decorator replaces the answer, and a recursive hand-off
//     reads a word the inner frame has not written yet (**ADR 0273**'s exclusion, inherited here);
//   - the argument is not re-lowered to fetch its tag. A pair-bound NAME's tag is a load of the two
//     allocas the pair road already wrote, so asking it cannot run anything twice; the tag of a
//     subscript, a `.pop()` or a call would come from re-emitting that read, and `g(xs.pop(0))` must not
//     empty the list twice to learn what it holds.
//
// The positions the door serves are the ones that take two words: a container literal's element, a list
// slot, a dict entry, a set member, and every comprehension builder — the roads **ADR 0306**, **ADR 0310**,
// **ADR 0311** and **ADR 0332** opened one spelling at a time. The positions that keep ONE word for a whole
// value (a call's argument, arithmetic's operand, a `return`) keep **Gap R.146**'s standing refusal: they
// have nowhere to put the register, and a slot written by a builder that guessed is the wrong answer the
// refusal exists to prevent.

import (
	"fmt"
	"strconv"
	"strings"
)

// forwardedParameterOperand answers the door's ONE question: "this call's answer is the value of one of
// the callee's parameters, handed in by the caller — here is the expression THIS call site hands it".
//
// It is a pure reading of the AST and emits nothing, which is what lets the same answer serve the gates
// that decide a container's shape before any IR exists and the doors that write the two words afterwards
// (**ADR 0249**'s gate-and-door rule: they must ask one question, not two).
func (g *irGen) forwardedParameterOperand(x Expr) (arg Expr, paramName string, ok bool) {
	if g == nil || x == nil {
		return nil, "", false
	}
	c, isCall := x.(*Call)
	if !isCall || c == nil {
		return nil, "", false
	}
	nm, isName := c.Fn.(*Name)
	if !isName || nm == nil {
		// A call through an attribute is the method road's question, and a method's first parameter is
		// `self` — the answer of `def m(self): return self` is the RECEIVER, not an argument the caller
		// handed (**Gap R.453**/**ADR 0430** own round for the method answer direction).
		return nil, "", false
	}
	if _, isClass := g.classInfos[nm.Value]; isClass {
		return nil, "", false
	}
	if g.externs[nm.Value] != nil {
		return nil, "", false
	}
	fd := g.lookupFuncDef(nm.Value)
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || len(fd.Captures) > 0 {
		return nil, "", false
	}
	if fd.Name == nm.Value && g.curFunc == fd.Name {
		// The recursive hand-off: the inner frame would write the word the outer caller is about to read.
		return nil, "", false
	}
	if containsYield(fd.Body) {
		return nil, "", false
	}
	returns, fallsOff := answerBodyReturns(fd.Body)
	if fallsOff || len(returns) == 0 {
		return nil, "", false
	}
	idx := -1
	for _, r := range returns {
		v, isName := r.Expr.(*Name)
		if !isName || v == nil {
			return nil, "", false
		}
		i := paramIndexOf(fd, v.Value)
		// The name has to be a parameter and nothing else: a `return` of a module name, a field, or a
		// local the body never bound is a kind the argument cannot speak for.
		if i < 0 || bodyBindsName(fd.Body, v.Value) {
			return nil, "", false
		}
		if idx >= 0 && idx != i {
			return nil, "", false
		}
		idx = i
	}
	if idx < 0 {
		return nil, "", false
	}
	arg, ok = callOperandForParameter(c, fd, idx)
	if !ok {
		return nil, "", false
	}
	return arg, fd.Params[idx].Name, true
}

// paramIndexOf names the position of a parameter by spelling, or -1. A method's `self` is a parameter
// like any other to this reading, which is why the caller excludes an attribute call before asking.
func paramIndexOf(fd *FuncDef, name string) int {
	if fd == nil {
		return -1
	}
	for i, p := range fd.Params {
		if p != nil && p.Name == name {
			return i
		}
	}
	return -1
}

// callOperandForParameter resolves which expression a call hands to parameter `idx`: the positional
// argument at that position, the `name=value` spelling that names it, or the declaration's own default
// when the call omits it. An argument that is neither (a keyword no parameter names, too few positionals
// with no default, the same parameter filled twice) declines the door — a call whose argument vector the
// reading cannot resolve has no single operand to ask about.
func callOperandForParameter(c *Call, fd *FuncDef, idx int) (Expr, bool) {
	if c == nil || fd == nil || idx < 0 || idx >= len(fd.Params) || fd.Params[idx] == nil {
		return nil, false
	}
	var positional Expr
	pos := 0
	for _, a := range c.Args {
		if kw, isKw := a.(*KeywordArg); isKw {
			if kw != nil && kw.Name == fd.Params[idx].Name {
				if positional != nil || kw.Value == nil {
					return nil, false
				}
				return kw.Value, true
			}
			continue
		}
		if pos == idx {
			positional = a
		}
		pos++
	}
	if positional != nil {
		return positional, true
	}
	if pos > idx {
		return nil, false // a hole in the positional vector: not this backend's call convention anyway
	}
	if d := fd.Params[idx].Default; d != nil {
		return d, true
	}
	return nil, false
}

// bodyBindsName reports whether any statement of a body binds a name — an assignment, an augmented
// assignment, a `for` target, a `with … as`, an `except … as`, or a `match` capture — anywhere in it,
// including in a nested `def`. The pattern half is deliberately conservative: capture patterns are `*Name`
// nodes and a name that can be captured is a name this reading cannot follow.
func bodyBindsName(sts []Stmt, name string) bool {
	for _, st := range sts {
		if stmtBindsName(st, name) {
			return true
		}
	}
	return false
}

func stmtBindsName(st Stmt, name string) bool {
	switch s := st.(type) {
	case *AssignStmt:
		if s == nil {
			break
		}
		if nameIsAssignedTo(s.Target, name) {
			return true
		}
	case *AugAssignStmt:
		if s == nil {
			break
		}
		if nameIsAssignedTo(s.Target, name) {
			return true
		}
	case *ForStmt:
		if s == nil {
			break
		}
		if nameIsAssignedTo(s.Var, name) || bodyBindsName(s.Body, name) || bodyBindsName(s.Else, name) {
			return true
		}
	case *WithStmt:
		if s == nil {
			break
		}
		if s.As != nil && s.As.Value == name {
			return true
		}
		if bodyBindsName(s.Body, name) {
			return true
		}
	case *IfStmt:
		if s == nil {
			break
		}
		for _, el := range s.Elifs {
			if el != nil && bodyBindsName(el.Then, name) {
				return true
			}
		}
		if bodyBindsName(s.Then, name) || bodyBindsName(s.Else, name) {
			return true
		}
	case *WhileStmt:
		if s == nil {
			break
		}
		if bodyBindsName(s.Body, name) || bodyBindsName(s.Else, name) {
			return true
		}
	case *TryStmt:
		if s == nil {
			break
		}
		if bodyBindsName(s.Body, name) || bodyBindsName(s.Finally, name) {
			return true
		}
		for _, ec := range s.Excepts {
			if ec == nil {
				continue
			}
			if ec.As == name || bodyBindsName(ec.Body, name) {
				return true
			}
		}
	case *MatchStmt:
		if s == nil {
			break
		}
		for _, mc := range s.Cases {
			if mc == nil {
				continue
			}
			if bodyBindsName(mc.Body, name) || patternBindsName(mc.Pattern, name) || patternBindsName(mc.Guard, name) {
				return true
			}
			for _, o := range mc.Or {
				if patternBindsName(o, name) {
					return true
				}
			}
		}
	case *FuncDef:
		// A nested definition's bindings are its own frame's, but a body that names the same spelling is
		// a body whose reading this is not: declining is the answer, not an argument either way.
		if s != nil && bodyBindsName(s.Body, name) {
			return true
		}
	}
	return false
}

// nameIsAssignedTo reports a binding of `name` through a target, including through a tuple/list unpacking
// — `v, w = 1, 2` binds `v` as surely as `v = 1` does.
func nameIsAssignedTo(target Expr, name string) bool {
	switch t := target.(type) {
	case nil:
		return false
	case *Name:
		return t != nil && t.Value == name
	case *Tuple:
		for _, el := range t.Elems {
			if nameIsAssignedTo(el, name) {
				return true
			}
		}
	case *ListLit:
		for _, el := range t.Elems {
			if nameIsAssignedTo(el, name) {
				return true
			}
		}
	}
	return false
}

// patternBindsName is the conservative capture reading: any `*Name` written in a pattern — or in a
// container of patterns — is treated as a name the arm binds.
func patternBindsName(p Expr, name string) bool {
	switch pt := p.(type) {
	case nil:
		return false
	case *Name:
		return pt != nil && pt.Value == name
	case *Tuple:
		for _, el := range pt.Elems {
			if patternBindsName(el, name) {
				return true
			}
		}
	case *ListLit:
		for _, el := range pt.Elems {
			if patternBindsName(el, name) {
				return true
			}
		}
	case *DictLit:
		for _, el := range pt.Vals {
			if patternBindsName(el, name) {
				return true
			}
		}
	}
	return false
}

// forwardedAnswerTagUnder asks the forwarded door's tag question WITHOUT emitting: the argument's kind
// as a constant, or, when only the objects can tell it, the register the pair road wrote for the argument.
// The second half is restricted to a name the pair road bound precisely because it is a load of the two
// allocas that already exist — asking it runs nothing, so a call whose argument is a read with effects
// (`g(xs.pop(0))`) is declined rather than emitted twice.
func (g *irGen) forwardedAnswerTagUnder(x Expr) (tag string, fromRegister bool, ok bool) {
	arg, _, okArg := g.forwardedParameterOperand(x)
	if !okArg || arg == nil {
		return "", false, false
	}
	if g.nameIsPairElem(arg) {
		return "", true, true
	}
	if !handOffArgumentTravelsAsOneWord(arg) {
		return "", false, false
	}
	t, hasTag := g.elemKindTag(arg)
	if !hasTag {
		return "", false, false
	}
	return strconv.FormatInt(int64(t), 10), false, true
}

// handOffArgumentTravelsAsOneWord asks whether the argument a hand-off will forward can reach the callee as
// the single word the ordinary call road passes. A READ cannot: `id({"a": "x"}["a"])` has its kind in the
// container the subscript asks, so the callee receives an index, hands an index back, and the element prints
// the interned word beside a kind nobody wrote — the answered wrong that
// `TestTheOneWordPositionsStillRefuseAPairElement` pins as a refusal (**Gap R.146**, **Gap R.164**: the call
// signature is the door that would carry the pair, and it is not written). The pair road BOUND the name, so a
// name is fine — its tag is a load of the two allocas that already exist.
func handOffArgumentTravelsAsOneWord(arg Expr) bool {
	switch arg.(type) {
	case *Index, *Slice, *Attr:
		return false
	}
	if c, isCall := arg.(*Call); isCall {
		// `xs.pop(0)` and the answer plan's carried calls are the same shape by another name: the kind lives
		// in the object the call reached, not in the word it returned.
		if nm, isName := c.Fn.(*Name); isName && nm.Value == "pop" {
			return false
		}
		if _, isAttr := c.Fn.(*Attr); isAttr {
			return false
		}
	}
	return true
}

// forwardedCallIsPairElem is the GATE: does this call's answer travel as a (payload, tag) pair the
// builder must hand to the tagged road? Only the register case qualifies — a constant tag needs no pair,
// and the ordinary builder already writes one, so opening the tagged road for it would allocate and tag
// a list the constant read answers for nothing (**ADR 0249** again).
func (g *irGen) forwardedCallIsPairElem(x Expr) bool {
	if g == nil || x == nil {
		return false
	}
	c, isCall := x.(*Call)
	if !isCall {
		return false
	}
	// A call the pair road or the answer plan already serves keeps its own door: this gate exists for the
	// hand-off neither of them reads, and a second gate over a value with two owners is two answers. The
	// broad pair-callee gate (`pairReturningCallIsPairElem`) asks THIS one first, so it is not asked here —
	// two gates each consulting the other is the recursion this door's own stack trace names.
	if g.carriedCallIsPairElem(c) || g.popAnswerIsPairElem(c) {
		return false
	}
	if callee, isName := c.Fn.(*Name); isName && g.pairCallAsked[callee.Value] {
		return false
	}
	_, fromReg, ok := g.forwardedAnswerTagUnder(c)
	if !ok {
		return false
	}
	return fromReg || g.forwardedCallSlotsItself(c)
}

// forwardedCallPair is the DOOR: emit the call, and hand its answer to the builder as the two words the
// argument carries — the constant the argument's spelling answers, or the register the pair road wrote.
//
// The tag is settled BEFORE the call is emitted, so a door that declines after lowering would leave the
// call's instructions in the module for nothing (roadmap ADR 0166's rule that a broken module is never the
// program's fault).
func (g *irGen) forwardedCallPair(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	c, isCall := e.(*Call)
	if !isCall {
		return "", "", false, nil
	}
	tag, fromRegister, answered := g.forwardedAnswerTagUnder(c)
	if !answered {
		return "", "", false, nil
	}
	arg, _, _ := g.forwardedParameterOperand(c)
	// The permission is this call's alone, exactly as the pair road takes it: `value` refuses any other
	// position that asks a pair-returning callee for one word, and a door that is asking for BOTH words has
	// to say so while it lowers, or the refusal it exists to avoid is the one it emits
	// (roadmap L11.1, **ADR 0273**'s gate).
	var callee string
	if nm, isName := c.Fn.(*Name); isName {
		callee = nm.Value
		g.pairCallAsked[callee] = true
	}
	v, verr := g.value(b, c)
	if callee != "" {
		delete(g.pairCallAsked, callee)
	}
	if verr != nil {
		return "", "", false, verr
	}
	if fromRegister {
		if nm, isName := arg.(*Name); isName {
			_, t := g.numericPairRegs(b, nm.Value, nm.Span())
			tag = t
		} else {
			return "", "", false, nil
		}
	}
	// The answer may be a handle — a float box, another container — and the word the callee returned is
	// only live while whoever made it keeps it alive. ADR 0280's root, taken at the call instead of at a
	// binding, so the next allocation cannot recycle the slot the pair is about to be written from.
	if t := forwardedTagIsHandle(tag); t {
		pay := g.newTmp()
		fmt.Fprintf(b, "  %s = alloca i32\n", pay)
		fmt.Fprintf(b, "  store i32 %s, i32* %s\n", v, pay)
		fmt.Fprintf(b, "  call void @rt_root_put(i32* %s)\n", pay)
		g.rooted = true
	}
	return v, tag, true, nil
}

// forwardedCallSlotsItself is the claim a container makes about one of its slots: does the value this
// hand-off call answers mean nothing without its tag? A kind the compiler cannot name (the tag travels in
// a register the objects wrote) always needs the slots to speak for themselves; a kind it can name needs
// them when the tag is one of the payload-less-by-itself kinds — a float box, the void, another container,
// a verdict, an instance (**ADR 0232**'s self-describing slots, asked of the ONE tag reading rather than of
// a list of shape predicates: roadmap **Gap R.440**'s rule that a gate and a door must not keep two
// readings, and **Gap R.502**'s reason for asking at all).
func (g *irGen) forwardedCallSlotsItself(x Expr) bool {
	tag, fromRegister, ok := g.forwardedAnswerTagUnder(x)
	if !ok {
		return false
	}
	if fromRegister {
		return true
	}
	t, err := strconv.Atoi(tag)
	if err != nil {
		return false
	}
	return slotTagSelfDescribing(int32(t)) || int32(t) == int32(TagTuple)
}

// handOffOrSpelledAnswer reports that a call's answer is one the compiler can name from the CALLEE's body:
// either the caller's own argument handed straight back, or a value the body SPELLS. A body that COMPUTES
// its answer (`def g(v): return v + 0`) is neither — the kind its tag slot holds is then a run-time fact of
// that frame, and an element built from a guess about it prints a float box's address as a number. Refusing
// that shape is the standing answer (roadmap **Gap R.146**'s one-word positions), and this is the gate that
// keeps the claim honest: a container literal may only take the tagged road for an answer whose kind some
// door can actually hand over (**ADR 0249**'s gate-and-door rule, **Gap R.502**'s boundary).
func (g *irGen) handOffOrSpelledAnswer(x Expr) bool {
	if g == nil || x == nil {
		return false
	}
	c, isCall := x.(*Call)
	if !isCall || c == nil {
		return false
	}
	if _, _, isHandOff := g.forwardedParameterOperand(c); isHandOff {
		// The argument must be able to reach the callee as one word, or the whole claim is void — see
		// `handOffArgumentTravelsAsOneWord`.
		if arg, _, ok := g.forwardedParameterOperand(c); ok && handOffArgumentTravelsAsOneWord(arg) {
			return true
		}
		return false
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		return false
	}
	fd := g.lookupFuncDef(nm.Value)
	if fd == nil {
		return false
	}
	returns, fallsOff := answerBodyReturns(fd.Body)
	if fallsOff || len(returns) == 0 {
		return false
	}
	for _, r := range returns {
		switch r.Expr.(type) {
		case *IntLit, *FloatLit, *BoolLit, *NoneLit, *StrLit, *ListLit, *DictLit, *SetLit, *Tuple:
		default:
			return false
		}
	}
	return true
}

// setLiteralHandsBackATag is the set's own version of the list literal's claim: does some MEMBER mean
// nothing without the kind word the hand-off carries? A set dedups on the (payload, kind) pair, so a member
// stored by the kind-less road not only prints wrong — it dedups wrong, and it never gets asked whether it
// hashes at all. `{g([1, 2])}` printed `{[1, 2]}` while the reference dies `TypeError: unhashable type:
// 'list'`: the guard lives on the tagged member road, and the gate that opens that road never admitted a
// hand-off (**Gap R.502**, **ADR 0232**'s self-describing slots).
func (g *irGen) setLiteralHandsBackATag(sl *SetLit) bool {
	if g == nil || sl == nil {
		return false
	}
	for _, el := range sl.Elems {
		if g.forwardedCallSlotsItself(el) {
			return true
		}
	}
	return false
}

// dictLiteralHandsBackATag is the same claim for a dict literal, asked of BOTH positions: a key the hand-off
// hands back needs its kind for the hashability question, and a value needs it to print at all
// (`{1: g(None)}` printed `{1: 0}`).
func (g *irGen) dictLiteralHandsBackATag(dl *DictLit) bool {
	if g == nil || dl == nil {
		return false
	}
	for _, k := range dl.Keys {
		if g.forwardedCallSlotsItself(k) {
			return true
		}
	}
	for _, v := range dl.Vals {
		if g.forwardedCallSlotsItself(v) {
			return true
		}
	}
	return false
}

// forwardedTagIsHandle reports that a slot holding this tag holds a heap handle rather than an immediate:
// the kinds whose payload only means something through the object it names.
func forwardedTagIsHandle(tag string) bool {
	t, err := strconv.Atoi(tag)
	if err != nil {
		return false
	}
	switch int32(t) {
	case int32(TagFloat), int32(TagList), int32(TagDict), int32(TagSet), int32(TagInstance):
		return true
	}
	return false
}

// guardHashableExprTag asks the hashing question of a slot whose tag the compiler read out of an
// EXPRESSION rather than off a spelling. The tag alone cannot tell a tuple from the list layout it shares
// (**ADR 0361**), and the two are not the same answer to CPython: `{(1, 2)}` and `{(1, 2): 1}` build, and
// `{[1, 2]}` dies `TypeError: unhashable type: 'list'`. So the AST gets the last word, and it is asked of
// the hand-off's ARGUMENT — the only place the tuple's spelling survives the call (**Gap R.81**, L11.3).
func (g *irGen) guardHashableExprTag(b *strings.Builder, tag string, e Expr, sp Span) {
	if g.exprIsHashableTupleAnswer(e) {
		return
	}
	// A CONSTANT tag the compiler can see needs no compares UNLESS it names one of the kinds that cannot
	// hash: a text, an int, a verdict emits three dead `icmp`s that the verifier folds away, and a literal
	// whose every kind is a constant is exactly the literal that keeps its static road — a module carrying
	// three raises per slot is the guard becoming the shape being compiled rather than the check it is.
	if t, err := strconv.Atoi(tag); err == nil {
		switch int32(t) {
		case int32(TagList), int32(TagDict), int32(TagSet):
		default:
			return
		}
	}
	g.guardHashableTag(b, tag, sp)
}

// exprIsHashableTupleAnswer reports that the value this expression answers is a tuple whose every element
// has a hash of its own — spelled as a tuple, or handed back by a body that forwards its parameter
// (roadmap **Gap R.502**; the element walk is `tupleElementsHashable`'s, the reference's own).
func (g *irGen) exprIsHashableTupleAnswer(e Expr) bool {
	if e == nil || g == nil {
		return false
	}
	if ln, isList := e.(*ListLit); isList && ln.IsTuple() {
		return tupleElementsHashable(ln)
	}
	if arg, _, ok := g.forwardedParameterOperand(e); ok && arg != nil {
		if ln, isList := arg.(*ListLit); isList && ln.IsTuple() {
			return tupleElementsHashable(ln)
		}
	}
	return false
}
