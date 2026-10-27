package lang

// A call's answer is a value, and a value has a kind.
//
// For most of this backend's life the answer of a call was ONE word and the tables keyed by the
// callee's name said what that word meant: `strFuncs` for a text, `floatFuncs` for a double,
// nothing at all for a number. That is sound exactly as long as a body answers one kind, and it is
// a wrong answer — at the exit code of success — the moment it does not: `def f(x):
//
//	if x:
//	    return "x"
//	return 0
//
// with `print(f(1))` / `print(f(0))` printed `x` and `x`, because the first text seen anywhere in
// the body registered the function as text-returning and the caller then rendered *every* answer
// with `rt_print_str`, whose job is to look an integer up in `@str_tab` (roadmap Gap R.219). The
// same missing word answered `7`/`0` for CPython's `7`/`None` (Gap R.228), `1`/`2` for CPython's
// `True`/`2`, a slot number for `{'k': 1}`, and the digit `0` for a container arm.
//
// This file is the answer direction, decided **once, program-wide, before any IR exists** — the
// rule ADR 0273 and ADR 0276 established for every fact that crosses a call boundary: a `define`
// and the `call`s to it must agree whichever of them the file mentions first. It answers two
// questions and nothing else:
//
//   - which functions answer more than one kind, so that no single table entry can say what their
//     payload means (`carries`), and
//   - what kind word each individual `return` road stores beside the payload it already produces
//     (`armOf`).
//
// A function that answers ONE kind keeps every road it has always had — the tables keyed by name
// are correct for it and this pass does not touch it. A function that answers more than one stores
// the constant tag of the arm that ran into the same word the pair road already uses
// (`pairTagSlot`), and every position that reads such an answer asks the pair door
// (`pairCallPair`), which is how `print`, a binding, a container element, a condition, `str`/`repr`
// and an f-string field come to agree about one value instead of each guessing from the payload.
//
// What this pass will NOT claim:
//
//   - a kind it cannot spell out of the source. An arm that returns a name, an attribute, a float,
//     or anything else whose kind is not written in the spelling leaves the function on the roads
//     it has always had. A gate that guesses is worse than a gate that declines: `1` printed for
//     `True` is the bug, `True` printed for `True` is the claim.
//   - a body that is not an ordinary body — a generator answers its accumulator handle, an `async
//     def` answers a coroutine, a decorated def answers through the decorator's own convention,
//     and a method answers through `emitClassMethod`, which does not share the return roads this
//     file drives. What a method body DOES get from this file is the one verdict that needs no
//     return road at all — "every road of it answers the void" — because a body whose payload is
//     the word `0` is the one body that cannot afford to wait for the tagged answer word
//     (`methodAnswersOnlyTheVoid`, roadmap **Gap R.452**, **ADR 0429**; the CARIED kind word for a
//     method is still this file's own row to claim).
//   - anything the pair road already owns. A body whose every return is arithmetic over a pair the
//     caller handed in stores a *run-time* tag, which is a better answer than the constant one
//     written here; those functions stay with `pairFnSpec.returnsPair`.

import (
	"fmt"
	"sort"
	"strings"
)

// The kind words the answer direction stores, spelled as the module's tag table means them
// (`TagInt`…`TagSet` in value.go, which is what `rt_print_mixed_value`, `rt_pair_truth`,
// `rt_payload_eq` and `rt_num_arith` all switch on). `pairTagNone` and `pairIntTag` are the same
// two digits the pair road already writes; they are named again here so the answer direction reads
// as one table, and `TestTheAnswerKindWordsAreTheTagTable` fails the suite if the two ever drift
// from `value.go`.
const (
	answerWordInt    = "0"
	answerWordBool   = "2"
	answerWordNone   = "3"
	answerWordStr    = "4"
	answerWordList   = "5"
	answerWordDict   = "6"
	answerWordSet    = "7"
	answerWordVaried = "" // a `return other(...)` arm: the kind is the callee's own run-time fact
)

// answerArm is one `return` road of one function: the constant kind word the road stores beside
// its payload, or the callee whose answer word this road forwards.
type answerArm struct {
	word string // one of the answerWord* constants, or answerWordVaried
	dyn  string // non-empty: `return other(...)`, so other's own answer is this arm's answer
	// fwdKind is the container kind a FORWARDED method answer carries with it, recorded by the arm that read
	// the callee (`answerForwardedMethodArm`). The roads that walk an answer — `len`, an index, `in`, a loop —
	// read a container by its KIND, and a body whose only road is `return self.m()` states no kind of its own;
	// without this the forwarding method's answer is an object those doors cannot name (roadmap **Gap R.454**,
	// **ADR 0431**; the doors that read a SLOT of one are still **Gap R.464**'s).
	fwdKind containerAnswerKind
}

// answerKindPlan is the whole-program answer to "which calls answer a value whose payload cannot
// say what it is". It is built from the AST and nothing else, so two compiles in flight cannot
// disagree about it and one compile cannot be answered by another's plan (ADR 0343's rule about a
// run-scoped fact).
type answerKindPlan struct {
	carry map[string]bool
	// void records the carried functions that answer NOTHING but the void: their payload is no value at
	// all, and the print road and the prompt have always been allowed to say `None` without asking.
	void map[string]bool
	arms map[*ReturnStmt]answerArm
	// methodCarry is the METHOD round's `carry` (roadmap **Gap R.453**, **ADR 0430**): the method bodies,
	// keyed by the symbol their `define` is emitted under, whose answer needs a kind word beside it. A
	// separate table rather than one more name in `carry`, because a method and a module `def` can share a
	// spelling — `def m():` and `class C(): def m(self):` are two bodies, and the dispatch runs the one the
	// RECEIVER names (the same reason `methodBodyOf` keys a body by its class).
	methodCarry map[string]bool
	// methodObject records, for a carried method, that some arm answers an OBJECT — a container the callee
	// builds in its own frame, or a call that answers one — so the caller must root the payload the way the
	// pair road roots a pair whose payload may be a float box (ADR 0181, ADR 0280).
	methodObject map[string]bool
	// methodKind is the body in a class that the container table `containerRet` describes for a `def` and
	// never could for a method: the ONE container kind every road of it answers. The loop road, the membership
	// road and the binding road read a container by name, so this is the word that lets them read the object a
	// method built (roadmap Gap R.453, ADR 0430 — the door `containerAnswerOperandRead` opens).
	methodKind map[string]containerAnswerKind
	// methodBodies and methodArmExprs keep the carried method's own reading — the body and the expressions
	// its `return` roads answer — so the door that hands the object to a road which WALKS it can ask the
	// same slot question the `def` scan asks, rather than a weaker one written beside it (roadmap
	// Gap R.453, ADR 0430; ADR 0232's one question).
	methodBodies   map[string]*FuncDef
	methodArmExprs map[string][]Expr
	// methodVoid is the METHOD round's `void`: the bodies, keyed by the same emitted symbol, that answer
	// NOTHING but the void. The forwarding arm (`return self.m()`, roadmap **Gap R.454**, **ADR 0431**) asks
	// this table and no other, because the reading that walked into the callee instead would follow two
	// methods that forward to each other around forever — a round reads a table, and only a call could read
	// a body. It is also why the void verdict is recorded for a body the CARRY round declines: a body with no
	// `return` road at all (`def m(self): self.items = []`) answers the void exactly as one whose every road
	// is `return None`, and the forwarding arm has to be able to say so without re-reading it.
	methodVoid map[string]bool
	// bodyClass names the class a method body was WRITTEN in, keyed by the body itself. An arm of the shape
	// `return self.m()` is answerable only if the reading knows whose `self` it is, and the only thing
	// `answerArmOf` is handed is the body (roadmap Gap R.454).
	bodyClass map[*FuncDef]string
	// classTable is the forwarding arm's inheritance walk read off the AST: the methods each class DECLARES
	// and the bases it names, in the order `registerClass` records them and `resolveMethod` walks them, so
	// the arm names the same body the dispatch will call (ADR 0273's rule that a call and its `define` answer
	// to one scan). A class name declared twice is dropped — two classes, one name, is no class, which is the
	// rule **ADR 0299** already applies to the method table.
	classTable map[string]*declaredClass
}

// carries answers the caller's question: does a call to this name answer a value that needs its
// kind beside it?
func (p *answerKindPlan) carries(fnName string) bool {
	return p != nil && p.carry[fnName]
}

// answersOnlyTheVoid reports a carried body whose every arm answers None. The print road's void arm and
// the prompt's are correct for it and stay; the gate that refuses to call a call "the void" asks this
// first (roadmap L11.1, Gap R.228, ADR 0368).
func (p *answerKindPlan) answersOnlyTheVoid(fnName string) bool {
	return p != nil && p.void[fnName]
}

// declaredClass is one row of the plan's class table: what the class declares itself and what it inherits
// from, in source order. It is the shape `registerClass` writes into `classInfo` and `resolveMethod` walks,
// read off the AST instead of off the emitted program, because the answer direction exists before any IR
// (roadmap **Gap R.454**, **ADR 0431**).
type declaredClass struct {
	bases   []string
	methods map[string]*FuncDef
}

// bodyAnswersTheVoid is the ONE reading of "does every road of this body answer nothing but the void", the
// question `answersOnlyTheVoid` asks of a module `def` by name, `methodAnswersOnlyTheVoid` asks of a body in
// a class, and a forwarding arm asks of the callee it is about to answer for. Three doors, one walk
// (**ADR 0232**), so a body cannot be the void at the print and a value at the `is`.
func bodyAnswersTheVoid(fd *FuncDef, p *answerKindPlan) bool {
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || containsYield(fd.Body) {
		return false
	}
	if _, voidOnly, planned := answerBodyPlan(fd, p); planned && voidOnly {
		return true
	}
	// A body with no `return` road at all — `def m(self): pass`, `def reset(self): self.x = 0` — answers the
	// void on every road that can reach it.
	returns, _ := answerBodyReturns(fd.Body)
	return len(returns) == 0
}

// methodAnswersOnlyTheVoid is the answer direction's METHOD round (roadmap **Gap R.452**, **ADR 0429**):
// the verdict `answersOnlyTheVoid` gives a module-level `def`, given to a body that lives in a class's own
// table instead of the module's. The two questions are ONE question — "does every road of this body answer
// the void?" — and they are answered by the ONE reading `answerBodyPlan`, which is why this lives beside
// that function rather than in the class file beside the tables that key a body by hand.
//
// The reason a method needs its own door and not simply the name-keyed one: `isNoneExpr` asked `g.fds` and
// the plan, both keyed by a MODULE-level `def`, so `class C():` / `    def m(self):` / `        return None`
// / `print(c.m() is None)` answered from the payload — the word `0`, which an int slot legally carries
// (**ADR 0187**) — and printed `False` at exit 0 where the reference prints `True`, while the identical
// question asked of a module-level `def` has printed `True` since **ADR 0427**.
//
// What it will NOT claim, in the same direction as every other census in this package: a body that is not
// an ordinary one (`async`, decorated, a generator), and a body with ANY road this file cannot name — one
// `return self.x` makes the whole body un-nameable, and the caller keeps the roads it has always had. A
// verdict that guesses is how `None` gets printed for a value.
func (g *irGen) methodAnswersOnlyTheVoid(fd *FuncDef) bool {
	// One reading, three doors: this is the same walk `answersOnlyTheVoid` answers a module `def` by and the
	// same walk a forwarding arm asks of its callee (**ADR 0232**), which is what keeps a method from being
	// the void at the print door and a value at the `is` (roadmap **Gap R.452**, **Gap R.454**).
	return bodyAnswersTheVoid(fd, g.answer)
}

// methodCallAnswersOnlyTheVoid is the `*Call` arm's question: is this an attribute call whose body the
// dispatch will run answers nothing but the void? The callee must be an attribute on a receiver the
// compiler can place — `c.m()`, `C().m()`, `self.m()` — because a call whose receiver the compiler cannot
// name is a call whose body it has never read.
func (g *irGen) methodCallAnswersOnlyTheVoid(c *Call) bool {
	if c == nil {
		return false
	}
	attr, isAttr := c.Fn.(*Attr)
	if !isAttr || attr.Name == nil || attr.Obj == nil {
		return false
	}
	return g.methodAnswersOnlyTheVoid(g.methodBodyOf(attr.Obj, attr.Name.Value))
}

// methodBodyOf resolves an attribute call to the FuncDef its dispatch runs, keyed the way the method
// tables key a body: by the RECEIVER'S CLASS and the name together, not by the name alone. The verdict
// census keys by name and drops an ambiguous one — the right conservatism when one class's body would
// stand in for another's (**ADR 0299**). It is the wrong conservatism for this question: the receiver's
// class is already known — that is what `receiverClass` answers — so two classes each declaring their own
// `__init__` are two bodies the compiler can each read, and a program with two constructors is not an
// ambiguous program.
func (g *irGen) methodBodyOf(recv Expr, name string) *FuncDef {
	if name == "" || recv == nil {
		return nil
	}
	if nm, isName := recv.(*Name); isName && nm.Value == "self" && g.curClassSrc != "" {
		// `self.helper()` inside a body: the class being lowered is the only answer, and only the
		// generator knows it (the same context `methodAnswer` asks for `self`).
		return g.methodCensus.declaredMethod(g.curClassSrc, name)
	}
	cls, ok := g.methodCensus.receiverClass(recv)
	if !ok {
		return nil
	}
	return g.methodCensus.declaredMethod(cls, name)
}

// declaredMethod answers WHICH BODY the dispatch runs for `recv.name()`: the class's own declaration, or —
// exactly as `resolveMethod` walks it at the call site — the first base that declares it (roadmap
// **Gap R.66**: the compiled backend walks every base and answers correctly, so a verdict that stopped at
// the class's own body would decline the question for an inherited method whose dispatch was never the
// problem). A duplicated class name is dropped from the table, so two classes, one name is no class.
func (c methodCensusT) declaredMethod(class, name string) *FuncDef {
	return c.declaredMethodAt(class, name, map[string]bool{}, 0)
}

// declaredMethodAt is the base walk, with the two guards a whole-program reading needs: a class already
// visited is not visited twice (a cycle in the source must not walk the stack away), and the depth is a
// bound, not a tuning number — no real hierarchy nests eight deep.
func (c methodCensusT) declaredMethodAt(class, name string, seen map[string]bool, depth int) *FuncDef {
	if class == "" || name == "" || depth > 8 || seen[class] {
		return nil
	}
	seen[class] = true
	cd, ok := c.classes[class]
	if !ok || cd == nil {
		return nil
	}
	for _, st := range cd.Body {
		if m, ok := st.(*FuncDef); ok && m != nil && m.Name == name {
			return m
		}
	}
	for _, b := range cd.Bases {
		if b == nil {
			continue
		}
		if fd := c.declaredMethodAt(b.Value, name, seen, depth+1); fd != nil {
			return fd
		}
	}
	return nil
}

// methodAnswerKey is the answer direction's key for a method body: the symbol its `define` is emitted
// under — the same name `registerClass` mints and `resolveMethod` hands back at the call site, so the
// body that stores a kind word and the caller that reads one are given no way to disagree about which
// word it is (roadmap **Gap R.453**, **ADR 0430**; ADR 0273's rule that a call and its `define` answer to
// one scan).
func methodAnswerKey(className, methodName string) string {
	return irSymbol(className + "_" + methodName)
}

// carriesMethod is the caller's question about a body that lives in a class: does a call to this method
// answer a value whose payload cannot say what it is?
func (p *answerKindPlan) carriesMethod(fnSymbol string) bool {
	return p != nil && p.methodCarry[fnSymbol]
}

// methodAnswerMayBeAnObject is the root's question: can any arm of this carried body answer an object
// rather than an immediate word? A container arm builds its object inside the callee's frame, which is
// closed the moment the call returns, so a payload read without a root is a handle to somebody else's
// bits the next allocation round (ADR 0181, ADR 0280, ADR 0430).
func (p *answerKindPlan) methodAnswerMayBeAnObject(fnSymbol string) bool {
	return p != nil && p.methodObject[fnSymbol]
}

// methodContainerKind is the answer "every road of this carried method body answers the SAME container",
// which is the claim the roads that read a container by name need in order to read the object a method
// built at all (roadmap **Gap R.453**, **ADR 0430**).
func (p *answerKindPlan) methodContainerKind(fnSymbol string) (containerAnswerKind, bool) {
	if p == nil {
		return notAContainerAnswer, false
	}
	k, ok := p.methodKind[fnSymbol]
	return k, ok && k != notAContainerAnswer
}

// methodReading hands the container door the body and the arm expressions it judged, so the question "are
// this answer's slots values the compiler can write down?" is asked of the ONE scan that answers it for a
// `def`'s answer too (roadmap Gap R.453, ADR 0430).
func (p *answerKindPlan) methodReading(fnSymbol string) (*FuncDef, []Expr, bool) {
	if p == nil {
		return nil, nil, false
	}
	fd, okBody := p.methodBodies[fnSymbol]
	exprs, okArms := p.methodArmExprs[fnSymbol]
	if !okBody || !okArms || fd == nil || len(exprs) == 0 {
		return nil, nil, false
	}
	return fd, exprs, true
}

// carriedMethodCall answers the doors' question — is this an attribute call whose body the answer
// direction carries — and hands back the symbol that body is emitted under. The receiver must be a class
// the compiler can place, because a call whose dispatch the compiler cannot name is a call that may run
// any body: the word it stores would be one of several, and the caller would read whichever the last call
// happened to leave there (**ADR 0232**'s collision, at a call boundary).
func (g *irGen) carriedMethodCall(c *Call) (string, bool) {
	if c == nil || g == nil {
		return "", false
	}
	attr, isAttr := c.Fn.(*Attr)
	if !isAttr || attr.Name == nil || attr.Obj == nil {
		return "", false
	}
	cls := g.receiverClass(attr.Obj)
	if cls == "" {
		return "", false
	}
	fn, ok := g.resolveMethod(cls, attr.Name.Value)
	if !ok || !g.answer.carriesMethod(fn) {
		return "", false
	}
	return fn, true
}

// armOf answers the callee's question: what kind word does this `return` road store?
func (p *answerKindPlan) armOf(n *ReturnStmt) (answerArm, bool) {
	if p == nil {
		return answerArm{}, false
	}
	a, ok := p.arms[n]
	return a, ok
}

// answerKindPlanOf reads the program once and decides the answer direction for every one of its
// module-level functions. `pairReturns` names the functions the pair road already serves; they
// keep it.
func answerKindPlanOf(prog *Program, pairReturns map[string]bool) *answerKindPlan {
	p := &answerKindPlan{
		carry:          map[string]bool{},
		void:           map[string]bool{},
		arms:           map[*ReturnStmt]answerArm{},
		methodCarry:    map[string]bool{},
		methodVoid:     map[string]bool{},
		methodObject:   map[string]bool{},
		methodKind:     map[string]containerAnswerKind{},
		methodBodies:   map[string]*FuncDef{},
		methodArmExprs: map[string][]Expr{},
		bodyClass:      map[*FuncDef]string{},
		classTable:     map[string]*declaredClass{},
	}
	if prog == nil {
		return p
	}
	fds := map[string]*FuncDef{}
	order := []string{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if _, seen := fds[fd.Name]; !seen {
				order = append(order, fd.Name)
			}
			fds[fd.Name] = fd
		}
	}
	sort.Strings(order)
	// The class side of the same walk: the methods, keyed by the symbol each body is emitted under. Read
	// once here, before any IR, because the `define` of a method and the calls to it are written by two
	// different roads — `registerClass` and the attribute-call door — and a kind word one of them forgot is
	// the wrong answer at the exit code of success (roadmap **Gap R.453**, **ADR 0430**).
	methods := map[string]*FuncDef{}
	methodOrder := []string{}
	declaredOnce := map[string]bool{}
	for _, st := range prog.Stmts {
		cd, ok := st.(*ClassDef)
		if !ok || cd == nil {
			continue
		}
		if declaredOnce[cd.Name] {
			// Two classes, one name: neither is that class, and no forwarding arm resolves through either
			// — **ADR 0299**'s conservatism about a duplicated name, read off the walk that fills the table
			// instead of written beside it (roadmap **Gap R.454**, **ADR 0431**).
			delete(p.classTable, cd.Name)
			continue
		}
		declaredOnce[cd.Name] = true
		dc := &declaredClass{methods: map[string]*FuncDef{}}
		p.classTable[cd.Name] = dc
		for _, b := range cd.Bases {
			if b != nil && b.Value != "" {
				dc.bases = append(dc.bases, b.Value)
			}
		}
		for _, ms := range cd.Body {
			fd, ok := ms.(*FuncDef)
			if !ok || fd == nil || fd.Name == "" {
				continue
			}
			if _, seen := dc.methods[fd.Name]; seen {
				dc.methods[fd.Name] = nil // two bodies in one class, one name: the dispatch answers neither
			} else {
				dc.methods[fd.Name] = fd
				p.bodyClass[fd] = cd.Name
			}
			key := methodAnswerKey(cd.Name, fd.Name)
			if _, seen := methods[key]; seen {
				continue
			}
			methodOrder = append(methodOrder, key)
			methods[key] = fd
		}
	}
	sort.Strings(methodOrder)
	// Rounds, not one pass: an arm that is `return other(...)` is answerable only once OTHER is
	// itself carrying a kind word, and other's own verdict may depend on a third body. Every round
	// only ever adds an entry (nothing here takes one away), so the marking grows monotonically and
	// the loop settles — the same fixed point ADR 0273's pair scan runs for the answer direction.
	for pass := 0; pass < 6; pass++ {
		grew := false
		for _, name := range order {
			arms, voidOnly, ok := answerBodyPlan(fds[name], p)
			if !ok {
				continue
			}
			if pairReturns[name] {
				// The pair road carries this body's answer, so no arm is taken from here — but the VOID
				// verdict still is. `def f():` / `    return None` is one of these: the pair road serves it,
				// and the print, truth and compare doors ask `answersOnlyTheVoid` BY NAME. Skipping the whole
				// body for a pair-served name left that question unanswered for exactly the bodies whose
				// payload is the word 0, which is how one program answered `None` to the printer and a number
				// to `==` (roadmap **Gap R.388**, **Gap R.228**'s Definition of done).
				if voidOnly {
					p.void[name] = true
				}
				continue
			}
			if p.carry[name] {
				continue
			}
			p.carry[name] = true
			if voidOnly {
				p.void[name] = true
			}
			for st, a := range arms {
				p.arms[st] = a
			}
			grew = true
		}
		// The METHOD round, in the same rounds: a body that forwards a module `def`'s answer (`def m(self):
		//     return f()`) is nameable only once `f` is, and `f` may itself be a forwarding body written
		// below the class (**ADR 0368**'s fixed point, read a second time by **ADR 0430**).
		for _, key := range methodOrder {
			if p.methodCarry[key] || p.methodVoid[key] {
				continue
			}
			fd := methods[key]
			arms, kind, mayBeObject, armExprs, ok := methodAnswerBodyPlan(fd, p)
			if ok {
				p.methodCarry[key] = true
				p.methodObject[key] = mayBeObject
				p.methodKind[key] = kind
				p.methodBodies[key] = fd
				p.methodArmExprs[key] = armExprs
				for st, a := range arms {
					p.arms[st] = a
				}
				grew = true
				continue
			}
			// The VOID half of the same round, and the reason this row had to add it: a void-only body is
			// never CARRIED (**ADR 0429**'s fold answers it without a word), so a forwarding arm reading only
			// `methodCarry` would name a call to a carried method and decline the same call over a void one —
			// two answers to one question, which is what **ADR 0232** exists to forbid, and it would strand
			// exactly the program **Gap R.452** paid (`def n(self):` / `    return self.m()` over a void `m`).
			if bodyAnswersTheVoid(fd, p) {
				p.methodVoid[key] = true
				grew = true
			}
		}
		if !grew {
			break
		}
	}
	return p
}

// answerOwns is the gate every road that writes `strFuncs` asks: a callable is registered as
// answering ONE kind only while it does answer one kind. A body the answer direction carries gets no
// entry, because the entry is what made its caller render the arm this table never saw through the
// printer of the arm it did (roadmap Gap R.219, ADR 0368).
func (g *irGen) answerOwns(fnName string) bool { return g.answer.carries(fnName) }

// answerOwnsMethod is the same gate at the class's door. `registerClass` registered a method as
// text-returning on the evidence of `methodReturnsStr` — which asks whether ANY road of the body hands
// back a text — so the body whose arms answer `"a"` and `0` was registered as a text and every caller
// rendered both arms with `rt_print_str`: `print(c.m(1))` / `print(c.m(0))` printed `a` / `a` where the
// reference prints `a` / `0`, at the exit code of success (roadmap **Gap R.453**, **ADR 0430**; the module
// half of this one guess is **Gap R.219** / **ADR 0368**, and `textReturningCallables` already asks the
// plan for a `def`).
func (g *irGen) answerOwnsMethod(fnSymbol string) bool { return g.answer.carriesMethod(fnSymbol) }

// answerDeclare emits the module's description of a carried answer: the one word per function that
// says which kind its last answer was. It is the same word the pair road uses, and a function is
// served by exactly one of the two roads, so the two never write it twice (roadmap L11.1, ADR 0273's
// convention, ADR 0368's second user).
func (g *irGen) answerDeclare(fnName string) {
	g.pairRetDone[fnName] = true
	if g.pairTagDeclared == nil {
		g.pairTagDeclared = map[string]bool{}
	}
	if !g.pairTagDeclared[fnName] {
		g.pairTagDeclared[fnName] = true
		g.globals.WriteString(pairTagGlobalIR(fnName))
	}
}

// answerDeclareMethod is the same description for a body that lives in a class: one word, named after the
// method's own `define`, declared where that `define` is written and read back by whichever caller was
// emitted first (**ADR 0276**'s rule that the call boundary is asked of the program, not of the emission
// order; roadmap **Gap R.453**, **ADR 0430**).
//
// It does NOT mark `pairRetDone`. That flag is the pair road's permission for the positions it serves and
// the gate that REFUSES every other position; seeding it for a method would turn `while c.m():`,
// `c.m() + 1` and `len(c.m())` into compile errors at exit 0 on programs the reference runs. The answer
// road stays an opt-in the tag-reading doors ask for themselves (the same distinction codegen.go draws for
// a module `def`, ADR 0273, ADR 0368).
func (g *irGen) answerDeclareMethod(fnSymbol string) {
	if g.pairTagDeclared == nil {
		g.pairTagDeclared = map[string]bool{}
	}
	if g.pairTagDeclared[fnSymbol] {
		return
	}
	g.pairTagDeclared[fnSymbol] = true
	g.globals.WriteString(pairTagGlobalIR(fnSymbol))
}

// answerReturn is the callee's side: the payload of one `return` road, produced by the same roads the
// ordinary return uses, and the constant kind word of the arm that ran. The word is stored before the
// deferred bodies run, exactly as the pair road stores its run-time tag: a `finally` that calls this
// function again must not leave its own answer's kind in the word (roadmap Gap R.23, ADR 0222).
func (g *irGen) answerReturn(b *strings.Builder, n *ReturnStmt, arm answerArm) error {
	payload := "0"
	word := arm.word
	switch {
	case arm.dyn != "":
		// `return other(...)`: the kind is the callee's own run-time fact, read with the same door a
		// print or a binding reads it (roadmap L11.1, Gap R.139's caller half).
		p, t, ok, err := g.pairCallPair(b, n.Expr)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("codegen: %q answers %s, whose own answer carries a kind word this door declined: the answer direction cannot store a kind it was promised (roadmap L11.1, Gap R.219, ADR 0368)", g.answerName(), exprSurface(n.Expr))
		}
		payload, word = p, t
	case word == answerWordList || word == answerWordDict || word == answerWordSet:
		// A container answer is an object, and an object has a handle rather than a global. The road that
		// builds it is the one a binding already uses; a builder that declines says so in words rather than
		// writing `ret i32 @.lst1`, which is the module `llc` rejects and ADR 0166 forbids outright
		// (roadmap Gap R.67, ADR 0333; the exit-2 face of the family this row closes).
		g.heapUsed = true
		h, _, okHandle := g.containerHandleOf(b, n.Expr)
		if !okHandle {
			h2, cerr := g.containerOperand(b, n.Expr)
			if cerr != nil {
				return cerr
			}
			if h2 == "" {
				return fmt.Errorf("codegen: %q answers a container this backend cannot build at the return; the arm's kind is a list, a dict or a set, and the builder that would have made it declined (roadmap Gap R.67, ADR 0333)", g.answerName())
			}
			h = h2
		}
		payload = h
	case word == answerWordStr:
		if txt, ok := g.stringVal(n.Expr); ok {
			// The raw text and its repr are interned together, so `print(f())`, `str(f())` and the element
			// printer inside a container all read the same @str_tab entry (ADR 0224, ADR 0258).
			payload = g.internStr(b, txt)
		} else {
			v, err := g.value(b, n.Expr)
			if err != nil {
				return err
			}
			payload = v
		}
	case word == answerWordNone:
		if n.Expr != nil {
			if _, err := g.value(b, n.Expr); err != nil {
				return err
			}
		}
	default:
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		payload = v
	}
	g.storePairTag(b, word)
	if err := g.runDeferred(b); err != nil {
		return err
	}
	g.gcCloseFrame(b)
	b.WriteString(fmt.Sprintf("  ret i32 %s\n", payload))
	return nil
}

// answerCallPair is the caller's question, narrow on purpose: it answers only for a call to a name the
// answer direction carries, so the roads that ask it cannot take a pair-shaped answer away from the
// pair road that has always owned those positions (roadmap L11.1, ADR 0273, ADR 0368).
func (g *irGen) answerCallPair(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	c, isCall := e.(*Call)
	if !isCall {
		return "", "", false, nil
	}
	// The popped slot is the one call answer whose kind the OBJECT holds rather than a callee's `return`,
	// so it is asked ahead of the name-carries-a-kind gate below — which is about a user function's word.
	// str(), repr(), the truth road and the f-string field ask THIS door, and each of them renders the
	// value by its tag, which is the whole point of asking (roadmap L11.1, ADR 0410).
	if p, t, okPop, perr := g.popAnswerPair(b, e); perr != nil {
		return "", "", false, perr
	} else if okPop {
		g.heapUsed = true
		g.floatFmtUsed = true
		return p, t, true, nil
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		// The same answer, asked at a class's door: a carried method's answer is the pair the print, truth,
		// `str`/`repr`, f-string and container-element roads already read (roadmap **Gap R.453**, **ADR 0430**).
		if _, isCarried := g.carriedMethodCall(c); isCarried {
			return g.pairCallPair(b, e)
		}
		return "", "", false, nil
	}
	if !g.answer.carries(nm.Value) {
		return "", "", false, nil
	}
	return g.pairCallPair(b, e)
}

// answerName names the body whose answer the direction is describing, for the sentences a refusal emits:
// a module `def` by its own name, a METHOD by the symbol its `define` carries, which is the name a reader
// of the module finds (roadmap **Gap R.453**, **ADR 0430**).
func (g *irGen) answerName() string {
	if g.curMethodAnswer != "" {
		return g.curMethodAnswer
	}
	return g.curFunc
}

// isCarriedAnswerCall is the one question the doors that read an ANSWER ask: is this expression a call
// whose body the answer direction carries, in the module or in a class? A body that answers nothing but the
// void is not one of them — its answer is a constant the fold already knows (**ADR 0427**, **ADR 0429**) —
// and an ordinary one-word call is not either, because it has no kind word and this door would be guessing
// one (roadmap **Gap R.453**, **ADR 0430**).
func (g *irGen) isCarriedAnswerCall(e Expr) bool {
	c, isCall := e.(*Call)
	if !isCall || g == nil {
		return false
	}
	if nm, isName := c.Fn.(*Name); isName {
		return g.answer.carries(nm.Value) && !g.answer.answersOnlyTheVoid(nm.Value)
	}
	_, ok := g.carriedMethodCall(c)
	return ok
}

// pairRoadStatesScalarLiteral is the short list of what the other side of `voidAgainstACarriedAnswer` may
// be: a written number, verdict or text, whose (payload, tag) pair `taggedOperand` states without walking
// anywhere. A float is NOT in the list: a carried float arm's payload is the word the float table left, and
// comparing it with a boxed literal is a different door's unshipped work (**Gap R.324**'s float half, named
// in `integration/mixed_answer_kind_test.go`).
func pairRoadStatesScalarLiteral(e Expr) bool {
	switch v := e.(type) {
	case *IntLit, *BoolLit, *StrLit:
		return true
	case *Name:
		return v.Value == "None" || v.Value == "True" || v.Value == "False"
	}
	return false
}

// voidAgainstACarriedAnswer is the identity question asked of an answer whose kind is a run-time fact:
//
//	class C():
//	    def m(self, x):
//	        if x:
//	            return 7
//	        return None
//
//	c = C()
//	print(c.m(0) is None)   # the reference: True — this backend, before: False, at exit 0
//
// The None-equality fold decides from what the SOURCE says each side is, and the source says a call is not
// a written `None`; which arm ran is not written anywhere the compiler can read, and the payload a void
// leaves behind is the word `0` — the same word an int slot legally carries (**ADR 0187**). So the question
// goes to the word the callee stored beside its own `ret`, through `rt_payload_eq`, which is the one
// equality that knows what a payload means and the only door that admits the void on either side: identity
// between two objects is not content equality, and a side whose kind the compiler can state already has the
// source-side fold (**ADR 0411**'s rule, read at the call boundary by roadmap **Gap R.453** / **ADR 0430**).
//
// The same door answers the module `def` twin — `def f(x):` / `    if x:` / `        return 7` /
// `    return None` / `print(f(0) is None)` — because "is this answer the void?" is one question about one
// value, and a fold that answers it for a `def` and not for a method is the two answers **ADR 0232** exists
// to forbid.
func (g *irGen) voidAgainstACarriedAnswer(b *strings.Builder, n *BinOp) (payload string, handled bool, err error) {
	switch n.Op {
	case "==", "!=", "is", "is not":
	default:
		return "", false, nil
	}
	answer, other := n.L, n.R
	if !g.isCarriedAnswerCall(answer) {
		answer, other = other, answer
		if !g.isCarriedAnswerCall(answer) {
			return "", false, nil
		}
	}
	if g.isCarriedAnswerCall(other) {
		// Two answers, two run-time words. This door leaves that equality to the door that has always asked
		// pairs: `comparePair` answers `f(1) == g(1)` across two BODIES with the reference's bytes, and this
		// road has no business re-deciding it. What it gets wrong is the collision this round's own design
		// creates — TWO calls of the SAME carried body, which share one kind word, so one arm's word answers
		// for both (`f(1) == f(0)` prints `True` where the reference prints `False`), pinned beside
		// **Gap R.460** in both suites rather than folded into a claim that the door declined.
		return "", false, nil
	}
	if !g.isNoneExpr(other) && !pairRoadStatesScalarLiteral(other) {
		return "", false, nil
	}
	// The other side must be a value rather than an expression with effects of its own: this door evaluates
	// only the answer, and a side that prints or allocates would lose those effects.
	switch other.(type) {
	case *NoneLit, *Name, *IntLit, *BoolLit, *StrLit:
	default:
		return "", false, nil
	}
	// The whole door is built in a scratch buffer and committed only once both sides answer: a read that
	// emitted its own bounds check and then changed its mind would leave instructions after a terminator,
	// which is the compiler's mistake reported as the program's (**ADR 0166**, the rule this file follows).
	var scratch strings.Builder
	p, t, okPair, perr := g.answerCallPair(&scratch, answer)
	if perr != nil {
		return "", true, perr
	}
	if !okPair {
		return "", false, nil
	}
	vp, vt, okVoid := g.taggedOperand(&scratch, other)
	if !okVoid {
		return "", false, nil
	}
	cmp := g.newTmp()
	fmt.Fprintf(&scratch, "  %s = call i32 @rt_payload_eq(i32 %s, i32 %s, i32 %s, i32 %s)\n", cmp, p, t, vp, vt)
	res := g.newTmp()
	fmt.Fprintf(&scratch, "  %s = icmp ne i32 %s, 0\n", res, cmp)
	g.markI1(res)
	if n.Op == "!=" || n.Op == "is not" {
		inv := g.newTmp()
		fmt.Fprintf(&scratch, "  %s = xor i1 %s, true\n", inv, res)
		g.markI1(inv)
		res = inv
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	b.WriteString(scratch.String())
	return g.asBoolI32(b, res), true, nil
}

// grantDiscardedCallAnswer is the statement position's permission. An expression statement keeps no
// word for what it evaluated: the call runs for its effects and the kind word the callee stored is
// never read, which is a different thing from a position that READS one word and would print a payload
// without its tag. The refusal this lifts belongs to the second kind, and leaving it on the first is
// the rule ADR 0273 wrote (roadmap L11.1, Gap R.146, ADR 0368).
func (g *irGen) grantDiscardedCallAnswer(e Expr) func() {
	c, isCall := e.(*Call)
	if !isCall {
		return func() {}
	}
	nm, isName := c.Fn.(*Name)
	if !isName || !g.pairRetDone[nm.Value] {
		return func() {}
	}
	g.pairCallAsked[nm.Value] = true
	return func() { delete(g.pairCallAsked, nm.Value) }
}

// answerTruthOf is the condition's door: the truth of a value whose payload cannot say what it is is
// asked of the tag, which is the same helper `and` and `or` ask about the operand they test
// (roadmap Gap R.147, ADR 0269; the call-answer half is Gap R.219, ADR 0368).
func (g *irGen) answerTruthOf(b *strings.Builder, e Expr) (string, bool, error) {
	p, t, ok, err := g.answerCallPair(b, e)
	if err != nil || !ok {
		return "", ok, err
	}
	r := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_pair_truth(i32 %s, i32 %s)\n", r, p, t)
	i1 := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", i1, r)
	return g.markI1(i1), true, nil
}

// answerBodyPlan decides one body. It returns the kind word of every `return` road, and whether
// the body needs them at all.
func answerBodyPlan(fd *FuncDef, p *answerKindPlan) (map[*ReturnStmt]answerArm, bool, bool) {
	arms, kinds, varied, voidOnly, _, ok := answerBodyArms(fd, p)
	if !ok {
		return nil, false, false
	}
	if !varied && len(kinds) <= 1 && !voidOnly {
		// Every arm answers the same kind: the name-keyed table the module already keeps says it, and a
		// second opinion here could only take an answer away. The void is the exception — its payload is
		// the word 0, and the printer that reads a word prints `0` where the reference prints `None`
		// (roadmap Gap R.228, ADR 0368).
		return nil, false, false
	}
	return arms, voidOnly, true
}

// methodAnswerBodyPlan is the same reading of a body that lives in a class, decided under one different
// rule (**Gap R.453**, **ADR 0430**). A `def` whose one answer kind is a container keeps the table
// **ADR 0333** built for it — `containerRet` names the kind, and the print road asks that table. A METHOD
// has no such table: `class C():` / `    def m(self):` / `        return [1, 2]` lowered the literal to its
// compile-time array and wrote `ret i32 @.lst1` into the method's return slot, which `llc` rejected at
// **exit 2** (**ADR 0166**'s compiler-bug code) on a program the reference prints `[1, 2]`, and
// `return {"a": 1}` printed the `@str_tab` position of `"a"` at the exit code of success. So the kind word
// is the only road a container answer has on this side of the class boundary, and a body that answers one
// is carried here even though it answers ONE kind — which is the one place the method round and the module
// round part company, and it is the difference between the two tables, not between two questions. The
// void-only body stays OUT of this round: **ADR 0429**'s fold already answers it statically, and a stored
// word no one reads would only add a global to the module.
func methodAnswerBodyPlan(fd *FuncDef, p *answerKindPlan) (map[*ReturnStmt]answerArm, containerAnswerKind, bool, []Expr, bool) {
	arms, kinds, varied, voidOnly, armExprs, ok := answerBodyArms(fd, p)
	if !ok || voidOnly {
		return nil, notAContainerAnswer, false, nil, false
	}
	if !varied && !answerKindsNeedAWord(kinds) {
		return nil, notAContainerAnswer, false, nil, false
	}
	mayBeObject := false
	for _, a := range arms {
		if a.dyn != "" || a.word == answerWordList || a.word == answerWordDict || a.word == answerWordSet {
			mayBeObject = true
		}
	}
	kind, isOne := containerAnswerKind(notAContainerAnswer), true
	for w := range kinds {
		switch w {
		case answerWordList:
			kind = listAnswer
		case answerWordDict:
			kind = dictAnswer
		case answerWordSet:
			kind = setAnswer
		default:
			isOne = false
		}
	}
	// A FORWARDED arm brings the callee's container kind with it. A body whose only road is `return self.m()`
	// states no kind in `kinds` — the word is the callee's run-time fact — and the doors that WALK an answer
	// (`len`, an index, `in`, a loop) choose their road from the kind, not from the word. Refusing to look at
	// the arm made those four doors refuse the answer of a method that answers a container while the direct
	// call to the same container answered (roadmap **Gap R.454**, **ADR 0431**; **ADR 0430**'s container round,
	// read through the forwarding arm, and the slot-reading half of it still **Gap R.464**'s). Two arms that
	// forward two DIFFERENT containers are not one kind, and
	// the door declines them exactly as it declines a body that mixes a list and a dict.
	if isOne {
		for _, a := range arms {
			if a.dyn == "" || a.fwdKind == notAContainerAnswer {
				continue
			}
			if kind != notAContainerAnswer && kind != a.fwdKind {
				isOne = false
				break
			}
			kind = a.fwdKind
		}
	}
	if !isOne {
		kind = notAContainerAnswer
	}
	return arms, kind, mayBeObject, armExprs, true
}

// answerKindsNeedAWord is the method round's version of the one-kind exemption: a body whose every arm
// answers an immediate word — a number, a verdict, a text — is described by the tables keyed by name and
// keeps them, while a body that answers an OBJECT needs the word that says which object printer to call.
func answerKindsNeedAWord(kinds map[string]bool) bool {
	if len(kinds) > 1 {
		return true
	}
	for w := range kinds {
		return w == answerWordList || w == answerWordDict || w == answerWordSet
	}
	return false
}

// answerForwardedMethodArm is the `*Call` arm's ATTRIBUTE road, and the whole of roadmap **Gap R.454**
// (**ADR 0431**):
//
//	class C():
//	    def m(self):
//	        return None
//	    def n(self):
//	        return self.m()
//
//	c = C()
//	print(c.n() is None, c.n())   # the reference: True None — this backend, before: False 0, at exit 0
//
// `answerArmOf`'s `*Call` arm read `v.Fn.(*Name)` and returned `ok=false` for anything else, so a body that
// forwarded a call to ANOTHER METHOD of its own class was one un-nameable road and the whole body declined —
// the same cliff **ADR 0428** stepped back from one shape earlier, when the forwarded callee was a module
// `def`. The answer is the callee's OWN word, void or carried, exactly as the name arm gives it, read out of
// the two tables the METHOD round keeps (`methodVoid`, `methodCarry`) rather than out of the callee's body:
// two methods that forward to each other are a reading that never ends, and a round reads a table.
//
// Three things it will not do, each declined and each pinned by a case in `pkg/lang/method_forward_test.go`:
//
//   - resolve a receiver the body's own spelling cannot name. `methodCensusT.receiverClass` knows a module
//     variable bound once to one class, and a plan that leaned on that would hand one class's answer word to
//     a call the dispatch may run against another (ADR 0299's ambiguity, wearing a name). `self` is the one
//     receiver whose class is written in the body that wrote the arm.
//   - forward a call to ITSELF. The name arm has the same exclusion (**ADR 0273**): the inner frame would
//     write the kind word after the outer caller has read it. A body whose only road is `return self.m()` is
//     one the plan leaves on the roads it has always had.
//   - read a class the plan never registered, or one declared twice. The first is a class the module has no
//     symbol for; the second is **ADR 0299**'s rule, and no arm resolves through either.
func answerForwardedMethodArm(fd *FuncDef, c *Call, p *answerKindPlan) (answerArm, bool) {
	if p == nil || fd == nil || c == nil {
		return answerArm{}, false
	}
	attr, isAttr := c.Fn.(*Attr)
	if !isAttr || attr == nil || attr.Name == nil || attr.Obj == nil || attr.Name.Value == "" {
		return answerArm{}, false
	}
	recv, isName := attr.Obj.(*Name)
	if !isName || recv.Value != "self" {
		return answerArm{}, false
	}
	cls := p.classOf(fd)
	if cls == "" {
		return answerArm{}, false
	}
	target, owner, found := p.methodAnswerBodyAt(cls, attr.Name.Value, map[string]bool{}, 0)
	if !found || target == nil || target == fd {
		return answerArm{}, false
	}
	key := methodAnswerKey(owner, attr.Name.Value)
	if p.methodVoid[key] {
		// FORWARDING the void answers the void — the constant word, not a forwarded one, for the reason
		// **ADR 0428** wrote for the module twin: a void-only callee has no word for an outer frame to wait
		// on, and the body still EVALUATES the call, so a `print` inside it still prints.
		return answerArm{word: answerWordNone}, true
	}
	if p.methodCarry[key] {
		// FORWARDING a carried answer forwards its run-time word, read at the return by the same door a print
		// or a binding reads it (**ADR 0368**, **ADR 0430**).
		arm := answerArm{word: answerWordVaried, dyn: key}
		if k, isContainer := p.methodContainerKind(key); isContainer {
			arm.fwdKind = k
		}
		return arm, true
	}
	return answerArm{}, false
}

// classOf names the class a method body was written in, and the empty string for a body the plan never saw
// in a class — a module `def`, or a method of a class declared twice (**ADR 0299**'s rule, applied by the
// walk that filled the table).
func (p *answerKindPlan) classOf(fd *FuncDef) string {
	if p == nil || fd == nil {
		return ""
	}
	return p.bodyClass[fd]
}

// methodAnswerBodyAt walks a class's own declarations and then every base in the order the source names
// them — the walk `resolveMethod` walks at the call site and `methodCensusT.declaredMethodAt` walks for the
// verdict census (**Gap R.66**: the compiled backend walks every base, so a reading that stopped at the
// class's own body would decline an inherited method whose dispatch was never the problem). It answers the
// body AND the class that declared it, because that class is the other half of the symbol the answer word
// is stored under (**Gap R.453**'s key). A class already visited is not visited twice, and the depth is a
// bound, not a tuning number.
func (p *answerKindPlan) methodAnswerBodyAt(class, name string, seen map[string]bool, depth int) (*FuncDef, string, bool) {
	if p == nil || class == "" || name == "" || depth > 8 || seen[class] {
		return nil, "", false
	}
	seen[class] = true
	dc, ok := p.classTable[class]
	if !ok || dc == nil {
		return nil, "", false
	}
	if fd, declared := dc.methods[name]; declared {
		// Declared-and-nil is the class's own ambiguity (two bodies, one name): the base walk does not
		// un-ambiguate a name the class itself declares twice.
		if fd == nil {
			return nil, "", false
		}
		return fd, class, true
	}
	for _, b := range dc.bases {
		if fd, owner, found := p.methodAnswerBodyAt(b, name, seen, depth+1); found {
			return fd, owner, true
		}
	}
	return nil, "", false
}

// answerBodyArms reads one body once and reports what its `return` roads answer: the kind word of each
// road, the set of kinds they cover, whether any of them forwards a call whose own kind is a run-time
// word, and whether the body answers NOTHING but the void. ok=false is the answer "some road's kind is not
// written in its spelling", and it retires the whole body — a plan that names four arms and misses the fifth
// is a plan that prints a payload wearing the fourth's bits (**ADR 0187**'s rule, **ADR 0368**'s reading).
func answerBodyArms(fd *FuncDef, p *answerKindPlan) (arms map[*ReturnStmt]answerArm, kinds map[string]bool, varied, voidOnly bool, exprs []Expr, ok bool) {
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || containsYield(fd.Body) {
		return nil, nil, false, false, nil, false
	}
	returns, fallsOff := answerBodyReturns(fd.Body)
	if len(returns) == 0 {
		// A body with no `return` at all answers the void, which the print road has always been able
		// to say (`isNoneExpr` asks `fdReturnsValue`); nothing here can improve on it.
		return nil, nil, false, false, nil, false
	}
	arms = map[*ReturnStmt]answerArm{}
	kinds = map[string]bool{}
	exprs = make([]Expr, 0, len(returns))
	for _, n := range returns {
		a, armOk := answerArmOf(fd, n.Expr, p)
		if !armOk {
			return nil, nil, false, false, nil, false // one arm the compiler cannot name: the body keeps its old roads
		}
		arms[n] = a
		exprs = append(exprs, n.Expr)
		if a.dyn != "" {
			varied = true
			continue
		}
		kinds[a.word] = true
	}
	if fallsOff {
		// Reaching the end of a body answers None, and the road that does it already stores the tag
		// for the pair road (`storePairTag`); counting it here is what makes `if x: return 7` answer
		// `7`/`None` rather than `7`/`0` (roadmap Gap R.228).
		kinds[answerWordNone] = true
	}
	voidOnly = !varied && len(kinds) == 1 && kinds[answerWordNone]
	return arms, kinds, varied, voidOnly, exprs, true
}

// answerArmOf asks the one question this file exists to ask: which kind does this `return`
// expression answer, and is the word it produces the natural word for that kind? ok=false means
// "the kind is not written in the spelling" — a name, an attribute, a float, a call the pass does
// not serve — and the caller leaves the whole function alone for that answer.
func answerArmOf(fd *FuncDef, e Expr, p *answerKindPlan) (answerArm, bool) {
	if e == nil {
		return answerArm{word: answerWordNone}, true // a bare `return` answers the void
	}
	if isStringExpr(e) {
		// A text, however it was built: the literal, the interpolation, the formatting, the repeat.
		// All four answer an @str_tab index, which is the payload the tag-reading printer wants.
		return answerArm{word: answerWordStr}, true
	}
	switch v := e.(type) {
	case *NoneLit:
		return answerArm{word: answerWordNone}, true
	case *BoolLit:
		return answerArm{word: answerWordBool}, true
	case *IntLit:
		return answerArm{word: answerWordInt}, true
	case *ListLit:
		return answerArm{word: answerWordList}, true
	case *DictLit:
		return answerArm{word: answerWordDict}, true
	case *SetLit:
		return answerArm{word: answerWordSet}, true
	case *Call:
		if a, armOk := answerForwardedMethodArm(fd, v, p); armOk {
			// `return self.m()` — the arm the NAME road below has never been able to read, because its callee
			// is an attribute and not a name (roadmap **Gap R.454**, **ADR 0431**). Asked FIRST and declined
			// whole: an attribute callee is not a name, so nothing below could say anything about it.
			return a, true
		}
		nm, isName := v.Fn.(*Name)
		if !isName || (fd != nil && nm.Value == fd.Name) {
			// A body that answers a call to ITSELF would have the inner frame write the word the
			// outer caller has not read yet — the same exclusion ADR 0273 made of the pair road.
			return answerArm{}, false
		}
		if p.answersOnlyTheVoid(nm.Value) {
			// FORWARDING the void answers the void — asked BEFORE the carried-only rule below, because a
			// void-only callee may be either carried (every arm a written `None`) or served by the pair
			// road (a one-arm body), and the forwarding body's answer is the same constant either way.
			// The carried-only rule exists so an inner frame writes its kind word before an outer caller
			// reads it, and a void-only callee has no word for the outer frame to wait on: every road of
			// it answers None, so this arm answers a CONSTANT word rather than forwarding one. The body
			// still EVALUATES the call — the arm is a kind, not a substitution — so `def g():` /
			// `    print("side")` / `    return f()` still prints `side`. Without this arm the forwarding
			// body declines whole, and `g() is None` falls to the fold that reads the payload — the word 0,
			// which an int slot legally carries (**ADR 0187**), one call deeper than the row this inherits
			// (**Gap R.388**, **ADR 0427**; roadmap **Gap R.451**).
			return answerArm{word: answerWordNone}, true
		}
		if !p.carries(nm.Value) {
			return answerArm{}, false
		}
		return answerArm{word: answerWordVaried, dyn: nm.Value}, true
	case *UnOp:
		if v.Op != "-" {
			return answerArm{}, false
		}
		a, ok := answerArmOf(fd, v.X, p)
		if !ok || a.dyn != "" || a.word != answerWordInt {
			return answerArm{}, false
		}
		return answerArm{word: answerWordInt}, true
	case *BinOp:
		if v.Op == "+" || v.Op == "-" || v.Op == "*" || v.Op == "//" || v.Op == "%" {
			// An arithmetic arm whose every leaf is a literal answers an int, and only an int: a name
			// in the expression is a kind this pass cannot see, and `/` is true division whatever its
			// operands are (ADR 0253), which is why it is not on the list.
			l, lok := answerArmOf(fd, v.L, p)
			r, rok := answerArmOf(fd, v.R, p)
			if lok && rok && l.dyn == "" && r.dyn == "" && l.word == answerWordInt && r.word == answerWordInt {
				return answerArm{word: answerWordInt}, true
			}
		}
	}
	return answerArm{}, false
}

// answerBodyReturns collects a body's own `return` roads and reports whether control can reach the
// end of the body without one. The walk stops at a nested `def` (its returns are its own) and is
// CONSERVATIVE about the fall-through: an answer of "yes" only ever adds a None arm, which is the
// safe direction, while "no" would let a road store no tag at all and leave the caller reading
// whatever the previous call left in the word.
func answerBodyReturns(sts []Stmt) (returns []*ReturnStmt, fallsOff bool) {
	walkStmtList(sts, &returns)
	if len(sts) == 0 {
		return returns, true
	}
	return returns, walksToEnd(sts[len(sts)-1])
}

func walkStmt(s Stmt, returns *[]*ReturnStmt) {
	switch st := s.(type) {
	case *ReturnStmt:
		*returns = append(*returns, st)
	case *IfStmt:
		for _, el := range st.Elifs {
			walkStmtList(el.Then, returns)
		}
		walkStmtList(st.Then, returns)
		walkStmtList(st.Else, returns)
	case *WhileStmt:
		walkStmtList(st.Body, returns)
		walkStmtList(st.Else, returns)
	case *ForStmt:
		walkStmtList(st.Body, returns)
		walkStmtList(st.Else, returns)
	case *TryStmt:
		walkStmtList(st.Body, returns)
		for _, ec := range st.Excepts {
			walkStmtList(ec.Body, returns)
		}
		walkStmtList(st.Finally, returns)
	case *WithStmt:
		walkStmtList(st.Body, returns)
	case *MatchStmt:
		for _, c := range st.Cases {
			walkStmtList(c.Body, returns)
		}
	case *FuncDef:
		// A nested def's returns belong to it, not to the body that wrote it.
	}
}

func walkStmtList(list []Stmt, returns *[]*ReturnStmt) {
	for _, st := range list {
		walkStmt(st, returns)
	}
}

// walksToEnd reports whether running a statement can reach the statement after it. Anything that
// is not a control-flow form obviously can; a `return` obviously cannot; the rest is answered
// conservatively, for the reason written above.
func walksToEnd(s Stmt) bool {
	switch st := s.(type) {
	case *ReturnStmt:
		return false
	case *IfStmt:
		if st.Else == nil && len(st.Elifs) == 0 {
			return true
		}
		if !allPathsEnd(st.Then) {
			return true
		}
		for _, el := range st.Elifs {
			if !allPathsEnd(el.Then) {
				return true
			}
		}
		if st.Else != nil && !allPathsEnd(st.Else) {
			return true
		}
		return false
	case *WhileStmt, *ForStmt, *TryStmt, *MatchStmt, *WithStmt:
		return true
	}
	return true
}

func allPathsEnd(list []Stmt) bool {
	if len(list) == 0 {
		return false
	}
	return !walksToEnd(list[len(list)-1])
}
