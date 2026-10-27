// The generator's buffer: the one list this backend grows one `yield` at a time, and the two claims
// every door that receives it has to be able to ask of it — that the register standing there IS the
// buffer, and whether its slots have to be asked what each one holds.
//
// A generator's buffer is a list (`rt_alloc`'s HeapKindList, walked by `rt_get_elem`, printed by the list
// printers), and every other list this module builds is filled by a road that decided the element's kind
// before it wrote the slot: the literal door, the `xs.append(...)` door, the comprehension door. A
// generator's slots are written by the body, one element at a time, at the point in the control flow the
// body reaches — so the kind of what is in the buffer is a fact about the VALUES, and values are what
// `rt_append_tagged`'s companion word exists to carry (ADR 0187, ADR 0311). The buffer never asked it, and
// the four doors that receive the answer read the payload as a number: a body that yielded a text, an
// integer, the void and a float printed `0` `2` `0` `1` where the reference prints `a` `2` `None` `1.5`,
// at the exit code of success — the artifact this contract ranks worst (roadmap **Gap R.468**,
// **ADR 0433**).
//
// The claims are deliberately two, not one, and the split is the difference between an answer and a
// refusal. A buffer whose every yielded element is written in the source as a plain integer keeps the
// roads that print the word it always printed — `for v in c.m(): s = s + v` is a sum, not a pair, and the
// pair road's standing refusal (**Gap R.146**) has no business firing on it. A buffer that yields a text,
// a float, a verdict or the void has to be walked and rendered through the tag, which is what
// `genBufferSlots` says.
package lang

import (
	"fmt"
	"strings"
)

// genBufferAppend writes one yielded element into the buffer a generator answers, WITH the kind that
// element carries.
//
// `xs.append(1.5)` and `xs.append("a")` have gone through `rt_append_tagged` since **ADR 0311** precisely
// because payload and tag must not be two operations: an `rt_append` alone leaves the new slot reading
// back as whatever the previous tenant of that slot was tagged as (ADR 0187). A generator's buffer is the
// one list this backend grew without that door, so this is the single place the fix is made — the same
// `elemPayloadAndTag` the `append` road asks, asked of the yielded expression, and every door that
// receives the buffer asks the tag that word left behind (roadmap **Gap R.468**, **ADR 0433**).
//
// An element the tag doors cannot name keeps the word today's road appended and the tag that word was
// printed as: the untagged printer showed it as a number, and `TagInt` is that same answer read through
// the tag. Nothing that printed bytes before prints different ones; the slot simply stops inheriting a
// stranger's kind.
func (g *irGen) genBufferAppend(b *strings.Builder, h string, e Expr) error {
	if p, t, err := g.mixedElemTag(b, e); err == nil {
		fmt.Fprintf(b, "  call void @rt_append_tagged(i32 %s, i32 %s, i32 %s)\n", h, p, t)
		return nil
	}
	v, err := g.value(b, e)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "  call void @rt_append_tagged(i32 %s, i32 %s, i32 %d)\n", h, v, int(TagInt))
	return nil
}

// srcCarriesTags answers whether the object a `yield from` copies its elements out of writes the tags this
// copy may travel with: a generator's own buffer (every slot was written by `genBufferAppend`) and a list
// the container roads registered as one whose slots speak for themselves. Any other object is a list whose
// slots may never have been tagged at all, and a tag read out of one is a stale word wearing the
// element's bits — so the copy keeps the word the road always appended (**ADR 0187**'s rule, read at the
// second buffer; roadmap **Gap R.468**, **ADR 0433**).
func (g *irGen) srcCarriesTags(e Expr) bool {
	if c, isCall := e.(*Call); isCall && g.callIsGeneratorCall(c) {
		return true
	}
	if nm, isName := e.(*Name); isName {
		return g.genBuffers[nm.Value] || g.mixedLists[nm.Value]
	}
	return false
}

// markGenBuffer makes the two claims beside an operand — the IR register a call left a generator's answer
// in, or the source name a binding parked it under.
//
// The first claim (`genBuffers`) says the list standing there is a generator's buffer, which is what the
// `yield from` copy asks when it decides whether to read tags out of a source. The second
// (`genBufferSlots`) says the doors that walk or render it have to ask each SLOT what it holds, and it is
// earned by the callee's body alone: every symbol that can answer this call has to yield an element whose
// kind is written in the source as something that is not a plain integer. Asking it of every candidate is
// the conservative reading — the arm that runs is not written at the call site, and a join whose one arm
// never wrote a tag would have its stale word read as the other arm's kind.
//
// `listOperands` cannot carry either claim: that table also holds the lists a comprehension and a
// `sorted(...)` build, which are untagged objects and keep their printers (roadmap **Gap R.468**,
// **ADR 0433**).
func (g *irGen) markGenBuffer(operand string, syms ...string) {
	if operand == "" {
		return
	}
	if g.genBuffers == nil {
		g.genBuffers = map[string]bool{}
	}
	g.genBuffers[operand] = true
	if !g.genSymsUseSlots(syms) {
		return
	}
	if g.genBufferSlots == nil {
		g.genBufferSlots = map[string]bool{}
	}
	g.genBufferSlots[operand] = true
}

// genSymsUseSlots answers the question every receiving door asks of the callee symbols behind one
// buffer: does EVERY body that can fill it yield something whose kind the untagged road cannot render?
// An empty list answers no — a buffer nobody can attribute to a body keeps the roads it has always had.
func (g *irGen) genSymsUseSlots(syms []string) bool {
	if len(syms) == 0 {
		return false
	}
	for _, sym := range syms {
		if !g.bodyWritesKinds(sym) {
			return false
		}
	}
	return true
}

// callBufferUsesSlots is the same question read straight off the call, for the doors that hold the call
// and not the register — the `for` road above all, which decides per iteration whether the loop variable
// travels as a (payload, tag) pair (roadmap **Gap R.468**, **ADR 0433**).
func (g *irGen) callBufferUsesSlots(c *Call) bool {
	return g.genSymsUseSlots(g.callGenSymbols(c))
}

// registerGenBuffer records what the body of a generator tells the doors that receive its buffer: that the
// body yields at all (`genFuncs`, **ADR 0432**'s reading, taken by the caller that owns this symbol's
// registration) and whether any of its yielded elements carries a kind the untagged roads cannot render.
// The verdict is a question about the SOURCE, asked once at the body's door the way the reading itself is,
// so no call site has to know which of its arguments reached the `yield` (roadmap **Gap R.468**,
// **ADR 0433**).
func (g *irGen) registerGenBuffer(sym string, body []Stmt) {
	if sym == "" || body == nil {
		return
	}
	if g.genBodies == nil {
		g.genBodies = map[string][]Stmt{}
	}
	g.genBodies[sym] = body
}

// bodyWritesKinds is the question a receiving door asks of one callee: does this body yield anything whose
// kind the untagged road cannot render? It is read off the recorded body, at the door that receives the
// answer, so the verdict does not depend on which generator the emission walk registered first — the shape
// that made a `yield from` of an integer generator answer a sum on one ordering and refuse it on another
// (roadmap **Gap R.468**, **ADR 0433**).
func (g *irGen) bodyWritesKinds(sym string) bool {
	body, ok := g.genBodies[sym]
	if !ok {
		return false
	}
	return generatorBodyWritesKinds(body, g.calleeBodies(sym))
}

// calleeBodies resolves a `yield from <call>` source to the body, or the arms, that fill the buffer it copies.
// A source this pass cannot attribute — a parameter, a callee it never registered, a generator that copies
// itself — answers nothing, and nothing earns nothing: an unearned claim hands the loop a (payload, tag) pair,
// and **Gap R.146**'s standing refusal then fires on a program that was adding numbers up. Every arm of a
// dynamically dispatched source has to earn the claim, because the arm that runs is a run-time fact and the
// receiving door holds one register (roadmap **Gap R.468**, **ADR 0433**).
func (g *irGen) calleeBodies(self string) func(Expr) ([][]Stmt, bool) {
	seen := map[string]bool{}
	if self != "" {
		seen[self] = true
	}
	return func(e Expr) ([][]Stmt, bool) {
		c, ok := e.(*Call)
		if !ok {
			return nil, false
		}
		syms := g.callGenSymbols(c)
		if len(syms) == 0 {
			return nil, false
		}
		var bodies [][]Stmt
		for _, sym := range syms {
			if seen[sym] {
				return nil, false // already walked, or the body that is doing the copying: nothing to inherit
			}
			body, ok := g.genBodies[sym]
			if !ok {
				return nil, false
			}
			seen[sym] = true
			bodies = append(bodies, body)
		}
		if len(bodies) == 0 {
			return nil, false
		}
		return bodies, true
	}
}

// callGenSymbols answers the callee symbols that can fill the buffer this generator call answers — the one
// symbol a receiver the compiler can see resolves to, and every class's arm when the receiver's class is a
// run-time fact. It is `callIsGeneratorCall`'s resolution, returned as names so `markGenBuffer` can ask its
// question of each of them.
func (g *irGen) callGenSymbols(c *Call) []string {
	if c == nil {
		return nil
	}
	switch fn := c.Fn.(type) {
	case *Name:
		if g.genFuncs[fn.Value] {
			return []string{fn.Value}
		}
	case *Attr:
		if fn.Name == nil {
			return nil
		}
		if cls := g.receiverClass(fn.Obj); cls != "" {
			if sym, ok := g.resolveMethod(cls, fn.Name.Value); ok && g.genFuncs[sym] {
				return []string{sym}
			}
			return nil
		}
		if !g.methodCallCasesAreGenerator(fn.Name.Value) {
			return nil
		}
		var syms []string
		for _, cls := range g.classOrder {
			if sym, ok := g.resolveMethod(cls, fn.Name.Value); ok {
				syms = append(syms, sym)
			}
		}
		return syms
	}
	return nil
}

// generatorBodyWritesKinds asks a generator body the question the receiving doors care about: does it yield
// anything whose kind the untagged road cannot render? It is `containsYield`'s walk (**ADR 0432**'s reading,
// of which this is the second half) — an `if`, a `while`, a `for`, a `match` arm all hide a `yield` — minus
// the nested `def`, whose yields fill that body's own buffer.
//
// A `yield from` is asked of the SOURCE, which is why the walk takes a resolver: a literal answers for itself,
// and any other source earns the claim only through the body that fills it. The AST alone cannot tell a
// `yield from gen(3)` from a `yield from src`, and the difference is the difference between the sum the record
// holds an answer for and a pair the arithmetic position cannot carry (roadmap **Gap R.468**, **ADR 0433**).
func generatorBodyWritesKinds(stmts []Stmt, resolve func(Expr) ([][]Stmt, bool)) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
			if yieldedElementWritesItsKind(s.Expr) {
				return true
			}
		case *YieldFromStmt:
			if ll, ok := s.Expr.(*ListLit); ok {
				// A literal the pass CAN see: the buffer holds exactly these elements, so the body earns the
				// claim only when one of them is written as a kind the untagged road cannot render. A `yield from
				// [1, 2]` is the same buffer as two `yield 1` / `yield 2`, and it stays a sum.
				for _, el := range ll.Elems {
					if yieldedElementWritesItsKind(el) {
						return true
					}
				}
				return false
			}
			// Anything else is a source whose elements this pass cannot see from here — a parameter, a call whose
			// callee it has not registered, a generator that copies itself. The COPY still carries the tags when
			// the object writes them (`srcCarriesTags` asks that at the copy), but the claim that the doors must
			// READ a tag is earned by the body the source names, or not at all: the record holds `104` for
			// `def gen(n):` / `    yield i * i` above `def outer():` / `    yield from gen(3)`, and a blanket
			// claim here spent **Gap R.146**'s refusal on that sum.
			if resolve == nil {
				return false
			}
			bodies, ok := resolve(s.Expr)
			if !ok || len(bodies) == 0 {
				return false
			}
			// EVERY arm, for the same reason `markGenBuffer` asks the question of every symbol behind a joined
			// register: the door holds one value, and the arm that produced it is a run-time fact.
			for _, body := range bodies {
				if !generatorBodyWritesKinds(body, resolve) {
					return false
				}
			}
			return true
		case *IfStmt:
			if generatorBodyWritesKinds(s.Then, resolve) {
				return true
			}
			for _, e := range s.Elifs {
				if generatorBodyWritesKinds(e.Then, resolve) {
					return true
				}
			}
			if generatorBodyWritesKinds(s.Else, resolve) {
				return true
			}
		case *WhileStmt:
			if generatorBodyWritesKinds(s.Body, resolve) || generatorBodyWritesKinds(s.Else, resolve) {
				return true
			}
		case *ForStmt:
			if generatorBodyWritesKinds(s.Body, resolve) || generatorBodyWritesKinds(s.Else, resolve) {
				return true
			}
		case *MatchStmt:
			for _, c := range s.Cases {
				if generatorBodyWritesKinds(c.Body, resolve) {
					return true
				}
			}
		}
	}
	return false
}

// yieldedElementWritesItsKind is the question asked of one `yield`'s element: is what it holds written in
// the spelling? A text, a fractional number, a verdict, the void and a container say so; an integer says
// the opposite, because the untagged road already prints an integer correctly; and a name, a sum or a call
// says nothing at all — its kind is whatever some caller or some other pass bound, and a claim made about
// it here would be a guess the emitter then reads as fact (**ADR 0187**'s rule about a tag nobody wrote).
func yieldedElementWritesItsKind(e Expr) bool {
	switch e.(type) {
	case *StrLit, *FString, *FloatLit, *BoolLit, *NoneLit, *ListLit, *DictLit, *SetLit, *Tuple:
		return true
	}
	return false
}

// genBufferUsesSlots answers the receiving door's question — the `for` loop, the print door, a binding
// walked later: must this buffer's slots be asked what each holds? It is the claim `markGenBuffer` made at
// the call site, read back where the answer is consumed (roadmap **Gap R.468**, **ADR 0433**).
func (g *irGen) genBufferUsesSlots(operand string) bool {
	return operand != "" && g.genBufferSlots[operand]
}
