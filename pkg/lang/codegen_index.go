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
		// The program's own words, not the parser's: `*lang.ListLit` is what the AST node is called and a
		// reader has no such table (ADR 0166, ADR 0249). What the reader needs is the thing they wrote, the
		// shape the door CAN lower, and the row that owns the rest (roadmap L11.1 · **Gap R.379**; the store
		// through an expression is what **Gap R.381** files: the reference mutates the temporary, this
		// backend refuses the write, and the read side of the same position closed as **Gap R.214** / ADR 0334).
		return fmt.Errorf("codegen: item assignment needs a container variable on the left (`d[k] = v`), and the program writes into %s — bind the container to a name and store through it there (CPython accepts more forms; roadmap L11.1, Gap R.381)", nodeSurface(ix.Obj))
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
			g.guardHashableExprTag(b, keyTag, ix.Idx, ix.Src)
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
				// The str/int census is TWO questions — is it text, is it a number — and a slot whose payload
				// cannot speak its own kind is a THIRD. A float's payload is a `@float_box` handle, the void's is
				// nothing at all, a verdict's is a bare 0/1, a container's and an instance's are heap handles; none
				// of them is a number, and the container-wide kind is exactly what the printer reads. So this road
				// — the third one that writes a slot — had to ask what `append` has asked since **ADR 0233** and what
				// the dict's entry store has asked since **ADR 0232**: the ELEMENT's own tag, and if it describes
				// itself, the container stops claiming one. Without the arm, `xs = [1]` / `xs[0] = 2.5` /
				// `print(xs)` wrote `[1]` — the float box's handle, at the exit code of success — beside
				// `xs.append(2.5)`, which printed `2.5` all along (roadmap Gap R.441).
				if t, ok := g.elemKindTag(val); ok && slotTagSelfDescribing(t) {
					if g.promoteMixed(b, h, nm.Value, "list", val) {
						g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
					}
				}
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

// dictKeyNameTag answers the question ADR 0232 says every key must answer — WHICH KIND is this key, beside
// its payload — of a key that reached the door through a BINDING rather than spelled as a literal.
//
// The road had one arm for that, and it was unreachable: it sat in the `else` of a test on
// `n.Idx.(*IntLit)`, so it could only run for an index that was BOTH an `*IntLit` and a `*Name`. Every
// name key therefore fell to the tag of its payload word alone, and the void's payload IS 0 — which is
// why `d = {None: 3.5}` / `k = None` / `print(d[k])` died `KeyError: key not found` where the reference
// prints `3.5`: the IR asked `rt_dict_has_tagged(i32 %h, i32 %_k.ld1, i32 0)`, 0 being TagInt, and the
// entry's key is stored as (0, TagNone) (roadmap Gap R.286, measured on the IR of ADR 0419's build).
//
// Three records answer it, in the order that keeps every road that works today working:
//
//   - a name the PAIR road bound (`taggedVars`, ADR 0233) carries its own tag word in `_<name>_tag`, and
//     the load is the answer — that was the arm's own intention, and the alloca's presence is asked too,
//     because a load of a register this function never allocated is exit 2 (ADR 0166) rather than an answer;
//   - everything else asks `elemKindTag`, the door every other position already asks: it is where the
//     container key, the unrolled loop's per-item tag, the VOID and the VERDICT a binding left behind
//     come from (Gap R.286 put the two singletons into that door rather than repeating them here, so the
//     read, the pair road and the membership test cannot disagree about what the same name is).
func (g *irGen) dictKeyNameTag(b *strings.Builder, key Expr) (string, bool) {
	if nm, isName := key.(*Name); isName && g != nil {
		// A loop variable the unroll gave a per-item tag is THIS item, not the kind some earlier binding
		// handed the name (Gap R.358's rule), so the object-reading door answers that name.
		if _, perItem := g.loopElemTag[nm.Value]; !perItem && g.taggedVars[nm.Value] && g.allocd[nm.Value+"_tag"] {
			kt := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s_tag\n", kt, nm.Value))
			return kt, true
		}
	}
	if t, ok := g.elemKindTag(key); ok {
		return strconv.FormatInt(int64(t), 10), true
	}
	return "", false
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
	// A container spelled by its CONSTRUCTOR is the same key as the same container spelled by its
	// brackets, and the reference's sentence for it is the repr the language itself prints: `set()`,
	// `[]`, `{}`. The bracket door above cannot see past the syntax, so the road asks the same reading
	// the member door asks (roadmap L11.2, ADR 0386; the sentence rule is Gap R.189's).
	if c, isCall := keyExpr.(*Call); isCall && g != nil {
		if lit, empty := g.emptyContainerCall(c); empty {
			if repr, ok := literalKeyRepr(lit); ok {
				return repr
			}
		}
	}
	// A key that arrived through a BINDING is nameable too, when the binding wrote one of the two
	// singletons: the reference prints `KeyError: None` for `k = None` over a dict that has no such entry,
	// and the generic sentence is the thing that made the traceback unactionable. This is the same record
	// the read road now asks (Gap R.286), so the message cannot disagree with the lookup that missed.
	// Every other key keeps the generic sentence: Gap R.189 owns naming a key the compiler cannot see.
	if nm, isName := keyExpr.(*Name); isName && g != nil {
		if item, isItem := g.loopItemExpr(nm); isItem {
			// The name is an ITEM of an unrolled loop, and the unroll still holds which element this body
			// copy walks (ADR 0424's record, widened by ADR 0425). Without that reading the sentence for
			//   s = {1, 2, 3}
			//   for k in [1, 2, 9]:
			//       s.remove(k)
			// says 'key not found' where the reference says KeyError: 9 - the half that makes a traceback
			// findable was sitting in a map no one consulted on the way to the message.
			if repr, ok := literalKeyRepr(item); ok {
				return repr
			}
		}
		switch {
		case g.noneVars[nm.Value]:
			return "None"
		case g.boolVars[nm.Value]:
			if v, ok := g.foldConstInt(nm); ok {
				if v != 0 {
					return "True"
				}
				return "False"
			}
		case g.strVals[nm.Value] != "":
			// The text the binding wrote is the same record the printer asks, so the traceback cannot
			// disagree with the lookup that missed (Gap R.189's sentence, for the keys ADR 0420 can name).
			return pyReprString(g.strVals[nm.Value])
		default:
			if v, ok := g.foldConstInt(nm); ok {
				return strconv.FormatInt(v, 10)
			}
		}
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
	case *SetLit:
		// A set written at the call is quotable too, and the reference quotes it in braces: the missing
		// member of `s.remove({1})` is named `KeyError: {1}`, not `key not found`. The empty set has no
		// brace spelling in the reference - `{}` is an empty dict - so the sentence says `set()`, the way
		// `repr(set())` does (roadmap L11.2, ADR 0386; roadmap Gap R.441's one question, asked of a trap).
		if len(k.Elems) == 0 {
			return "set()", true
		}
		parts := make([]string, 0, len(k.Elems))
		for _, el := range k.Elems {
			s, ok := literalKeyRepr(el)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "{" + strings.Join(parts, ", ") + "}", true
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

// valueIndex is the road a subscript takes, container-agnostic: every kind question and every trap
// it asks is answered by the doors beside it in this file.
func (g *irGen) valueIndex(b *strings.Builder, e Expr, n *Index) (string, error) {
	// An index that is not a position is asked about before the subscript is lowered at all.
	// The bounds check downstream takes an i32 and never asked what it meant, so `xs["a"]`
	// counted to slot 1 with the text's @str_tab index and printed an element nobody asked
	// for, and `xs[[0]]` lowered a list literal to a @.lstN global reference inside an
	// `icmp slt` — a module llc rejected, exit 2, the code the contract reserves for a
	// compiler bug. The reference raises TypeError naming the type, and so does this (roadmap
	// Gap R.222, ADR 0329). A dict or a set is not subscripted by position and is left to the
	// hashing door beside it (Gap R.217, ADR 0328).
	if g.badIndexGateAtSubscript(b, n) {
		return "0", nil
	}
	// A subscript whose base is a call that hands back a container asks the door the bound form
	// already answers. The callee materialised the object at its `ret` and the answer arrives under a
	// rooted name carrying the notebooks this road consults for a container variable, so `build()[7]`
	// is `ys = build(); ys[7]` written shorter — same read, same bounds check, same catchable raise.
	// Until this arm the fold's own gate condemned the shape because a call is not a literal, which
	// is the base's *spelling* rather than what the compiler holds: the same misattribution ADR 0251
	// fixed for a container the program built, one construct over (roadmap Gap R.214, ADR 0334).
	if redo, okAns := g.containerAnswerSubscript(b, n); okAns {
		return g.value(b, redo)
	}
	// A subscript whose base is an OBJECT with no name — the copy a slice just built, the brace a
	// comprehension's loop built, the list a slot holds — asks the door that names such an object the way
	// the binding road names it. `print(xs[0:2][0])` and `ys = xs[0:2]` / `print(ys[0])` are one read of one
	// object; the fold's gate had been asking for a literal, which is the base's spelling rather than what
	// the compiler holds (roadmap Gap R.235, ADR 0459; the shape of fix ADR 0334 and ADR 0458 record).
	if redo, okCopy := g.containerCopySubscript(b, n); okCopy {
		return g.value(b, redo)
	}
	// A subscript of a string the compiler cannot read asks the table (ADR 0229): the
	// base only has to *be* a string, not be a constant. The fold below still answers
	// everything the compiler can name, so this path is reached exactly when the fold
	// cannot — which used to be a refusal, so `print(s[1])` worked and `print(get()[1])`
	// did not, one rule with two behaviours.
	_, foldBase := g.stringVal(n.Obj)
	_, foldIdx := n.Idx.(*IntLit)
	if !foldBase || !foldIdx {
		if ch, isStr, err := g.emitStrChar(b, n.Obj, n.Idx, n.Span()); err != nil {
			return "", err
		} else if isStr {
			return ch, nil
		}
	}
	// list/dict/set indexing against an inline literal with a constant
	// index/key (this llc build accepts only constant GEP indices).
	// Constant keys/elements are resolved at compile time.
	key := int64(0)
	if il, ok := n.Idx.(*IntLit); ok {
		key = il.Value
	} else {
		// non-literal index: only runtime containers support it (x[a]) — and a dict literal, which is
		// a runtime container the compiler happens to be looking at. Refusing the text key here is what
		// made `print({"a": 1}["a"])` stop the program at exit 1 while `d = {"a": 1}; print(d["a"])`
		// answered `1`: one construct, two doors (roadmap Gap R.215, ADR 0327).
		if nm, ok := n.Obj.(*Name); ok {
			if !g.listVars[nm.Value] && !g.runtimeDicts[nm.Value] && !g.runtimeSets[nm.Value] {
				return "", fmt.Errorf("index must be a constant")
			}
		} else if _, isDictLit := n.Obj.(*DictLit); !isDictLit {
			return "", fmt.Errorf("index must be a constant")
		}
	}
	// A subscript of a string is a one-character string (ADR 0225), and the base the
	// compiler can name is folded: a literal, a variable holding text, a folded
	// concatenation. Negative positions are positions, as ADR 0210 ruled for containers,
	// and the count is code points, which is how `len` and slicing already count. Without
	// this the base only worked as a literal, so `s = "abc"; print(s[1])` refused while
	// `"abc"[1]` answered -- one rule, two behaviours.
	if _, isInt := n.Idx.(*IntLit); isInt {
		if txt, isStr := g.stringVal(n.Obj); isStr {
			runes := []rune(txt)
			i := normPosIndex(key, int64(len(runes)))
			if i < 0 || i >= int64(len(runes)) {
				g.constantTrap(b, "IndexError", stringIndexOutOfRange(), n.Span())
				return "0", nil
			}
			return g.internStr(b, string(runes[i])), nil
		}
	}
	switch obj := n.Obj.(type) {
	case *Name:
		// runtime heap list variable: x[i] reads heap[x].data[i].
		if g.runtimeDicts[obj.Value] {
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
			// The key may be a runtime value (`for k in d: d[k]`), so it is
			// lowered like the list index path does; only a literal key folds. A
			// string key becomes its @str_tab index, because rt_dict_get/rt_dict_has
			// take i32 keys and a global pointer there is what LLVM rejected.
			keyOp := strconv.FormatInt(key, 10)
			// The key is compared as (payload, tag). A string needle's payload is an
			// @str_tab index, and an int key holding the same number is a different key:
			// {1: "one"} asked for "a" must raise KeyError, not answer "one" — which is
			// what an untagged comparison did until the tag was consulted here too
			// (ADR 0232, and ADR 0189's promise that the tag is always there to read).
			keyTag := ""
			if _, isLit := n.Idx.(*IntLit); !isLit {
				kv, isStr, e := g.heapElemKind(b, n.Idx)
				if e != nil {
					return "", e
				}
				if isStr {
					g.dictKeyStr[obj.Value] = true
				}
				keyOp = kv
				if t, ok := g.dictKeyNameTag(b, n.Idx); ok {
					keyTag = t
				} else if isStr {
					keyTag = strconv.FormatInt(int64(TagStr), 10)
				} else {
					keyTag = strconv.FormatInt(int64(TagInt), 10)
				}
			} else {
				// An integer key is a key like any other, and the object answers it with the pair the writer
				// left beside it. The untagged `rt_dict_has`/`rt_dict_get` pair compares PAYLOADS, and the
				// void's payload is 0 — so `{None: "a"}` read by `0` found the entry the program never wrote
				// and handed back the interned index of "a", which `* 2` printed as `0` at the exit code of
				// success while the reference raises `KeyError: 0` (roadmap Gap R.285, ADR 0419). The tag is
				// where the void, the verdict and the number stop colliding: `rt_payload_eq` compares a bool
				// and a number AS numbers, so `d[1]` over `{True: "a"}` stays the one lookup the reference
				// performs, and it compares the void against nothing at all. Same door, one question, for a
				// key spelled a number and a key spelled anything else (ADR 0232's rule, taken to the last
				// road that had not taken it).
				keyTag = strconv.FormatInt(int64(TagInt), 10)
			}
			if keyTag != "" {
				g.checkKeyReadTagged(b, fmt.Sprintf("%%h%d", hs), keyOp, keyTag, n.Idx, n.Span())
				b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, hs, keyOp, keyTag))
			} else {
				g.checkKeyRead(b, fmt.Sprintf("%%h%d", hs), keyOp, n.Idx, n.Span())
				b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get(i32 %%h%d, i32 %s)\n", hs, hs, keyOp))
			}
			return fmt.Sprintf("%%g%d", hs), nil
		}
		if g.listVars[obj.Value] {
			idxOp := strconv.FormatInt(key, 10)
			if _, ok := n.Idx.(*IntLit); !ok {
				v, e := g.value(b, n.Idx)
				if e != nil {
					return "", e
				}
				idxOp = v
			}
			g.heapSeq++
			hs := g.heapSeq
			if g.mixedLists[obj.Value] {
				// A numeric use of an element is not a use that needs the tag. When the container is one the
				// program spelled out and never changed, and the element is a number literal, the slot holds
				// that number: `xs = [1, "a"]; print(xs[0] + 1)` is an addition on 1, and the float form takes
				// the float path with it (roadmap L11.1, ADR 0243). What the promise does not cover — a name
				// element, a runtime index, text or a container in the slot — keeps the refusal below, because
				// for those the tag is exactly what the answer depends on.
				if v, ok := g.numericElemUse(b, n); ok {
					return v, nil
				}
				if v, ok := g.numericSlotUse(b, obj.Value, n); ok {
					return v, nil
				}
				return "", mixedReadErr("list")
			}
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
			idxOp = g.normalizeIndexFor(b, fmt.Sprintf("%%h%d", hs), idxOp, n.Span(), indexOutOfRangeSentenceFor(g.isTupleExpr(obj)))
			b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_get_elem(i32 %%h%d, i32 %s)\n", hs, hs, idxOp))
			return fmt.Sprintf("%%g%d", hs), nil
		}
		if g.dictVals != nil {
			if dl, ok := g.dictVals[obj.Value]; ok {
				return g.dictIndex(dl, key)
			}
		}
		// A name whose container the program spelled out and never changed answers from the
		// literal, whatever the kind maps happen to record: the mixed-list arm above consults this
		// fold and the arms below do not, so a chain — which lowers each operand through an
		// ordinary `value` read rather than through the comparison's own context — reached the
		// refusal for `0 < xs[1] < 3` while the same program written as one comparison did not
		// (roadmap L12.1 / Gap R.53, ADR 0288). Asking the fold here is the choke-point form of
		// ADR 0243's promise: the slot holds that number, so the answer is the number.
		if v, ok := g.numericElemUse(b, n); ok {
			return v, nil
		}
		if v, ok := g.numericSlotUse(b, obj.Value, n); ok {
			return v, nil
		}
		return "", fmt.Errorf("index of a non-literal variable")

	case *Attr:
		// imported module list global (data imports): mod.list[i]
		if nm, ok := obj.Obj.(*Name); ok {
			if globals, ok2 := g.imports.Globals[nm.Value]; ok2 {
				if lit, ok3 := globals[obj.Name.Value]; ok3 {
					if lst, ok4 := lit.(*ListLit); ok4 {
						if key >= 0 && int(key) < len(lst.Elems) {
							return g.value(b, lst.Elems[key])
						}
					}
					if dct, ok4 := lit.(*DictLit); ok4 {
						ks, err := dictLiteralKeys(dct)
						if err != nil {
							return "", err
						}
						vs, err := dictLiteralVals(dct)
						if err != nil {
							return "", err
						}
						for i := range ks {
							if ks[i] == key {
								return fmt.Sprintf("%d", vs[i]), nil
							}
						}
					}
				}
			}
		}
		return "", fmt.Errorf("codegen: index of non-list module attr")
	case *ListLit:
		// index into a list literal: evaluate the element directly. A negative key
		// counts from the end like every other positional subscript (L11.4), and it has
		// to be checked here: this line used to hand `-1` straight to the Go slice and
		// crash the compiler (`panic: runtime error: index out of range [-1]`), which the
		// exit-code contract classifies as a compiler bug (ADR 0168's rule).
		li := int(normPosIndex(key, int64(len(obj.Elems))))
		if li < 0 || li >= len(obj.Elems) {
			// The position is a constant, so this backend is the first to notice it is not there — and
			// notice it the reference does: an IndexError the program's `except IndexError:` can take,
			// not a compile stop that no handler can reach (roadmap Gap R.37, ADR 0326). The sentence
			// names the spelling the source wrote, because `(1, 2)[5]` is not a list running off its
			// end (roadmap L11.3, ADR 0361).
			g.constantTrap(b, "IndexError", indexOutOfRangeSentenceFor(obj.IsTuple()), n.Span())
			return "0", nil
		}
		if !g.listLiteralFoldAnswers(obj, n.Idx) {
			// Two doors, one question — the dict arm above, taken for the other container (ADR 0327).
			// Where the compiler can read the whole literal (every element an integer constant) the read
			// is a compile-time lookup and the program gets the number. Where it cannot, the element's
			// kind lives in the tag the builder wrote beside its payload, and a position that keeps one
			// word for a whole value has nowhere to put it: this road refuses by name rather than hand
			// back an @str_tab index or a @float_box handle wearing a number. print, str()/repr(), an
			// f-string field and a binding ask the pair door instead, which is where the answer is
			// the reference's (roadmap Gap R.218, ADR 0331; the residue is Gap R.146).
			return g.listLiteralRead(b, obj, n.Idx, n.Span())
		}
		return g.value(b, obj.Elems[li])
	case *DictLit:
		// Two doors, one question. Where the compiler can read the whole literal and the key — every key
		// and every value an integer constant, the key an integer literal — the read is a compile-time
		// lookup and the program gets the number. Where it cannot, the read is a message to the object:
		// the same three calls a dict variable's read makes, on a dict the builder just tagged slot by
		// slot, so a text key, a number key, a key of a kind the dict's other entries never held, and a
		// key that is simply absent all get the answer the object gives rather than the answer the
		// spelling suggests (roadmap Gap R.215, ADR 0327).
		if g.dictLiteralFoldAnswers(obj, n.Idx) {
			keys, err := dictLiteralKeys(obj)
			if err != nil {
				return "", err
			}
			vals, err := dictLiteralVals(obj)
			if err != nil {
				return "", err
			}
			// A repeated key in a literal is the SAME key written twice, so the later entry wins the value
			// and the earlier one only contributes its spelling of the key: `{1: 2, 1: 3}[1]` is `3` in the
			// reference, which is what a reverse scan says and a forward one gets backwards. The dict is
			// built by insertion, so the object already answered this correctly — the fold was the half that
			// had never been asked (roadmap Gap R.216, ADR 0327).
			for i := len(keys) - 1; i >= 0; i-- {
				if keys[i] == key {
					return fmt.Sprintf("%d", vals[i]), nil
				}
			}
			// A constant key the literal does not hold is the KeyError the reference raises, with the key
			// itself as its message — the same sentence the dynamic read of a heap dict raises through
			// `checkKeyRead`, so one program gets one message whichever way the compiler reached it
			// (roadmap Gap R.37, ADR 0326).
			g.constantTrap(b, "KeyError", g.keyNotFoundMessage(n.Idx), n.Span())
			return "0", nil
		}
		return g.literalDictRead(b, obj, n.Idx, n.Span())
	case *SetLit:
		// constant membership lookup: return the element if present.
		elems, err := setLiteralElems(obj)
		if err != nil {
			return "", err
		}
		for _, el := range elems {
			if el == key {
				return fmt.Sprintf("%d", key), nil
			}
		}
		return "", fmt.Errorf("not in set")
	case *Comp:
		// indexing into a lowered comprehension result. The struct shape
		// depends on the comprehension kind:
		//   - list: positional GEP+load into {i32, [n x i32]}.
		//   - set:  membership test against the folded elements.
		//   - dict: constant key lookup against the folded key/value pairs.
		name, err := g.comp(b, obj)
		if err != nil {
			return "", err
		}
		if g.rtComps[obj] {
			// TWO comprehensions, one door — the rule **ADR 0442** wrote for `len`, asked at the SUBSCRIPT
			// (roadmap **Gap R.501**). A comprehension the FOLD answered is the unrolled global below, whose count
			// and slots this pass can read. A comprehension the RUN time BUILDS is a heap object in a register:
			// there is no global, `g.compLen`/`g.compKeys`/`g.compEls` hold nothing, and the constant road below
			// bounds-checked the position against a count of ZERO — so `print([g(x) for x in [1, 2, 3]][1])` died
			// with `IndexError: list index out of range` at exit **3** for the reference's `4`, and every element
			// of a built comprehension was out of range. The read is the one a container VARIABLE gets
			// (**ADR 0334**'s call-result arm, **ADR 0241**'s pair): the object answers its own length, its slot,
			// and the tag beside it, and the program's own accident (a position past the end) is the error that
			// arrives (**ADR 0188**'s rule about what may sit in an operand, **ADR 0443**'s brace for the kind).
			kind, okKind := compContainerKind(obj)
			if !okKind {
				return "", fmt.Errorf("index of a comprehension the run time builds needs the kind its brace states (roadmap Gap R.501)")
			}
			if kind == "set" {
				// The brace is the answer: a set has no position, and the program that wrote one gets the
				// reference's raise rather than a compiler refusal or a membership answer borrowed from the
				// slot-extension road (roadmap Gap R.501, Gap R.217's sentence for this spelling).
				g.constantTrap(b, "TypeError", notSubscriptableSentence("set"), n.Span())
				return "0", nil
			}
			v, _, okRead := g.containerSlotRead(b, name, kind, n.Idx, n.Span())
			if !okRead {
				return "", fmt.Errorf("index of a comprehension the run time builds could not be read from the object it built (roadmap Gap R.501)")
			}
			return v, nil
		}
		switch obj.Kind {
		case CompList:
			n := g.compLen[obj]
			key = normPosIndex(key, int64(n))
			if key < 0 || key >= int64(n) {
				g.constantTrap(b, "IndexError", listIndexOutOfRange(), obj.Span())
				return "0", nil
			}
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 1, i32 %d)\n", v, n, n, name, key))
			return v, nil
		case CompSet:
			// set membership: `s[key]` returns the element if present,
			// otherwise errors (interpreter semantics). comp() above populated
			// compEls and emitted the set global.
			for _, el := range g.compEls[obj] {
				if el == key {
					return fmt.Sprintf("%d", key), nil
				}
			}
			return "", fmt.Errorf("not in set")
		case CompDict:
			// dict lookup: `d[key]` returns the mapped value, else errors.
			// comp() above populated compKeys/compEls and emitted the dict global.
			for i, k := range g.compKeys[obj] {
				if k == key {
					return fmt.Sprintf("%d", g.compEls[obj][i]), nil
				}
			}
			return "", fmt.Errorf("key not found")
		default:
			return "", fmt.Errorf("unsupported comprehension kind %d", obj.Kind)
		}
	case *StrLit:
		str, ok := g.stringVal(obj)
		if !ok {
			return "", fmt.Errorf("string index on non-constant string")
		}
		// The same rule as the interpreter (ADR 0225): a subscript of a string is a
		// one-character string, so it comes back as an @str_tab index, and it is counted in
		// code points. `%d`-ing the byte made `s[1] == "b"` compile and answer false -- a wrong
		// value, not a refusal, which is the worst thing this backend can do.
		runes := []rune(str)
		key = normPosIndex(key, int64(len(runes)))
		if key < 0 || key >= int64(len(runes)) {
			g.constantTrap(b, "IndexError", stringIndexOutOfRange(), n.Span())
			return "0", nil
		}
		return g.internStr(b, string(runes[key])), nil
	case *Call:
		// Element access into list-producing call expressions: keys(),
		// values(), sorted(...), reversed(...), split(...). partition()
		// returns only dummy length elems (see dictMethodElems), so it
		// is excluded here to avoid silently wrong results.
		if elems, ok2 := g.indexListElems(obj); ok2 {
			key = normPosIndex(key, int64(len(elems)))
			if key < 0 || int(key) >= len(elems) {
				g.constantTrap(b, "IndexError", listIndexOutOfRange(), n.Span())
				return "0", nil
			}
			return g.value(b, elems[key])
		}
		// The base is an expression rather than a name the compiler holds (`t[0][0]`, `d["a"][1]`,
		// `xs[0][0]`). Same rule as the mixed-list arm above: a literal number in a slot of a container
		// the program spelled out and never changed *is* that number, and the arithmetic the language
		// already has runs on it (roadmap L11.1, ADR 0243).
		if v, ok := g.numericElemUse(b, n); ok {
			return v, nil
		}
		if kind, isConstant := g.constantSubscriptKind(n.Obj); isConstant {
			g.constantTrap(b, "TypeError", notSubscriptableSentence(kind), n.Span())
			return "0", nil
		}
		return "", g.slotReadRefusal(n, "index")
	default:
		if v, ok := g.numericElemUse(b, n); ok {
			return v, nil
		}
		// The base is a CONSTANT because the fold already answered the read underneath it:
		// `print([[1, 2][0][0], 1])` and `print([1, 2, 3][0][0])` leave `1[0]` standing where a chain of
		// two reads was written, and the reference answers that with a catchable TypeError rather than a
		// build that stops. The refusal below blames a literal the program did write, which sends the
		// reader after the wrong half (roadmap Gap R.227, ADR 0440).
		if kind, isConstant := g.constantSubscriptKind(n.Obj); isConstant {
			g.constantTrap(b, "TypeError", notSubscriptableSentence(kind), n.Span())
			return "0", nil
		}
		return "", g.slotReadRefusal(n, "index")
	}
}

// valueSlice is the road a slice takes: the three optional bounds, the fold that answers them at
// compile time, and `rt_slice` when the compiler cannot (the index arithmetic is fold.go's).
func (g *irGen) valueSlice(b *strings.Builder, e Expr, n *Slice) (string, error) {
	// A slice of a call's container answer asks the road that slices a container variable, by naming the
	// slot the answer was recorded under. `rt_slice` copies the slots with their tags and the binding road
	// records the copy, so `print(build()[0:2])` and `ys = build(); print(ys[0:2])` are one program written
	// two ways — the base's spelling stopped being the relevant fact the day the answer became an object
	// (roadmap Gap R.230, ADR 0335; the answer itself is ADR 0333, the index door ADR 0334).
	if redo, okAns := g.containerAnswerSubscript(b, n); okAns {
		return g.value(b, redo)
	}
	// String slices are folded at compile time via stringVal; the fold is a string
	// value, so it is emitted as its @str_tab index -- the pointer is for printf
	// contexts, which fold the text themselves (Gap R.42, ADR 0224).
	// The gate is "is this a string?", not "can the compiler read its text?": a slice of a
	// string the emitter only knows the *kind* of (`s = "abcdef"` inside a function body)
	// still has an answer, and the table gives it (ADR 0229).
	_, objIsFoldedStr := g.stringVal(n.Obj)
	if objIsFoldedStr || g.exprIsString(n.Obj) {
		// A text slice counts characters, so its bounds are positions and the reference's sentence for a
		// bound that is neither an integer nor `None` is the same one a list's slice gets. Asked before the
		// fold and before the helper, because both read a bound as a number without asking what it holds:
		// `print(s["a":"b"])` over `s = "abcd"` answered `b` at exit 0 (roadmap Gap R.233, ADR 0336).
		if g.badSliceBoundGate(b, n) {
			return "0", nil
		}
		// A step of zero is the reference's `ValueError: slice step cannot be zero`, and the fold reads a
		// step as a stride without asking whether it strides at all — asked here, before the fold and
		// before the helper, so the shape that used to be refused with `cannot fold string slice` is the
		// program's exception instead (roadmap Gap R.236, ADR 0336).
		if g.sliceStepIsKnownZero(n) {
			g.raiseSliceStepIsZero(b, n)
			return "0", nil
		}
		if sv, ok := g.stringVal(n); ok {
			return g.internStr(b, sv), nil
		}
		// Bounds that are values rather than constants are still a slice of the same
		// string: an absent bound is spelled with the helper's own sentinel, because an
		// i32 argument cannot be "missing" (ADR 0229).
		if g.sliceBoundIsAbsent(n.Step) {
			if sreg, sok, serr := g.strReg(b, n.Obj); serr != nil {
				return "", serr
			} else if sok {
				lo := "-2147483648"
				hi := "2147483647"
				if !g.sliceBoundIsAbsent(n.Low) {
					v, lerr := g.strPosReg(b, n.Low)
					if lerr != nil {
						return "", lerr
					}
					lo = v
				}
				if !g.sliceBoundIsAbsent(n.High) {
					v, herr := g.strPosReg(b, n.High)
					if herr != nil {
						return "", herr
					}
					hi = v
				}
				r := g.rtStrCall(b, "rt_str_slice", "i32 "+sreg, "i32 "+lo, "i32 "+hi)
				g.checkStrSentinels(b, r, "IndexError", "string slice out of range", n.Span(), "stslice")
				return r, nil
			}
		}
		return "", fmt.Errorf("cannot fold string slice")
	}
	// The copy is only as good as the description of what it copies: `rt_slice` needs a heap list, and the
	// roads that read the copy need to be told whether its slots describe themselves. Asked of a base
	// neither record describes, the road used to emit the copy over whatever word the base lowered to and
	// the caller printed that word — a heap handle wearing the program's answer (`print(m()["k"][0:1])`
	// printed `3`, exit 0). The refusal names the missing record (ADR 0249's rule; roadmap Gap R.235,
	// ADR 0335).
	if !g.sliceBaseIsDescribed(n.Obj) {
		// The base may still BE a container the run time built — the object a comprehension's loop wrote, or
		// the list a slot holds — with no name standing in front of it. Ask the door that gives such an object
		// the name the binding road would have given it, and slice that; `print(m()["k"][0:1])` and
		// `ys = m()["k"]` / `print(ys[0:1])` become one program written two ways (roadmap Gap R.235, ADR 0459).
		if redo, okCopy := g.containerCopySliceBase(b, n); okCopy {
			return g.value(b, redo)
		}
		return "", fmt.Errorf("cannot slice %s: the copy is a list whose slots are the base's payloads with their tags, and the base is not a list the records describe — a literal, a variable a binding recorded as a list, a call that hands back a list, or a slice of one (roadmap Gap R.235, ADR 0335)", exprSnippet(n.Obj))
	}
	// The base is a container the records describe; now the three bounds are asked the same question the
	// subscript door asks its position (Gap R.222's gate, one construct over): an integer or a bool is a
	// position, `None` is the absence of one, and anything else the compiler can name the kind of is the
	// reference's `TypeError: slice indices must be integers or None or have an __index__ method`. Asked
	// before any bound is lowered, so a text bound never becomes its `@str_tab` index and a position
	// (`print(xs["a":"b"])` answered `[1]` at exit 0) and a list bound never reaches `rt_slice`'s i32
	// arguments as a global address (exit 2, roadmap Gap R.233, ADR 0336).
	if g.badSliceBoundGate(b, n) {
		return "0", nil
	}
	objReg, err := g.value(b, n.Obj)
	if err != nil {
		return "", err
	}
	// A container LITERAL lowers to the ADDRESS of a compile-time global (`@.lst1`), which is not a
	// heap handle, and `rt_slice` walks the heap — so `print([1, 2, 3][1:])` emitted
	// `call i32 @rt_slice(i32 @.lst1, …)` and llc rejected the compiler's own module (exit 2, which
	// ADR 0166 counts as OUR bug). The comprehension road beside this one already solved it (ADR
	// 0234): ask the literal records, and if the operand is one, BUILD it as a real heap object
	// first and slice that (roadmap Gap R.179).
	// The base may be a literal NODE (`[1, 2, 3][1:]`) or a NAME the records hold a literal for
	// (`xs = [1, 2, 3]` / `xs[1:]`); both lower to a global address and both need a heap object.
	if lit := g.listLiteralOf(n.Obj); lit != nil {
		// The same question the build site asks wherever a list literal becomes an object: do the slots
		// need tags to be understood? A text's word is an interned index, a float's a box handle, a
		// nested container's a handle — all of which the printer can only read from a tag. Asking
		// `literalNeedsHeap` alone built the sliced source untagged, and `rt_slice` then faithfully copied
		// tags nobody had written (roadmap Gap R.230, ADR 0187's pairing rule).
		var berr error
		if g.literalNeedsHeap(lit) || g.literalNeedsTags(lit) || g.literalHasPairElement(lit) {
			built, terr := g.heapListFromTagged(b, lit)
			if terr != nil {
				return "", terr
			}
			objReg, berr = built, nil
		} else {
			objReg, berr = g.heapListFrom(b, lit, "")
		}
		if berr != nil {
			return "", berr
		}
	}
	lowReg := "0"
	highReg := "0"
	stepReg := "1"
	hasLow := "0"
	hasHigh := "0"
	hasStep := "0"
	if !g.sliceBoundIsAbsent(n.Low) {
		lowReg, err = g.value(b, n.Low)
		if err != nil {
			return "", err
		}
		hasLow = "1"
	}
	if !g.sliceBoundIsAbsent(n.High) {
		highReg, err = g.value(b, n.High)
		if err != nil {
			return "", err
		}
		hasHigh = "1"
	}
	if !g.sliceBoundIsAbsent(n.Step) {
		stepReg, err = g.value(b, n.Step)
		if err != nil {
			return "", err
		}
		hasStep = "1"
	}
	if hasStep == "1" {
		// The kind gate above could not ask this: `0` is a perfectly good integer. It is the one
		// value a step may not take, and `rt_slice` walks its own loop forever on it — a run that has
		// to be killed comes out as exit 2, which the contract blames on the compiler (ADR 0166), and
		// that is what `print(xs[::0])` did. The question is asked where the value is, after every
		// bound has been evaluated and before any copy is built, which is the order the reference
		// raises in: a step the compiler can read is settled there and then — a known zero raises with
		// no copy and no counter touched (a gate stays pure, ADR 0273), a known non-zero needs no test
		// at all — and only a step it cannot read carries the run-time guard (roadmap Gap R.236,
		// ADR 0336's neighbour, paid by ADR 0337).
		if v, known := g.sliceStepIsKnown(n); known {
			if v == 0 {
				g.raiseSliceStepIsZero(b, n)
				return "0", nil
			}
		} else {
			g.zeroStepGuardAtSlice(b, stepReg, n.Span())
		}
	}
	r := g.heapSeq
	g.heapSeq++
	// The copy is built inside rt_slice, which allocates through rt_alloc — so a full arena reaches
	// this caller as a negative handle too, and the store that follows the call would be the write
	// below the arena (roadmap Gap Q.2, ADR 0347). The guard stands after every bound has been
	// evaluated and after ADR 0337's zero-step question, which is the order the reference raises in.
	// A slice of the immutable spelling comes back a tuple, because `rt_slice` copies the source
	// object's attribute word along with its slots and tags (ADR 0361) — the door that decides what a
	// value prints is the object, not the road that copied it.
	g.emitHeapCall(b, fmt.Sprintf("%%sl%d", r), fmt.Sprintf("call i32 @rt_slice(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)", objReg, lowReg, highReg, stepReg, hasLow, hasHigh, hasStep), n.Span())
	// The copy is live in a bare register while the rest of the STATEMENT is lowered, and the rest of the
	// statement is free to CALL — `print(xs[0:2], m())` lowers the second argument after the first copy is
	// built, and a call's body collects (ADR 0347, ADR 0317). An unrooted live object is ADR 0181's forbidden
	// shape: the collector does not see the register, the arena recycles the slot to the next `rt_alloc`, and
	// the print answers whatever that allocation wrote there. Measured while landing ADR 0459 —
	// `print(table()["k"][0:1], table()["k"][1:2])` printed `['k'] ['a']` for CPython's `[1.5] ['a']` at exit 0,
	// the copy of a list come back holding the interned index of the dict's own KEY (roadmap Gap R.541, the
	// slice half of the register-liveness class ADR 0443 closed for a comprehension's build).
	g.gcRootSliceCopy(b, fmt.Sprintf("%%sl%d", r))
	return fmt.Sprintf("%%sl%d", r), nil
}
