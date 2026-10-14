package lang

// ═══════════════════════════════════════════════════════════════════════════
// The `key=` argument is asked what it is — and a key the compiler can call is
// called where the comparison is made (roadmap L11.7 · the ordering builtins'
// callable argument; closes Gap R.249; ADR 0340).
//
// `sorted([3, 1, 2], key=abs)` and `min(3, 1, -2, key=abs)` are programs the reference answers —
// `[1, 2, 3]` and `-2`. This backend answered neither. Worse, it answered three of the family
// *at exit 0*: the option arguments were read by position, so anything in the second slot was the
// reverse flag. `sorted([3, 1, 2], key=abs, reverse=True)` printed `[1, 2, 3]` where CPython prints
// `[3, 2, 1]` — the `reverse=True` was never seen, because the first option the door looked at was
// the key — and `sorted([3, 1, 2], key=3)` printed `[3, 2, 1]` where CPython raises
// `TypeError: 'int' object is not callable`: a non-callable was tested for truthiness and used as a
// direction. A third shape reached `llc` at all: `min(3, 1, -2, key=abs)` emitted a load of `%_abs`,
// a global that does not exist, because `abs` was lowered as an *operand* of the fold — exit 2, the
// code the contract reserves for a module the compiler wrote and LLVM refused (ADR 0166). Two more
// refusals blamed the wrong thing: `sorted(..., key=...)` said `sorted(reverse=...) needs a constant
// flag`, and `min([...], key=...)` said "not a container among values" about a program that wrote one
// container and one function (ADR 0322's class: a pass keyed by the wrong thing).
//
// The fix is the same one this file's neighbours use: **ask the argument what it is, at the door,
// before the road reads it.** Options are read by NAME — `key=`, `reverse=` — and a positional second
// argument stays what this language documented it to be (a constant repeat flag, ADR 0207's family of
// the language's own readings). A key the compiler can call is called by the ordinary call road, once
// per element, on the element's own expression: the door that answers `abs(3)` anywhere else in the
// program answers it here, so an arity mistake inside the key raises the reference's `TypeError`, a
// key that cannot run on an element raises the reference's `TypeError` (`object of type 'int' has no
// len()`), and nothing about calling a function is re-implemented here (ADR 0233's rule against a
// second answer for one construct).
//
// The ordering itself is computed with the runtime's own comparator and no new helper: the rank of an
// element is how many keys come before it — `@rt_elem_gt` for the strict half, and payload equality for
// the tie half, which is content-addressed for texts because interning is (ADR 0173) — and the element
// is written to its rank. Ranks are distinct and 0..n-1, so one pass places the whole list, stability
// comes from the tie count comparing indices rather than keys, and `reverse=True` flips which side of
// the comparison is counted rather than reversing ties (which is what the reference's stable descending
// sort does).
//
// What stays refused is refused by name: elements whose kind the compiler cannot see (a tagged slot's
// word is the value word's, L11.1), a key whose answer is a float (no comparator prints from one word,
// Gap R.248's family), a key the call road cannot answer, and a key the compiler cannot see the kind of
// at all — the last is L11.7's own sentence, "the reference asks the value what it is at the moment of
// the call". What stays *raising* is what the reference raises: a non-callable key, a key of the wrong
// arity, an option the builtin does not take. Those are emitted with `raiseTo`, so the program's own
// `except TypeError:` catches them (ADR 0214, ADR 0228) and the answer is not a compile-time refusal
// wearing a runtime error's clothes.
// ═══════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"strings"
)

// keyPosition is what the door decided a `key=` argument is.
type keyPosition int

const (
	keyAbsent   keyPosition = iota // no key= at all: the unkeyed road answers
	keyCallable                    // a function the ordinary call road can call with one element
	keyRaises                      // the reference raises a TypeError here; the message is the reference's
	keyRefused                     // neither: a refusal that names what is missing
)

// keyAnswer is the door's verdict about a `key=` argument, with the sentence the caller owes the
// program. modeKnown/mode say how the KEY's answers compare — 0 orders payloads as numbers, 1 orders
// them as indices into the intern table, the two modes @rt_elem_gt already answers.
type keyAnswer struct {
	pos        keyPosition
	spelling   string
	name       string
	raiseType  string
	raiseMsg   string
	refusal    string
	mode       int
	modeKnown  bool
	minMaxOnly bool // set when the verdict differs between the two doors (unused today)
}

// callOptions reads a call's arguments the way the reference reads them: positional operands keep
// their order, and the options this family takes — `key=` and `reverse=` — are recognised by NAME.
// The second positional argument of `sorted` is deliberately left in `pos`: this language documents it
// as a constant repeat flag, and a row that quietly turned that documented reading into a TypeError
// would be the same class of surprise ADR 0207 settled for `for x in <integer>`.
func (g *irGen) callOptions(c *Call) (pos []Expr, key Expr, reverse Expr, badKw string) {
	if c == nil {
		return nil, nil, nil, ""
	}
	for _, a := range c.Args {
		kw, isKw := a.(*KeywordArg)
		if !isKw {
			pos = append(pos, a)
			continue
		}
		switch kw.Name {
		case "key":
			key = kw.Value
		case "reverse":
			reverse = kw.Value
		default:
			if badKw == "" {
				badKw = kw.Name
			}
		}
	}
	return pos, key, reverse, badKw
}

// keyDoorOf asks the `key=` argument what it is. It emits nothing: the question is answered from the
// AST and the notebooks, and the caller decides whether to raise, refuse or call (ADR 0273's purity,
// the reason the three verdicts are values and not side effects).
func (g *irGen) keyDoorOf(e Expr) keyAnswer {
	ans := keyAnswer{spelling: exprSurface(e)}
	if e == nil {
		ans.pos = keyAbsent
		return ans
	}
	if _, isNone := e.(*NoneLit); isNone {
		// `key=None` is not a non-callable — the reference reads it as "no key at all", the identity,
		// and answers the program. The door that refused it with TypeError would be stopping a program
		// the reference runs (measured: sorted([3,1,2], key=None) is [1, 2, 3]).
		ans.pos = keyAbsent
		return ans
	}
	if _, isLambda := e.(*Lambda); isLambda {
		// A lambda IS callable in the reference; here a function is declared rather than assigned, so
		// there is no value to hand to the call road. The refusal says which door is missing (L11.7).
		ans.pos = keyRefused
		ans.refusal = "`key=lambda ...` is refused: a lambda is a function VALUE, and this backend has none — `def` bindings are declared, not assigned, so the key has no word to carry to the call (roadmap L11.7)"
		return ans
	}
	nm, isName := e.(*Name)
	if !isName {
		// A call expression as the key is an expression whose RESULT kind the door cannot safely read as a
		// compile-time name. `print(1)` must first print, then raise that None is not callable; a key door that
		// statically raises would skip the side effect, and one that statically answers would ignore it. Refuse
		// until the value word can carry the call's answer and call it.
		if _, isCall := e.(*Call); isCall {
			ans.pos = keyRefused
			ans.refusal = fmt.Sprintf("`key=%s` is refused: the key is an expression whose answer this backend cannot name a kind for, and the door that calls a key has to know what the key hands back before it can compare two of them (roadmap L11.1, L11.7)", exprSurface(e))
			return ans
		}
		// A literal or container: the reference calls it and stops. When the door can read the answer's kind it
		// writes the reference's own type word ('int', 'list'); when it cannot, that sentence would be a guess,
		// and the honest answer is the refusal beside it.
		if t, ok := g.elemKindTag(e); ok {
			ans.pos = keyRaises
			ans.raiseType = "TypeError"
			ans.raiseMsg = fmt.Sprintf("'%s' object is not callable", tagTypeWord(t, e))
			return ans
		}
		ans.pos = keyRaises
		ans.raiseType = "TypeError"
		ans.raiseMsg = fmt.Sprintf("'%s' object is not callable", referenceTypeWord(e))
		return ans
	}
	name := nm.Value
	if g.builtinShadowed(name) {
		// The program's own name, not the builtin's. Its kind is answerable from the notebooks; an
		// answer that cannot be read is L11.7's own sentence, not an invention of this row.
		if g.isFloat(e) {
			ans.pos = keyRaises
			ans.raiseType = "TypeError"
			ans.raiseMsg = "'float' object is not callable"
			return ans
		}
		if _, ok := g.fds[name]; ok {
			return g.userFunctionKey(name)
		}
		if kind, known := g.keyWordKind(name); known {
			ans.pos = keyRaises
			ans.raiseType = "TypeError"
			ans.raiseMsg = fmt.Sprintf("'%s' object is not callable", kind)
			return ans
		}
		if t, ok := g.elemKindTag(e); ok {
			ans.pos = keyRaises
			ans.raiseType = "TypeError"
			ans.raiseMsg = fmt.Sprintf("'%s' object is not callable", tagTypeWord(t, e))
			return ans
		}
		ans.pos = keyRefused
		ans.refusal = fmt.Sprintf("`key=%s` is refused: the program bound this name to something the compiler cannot read a kind from, and the reference asks the value what it is at the moment of the call — which needs the value word that carries its own kind (roadmap L11.1, L11.7)", exprSurface(e))
		return ans
	}
	if fd, ok := g.fds[name]; ok {
		return g.userFunctionKey(name, fd)
	}
	if kind, known := g.keyWordKind(name); known {
		ans.pos = keyRaises
		ans.raiseType = "TypeError"
		ans.raiseMsg = fmt.Sprintf("'%s' object is not callable", kind)
		return ans
	}
	if isPredeclaredName(name) {
		return g.builtinKey(name)
	}
	// A name the front end resolved but that is neither a def nor a builtin: whatever it is bound to,
	// a function is declared rather than assigned in this backend, so a name that is not a def is not
	// callable — the same reading that refuses `g = dbl` (roadmap L11.7).
	if t, ok := g.elemKindTag(e); ok {
		ans.pos = keyRaises
		ans.raiseType = "TypeError"
		ans.raiseMsg = fmt.Sprintf("'%s' object is not callable", tagTypeWord(t, e))
		return ans
	}
	ans.pos = keyRefused
	ans.refusal = fmt.Sprintf("`key=%s` is refused: %s is not a function this backend can call — a function is declared rather than assigned here, so a name that does not name a def has no call to make (roadmap L11.7)", exprSurface(e), exprSurface(e))
	return ans
}

// userFunctionKey answers for `key=f` where f is the program's own def. The arity question is the
// reference's, because the reference calls the key with exactly one argument and says so when the
// function cannot take one.
func (g *irGen) userFunctionKey(name string, fd ...*FuncDef) keyAnswer {
	ans := keyAnswer{spelling: name, name: name}
	def := g.fds[name]
	if len(fd) == 1 && fd[0] != nil {
		def = fd[0]
	}
	if def == nil {
		ans.pos = keyRefused
		ans.refusal = fmt.Sprintf("`key=%s` is refused: this backend has no definition to call for that name (roadmap L11.7)", name)
		return ans
	}
	// A parameter with a default can be omitted, so `def f(x, y=1)` is callable with one argument.
	required := 0
	for _, p := range def.Params {
		if p.Default == nil {
			required++
		}
	}
	switch {
	case len(def.Params) == 0:
		ans.pos = keyRaises
		ans.raiseType = "TypeError"
		ans.raiseMsg = fmt.Sprintf("%s() takes 0 positional arguments but 1 was given", name)
		return ans
	case required > 1:
		missing := def.Params[required-1]
		ans.pos = keyRaises
		ans.raiseType = "TypeError"
		ans.raiseMsg = fmt.Sprintf("%s() missing %d required positional argument%s: '%s'", name, required-1, pluralWord(required-1), missing.Name)
		return ans
	case len(def.Params) > 1 && required == 1:
		// One required parameter and optional extras: callable with one argument, exactly as the
		// reference calls a key.
	}
	ans.pos = keyCallable
	switch {
	case g.strFuncs[name]:
		ans.mode, ans.modeKnown = 1, true
	case g.floatFuncs[name]:
		ans.pos = keyRefused
		ans.refusal = fmt.Sprintf("`key=%s` is refused: the key answers a number that is not an integer, and no comparator in this backend orders two of them from one word — the tag the value word carries decides it (roadmap L11.1, Gap R.248)", name)
		return ans
	default:
		ans.mode, ans.modeKnown = 0, true
	}
	return ans
}

// builtinKey answers for `key=<builtin>`: the callable is the ordinary call road's, so the answer set
// is whatever `abs(elem)`/`len(elem)`/`str(elem)` already answers elsewhere in the program — including
// the raise for a builtin that cannot run on that element (`len(3)` is the reference's `TypeError:
// object of type 'int' has no len()`, and it comes from the road, not from a sentence written here).
func (g *irGen) builtinKey(name string) keyAnswer {
	ans := keyAnswer{pos: keyCallable, spelling: name, name: name}
	switch name {
	case "len", "abs", "ord", "int":
		ans.mode, ans.modeKnown = 0, true
	case "str", "repr":
		ans.mode, ans.modeKnown = 1, true
	case "float", "round", "sum", "min", "max", "sorted", "reversed":
		ans.pos = keyRefused
		ans.refusal = fmt.Sprintf("`key=%s` is refused: the key answers something this backend has no comparator for from one word — either a float or a container, and both need the tag the value word carries (roadmap L11.1, Gap R.248)", name)
	default:
		ans.pos = keyRefused
		ans.refusal = fmt.Sprintf("`key=%s` is refused: this backend cannot call that builtin with one element as its argument (roadmap L11.7)", name)
	}
	return ans
}

// keyedOrderError is the refusal shared by the three doors when the ELEMENTS, not the key, are the
// half that is missing.
func (g *irGen) keyedOrderError(elems []Expr, ans keyAnswer) error {
	if ans.pos == keyRefused {
		return fmt.Errorf("%s", ans.refusal)
	}
	for _, el := range elems {
		if _, ok := g.elemKindTag(el); !ok {
			return fmt.Errorf("codegen: ordering by `key=%s` is refused over %s: the door compares the elements it can name, and this one's kind is only written by the run time — the tagged slot's word is the value word's (roadmap L11.1)", ans.spelling, exprSurface(el))
		}
	}
	return nil
}

// keyRaisesTo emits the reference's TypeError for a key it would have tried to call. It is a raise and
// not a refusal because the reference stops the program there, mid-run, with that sentence — and a
// program that writes `except TypeError:` around it has run to completion (ADR 0214, ADR 0228).
func (g *irGen) keyRaisesTo(b *strings.Builder, ans keyAnswer, sp Span) {
	g.raiseTo(b, exnCode(ans.raiseType), ans.raiseType, ans.raiseMsg, sp)
}

// keyCallReg calls the key on one element, through the road that answers that call anywhere else in
// the program: the synthesised `key(element)` is lowered by g.value, so arity, parameter pairs,
// closures, float returns and every raise inside the key are the ordinary call's business and not
// this row's (ADR 0233: one construct, one answer).
func (g *irGen) keyCallReg(b *strings.Builder, key Expr, elem Expr, sp Span) (string, error) {
	call := &Call{Fn: key, Args: []Expr{elem}, Src: sp}
	return g.value(b, call)
}

// keyedOrderInto builds the list `elems` in the order the key orders them, at the call site, and
// hands back the heap handle. The rank of an element is the number of keys that come before it plus
// the number of earlier elements whose key equals it — strict ordering from @rt_elem_gt, ties from
// payload equality — so ranks are distinct, land on 0..n-1, and equal keys keep the order the program
// wrote (the stability ADR 0191's sorter promised). reverse=True counts the other side of the same
// comparison, which is how the reference's descending sort stays stable.
func (g *irGen) keyedOrderInto(b *strings.Builder, elems []Expr, key Expr, ans keyAnswer, reverse bool, sp Span) (string, error) {
	if len(elems) == 0 {
		g.heapSeq++
		h := fmt.Sprintf("%%h%d", g.heapSeq)
		g.heapUsed = true
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapKindList))
		g.listOperands[h] = true
		return h, nil
	}
	vals := make([]string, len(elems))
	keys := make([]string, len(elems))
	for i, el := range elems {
		v, err := g.value(b, el)
		if err != nil {
			return "", err
		}
		k, err := g.keyCallReg(b, key, el, sp)
		if err != nil {
			return "", err
		}
		vals[i], keys[i] = v, k
	}
	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapKindList))
	interned := ans.mode == 1
	for i, el := range elems {
		b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %s, i32 %s, i32 %s)\n", h, vals[i], g.elemTagOperand(b, el, interned)))
	}
	for i := range elems {
		rank := "0"
		for j := range elems {
			// rank_i = #{ j : key_j comes before key_i } + #{ j < i : key_j equals key_i }.
			// Ascending counts the keys SMALLER than this one — gt(k_i, k_j) — and descending counts the
			// ones that beat it, gt(k_j, k_i): flipping which side of the same comparator is counted is
			// how reverse=True stays stable, with equal keys keeping the order the program wrote.
			strict := g.newTmp()
			if reverse {
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_elem_gt(i32 %s, i32 %s, i32 %d)\n", strict, keys[j], keys[i], ans.mode))
			} else {
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_elem_gt(i32 %s, i32 %s, i32 %d)\n", strict, keys[i], keys[j], ans.mode))
			}
			isGt := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", isGt, strict))
			g.markI1(isGt)
			gtBit := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", gtBit, isGt))
			added := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", added, rank, gtBit))
			rank = added
			if j < i {
				// An earlier element with an equal key stays ahead of this one.
				tie := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", tie, keys[j], keys[i]))
				g.markI1(tie)
				tieBit := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", tieBit, tie))
				kept := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", kept, rank, tieBit))
				rank = kept
			}
		}
		b.WriteString(fmt.Sprintf("  call void @rt_put_elem(i32 %s, i32 %s, i32 %s)\n", h, rank, vals[i]))
	}
	g.listOperands[h] = true
	return h, nil
}

// keyedPickInto chooses the element whose key wins, by the same comparator the order is written with.
// Ties keep the first element, which is what the reference does, so the test is the strict one.
func (g *irGen) keyedPickInto(b *strings.Builder, elems []Expr, key Expr, ans keyAnswer, wantMin bool, sp Span) (string, error) {
	if len(elems) == 0 {
		return "", fmt.Errorf("codegen: %s over an empty list with `key=%s` has no winner — the reference raises ValueError here, and this door does not answer an empty choice", map[bool]string{true: "min", false: "max"}[wantMin], ans.spelling)
	}
	vals := make([]string, len(elems))
	keys := make([]string, len(elems))
	for i, el := range elems {
		v, err := g.value(b, el)
		if err != nil {
			return "", err
		}
		k, err := g.keyCallReg(b, key, el, sp)
		if err != nil {
			return "", err
		}
		vals[i], keys[i] = v, k
	}
	// The comparator lives in the heap runtime block (@rt_elem_gt), and a block that a body
	// references must have its data half too — the flag is the honest way to say it (Gap R.256).
	g.heapUsed = true
	bestV, bestK := vals[0], keys[0]
	for i := 1; i < len(elems); i++ {
		ord := g.newTmp()
		if wantMin {
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_elem_gt(i32 %s, i32 %s, i32 %d)\n", ord, bestK, keys[i], ans.mode))
		} else {
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_elem_gt(i32 %s, i32 %s, i32 %d)\n", ord, keys[i], bestK, ans.mode))
		}
		take := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", take, ord))
		g.markI1(take)
		nv := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", nv, take, vals[i], bestV))
		nk := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", nk, take, keys[i], bestK))
		bestV, bestK = nv, nk
	}
	return bestV, nil
}

// keyedEmptyTrap is the reference's sentence for min/max over nothing: there is no winner, and the
// program is told with the name of the builtin that asked for one.
func (g *irGen) keyedEmptyTrap(b *strings.Builder, wantMin bool, sp Span) {
	name := "min()"
	if !wantMin {
		name = "max()"
	}
	g.raiseTo(b, exnCode("ValueError"), "ValueError", name+" iterable argument is empty", sp)
}

// referenceTypeWord is the word CPython writes for a value's type in a TypeError sentence, for the
// keys the door rejects without calling them.
func referenceTypeWord(e Expr) string {
	switch e.(type) {
	case *IntLit:
		return "int"
	case *FloatLit:
		return "float"
	case *StrLit, *FString:
		return "str"
	case *ListLit, *Comp, *Generator:
		return "list"
	case *DictLit:
		return "dict"
	case *SetLit:
		return "set"
	case *Tuple:
		return "tuple"
	case *BoolLit:
		return "bool"
	case *NoneLit:
		return "NoneType"
	}
	return "object"
}

// tagTypeWord is the same word read out of an element's tag, with the two spellings the reference
// keeps apart: a tag that says text is 'str' and a tag that says verdict is 'bool'.
func tagTypeWord(tag int32, e Expr) string {
	switch tag {
	case int32(TagStr):
		return "str"
	case int32(TagFloat):
		return "float"
	case int32(TagBool):
		return "bool"
	case int32(TagNone):
		return "NoneType"
	case int32(TagInt):
		return "int"
	}
	return referenceTypeWord(e)
}

func pluralWord(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// keyConstBool reads a `reverse=` argument the way the door always has: a constant, or nothing.
func (g *irGen) keyConstBool(e Expr) (bool, bool) {
	if e == nil {
		return false, false
	}
	if bl, ok := e.(*BoolLit); ok {
		return bl.Value, true
	}
	v, ok := g.foldConstInt(e)
	if !ok {
		return false, false
	}
	return v != 0, true
}

// identityKey is `key=None`: the reference reads it as "no key", and the door has to answer it by
// stepping aside — but by handing the road beside it the SAME arguments minus that one option, so no
// fold ever reads a `name = value` argument as an operand again (that reading is what emitted a load
// of `%_abs` and made `llc` reject the module; ADR 0166's class).
func identityKey(e Expr) bool {
	if e == nil {
		return false
	}
	_, isNone := e.(*NoneLit)
	return isNone
}

// withoutIdentityKey rebuilds the call with the identity key removed and every other argument, in
// order, untouched — including a documented constant repeat flag, which this door does not own.
func withoutIdentityKey(c *Call) *Call {
	args := make([]Expr, 0, len(c.Args))
	for _, a := range c.Args {
		if kw, isKw := a.(*KeywordArg); isKw && kw.Name == "key" && identityKey(kw.Value) {
			continue
		}
		args = append(args, a)
	}
	return &Call{Fn: c.Fn, Args: args, Src: c.Src, Ty: c.Ty}
}

// normaliseIdentityKey is the same step for the roads that ask their questions about the CALL rather
// than lower it — a `for` loop classifies its iterable before anything lowers it, and an option that
// means "no key" has to be gone before that classification, not after: the loop's fold asks for the
// shape of `n.Args[1:]`, and a no-op option sitting in it would decline a program the builtin answers
// (roadmap Gap R.249, ADR 0340).
func normaliseIdentityKey(e Expr) Expr {
	c, ok := e.(*Call)
	if !ok {
		return e
	}
	fn, isName := c.Fn.(*Name)
	if !isName || (fn.Value != "sorted" && fn.Value != "reversed") {
		return e
	}
	for _, a := range c.Args {
		if kw, isKw := a.(*KeywordArg); isKw && kw.Name == "key" && identityKey(kw.Value) {
			return withoutIdentityKey(c)
		}
	}
	return e
}

// keyWordKind reads the notebooks for what a NAME holds, so the TypeError the door raises writes the
// word the reference writes: a list variable's word is an i32 handle, and asking only for its element
// tag answered 'int' where the program's own name is a list (measured: sorted([3,1], key=xs) — the
// reference says 'list', and a sentence that blames the wrong kind sends the reader to the wrong row,
// which is ADR 0322's complaint in a new place).
func (g *irGen) keyWordKind(name string) (string, bool) {
	// The pair's own question, asked of the name: it answers a list the program built at run time and
	// a list the compiler kept as a constant with the same word, which is what the TypeError sentence
	// needs (the notebooks alone miss the constant case, and the answer came out 'int').
	if kind, ok := g.staticContainerKind(&Name{Value: name}); ok {
		switch kind {
		case "list", "dict", "set":
			return kind, true
		}
	}
	switch {
	case g.listVars[name]:
		return "list", true
	case g.runtimeDicts[name]:
		return "dict", true
	case g.runtimeSets[name]:
		return "set", true
	case g.noneVars[name]:
		return "NoneType", true
	}
	return "", false
}

// keyedAnswer is the door's answer to a call it took over: done means the caller must not lower the
// call any other way, handle is the register (or the empty string when err carries a refusal).
type keyedAnswer struct {
	done   bool
	handle string
	err    error
}

// keyedSortedCall is `sorted`'s door for a call that wrote a key= (or an option it does not take).
// handleOnly says which answer the caller needs: sortedRuntime's callers (print's container path, the
// loop's materialisation) print or walk a HEAP HANDLE, while the expression road may still take the
// fold's compile-time list global. An identity key that re-dispatched without saying which would hand
// the print road `rt_print_list_mixed(i32 @.lst1)` — a global where a word belongs, exit 2 (ADR 0166).
func (g *irGen) keyedSortedCall(b *strings.Builder, c *Call, handleOnly bool) keyedAnswer {
	if fn, ok := c.Fn.(*Name); !ok || fn.Value != "sorted" || g.builtinShadowed(fn.Value) {
		return keyedAnswer{}
	}
	pos, key, reverse, badKw := g.callOptions(c)
	if key == nil && badKw == "" {
		return keyedAnswer{} // no key: the literal fold, the runtime copy and the repeat flag answer, unchanged
	}
	if identityKey(key) {
		if handleOnly {
			h, err := g.sortedRuntime(b, withoutIdentityKey(c))
			return keyedAnswer{done: true, handle: h, err: err}
		}
		reg, err := g.call(b, withoutIdentityKey(c))
		return keyedAnswer{done: true, handle: reg, err: err}
	}
	sp := c.Span()
	if badKw != "" {
		// CPython's own sentence, measured: sorted([1], cmp=f) → "TypeError: 'cmp' is an invalid
		// keyword argument for sort()". It is a raise, not a refusal, because the reference runs the
		// program to that point and stops it there.
		g.keyRaisesTo(b, keyAnswer{raiseType: "TypeError", raiseMsg: fmt.Sprintf("'%s' is an invalid keyword argument for sort()", badKw)}, sp)
		return keyedAnswer{done: true, handle: "0"}
	}
	if len(pos) != 1 {
		return keyedAnswer{done: true, err: fmt.Errorf("codegen: sorted(...) with `key=%s` is refused over %d positional arguments: the reference takes the iterable alone positionally — its answer for a second one is TypeError: sorted expected 1 argument, got 2 — and this language's documented constant repeat flag is read only when no key is given (roadmap L11.7, Gap R.249)", exprSurface(key), len(pos))}
	}
	ans := g.keyDoorOf(key)
	switch ans.pos {
	case keyRaises:
		g.keyRaisesTo(b, ans, sp)
		return keyedAnswer{done: true, handle: "0"}
	case keyRefused:
		return keyedAnswer{done: true, err: fmt.Errorf("%s", ans.refusal)}
	case keyAbsent:
		return keyedAnswer{}
	}
	operand := pos[0]
	lit, isList := operand.(*ListLit)
	if !isList {
		return keyedAnswer{done: true, err: fmt.Errorf("codegen: sorted(%s, key=%s) is refused: the order comes from calling %s on every element, and the elements of that container exist only at run time — the call would have to carry an element to the key, which is the value word's job (roadmap L11.1, L11.7). The same container without a key is answered by @rt_sort (ADR 0339, ADR 0191)", exprSurface(operand), exprSurface(key), exprSurface(key))}
	}
	if err := g.keyedOrderError(lit.Elems, ans); err != nil {
		return keyedAnswer{done: true, err: err}
	}
	rv := false
	if reverse != nil {
		var ok bool
		if rv, ok = g.keyConstBool(reverse); !ok {
			return keyedAnswer{done: true, err: fmt.Errorf("codegen: sorted(..., reverse=...) needs a constant flag beside a key (roadmap L11.7)")}
		}
	}
	h, err := g.keyedOrderInto(b, lit.Elems, key, ans, rv, sp)
	if err != nil {
		return keyedAnswer{done: true, err: err}
	}
	return keyedAnswer{done: true, handle: h}
}

// keyedMinMaxCall is `min`/`max`'s door for a call that wrote an option. The two builtins take a key
// and no reverse; the reference says so, and this door says the same thing in the same words.
func (g *irGen) keyedMinMaxCall(b *strings.Builder, c *Call) keyedAnswer {
	fn, ok := c.Fn.(*Name)
	if !ok || g.builtinShadowed(fn.Value) {
		return keyedAnswer{}
	}
	isMin := fn.Value == "min"
	if !isMin && fn.Value != "max" {
		return keyedAnswer{}
	}
	pos, key, reverse, badKw := g.callOptions(c)
	if key == nil && badKw == "" && reverse == nil {
		return keyedAnswer{}
	}
	if identityKey(key) {
		reg, err := g.call(b, withoutIdentityKey(c))
		return keyedAnswer{done: true, handle: reg, err: err}
	}
	sp := c.Span()
	if badKw != "" {
		g.keyRaisesTo(b, keyAnswer{raiseType: "TypeError", raiseMsg: fmt.Sprintf("'%s' is an invalid keyword argument for %s()", badKw, fn.Value)}, sp)
		return keyedAnswer{done: true, handle: "0"}
	}
	if reverse != nil {
		g.keyRaisesTo(b, keyAnswer{raiseType: "TypeError", raiseMsg: fmt.Sprintf("'reverse' is an invalid keyword argument for %s()", fn.Value)}, sp)
		return keyedAnswer{done: true, handle: "0"}
	}
	if key == nil {
		return keyedAnswer{}
	}
	ans := g.keyDoorOf(key)
	switch ans.pos {
	case keyRaises:
		g.keyRaisesTo(b, ans, sp)
		return keyedAnswer{done: true, handle: "0"}
	case keyRefused:
		return keyedAnswer{done: true, err: fmt.Errorf("%s", ans.refusal)}
	case keyAbsent:
		return keyedAnswer{}
	}
	var elems []Expr
	switch {
	case len(pos) == 1:
		if lit, isLit := pos[0].(*ListLit); isLit {
			elems = lit.Elems
		} else if len(pos) == 1 {
			return keyedAnswer{done: true, err: fmt.Errorf("codegen: %s(%s, key=%s) is refused: the candidates are a container the program built at run time, and the key would be called on words that only exist there — calling across that gap needs the value word (roadmap L11.1, L11.7)", fn.Value, exprSurface(pos[0]), exprSurface(key))}
		}
	case len(pos) >= 2:
		elems = pos
	default:
		return keyedAnswer{done: true, err: fmt.Errorf("codegen: %s with `key=%s` has no candidates (roadmap L11.7)", fn.Value, exprSurface(key))}
	}
	if len(elems) == 0 {
		g.keyedEmptyTrap(b, isMin, sp)
		return keyedAnswer{done: true, handle: "0"}
	}
	if err := g.keyedOrderError(elems, ans); err != nil {
		return keyedAnswer{done: true, err: err}
	}
	reg, err := g.keyedPickInto(b, elems, key, ans, isMin, sp)
	if err != nil {
		return keyedAnswer{done: true, err: err}
	}
	return keyedAnswer{done: true, handle: reg}
}
