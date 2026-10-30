package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// A slice whose base is not a sequence the language slices.
//
// `rt_slice` walks a heap list and copies its slots, so the slice road is a road about a container
// that HAS positions. Until this row it never asked the base what it was:
//
//	print({1, 2}[0:1])              # TypeError: 'set' object is not subscriptable
//	                              # this toolchain: `cannot slice a set literal` at exit 1 — and, before
//	                              # ADR 0335's gate, `@.set1` handed to an i32 argument: exit 2
//	print({1: 2}[0:1])              # KeyError: slice(0, 1, None)
//	                              # this toolchain: the same refusal, and `[1]` at exit 0 before the gate
//	s = {1, 2}
//	print(s[0:1])                   # the set's sentence again — the base, not the bounds, is the complaint
//	d = {1: 2}
//	print(d["a":"b"])               # KeyError: slice('a', 'b', None) — a dict never asks a bound to be a
//	                              # position; it hashes the slice, and a text is hashable
//
// The bounds cannot answer this. `badSliceBoundGate` (Gap R.233, ADR 0336) asks what a bound IS, and
// its sentence is the LIST's complaint — the one the reference makes when a container that counts is
// handed a bound it cannot count with. A set has nothing to count with and a dict does not count at
// all, so for those two bases the reference never reaches the bound question: it complains about the
// object, or it hashes the slice and reports the miss by naming the key.
//
// So the two questions have an order, and it is the reference's: the object first, the bounds second.
// `staticContainerKind` (the door the comparison, the membership test and the loop all decide with)
// names the base's kind where a record names it, and:
//
//   - a set gets `'set' object is not subscriptable` — the same sentence `unhashable_key.go`'s door
//     quotes through `notSubscriptableSentence`, so one program gets one message whichever road read
//     the subscript;
//   - a dict gets `KeyError: slice(0, 1, None)`: the slice IS the key, the dict hashes it, finds no
//     entry, and names the key it missed. A bound the language refuses to hash (`d[[1]:2]`) is the
//     hashing door's own complaint first — `TypeError: unhashable type: 'list'` — asked of the three
//     bounds in the order the reference hashes them, which is the order the source wrote them;
//   - every other kind is left exactly where it was, including the refusal. A base whose kind only the
//     run time can describe keeps the road it has always had rather than being accused of a kind
//     nobody wrote (ADR 0273's rule that a gate may not cost a program that compiles today, and
//     ADR 0232's rule that a kind claim is imported from a writer, never invented here).
//
// A bound that would itself raise (`d[1//0:1]`, the reference's ZeroDivisionError) is a question the
// gate does not reach: it fires only where every bound is a value the compiler can read, so the
// raise it emits can never outrank a raise that lives inside a bound the reference evaluates first.
// That residue is filed, not fixed.

// sliceRaisesWhatTheBaseIs asks the slice road, before any bound is lowered, whether the base is one
// of the two containers the language refuses to slice. It reports whether it raised, so the caller
// returns the (unreachable) word the trap makes and keeps emitting in the reopened block.
func (g *irGen) sliceRaisesWhatTheBaseIs(b *strings.Builder, n *Slice) bool {
	if n == nil {
		return false
	}
	kind, known := g.staticContainerKind(n.Obj)
	if !known {
		return false
	}
	switch kind {
	case "set":
		// A set has no position to slice from, and the complaint is about the object: no bound is
		// asked anything, which is why `s["a":"b"]` says `'set'` and not the slice-indices sentence.
		g.heapUsed = true
		g.raiseTo(b, exnCode("TypeError"), "TypeError", notSubscriptableSentence("set"), n.Span())
		cont := g.newLabel("sbb.cont")
		b.WriteString(fmt.Sprintf("%s:\n", cont))
		return true
	case "dict":
		// The slice is the key: the dict hashes it and reports the miss by naming it. The hash is asked
		// of FIRST, because hashing is what the reference does before it looks for the entry — `d[[1]:2]`
		// never reaches the miss, it reaches `TypeError: unhashable type: 'list'`. Reading the miss first
		// instead would answer a question the reference does not allow to be asked (the standing rule of
		// the hashing door beside it, **ADR 0328**).
		if typeName, unhashable := g.unhashableSliceBound(n); unhashable {
			g.heapUsed = true
			g.raiseTo(b, exnCode("TypeError"), "TypeError", unhashableKeyMessage(typeName), n.Span())
			cont := g.newLabel("sbb.hv")
			b.WriteString(fmt.Sprintf("%s:\n", cont))
			return true
		}
		key, okKey := g.sliceAsDictKey(n)
		if !okKey {
			// A bound the compiler cannot read has no repr to name, and a fabricated key is the
			// defect class this repo files (ADR 0273). The road keeps its own refusal.
			return false
		}
		g.heapUsed = true
		g.raiseTo(b, exnCode("KeyError"), "KeyError", key, n.Span())
		cont := g.newLabel("sbb.k")
		b.WriteString(fmt.Sprintf("%s:\n", cont))
		return true
	}
	return false
}

// unhashableSliceBound asks the three bounds in the order the reference hashes them — low, high, step
// — whether one of them is a value the language refuses to hash, and answers the type the reference
// names. `d[[1]:"a"]` says 'list': the tuple hash walks left to right and names the first member it
// cannot hash, so the order the source wrote the bounds in is the order the complaint comes in.
func (g *irGen) unhashableSliceBound(n *Slice) (string, bool) {
	for _, bound := range []Expr{n.Low, n.High, n.Step} {
		if typeName, bad := g.unhashableKeyName(bound); bad {
			return typeName, true
		}
	}
	return "", false
}

// sliceAsDictKey renders the KeyError's message: the repr of the slice object the subscript built.
// The reference spells an omitted bound and a `None` bound the same way — `slice(None, None, None)` —
// and quotes a text bound the way `repr` quotes a text, because a KeyError names its key (the same
// rule `keyNotFoundMessage` follows for an entry that is not there). It declines for a bound whose
// value the compiler cannot read, which is the reference's `slice(?, ...)` the front end has no
// evidence for.
func (g *irGen) sliceAsDictKey(n *Slice) (string, bool) {
	parts := make([]string, 0, 3)
	for _, bound := range []Expr{n.Low, n.High, n.Step} {
		if g.sliceBoundIsAbsent(bound) {
			parts = append(parts, "None")
			continue
		}
		if _, bad := g.unhashableKeyName(bound); bad {
			return "", false // the hashing door owns this bound; it is not a repr question
		}
		repr, ok := g.sliceBoundRepr(bound)
		if !ok {
			return "", false
		}
		parts = append(parts, repr)
	}
	return "slice(" + strings.Join(parts, ", ") + ")", true
}

// sliceBoundRepr is the repr of one bound as the reference would write it, or false when no record
// says what the bound holds. Every arm reads a record the read roads already ask — the same notebooks
// `keyNotFoundMessage` quotes for a missing dict key — so the traceback cannot disagree with a lookup
// that already missed, and nothing here infers a kind from the spelling alone.
func (g *irGen) sliceBoundRepr(e Expr) (string, bool) {
	switch k := e.(type) {
	case nil:
		return "", false
	case *IntLit:
		return strconv.FormatInt(k.Value, 10), true
	case *FloatLit:
		return pyFloatRepr(k.Value), true
	case *BoolLit:
		if k.Value {
			return "True", true
		}
		return "False", true
	case *StrLit:
		return pyReprString(k.Value), true
	case *NoneLit:
		return "None", true
	case *Tuple:
		return literalKeyRepr(k)
	case *ListLit:
		// A bound written in parentheses is a `ListLit` the parser flagged, not a `Tuple` node: the one
		// container layout this backend gives both spellings (**ADR 0361**). The reference hashes a
		// parenthesised bound and names it in the miss — `KeyError: slice((1, 'a'), 2, None)` — so the
		// repr is the tuple's own, commas and all, while a bracketed bound stays unhashable's business.
		if k.IsTuple() {
			return literalKeyRepr(k)
		}
		return "", false
	case *DictLit, *SetLit:
		return "", false // an unhashable bound never reaches a repr
	case *Name:
		// The notebooks the missing-key door already asks, in the same order, so one program gets
		// one answer from the two doors that name a value the source wrote.
		switch {
		case g.noneVars[k.Value]:
			return "None", true
		case g.boolVars[k.Value]:
			if v, ok := g.foldConstInt(k); ok {
				if v != 0 {
					return "True", true
				}
				return "False", true
			}
			return "", false
		case g.strVals[k.Value] != "":
			return pyReprString(g.strVals[k.Value]), true
		}
		if v, ok := g.foldConstInt(k); ok {
			return strconv.FormatInt(v, 10), true
		}
		if g.tupleVars != nil && g.tupleVars[k.Value] {
			if lit, has := g.containerLits[k.Value]; has {
				return literalKeyRepr(lit)
			}
		}
		return "", false
	default:
		return "", false
	}
}
