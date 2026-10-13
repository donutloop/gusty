package lang

import (
	"fmt"
	"strings"
)

// A list literal is read by the door a list variable's element is read by.
//
// A subscript has two roads in this backend. When the compiler can read the whole thing — a literal whose
// elements are all integer constants, and an integer position — the read is a compile-time lookup and the
// program gets the number. When it cannot, the read is a *message to the object*: the container is built in
// the heap, each element's kind is written beside its payload as the builder runs, and the read answers a
// (payload, tag) pair. The second road is the one a list **variable** has always taken, and it is the one
// that knows what an element is.
//
// So the four shapes whose element is not a plain integer answered the payload instead of the value, at exit
// 0:
//
//	print(["a", "b"][0])   # `a` in the reference; the program printed 0  — an index into @str_tab
//	print([1.5, 2][0])     # 1.5 in the reference; the program printed 1  — a @float_box handle
//	print([None, 2][0])    # None in the reference; the program printed 0
//	print([True, 2][0])    # True in the reference; the program printed 1
//
// while the same element read off a variable (`xs = ["a", "b"]` / `print(xs[0])`) printed `a`, and the
// literal road was already right for the elements that carry a tag wherever they sit —
// `print([[1, 2]][0])` is `[1, 2]` and `print([{"a": 1}][0])` is `{'a': 1}`. A literal is not a different
// kind of list; it is a list the compiler happens to be looking at (roadmap Gap R.218, ADR 0331 — the same
// asymmetry ADR 0327 closed on the dict side, one container over).
//
// The door already existed. `runtimeSlotPairDeep` in `heapargs.go` grew a `*DictLit` arm in ADR 0327 and
// never grew the list one beside it, so a list literal was simply never admitted, and the fold — which has
// one word and nowhere to put a kind — stayed in charge of shapes it cannot read. This file is that arm plus
// the gate that decides what the fold may still answer.
//
// The split is ADR 0327's and it is deliberate, not incidental:
//
//   - a position that reads a value **by its kind** — print, `str`/`repr`, an f-string field, a binding, a
//     `len`, a membership test, a loop — asks the door for the pair and is answered;
//   - a position that keeps **one word** for a whole value — a `return`, a call argument, an operand of
//     arithmetic — cannot carry a kind, so it refuses with the missing half named rather than handing out an
//     `@str_tab` index and calling it a string (Gap R.146's standing, the reason
//     `print(["a", "b"][0] + "!")` stays a refusal instead of printing `0!`).
//
// And the fold keeps the shape it genuinely reads: every element an integer constant and the position an
// integer. `print([1, 2][0])` still emits a constant, which is what it has always emitted and what its IR
// test asserts.

// listLiteralFoldAnswers reports whether the compile-time fold under the subscript can answer this read on
// its own: an integer position, and a literal every element of which is an integer constant. That is the
// shape the fold has always answered and keeps answering — it emits a constant, not an allocation.
//
// Anything else goes down the road the variable walks. The position is part of the question, not an
// afterthought: `["a","b"][True]` has a bool where a position belongs, and a fold that accepted it would be
// answering a question the reference answers differently — that shape is the bounds door's, and
// Gap R.222's gate is what stands in front of it today.
func (g *irGen) listLiteralFoldAnswers(ll *ListLit, idx Expr) bool {
	if _, isInt := idx.(*IntLit); !isInt {
		return false
	}
	if len(ll.Elems) == 0 {
		return false
	}
	// The position has to be one the literal has. The bounds check upstream is what raises the
	// IndexError, and it runs first on every road that reaches this gate — but a gate and a door that
	// answer different questions are how a refusal nobody measured ships (ADR 0249), so the gate asks
	// the whole question it is named for.
	if il, isInt := idx.(*IntLit); isInt && (il.Value < 0 || il.Value >= int64(len(ll.Elems))) {
		return false
	}
	for _, el := range ll.Elems {
		if _, isInt := el.(*IntLit); !isInt {
			return false
		}
	}
	return true
}

// listLiteralPair reads one element of a list literal through the one subscript door that answers a
// (payload, tag) pair — `containerSlotRead`, the same call a list *variable*'s read emits. It is not a
// second read: that is the whole reason `print(["a","b"][0])` and `print(xs[0])` cannot grow apart, which is
// what this row was.
func (g *irGen) listLiteralPair(b *strings.Builder, ll *ListLit, idx Expr, sp Span) (val, tag string, ok bool) {
	if g.listLiteralFoldAnswers(ll, idx) {
		// The fold reads this shape and emits no allocation for it; the door declines and the fold stays
		// in charge, exactly as the dict arm decided in ADR 0327.
		return "", "", false
	}
	h, kind, okHandle := g.containerHandleOf(b, ll)
	if !okHandle || kind != "list" {
		return "", "", false
	}
	return g.containerSlotRead(b, h, kind, idx, sp)
}

// listLiteralEntryIsPlainInt reports the one case the one-word roads may still answer themselves: an
// integer position naming an element that *is* an integer constant, where the payload is the value and no
// tag is needed to say so.
func (g *irGen) listLiteralEntryIsPlainInt(ll *ListLit, idx Expr) bool {
	il, isInt := idx.(*IntLit)
	if !isInt || il.Value < 0 || int(il.Value) >= len(ll.Elems) {
		return false
	}
	_, ok := ll.Elems[il.Value].(*IntLit)
	return ok
}

// listLiteralRead is the one-word answer, and it is given only where the payload *is* the value. Every
// other kind arrives here as a word whose meaning lives in a tag register the caller has no slot for, and
// says so by name rather than printing an index (Gap R.146, ADR 0187).
func (g *irGen) listLiteralRead(b *strings.Builder, ll *ListLit, idx Expr, sp Span) (string, error) {
	if !g.listLiteralEntryIsPlainInt(ll, idx) {
		return "", listLiteralOneWordErr(ll, idx)
	}
	elems, err := listLiteralElems(ll)
	if err != nil {
		return "", err
	}
	il := idx.(*IntLit)
	return fmt.Sprintf("%d", elems[il.Value]), nil
}

// listLiteralElems is the fold's view of a list literal: the integer constants, in source order, or an
// error saying the literal holds something the fold cannot read. It is the list twin of `setLiteralElems`
// and `dictLiteralVals`, and it is what keeps the two questions — can the fold answer this, and must the
// door be asked — asked the same way everywhere (ADR 0249's rule that a gate and a door testing different
// things ship a refusal nobody measured).
func listLiteralElems(ll *ListLit) ([]int64, error) {
	elems := make([]int64, 0, len(ll.Elems))
	for _, el := range ll.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("list literal elements must be constant integers")
		}
		elems = append(elems, il.Value)
	}
	return elems, nil
}

// listLiteralOneWordErr names the missing half, and the row that owns it: the element's kind is written
// beside its payload by the builder and read back by the pair door, and a road that keeps one word for a
// whole value has nowhere to put it.
func listLiteralOneWordErr(ll *ListLit, idx Expr) error {
	return fmt.Errorf("codegen: reading element %s of the list literal %s needs the element's kind to travel with "+
		"the element; this position keeps one word for a whole value, so a text (an index into @str_tab), a float "+
		"(a @float_box handle), None or a bool would be printed or added as a number nobody wrote — the pair door "+
		"answers it for print, str/repr, a binding, len, a membership test and a loop (roadmap Gap R.218, ADR 0331; "+
		"the one-word positions are Gap R.146)", indexText(idx), indexText(ll))
}

// indexText renders an expression for a diagnostic, and says so generically when the source text is not
// recoverable — a refusal that invents a spelling is worse than one that admits it does not have it.
func indexText(e Expr) string {
	if txt, ok := exprSourceText(e); ok {
		return txt
	}
	return "<this element>"
}
