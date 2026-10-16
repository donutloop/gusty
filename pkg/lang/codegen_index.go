// Subscripts: a read, a write, a slice, a key lookup, and the bounds/key questions that raise IndexError and
// KeyError when they miss.
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// assignIndex lowers `ix.Obj[ix.Idx] = val` against a runtime container variable.
func (g *irGen) assignIndex(b *strings.Builder, ix *Index, val Expr) error {
	nm, ok := ix.Obj.(*Name)
	if !ok {
		return fmt.Errorf("codegen: item assignment needs a container variable on the left (d[k] = v), got %T; CPython accepts more forms", ix.Obj)
	}
	// `t[0] = 1` on the immutable spelling: the reference raises TypeError — 'tuple' object does not
	// support item assignment — and the shared container layout could carry the write, so the door
	// refuses before emitting none of it rather than answering a mutated tuple
	// (roadmap L11.3, ADR 0361, immutable container).
	if g.isTupleExpr(nm) {
		return refusesTupleMutation("`"+nm.Value+"[i] = v`", "TypeError",
			"'tuple' object does not support item assignment")
	}
	// Storing under an unhashable key is the same refusal as reading under one, asked at the
	// other door: the reference raises TypeError naming the type, and the put road happily
	// built the entry, so `d = {}; d[[1]] = 2; print(d)` answered `{[1]: 2}` at exit 0 where the
	// reference never stores it (roadmap Gap R.217, ADR 0328). The store below is still emitted
	// — unreachable, so the module stays valid — and only after the raise branched away.
	if g.badIndexGateAtSubscript(b, ix) {
		// The store road is the same question: `xs["a"] = 1` counted to slot 0 and stored there
		// while the reference refuses to treat a text as a position (Gap R.222, ADR 0329).
		return nil
	}
	if g.dictVarHasKind(nm.Value) {
		if _, bad := g.unhashableKeyName(ix.Idx); bad {
			tn, _ := g.unhashableKeyName(ix.Idx)
			g.heapUsed = true
			g.raiseTo(b, exnCode("TypeError"), "TypeError", unhashableKeyMessage(tn), ix.Span())
			cont := g.newLabel("uhk.cont")
			b.WriteString(fmt.Sprintf("%s:\n", cont))
		}
	}
	h, err := g.value(b, ix.Obj)
	if err != nil {
		return err
	}
	// The value is asked what it is by the road that is going to store it. A value the pair road bound
	// hands the container its payload and its tag as two registers, and is the only case that question is
	// asked differently: every other value still goes through the ordinary road first, which is what keeps
	// `d["k"] = xs[0] / 2` and `xs[0] = xs[0] / 2` an honest refusal today rather than a payload stored where
	// its meaning lives in a register nobody read (roadmap L11.1, Gap R.146, ADR 0311).
	valWord, valTag, valIsPair := g.pairElemPair(b, val)
	if !valIsPair {
		if _, verr := g.value(b, val); verr != nil {
			return verr
		}
	}
	// A key the pair road bound arrives as two registers, and `rt_dict_put_tagged` is the door that takes
	// them. The list branch keeps asking ONE word for its index: a pair used as a position is a different
	// question — a text slot's payload is an index into another table, where the reference raises `list
	// indices must be integers or slices` — and it stays owed (roadmap L11.1, Gap R.146, ADR 0311).
	var key, keyTag string
	var kIsStr, kIsPair bool
	if g.runtimeDicts[nm.Value] {
		var kErr error
		key, keyTag, kIsPair, kIsStr, kErr = g.mutationSlotWords(b, ix.Idx)
		if kErr != nil {
			return kErr
		}
	} else {
		var kErr error
		key, kIsStr, kErr = g.heapElemKind(b, ix.Idx)
		if kErr != nil {
			return kErr
		}
	}
	switch {
	case g.runtimeDicts[nm.Value]:
		var v string
		var vIsStr bool
		if valIsPair {
			// The pair's payload is the word and its tag travels beside it (ADR 0187); asking
			// `heapElemKind` here would ask a name for a spelling it does not have.
			v, vIsStr = valWord, false
		} else {
			var verr error
			v, vIsStr, verr = g.heapElemKind(b, val)
			if verr != nil {
				return verr
			}
		}
		if g.printsAsInternedStr(val) && !valIsPair {
			vIsStr = true
		}
		// The pair and its two tags go in together: a dict entry that keeps the tag of the
		// value it used to hold prints and compares as the old kind (ADR 0187, ADR 0189). And
		// every entry is stored with both tags, because the lookup compares them (ADR 0232):
		// an entry whose tag was never written would answer to the wrong needle. Where
		// elemKindTag cannot describe an operand, this site knows which word it interned — an
		// @str_tab index or a number — and says so.
		//
		// Storing a value of the kind this dict has not held before leaves it with no single
		// kind to claim. It used to *replace* the recorded one, which is a statement about the
		// slot being written and was read as a statement about every slot: `d = {"a": 1}` then
		// `d["b"] = "x"` printed {'a': 'x', 'b': 'x'} — the number 1 printed as the string whose
		// interned index happens to be 1. The slots carry their tags, so the container is
		// promoted to describing itself, and the refusal stays for a value no tag can name.
		contradicts := (vIsStr && g.dictValInt[nm.Value]) || (!vIsStr && g.dictValStr[nm.Value]) ||
			(kIsStr && g.dictKeyInt[nm.Value]) || (!kIsStr && g.dictKeyStr[nm.Value])
		if kIsPair || valIsPair {
			// An entry whose key or value arrives as a (payload, tag) pair leaves the dict with no
			// single kind to claim: the kind is a register now, per slot (ADR 0232's promotion, asked of
			// a value this pass cannot label — roadmap L11.1, Gap R.146, ADR 0311).
			g.promotePairMixed(b, h, nm.Value, "dict value")
		}
		if !g.mixedDicts[nm.Value] {
			if contradicts {
				if !g.promoteMixed(b, h, nm.Value, "dict value", val, ix.Idx) {
					return mixedKindErr("dict")
				}
			} else {
				g.replaceElemKind(nm.Value, "dict value", vIsStr)
				g.replaceElemKind(nm.Value, "dict key", kIsStr)
			}
		}
		kt, ktOK := g.elemKindTag(ix.Idx)
		if !ktOK {
			if kIsStr {
				kt = int32(TagStr)
			} else {
				kt = int32(TagInt)
			}
		}
		vt, vtOK := g.elemKindTag(val)
		if !vtOK {
			if vIsStr {
				vt = int32(TagStr)
			} else {
				vt = int32(TagInt)
			}
		}
		// The tag is the number the element's spelling describes, except where the element is a pair: then
		// it is the register the objects wrote, which is the only thing in the module that knows what the
		// payload means (ADR 0187). A key whose kind only that register can tell is asked whether it can be
		// a key at all, in the reference's own words (Gap R.81, ADR 0310).
		ktS, vtS := strconv.FormatInt(int64(kt), 10), strconv.FormatInt(int64(vt), 10)
		if kIsPair {
			ktS = keyTag
			g.guardHashableTag(b, keyTag, ix.Src)
		}
		if valIsPair {
			vtS = valTag
		}
		b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)\n", h, key, v, ktS, vtS))
		// An entry whose key or value is a float or None has a payload the dict's compiled kinds
		// cannot render — a box handle and nothing — so the dict stops claiming them, the same move
		// ADR 0232 made for an entry that mixes numbers with text (roadmap L11.1, ADR 0233).
		if !g.mixedDicts[nm.Value] && (slotTagSelfDescribing(kt) || slotTagSelfDescribing(vt)) {
			if g.promoteMixed(b, h, nm.Value, "dict value", val, ix.Idx) {
				g.floatFmtUsed = g.floatFmtUsed || kt == int32(TagFloat) || vt == int32(TagFloat)
			}
		}
		if kIsStr || vIsStr {
			bits := 0
			if kIsStr {
				bits |= 2 // dict keys are interned strings
			}
			if vIsStr {
				bits |= 4 // dict values are interned strings
			}
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, bits))
		}
		return nil
	case g.listVars[nm.Value]:
		// The element is written with its tag in one operation, whether the tag is a constant this pass
		// read off a spelling or a register the objects wrote: `elemKindTag` answers the first and
		// `pairElemPair`/`mixedElemTag` the second, and a value whose kind only they can tell leaves the
		if valIsPair || g.mixedLists[nm.Value] {
			if valIsPair {
				// The slot is written with its tag, and the list stops claiming a kind: the kind is the
				// register now (ADR 0187, ADR 0232; roadmap L11.1, Gap R.146, ADR 0311).
				g.promotePairMixed(b, h, nm.Value, "list")
			}
			// An element of a tagged list is (payload, tag); writing the payload alone would
			// leave the slot tagged as whatever lived there before, so the tag is written with
			// it. This is the shape that used to answer `[1, 'a', None]` for `xs[0] = "z"` —
			// the interned index printed through the stale int tag (roadmap L11.1, ADR 0187).
			sv, tag := valWord, valTag
			if !valIsPair {
				var serr error
				sv, tag, serr = g.mixedElemTag(b, val)
				if serr != nil {
					return serr
				}
			}
			ln := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, h))
			hi := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, key, ln))
			g.markI1(hi)
			lo := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, key))
			g.markI1(lo)
			bad := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
			g.markI1(bad)
			badL, okL, endL := g.newLabel("item.bad"), g.newLabel("item.ok"), g.newLabel("item.end")
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
			b.WriteString(fmt.Sprintf("%s:\n", badL))
			g.raiseTo(b, exnCode("IndexError"), "IndexError", listIndexOutOfRange(), ix.Span())
			b.WriteString(fmt.Sprintf("%s:\n", okL))
			b.WriteString(fmt.Sprintf("  call void @rt_put_elem(i32 %s, i32 %s, i32 %s)\n", h, key, sv))
			b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %s, i32 %s, i32 %s)\n", h, key, tag))
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			b.WriteString(fmt.Sprintf("%s:\n", endL))
			return nil
		}
		// A string element is stored as its @str_tab index (Gap I.2), like every other
		// container slot write; the printed form follows from listElemStr. What counts as a
		// string word is asked of the value twice, because heapElemKind answers for the word it
		// interned and printsAsInternedStr answers for the value the source says it is: `xs[0] =
		// d["a"]` reads a string out of a dict, and deciding "not a string" there promoted
		// nothing and left the list printing its new string element through the number printer —
		// the store happened, the print lied (ADR 0232).
		sv, sIsStr, serr := g.heapElemKind(b, val)
		if serr != nil {
			return serr
		}
		if g.printsAsInternedStr(val) {
			sIsStr = true
		}
		// Writing one slot is not the same statement as what the whole list holds. This site used
		// to overwrite the recorded kind, so `xs = [1, 2]; xs[0] = "s"` left the list claiming
		// strings and printed the untouched 2 as whatever string its index happens to name
		// (`['s', 'b']`). Contradiction promotes the list to describing its slots (ADR 0232);
		// agreement keeps the static path.
		if !g.mixedLists[nm.Value] {
			if contradicts := (sIsStr && g.listElemInt[nm.Value]) || (!sIsStr && g.listElemStr[nm.Value]); contradicts {
				if !g.promoteMixed(b, h, nm.Value, "list", val) {
					return mixedKindErr("list")
				}
			} else {
				g.replaceElemKind(nm.Value, "list", sIsStr)
			}
		}
		// Bounds are checked so an out-of-range index raises IndexError through the
		// The index is normalised and bounds-checked by the same helper the read path
		// uses, so `xs[-1] = v` writes the last element and an out-of-range write
		// raises IndexError through the same path an explicit `raise` uses
		// (roadmap L11.4, ADR 0210).
		//
		// The element is lowered once, by `heapElemKind` just above, and not first asked of `value` as
		// this site used to do: the extra call emitted a word nobody read, and for a name the pair road
		// bound it refused before this road ever got to ask its own question (roadmap L11.1, Gap R.146).
		endL := g.newLabel("item.end")
		key = g.normalizeIndexFor(b, h, key, ix.Span(), listAssignmentIndexOutOfRange())
		b.WriteString(fmt.Sprintf("  call void @rt_put_elem(i32 %s, i32 %s, i32 %s)\n", h, key, sv))
		b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %s, i32 %s, i32 %s)\n", h, key, g.elemTagOperand(b, val, sIsStr)))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
		return nil
	case g.runtimeSets[nm.Value]:
		return fmt.Errorf("codegen: sets do not support item assignment (s[k] = v); CPython raises TypeError")
	case isStringExpr(ix.Obj) || (g.strVals != nil && g.strVals[nm.Value] != ""):
		return fmt.Errorf("codegen: strings are immutable, so s[k] = v is not allowed")
	}
	return fmt.Errorf("codegen: item assignment is only supported for runtime dict and list variables")
}

// checkIndexRead emits the bounds test for a heap-list read: `xs[i]` used to load
// whatever sat at that slot and print 0, where Python raises IndexError.
// normalizeIndex turns a possibly-negative positional index into the offset the runtime
// understands and bounds-checks the *normalised* value, raising IndexError through the
// same path an explicit `raise` uses. One rule for read and write, shared with `pop` and
// slicing: a negative index counts from the end (roadmap L11.4, ADR 0210). Dict and set
// subscripts never come here — their index is a key, and `-1` is a key you can store.
func (g *irGen) normalizeIndex(b *strings.Builder, h, idx string, sp Span) string {
	ix, bad := g.indexBoundsCheck(b, h, idx)
	badL, okL := g.newLabel("ix.bad"), g.newLabel("ix.ok")
	// The three roads that come here without a sentence are the ones where the container's kind is a
	// run-time fact — a slot read (`xs[0][5]`, `d["k"][5]`) arrives with a handle and a tag, and the one
	// list layout answers for the list AND for the immutable spelling. So the object is asked, through
	// its own door, which sentence the raise quotes (roadmap L11.3, ADR 0361).
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	tupL, lstL := g.newLabel("ix.bad.tup"), g.newLabel("ix.bad.lst")
	flag := g.newTmp()
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_tuple_flag(i32 %s)\n", flag, h))
	isTup := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", isTup, flag))
	g.markI1(isTup)
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isTup, tupL, lstL))
	b.WriteString(fmt.Sprintf("%s:\n", tupL))
	g.raiseTo(b, exnCode("IndexError"), "IndexError", tupleIndexOutOfRange(), sp)
	b.WriteString(fmt.Sprintf("%s:\n", lstL))
	g.raiseTo(b, exnCode("IndexError"), "IndexError", listIndexOutOfRange(), sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	g.heapUsed = true
	return ix
}

// indexBoundsCheck emits the length, the negative wrap and the range test, and answers the two
// registers the raise strategies branch on: the normalised position and the `bad` verdict. One copy
// of the arithmetic for the door that already knows its sentence and the door that must ask the
// object (roadmap Gap R.223, ADR 0330, ADR 0361).
func (g *irGen) indexBoundsCheck(b *strings.Builder, h, idx string) (string, string) {
	ln := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, h))
	neg := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", neg, idx))
	g.markI1(neg)
	wrapped := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", wrapped, idx, ln))
	ix := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", ix, neg, wrapped, idx))
	hi := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, ix, ln))
	g.markI1(hi)
	lo := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, ix))
	g.markI1(lo)
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
	g.markI1(bad)
	return ix, bad
}

// normalizeIndexFor is the bounds check with the sentence the door owns. The reference distinguishes
// `list index out of range` from `list assignment index out of range` because reading past the end and
// writing past it are different acts, and one shared normaliser cannot know which it is doing unless
// the caller says (roadmap Gap R.223, ADR 0330). The same door takes the tuple's sentence when the
// caller can see the parentheses the program wrote (ADR 0361).
func (g *irGen) normalizeIndexFor(b *strings.Builder, h, idx string, sp Span, sentence string) string {
	ix, bad := g.indexBoundsCheck(b, h, idx)
	badL, okL := g.newLabel("ix.bad"), g.newLabel("ix.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("IndexError"), "IndexError", sentence, sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	g.heapUsed = true
	return ix
}
func (g *irGen) checkIndexRead(b *strings.Builder, h, idx string, sp Span) {
	ln := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, h))
	hi := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, idx, ln))
	g.markI1(hi)
	lo := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, idx))
	g.markI1(lo)
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
	g.markI1(bad)
	badL, okL := g.newLabel("rd.bad"), g.newLabel("rd.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("IndexError"), "IndexError", listIndexOutOfRange(), sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
}

// checkKeyReadTagged is checkKeyRead for a dict whose keys are tagged: the same KeyError, raised
// by the same path, asked of the (payload, tag) key. Without the tag the check would answer "the
// key is there" for {0: 1} when asked for "0", and the raise below would never come.
func (g *irGen) checkKeyReadTagged(b *strings.Builder, h, key, keyTag string, keyExpr Expr, sp Span) {
	// A key the language refuses to hash is not a missing entry, and the two questions must be
	// asked in that order: the reference raises TypeError naming the type, and a scan that cannot
	// match the key would otherwise report KeyError (roadmap Gap R.217, ADR 0328).
	if res, bad := g.unhashableKeyGate(b, keyExpr, sp); bad {
		_ = res
		return
	}
	ok := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_has_tagged(i32 %s, i32 %s, i32 %s)\n", ok, h, key, keyTag))
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isZero, ok))
	g.markI1(isZero)
	badL, okL := g.newLabel("rd.bad"), g.newLabel("rd.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isZero, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("KeyError"), "KeyError", g.keyNotFoundMessage(keyExpr), sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
}

// checkKeyRead emits the membership test for a heap-dict read: `d[k]` for a missing
// key used to return 0, where Python raises KeyError.
func (g *irGen) checkKeyRead(b *strings.Builder, h, key string, keyExpr Expr, sp Span) {
	// Asked before the dict is consulted, for the same reason as the tagged door above: a list,
	// dict or set key is unhashable, and a scan that can never match it answers "no such entry"
	// where the reference raises TypeError naming the type (roadmap Gap R.217, ADR 0328).
	if res, bad := g.unhashableKeyGate(b, keyExpr, sp); bad {
		_ = res
		return
	}
	ok := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_has(i32 %s, i32 %s)\n", ok, h, key))
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isZero, ok))
	g.markI1(isZero)
	badL, okL := g.newLabel("rd.bad"), g.newLabel("rd.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isZero, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("KeyError"), "KeyError", g.keyNotFoundMessage(keyExpr), sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
}

// keyNotFoundMessage words the KeyError the way the reference does: `KeyError: 'z'`, the key itself
// as the message — which is what a reader greps for and what makes a traceback actionable. A key the
// compiler cannot see as a literal at this point (a name whose kind is a run-time fact, a computed
// index) keeps the sentence the backend has always used, because inventing a key would be worse than
// a generic one; naming it for those keys needs the tagged value word (roadmap L11.1), which is the
// same debt that silences the echo of a value whose kind is not statically known.
//
// The immutable spelling joins the keys the compiler can quote, because the source wrote it out: a
// tuple written from literals has a repr the pass can read, and `d[(3, 4)]` over a dict that has no
// such entry says `KeyError: (3, 4)` the way the reference does (roadmap L11.3, ADR 0361; the row
// that owns the keys this door still cannot quote is Gap R.189).
func (g *irGen) keyNotFoundMessage(keyExpr Expr) string {
	switch k := keyExpr.(type) {
	case *StrLit:
		return pyReprString(k.Value)
	case *IntLit:
		return strconv.FormatInt(k.Value, 10)
	case *FloatLit:
		return pyFloatRepr(k.Value)
	case *BoolLit:
		if k.Value {
			return "True"
		}
		return "False"
	case *NoneLit:
		return "None"
	}
	if repr, ok := literalKeyRepr(keyExpr); ok {
		return repr
	}
	return "key not found"
}

// literalKeyRepr renders the repr of a container key the program spelled out of scalars — `(1, 2)`,
// `[1, 2]`, `(1, (2, 3))` — or declines. It is a repr, not a print: a text member is quoted, and the
// tuple spelling keeps its parentheses and its one-element comma (roadmap L11.3, ADR 0361).
func literalKeyRepr(e Expr) (string, bool) {
	switch k := e.(type) {
	case *ListLit:
		parts := make([]string, 0, len(k.Elems))
		for _, el := range k.Elems {
			s, ok := literalKeyRepr(el)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		if k.IsTuple() {
			if len(parts) == 1 {
				return "(" + parts[0] + ",)", true
			}
			return "(" + strings.Join(parts, ", ") + ")", true
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	case *StrLit:
		return pyReprString(k.Value), true
	case *IntLit:
		return strconv.FormatInt(k.Value, 10), true
	case *FloatLit:
		return pyFloatRepr(k.Value), true
	case *BoolLit:
		if k.Value {
			return "True", true
		}
		return "False", true
	case *NoneLit:
		return "None", true
	}
	return "", false
}
