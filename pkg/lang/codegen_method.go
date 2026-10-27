// codegen_method.go is the road a method call takes: the receiver's class decides whether
// this is a container's own door, a text method, a user method or a reference method, and
// each of those questions is asked once, in this file, rather than on the caller's road.
package lang

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// callMethod is the road a method call takes — the receiver's class, then the method's own door.
// It answers every call whose callee is an attribute: the container methods that share one layout,
// the text methods that answer text, a user method reached through the class chain, and the
// reference methods the backend declines to invent an answer for.
// refuseUnmarkedMethodText is the method road's `strArgCheck`. A text may only arrive at a parameter the
// text census MARKED, because the mark is what tells the body that the i32 in its slot is an @str_tab index
// rather than a number. An unmarked parameter is one some call site filled with a non-text, or one the census
// could not resolve to a single callable — and handing that slot a text is how `print(C().m("hi"))` answered
// the intern POSITION at exit 0, the class this backend ranks worst. The function call road has refused the
// shape in words since Gap J.5; only the method road was answering it quietly (roadmap Gap R.315, ADR 0366).
//
// Two bounds keep it honest. It fires only for an argument that is PROVABLY a text — a literal, a foldable
// constant, an interpolation, or the answer of the formatting/repeat doors — so a name the census never
// classified keeps the road it always had; and it stays out of the way entirely of a keyword spelling, which
// the census resolves by parameter name elsewhere. It can only ever narrow a wrong answer.
func (g *irGen) refuseUnmarkedMethodText(mname string, args []Expr) error {
	for i, a := range args {
		if _, isKw := a.(*KeywordArg); isKw {
			return nil
		}
		if !g.argIsProvablyText(a) {
			continue
		}
		// The census indexes a method's parameters the way the callable declares them, `self` at 0, which
		// is why the first argument is position 1 here as it is in emitClassMethod (Gap R.111's bool arm
		// reads the same table with the same offset).
		if g.strParamOf[mname][i+1] {
			continue
		}
		return fmt.Errorf("codegen: a text argument to method %q has no compiled word: the parameter it arrives at is not one every call site fills with a text, so the body reads the slot as a number and would print the @str_tab position under the text — annotate the parameter (`s: str`) or pass texts at every call site (roadmap Gap R.315, ADR 0366)", mname)
	}
	return nil
}

// argIsProvablyText is the conservative half of the gate above: what the compiler can SEE is a text at the
// call site, as opposed to a name whose kind only the run time knows.
func (g *irGen) argIsProvablyText(a Expr) bool {
	if a == nil {
		return false
	}
	if _, ok := g.stringVal(a); ok {
		return true
	}
	if _, isFs := a.(*FString); isFs {
		return true
	}
	return g.textDoorIsText(a)
}

func (g *irGen) callMethod(b *strings.Builder, c *Call, attr *Attr, fnName string) (string, error) { // The one thing the shared container layout may not be allowed to do quietly: write. The
	// reference raises for every one of these on the immutable spelling — `TypeError` for the slot
	// writes, `AttributeError` for the methods a tuple does not have — and a road that appended to a
	// tuple happily would answer `(1, 2, 3)`, a value no program of this language can print in the
	// reference. The door refuses, naming the call, the class the reference raises, and the row that
	// owns the raise this shape still lacks (roadmap L11.3, ADR 0361).
	if containerMutators[attr.Name.Value] && g.receiverIsTuple(attr.Obj) {
		return "", refusesTupleMutation("`"+attr.Name.Value+"()`", "AttributeError",
			"'tuple' object has no attribute '"+attr.Name.Value+"'")
	}
	// mod.fn(args): dispatch to an AOT module function.
	if modName, isName := attr.Obj.(*Name); isName {
		if modFuncs := g.imports.Funcs[modName.Value]; modFuncs != nil {
			if fd, ok := modFuncs[attr.Name.Value]; ok && fd != nil {
				mangle := modName.Value + "$" + attr.Name.Value
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, irSymbol(mangle)))
				for i, arg := range c.Args {
					if i > 0 {
						b.WriteString(", ")
					}
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					b.WriteString("i32 " + av)
				}
				b.WriteString(")\n")
				return ret, nil
			}
		}
	}
	mname := attr.Name.Value

	// super().m(args): dispatch m on the base class of the enclosing class.
	if call, isSuper := attr.Obj.(*Call); isSuper && len(call.Args) == 0 {
		if n, isN := call.Fn.(*Name); isN && n.Value == "super" {
			base := ""
			if ci := g.classInfos[g.selfClass]; ci != nil && len(ci.bases) > 0 {
				base = ci.bases[0]
			}
			if fn, ok := g.resolveMethod(base, mname); ok {
				// A generator body that walks its own parameter is refused here, before an operand is emitted
				// (roadmap **Gap R.475**, **ADR 0432**).
				if err := g.refuseGeneratorMethodParameterWalk(fn, mname); err != nil {
					return "", err
				}
				// Every argument is emitted BEFORE the call line is started: a value that
				// needs an instruction of its own (interning a string literal does) would
				// otherwise be written into the middle of the operand list (Gap R.42).
				argRegs := make([]string, len(c.Args))
				for i, arg := range c.Args {
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					argRegs[i] = ", i32 " + av
				}
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %%self%s)\n", ret, fn, strings.Join(argRegs, "")))
				// A generator method answers its buffer, so the register the call leaves the value in IS a
				// heap list: the print road and the `in` road ask `listOperands` of it, exactly as the `def`
				// road's call site does for a module generator (roadmap **Gap R.455**, **ADR 0432**).
				if g.genFuncs[fn] {
					g.listOperands[ret] = true
					g.markGenBuffer(ret, fn)
				}
				// A method is program code, and program code raises: without the
				// call-site check the exception stayed in flight, the method returned
				// its unwind value, and the caller carried on printing (Gap R.41, ADR 0223).
				g.checkExn(b)
				return ret, nil
			}
		}
	}

	// A receiver the compiler can see is a TEXT belongs to the text road at the bottom of this
	// function, whatever the enclosing class happens to call itself. `class C: def upper(self, s):
	// return s.upper()` is the shape: `upper` is a name the class declares, so the dispatch below
	// resolved the body's call to the class's OWN symbol, every call called itself, and the process
	// died by SIGSEGV with nothing on stdout — the artifact at fault, which ADR 0166 forbids and
	// roadmap Gap R.317 records. A text is never an instance, so neither road below can be right for
	// it: skipping them can only turn the nonsense into the text's own answer or a refusal in words.
	textReceiver := g.exprIsString(attr.Obj) && g.receiverClass(attr.Obj) == ""

	// Class method dispatch: `recv.m(args)` where recv's class is known.
	if className := g.receiverClass(attr.Obj); className != "" && !textReceiver {
		if fn, ok := g.resolveMethod(className, mname); ok {
			// Read before an operand is written: a refusal that arrives after `g.value` emitted the receiver
			// and the arguments has already left instructions in the block it abandons (roadmap **Gap R.475**,
			// **ADR 0432**).
			if err := g.refuseGeneratorMethodParameterWalk(fn, mname); err != nil {
				return "", err
			}
			// A text into a parameter the call sites never agreed is a text is the shape the function road
			// has refused in words since Gap J.5 (`strArgCheck`); the method road had no such check and
			// handed the @str_tab POSITION to a body that reads an i32 — `print(C().m("hi"))` answered the
			// intern index at exit 0 (roadmap Gap R.315, ADR 0366).
			if err := g.refuseUnmarkedMethodText(mname, c.Args); err != nil {
				return "", err
			}
			recvHandle, err := g.value(b, attr.Obj)
			if err != nil {
				return "", err
			}
			argRegs := make([]string, len(c.Args))
			for i, arg := range c.Args {
				av, err := g.value(b, arg)
				if err != nil {
					return "", err
				}
				argRegs[i] = ", i32 " + av
			}
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s%s)\n", ret, fn, recvHandle, strings.Join(argRegs, "")))
			// The generator's buffer travels out of the call in this one register: say so, or the print road
			// renders a heap slot number where the module twin prints `[1, 2]` and the `for` road reads the
			// handle as a count (roadmap **Gap R.455**, **ADR 0432**).
			if g.genFuncs[fn] {
				g.listOperands[ret] = true
				g.markGenBuffer(ret, fn)
			}
			g.checkExn(b) // a method call propagates like any other call (Gap R.41, ADR 0223)
			return ret, nil
		}
	}

	// Dynamic dispatch: `recv.m(args)` where recv is a class instance whose
	// class is unknown at compile time. Dispatch on the runtime class-id
	// stored in instance slot 0 (set at instantiation).
	if mname := attr.Name.Value; g.hasMethod(mname) && !textReceiver {
		// A generator method reached through a receiver whose class is a run-time fact is answered by the
		// dispatch below only when EVERY class that declares the name yields. A split census means the joined
		// register is a buffer on one arm and a plain value on another, and the road that receives it picks
		// its helper by a kind the line does not state — so the door refuses naming the row that owns the
		// kind word beside the joined register (roadmap **Gap R.467**, **ADR 0432**), instead of the `for`
		// road walking a counter and printing `0` for the reference's `3` and `4` at the exit code of success.
		if g.methodCallYieldIsSplit(mname) {
			return "", fmt.Errorf("codegen: %q is a generator in some classes this program declares and not in others, and the receiver's class is only known at run time: the register the class-id switch joins would be the yielded buffer on one arm and a plain value on the other, and no road can ask a word which it is — give the classes a common base that declares %q, or wait for the tag beside a dynamically dispatched answer (roadmap Gap R.467, ADR 0432; ADR 0166 why nothing is answered rather than guessed)", mname, mname)
		}
		// The dynamic road joins one register out of every class that declares the name, so the parameter walk
		// is refused if ANY of those bodies walks one: the arm that runs is not written in the line (roadmap
		// **Gap R.475**, **ADR 0432**).
		for _, cls := range g.classOrder {
			if sym, ok := g.resolveMethod(cls, mname); ok {
				if err := g.refuseGeneratorMethodParameterWalk(sym, mname); err != nil {
					return "", err
				}
			}
		}
		// The receiver is read BEFORE it is wrapped: an operand the function body cannot read — a
		// module instance reached from inside a `def` — used to come back as an empty word with its
		// error thrown away, and the dispatch went on to emit `rt_mkobj(i32 3, i32 )`, which is not IR.
		// An unbuildable operand is a refusal (exit 1), not a malformed module the verifier rejects
		// (exit 2) — roadmap Gap R.318, ADR 0029's rule that no path may emit IR it cannot name.
		h, herr := g.value(b, attr.Obj)
		if herr != nil {
			return "", herr
		}
		// Runtime dispatch operates on the canonical %obj-tagged value
		// representation: the receiver is wrapped as {tag=instance,
		// payload=heap-handle}, its kind tag is verified, and the payload
		// (the instance heap handle) is extracted before the class-id is
		// read from instance slot 0. The compiled runtime and the retired engine both
		// derived these tags from the same canonical table (value.go).
		recv := g.newTmp() // %obj
		b.WriteString(fmt.Sprintf("  %s = call %%obj @rt_mkobj(i32 %d, i32 %s)\n", recv, int(TagInstance), h))
		isInst := g.newTmp() // i1
		b.WriteString(fmt.Sprintf("  %s = call i1 @rt_obj_is(%%obj %s, i32 %d)\n", isInst, recv, int(TagInstance)))
		h2 := g.newTmp() // i32 payload = the instance heap handle
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_obj_payload(%%obj %s)\n", h2, recv))
		cid := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, h2))
		argvals := make([]string, len(c.Args))
		for i, a := range c.Args {
			v, verr := g.value(b, a)
			if verr != nil {
				return "", verr
			}
			argvals[i] = v
		}
		type dynCase struct {
			id int
			// cont is the block the exception check branches into; the join below has to be
			// entered from there, not from the call's own arm, once the call can raise (ADR 0223).
			fn, lab, ret, cont string
		}
		var cases []dynCase
		for _, cls := range g.classOrder {
			if fn, ok := g.resolveMethod(cls, mname); ok {
				cases = append(cases, dynCase{id: g.classIDs[cls], fn: fn, lab: g.newLabel("dyn.c"), ret: g.newTmp()})
			}
		}
		done := g.newLabel("dyn.done")
		miss := g.newLabel("dyn.miss")
		b.WriteString(fmt.Sprintf("  switch i32 %s, label %%%s [\n", cid, miss))
		for _, dc := range cases {
			b.WriteString(fmt.Sprintf("    i32 %d, label %%%s\n", dc.id, dc.lab))
		}
		b.WriteString("  ]\n")
		for i := range cases {
			dc := &cases[i]
			b.WriteString(fmt.Sprintf("%s:\n", dc.lab))
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s", dc.ret, dc.fn, h))
			for _, av := range argvals {
				b.WriteString(fmt.Sprintf(", i32 %s", av))
			}
			b.WriteString(")\n")
			// The check has to happen inside the arm, and the arm's `phi` incoming label
			// becomes the check's continuation -- a call site that raises cannot also be a
			// value producer for the join (Gap R.41, ADR 0223).
			dc.cont = g.checkExnLabel(b)
			b.WriteString(fmt.Sprintf("  br label %%%s\n", done))
		}
		b.WriteString(fmt.Sprintf("%s:\n", miss))
		// Runtime miss: the receiver is not an instance of a class that
		// defines this method. Return 0 (the interpreter raises; AOT returns
		// a sentinel since no runtime error infrastructure exists).
		b.WriteString(fmt.Sprintf("  br label %%%s\n", done))
		b.WriteString(fmt.Sprintf("%s:\n", done))
		phi := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = phi i32 [ 0, %%%s ]", phi, miss))
		for _, dc := range cases {
			b.WriteString(fmt.Sprintf(", [ %s, %%%s ]", dc.ret, dc.cont))
		}
		b.WriteString("\n")
		// The dynamic road joins every class that can answer the call in one register, so the register is a
		// buffer only when every case yields; a class whose same-named method is a plain procedure keeps the
		// value reading it always had (roadmap **Gap R.455**, **ADR 0432**).
		if g.methodCallCasesAreGenerator(mname) {
			g.listOperands[phi] = true
			// Every arm that can answer the join is asked, because the arm that runs is a run-time fact:
			// one class whose body never writes a tag and the join would have the walk read a stale word
			// as the other arm's kind (roadmap **Gap R.468**, **ADR 0433**).
			var syms []string
			for _, cls := range g.classOrder {
				if sym, ok := g.resolveMethod(cls, mname); ok {
					syms = append(syms, sym)
				}
			}
			g.markGenBuffer(phi, syms...)
		}
		return phi, nil
	}
	if nm, ok := attr.Obj.(*Name); ok && g.runtimeSets[nm.Value] {
		// Set methods on a heap set: without `add` the `set()` constructor produced
		// a value nothing could grow (roadmap Gap K.3).
		switch attr.Name.Value {
		case "add", "discard", "remove":
			if len(c.Args) != 1 {
				return "", fmt.Errorf("codegen: %s() takes exactly 1 argument", attr.Name.Value)
			}
			if attr.Name.Value != "add" {
				// REMOVAL IS NOT GROWING. `s.discard(v)` and `s.remove(v)` share one road, and it asks the
				// ARGUMENT what it carries rather than the SET what it remembers — the payload-only door this
				// arm used to fall through to deleted whatever member's payload happened to collide with the
				// argument's, and it recorded the discarded argument as a member the set had (roadmap Gap R.422,
				// ADR 0425). `remove` is the same road with a `KeyError` on the absent branch, which the reference
				// has always answered and this backend refused under Gap R.174's standing.
				return g.setMemberRemoval(b, nm.Value, attr.Name.Value, c.Args[0], c.Src)
			}
			if g.mixedSets[nm.Value] {
				// Growing a set whose members describe themselves has to keep that promise:
				// the member travels with its tag, and adding without one would make the new
				// member print as whatever kind its slot held last (ADR 0232).
				mv, mt, ok := g.taggedOperand(b, c.Args[0])
				if !ok {
					return "", fmt.Errorf("codegen: %s.%s(%s) adds a member whose kind the compiler cannot prove to a set whose members are of more than one kind; a float or container member needs the tagged value word (roadmap L11.1, ADR 0232)", nm.Value, attr.Name.Value, g.exprSummary(c.Args[0]))
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, mv, mt))
				return "", nil
			}
			// A member the pair road bound hands the set its payload and its tag as two registers,
			// and the tagged add is the door that takes them — adding and tagging are one operation,
			// because a member added without its tag dedups against the payload alone and prints
			// through whatever the slot last held (ADR 0187, ADR 0232). A member is also the position
			// that has to be hashable, and only the tag can say whether it is (Gap R.81, ADR 0310's
			// literal road; roadmap L11.1, Gap R.146, ADR 0311).
			if pv, pt, isPair := g.pairElemPair(b, c.Args[0]); isPair {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				g.promotePairMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set")
				g.guardHashableTag(b, pt, c.Src)
				b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, pv, pt))
				return "", nil
			}
			av, interned, err := g.heapElemKind(b, c.Args[0])
			if err != nil {
				return "", err
			}
			if g.printsAsInternedStr(c.Args[0]) {
				interned = true
			}
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
			if err := g.recordElemKind(nm.Value, "set", interned, c.Args[0]); err != nil {
				// A set grown with a member of the other kind is promoted, not refused, for
				// the same reason an appended element is (ADR 0232).
				if !g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set", c.Args[0]) {
					return "", err
				}
			}
			// A member that is a float or None has no payload the set's compiled kind can render,
			// so the set stops claiming one — the same promotion an appended element gets
			// (roadmap L11.1, ADR 0233).
			if !g.mixedSets[nm.Value] {
				if mt, mok := g.elemKindTag(c.Args[0]); mok && slotTagSelfDescribing(mt) {
					if g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set", c.Args[0]) {
						g.floatFmtUsed = g.floatFmtUsed || mt == int32(TagFloat)
					}
				}
			}
			if kt, ok := g.elemKindTag(c.Args[0]); ok {
				b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %d)\n", hs, av, kt))
			} else {
				b.WriteString(fmt.Sprintf("  call void @rt_set_add(i32 %%h%d, i32 %s)\n", hs, av))
			}
			if interned {
				// The object, not the variable, records that its members are strings: a helper
				// that fills a container it was handed would otherwise leave the caller's print
				// rendering raw indices (roadmap Gap J.5).
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 1)\n", hs))
			}
			return "", nil
		case "clear":
			if len(c.Args) != 0 {
				return "", fmt.Errorf("codegen: clear() takes no arguments")
			}
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
			b.WriteString(fmt.Sprintf("  call void @rt_set_clear(i32 %%h%d)\n", hs))
			return "", nil
		}
	}
	if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "pop" {
		// xs.pop() removes and returns the last element; xs.pop(i) removes index i
		// (negative counts from the end). The bounds test is emitted around the
		// removal so an out-of-range index raises IndexError through the same
		// exception path a `raise` uses (roadmap Gap K.3).
		//
		// One road, one emission: the pair door (popAnswerPair, pop_answer.go) asks THIS function for
		// the removal too, so the position that prints the popped value and the position that discards it
		// cannot ever disagree about which slot was removed or which bounds raise runs (ADR 0232's one
		// question, ADR 0410).
		if len(c.Args) > 1 {
			return "", fmt.Errorf("codegen: pop() takes at most 1 argument (xs.pop(), xs.pop(i))")
		}
		ret, _, err := g.emitPopRoad(b, nm.Value, c.Args, c.Span(), false)
		if err != nil {
			return "", err
		}
		return ret, nil
	}
	// xs.sort() and xs.reverse() mutate in place and return None. Neither existed in
	// either backend, and the compiled path diagnosed xs.sort() as a *string* method:
	// the call fell through to string-method dispatch and told the user their list was
	// a string (roadmap L11.7, ADR 0191).
	if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] &&
		(attr.Name.Value == "sort" || attr.Name.Value == "reverse") {
		if len(c.Args) != 0 {
			return "", fmt.Errorf("%s() takes no arguments in this build — key= and reverse= need first-class functions (roadmap L11.7)", attr.Name.Value)
		}
		if attr.Name.Value == "sort" && g.mixedLists[nm.Value] {
			// Python raises TypeError for a str/int mix rather than inventing an order,
			// and ordering interned strings by payload would sort them by arrival.
			return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
		}
		g.heapSeq++
		hslot := g.heapSeq
		b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hslot, nm.Value))
		if attr.Name.Value == "sort" {
			mode := 0
			if g.listElemStr[nm.Value] {
				mode = 1
			}
			b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %%h%d, i32 %d)\n", hslot, mode))
		} else {
			b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %%h%d)\n", hslot))
		}
		return "", nil
	}
	if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "append" {
		if len(c.Args) != 1 {
			return "", fmt.Errorf("append expects one argument")
		}
		// `xs.append(build())` puts the container the callee built into a slot. The answer is materialised
		// to a name first, so the slot is filled the way `ys = build()` / `xs.append(ys)` fills it and every
		// question below — pair, tag, kind — is asked of a container variable rather than of a call this
		// road can only lower to one word (roadmap Gap R.67, ADR 0333).
		arg := c.Args[0]
		if ansName, _, okAns := g.containerAnswerOperand(b, arg); okAns {
			arg = ansName
		}
		// An element the pair road bound carries its kind in a register, and the tagged append is
		// the door that takes it: payload and tag are one operation (ADR 0187), and the list stops
		// claiming a kind this pass cannot state, because the kind is now a register the objects
		// wrote (roadmap L11.1, Gap R.146; ADR 0306's literal element, ADR 0232's promotion, ADR 0311).
		if pv, pt, isPair := g.pairElemPair(b, arg); isPair {
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
			g.promotePairMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list")
			b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, pv, pt))
			return "", nil
		}
		if g.mixedLists[nm.Value] {
			// Appending to a tagged list writes the payload and the tag together:
			// rt_append alone would leave the new slot reading back with whatever tag the
			// freed slot last carried, which is how an appended string would print as its
			// interned index (roadmap L11.1, ADR 0187).
			if av, tag, terr := g.mixedElemTag(b, arg); terr == nil {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, av, tag))
				return "", nil
			} else {
				return "", terr
			}
		}
		av, interned, err := g.heapElemKind(b, arg)
		if err != nil {
			return "", err
		}
		// The value is asked twice what it is: heapElemKind answers for the word it interned,
		// printsAsInternedStr for the value the source says it is, and `xs.append(d["a"])` is
		// the shape where only the second one knows (ADR 0232).
		if g.printsAsInternedStr(arg) {
			interned = true
		}
		g.heapSeq++
		hs := g.heapSeq
		b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
		if err := g.recordElemKind(nm.Value, "list", interned, arg); err != nil {
			// Growing a list with a value of the other kind used to be the refusal, because
			// the container records one kind and would print the new element through the old
			// one. The slots carry tags, so the honest move is to stop claiming a kind
			// (ADR 0232); the refusal stays for a value no tag can describe.
			if !g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list", c.Args[0]) {
				return "", err
			}
		}
		// A float or None element is the case the container's one compiled kind cannot render:
		// its payload is a box handle or nothing at all, so printing the list through the number
		// printer shows 1 where Python shows 1.5. The slots already carry tags, so the honest
		// move is the one ADR 0232 made for strings — stop claiming a kind (roadmap L11.1, ADR 0233).
		if !g.mixedLists[nm.Value] {
			if t, ok := g.elemKindTag(arg); ok && slotTagSelfDescribing(t) {
				if g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list", arg) {
					g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
				}
			}
		}
		// Payload and tag are one operation, so an append cannot leave the new slot carrying
		// the tag of whoever held it last (ADR 0187, ADR 0189): there is no untagged append
		// left to fall back to.
		b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, av, g.elemTagOperand(b, arg, interned)))
		if interned {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 1)\n", hs))
		}
		return "", nil
	}

	// list method: `[1, 2, 3].append(4)` -> [1, 2, 3, 4].
	if ll, ok := attr.Obj.(*ListLit); ok {
		if attr.Name.Value == "append" {
			if len(c.Args) != 1 {
				return "", fmt.Errorf("append expects one argument")
			}
			elems := append(append([]Expr{}, ll.Elems...), c.Args[0])
			return g.value(b, &ListLit{Elems: elems})
		}
		// Three words ("unsupported list method index") tell a reader nothing they can act on. The
		// sentence now says which door this is, what the reference does for this method, and the row
		// that owns the rest (ADR 0166's rule: a refusal names the half it lacks).
		return "", fmt.Errorf("codegen: %s is refused: this backend builds the list methods it can lower against the container's own word (append, extend, insert, remove, pop, clear, sort, reverse, index-of-a-literal, count, and membership), and %s needs a run-time search the compiled doors do not have — the reference answers it (with %s when the value is absent), which needs the value word that carries its own kind (roadmap L11.1, Gap I.2)", exprSurface(&Attr{Obj: attr.Obj, Name: attr.Name, Src: attr.Src}), attr.Name.Value, referenceMethodTrap(attr.Name.Value))
	}
	// dict methods: `{1: 2, 3: 4}.keys()` -> [1, 3], `.values()` -> [2, 4].
	if dl, ok := attr.Obj.(*DictLit); ok {
		switch attr.Name.Value {
		case "keys":
			return g.value(b, &ListLit{Elems: dl.Keys})
		case "values":
			return g.value(b, &ListLit{Elems: dl.Vals})
		case "get":
			// dict.get(key, default) returns the value for key or the default.
			if len(c.Args) < 1 || len(c.Args) > 2 {
				return "", fmt.Errorf("get expects 1 or 2 arguments")
			}
			// int key lookup
			if kv, kerr := g.constIntVal(c.Args[0]); kerr == nil {
				for i, k := range dl.Keys {
					if il, ok := k.(*IntLit); ok && il.Value == kv {
						return g.value(b, dl.Vals[i])
					}
				}
			} else if sv, ok := stringConst(c.Args[0], g.builtinShadowed); ok {
				// string key lookup
				for i, k := range dl.Keys {
					if sl, ok := k.(*StrLit); ok && sl.Value == sv {
						return g.value(b, dl.Vals[i])
					}
				}
			}
			// not found: return the default value, if given
			if len(c.Args) == 2 {
				return g.value(b, c.Args[1])
			}
			// A missing key with no default hands back NONE — the reference's answer, and the
			// interpreter's since this cycle. Refusing here (`get: key not found and no default`)
			// turned a program CPython prints `None` for into an error at exit 1, while the fold
			// already had every fact it needed: the key is absent, so the answer is void. The void
			// lowers to the word 0 and `isNoneExpr` renders it `None` through rt_print_none, which is
			// the same arrangement ADR 0172 set for a void return (roadmap Gap R.174, ADR 0291).
			return g.value(b, &NoneLit{})
		default:
			return "", fmt.Errorf("codegen: %s is refused: this backend builds the dict methods it can lower against the container's own word (get, keys, values, items, clear, and a literal read), and %s needs a run-time search the compiled doors do not have — the reference answers it (with %s when the key is absent), which needs the value word that carries its own kind (roadmap L11.1, Gap I.2)", exprSurface(&Attr{Obj: attr.Obj, Name: attr.Name, Src: attr.Src}), attr.Name.Value, referenceMethodTrap(attr.Name.Value))
		}
	}
	// string methods: `"AbC".upper()`, `.lower()`, `.strip()`.
	v, ok := g.stringVal(attr.Obj)
	if !ok {
		// The receiver's name, when the receiver is one: `xs.append(2)` over a module list
		// is not a string method with a non-constant string, and saying so is a lie about
		// the program (Gap R.38).
		if nm, ok2 := attr.Obj.(*Name); ok2 {
			if err := g.moduleStateErr(nm.Value); err != nil {
				return "", err
			}
		}
		// upper() and lower() of a string the compiler cannot read ask the table
		// (ADR 0229). The compiled fold covers ASCII and copies other bytes unchanged;
		// the interpreter has the full case tables — the difference is recorded as a
		// limit of this cycle, not hidden (roadmap Gap R.47).
		if sreg, isStr, err2 := g.strReg(b, attr.Obj); err2 != nil {
			return "", err2
		} else if isStr {
			// A text the compiler cannot read — a parameter, an element, a call's answer — is folded by
			// the runtime door, and since ADR 0362 the door reads the same generated table the fold
			// above does, so the two answers cannot part company. It answers the three operations whose
			// rules the old ASCII loop had no room for as well: title, capitalize and swapcase.
			switch attr.Name.Value {
			case "upper", "lower", "title", "capitalize", "swapcase":
				op := caseOpOfMethod(attr.Name.Value)
				if op < 0 {
					return "", fmt.Errorf("codegen: %s() has no case operation (roadmap L11.5, ADR 0362)", attr.Name.Value)
				}
				g.caseOps |= 1 << uint(op)
				return g.rtStrCall(b, "rt_str_case", "i32 "+sreg, "i32 "+strconv.Itoa(op)), nil
			case "strip", "lstrip", "rstrip":
				// A text the compiler cannot read is trimmed by the door, which walks the artifact's
				// whitespace set — the same one caseFoldTrim read above (ADR 0232).
				return g.emitTrimRoad(b, c, attr.Name.Value, sreg)
			}
		}
		// A NAME bound to a container is not a string with a value the compiler cannot read, and
		// saying "string method keys on non-constant string" about `d = {"a": 1}` / `d.keys()`
		// describes a program the author did not write (Gap R.38's rule). The records that decide
		// this are the same ones the loop and subscript roads read, so the refusal cannot disagree
		// with the codegen beside it (roadmap Gap R.174, ADR 0291).
		if nm, ok2 := attr.Obj.(*Name); ok2 && g.nameHoldsContainer(nm.Value) {
			return "", fmt.Errorf("%s() on %q, which the program bound to a container rather than a text: the compiled backend folds container methods only over a literal written at the call, and %q is a name whose slots the runtime owns — CPython answers this program, and the compiled leg waits for the tagged value word (roadmap L11.1, Gap R.174, ADR 0166)", attr.Name.Value, nm.Value, nm.Value)
		}
		if nm, ok2 := attr.Obj.(*Name); ok2 && g.caughtVars[nm.Value] {
			// Same split as the length door: the name an `except ... as` clause bound prints as the
			// message and is not a text to call a method on — the reference raises AttributeError for
			// `err.upper()`, and answering it with the message in caps is a wrong answer at exit 0
			// (roadmap Gap R.113, Gap R.395).
			return "", fmt.Errorf("%s() on %q is refused: the name an `except ... as` clause bound is not a text to call a method on — the reference asks the exception OBJECT, which carries no such attribute (`AttributeError: 'ZeroDivisionError' object has no attribute %q`), and the object, with its methods, is what the value word would carry (roadmap Gap R.113, Gap R.395, L11.1)", attr.Name.Value, nm.Value, attr.Name.Value)
		}
		return "", fmt.Errorf("string-method %s on a receiver that is not a text the compiler can read: %s (roadmap Gap I.2, ADR 0166)", attr.Name.Value, g.exprSummary(attr.Obj))
	}
	switch attr.Name.Value {
	// The third fold road, and the only one of the three that can say the table would not answer:
	// it is inside a road that returns an error, so it says so rather than keeping the old answer
	// (roadmap L11.5, ADR 0362).
	case "upper":
		t2, cerr := caseMapString(caseUpper, v)
		if cerr != nil {
			return "", cerr
		}
		v = t2
	case "capitalize":
		t2, cerr := caseMapString(caseCapitalize, v)
		if cerr != nil {
			return "", cerr
		}
		v = t2
	case "title":
		t2, cerr := caseMapString(caseTitle, v)
		if cerr != nil {
			return "", cerr
		}
		v = t2
	case "swapcase":
		t2, cerr := caseMapString(caseSwapcase, v)
		if cerr != nil {
			return "", cerr
		}
		v = t2
	case "lower":
		t2, cerr := caseMapString(caseLower, v)
		if cerr != nil {
			return "", cerr
		}
		v = t2
	case "strip", "lstrip", "rstrip":
		// The fold answers when it can read BOTH texts. A cutset it cannot read does not make the question
		// unanswerable — the receiver is right here, and the door beside it reads the same artifact (ADR 0232).
		if len(c.Args) > 1 {
			return "", fmt.Errorf("%s() takes one cutset at most; the reference raises TypeError for %d arguments (roadmap L11.5, ADR 0362)", attr.Name.Value, len(c.Args))
		}
		if len(c.Args) == 1 {
			if _, okCut := g.stringVal(c.Args[0]); !okCut {
				return g.emitTrimRoad(b, c, attr.Name.Value, g.internStr(b, v))
			}
		}
		t2, ok3 := caseFoldTrim(caseTrimMode(attr.Name.Value), v, c.Args, g.stringVal)
		if !ok3 {
			return "", fmt.Errorf("the trim table would not answer %s() (roadmap L11.5, ADR 0362)", attr.Name.Value)
		}
		v = t2
	case "replace":
		if len(c.Args) != 2 {
			return "", fmt.Errorf("replace() takes exactly 2 arguments")
		}
		oldv, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("replace() old must be a constant string")
		}
		newv, ok := g.stringVal(c.Args[1])
		if !ok {
			return "", fmt.Errorf("replace() new must be a constant string")
		}
		v = strings.ReplaceAll(v, oldv, newv)
	case "join":
		if len(c.Args) != 1 {
			return "", fmt.Errorf("join() takes exactly 1 argument")
		}
		ll, ok := c.Args[0].(*ListLit)
		if !ok {
			return "", fmt.Errorf("join() argument must be a constant list")
		}
		parts := []string{}
		allConstant := true
		for _, el := range ll.Elems {
			sv, ok := g.stringVal(el)
			if !ok {
				allConstant = false
				break
			}
			parts = append(parts, sv)
		}
		if allConstant {
			v = strings.Join(parts, v)
			break
		}
		// A slot the compiler cannot read is not a slot the seam cannot cross. `"-".join([f"a{n}", "z"])`
		// is the same three texts the `+` road already bolts together while the program runs, and it needs
		// no new door to do it: one `@rt_str_cat` per seam, the separator interned once, in the order the
		// source wrote. The elements are asked one at a time through `strReg`, which is the same question
		// concatenation asks — is this expression a text, and where is its index — so a list that holds a
		// number, a container or a verdict is declined here rather than interned as somebody's repr
		// (roadmap Gap R.403, ADR 0408; the reference raises TypeError for that program, which it owns).
		//
		// An EMPTY list is answered by the fold above (nothing to concatenate, the empty text back); a
		// ONE-element list never reaches the seam at all, and returns its element's own index.
		if len(ll.Elems) == 0 {
			v = ""
			break
		}
		if cls, named := g.joinItemKind(ll.Elems[0]); named {
			g.constantTrap(b, "TypeError", joinItemTypeError(0, cls), ll.Span())
			return "0", nil
		}
		acc, accOK, accErr := g.strReg(b, ll.Elems[0])
		if accErr != nil {
			return "", accErr
		}
		if !accOK {
			return "", fmt.Errorf("join() element %s is not a text: the seam is built by concatenating texts at run time, and this slot holds something no road can name \u2014 the reference raises TypeError over it, and the tagged value word that would say what it holds is roadmap Gap R.400 / L11.1 (ADR 0408)", g.exprSummary(ll.Elems[0]))
		}
		sep, sepOK, sepErr := g.strReg(b, &StrLit{Value: v})
		if sepErr != nil {
			return "", sepErr
		}
		if !sepOK {
			return "", fmt.Errorf("join() separator is not a text")
		}
		for i, el := range ll.Elems[1:] {
			acc = g.rtStrCall(b, "rt_str_cat", "i32 "+acc, "i32 "+sep)
			er, elOK, elErr := g.strReg(b, el)
			if elErr != nil {
				return "", elErr
			}
			if !elOK {
				if cls, named := g.joinItemKind(el); named {
					g.constantTrap(b, "TypeError", joinItemTypeError(i+1, cls), ll.Span())
					return "0", nil
				}
				return "", fmt.Errorf("join() element %s is not a text: the seam is built by concatenating texts at run time, and this slot holds something no road can name \u2014 the reference raises TypeError over it, and the tagged value word that would say what it holds is roadmap Gap R.400 / L11.1 (ADR 0408)", g.exprSummary(el))
			}
			acc = g.rtStrCall(b, "rt_str_cat", "i32 "+acc, "i32 "+er)
		}
		return acc, nil
	case "find":
		// s.find(sub) -> index of first occurrence of sub, or -1 if absent.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("find() takes exactly 1 argument")
		}
		subv, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("find() argument must be a constant string")
		}
		return fmt.Sprintf("%d", strings.Index(v, subv)), nil
	case "rfind":
		// s.rfind(sub) -> index of last occurrence of sub, or -1 if absent.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("rfind() takes exactly 1 argument")
		}
		subv, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("rfind() argument must be a constant string")
		}
		return fmt.Sprintf("%d", strings.LastIndex(v, subv)), nil
	case "count":
		// s.count(sub) -> number of non-overlapping occurrences of sub.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("count() takes exactly 1 argument")
		}
		subv, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("count() argument must be a constant string")
		}
		return fmt.Sprintf("%d", strings.Count(v, subv)), nil
	case "isdigit":
		// s.isdigit() -> 1 if all runes are digits, else 0.
		res := 0
		if v != "" {
			all := true
			for _, r := range v {
				if !unicode.IsDigit(r) {
					all = false
					break
				}
			}
			if all {
				res = 1
			}
		}
		return fmt.Sprintf("%d", res), nil
	case "isalpha":
		// s.isalpha() -> 1 if all runes are alphabetic, else 0.
		res := 0
		if v != "" {
			all := true
			for _, r := range v {
				if !unicode.IsLetter(r) {
					all = false
					break
				}
			}
			if all {
				res = 1
			}
		}
		return fmt.Sprintf("%d", res), nil
	case "isalnum":
		res := 0
		if v != "" {
			all := true
			for _, r := range v {
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
					all = false
					break
				}
			}
			if all {
				res = 1
			}
		}
		return fmt.Sprintf("%d", res), nil
	case "isspace":
		res := 0
		if v != "" {
			all := true
			for _, r := range v {
				if !unicode.IsSpace(r) {
					all = false
					break
				}
			}
			if all {
				res = 1
			}
		}
		return fmt.Sprintf("%d", res), nil
	case "islower":
		res := 0
		hasCased := false
		allLower := true
		for _, r := range v {
			if unicode.IsLower(r) {
				hasCased = true
			} else if unicode.IsUpper(r) {
				hasCased = true
				allLower = false
			}
		}
		if hasCased && allLower {
			res = 1
		}
		return fmt.Sprintf("%d", res), nil
	case "isupper":
		res := 0
		hasCased := false
		allUpper := true
		for _, r := range v {
			if unicode.IsUpper(r) {
				hasCased = true
			} else if unicode.IsLower(r) {
				hasCased = true
				allUpper = false
			}
		}
		if hasCased && allUpper {
			res = 1
		}
		return fmt.Sprintf("%d", res), nil
	case "startswith", "endswith":
		// s.startswith(sub) / s.endswith(sub) -> 1 or 0.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s() takes exactly 1 argument", attr.Name.Value)
		}
		subv, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("%s() argument must be a constant string", attr.Name.Value)
		}
		res := 0
		if attr.Name.Value == "startswith" {
			if strings.HasPrefix(v, subv) {
				res = 1
			}
		} else if strings.HasSuffix(v, subv) {
			res = 1
		}
		return fmt.Sprintf("%d", res), nil
	case "ljust", "rjust":
		// ljust pads the receiver on the right with spaces to width w;
		// rjust pads on the left (no-op when len(v) >= w).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s expects one argument", attr.Name.Value)
		}
		wv, werr := g.constIntVal(c.Args[0])
		if werr != nil {
			return "", fmt.Errorf("%s: codegen folds only a constant width arg", attr.Name.Value)
		}
		pad := int(wv) - len(v)
		if pad <= 0 {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, v), nil
		}
		spaces := strings.Repeat(" ", pad)
		if attr.Name.Value == "ljust" {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, v+spaces), nil
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, spaces+v), nil
	case "zfill":
		// zfill pads the receiver on the left with '0' to width w
		// (no-op when len(v) >= w), mirroring the interpreter.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("zfill expects one argument")
		}
		wv, werr := g.constIntVal(c.Args[0])
		if werr != nil {
			return "", fmt.Errorf("zfill: codegen folds only a constant width arg")
		}
		if int(wv) <= len(v) {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, v), nil
		}
		// One rule with the interpreter — zfillTo pads after a leading sign, which neither
		// engine did when each padded on its own (roadmap Gap R.183, ADR 0297).
		return g.internStr(b, zfillTo(v, int(wv))), nil
	case "removeprefix", "removesuffix":
		// removeprefix strips the given prefix from the receiver;
		// removesuffix strips the suffix, mirroring strings.TrimPrefix/TrimSuffix.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s expects one argument", attr.Name.Value)
		}
		sub, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("%s: codegen folds only a constant string arg", attr.Name.Value)
		}
		var res string
		if attr.Name.Value == "removeprefix" {
			res = strings.TrimPrefix(v, sub)
		} else {
			res = strings.TrimSuffix(v, sub)
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, res), nil
	case "index":
		// "s".index(sub) returns the byte index of sub (strings.Index).
		// The interpreter raises on not-found; the AOT codegen has no error
		// channel, so it folds to -1 on not-found (like find).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("index expects one argument")
		}
		sub, ok := g.stringVal(c.Args[0])
		if !ok {
			return "", fmt.Errorf("index: codegen folds only a constant string arg")
		}
		return fmt.Sprintf("%d", int64(strings.Index(v, sub))), nil
	case "expandtabs":
		// expandtabs replaces each tab with the spaces up to the next tab
		// stop at width w, tracking the running column, mirroring the
		// interpreter's tab-stop algorithm. Source string literals have no
		// escape sequences, so literal receivers contain no tabs (no-op).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("expandtabs expects one argument")
		}
		wv, werr := g.constIntVal(c.Args[0])
		if werr != nil {
			return "", fmt.Errorf("expandtabs: codegen folds only a constant width arg")
		}
		w := int(wv)
		if w <= 0 {
			return "", fmt.Errorf("expandtabs width must be positive")
		}
		var sb strings.Builder
		col := 0
		for _, r := range v {
			if r == '\t' {
				n := w - (col % w)
				sb.WriteString(strings.Repeat(" ", n))
				col += n
			} else {
				sb.WriteRune(r)
				col++
			}
		}
		v = sb.String()
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, v), nil
	default:
		return "", fmt.Errorf("unsupported string method %s", attr.Name.Value)
	}
	// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
	return g.internStr(b, v), nil
}

// joinItemKind names the type CPython would report for a `str.join` element the fold can read — the class
// the reference puts in `TypeError: sequence item N: expected str instance, <type> found` — and answers false
// for everything the ordinary roads own, including a text and a name whose kind is a run-time fact. A bool is
// its own word to the reference here (it reports `bool`, not `int`), which is why it is spelled out.
func (g *irGen) joinItemKind(e Expr) (string, bool) {
	switch n := exprUnwrap(e).(type) {
	case nil:
		return "", false
	case *IntLit:
		return "int", true
	case *FloatLit:
		return "float", true
	case *BoolLit:
		return "bool", true
	case *NoneLit:
		return "NoneType", true
	case *ListLit:
		return "list", true
	case *Tuple:
		return "tuple", true
	case *DictLit:
		return "dict", true
	case *SetLit:
		return "set", true
	case *Name:
		if g.isNoneExpr(n) {
			return "NoneType", true
		}
		if g.nameHoldsContainer(n.Value) {
			return "list", true
		}
	}
	return "", false
}

// joinItemTypeError is CPython's own sentence for the element that is not a text, with the position the
// reference counted. Writing it here rather than refusing keeps the raise catchable by the program's own
// `except TypeError:`, which is what the reference's program does with it (roadmap Gap R.403, ADR 0408).
func joinItemTypeError(at int, cls string) string {
	return fmt.Sprintf("sequence item %d: expected str instance, %s found", at, cls)
}
