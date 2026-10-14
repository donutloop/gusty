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
		return nil, false
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
		if g.mixedLists[n.Value] {
			return "", false
		}
		if g.listElemStr[n.Value] {
			return "text", true
		}
		if g.listVars[n.Value] {
			return "number", true
		}
		return "", false
	case *Slice:
		// A slice copies its base's slots and tags, so the base answers the question — except that
		// @rt_sort chooses its comparator from the ARGUMENT's spelling (a Name's marker, a literal's
		// elements) and a slice is neither: text sliced into `sorted` would be ordered by payload,
		// which is arrival order. Only the numeric base is safe to walk; the text case is refused
		// beside this and filed with the comparator's own row (roadmap Gap R.247).
		kind, known := g.forIterSortedElemKind(n.Obj, sorts)
		if !known || (sorts && kind != "number") {
			return "", false
		}
		return kind, true
	case *Call:
		// A call that answers a container answers it with a literal this scan already saw; one arm
		// whose elements are visible is enough to name the kind (roadmap Gap R.67, ADR 0333).
		// As above: @rt_sort's comparator is not chosen for a call's answer, so text is refused.
		nm, isName := n.Fn.(*Name)
		if !isName {
			return "", false
		}
		ans, okAns := g.containerAnswerOf(nm.Value)
		if !okAns || len(ans.arms) != 1 {
			return "", false
		}
		kind, known := g.forIterSortedElemKind(ans.arms[0], sorts)
		if !known || (sorts && kind != "number") {
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
	fn, ok := c.Fn.(*Name)
	return ok && g.genFuncs[fn.Value]
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
	// What kind of thing the argument is.
	kind, known := g.staticContainerKind(arg)
	if !known {
		switch arg.(type) {
		case *StrLit:
			return "its argument is a TEXT, which the reference sorts by building a list of one-character strings, and the sort that would order text needs the tag the value word carries (roadmap Gap R.247)"
		}
		return "its argument is not a container this backend can name: only a list literal, a list" +
			" variable, a numeric slice and a call that answers a list can be put in front of @rt_sort /" +
			" @rt_reverse (roadmap Gap R.247)"
	}
	switch kind {
	case "dict":
		return "its argument is a DICT: sorted() would order its KEYS, which the runtime stores as" +
			" [key, value] pairs the sort does not walk (roadmap Gap R.247)"
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
			return "@rt_sort chooses its comparator from the argument's spelled kind (a list variable's" +
				" marker or a literal's elements), and this argument is neither, so text handed to it would" +
				" be ordered by payload — that is, by arrival (roadmap Gap R.247)"
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
