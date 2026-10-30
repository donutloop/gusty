package lang

import (
	"fmt"
	"sort"
	"strings"
)

// =============================================================================
// A `for` loop asks its iterable what it is (L11.1, Gap R.246, ADR 0339)
//
// Five shapes of the same program —
//
//	for v in sorted([3, 1, 2]):                print(v)
//	for v in reversed([1, 2, 3]):              print(v)
//	for v in sorted([]):                       print(v)
//	for v in sorted([3, 1, 2], reverse=True):  print(v)
//	for v in sorted([3, 1, 2][0:2]):           print(v)
//
// — landed on two answers, and neither was the reference's. The first four reached
// `llc-20`, which wrote out the module it had been handed and refused to assemble it:
//
//	global variable reference must have pointer type: %t1 = icmp slt i32 %_ctr1.ld1, @.lst1
//
// — the loop's counter had been told to count up to the ADDRESS of a list's global, and
// exit 2, which the exit-code contract reserves for exactly that: a module the compiler
// wrote and LLVM refused (ADR 0166). The fifth answered `0` at exit 0, because the counter
// road went on to *succeed*, and a loop variable bound to the counter prints the counter.
// A loop over an empty list, and a loop over a text handed to `reversed`, were silent at
// exit 0 too.
//
// Both behaviours come from one omission: `case *ForStmt:` asks whether the iterable is a
// literal it can unroll, or a name whose kind it can look up, and if neither, hands the
// expression to the numeric-range road — which asks the iterable for a BOUND and, given a
// container, receives a global's address. `sorted` and `reversed` were never in that
// question, while the same calls standing where an expression goes were answered long ago:
// `print(sorted([3, 1, 2]))` prints `[1, 2, 3]`, and `print(sorted(xs))` sorts a real heap
// list with @rt_sort.
//
// The two doors this row opens lead to roads that already exist, and each shares its
// implementation with the expression road rather than re-writing it (ADR 0258's discipline:
// an ordering that lives in one place cannot disagree with itself):
//
//   - a `sorted`/`reversed` whose iterable the compiler can WRITE DOWN — a literal list, a
//     literal text, either nested in the other — is FOLDED to the list it denotes, by the
//     builtin's own literal sort, and the loop then walks that literal the way
//     `for v in [1, 2, 3]` does;
//   - a `sorted`/`reversed` over a container the runtime can measure joins the container pair
//     — `isContainerExpr`, `staticContainerKind`, `containerHandleOf` — and hands back the
//     handle @rt_sort built or @rt_list_copy + @rt_reverse flipped, the same helpers
//     `print(sorted(xs))` uses (ADR 0333's pair, this row's two new members);
//
// and everything else the loop cannot walk is REFUSED by name, which is the third answer a
// loop can give and the only honest one left: a `for` whose body prints the loop variable
// answers at exit 0 whether it walked anything or not, so a container this backend cannot
// step over would print one fabricated number and call it success.
// =============================================================================

// compContainerKind is a comprehension's OWN answer to “what kind of container are you”, read off the
// brace the program wrote: `[` → a list, `{a}` → a set, `{a: b}` → a dict. It is the one question
// **ADR 0439** asks of an element's tag, **ADR 0442** asks of the count, and **ADR 0443** asks of the
// loop head, the condition and `bool()` — asked ONCE, of the spelling, because a comprehension's kind
// is not a run-time fact: the author wrote the bracket. Anything else is not a comprehension, and the
// caller's own question stands (roadmap **Gap R.490**, **Gap R.500**).
func compContainerKind(e Expr) (string, bool) {
	c, ok := e.(*Comp)
	if !ok {
		return "", false
	}
	switch c.Kind {
	case CompList:
		return "list", true
	case CompSet:
		return "set", true
	case CompDict:
		return "dict", true
	}
	return "", false
}

// foldedForIterable rewrites a `for`'s iterable when the loop can walk the container the
// expression DENOTES rather than the expression itself: `sorted([3, 1, 2])` denotes the literal
// list [1, 2, 3]. Only a call is rewritten — a literal list or text is already the road's own
// shape, and rewriting it would give the fold power over programs it never answered.
func (g *irGen) foldedForIterable(e Expr) (Expr, bool) {
	if _, isCall := e.(*Call); !isCall {
		return nil, false
	}
	elems, ok := g.foldedIterableElems(e)
	if !ok {
		return nil, false
	}
	// An EMPTY result is still an answer: `for v in sorted([])` steps over nothing, which is what
	// `for v in []` already prints — nothing, at the exit code of success. Refusing it would turn a
	// working answer into a refusal, which ADR 0249 forbids.
	return &ListLit{Elems: elems, Src: e.Span()}, true
}

// foldedIterableElems is the fold asked one layer at a time, so the operations NEST in the
// order the reference applies them: `reversed(sorted([1, 3, 2]))` is the sorted list walked
// backwards, not the literal backwards and then sorted. Unwrapping to the innermost literal —
// which is what `listCallElems` does for the len/sum folds, where order does not matter —
// would answer 2, 3, 1 where the reference answers 3, 2, 1.
func (g *irGen) foldedIterableElems(e Expr) ([]Expr, bool) {
	switch n := e.(type) {
	case *ListLit:
		// An empty literal is a container the loop walks for zero elements, which is an answer.
		return n.Elems, true
	case *StrLit:
		// A bare text is the loop's OWN road below (`for c in "abc"`); this fold claims a text only
		// from inside `reversed`, where that road does not reach.
		return nil, false
	case *Call:
		fn, ok := n.Fn.(*Name)
		if !ok || g.builtinShadowed(fn.Value) || len(n.Args) == 0 {
			return nil, false
		}
		inner, okInner := g.foldedIterableElems(n.Args[0])
		if !okInner {
			if s, isStr := n.Args[0].(*StrLit); isStr && fn.Value == "reversed" && len(n.Args) == 1 {
				return runeElemsReversed(s.Value), true // reversed("abc") walks c, b, a by CODE POINT
			}
			return nil, false
		}
		switch fn.Value {
		case "reversed":
			if len(n.Args) != 1 {
				return nil, false
			}
			return reversedExprs(inner), true
		case "sorted":
			// Text sorts at RUNTIME through @rt_sort (the handle road beside this), and floats not
			// at all; the fold answers the integer literal list, which is the shape the builtin's
			// literal road answers, through that road's own function (ADR 0258).
			if !reverseFlagIsSpellable(n.Args[1:]) {
				// A second POSITIONAL argument is the reference's `key`, and the reference stops the
				// program with a TypeError; answering it here would be answering a program the
				// reference refuses (roadmap Gap R.247 owns the callable-argument door).
				return nil, false
			}
			if elems, folded, _ := g.sortedLiteralElems(inner, n.Args[1:]); folded {
				return elems, true
			}
			return nil, false
		}
	}
	return nil, false
}

// runeElemsReversed is a text walked from the other end, in CODE POINT order — the same
// per-rune string literals the loop's own text road builds for `for c in "abc"`, so this row's
// fold and that road differ in nothing but which end they start at (L11.5's code-point rule:
// `reversed("café")` is é, f, a, c, and a byte-reversed text would print two bytes of the é).
func runeElemsReversed(s string) []Expr {
	var elems []Expr
	runes := []rune(s)
	for i := len(runes) - 1; i >= 0; i-- {
		elems = append(elems, &StrLit{Value: string(runes[i])})
	}
	return elems
}

// runeElems is a text walked in CODE POINT order — the forward sibling of `runeElemsReversed`, and the
// same per-rune string literals the loop's own text road builds for `for c in "abc"`. The reference's
// `sorted("bca")` builds a list of one-character strings and orders them; the list it builds is THIS
// list, spelled the same way, so the sort, the loop and the print all order the same elements
// (roadmap **Gap R.247**, **ADR 0473**; the code-point rule is **ADR 0225**/L11.5 — a byte-reversed or
// byte-sorted text would split an `é` into two members).
func runeElems(s string) []Expr {
	var elems []Expr
	for _, r := range []rune(s) {
		elems = append(elems, &StrLit{Value: string(r)})
	}
	return elems
}

// constRangeElems is `range(...)` answered as the LIST the reference's `sorted(range(3))` builds: the
// integers, in the order `range` yields them, when every bound is a constant the compiler can read.
// The semantics are the reference's — one, two or three arguments, a negative step, and an empty range
// when the step points away from the stop — because a `sorted` that ordered a different sequence than
// the one `range` yields would be a second answer to one question (roadmap **Gap R.247**).
//
// A non-constant bound is `ok == false`, which is the scan's own limitation and roadmap **Gap R.250**'s
// to own: the value road has no table for a builtin call's answer, so the sequence behind a variable's
// number has no home here yet.
func (g *irGen) constRangeElems(c *Call) ([]Expr, bool) {
	if len(c.Args) == 0 || len(c.Args) > 3 {
		return nil, false
	}
	for _, a := range c.Args {
		if _, isKw := a.(*KeywordArg); isKw {
			return nil, false
		}
	}
	bound := func(i int) (int64, bool) {
		if i >= len(c.Args) {
			return 0, true
		}
		v, err := g.constIntVal(c.Args[i])
		return v, err == nil
	}
	var start, stop, step int64
	switch len(c.Args) {
	case 1:
		v, ok := bound(0)
		if !ok {
			return nil, false
		}
		stop, step = v, 1
	case 2:
		a, okA := bound(0)
		b, okB := bound(1)
		if !okA || !okB {
			return nil, false
		}
		start, stop, step = a, b, 1
	default:
		a, okA := bound(0)
		b, okB := bound(1)
		s, okS := bound(2)
		if !okA || !okB || !okS {
			return nil, false
		}
		start, stop, step = a, b, s
	}
	if step == 0 {
		return nil, false // range()'s own step-zero stop lives in its road; nothing is walked here
	}
	var elems []Expr
	if step > 0 {
		for v := start; v < stop; v += step {
			elems = append(elems, &IntLit{Value: v})
		}
	} else {
		for v := start; v > stop; v += step {
			elems = append(elems, &IntLit{Value: v})
		}
	}
	return elems, true
}

// orderingSequenceArg is the ONE question the ordering builtins ask of an argument that is not a
// container but an ITERABLE the reference builds one from: it answers the LIST the sequence denotes, in
// the order the sequence yields it, for the three shapes this backend had no road for — a TEXT (the
// one-character strings, in code-point order), a `range(...)` whose bounds are constants (the integers
// `range` yields), and nothing else. ok is false for every argument the roads beside this already own
// (a list literal, a name bound to one, a slice, a call that answers a list) and for every argument no
// road can name, which is the walkability gate's answer rather than this one's.
//
// A DICT is deliberately absent. `sorted(d)` walks a dict for its KEYS and that walk has an owner
// already — **ADR 0417**'s `sortedDictAnswers` and `@rt_dict_keys`, which stride the entry array by two
// and stop at the entry count — and a second road pulling keys out of a dict would be a second stride
// rule, which is the defect (**Gap R.426**) that road exists to prevent. What the dict was owed is the
// element's KIND, and that is `forIterSortedElemKind` beside this, asked of the same records the dict
// sort reads.
//
// Each answer is the sequence the REFERENCE yields, because the ordering, the loop and the print all
// read it: an order that disagreed with `range`'s, or with the text road's per-rune list, is a program
// printing one sequence through `for` and another through `print` (roadmap **Gap R.247**, **ADR 0258**'s
// one table, **ADR 0472**'s three roads).
func (g *irGen) orderingSequenceArg(a Expr) (Expr, bool) {
	switch x := a.(type) {
	case *StrLit:
		return &ListLit{Elems: runeElems(x.Value), Src: x.Span()}, true
	case *Name:
		// A text the program NAMED is the same sequence: the record the front end keeps of the name's one
		// text answers "what characters", exactly as **ADR 0417**'s key road asks the same record what kind
		// of key it holds. A name the record cannot see is not a refusal here — the gate beside this one
		// says no, and its sentence names the missing half.
		if s, held := g.strVals[x.Value]; held {
			return &ListLit{Elems: runeElems(s), Src: x.Span()}, true
		}
		return nil, false
	case *Call:
		nm, isName := x.Fn.(*Name)
		if !isName || g.builtinShadowed(nm.Value) || nm.Value != "range" {
			return nil, false
		}
		if elems, isRange := g.constRangeElems(x); isRange {
			return &ListLit{Elems: elems, Src: x.Span()}, true
		}
		return nil, false
	}
	return nil, false
}

// reverseFlagIsSpellable reports whether the arguments after the iterable are spellable by
// this backend's own ordering: nothing, or `reverse=<constant>`. A bare positional second
// argument is the reference's `key`, and a third argument is neither.
func reverseFlagIsSpellable(rest []Expr) bool {
	if len(rest) == 0 {
		return true
	}
	if len(rest) != 1 {
		return false
	}
	kw, isKw := rest[0].(*KeywordArg)
	return isKw && kw.Name == "reverse"
}

// sortedLiteralElems is sorted() over a list the program wrote down, and the ONE ordering the
// `sorted` answer and the `for` loop share: builtinSorted's literal branch calls it, and
// foldedIterableElems beside it calls it, so the order a program sees when it prints a sorted
// list and the order it sees when it steps over one cannot be written twice and drift apart
// (ADR 0258's one-table rule, applied to a loop; roadmap Gap R.246, ADR 0339).
//
// folded is false when an element is not an integer literal — the literal roads' own
// limitation, which the caller states in its own sentence. err says so when the `reverse=`
// flag is not a constant, or names something other than `reverse`.
func (g *irGen) sortedLiteralElems(elems []Expr, rest []Expr) ([]Expr, bool, error) {
	if len(elems) == 0 {
		// Nothing to order is an order this road answers: the caller walks zero elements.
		return elems, true, nil
	}
	type sortable struct {
		val  int64
		expr Expr
	}
	pairs := make([]sortable, len(elems))
	for i, el := range elems {
		v, isInt := intLiteralValue(el)
		if !isInt {
			return nil, false, nil
		}
		pairs[i] = sortable{val: v, expr: el}
	}
	reverse := false
	if len(rest) == 1 {
		arg := rest[0]
		if kw, isKw := arg.(*KeywordArg); isKw {
			if kw.Name != "reverse" {
				return nil, false, fmt.Errorf("sorted: %s= is refused: this backend orders a literal list by value and by the reverse= flag, and %s= needs a callable this backend can build a call to (roadmap L11.1)", kw.Name, kw.Name)
			}
			arg = kw.Value
		}
		v, cErr := g.constIntVal(arg)
		if cErr != nil {
			return nil, false, fmt.Errorf("sorted(reverse=...) needs a constant flag (roadmap L11.7)")
		}
		reverse = v != 0
	} else if len(rest) > 1 {
		return nil, false, nil
	}
	// The value and its expression travel TOGETHER. Sorting a permutation of `elems` while
	// comparing a separate array of values — the shape this road had — compares positions that the
	// sort is itself moving: `sorted([3, 1, 2])` came out 1, 3, 2 and `sorted([2, 2, 1])` came out
	// 2, 1, 2, each at the exit code of success (roadmap Gap R.246).
	key := func(i, j int) bool {
		if pairs[i].val != pairs[j].val {
			return pairs[i].val < pairs[j].val
		}
		return i < j // stable: equal elements keep the order the program wrote them in
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		if reverse {
			return key(j, i)
		}
		return key(i, j)
	})
	out := make([]Expr, len(pairs))
	for i, p := range pairs {
		out[i] = p.expr
	}
	return out, true, nil
}

// intLiteralValue reads an integer literal's value without the fold's own error: the caller
// decides how to say no.
func intLiteralValue(e Expr) (int64, bool) {
	if il, ok := e.(*IntLit); ok {
		return il.Value, true
	}
	return 0, false
}

// containerSortBuiltin recognises the two builtins whose whole job is to take a container and
// hand back one: sorted(iterable[, reverse=…]) and reversed(seq). A shadowed name — a program
// that defines `sorted` itself — is not this shape, which is why the builtin's shadowing table
// is asked. The arity that is not the builtin's (a second positional argument to `sorted`) is
// still recognised, because the guard beside it has to refuse that program too.
func (g *irGen) containerSortBuiltin(e Expr) (fn string, arg Expr, ok bool) {
	c, isCall := e.(*Call)
	if !isCall || len(c.Args) == 0 {
		return "", nil, false
	}
	name, isName := c.Fn.(*Name)
	if !isName {
		return "", nil, false
	}
	switch name.Value {
	case "sorted", "reversed":
	default:
		return "", nil, false
	}
	if g.builtinShadowed(name.Value) {
		return "", nil, false
	}
	return name.Value, c.Args[0], true
}

// printRoadSeesAnOrderingArgument is the print door's question about the argument of one of the two
// ordering builtins: does this argument reach the heap list `sortedRuntime` builds? A container does, and
// so does one of the three iterables the ordering substitutes a list for — a TEXT (its characters), a DICT
// (its keys) and a `range(...)` whose bounds the compiler can read (its integers). Asking a SPELLING test
// instead (`isContainerExpr`, a record of what a NAME holds) is how `print(sorted("bca"))` and
// `print(sorted(range(3)))` fell past the printer road to the numeric one, which printed the ordering's own
// answer with `%d` — `0`, the interned index of "a", and the address of a compile-time global, both at the
// exit code of success (roadmap **Gap R.247**, **ADR 0473**). `reversed` keeps the narrow gate: a text handed
// to it is the TEXT road's reversal, and printing its iterator is roadmap **Gap R.251**'s row.
func (g *irGen) printRoadSeesAnOrderingArgument(fn string, a Expr) bool {
	if a == nil || (fn != "sorted" && fn != "reversed") {
		return false
	}
	if g.isContainerExpr(a) {
		return true
	}
	if fn != "sorted" {
		return false
	}
	_, isSeq := g.orderingSequenceArg(a)
	return isSeq
}

// orderingArgumentIsNameableSequence is the walkable half of the ordering pair: can this backend put in
// front of @rt_sort / @rt_reverse the sequence the reference would build from this argument? Four shapes
// reach it: the TEXT and the constant `range(...)` that `orderingSequenceArg` substitutes a list for, the
// DICT whose keys `@rt_dict_keys` strides out, and an ordering handed ANOTHER ordering — `sorted(sorted(x))`
// walks the very sequence the inner call walked, so it is walkable wherever that call is. Anything else — a
// call the doors do not name, a generator, a number — is not, and the loop beside this refuses it by name
// rather than walking the wrong object (roadmap **Gap R.247**, **ADR 0473**).
func (g *irGen) orderingArgumentIsNameableSequence(a Expr) bool {
	if a == nil {
		return false
	}
	if _, isSeq := g.orderingSequenceArg(a); isSeq {
		return true
	}
	if _, _, handled, derr := g.sortedDictAnswers(a); handled && derr == nil {
		return true
	}
	if c, isCall := a.(*Call); isCall {
		if _, _, isNested := g.containerSortBuiltin(c); isNested {
			_, walkable := g.sortedReversedOverWalkableList(c)
			return walkable
		}
	}
	return false
}

// orderingDoorIsSorted names which of the two ordering doors a call goes to: `sorted` orders, `reversed`
// flips. It is one word, and every road that has to know it has to know it the same way — the fold, the
// comparator and the binding's element kind all read the same name through this question rather than each
// writing `fn.Value == "sorted"` beside its own argument walk (**ADR 0258**'s one-table rule; roadmap
// **Gap R.247**, **ADR 0473**).
func (g *irGen) orderingDoorIsSorted(e Expr) bool {
	fn, _, isCall := g.containerSortBuiltin(e)
	return isCall && fn == "sorted"
}

// recordOrderingBinding writes what the roads look up for a name bound to the answer of `sorted` or
// `reversed` — the three questions every container binding answers: which kind it is, whether the slots
// describe themselves, and whether a static literal stands behind the name. The element kind is asked of
// `forIterSortedElemKind`, the one door that also picks @rt_sort's comparator and the loop's slot, so
// `xs = sorted("bca")` cannot be a text list at the print door and an unlabelled word at the subscript:
// the binding used to record NOTHING, which is why `print(xs)` answered `['a', 'b', 'c']` while
// `print(xs[0])` answered `2` — the interned index of "a" — and `for v in xs:` printed `2|0|1` where the
// reference prints `a|b|c`, all at the exit code of success (roadmap **Gap R.247**, **ADR 0473**). A text's
// list is BUILT tagged, with bit 8 set (**ADR 0232**), so the name claims `mixedLists`, the claim that sends
// a reader to the slot tags; an integer list is the plain one, and the record that names it is the same
// `listElemInt` an `xs = [1, 2]` assignment writes.
func (g *irGen) recordOrderingBinding(name string, e Expr) bool {
	fn, arg, isCall := g.containerSortBuiltin(e)
	if !isCall || arg == nil {
		return false
	}
	kind, known := g.forIterSortedElemKind(arg, fn == "sorted")
	if !known {
		return false
	}
	g.settleContainerKind(name, containerKindList)
	switch kind {
	case "text":
		// Every slot holds an interned text — the same claim `xs = ["bb", "aa"]`'s builder writes through
		// `heapListFrom`, and the reason both spellings of a text list now answer the subscript, the
		// concatenation and the loop the same way (**ADR 0232**'s slots are the truth; roadmap **Gap R.247**).
		g.mixedLists[name] = false
		g.listElemStr[name] = true
		g.listElemInt[name] = false
	case "object":
		// The builder wrote a tag with every slot and let the object speak for itself; the reader has to
		// ask them, which is what the mixed claim does.
		g.mixedLists[name] = true
		g.listElemStr[name] = false
		g.listElemInt[name] = false
	case "number":
		g.mixedLists[name] = false
		g.listElemStr[name] = false
		g.listElemInt[name] = true
	default:
		return false
	}
	delete(g.containerLits, name)
	delete(g.noneVars, name)
	delete(g.boolVars, name)
	if g.strVals != nil {
		delete(g.strVals, name)
	}
	if g.dictVals != nil {
		delete(g.dictVals, name)
	}
	return true
}

// sortedReversedOverWalkableList is the container pair's question: is this expression a
// `sorted` or `reversed` whose argument is a LIST this backend can put in front of @rt_sort /
// @rt_reverse — a list literal, a list variable, a slice of a list, or a call that carries a
// list? A dict, a set, a text, a mixed list, a generator or a number is not: the runtime sort
// would walk the wrong object, and the answer a loop takes from that is a number the program
// never wrote. ok is false for those, and the caller refuses them by name.
func (g *irGen) sortedReversedOverWalkableList(e Expr) (arg Expr, ok bool) {
	fn, a, isCall := g.containerSortBuiltin(e)
	if !isCall {
		return nil, false
	}
	c, _ := e.(*Call)
	if fn == "reversed" && len(c.Args) != 1 {
		return nil, false // reversed() takes one argument; the extra is the program's, not this road's
	}
	pos, key, _, badKw := g.callOptions(c)
	if badKw != "" {
		return nil, false // an option the builtin does not take raises at the builtin's own door
	}
	if fn == "sorted" {
		if len(pos) > 2 {
			return nil, false
		}
		if len(pos) == 2 {
			// A second POSITIONAL argument is this language's constant repeat flag; the reference's
			// key in that seat is the reference's TypeError, and neither is walked here (Gap R.247).
			return nil, false
		}
		if key != nil {
			// A `key=` is answered by the builtin's door beside this one (ADR 0340): the loop walks
			// whatever that door builds, so the only question here is whether the key can be called —
			// and `key=None`, which the reference reads as no key at all, is not a missing half.
			if kd := g.keyDoorOf(key); kd.pos != keyCallable && kd.pos != keyAbsent {
				return nil, false
			}
		}
	}
	kind, known := g.staticContainerKind(a)
	if !known || kind != "list" {
		// The container tables have no entry for the shapes the REFERENCE still walks element by element — a
		// TEXT (its characters), a DICT (its keys), a constant `range(...)` (its integers), and an ordering
		// handed another ordering (the sequence the inner call walked). Each is asked of the road that owns its
		// sequence rather than of a table built from the shapes this backend already knew:
		// `staticContainerKind` is a record of what a NAME holds, and an iterable the program handed the
		// ordering has no name to look up (roadmap **Gap R.247**, **ADR 0473**).
		if !g.orderingArgumentIsNameableSequence(a) {
			return nil, false
		}
	}
	if ll, isListLit := a.(*ListLit); isListLit && len(ll.Elems) == 0 {
		// An empty literal has nothing to walk; the fold beside this answers it without building
		// an object for nothing.
		return nil, false
	}
	// The element's KIND has to be visible too, not just the container's. The loop that walks this
	// handle binds its variable to one word per element; a container whose elements are of more
	// than one kind travels as a (payload, tag) pair, and the pair has no home in a single-word
	// loop variable — the road beside this would tag the name and leave it tagged for every later
	// loop in the function (measured: a float list, and a text list followed by an integer loop).
	// Declining is the honest answer: the loop's guard then refuses the program by name.
	if _, known := g.forIterSortedElemKind(a, fn == "sorted"); !known {
		return nil, false
	}
	return a, true
}

// forIterSortedElemKind asks what a `sorted`/`reversed` yields its loop (`sorts` says which), and
// answers only when the
// answer is one kind for the whole container and this backend can print that kind from a single
// word: "number" for integers, "text" for interned strings. Floats, Nones, nested containers and
// a mix of any of them need the tagged value word (roadmap L11.1, Gap R.146), and an unknown list
// — one built by `append` rather than written down — is the same door: `ok` is false for all of them.
func (g *irGen) forIterSortedElemKind(a Expr, sorts bool) (string, bool) {
	switch n := a.(type) {
	case *ListLit:
		sawText, sawNum := false, false
		for _, el := range n.Elems {
			switch el.(type) {
			case *StrLit:
				sawText = true
			case *IntLit:
				sawNum = true
			default:
				return "", false // a float, None, a container, a call: the tag road's own elements
			}
		}
		switch {
		case sawText && sawNum:
			return "", false
		case sawText:
			return "text", true
		default:
			return "number", true
		}
	case *Name:
		// A name the ordering walks may hold a TEXT (its characters), a DICT (its keys) or a LIST, and each
		// is asked of the record that owns it. The container records are asked FIRST — a name the tables
		// claim holds a container is a container's business (**ADR 0273**'s rule that a gate may not cost a
		// program that compiles) — and the text record only answers a name the front end saw hold one string.
		if g.mixedLists[n.Value] {
			return "", false
		}
		if g.listElemStr[n.Value] {
			return "text", true
		}
		if g.listVars[n.Value] {
			return "number", true
		}
		if mode, _, handled, err := g.sortedDictAnswers(n); handled {
			// A dict's elements ARE its keys, in entry order — the fact **ADR 0417** paid for by striding the
			// payload array by two and stopping at the entry count. The KIND comes from the very records that
			// road reads, so `for k in sorted(d)` and `print(sorted(d))` cannot keep two answers about one
			// dict (roadmap **Gap R.247**, **Gap R.426**). `handled` is that road's own "is this a dict";
			// asked here it keeps the two roads from keeping two lists of dict shapes.
			if err != nil {
				return "", false
			}
			if mode == 1 {
				return "text", true
			}
			return "number", true
		}
		if _, held := g.strVals[n.Value]; held {
			// A TEXT the ordering was handed yields its CHARACTERS, which is what the reference's
			// `sorted("bca")` builds a list of. The order is not spelled here — @rt_sort's text comparator
			// orders them, the same comparator a text list is ordered by — so this backend keeps one text
			// ordering rather than a compile-time copy of it that could drift (**ADR 0225**'s code-point rule).
			return "text", true
		}
		return "", false
	case *DictLit:
		// A dict literal's elements are its KEYS, in the order the program wrote the entries: the same
		// question **ADR 0417**'s sort road answers with `@rt_dict_keys`, asked here of the same records so
		// the walk and the ordering cannot disagree about what a dict yields (roadmap **Gap R.247**,
		// **Gap R.426**).
		mode, keyTag, handled, err := g.sortedDictAnswers(n)
		if !handled || err != nil {
			return "", false
		}
		// The MODE is the comparator @rt_dict_keys will be asked to run (text keys compare as texts);
		// the KEY TAG is what a slot holds, and it is what a one-entry dict's kind turns on —
		// `sorted({"a": 1})` compares nothing and still has to say what its element IS, or the binding
		// claims an integer slot over an interned text and `print(xs[0])` shows the index
		// (**ADR 0232**'s slots-are-the-truth, roadmap **Gap R.247**).
		switch {
		case mode == 1, keyTag == elemTagStr:
			return "text", true
		case keyTag == elemTagInt:
			return "number", true
		default:
			// A one-entry dict keyed by a verdict, None or a container: the sort compares nothing, but a
			// slot still has to say what it holds, and "object" is the honest word for it.
			return "object", true
		}
	case *StrLit:
		// The characters of a text, in code-point order — the list the reference builds when you sort a
		// string. `runeElems` is the road that spells them, the same one the loop's `for c in "abc"` walks.
		return "text", true
	case *Slice:
		// A slice copies its base's slots and tags, so the base answers the question. It used to be that
		// only a NUMERIC base was safe to walk, because @rt_sort chose its comparator from the ARGUMENT's
		// spelling (a Name's marker, a literal's elements) and a slice is neither — so a list of text sliced
		// into `sorted` was ordered by payload, which is arrival order, and `xs = ["b", "a"]` /
		// `print(sorted(xs[0:2]))` printed `['b', 'a']` at the exit code of success where the reference
		// answers `['a', 'b']` (and `reverse=True` printed the OTHER wrong order). The comparator is asked of
		// the OBJECT now — `sortedRuntime`'s default arm reads this very gate — which is what makes a text
		// base walkable beside a numeric one (roadmap **Gap R.247**, **ADR 0473**).
		kind, known := g.forIterSortedElemKind(n.Obj, sorts)
		if !known {
			return "", false
		}
		return kind, true
	case *Call:
		// A `range(...)` whose bounds are constants is the integer list the reference's `sorted(range(3))`
		// builds, and the gate asks the SAME road the fold asks, so the loop cannot walk a sequence the sort
		// ordered by a different rule (**ADR 0258**'s one table; roadmap **Gap R.247**).
		if nmRange, isRangeName := n.Fn.(*Name); isRangeName && !g.builtinShadowed(nmRange.Value) && nmRange.Value == "range" {
			if _, isRange := g.constRangeElems(n); isRange {
				return "number", true
			}
		}
		// A call that answers a container answers it with a literal this scan already saw; one arm
		// whose elements are visible is enough to name the kind (roadmap Gap R.67, ADR 0333). The text
		// answer is walkable now too, for the reason the slice arm above gives: the comparator is chosen
		// from the object, not from the spelling of the call.
		nm, isName := n.Fn.(*Name)
		if !isName {
			return "", false
		}
		// An ordering handed ANOTHER ordering walks the very sequence the inner call walked, so the KIND —
		// and with it @rt_sort's comparator — is the INNER argument's. Asked of the outer call's spelling
		// instead, the elements arrived as the intern table's INDICES, the numeric comparator ordered them by
		// ARRIVAL, and `print(sorted(sorted("bca")))` answered `['b', 'c', 'a']` at the exit code of success
		// where the reference answers `['a', 'b', 'c']` (roadmap **Gap R.247**, **ADR 0473**). The recursion
		// follows the same argument `foldedIterableElems` folds, so a nest cannot be ordered one way and
		// tagged another (**ADR 0258**'s one table).
		if _, inner, isOrdering := g.containerSortBuiltin(n); isOrdering {
			if inner == nil {
				return "", false
			}
			return g.forIterSortedElemKind(inner, sorts)
		}
		ans, okAns := g.containerAnswerOf(nm.Value)
		if !okAns || len(ans.arms) != 1 {
			return "", false
		}
		kind, known := g.forIterSortedElemKind(ans.arms[0], sorts)
		if !known {
			return "", false
		}
		return kind, true
	}
	return "", false
}

// containerSortHandle lowers a `sorted`/`reversed` over a walkable list to the handle of the
// heap list it denotes. The helpers are the builtin's own: @rt_sort through sortedRuntime, so
// the comparator choice — numbers by payload, interned text by its text, and the mixed-kind
// refusal — is asked of the one place that answers it (ADR 0191), and @rt_list_copy +
// @rt_reverse for the reversed copy. Neither builtin may touch the sequence it was given:
// `sorted(xs)` leaves xs in its own order and `reversed(xs)` does not consume it, so both work
// on a copy.
func (g *irGen) containerSortHandle(b *strings.Builder, e Expr) (string, bool) {
	fn, arg, isCall := g.containerSortBuiltin(e)
	if !isCall {
		return "", false
	}
	if _, ok := g.sortedReversedOverWalkableList(e); !ok {
		return "", false
	}
	if fn == "sorted" {
		c, _ := e.(*Call)
		h, err := g.sortedRuntime(b, c)
		if err != nil {
			return "", false
		}
		return h, true
	}
	// `reversed` over a DICT flips its KEYS, which is the one helper the dict's own sort road already uses
	// to ask them: `@rt_dict_keys` strides the entry array by two and stops at the entry count, and hands
	// back a FRESH list, which is what `@rt_reverse` may flip in place. Copying the dict itself — what this
	// road did before the question was asked — would flip keys and values together, the defect **ADR 0417**
	// closed on the sort side (roadmap **Gap R.247**, **Gap R.426**).
	if _, keyTag, handled, derr := g.sortedDictAnswers(arg); handled && derr == nil {
		dv, kerr := g.containerOperand(b, arg)
		if kerr != nil {
			return "", false
		}
		g.heapSeq++
		keys := fmt.Sprintf("%%h%d", g.heapSeq)
		// The helper is the sort road's own question — a dict's KEYS, in entry order, into a fresh list —
		// asked with the keys' own tag, so the flip and the ordering read the same array (**ADR 0417**).
		g.emitHeapHelperCall(b, keys, "rt_dict_keys", "i32 "+dv+", i32 "+fmt.Sprint(keyTag), Span{})
		b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %s)\n", keys))
		g.heapUsed = true
		return keys, true
	}
	// A TEXT and a constant `range(...)` are the sequences the reference builds a list from before it
	// flips it; `orderingSequenceArg` spells that list, and the copy is made from it rather than from the
	// iterable, which has no container handle to copy.
	if seq, isSeq := g.orderingSequenceArg(arg); isSeq {
		arg = seq
	}
	sv, err := g.containerOperand(b, arg)
	if err != nil {
		return "", false
	}
	g.heapSeq++
	// Handle registers are named %h<N> off heapSeq the way sortedRuntime names its copy: a
	// %t<N> name reads as an ordinary value to the print and call lowerings, which would then
	// printf a handle as a number (the bug that comment was written for).
	slot := fmt.Sprintf("%%h%d", g.heapSeq)
	g.emitHeapHelperCall(b, slot, "rt_list_copy", "i32 "+sv, Span{})
	b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %s)\n", slot))
	return slot, true
}

// materializeSortedReversedForLoop is the loop's SECOND question: not "can the compiler write the
// container down" (the fold above) but "can the compiler NAME the container the builtin denotes".
// `sorted(xs)`, `reversed(xs)`, `sorted([3, 1, 2][0:2])`, `sorted(b())` — the builtin's own helpers
// build the object (@rt_sort for the order, @rt_list_copy + @rt_reverse for the flipped copy), it is
// rooted in a slot, and the loop walks that slot down the road it has always used for a list
// variable.
//
// Naming it rather than handing the loop a bare handle is the point (ADR 0333's pattern, applied to
// the two builtins whose job is to hand back a list): the slot carries its element kind in the same
// notebooks a `xs = [...]` assignment fills, so `print(v)` renders text as text and numbers as
// numbers WITHOUT tagging the loop variable. Tagging it — which is what the generic handle arm of the
// dispatch does, for the slot-reached-through-a-handle case — left the name tagged for every later
// loop in the function: `for v in sorted(words)` followed by `for v in [7, 8]` printed `(null)` twice,
// and the same variable used as a number refused with Gap R.146's sentence (roadmap Gap R.246, ADR 0339).
func (g *irGen) materializeSortedReversedForLoop(b *strings.Builder, e Expr) (Expr, bool) {
	arg, ok := g.sortedReversedOverWalkableList(e)
	if !ok {
		return nil, false
	}
	h, okHandle := g.containerSortHandle(b, e)
	if !okHandle {
		return nil, false
	}
	fkBuiltin, _, _ := g.containerSortBuiltin(e)
	elemKind, _ := g.forIterSortedElemKind(arg, fkBuiltin == "sorted")
	g.tmp++
	name := fmt.Sprintf("__srt%d", g.tmp)
	g.allocd[name] = true
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", "%_"+name))
	g.gcReg(b, name)
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", h, "%_"+name))
	g.listVars[name] = true
	if elemKind == "text" {
		g.listElemStr[name] = true
	} else {
		g.listElemInt[name] = true
	}
	return &Name{Value: name, Src: e.Span()}, true
}

// forIterCallIsGenerator is the `*Call` arm's OWN condition in the loop's dispatch: a call that
// yields a sequence is walked by the runtime road. The arm used to match every call and then set
// nothing, which swallowed `sorted(xs)`, `reversed(xs)` and `sorted(b())` before the container
// pair below could be asked — the three shapes that answered `0`, nothing and nothing at the exit
// code of success (roadmap Gap R.246, ADR 0339).
func (g *irGen) forIterCallIsGenerator(c *Call) bool {
	// Two spellings hand back a generator's buffer: `g()`, whose body the module walk registered, and
	// `recv.m(...)`, whose body the CLASS door registered (roadmap **Gap R.455**, **ADR 0432**). The arm used
	// to ask only the first, so a method generator fell through the dispatch to the numeric-range road and
	// walked a counter while printing the loop variable — `for v in c.m():` printed `0 1` for `1 2`, at the
	// exit code of success.
	return g.callIsGeneratorCall(c)
}

// forIterableIsUnwalkableContainer is the loop's last question before it falls to the
// numeric-range road: is the iterable a container this backend cannot step over? A `for` whose
// body prints the loop variable answers at exit 0 whether it walked anything or not, so a
// container whose elements no road can reach must be REFUSED by name: an answer the program
// never wrote is the answer this contract ranks worst, and exit 2 — what these shapes did
// before, with `llc` holding the evidence — is the second worst (ADR 0166).
//
// The question is asked of the two builtins whose whole job is to hand back a container
// (`sorted`, `reversed`), over an argument this backend cannot walk; every other iterable is
// left to the roads beside this one, each of which answers for its own kind.
func (g *irGen) forIterableIsUnwalkableContainer(e Expr) bool {
	if _, _, isSort := g.containerSortBuiltin(e); !isSort {
		return false
	}
	if _, ok := g.foldedIterableElems(e); ok {
		return false // the fold above already gave the loop its elements
	}
	_, ok := g.sortedReversedOverWalkableList(e)
	return !ok
}

// forIterableMissingHalf names, for THIS program, the half of the door that is missing. A refusal
// that only says "not supported" is not actionable (ADR 0249), and one that blames the wrong cause
// sends the reader to the wrong row — so the sentence is chosen by asking the iterable what it is:
// a callable argument, an argument that is not a list, elements of a kind no comparator reaches, or
// a container inside a call the whole-program scan cannot see every arm of.
func (g *irGen) forIterableMissingHalf(e Expr) string {
	fn, arg, isCall := g.containerSortBuiltin(e)
	if !isCall {
		return "its iterable is not a container this backend can build"
	}
	c, _ := e.(*Call)
	// The argument the builtin does not take: a `key=` whose callable this backend cannot call,
	// and the reference's positional key, which the reference itself stops (roadmap Gap R.249,
	// paid for the callable half by ADR 0340 — a key the door CAN call is not the missing half).
	for _, a := range c.Args[1:] {
		if kw, isKw := a.(*KeywordArg); isKw && kw.Name != "reverse" {
			if kw.Name == "key" {
				if kd := g.keyDoorOf(kw.Value); kd.pos == keyCallable {
					continue // the key is callable; the half that is missing is asked about below
				}
			}
			return "its " + kw.Name + "= argument names a callable this backend cannot build a call to, which is the value word's door (roadmap Gap R.249)"
		}
		if _, isPos := c.Args[1].(*KeywordArg); !isPos && len(c.Args) > 1 {
			return "its second POSITIONAL argument is the reference's key, which the reference itself refuses: sorted() takes one positional argument (roadmap Gap R.249)"
		}
	}
	// What kind of thing the argument is. A TEXT, a DICT, a constant `range(...)` and an ordering handed
	// another ordering are sequences the door walks (**Gap R.247**, **ADR 0473**), so a refusal about one of
	// them is never about its being one — it is about the ELEMENT the loop variable would have to hold, and
	// the switch below says which. What is left over is an argument no door can put in front of the ordering
	// at all: a generator, a number, a SET, a call the scan cannot see every arm of.
	// A `range(...)` whose bound the program did not write is its own missing half: the fold that would
	// spell its integers has no constant to read, and the value road keeps no table for a builtin call's
	// answer (roadmap Gap R.250 — not the row that opened the text, the dict and the written-out range).
	if rc, isRangeCall := arg.(*Call); isRangeCall {
		if nm, isName := rc.Fn.(*Name); isName && !g.builtinShadowed(nm.Value) && nm.Value == "range" {
			if _, isConst := g.constRangeElems(rc); !isConst {
				return "its argument is a `range(...)` whose bound the program did not write, and the road" +
					" that spells a range's integers has no constant to read (roadmap Gap R.250)"
			}
		}
	}
	kind, known := g.staticContainerKind(arg)
	if !known && !g.orderingArgumentIsNameableSequence(arg) {
		return "its argument is not a sequence this backend can put in front of @rt_sort / @rt_reverse: a list" +
			" (literal, variable or slice), a TEXT (whose characters it sorts), a DICT (whose KEYS it sorts), a" +
			" `range(...)` whose bounds the program wrote, and an ordering handed another ordering are what the" +
			" compiled backend walks; anything else needs the value word that carries its own kind (roadmap Gap R.247)"
	}
	switch kind {
	case "set":
		return "its argument is a SET, whose members the runtime keeps in its own table and @rt_sort" +
			" does not order (roadmap Gap R.247)"
	}
	// A list whose ELEMENTS no comparator reaches, or whose kind the compiler cannot see.
	if _, okKind := g.forIterSortedElemKind(arg, fn == "sorted"); !okKind {
		if ll, isLit := arg.(*ListLit); isLit {
			for _, el := range ll.Elems {
				switch el.(type) {
				case *FloatLit, *BoolLit, *NoneLit, *ListLit, *DictLit, *SetLit:
					return "its elements are not all integers or all text: @rt_sort orders integers by" +
						" payload and interned text by its text, and a float, a verdict, None or a nested" +
						" container travels as a tagged pair this loop variable cannot hold (roadmap Gap R.248)"
				}
			}
			return "the kind of its elements is more than this backend's sort orders (roadmap Gap R.248)"
		}
		if _, isName := arg.(*Name); isName {
			return "the ELEMENT kind of that list is not something this backend's roads print from one" +
				" word — a mixed list, a float list or a list built by `append` needs the tag the value" +
				" word carries (roadmap Gap R.248)"
		}
		if fn == "sorted" {
			// The comparator is asked of the OBJECT since this row — the argument's spelling no longer decides
			// it — so what is left for a call-shaped argument to be missing is the element kind itself. Say that,
			// and say what the alternative answer was: @rt_sort handed elements of no kind orders them by payload
			// alone, which for text is ARRIVAL order, the answer this contract ranks worst (roadmap **Gap R.247**,
			// **ADR 0473**).
			return "the sequence behind it is one whose elements no door can name, and @rt_sort handed elements of" +
				" no kind would order them by payload alone — which for text is ARRIVAL order, the answer this" +
				// contract ranks worst (roadmap Gap R.247, ADR 0473)
				" backend refuses rather than prints"
		}
		return "the container inside it is the answer of a call this backend's scan cannot see every" +
			" arm of, so the kind of its elements is not known (roadmap Gap R.250)"
	}
	return "its container is one this backend cannot put in front of @rt_sort / @rt_reverse (roadmap Gap R.247)"
}

// forIterableRefusal names the iterable, says which half of the door this program is missing, and
// says what the alternative answer would have been. The three are one sentence on purpose: a refusal
// that omits the third reads like a limitation of the language rather than a choice the backend made
// against a worse one (ADR 0249, ADR 0166).
func (g *irGen) forIterableRefusal(e Expr) string {
	surface := exprSurface(e)
	half := g.forIterableMissingHalf(e)
	return "`for` over " + surface + " is refused: this backend steps over a container it can" +
		" name (a literal, unrolled element by element) or one the runtime can measure (a heap" +
		" list built by @rt_sort / @rt_reverse), and " + surface + " is a container neither door" +
		" reaches — " + half + ". Reading its word as a repeat count would answer at exit 0 with a" +
		" number the program never wrote, which is the answer this contract ranks worst; the" +
		" reference asks the value what it is and steps over it, and that question needs the value" +
		" word that carries its own kind (roadmap L11.1, ADR 0339)"
}
