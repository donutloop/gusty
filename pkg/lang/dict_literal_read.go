// pkg/lang/dict_literal_read.go — a dict literal is read by asking the object (roadmap Gap R.215, ADR 0327).
//
// A subscript has always had two roads in this backend. When the compiler can read the whole thing —
// a literal dict whose keys and values are all integer constants, and an integer key — the read is a
// compile-time lookup and the program gets the number. When it cannot, the read is a *message to the
// object*: the dict is built in the heap, the key arrives as the (payload, tag) pair it stands for, and
// `rt_dict_has_tagged`/`rt_dict_get_tagged`/`rt_dict_value_tag` answer from the slots the builder wrote.
// The second road is the one a dict **variable** has always taken, and it is the one that knows what a key
// is: a text key, a number key, a key of a kind the entry does not share with the rest of the dict, a key
// that is simply not there.
//
// Until now the first road was the only one a literal could reach, because the gate above the subscript
// refused any key that was not an `IntLit` unless the base was a name the generator had registered as a
// runtime container. So `print({"a": 1}["a"])` — the most constant base the language can write — stopped
// the program at exit 1 with `index must be a constant`, while `d = {"a": 1}; print(d["a"])` answered `1`,
// and `print({"a": 1}["b"])` refused where the same key into a dict variable raises `KeyError: 'b'` at
// exit 3. One construct, two doors, and neither answer the reference's. A literal is not a different kind
// of dict; it is a dict the compiler happens to be looking at. So the fold stays where it reads the whole
// thing, and everything else goes down the road the variable already walks — `taggedSlotPair`, the one
// door that answers "which (payload, tag) pair does this subscript denote", with the literal as its base.
//
// That reuse is the whole design. The entry is built by `heapDictFrom` (the binding road's builder, tags
// and all — ADR 0232), the key is lowered by `taggedOperand` (the pair the mixed-container helpers already
// take), the missing key is raised by `checkKeyReadTagged` (the same KeyError, the same landing pad, the
// key's own repr as its message — ADR 0326's door reached from the subscript's other side), and the
// answer's kind comes back in `rt_dict_value_tag` beside the payload rather than being guessed from the
// spelling: ADR 0187's rule that the word which writes a payload writes its tag beside it, read back.
//
// What this file therefore decides is *who may ask the object*, and it is the same split the pair road
// already draws (roadmap Gap R.146):
//
//   - a position that reads a value **by its kind** — print, `str`/`repr`, an f-string field, a binding,
//     a `len`, a membership test, a loop — asks for the pair and is answered;
//   - a position that keeps **one word** for a whole value — a `return`, a call argument, an operand of
//     arithmetic — cannot carry a kind, so it refuses with the missing half named instead of handing out a
//     `@float_box` handle or an `@str_tab` index and calling it a number.
//
// And what stays a refusal even to the pair door is a key whose kind the compiler cannot name (the same
// residue as Gap R.212's untaggable needle) and a dict keyed by a container, which has no hashing rule in
// this language (`heapDictFrom`'s own guard). Both say what is missing rather than compare under a guess.
package lang

import (
	"fmt"
	"strings"
)

// dictLiteralFoldAnswers reports whether the compile-time fold below the subscript can answer this read
// on its own: an integer key, and a literal every key *and* every value of which is an integer constant.
// That is exactly the shape the fold has always answered, and it keeps answering it — the fold emits a
// constant, not an allocation, and `print({1: 2}[1])` has been asserted down to the IR that answers it.
//
// The key question is part of the answer, not an afterthought: `{0: 2}["a"]` has an all-integer literal
// underneath it and a text key above it, and a fold that looked up a text key in an integer table would
// answer 2 where the reference raises `KeyError: 'a'`. A fold may only be asked the questions it can read.
func (g *irGen) dictLiteralFoldAnswers(dl *DictLit, key Expr) bool {
	if _, isInt := key.(*IntLit); !isInt {
		return false
	}
	if _, err := dictLiteralKeys(dl); err != nil {
		return false
	}
	if _, err := dictLiteralVals(dl); err != nil {
		return false
	}
	return true
}

// literalDictPair reads one entry of a dict literal through the one subscript door that answers a
// (payload, tag) pair. It is not a second read: the Index it builds is the shape `taggedSlotPair` asks
// the static door about, whose dict arm is the `rt_dict_has_tagged`/`rt_dict_get_tagged`/
// `rt_dict_value_tag` trio a dict *variable*'s read emits — so `print({"a": "x"}["a"])` and
// `print(d["a"])` cannot grow apart, which is the whole reason this row was a refusal and not a bug list.
func (g *irGen) literalDictPair(b *strings.Builder, dl *DictLit, key Expr, sp Span) (val, tag string, err error) {
	ix := &Index{Obj: dl, Idx: key, Src: sp}
	v, t, ok := g.taggedSlotPair(b, ix)
	if !ok {
		return "", "", literalDictDoorErr(key)
	}
	return v, t, nil
}

// literalDictRead is the one-word answer: the entry's payload, and only for the entries whose payload *is*
// the word — an integer entry. Every other kind needs the tag beside it, and a consumer that cannot carry
// the tag is the position Gap R.146 still owes, so it refuses here rather than print a handle. `print`,
// `str`/`repr`, an f-string field and a binding ask `literalDictPair` instead (roadmap L11.1, ADR 0273's
// boundary drawn one construct earlier than the call).
func (g *irGen) literalDictRead(b *strings.Builder, dl *DictLit, key Expr, sp Span) (string, error) {
	if g.dictLiteralEntryNeedsTag(dl, key) {
		return "", literalDictOneWordErr(key)
	}
	val, _, err := g.literalDictPair(b, dl, key, sp)
	if err != nil {
		return "", err
	}
	return val, nil
}

// dictLiteralEntryNeedsTag answers "could the answer to this read be a value whose kind lives in the tag
// rather than in the word?" — a float box, a None, a bool, an instance, a nested container. A text is not
// on the list because a text's payload *is* its word in this backend: an @str_tab index travels in one i32
// the way it does for every other text-valued expression, and `exprIsString`/`internedVars` carry it.
// Where the key names one entry the question is asked of that entry; where the key is a run-time fact the
// question has to be asked of every entry, because any of them could be the answer, and the honest answer
// about a kind the compiler cannot pin down is "it needs the tag".
func (g *irGen) dictLiteralEntryNeedsTag(dl *DictLit, key Expr) bool {
	t, ok := g.entryKindAt(dl, key)
	if !ok {
		return true
	}
	return t != int32(TagInt) && t != int32(TagStr)
}

// literalDictReadOf decomposes the subscript this file routes: "is this a read whose base is a dict
// literal?", handing back the literal and the key. Every door that asks the object asks it through this
// question, so the gate and the door cannot disagree about which expressions are in question (ADR 0249's
// lesson: a gate and a door that test different things ship a refusal nobody measured).
func literalDictReadOf(e Expr) (dl *DictLit, key Expr, ok bool) {
	ix, isIx := e.(*Index)
	if !isIx {
		return nil, nil, false
	}
	dl, isLit := ix.Obj.(*DictLit)
	if !isLit {
		return nil, nil, false
	}
	return dl, ix.Idx, true
}

// dictLiteralEntryIsPlainInt reports whether the entry this read answers travels in one word — an integer
// entry, whose payload IS the number. That is the whole difference between the positions that can take a
// dict literal's read across a function boundary and the ones that cannot: an @str_tab index, a @float_box
// handle, a bool's 0/1 and a nested container's handle all mean something only beside their tag.
func (g *irGen) dictLiteralEntryIsPlainInt(dl *DictLit, key Expr) bool {
	t, ok := g.entryKindAt(dl, key)
	return ok && t == int32(TagInt)
}

// entryKindAt is the tag question asked of the entry a read names. A key the compiler cannot read makes
// every entry a candidate, and a candidate set whose tags disagree has no single kind.
func (g *irGen) entryKindAt(dl *DictLit, key Expr) (int32, bool) {
	if i, ok := dictLiteralKeyIndex(dl, key); ok {
		return g.elemKindTag(dl.Vals[i])
	}
	var seen int32 = -1
	for _, v := range dl.Vals {
		t, ok := g.elemKindTag(v)
		if !ok {
			return 0, false
		}
		if seen >= 0 && seen != t {
			return 0, false
		}
		seen = t
	}
	if seen < 0 {
		return 0, false
	}
	return seen, true
}

// literalDictDoorErr is the refusal for a key whose kind the compiler cannot name, so no tag can travel
// beside it and the entry cannot be found by the pair rule.
func literalDictDoorErr(key Expr) error {
	return fmt.Errorf("codegen: reading a dict literal by a key the compiler cannot type (%s) needs the key's kind to travel with the key; a dict literal has no variable for the tag to be recorded beside, so this door refuses rather than compare a payload under a guessed tag (roadmap Gap R.215, Gap R.212)", exprSnippet(key))
}

// literalDictOneWordErr is the refusal for the position that keeps one word for a whole value. It names
// both halves of what is missing — where the value came from, and which word the caller is short of —
// because a refusal that does not say what it needs is a dead end for the person who reads it and for the
// agent that has to plan around it (the rule every refusal in this backend owes, ADR 0181's wording rule).
func literalDictOneWordErr(key Expr) error {
	return fmt.Errorf("codegen: reading %s of a dict literal answers an entry whose kind the object chooses, so the answer is a (payload, tag) pair; this position keeps one word for a whole value and would read a text's table index, a float's box handle or a container's handle as a number — print, str(), repr(), an f-string field and a binding all carry the pair (roadmap L11.1, Gap R.146, Gap R.215)", exprSnippet(key))
}

// dictLiteralValueIsString answers the printer's question about a read of a dict literal — "is the answer
// a text?" — by finding the entry the key names and asking that value. It answers true only when the key
// names exactly one entry and that entry's value is a text; an absent key has no answer to give about a
// kind, and an entry whose value is a number is not text whatever the dict's other entries hold.
//
// The question has to be answerable per entry: the dict-wide `dictValStr` bookkeeping the variable road
// leans on records "some value of this dict is text", which is enough for a container that has a name and
// never enough for a literal that has no name and one read.
func (g *irGen) dictLiteralValueIsString(dl *DictLit, key Expr) bool {
	i, ok := dictLiteralKeyIndex(dl, key)
	if !ok {
		return false
	}
	v := dl.Vals[i]
	if g.exprIsString(v) || g.printsAsInternedStr(v) {
		return true
	}
	if _, isStr := g.stringVal(v); isStr {
		return true
	}
	return false
}

// dictLiteralKeyIndex resolves a constant key against a dict literal's keys with the reference's own
// key-identity rule: `1`, `1.0` and `True` are one key, and a text is never one of them. That rule is why
// this cannot be a `==` on the spellings — `{1: "a"}[True]` answers `"a"` in the reference, and a spelling
// comparison says the key is absent.
func dictLiteralKeyIndex(dl *DictLit, key Expr) (int, bool) {
	k, ok := constantKeyOf(key)
	if !ok {
		return 0, false
	}
	for i, kk := range dl.Keys {
		if other, ok := constantKeyOf(kk); ok && other.sameAs(k) {
			return i, true
		}
	}
	return 0, false
}

// The three key families the compiler can read a dict literal by. A container key is deliberately absent:
// it has no hashing rule in this language, and `heapDictFrom` refuses the dict before this door is reached.
const (
	ckNone = iota
	ckNum
	ckStr
)

// constKey is a dict key the compiler can read — one entry in the reference's own identity space. Numbers
// keep both words they can be written with, because `1`, `True` and `1.0` are one key in the reference and
// three spellings of it: two integers compare as integers (exactly, whatever their size) and only a key
// written as a float reaches the float comparison, which is where the reference's own precision boundary is.
type constKey struct {
	kind  int
	num   float64
	i     int64
	isInt bool
	text  string
}

func (k constKey) sameAs(o constKey) bool {
	if k.kind != o.kind {
		return false
	}
	switch k.kind {
	case ckStr:
		return k.text == o.text
	case ckNum:
		if k.isInt && o.isInt {
			return k.i == o.i
		}
		return k.num == o.num
	}
	return true // None is None
}

// constantKeyOf reads an expression as the key it would be. The kinds it reads are the ones a dict key can
// be written as in source and still be a constant: an integer, a float, a bool, a text, `None`. Anything
// else — a name whose kind is a run-time fact, a call, a container — answers false, and the caller either
// takes the dynamic road or refuses.
func constantKeyOf(e Expr) (constKey, bool) {
	switch k := e.(type) {
	case *IntLit:
		return constKey{kind: ckNum, num: float64(k.Value), i: k.Value, isInt: true}, true
	case *BoolLit:
		v := int64(0)
		if k.Value {
			v = 1
		}
		return constKey{kind: ckNum, num: float64(v), i: v, isInt: true}, true
	case *FloatLit:
		return constKey{kind: ckNum, num: k.Value}, true
	case *StrLit:
		return constKey{kind: ckStr, text: k.Value}, true
	case *NoneLit:
		return constKey{kind: ckNone}, true
	}
	return constKey{}, false
}
