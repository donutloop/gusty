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
//   - a value with NO slots to slice — a number, a bool, the singleton — gets `'int' object is not
//     subscriptable` and its three siblings, asked through the subscript road's own classifier so
//     `print(1[0])` and `print(1[0:1])` are one complaint rather than two doors' (**Gap R.559**,
//     **ADR 0420**); yesterday the copy road refused those programs with a sentence about a list nobody
//     wrote.
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
	// The base that has NO slots is asked first, because it is the base that never gets to the question
	// the two containers above answer: a set complains about the object and a dict hashes the slice, but a
	// number, a bool and the singleton have nothing to slice INTO, so their complaint is the object's and
	// nothing else. It is asked through the subscript road's own door, so `print(1[0])` and `print(1[0:1])`
	// are one program's complaint rather than two doors' (roadmap **Gap R.559**, **ADR 0420**'s one question
	// asked once, beside **ADR 0466**'s base-first order).
	if kind, noSlots := g.sliceBaseWithNoSlots(n.Obj); noSlots {
		g.heapUsed = true
		g.raiseTo(b, exnCode("TypeError"), "TypeError", notSubscriptableSentence(kind), n.Span())
		cont := g.newLabel("sbb.ns")
		b.WriteString(fmt.Sprintf("%s:\n", cont))
		return true
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

// sliceBaseWithNoSlots names the kind of a base that has no slots to slice at all: a number, a bool, the
// singleton. The reference refuses every one of them by naming the kind — `print(1[0:1])` is
// `TypeError: 'int' object is not subscriptable`, `print(None[::2])` is `'NoneType' object is not
// subscriptable` — and this backend refused the same program with the copy road's complaint about a list the
// program never wrote, which is a refusal where the program raises (roadmap **Gap R.559**):
//
//	print(1[0:1])       # TypeError: 'int' object is not subscriptable
//	print(1.5[0:1])     # TypeError: 'float' object is not subscriptable
//	b = True            # TypeError: 'bool' object is not subscriptable
//	print(b[0:1])
//	o = None            # TypeError: 'NoneType' object is not subscriptable
//	print(o[:])
//
// The kinds are read from the doors that already own them, never from the spelling:
//
//   - `constantSubscriptKind` is the subscript road's own classifier — the one that already raises
//     `'int' object is not subscriptable` for `print(1[0])` (**ADR 0420**, Gap R.227's folded element read
//     included, so `xs = [1, 2]` / `print(xs[0][0:1])` is the same complaint as `print(1[0:1])`). Asking it
//     again here is the point: a second classifier would be a second answer.
//   - the last-binding records `nonIntIndexTypeName` reads (**ADR 0336**'s classifier, **ADR 0172**/ADR 0257's
//     rule) answer for a NAME the program last filled with a float, a bool or the singleton. There is no
//     record for a name last filled with an INT — ints are the default word, and nothing writes a claim — so
//     `n = 1` / `print(n[0:1])` stays the copy road's refusal rather than being accused of a kind nobody
//     wrote, and **Gap R.560** owns it.
//
// A base any container door answers, and a text, answer nothing here: those are the bases the slice road
// serves, and the ones ADR 0466 already words.
func (g *irGen) sliceBaseWithNoSlots(e Expr) (string, bool) {
	if g == nil || e == nil {
		return "", false
	}
	// The container notebooks are asked FIRST, and that order is a safety rule rather than a preference:
	// `floatVars`/`boolVars`/`noneVars` are written by the scalar binding road but a container binding that
	// takes the name over does not clear them (`recordNoneAndBool`'s `delete` clears the pair for the
	// singleton family only when the binding is not a container rebind, and `bindFloatValue` clears the
	// tagged pair, not `listVars`). Asking the scalar records first would take an answer away from a program
	// that compiles today — `xs = 1.5` / `xs = [1, 2]` / `print(xs[0:1])` answers `[1]` in the reference and
	// must keep answering it (ADR 0273's rule that a gate may not cost a program that compiles). The
	// residue — a name a container binding filled and a scalar binding later rebound — is **Gap R.561**.
	if _, known := g.staticContainerKind(e); known {
		return "", false
	}
	if g.isTupleExpr(e) {
		return "", false
	}
	if _, isText := g.stringVal(e); isText {
		return "", false
	}
	if g.exprIsString(e) {
		return "", false
	}
	if nm, isName := e.(*Name); isName {
		switch {
		case g.boolVars[nm.Value]:
			return "bool", true
		case g.noneVars[nm.Value]:
			return "NoneType", true
		case g.floatVars[nm.Value]:
			// The double the last binding stored has no tag to read, and the copy road cannot copy it: the
			// reference refuses the object, and so does this door (ADR 0274's double slot).
			return "float", true
		}
	}
	// The subscript road's own classifier, asked again rather than reinvented: it names the kinds the
	// source WROTE, and answers nothing where a kind would be invented.
	if kind, isConstant := g.constantSubscriptKind(e); isConstant {
		return kind, true
	}
	return "", false
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
