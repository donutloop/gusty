// chain_operand.go — the ONE question a comparison chain asks of each of its operands, asked before
// anything is lowered: *can this value travel in the chain's single i32 slot?*
//
// `chainValue` (codegen_arith.go) stores every operand once into a fresh local and lets each link read
// the slot back, which is what makes "the middle operand is evaluated exactly once" a property of the
// emitted module (L12.1 / Gap R.53, ADR 0288). The store, however, was written for one shape of value:
//
//	store i32 %w, i32* %_chain1
//
// Three kinds of operand do not fit that word, and until this file the chain found out by lowering them
// through the ordinary one-word road:
//
//   - a **slot read whose kind the object carries** — `xs[0]` of a list a store promoted, `d["a"]` of a
//     dict whose values are not all one kind. The ordinary road either REFUSES (`this context needs a
//     single static kind`) or, for a dict slot, hands back the PAYLOAD alone: the `@float_box` handle of
//     a float entry, the `@str_tab` index of a text entry, the void's `0`. Each of those is a number for
//     reasons that have nothing to do with the value, which is how `d = {"a": 2.5}` / `print(0 < d["a"] < 3)`
//     answered `False` and `d = {"a": "x"}` / `print(0 < d["a"] < 3)` answered `True` — both at the exit
//     code of success, where the reference prints `True` and raises `TypeError` (roadmap Gap R.442).
//   - a **name the pair road bound** — `n = xs[0]` then `0 < n < 3`. The value slot only means something
//     together with its tag, so the ordinary read refuses outright (Gap R.146's standing).
//   - a **double** — `0 < a + b < 4`, `0 < abs(-2.5) < 3`, `0 < g() < 3` of a float-returning `def`. The
//     value is an LLVM `double`; the slot is four bytes. The module either reached `llc` as
//     `store i32 %t5, i32* %_chain1` — the contract's exit 2, spent on the compiler's own module (ADR 0166) —
//     or the double was truncated into the i32 and `print(0 < a + b < 4)` answered `False` for `a = 1.0`,
//     `b = 2.0` at the exit code of success.
//
// What the three have in common is that the position keeps one word for a value that arrives with more
// than one: exactly **ADR 0331**'s tagged value word, missing at one position. So this file asks the
// operand the one question, in the order the neighbouring positions ask it — the comparison road's own
// pair door, then the binding road's ordered list (assign_pair.go), then the float road's — and when the
// answer is a pair or a double it binds the chain's slot with THAT, through the same binding doors an
// ordinary assignment uses (`bindAssignPair`, `bindFloatValue`). Each link then reads a NAME, which is
// what the comparison roads were written to interrogate: a pair-bound name is asked its tag by
// `mixedTaggedCompare` / `taggedOrderApplies` / `pairOrder`, and a float name is asked by the float arms.
// No second comparison road is written here, and none is widened: the chain stops throwing the second
// word away.
//
// Every door is allowed to decline, and declining is the common case — `0 < 2 < 3`, `0 < x < 3`, a slot
// of a container the literal still describes all keep the road they have always had, byte for byte. That
// is why the gate is asked first and is pure: a door that lowers into the module and then changes its
// mind would leave the operand evaluated twice, which is the very bug ADR 0288 exists to prevent. So each
// arm builds into a scratch buffer and is committed only once it has answered (the same discipline
// `mixedTaggedCompare` learned for the same reason, ADR 0166).
//
// roadmap L11.1 / Gap R.442 (the pair and the double), L12.1 / Gap R.53 (the chain), ADR 0331
// (the tagged value word), ADR 0232 (one question, asked once), ADR 0273 (a door that answers must not
// speak over the road that would have named what it could not carry).
package lang

import "strings"

// chainOperandCarriesMoreThanAWord is the pure gate in front of the three doors: it asks only the
// records the position already keeps, and it emits nothing. A false answer leaves `chainValue`'s ordinary
// arm exactly where it was, so a chain that answers today answers identically tomorrow.
func (g *irGen) chainOperandCarriesMoreThanAWord(e Expr) bool {
	switch o := e.(type) {
	case *Name:
		// The pair road bound this name's two words; the ordinary read refuses on it (Gap R.146).
		return g.numericPairVar(o.Value) || (g.taggedVars != nil && g.taggedVars[o.Value])
	case *Index:
		// Which subscript doors answer is the doors' own business; asking them is what tells the chain
		// whether the read's kind lives in the object. A constant position of a container the literal
		// still describes is answered by NONE of them and keeps ADR 0241's checked read.
		return true
	default:
		// A double — the operand whose LLVM word is eight bytes and whose slot, as written, was four.
		return g.isFloat(e)
	}
}

// bindChainOperand asks the three doors of one operand and, when one answers, writes the chain's slot
// with the pair or the double it was given. It reports whether the operand is now bound, in which case
// every link reads the slot's NAME and the ordinary store is not emitted at all.
func (g *irGen) bindChainOperand(b *strings.Builder, o Expr, slot string) (bool, error) {
	if !g.chainOperandCarriesMoreThanAWord(o) {
		return false, nil
	}
	// 1 — the comparison road's own pair door. Asked first because it is the door the single-comparison
	// form of this very program already answers with: `print(xs[0] < 3)` prints True for a slot holding
	// 2.5, and the chain must not need a different door to say the same thing (ADR 0232).
	var scratch strings.Builder
	if v, t, ok := g.comparePair(&scratch, o); ok && v != "" && t != "" {
		b.WriteString(scratch.String())
		p := assignPair{payload: v, tag: t, origin: taggedOriginSlot, restateOrigin: true, roots: true}
		if nm, isName := o.(*Name); isName && g.taggedOrigin[nm.Value] != "" {
			// The pair came out of a name that already knew where its own tag came from; a refusal this
			// operand's chain later meets may only repeat that truth (Gap R.38).
			p.origin, p.restateOrigin = g.taggedOrigin[nm.Value], true
		}
		g.bindAssignPair(b, slot, p)
		return true, nil
	}
	// 2 — the binding road's ordered list: the answer of a pair-returning call, the arithmetic over a
	// slot, the fold's winner, a slot of a container the program built. Same list, same order, as the
	// plain assignment and the unpacking (assign_pair.go), because a chain operand is a value position
	// exactly like `v = <the same expression>` and the two roads may not disagree about what it carries.
	scratch.Reset()
	if p, ok, err := g.assignPairWords(&scratch, o); err != nil {
		return false, err
	} else if ok && p.payload != "" && p.tag != "" {
		b.WriteString(scratch.String())
		g.bindAssignPair(b, slot, p)
		return true, nil
	}
	// 3 — the double. The gate above asked `isFloat` of the expression; the answer is checked again of the
	// register actually produced, because storing a non-double into `alloca double` is the same class of
	// module llc rejects as storing a double into an `i32`, and the compiler's own module is the one place
	// the exit-code contract refuses to spend exit 2 (ADR 0166).
	if !g.isFloat(o) {
		return false, nil
	}
	scratch.Reset()
	w, err := g.value(&scratch, o)
	if err != nil {
		return false, err
	}
	if w == "" || !emittedRegisterIsDouble(scratch.String(), w) {
		return false, nil
	}
	b.WriteString(scratch.String())
	g.bindChainDouble(b, slot, w)
	return true, nil
}

// bindChainDouble gives the chain's slot the word the value is: an `alloca double` written once, and the
// two records an ordinary float binding writes beside it, so every link reads a float NAME and asks the
// float road it already answers — `y = 5.5` / `print(0 < y < 3)` has always worked, and this is that shape
// arrived at from an expression instead of a literal.
func (g *irGen) bindChainDouble(b *strings.Builder, slot, v string) {
	b.WriteString("  %" + "_" + slot + " = alloca double\n")
	g.allocd[slot] = true
	if g.doubleSlot == nil {
		g.doubleSlot = map[string]bool{}
	}
	g.doubleSlot[slot] = true
	b.WriteString("  store double " + v + ", double* %" + "_" + slot + "\n")
	if g.floatVars == nil {
		g.floatVars = map[string]bool{}
	}
	g.floatVars[slot] = true
	g.floatFmtUsed = true
	// The name is a double, not a pair: the tag the last binding of this slot wrote has no reader left
	// (ADR 0172's latest-binding rule, Gap R.142's lesson at a new site).
	g.forgetTaggedBinding(slot)
}

// emittedRegisterIsDouble reads back the scratch the operand was lowered into and answers whether the
// register it produced was defined by a floating-point instruction. It is the cheap, honest version of a
// type check this IR has no other place to get one: the definition text either says `double`/`float` or
// it does not.
func emittedRegisterIsDouble(src, reg string) bool {
	start := "  " + reg + " = "
	for _, line := range strings.Split(src, "\n") {
		if !strings.HasPrefix(line, start) {
			continue
		}
		def := line[len(start):]
		return strings.HasPrefix(def, "double ") ||
			strings.HasPrefix(def, "float ") ||
			strings.HasPrefix(def, "call double ") ||
			strings.HasPrefix(def, "call float ") ||
			strings.Contains(def, " double ") ||
			strings.Contains(def, " float ")
	}
	return false
}
