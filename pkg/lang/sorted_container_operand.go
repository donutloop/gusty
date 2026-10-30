// The container question the doors BESIDE the loop ask of a `sorted`/`reversed` call (roadmap
// **Gap R.253**, **ADR 0472**).
//
// ADR 0333 established the shape of a container answer: a door first DECIDES that an expression
// denotes a heap container, and then BUILDS it, and the two halves must be the same function —
// `isContainerExpr` beside `containerOperand`, `containerHandleOf` beside both. ADR 0339 then gave
// the `for` loop two questions to ask of a `sorted`/`reversed`: can the compiler WRITE the
// container down (the fold, sharing `sortedLiteralElems` with the `print` answer), and if not, can
// it NAME the container the builtin denotes (the copy `@rt_sort` built, or `@rt_list_copy` +
// `@rt_reverse` flipped). The loop asked; the doors beside it never did.
//
// What they did instead is what this file is for. `g.value(sorted([3, 1, 2]))` is answered by the
// builtin's literal road, which folds the ordered elements into a compile-time CONSTANT —
// `@.lst1 = private global {i32, [3 x i32]} { i32 3, [3 x i32] [i32 1, i32 2, i32 3] }` — and hands
// back that global's name. Every door that reads the word as a *handle* and feeds it to a runtime
// helper therefore emits a global in an i32 slot, which `llc` refuses:
//
//	print(1 in sorted([3, 1, 2]))          %t2 = call i32 @rt_contains(i32 @.lst1, i32 1)
//	print(2 in reversed([1, 2, 3]))        %t2 = call i32 @rt_contains(i32 @.lst1, i32 2)
//	print(sorted([3,1,2]) == sorted([1,2,3]))   %t1 = icmp eq i32 @.lst1, @.lst2
//
// three programs CPython answers `True`, `True`, `True` — each of them **exit 2**, the code ADR
// 0166 reserves for a compiler bug. A fourth family refuses at exit 1 instead, for the same missing
// question: `print(sorted([3, 1, 2]) == [1, 2, 3])` reaches ADR 0189's "comparing a container with
// *lang.Call needs a tagged value", because `isContainerExpr` cannot see that the call on the other
// side of the `==` IS a container.
//
// The repair is the same instruction ADR 0234 applied to a comprehension's folded global at the
// membership door (`rt_contains(i32 @.set1, i32 2)`) and ADR 0335 to a slice at the comparison door:
// ASK THE OBJECT, and where the answer is a constant, BUILD the object the constant denotes. The
// build goes through the roads that already own it — the literal arms of `containerOperand` for the
// fold, `@rt_sort`/`@rt_list_copy`+`@rt_reverse` for the copy — so this file adds no builder, no
// comparator and no ordering of its own: the order comes from `sortedLiteralElems`, the one table
// the `print` answer and the loop already share (**ADR 0258**), which is what keeps `print` and `in`
// from disagreeing about which end of the list is first.
//
// The file answers FOUR questions, and a value door asks all four through `sortedReversedDoorIsAskedOf`, which is
// build-OR-REFUSE rather than build alone:
//
//   - `sortedReversedFoldIsAContainer` — can the compiler WRITE the container down?
//   - `sortedReversedContainerIsNameable` — can it NAME the container the runtime builds? BOTH halves of ADR 0333's
//     pair at once: ADR 0339's walkability gate and the loop's own element-kind question
//     (`sortedReversedContainerElemKind` → `forIterSortedElemKind`). A door asking only the first would claim a
//     container whose slots carry a tag its elements do not have — a dict's walked elements are its KEYS while the
//     tag road reads its VALUES.
//   - `sortedReversedIsBeyondTheDoor` — the builtin knows the call, the argument is walkable, and yet nothing can be
//     built: today one spelling, `sorted(xs, True)`, whose second POSITIONAL argument is the reference's `key` and
//     which CPython therefore stops. The value road still FOLDS such a call, so a door that read its word would put
//     a module global in a register slot — this row's exit-2 class, on a program that should never run. It is
//     REFUSED, in one sentence, at all six roads that could store such an element (`in`, `==`, a list slot, a set
//     element, `append`, a dict value), each of which asks `sortedReversedRefusalInside` BEFORE an unrelated gate
//     can misdescribe the shape (**ADR 0249**). A refusal above a miscompile is **ADR 0166**'s order; the option
//     itself is **Gap R.247**/**Gap R.249**.
//   - `reversedIsAnIterator` — the object `reversed` returns is an ITERATOR, which the loop and a haystack may walk
//     but a comparison may not compare (**Gap R.195**, **Gap R.251**).
//
// The `for` loop is deliberately NOT a caller of the refusal: it keeps ADR 0339's own gate, whose sentence for that
// same call is pinned by **Gap R.249**, and `containerHandleOf` answers `ok` rather than handing back a half-named
// handle — a road that accepted a refusing door would walk an empty register against the fold's global.
//
// What this file deliberately does NOT do:
//
//   - it answers only where the builtin's OWN road answers. `reversed` over a TEXT folds to an
//     interned string, not to a container, so membership over it stays the text road's question —
//     and that question is wrong today (`"a" in reversed("abc")` answers `False` where the reference
//     answers `True`, an element-wise question the substring road cannot ask), filed as **Gap R.568**
//     rather than half-fixed here by a door that would then disagree with `print`.
//   - it does not sort a list whose elements the printer cannot name from one word: floats, Nones,
//     nested containers and mixes are declined by `forIterSortedElemKind`, the loop's own gate. The
//     shape those take is unchanged — including `1 in sorted([1.5, 0.5])`, which answers wrongly
//     today for the comparator **Gap R.248** owns. Widening the door there would have turned a
//     documented wrong answer into an undocumented one.
//   - it never emits the fold. The fold stays the builtin's answer for a value position; this file
//     only declines to hand that constant to a helper that reads it as a handle.
package lang

import (
	"fmt"

	"strings"
)

// sortedReversedSlotsAreTaggable is ADR 0457's condition on the element door, read of the object this door
// builds: every slot must be able to carry a tag beside its payload, which means the compiler must be able to
// DESCRIBE what the slots hold. For the fold that is the literal whose elements it ordered, asked of the same
// question a `xs = [...]` binding asks; for a nameable container it is the one printable kind
// `forIterSortedElemKind` named for the whole container — a container whose elements are of more than one kind
// is declined there, and stays declined here (**ADR 0187**'s rule that no builder writes a payload without its
// tag; roadmap **Gap R.253**, **ADR 0472**).
func (g *irGen) sortedReversedSlotsAreTaggable(e Expr) bool {
	if lit, ok := g.sortedReversedFoldIsAContainer(e); ok {
		return g.taggableNestedElem(lit)
	}
	// The refusal clause of the decide door: a slot holding this shape goes through the BUILD half,
	// which refuses in words, rather than being stored untagged with the fold's global inside it.
	if g.sortedReversedIsBeyondTheDoor(e) {
		return true
	}
	fn, arg, isSort := g.containerSortBuiltin(e)
	if !isSort {
		return false
	}
	if _, ok := g.sortedReversedOverWalkableList(e); !ok {
		return false
	}
	_, ok := g.forIterSortedElemKind(arg, fn == "sorted")
	return ok
}

// reversedIsAnIterator names the object `reversed` actually returns: an ITERATOR. The two doors that walk
// elements (the `for` loop, the membership haystack) are right to walk the flipped copy — that is what the
// iterator yields, element by element — but the doors that compare or match a VALUE must not treat the copy
// as the answer: the reference's iterator has no `__eq__`, so it compares by identity and is equal to no
// list, and two calls to `reversed` are two objects (**Gap R.251**, **Gap R.195**, **ADR 0472**).
func (g *irGen) reversedIsAnIterator(e Expr) bool {
	fn, _, isSort := g.containerSortBuiltin(e)
	if !isSort || fn != "reversed" {
		return false
	}
	return g.sortedReversedIsAContainer(e)
}

// iteratorEqualsAContainer is the equality door's answer for an iterator beside a container: the operands
// have already been lowered by the caller — an element that prints still prints — and the verdict is the
// reference's, which is that the iterator never equals the container it appears to mirror. `!=` is the
// other half of the same sentence. The rows that read it are `print(reversed([1, 2]) == [2, 1])` (False,
// where comparing the flipped copy printed True) and `print(reversed([1, 2]) == reversed([1, 2]))` (False,
// where comparing two copies printed True).
func (g *irGen) iteratorEqualsAContainer(op string) string {
	if op == "!=" {
		return "1"
	}
	return "0"
}

// iteratorIsNoMember is the membership door's answer for an iterator standing where the NEEDLE goes: the
// reference matches it by identity against the haystack's own slots, so it is a member of nothing — not
// even of the container holding the very elements it yields. The haystack and the copy are built first, so
// an operand that acts still acts; only the verdict is decided here (**Gap R.251**, **ADR 0472**).
func (g *irGen) iteratorIsNoMember(op string) string {
	if op == "not in" {
		return "1"
	}
	return "0"
}

// sortedReversedFoldIsAContainer is the DECIDE half, for the shape the compiler can WRITE down: a
// `sorted` over a list literal the literal road orders, or a `reversed` over a list literal the
// literal road flips. It answers the literal the builtin would have emitted — ordered, or flipped —
// so the caller builds the same elements in the same order from one table (**ADR 0258**).
//
// The conditions are `builtinSorted`'s and `builtinReversed`'s own, read rather than restated:
// `sortedLiteralElems` for the order (which is also what declines a `key=`, a bare positional second
// argument and a non-constant `reverse=`), and one argument for `reversed`. That is why a call this
// door answers is exactly a call whose value-position fold exists: `print(sorted(sorted([3, 1, 2])))`
// keeps refusing — as it does today — because the builtin's road cannot fold a call, and a door that
// answered where the builtin refuses would be a second answer to one question.
func (g *irGen) sortedReversedFoldIsAContainer(e Expr) (*ListLit, bool) {
	fn, arg, isSort := g.containerSortBuiltin(e)
	if !isSort {
		return nil, false
	}
	c, isCall := e.(*Call)
	if !isCall {
		return nil, false
	}
	ln, isListLit := arg.(*ListLit)
	if !isListLit {
		return nil, false
	}
	if fn == "sorted" {
		if len(c.Args) < 1 || len(c.Args) > 2 {
			return nil, false // sorted() takes the iterable and its own options; the rest is the builtin's sentence
		}
		// The builtin's own gate, read rather than restated: a second POSITIONAL argument is the
		// reference's `key`, and the reference stops the program on it, so this door declines it too
		// (**reverseFlagIsSpellable** is **ADR 0339**'s gate, shared with the loop's fold — one
		// question, one asking place, ADR 0232). Answering it here would let the LOOP walk a container
		// its own road refuses, which is the answer pinned for **Gap R.249**.
		if !reverseFlagIsSpellable(c.Args[1:]) {
			return nil, false
		}
		elems, folded, ferr := g.sortedLiteralElems(ln.Elems, c.Args[1:])
		if ferr != nil || !folded {
			return nil, false
		}
		return &ListLit{Elems: elems, Src: ln.Span()}, true
	}
	if len(c.Args) != 1 {
		return nil, false
	}
	// reversed()'s fold is `emitList` over the flipped elements, and `emitList` writes one i32 word
	// per slot: an element that is not an integer literal is the builtin's own refusal, and this door
	// declines with it rather than building an object the builtin could not emit.
	for _, el := range ln.Elems {
		if _, isInt := el.(*IntLit); !isInt {
			return nil, false
		}
	}
	return &ListLit{Elems: reversedExprs(ln.Elems), Src: ln.Span()}, true
}

// sortedReversedContainerIsNameable is the DECIDE half for the shape the compiler can only NAME: a
// `sorted`/`reversed` over a list the runtime walks — a list variable, a numeric slice, a call that
// carries a list — whose elements are of one kind the printer can name. It is the loop's second
// question, asked through the loop's own gate (**ADR 0339**'s `sortedReversedOverWalkableList`) TOGETHER
// with the half of the pair the loop asks beside it (**`forIterSortedElemKind`**), so the two doors
// cannot disagree about whether the container exists — and a door can never claim a container whose
// ELEMENT has no tag road, which is **Gap R.248**'s case and the loop's own refusal.
//
// The kind half is not decoration. The walk can name a payload road for a dict literal, but the elements
// a dict walk yields are its KEYS while the tag road reads a dict literal's VALUES: building that pair
// would store a `number` tag over text slots. The loop knew this; the doors learning it is the same
// single asking place (ADR 0232), not a second rule.
func (g *irGen) sortedReversedContainerIsNameable(e Expr) bool {
	if _, ok := g.sortedReversedOverWalkableList(e); !ok {
		return false
	}
	_, ok := g.sortedReversedContainerElemKind(e)
	return ok
}

// sortedReversedContainerElemKind is the tag half of the pair, asked of the ARGUMENT the elements come
// from. `sorted` asks it as a sort does — @rt_sort chooses its comparator from the argument's spelling,
// so text arriving through a slice or a call is refused — and `reversed` asks it as a walk does, because
// nothing is compared. This is the loop's own question (`forIterSortedElemKind`), asked once.
func (g *irGen) sortedReversedContainerElemKind(e Expr) (string, bool) {
	c, isCall := e.(*Call)
	if !isCall || len(c.Args) == 0 {
		return "", false
	}
	fn, _, isBuiltin := g.containerSortBuiltin(e)
	if !isBuiltin {
		return "", false
	}
	return g.forIterSortedElemKind(c.Args[0], fn == "sorted")
}

// sortedReversedIsAContainer is the ONE decide question the membership, equality, truthiness and
// length doors ask: is this expression the container that `sorted`/`reversed` denotes? Ask it with
// `containerOperand`/`containerHandleOf`, never on its own — the pair is ADR 0333's rule, and a door
// that decides without asking the builder is how a fold global reaches an i32 parameter.
func (g *irGen) sortedReversedIsAContainer(e Expr) bool {
	if _, ok := g.sortedReversedFoldIsAContainer(e); ok {
		return true
	}
	return g.sortedReversedContainerIsNameable(e)
}

// sortedReversedIsBeyondTheDoor is the refuse-rather-than-pretend question: the builtin recognises
// the call, its argument IS a list the doors could build, and yet neither build half can make an
// object of it — today one spelling, `sorted(xs, True)`, whose second POSITIONAL argument is the
// reference's `key` and which the reference therefore stops the program on. The value road still
// FOLDS such a call to its `@.lstN` constant, so a door that read its word would put a module global
// in a register slot: this row's own exit-2 class, on a program that should never run. The doors
// refuse it in words through `sortedReversedContainerOf` (**ADR 0166**: a refusal above a miscompile;
// the option door itself is **Gap R.247**/**Gap R.249**).
//
// Deliberately NOT the shapes the builtin's own door declines — a dict, a number, a mixed or float
// literal keep their existing sentences, which the tables beside **Gap R.247**/**Gap R.248** pin — so
// widening these doors cannot re-word another row's refusal.
func (g *irGen) sortedReversedIsBeyondTheDoor(e Expr) bool {
	c, isCall := e.(*Call)
	if !isCall {
		return false
	}
	if _, ok := g.sortedReversedFoldIsAContainer(e); ok {
		return false
	}
	fn, arg, isBuiltin := g.containerSortBuiltin(e)
	if !isBuiltin {
		return false
	}
	// The argument is one the doors could build an object from — a list the program wrote down, or a
	// walkable name or slice whose element kind a tag road can name. Anything else is the builtin's own
	// door to decline, in its own words (`reversed(1, 2)` keeps `reversed requires a list`).
	switch arg.(type) {
	case *ListLit:
	default:
		if _, okKind := g.forIterSortedElemKind(arg, fn == "sorted"); !okKind {
			return false
		}
	}
	if fn == "sorted" {
		if len(c.Args) < 2 {
			return false
		}
		// Only a POSITIONAL second argument is the reference's `key`. A KEYWORD option is the builtins' own
		// door to read (ADR 0340 answers `reverse=` and a callable `key=`), and this door must not steal it
		// — `print(1 in sorted([3, 1, 2], key=abs))` answers, and a sentence about a positional argument
		// spoken of a keyword call is the lie ADR 0249 counts as worse than no refusal.
		if _, isKeyword := c.Args[1].(*KeywordArg); isKeyword {
			return false
		}
		return !reverseFlagIsSpellable(c.Args[1:])
	}
	return len(c.Args) != 1
}

// sortedReversedDoorIsAskedOf is the question a VALUE door asks: is this operand an ordering builtin
// the doors must handle — either because they can build it, or because they must refuse it? The pair
// rule (**ADR 0333**) is what makes both answers necessary: a door that decided "container" only for
// the shapes it can build would walk past the third shape and read the fold's global, which is this
// row's exit-2 class. The LOOP does not ask this question — it keeps **ADR 0339**'s own gate, whose
// refusal for the positional spelling is pinned by **Gap R.249** — and asks only the fold/nameable
// pair, so widening the doors could not cost that program its refusal.
func (g *irGen) sortedReversedDoorIsAskedOf(e Expr) bool {
	return g.sortedReversedIsAContainer(e) || g.sortedReversedIsBeyondTheDoor(e)
}

// sortedReversedDoorRefusal is the ONE sentence the container doors speak for such an argument, so a
// program cannot get one message from `in` and another from `==` (**ADR 0232**'s one question, and
// **ADR 0249**'s rule that a refusal names the half that is missing).
func (g *irGen) sortedReversedDoorRefusal(e Expr) error {
	c, isCall := e.(*Call)
	fn, _, _ := g.containerSortBuiltin(e)
	if isCall && fn == "sorted" && len(c.Args) >= 2 && !reverseFlagIsSpellable(c.Args[1:]) {
		return fmt.Errorf("codegen: %s is refused: its second POSITIONAL argument is the reference's key, which the "+
			"reference itself stops the program on (roadmap L11.1, Gap R.247, Gap R.249, ADR 0340), and the container "+
			"doors will not read the builtin's folded word as a handle — a container global in a word is a compiler "+
			"bug, not a program (Gap R.253, ADR 0166)", exprSurface(e))
	}
	return fmt.Errorf("codegen: %s is refused: the container doors cannot build it from the argument the program wrote, "+
		"and reading the builtin's folded word instead would put a container global in a word (roadmap L11.1, Gap R.247, "+
		"Gap R.248, Gap R.253, ADR 0166)", exprSurface(e))
}

// sortedReversedRefusalInside is a container literal's FIRST question: does one of my elements hold an
// ordering call that the container doors must refuse?
//
// The literal roads ask their gates — the mixed-element-kind gate, roadmap Gap J.6 — before they lower an
// element, so without this question a program holding `sorted(xs, True)` in a slot is told its LIST "holds
// either strings or numbers, not both": a sentence about the wrong thing, spoken at the wrong door, about a
// call whose second positional argument is the reference's `key`. **ADR 0249**'s rule that a refusal names
// the missing half is what makes this the same sentence everywhere (roadmap **Gap R.253**, **ADR 0472**).
func (g *irGen) sortedReversedRefusalInside(e Expr) error {
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if err := g.sortedReversedRefusalInside(el); err != nil {
				return err
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if err := g.sortedReversedRefusalInside(el); err != nil {
				return err
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if err := g.sortedReversedRefusalInside(k); err != nil {
				return err
			}
		}
		for _, v := range n.Vals {
			if err := g.sortedReversedRefusalInside(v); err != nil {
				return err
			}
		}
	case *Index:
		// A slot read of a container is a container the doors may have to build, spelled as a read.
		if g.sortedReversedIsBeyondTheDoor(n) {
			return g.sortedReversedDoorRefusal(n)
		}
	case *Call:
		if g.sortedReversedIsBeyondTheDoor(n) {
			return g.sortedReversedDoorRefusal(n)
		}
	}
	return nil
}

// sortedReversedContainerOf is the BUILD half: the handle of the container the two builtins denote,
// made by the roads that already build it. The fold's elements go to `containerOperand`'s literal
// arms — the same builders a `xs = [1, 2, 3]` assignment fills, tags and all (**ADR 0187**) — and a
// nameable container to the builtin's own helpers through `containerSortHandle`, which copies before
// it sorts or flips because neither builtin may touch the sequence it was given.
//
// ok is false for every shape the two builtins themselves decline, and the caller's refusal then
// stands. An error is a real failure to build (a slot with no representation), not a refusal; it is
// never a fold global handed to a helper, which is what this door used to be.
func (g *irGen) sortedReversedContainerOf(b *strings.Builder, e Expr) (string, bool, error) {
	if lit, ok := g.sortedReversedFoldIsAContainer(e); ok {
		h, err := g.containerOperand(b, lit)
		if err != nil {
			return "", false, err
		}
		g.heapUsed = true
		return h, true, nil
	}
	if !g.sortedReversedContainerIsNameable(e) || g.sortedReversedIsBeyondTheDoor(e) {
		if g.sortedReversedIsBeyondTheDoor(e) {
			return "", false, g.sortedReversedDoorRefusal(e)
		}
		return "", false, nil
	}
	h, ok := g.containerSortHandle(b, e)
	if !ok {
		// The gate said the pair could be named and the builder disagreed: the only way that happens is
		// an argument the builtin's own road declines to order — and the word the builtin would have
		// answered with is a fold global. Say the refusal; never emit the global (**ADR 0166**).
		if g.sortedReversedIsBeyondTheDoor(e) {
			return "", false, g.sortedReversedDoorRefusal(e)
		}
		return "", false, nil
	}
	g.heapUsed = true
	return h, true, nil
}
