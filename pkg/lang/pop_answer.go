// pop_answer.go is the road a popped slot takes out of a list.
//
// One `xs.pop([i])` has always been lowered by one function — emitPopRoad, which the method road and
// the door below share, so the position that discards the answer and the position that prints it cannot
// disagree about which slot was removed or which bounds raise runs. What this file adds is the second
// half of that answer: the removed slot's TAG, read by the same call that removed the payload.
//
// A list slot is two words — a payload and the tag that says what the payload means — and until now the
// door that TOOK a slot out of a list answered only the first. `xs = ["a", "b"]` / `print(xs.pop(0))`
// wrote `0`, the @str_tab position of the first text a program interns; `xs = [1.5, 2.5]` /
// `print(xs.pop(0))` wrote `1`, the float box's heap handle; `xs = [[1, 2], 3]` / `print(xs.pop(0))`
// wrote `1`, the inner list's slot; and `xs = [1, "a"]` / `if xs.pop(1):` took the else arm, because the
// truth of a payload is "the word is not zero" and a text's index is never zero for reasons that have
// anything to do with whether it has characters. Every one of those answers was the same shape: a word
// the object had already labelled, asked of a road that could not read the label (roadmap L11.1, ADR 0410).
//
// The tag is not invented here. It is loaded out of `@heap_tags` by `@rt_pop` itself, which now moves it
// with its payload through the shift — the same rule `rt_reverse`'s swap (ADR 0187) and
// `rt_set_discard_tagged`'s shift keep, on the third positional door and the one that had not been told.
package lang

import (
	"fmt"
	"strings"
)

// emitPopRoad lowers one `xs.pop([i])` on a list the program built: the handle, the length, the
// negative-index fix-up, the IndexError the bounds raise, the removal and the shift.
//
// It answers the popped payload, and — when wantTag is set — the tag the object gave that slot, read by
// the same call. A position that keeps one word for a whole value passes wantTag false and keeps the road
// it always had; a pair position passes true and asks the door below, which is the only reason
// `xs.pop(0)` discarded, `print(xs.pop(0))` and `v = xs.pop(0)` can never disagree about which element
// left the list (ADR 0232's one question, asked once).
func (g *irGen) emitPopRoad(b *strings.Builder, listName string, args []Expr, sp Span, wantTag bool) (payload, tag string, err error) {
	g.heapUsed = true
	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", h, "_"+listName))
	n := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", n, h))
	idx := g.newTmp()
	badL, okL := g.newLabel("pop.bad"), g.newLabel("pop.ok")
	var bad string
	if len(args) == 1 {
		iv, aerr := g.value(b, args[0])
		if aerr != nil {
			return "", "", aerr
		}
		neg := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", neg, iv))
		g.markI1(neg)
		adj := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", adj, iv, n))
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", idx, neg, adj, iv))
		hi := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, idx, n))
		g.markI1(hi)
		lo := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, idx))
		g.markI1(lo)
		bad = g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
		g.markI1(bad)
	} else {
		// No index: the last element, and an empty list is the error case.
		b.WriteString(fmt.Sprintf("  %s = sub i32 %s, 1\n", idx, n))
		bad = g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", bad, n))
		g.markI1(bad)
	}
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	if len(args) == 1 {
		// Two sentences, and which one runs is the OBJECT's fact, not the source's: CPython asks whether
		// the list has any elements at all before it asks whether the index fits, so `xs = []` /
		// `xs.pop(3)` reads `IndexError: pop from empty list` and `xs = [1]` / `xs.pop(3)` reads
		// `IndexError: pop index out of range`. Choosing the sentence by whether the source WROTE an index
		// — the rule this road had — made the first program quote the second program's message, which a
		// program that matches on `except IndexError` reads and a traceback reader greps for.
		emptyL, rangeL := g.newLabel("pop.bad.empty"), g.newLabel("pop.bad.range")
		isEmpty := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isEmpty, n))
		g.markI1(isEmpty)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isEmpty, emptyL, rangeL))
		b.WriteString(fmt.Sprintf("%s:\n", emptyL))
		g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop from empty list", sp)
		b.WriteString(fmt.Sprintf("%s:\n", rangeL))
		g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop index out of range", sp)
	} else {
		g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop from empty list", sp)
	}
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	// The slot's two words leave together: the payload as the call's answer, the tag through the slot
	// the caller reads. hoistAllocas moves the out-parametrised slot to the function's entry, so the
	// address is stable for the frame and the load below is the only reader.
	outT := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", outT))
	ret := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_pop(i32 %s, i32 %s, i32* %s)\n", ret, h, idx, outT))
	if !wantTag {
		return ret, "", nil
	}
	tag = g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", tag, outT))
	// The popped object is no longer in the list's slots, so from here its only witness is this register.
	// A float payload is a box the list was holding a reference to, and an unregistered handle is recycled
	// by the next allocation — the printed value then belongs to whoever allocated that slot next, which is
	// the order-dependent wrong number ADR 0181's rule and ADR 0280's measurement exist to prevent. The
	// registration costs one frame entry for a payload that turns out to be an immediate int, which is a
	// word wasted, not a bug.
	rootSlot := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", rootSlot))
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", ret, rootSlot))
	b.WriteString(fmt.Sprintf("  call void @rt_root_put(i32* %s)\n", rootSlot))
	g.rooted = true
	return ret, tag, nil
}

// popAnswerShape is the door's one gate, asked by the door itself and by the two scan questions that
// decide whether to ask it (arithWouldRefuse, pairCallScan). It answers which list the call pops from,
// whether the tag must come from the object, and what the container's own record claims the slots hold.
// ok=false means the call is not a list pop, or the notebooks say nothing about its elements — and a
// container whose kind nobody records is declined rather than guessed (roadmap L11.1, ADR 0410).
func (g *irGen) popAnswerShape(c *Call) (listName string, fromObject, ok bool) {
	attr, isAttr := c.Fn.(*Attr)
	if !isAttr || attr.Name.Value != "pop" || len(c.Args) > 1 {
		return "", false, false
	}
	nm, isName := attr.Obj.(*Name)
	if !isName || !g.listVars[nm.Value] || g.receiverIsTuple(attr.Obj) {
		return "", false, false
	}
	// The handle is loaded from the name the call is written against; the CLAIMS are asked of the name
	// whose record describes the object, which is a different name for a list the program shared
	// (`ys = xs`) — and asking the alias's own empty record is what made `print(ys.pop(0))` write `0`
	// beside `print(xs.pop(0))`'s `None` (roadmap L11.1, Gap R.415, ADR 0412).
	mixed, elemStr, elemInt, ok := g.listElementClaims(nm.Value)
	if !ok {
		return "", false, false
	}
	if mixed {
		return nm.Value, true, true
	}
	if elemStr || elemInt {
		return nm.Value, false, true
	}
	return "", false, false
}

// popAnswerAsksTheArithmeticDoor is arithWouldRefuse's arm for a popped slot: the arm of a sum whose
// kind the ordinary numeric road cannot state, because the number it would use is a payload and the
// label sits in the container. A list whose record says "integers" keeps the road it has always had —
// the payload IS the number there, and opening a working program's road is churn, not a fix. What this
// removes is the asymmetry that made `n = xs.pop(0)` / `print(n + 1)` answer 2.5 while
// `print(xs.pop(0) + 1)` answered 2 for the same three lines (roadmap L11.1, ADR 0265's door, ADR 0410).
func (g *irGen) popAnswerAsksTheArithmeticDoor(c *Call) bool {
	listName, fromObject, ok := g.popAnswerShape(c)
	if !ok {
		return false
	}
	if !fromObject {
		// The container still claims one element kind, so the ordinary numeric road's answer is the
		// road's own — the same answer `xs[0]` gets in the same position, which is the point of the
		// two spellings agreeing. A list of texts joined by `+` is CPython's `xy`, and the door would
		// have raised a TypeError the reference never raises.
		return false
	}
	// A list whose slots describe themselves is the shape whose kind the ordinary road cannot state —
	// the same test `arithWouldRefuse` asks of a slot read (`indexKindIsRuntimeObject`). Whether the
	// operator may take the door at all (`+`, `*` and `%` need the number proof, `-` and `//` do not)
	// stays the door's question, asked of the same program-wide pass (ADR 0265).
	_ = listName
	return true
}

// popAnswerPair is the pair door: it answers the (payload, tag) words of a popped slot for the positions
// that read a VALUE — print, str/repr, the truth road, a binding, a call argument, an arithmetic arm —
// and declines every other shape so its road stays where it was, refusal included. It is asked by the
// doors those positions already ask (pairCallPair, answerCallPair), never by a road of its own, which is
// what keeps `print(x)` and `str(x)` and `if x:` answering one kind for one value (ADR 0258's one printer).
//
// Which tag the answer carries is the container's fact, not the door's:
//
//   - a list whose slots describe themselves (`mixedLists`, the containers ADR 0184/0232 promoted) gets
//     the tag the OBJECT stored, loaded by the removal itself;
//   - a list that still claims one kind gets that kind, which is the notebook's own claim — the same one
//     the printer, the loop and the comparison roads act on — so a list of texts answers TagStr because
//     that is why its print shows ['a', 'b'] and not [0, 1];
//   - a list whose kind no notebook describes is declined rather than guessed. Guessing is how a payload
//     gets printed under a kind the object never gave it, and a wrong kind at exit 0 is the class of bug
//     this file exists to remove, not to add (roadmap L11.1, ADR 0166).
func (g *irGen) popAnswerPair(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	c, isCall := e.(*Call)
	if !isCall {
		return "", "", false, nil
	}
	listName, fromObject, serves := g.popAnswerShape(c)
	if !serves {
		return "", "", false, nil
	}
	// One removal per node. The doors that ask this question are stacked — print asks one door, an
	// f-string field another, a binding a third — and a second ask of the SAME call node has to answer
	// with the registers the first emitted, or the element leaves the list twice (roadmap L11.1, ADR 0410).
	if g.popPairMemo == nil {
		g.popPairMemo = map[*Call][2]string{}
	}
	if memo, seen := g.popPairMemo[c]; seen {
		return memo[0], memo[1], true, nil
	}
	// The tag the removal read out of the object, and the tag this container's own record claims. One of
	// the two travels with the payload; neither is invented here. The claim is asked through the same
	// resolver the gate asked — the record of a list the program shared lives on the name that built it
	// (roadmap L11.1, Gap R.415, ADR 0412) — so the gate's answer and the door's cannot drift.
	staticTag := int32(TagInt)
	if _, elemStr, _, _ := g.listElementClaims(listName); !fromObject && elemStr {
		staticTag = int32(TagStr)
	}
	payload, loaded, perr := g.emitPopRoad(b, listName, c.Args, c.Span(), true)
	if perr != nil {
		return "", "", false, perr
	}
	if fromObject {
		g.popPairMemo[c] = [2]string{payload, loaded}
		return payload, loaded, true, nil
	}
	g.popPairMemo[c] = [2]string{payload, fmt.Sprintf("%d", staticTag)}
	return payload, fmt.Sprintf("%d", staticTag), true, nil
}
