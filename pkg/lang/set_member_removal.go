package lang

import (
	"fmt"
	"strings"
)

// Removing a member is the mirror image of adding one, and the compiled backend had two different
// answers to the two halves of that mirror. `s.add(v)` asks its argument what it carries — the member
// travels as a (payload, tag) pair into `rt_set_add_tagged`, because ADR 0187 and ADR 0189 established
// that a slot's payload is never the discriminator: a stored text is an index into `@str_tab`, so the
// text "0" and the number 0 are the SAME i32. `s.discard(v)` asked the SET instead: the road called
// `heapElemKind` for the argument's payload, asked `elemKindTag` only to decide whether the ADD should
// be tagged, and handed the removal door `rt_set_discard(h, v)` one word — so the door scanned the
// payload array and deleted the first member whose payload happened to equal whatever the argument's
// payload happened to be. Measured on the pinned reference, all at exit 0:
//
//	s = {1, 2} / s.discard("a") / print(s)   the compiled leg wrote {1}     the reference wrote {1, 2}
//	s = {1, 2} / s.discard(9)   / print(s)   the compiled leg wrote {1}     the reference wrote {1, 2}
//	s = {0, 1} / s.discard(None) / print(s)  the compiled leg wrote {1}     the reference wrote {0, 1}
//	s = {1, 2} / s.discard([1])              the compiled leg wrote {2}     the reference raised TypeError: unhashable type: 'list'
//
// The first three are the same lesion: an argument the set has never held removed a member it does
// hold. (`discard(9)` removing a member is the sentence that makes this the door's row rather than the
// notebook's: even a member the compiler can read is judged by its payload alone.) The fourth is the
// question the payload cannot ask at all — hashability is a fact about the ARGUMENT, and a heap handle
// answers it "yes".
//
// `s.remove(v)` did not exist. It fell past the set-method door to the string-method road, whose
// name-holds-container gate refused it at exit 1 (**Gap R.174**'s standing for a container METHOD over
// a NAME) — where the reference answers `KeyError: 9`. Refusing a program CPython prints is worse than
// answering it wrongly only when the refusal is the last word; here the door beside it already had
// every fact a `remove` needs.
//
// # The rule this file installs
//
// ONE QUESTION, ASKED OF THE ARGUMENT, ONCE: which (payload, tag) pair does this member carry? Both
// verbs ask it through the same reading `add` uses — `taggedOperand` (the pair road a bound name
// answers from its own tag word, a float from its box, a container from its materialised handle) and
// then `pairElemPair` (the pair a subscript or a carried call left behind). Neither reading is asked
// of the set: the road never calls `recordElemKind`, `promoteMixed` or `rt_mark_estr`, because removing
// a member is not evidence about the members that remain — that was the second half of the bug, which
// is why `print` after a discarded text rendered the survivors through the kind of the argument that
// was never a member.
//
// Where no reading answers, the road REFUSES at exit 1 naming the argument, the missing half and the
// rows, rather than run a removal the payload alone would decide. A refusal the reference does not
// answer is debt (**ADR 0166**); a wrong answer at exit 0 is a lie, and the difference is the whole
// content of this row.
//
// Both verbs then go to the same two runtime doors, in the same order: `rt_set_find` answers "which
// position, if any, holds this pair?" by `rt_slot_matches` (payload AND tag, through `rt_payload_eq`,
// which is why `{1}` and `{True}` are each other's member and `"1"` is not), and `rt_set_remove_at`
// shifts the tail of BOTH arrays. `discard` hands the pair to `rt_set_discard_tagged`, which is those
// two with an absent test between them; `remove` asks `rt_set_find` itself, raises `KeyError` naming
// the member from the answer that came back -1, and only then calls `rt_set_remove_at`. One scan per
// verb, and the finding is not asked twice from two doors that could answer differently (**ADR 0232**'s
// one question, **ADR 0288**'s one evaluation).
//
// Hashability is asked BEFORE the set is consulted, exactly as `checkKeyReadTagged` asks it for a dict
// read: a member the language refuses to hash is not a missing member, and a scan that can never match
// it would report `KeyError` where the reference raises `TypeError` naming the type (**Gap R.217**,
// **ADR 0328**, and one trap one wording per **ADR 0271**).
//
// The question is NOT the dict door's question, though, and the difference is measured rather than
// reasoned about. Asked of the pinned reference (CPython 3.12.3), one program per cell:
//
//		discard([1])                             TypeError: unhashable type: 'list'
//		discard({"a": 1})                        TypeError: unhashable type: 'dict'
//		discard({1})                             NO ERROR AT ALL — the set is printed unchanged
//		discard((1, [2]))                        TypeError: unhashable type: 'list'
//		discard((1, {2}))                        TypeError: unhashable type: 'set'
//		remove([1]) / remove({"a": 1})           TypeError, as above
//		remove({1})                              KeyError: {1} — the reference's word is MISSING, not UNHASHABLE
//
// So a BARE set is exempt from the TypeError for both verbs — `discard` says nothing, `remove` says the
// member was not there — while a set NESTED inside a tuple still names itself. That asymmetry is a
// property of the two CPython entry points, not of this backend's convenience, and the road copies it
// cell for cell rather than sharing the dict door's blanket guard. A tuple is the other exemption the
// tag forces on this door: this backend gives the two container spellings ONE layout (**ADR 0361**), so
// a tuple's slot carries TagList, and a guard that read the tag would refuse `s.discard((1, 2))` with
// `TypeError: unhashable type: 'list'` where the reference prints the set untouched. The static door
// asks the question the language actually has — it walks a tuple's elements left to right and names the
// first it cannot hash, the way CPython does (roadmap L11.3, Gap R.217).
//
// (roadmap L11.1, Gap R.422, ADR 0425; the pair rule from ADR 0187, ADR 0189 and ADR 0232)

// setMemberRemoval lowers `s.discard(x)` and `s.remove(x)` over a name the program bound to a heap set.
// It returns the empty register — both verbs hand back the void, and `print(s.discard(1))` printing
// `None` is the same arrangement ADR 0172 set for a void return (pinned by the record's
// `s = {1}` / `print(s.discard(1))` / `print(s)`).
func (g *irGen) setMemberRemoval(b *strings.Builder, setName, verb string, arg Expr, sp Span) (string, error) {
	g.heapUsed = true

	// 1. HASHABILITY, before the set is asked anything, and worded as the reference words it for THIS
	//    verb. `memberIsABareSet` carries the one exemption the reference grants this door and the dict
	//    door does not have; everything else goes to the shared static gate, which names the type the
	//    source wrote (`s.discard([1])` → 'list').
	if bareSet := g.memberIsABareSet(arg); !bareSet {
		if _, bad := g.unhashableKeyGate(b, arg, sp); bad {
			return "", nil // the gate raised; the removal it stands in front of never happens
		}
	}

	// 2. THE ARGUMENT ANSWERS FOR ITSELF. The same two readings `add` asks, ordered so that exactly one
	//    of them is ever reached and neither has emitted before the other is tried: `pairElemPair` asks
	//    its pure `isPairElem` gate first and emits nothing until it has answered yes, and the pair road
	//    is consulted before `taggedOperand`, so a name the pair road bound is never also lowered by the
	//    literal reading beside it — the operand is built once (ADR 0288).
	payload, tag, answered := g.pairElemPair(b, arg)
	if !answered {
		payload, tag, answered = g.taggedOperand(b, arg)
	}
	if !answered {
		// Nothing answers the argument's kind. The payload-only door beside this one answered every such
		// program with a removal it decided by accident — which is the wrong answer this row exists to
		// retire, so the road says what it cannot do instead of doing it (ADR 0166).
		return "", fmt.Errorf("%s() on %q removes a member whose kind the compiled leg cannot name: the removal compares a member's (payload, tag) pair, and %s — CPython answers this program, which needs the value word that carries its own kind (roadmap L11.1, Gap R.422, ADR 0425)",
			verb, setName, g.exprSummary(arg))
	}
	// The other half of the same answer, and the same exemption where the kind arrives as a register:
	// the two kinds the reference refuses for BOTH verbs are tested around the tag word, so the sentence
	// is the same whether the kind was a constant this pass wrote or a register the pair road loaded
	// (ADR 0271). The set arm is deliberately absent — see the table above.
	if !g.isTupleExpr(arg) {
		g.guardRemovableMemberTag(b, tag, sp)
	}

	// 3. THE SET IS ASKED ONLY "WHICH POSITION?", never "WHAT KIND AM I?".
	g.heapSeq++
	hs := g.heapSeq
	b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+setName))
	if verb == "discard" {
		// The absent case is the door's own no-op — `discard` is `remove` without the raise, and the
		// no-op lives where the finding does, so no caller can forget it.
		b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s, i32 %s)\n", "rt_set_discard_tagged", hs, payload, tag))
		return "", nil
	}
	at := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_set_find(i32 %%h%d, i32 %s, i32 %s)\n", at, hs, payload, tag))
	missing := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", missing, at))
	g.markI1(missing)
	badL, okL := g.newLabel("setrm.bad"), g.newLabel("setrm.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", missing, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	// The reference's own sentence, with the member's repr in it: `KeyError: 9`, `KeyError: 'a'`,
	// `KeyError: None` — the same door `d[k]` reads for the name a missing key carries (Gap R.189 owns
	// naming one the compiler cannot see, and the generic sentence it leaves is that row's, not new).
	g.raiseTo(b, exnCode("KeyError"), "KeyError", g.keyNotFoundMessage(arg), sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	b.WriteString(fmt.Sprintf("  call void @rt_set_remove_at(i32 %%h%d, i32 %s)\n", hs, at))
	return "", nil
}

// memberIsABareSet is the exemption the pinned reference grants this door and no other: the argument IS
// a set — not a value that merely carries one. Measured on CPython 3.12.3, `s.discard({1})` prints the
// set unchanged and exits 0, and `s.remove({1})` raises `KeyError: {1}`, while `s.discard((1, {2}))`
// raises `TypeError: unhashable type: 'set'`. A door that asked its blanket "can this be hashed?" here
// would answer both of the first two with a TypeError the reference never raises — a wrong trap at exit 3
// is the same class of defect as a wrong answer at exit 0 (**ADR 0166**, **ADR 0271**), so the exemption
// is exactly as wide as the measurement and no wider: a `*SetLit`, the `set()` spelling of one, and a
// name whose record says it holds a set.
//
// It is NOT a hashability claim. Nothing on this road can put a set INTO a set — the member door beside
// it refuses an unhashable member — so the scan below cannot find one, which is why `discard` lands on
// its no-op and `remove` on its `KeyError` without either of them having to know the exception.
func (g *irGen) memberIsABareSet(arg Expr) bool {
	if _, isSet := arg.(*SetLit); isSet {
		return true
	}
	if c, isCall := arg.(*Call); isCall && g != nil {
		if lit, isEmpty := g.emptyContainerCall(c); isEmpty {
			_, isSet := lit.(*SetLit)
			return isSet
		}
		return false
	}
	nm, isName := arg.(*Name)
	if !isName || g == nil {
		return false
	}
	if g.runtimeSets[nm.Value] {
		return true
	}
	if lit, has := g.containerLits[nm.Value]; has {
		_, isSet := lit.(*SetLit)
		return isSet
	}
	return false
}

// guardRemovableMemberTag is `guardHashableTag`'s shape minus the arm this door's reference does not
// have. The dict-key guard asks one question of every key — a list, a dict OR a set key is unhashable —
// and CPython answers all three (`{"a": 1}[{1}]` dies `TypeError: unhashable type: 'set'`). The removal
// door may not copy that guard: measured on the pinned reference, `s.discard({1})` raises NOTHING and
// `s.remove({1})` raises `KeyError: {1}`, because those two entry points swallow a TypeError from hashing
// a set argument and report it as an absent member instead. The two kinds that DO raise for both verbs,
// list and dict, are tested here with the same `unhashableKeyMessage` the shared doors use, so the
// sentence stays one sentence (ADR 0271); the set kind is left to the scan, which cannot find it.
//
// (roadmap Gap R.422, ADR 0425; the guard this one is derived from is Gap R.81's)
func (g *irGen) guardRemovableMemberTag(b *strings.Builder, tag string, sp Span) {
	for _, k := range []struct {
		tg   int32
		name string
	}{
		{int32(TagList), "list"},
		{int32(TagDict), "dict"},
	} {
		c := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, %d\n", c, tag, k.tg)
		g.markI1(c)
		g.branchRaise(b, c, "TypeError", unhashableKeyMessage(k.name), sp, "rmemhash")
	}
}
