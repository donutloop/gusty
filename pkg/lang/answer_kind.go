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
	dyn  string // non-empty: `return other(...)`, so other's own word is this arm's word
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
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || containsYield(fd.Body) {
		return false
	}
	if _, voidOnly, planned := answerBodyPlan(fd, g.answer); planned && voidOnly {
		return true
	}
	// A body with no `return` road at all — `def m(self): pass`, `def reset(self): self.x = 0` — answers
	// the void on every road that can reach it. The module road answers this shape with `fdReturnsValue`,
	// which asks by REFLECTION and therefore walks into a nested `def`: `def m(self):` /
	// `    def inner():` / `        return 7` / `    return None` is a method that answers None and a
	// nested function that answers 7, and only the walk that STOPS at a nested body (**Gap R.392**'s two
	// roads over one AST) tells them apart.
	returns, _ := answerBodyReturns(fd.Body)
	return len(returns) == 0
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
	p := &answerKindPlan{carry: map[string]bool{}, void: map[string]bool{}, arms: map[*ReturnStmt]answerArm{}}
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
			return fmt.Errorf("codegen: %q answers %s, whose own answer carries a kind word this door declined: the answer direction cannot store a kind it was promised (roadmap L11.1, Gap R.219, ADR 0368)", g.curFunc, exprSurface(n.Expr))
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
				return fmt.Errorf("codegen: %q answers a container this backend cannot build at the return; the arm's kind is a list, a dict or a set, and the builder that would have made it declined (roadmap Gap R.67, ADR 0333)", g.curFunc)
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
	if !isName || !g.answer.carries(nm.Value) {
		return "", "", false, nil
	}
	return g.pairCallPair(b, e)
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
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || containsYield(fd.Body) {
		return nil, false, false
	}
	returns, fallsOff := answerBodyReturns(fd.Body)
	if len(returns) == 0 {
		// A body with no `return` at all answers the void, which the print road has always been able
		// to say (`isNoneExpr` asks `fdReturnsValue`); nothing here can improve on it.
		return nil, false, false
	}
	arms := map[*ReturnStmt]answerArm{}
	kinds := map[string]bool{}
	varied := false
	for _, n := range returns {
		a, ok := answerArmOf(fd, n.Expr, p)
		if !ok {
			return nil, false, false // one arm the compiler cannot name: the body keeps its old roads
		}
		arms[n] = a
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
	voidOnly := !varied && len(kinds) == 1 && kinds[answerWordNone]
	if !varied && len(kinds) <= 1 && !voidOnly {
		// Every arm answers the same kind: the name-keyed table the module already keeps says it, and a
		// second opinion here could only take an answer away. The void is the exception — its payload is
		// the word 0, and the printer that reads a word prints `0` where the reference prints `None`
		// (roadmap Gap R.228, ADR 0368).
		return nil, false, false
	}
	return arms, voidOnly, true
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
