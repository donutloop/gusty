// The METHOD door on a container the CALLEE builds.
//
// Four roads already read the object a call hands back. `print(b())`, `str(b())`, `if b():`,
// `for v in b():` and `ys = b()` all ask `containerAnswerOperandRead` (**ADR 0333**'s door), which
// emits the call, roots the handle, and records the container notebooks the print and loop roads
// read. A fifth position — asking that same object to DO something, `b().append(3)` — did not ask
// it, and fell all the way through the method road to the text door:
//
//	def b():
//	    return [1, 2]
//
//	print(b().append(3))   # the reference: None — this backend, before this row: `string-method
//	                       # append on a receiver that is not a text the compiler can read: b(...)`
//
// The bound spelling of every one of those programs — `ys = b()` / `ys.append(3)` — has answered the
// reference's bytes for many cycles. So one program had an answer when the name was written and lost
// it when the name was left out, which is the same defect **ADR 0334** fixed for the subscript with
// `containerAnswerSubscript`: the base's *spelling* chose the road. This file gives the method road
// the same judgement — materialise the answer, then ask the road that has always answered the
// variable (roadmap **Gap R.229**, **ADR 0458**).
//
// Three rules decide the shape of the door.
//
//   - ONE question, asked twice. Whether a receiver is a container answer is asked by the dispatch
//     below and by `callIsInPlaceMutation` (the print/`str`/f-string road has to know that
//     `b().append(3)` hands back the VOID, or it lowers the void's word into a `printf` operand and
//     `llc` rejects the module at **ADR 0166**'s exit class). Both ask `containerAnswerIsProven`, and
//     it is the only place the scan's two sets are consulted for this position.
//   - The ROAD decides which methods it serves. This file keeps no served-method list that the door
//     consults: it dispatches the materialised receiver through `callMethod` itself, in a scratch
//     buffer, and commits only if the road answered. A door carrying its own list of served names
//     would be a second answer to one question (**ADR 0232**), and the first thing such a list gets
//     wrong is a method the road grew since the list was written — or one it never had.
//     `containerAnswerMethodIsServed` below is this row's CLAIM about today, asked by the tests,
//     never by the door.
//   - A refusal stays the road's own. `append` of an element whose kind cannot be tagged must still
//     refuse with the pair sentence, not with a note that the receiver is not a text. Only the
//     refusal that means *"this is not a method I serve on a container"* — the two fall-through
//     sentences the method road has always printed — is let go, and then the position continues on
//     the road it has always taken, refusal and all (**ADR 0273**: an extra answer, never a
//     substitute for one).
//
// What the door does NOT take: a class's own instantiation or a `super()` receiver, which belong to
// the class roads above; a module's `m.fn(...)`; a text receiver, which belongs to the text road
// below; and a tuple answer's mutator, which the tuple gate at the head of `callMethod` refuses
// before this door is ever asked. Each container method is given the READ its own position asks — a
// mutator that only writes a slot takes an answer the slot table could not describe (the objects
// wrote their own tags), while a method that hands a slot back takes the described answer alone
// (**Gap R.261**'s rule, **ADR 0344**).
package lang

import (
	"fmt"
	"strings"
)

// containerAnswerMethodPositionRead is the position's question: what does THIS method mean to read
// out of the answer, and so what the answer has to be (**ADR 0344**'s split, asked of a method rather
// than of a print or a loop). A mutator hands the handle to the runtime door that writes it; a method
// that answers an element reads a slot back.
//
// `sort` and `reverse` sit with the mutators although they compare slots: the comparison is the runtime
// door's own walk of the object (`rt_list_sort`), not a compile-time read of a notebook, so the answer
// it needs is the object and the record's kind, exactly like `append`'s.
func containerAnswerMethodPositionRead(mname string) containerAnswerRead {
	switch mname {
	case "append", "extend", "insert", "add", "discard", "remove", "clear", "update",
		"sort", "reverse", "difference_update", "intersection_update", "symmetric_difference_update":
		return containerReadWhole
	}
	return containerReadSlots
}

// containerAnswerIsProven is the door's question with no IR attached to it: is this expression a call
// whose answer the container scan proves is a container, at the read this position is about to ask?
// It is the MIRROR of `containerAnswerOperandRead`, and mirrors are only worth having if they agree on
// every branch: the described set answers the position alone (its slots are what the table wrote the
// notebooks from, so a mutator and a slot read are the same object to it), while the set the scan
// proved and the slot table declined is asked for the READ — the doors that ask the OBJECT may take it,
// the doors that read one slot back may not (**Gap R.261**, **ADR 0344**). The print road and the
// dispatch must never disagree about who answered (**ADR 0232**), and **ADR 0300**'s lesson is that the
// void and the value are one question: a `print` that called `b().pop()` a value while the call road
// called it a mutation lowers the void's word into a `printf` operand and `llc` rejects the module.
func (g *irGen) containerAnswerIsProven(recv *Call, read containerAnswerRead) bool {
	if recv == nil || g == nil {
		return false
	}
	nm, isName := recv.Fn.(*Name)
	if !isName {
		// A receiver reached through an expression: `c.build().add(1)`. The carried-method round
		// (**Gap R.453**, **ADR 0430**) is the only road that can name such an answer, and it names
		// it here under the same read the door will ask.
		ans, ok := g.methodContainerAnswerOf(recv)
		return ok && ans.takesRead(read) && g.containerAnswerCallIsCarriable(recv)
	}
	if nm.Value == "super" {
		return false
	}
	if _, isClass := g.classInfos[nm.Value]; isClass {
		return false
	}
	if g.imports != nil && g.imports.Funcs[nm.Value] != nil {
		return false
	}
	if !g.containerAnswerCallIsCarriable(recv) {
		return false
	}
	if ans, ok := g.containerAnswerOf(nm.Value); ok {
		// The described set: the same branch `containerAnswerOperandRead` takes without asking the read,
		// because the notebooks a slot read consults are the ones this record was written from.
		return ans.kind != notAContainerAnswer
	}
	// Proven a container, declined by the slot table: the doors that ask the OBJECT may still take it
	// (**Gap R.261**, **ADR 0344**), and a mutator is such a door.
	if und, isUnd := g.containerRetOpaque[nm.Value]; isUnd {
		return und.kind != notAContainerAnswer && und.takesRead(read)
	}
	return false
}

// containerAnswerMethodGate answers the door's three cheap questions about a written method call: is
// the receiver a call the container scan proves, is the method one of the container vocabulary, and is
// the receiver a text the text road owns. It emits nothing and allocates nothing — it is asked by the
// print road before it chooses a printer and by the dispatch before it chooses a road, and the two must
// give the same answer (**ADR 0232**).
func (g *irGen) containerAnswerMethodGate(attr *Attr) (recv *Call, read containerAnswerRead, ok bool) {
	if g == nil || attr == nil || attr.Name == nil {
		return nil, containerReadSlots, false
	}
	recv, isCall := attr.Obj.(*Call)
	if !isCall || !containerAnswerMethodKnown[attr.Name.Value] {
		return nil, containerReadSlots, false
	}
	read = containerAnswerMethodPositionRead(attr.Name.Value)
	if !g.containerAnswerIsProven(recv, read) {
		return nil, containerReadSlots, false
	}
	// A receiver the text road owns: a call whose answer the compiler can read as a text belongs to
	// the door at the bottom of `callMethod`, the one that knows `%`-formats and case tables.
	if g.exprIsString(recv) && g.receiverClass(recv) == "" {
		return nil, containerReadSlots, false
	}
	return recv, read, true
}

// containerAnswerMethodRoadServes is the proof that the road under the rewritten node answers it. A
// mutator is served by the mutator road, which hands the handle to the runtime and answers the void.
// A method that reads a slot back is served only by the one door that can render a slot — the pop door
// — and its own shape question, asked of the REWRITTEN call, is the proof: `xs.pop()` where the list's
// record says what its slots hold. Everything else (`get`, `index`, `count`, `copy`, `keys`) keeps the
// refusal the container-name sentence has always printed, because the alternative is the raw slot word
// printed as a number — `print(b().pop())` answering `1` for the text `"b"`, which is
// **Gap R.261**'s defect class and **ADR 0166**'s forbidden answer. The measured pair, from this row's
// probe: `def b(): return ["a", "b"]` / `print(b().pop())` gave `1` against `ys.pop()`'s `b`, and
// `return [1, "a", None]` gave `0` against `ys.pop()`'s `None`. What the READER methods still refuse is
// **Gap R.174**'s standing, unchanged by this door — the roads do not serve `get`, `keys`, `index`,
// `count` or `copy` for a container VARIABLE either, and the answer refuses with the variable's own
// sentence (roadmap **Gap R.538**, filed beside this row).
func (g *irGen) containerAnswerMethodRoadServes(redo *Call, read containerAnswerRead) bool {
	if read == containerReadWhole {
		return true
	}
	attr, isAttr := redo.Fn.(*Attr)
	if !isAttr || attr.Name == nil {
		return false
	}
	if attr.Name.Value != "pop" {
		return false
	}
	// One road, one emission — the two arms of the pop door, asked of the rewritten node.
	if _, _, serves := g.popAnswerShape(redo); serves {
		return true
	}
	// The mutator road beside it (`codegen_method.go`, the `listVars[…] && pop` arm): the removal and
	// its bounds raise need only the object, so a list whose slots say nothing about their kind —
	// `def b(): return []`, whose `pop()` the reference ends in `IndexError: pop from empty list` — is
	// served too. Without this arm the empty answer fell back to the text refusal while the bound name
	// raised, which is the same program answered and refused by whether it named the object.
	if nm, isName := attr.Obj.(*Name); isName && g.listVars[nm.Value] && len(redo.Args) <= 1 {
		return true
	}
	return false
}

// containerAnswerMethodCall is the door as the positions above the dispatch see it: a method call whose
// receiver is a call that hands back a container is REWRITTEN to the same call written against the
// rooted name the answer was recorded under, and the caller then lowers that node through the roads
// that have always answered `ys.append(3)`.
//
// This is `containerAnswerSubscript` (**ADR 0334**) one construct further out, for the same reason: a
// position that only looks at the node's SHAPE calls a call what it cannot be — the text road, the fold,
// the number door — and the answer a program gets depends on whether the program named the object.
// Asking the rewrite before the position chooses is what makes the two spellings one program by
// construction instead of two roads that have to agree (**ADR 0232**, roadmap **Gap R.229**).
func (g *irGen) containerAnswerMethodCall(b *strings.Builder, e Expr) (Expr, bool, error) {
	c, isCall := e.(*Call)
	if !isCall {
		return e, false, nil
	}
	attr, isAttr := c.Fn.(*Attr)
	if !isAttr {
		return e, false, nil
	}
	recv, read, ok := g.containerAnswerMethodGate(attr)
	if !ok {
		return e, false, nil
	}
	var scratch strings.Builder
	name, _, ok := g.containerAnswerOperandRead(&scratch, recv, read)
	if !ok {
		return e, false, nil
	}
	synth, isName := name.(*Name)
	if !isName {
		return e, false, nil
	}
	redo := g.containerAnswerMethodRewrite(c, attr, name)
	if !g.containerAnswerMethodRoadServes(redo, read) {
		return e, false, nil
	}
	// The road decides, and it decides in a buffer that is thrown away. The caller lowers the rewritten
	// node itself — one emission, never two — so the door's only proof is a TRIAL: run the roads under
	// the name and see whether they answer. A shape they decline (`dict.clear` on a run-built answer is
	// Gap R.174's) is a shape this door never took, and the position keeps the refusal it has always
	// printed, about the receiver the program wrote. A shape they refuse for a real reason is kept as
	// the refusal it is, with the door's own name in it replaced by the receiver the source spelled
	// (**ADR 0166**: a refusal names the missing thing, and the missing thing is not the compiler).
	var trial strings.Builder
	if _, err := g.value(&trial, redo); err != nil {
		if containerMethodRoadDeclinesThisMethod(err.Error()) {
			return e, false, nil
		}
		return e, false, g.nameTheAnswerInTheRefusal(err, synth.Value, recv)
	}
	// Only the answer is committed here. The method itself is lowered by the caller, once, from the
	// returned node — the trial's bytes are the proof, not the emission.
	b.WriteString(scratch.String())
	return redo, true, nil
}

// containerAnswerMethodRewrite is the copied node: the same call, the same arguments, the same span,
// with the rooted name in the receiver's place. A copy, never an in-place edit — the AST is shared by
// every position that reads the program and by the fold, which is the rule `containerAnswerSubscript`
// follows for the same reason.
func (g *irGen) containerAnswerMethodRewrite(c *Call, attr *Attr, name Expr) *Call {
	redoAttr := *attr
	redoAttr.Obj = name
	redo := *c
	redo.Fn = &redoAttr
	return &redo
}

// containerAnswerMethodReceiver is the dispatch's half of the door: the position that reached
// `callMethod` without asking the rewrite above still gets the answer, because the roads below choose
// from what the receiver LOOKS like and a call looks like none of them.
// `handled=false` is the door declining: the caller continues on the road it has always taken.
func (g *irGen) containerAnswerMethodReceiver(b *strings.Builder, c *Call, attr *Attr, fnName string) (string, bool, error) {
	recv, read, ok := g.containerAnswerMethodGate(attr)
	if !ok {
		return "", false, nil
	}

	// The road decides, and it decides in a scratch buffer. `containerAnswerOperandRead` emits the
	// call, the rooted slot and the notebooks; the dispatch emits the mutator. Committing both only
	// once the road answers is what keeps a declined shape from leaving instructions in a block the
	// program never reaches — the rule the neighbour door `voidAgainstACarriedAnswer` follows
	// (**ADR 0166**: the compiler's mistake must never be reported as the program's).
	var scratch strings.Builder
	name, _, ok := g.containerAnswerOperandRead(&scratch, recv, read)
	if !ok {
		return "", false, nil
	}
	synth, isName := name.(*Name)
	if !isName {
		return "", false, nil
	}
	redo := g.containerAnswerMethodRewrite(c, attr, name)
	if !g.containerAnswerMethodRoadServes(redo, read) {
		return "", false, nil
	}
	// The rewritten node goes back through the WHOLE call road, not into `callMethod` by hand. The
	// shape decides which door answers: `xs.pop()` is served by the pop door (`pop_answer.go`), the pair
	// door and the fold doors as well as by the mutator road, and each of them asks its questions of the
	// NAME the container is recorded under. Calling `callMethod` directly would have skipped those doors
	// and left the raw slot word to be printed — which is how `b().pop()` of a list of texts handed back
	// the text's interned index (`1`) where `ys.pop()` of the very same object hands back `b`. Re-running
	// the road on the materialised name makes the two spellings one program by construction, which is
	// what this row is for (**ADR 0232**, **ADR 0334**'s rewrite-not-a-second-reader).
	var body strings.Builder
	out, err := g.value(&body, redo)
	if err != nil {
		if containerMethodRoadDeclinesThisMethod(err.Error()) {
			// "not a method I serve on a container" — the position keeps the sentence the road it has
			// always taken prints, which names the receiver the program itself wrote.
			return "", false, nil
		}
		return "", true, g.nameTheAnswerInTheRefusal(err, synth.Value, recv)
	}
	b.WriteString(scratch.String())
	b.WriteString(body.String())
	return out, true, nil
}

// containerMethodRoadDeclinesThisMethod recognises the two sentences the method road prints when the
// receiver is a container but the method is not one of its doors: the container-name refusal (Gap
// R.174's sentence) and the text-method refusal (Gap I.2's). Both mean *"wrong method, not wrong
// value"*. Both are asserted to still exist by `TestTheMethodDoorReadsTheRoadsOwnDecline`, because a
// rewording that this door stopped recognising would silently turn an answered shape back into a
// refusal about the wrong thing.
func containerMethodRoadDeclinesThisMethod(msg string) bool {
	return strings.Contains(msg, "which the program bound to a container rather than a text") ||
		strings.Contains(msg, "on a receiver that is not a text the compiler can read")
}

// nameTheAnswerInTheRefusal keeps a real refusal real. The road below was handed a name the program
// never wrote — the slot the answer was rooted under — so a sentence quoting it would blame the
// reader's compiler for the program's missing door. The name is replaced by the receiver the source
// spelled; everything else, including the roadmap row the sentence names, is the road's own
// (**ADR 0166**: a refusal names the missing thing, and the missing thing is not the compiler).
func (g *irGen) nameTheAnswerInTheRefusal(err error, synth string, recv Expr) error {
	msg := err.Error()
	if !strings.Contains(msg, synth) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(msg, synth, g.exprSummary(recv)+" (the container it hands back)"))
}

// containerAnswerMethodKnown is the head-gate's vocabulary: the container methods — the mutators that
// change what a slot holds, plus the methods that read one back or walk the whole object. It is
// deliberately WIDER than what any road serves (the road narrows it), and exists only so that
// `"a".upper()` and `c.m()` do not pay for a door they cannot enter. Anything outside it never reaches
// the door at all, which is what keeps the text and class roads exactly where they were.
var containerAnswerMethodKnown = map[string]bool{
	// the mutators
	"append": true, "extend": true, "insert": true, "pop": true, "clear": true,
	"sort": true, "reverse": true, "add": true, "discard": true, "remove": true,
	"update": true, "setdefault": true, "difference_update": true, "intersection_update": true,
	"symmetric_difference_update": true, "popitem": true,
	// the readers that walk the object
	"keys": true, "values": true, "items": true, "get": true, "count": true, "index": true,
	"copy": true, "union": true,
}

// containerAnswerMethodIsServed is this row's claim about which (container kind, method) pairs the
// materialised receiver answers — asked by the tests as a predicate, NEVER by the door above, which
// asks the road. It is the record of what the roads serve for a container VARIABLE, which is the set
// the door can reach: a method the variable roads do not serve (`extend`, `index`, `count`, the dict
// readers) is refused for the answer too, by the road and not by this list, and refuses with the
// sentence the variable gets — roadmap Gap R.174's standing, not this row's. A widening of a container
// road widens the answer set, and the row that asks this predicate fails until the claim is updated
// beside the road.
func containerAnswerMethodIsServed(kind containerAnswerKind, mname string) bool {
	switch kind {
	case listAnswer:
		switch mname {
		case "append", "pop", "sort", "reverse":
			return true
		}
	case setAnswer:
		switch mname {
		case "add", "discard", "remove", "clear":
			return true
		}
	}
	return false
}
