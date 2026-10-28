// codegen_iterator.go is the road a `for` takes over an object that answers the reference's ITERATOR
// PROTOCOL: `__iter__` hands back the iterator, `__next__` hands back one element, and the
// `StopIteration` the iterator raises when it has none left IS the loop's end signal (roadmap L12.2,
// Gap R.54, ADR 0449).
//
// Before this row the loop had one question for an iterable — can I count its slots? — and an object
// that answers a different question fell through it: `for x in R():` where `R.__next__` counts to two
// compiled cleanly, ran ZERO iterations, printed nothing, and exited **0**, the code that means the
// program ran. The reference prints `1`, `2`. A loop that silently runs zero times is the worst answer
// this backend can give, because it is indistinguishable from a loop whose iterable was empty.
//
// The protocol is not a container road. The loop asks the object, once, for its iterator (`__iter__`),
// roots what it is handed, and then per iteration emits `v = it.__next__()` through the ordinary
// binding road — the same road the statement `v = it.__next__()` takes — with THIS loop's end-signal as
// the handler the raise reaches. Because the binding is the ordinary road, the element keeps whatever
// kind the method answered with: a text stays a text, a float stays a float (ADR 0267's pair travels
// the same way it does from any call).
package lang

import (
	"fmt"
	"strings"
)

// iteratorState is the loop road's answer to "what does this iterable's class say about iteration?".
type iteratorState int

const (
	// iteratorNotProtocol is not our road at all: the iterable is not an object of a class the module
	// can name, so the container roads beside this keep answering — a list, a dict, a text, a range.
	iteratorNotProtocol iteratorState = iota
	// iteratorRuns is the protocol: the class says `__iter__`, and the object `__iter__` hands back
	// says `__next__`, both named by the source.
	iteratorRuns
	// iteratorNotIterable is a plain object: no `__iter__`, no `__next__`, and no `__getitem__` to fall
	// back on. The reference raises `TypeError: 'C' object is not iterable`, so the compiled loop raises.
	iteratorNotIterable
	// iteratorNotAnIterator is the object `__iter__` hands back having no `__next__`: the reference
	// raises, and this road refuses at the point the source names — where the class of the value is
	// still a fact the compiler holds.
	iteratorNotAnIterator
	// iteratorSequenceIteration belongs to `__getitem__`: the reference keeps calling it with 0, 1, 2…
	// until IndexError. No road here lowers that, so the loop says so instead of running zero times.
	iteratorSequenceIteration
	// iteratorOpaqueHandoff is `__iter__` returning a value the compiler cannot name a class for; the
	// element dispatch would be a run-time fact about an object the loop never saw.
	iteratorOpaqueHandoff
	// iteratorMixedHandoff is a loop over a frame parameter that one call site fills with an instance
	// of this class and another with something else: the protocol is a fact about the argument, and the
	// line does not say which argument will arrive.
	iteratorMixedHandoff
	// iteratorFloatAnswer is a `__next__` whose body answers a fractional number: the class road has no
	// float word for a method's answer, so the element would arrive as the payload without its kind.
	iteratorFloatAnswer
)

// iteratorPlan is what the loop road needs before it emits a line of the protocol.
type iteratorPlan struct {
	state iteratorState
	// class is the class the ITERABLE's value belongs to.
	class string
	// iterFn is the symbol of the `__iter__` the loop calls, "" when the class declares none.
	iterFn string
	// iterClass is the class of the object `__iter__` hands back — the one whose `__next__` runs.
	iterClass string
	// fromCallSite says the class came from a call site rather than from the line that loops: the loop
	// iterates a frame parameter, and what the element is is a fact about whoever called the frame.
	fromCallSite bool
	// param names the frame parameter the line loops over, for the two sentences that have to name it.
	param string
	// sentence carries the reference's own words for the states that end in a TypeError.
	sentence string
}

// iteratorProtocolOf asks the iterable what its class says. It is a whole-class question, asked of the
// table ADR 0446's resolution walk already built: a subclass inherits the protocol from its base the
// way it inherits every other method, and the loop asks the base too.
func (g *irGen) iteratorProtocolOf(e Expr) iteratorPlan {
	cls := g.instanceExpressionClass(e)
	fromCallSite := false
	paramName := ""
	if cls == "" {
		// The iterable's class is not written at the line that loops: `def total(it):` / `    for v in it:`
		// is answered by the call site that hands the frame its argument. That is a program-wide question,
		// so it is asked of the whole program — and a parameter some call fills with an instance and
		// another does not is refused, not guessed (roadmap L12.2, Gap R.517).
		if nm, isName := e.(*Name); isName && nm != nil && g.curFD != nil {
			c := g.iteratorCensusOf()
			// The mixed question is asked FIRST: a position that has seen two kinds has no single class to
			// answer with, and reading the class first would let a stale entry speak over the refusal.
			if c.mixed[g.curFD][nm.Value] {
				return iteratorPlan{state: iteratorMixedHandoff, class: nm.Value, param: nm.Value}
			}
			if bound := c.bound[g.curFD][nm.Value]; bound != "" {
				cls = bound
				fromCallSite = true
				paramName = nm.Value
			}
		}
	}
	if cls == "" {
		return iteratorPlan{state: iteratorNotProtocol}
	}
	_, hasIter := g.resolveMethod(cls, "__iter__")
	_, hasNext := g.resolveMethod(cls, "__next__")
	_, hasGetItem := g.resolveMethod(cls, "__getitem__")
	if !hasIter && !hasNext {
		if hasGetItem {
			return iteratorPlan{state: iteratorSequenceIteration, class: cls}
		}
		return iteratorPlan{state: iteratorNotIterable, class: cls, sentence: notIterableLoopSentence(cls)}
	}
	if !hasIter {
		// `__next__` alone is not an iterable: `iter(obj)` asks for `__iter__`, and the reference says
		// the object is simply not iterable.
		return iteratorPlan{state: iteratorNotIterable, class: cls, sentence: notIterableLoopSentence(cls)}
	}
	// `__getitem__` beside `__iter__` is the reference's fallback that never runs — `__iter__` wins — so
	// the protocol road goes, and the unbuilt fallback is recorded by the row that owns it.
	iterFn, _ := g.resolveMethod(cls, "__iter__")
	handBack, handBackKind, ok := g.iteratorHandoffClass(cls)
	if !ok {
		return iteratorPlan{state: iteratorOpaqueHandoff, class: cls, iterFn: iterFn}
	}
	if handBack == "" {
		// `__iter__` hands back a value that is not an object at all — `return 1`, `return "x"`. The
		// reference raises `TypeError: iter() returned non-iterator of type 'int'`, and the word the
		// sentence names is the literal's own kind, which the source already told us.
		return iteratorPlan{state: iteratorNotAnIterator, class: cls, iterFn: iterFn,
			sentence: "iter() returned non-iterator of type '" + handBackKind + "'"}
	}
	if _, ok := g.resolveMethod(handBack, "__next__"); !ok {
		return iteratorPlan{state: iteratorNotAnIterator, class: cls, iterFn: iterFn, iterClass: handBack,
			sentence: notIteratorSentence(handBack)}
	}
	// The element's kind is whatever `__next__` answers, and one kind still has no word to travel in:
	// the class road emits every method as an `i32` function whatever its body answers, so a fractional
	// answer arrives as the payload with the float-ness gone (roadmap L12.2, Gap R.519). The loop asks
	// the question the print road asks of a method answer, and refuses on the same answer.
	if fd := g.resolveMethodDef(handBack, "__next__"); fd != nil && funcReturnsFloat(g, fd) {
		return iteratorPlan{state: iteratorFloatAnswer, class: cls, iterFn: iterFn, iterClass: handBack}
	}
	return iteratorPlan{state: iteratorRuns, class: cls, iterFn: iterFn, iterClass: handBack, fromCallSite: fromCallSite, param: paramName}
}

// iteratorContainerAnswerRefusal guards the one shape the protocol loop cannot certify from inside a
// frame: the loop builds a container the frame hands back. The answer table (ADR 0333) describes what
// a callee's container holds by reading the bindings that wrote its slots, and a slot written from a
// loop whose element arrives from a call the compiler cannot see through has no kind for the table to
// record — absence was then read as "not a container", and the caller printed the allocator's slot
// number at the exit code of success (ADR 0344's fault, this row's shape). Refusing is the honest
// answer for the shape; the container that a loop over a class the SOURCE names answers unchanged
// (`for v in K():` beside this row's `programs/iterator_protocol.gy`).
func (g *irGen) iteratorContainerAnswerRefusal(n *ForStmt, p iteratorPlan) string {
	if !p.fromCallSite || g.curFD == nil {
		return ""
	}
	written := map[string]bool{}
	var walkBody func([]Stmt)
	collect := func(name string) {
		if name != "" {
			written[name] = true
		}
	}
	walkBody = func(sts []Stmt) {
		for _, st := range sts {
			switch s := st.(type) {
			case *ExprStmt:
				if c, ok := s.Expr.(*Call); ok && c != nil {
					if at, ok := c.Fn.(*Attr); ok && at != nil && at.Name != nil {
						switch at.Name.Value {
						case "append", "add", "insert", "extend":
							if nm, ok := at.Obj.(*Name); ok && nm != nil {
								collect(nm.Value)
							}
						}
					}
				}
			case *AssignStmt:
				if ix, ok := s.Target.(*Index); ok && ix != nil {
					if nm, ok := ix.Obj.(*Name); ok && nm != nil {
						collect(nm.Value)
					}
				}
			case *IfStmt:
				if s != nil {
					walkBody(s.Then)
					for _, e := range s.Elifs {
						if e != nil {
							walkBody([]Stmt{e})
						}
					}
					walkBody(s.Else)
				}
			case *WhileStmt:
				if s != nil {
					walkBody(s.Body)
				}
			case *ForStmt:
				if s != nil {
					walkBody(s.Body)
				}
			case *TryStmt:
				if s == nil {
					continue
				}
				walkBody(s.Body)
				for _, ex := range s.Excepts {
					if ex != nil {
						walkBody(ex.Body)
					}
				}
				walkBody(s.Finally)
			}
		}
	}
	walkBody(n.Body)
	if len(written) == 0 {
		return ""
	}
	var returns []string
	for _, st := range g.curFD.Body {
		if r, ok := st.(*ReturnStmt); ok && r != nil {
			if nm, ok := r.Expr.(*Name); ok && nm != nil && written[nm.Value] {
				returns = append(returns, nm.Value)
			}
		}
	}
	if len(returns) == 0 {
		return ""
	}
	return fmt.Sprintf("codegen: `for` over the parameter '%s' writes the container '%s' that this frame hands back, and the element arrives from a call the compiler cannot see through: the answer table has no kind to record for those slots, so the caller would print the allocator's slot where the reference prints the container (roadmap L12.2, Gap R.518)", p.param, returns[0])
}

// iteratorHandoffClass answers which class the object `__iter__` hands back belongs to. `return self`
// is the shape every protocol class writes; `return Inner()` names a second class the loop can also
// resolve.
//
// The three answers are: the class of the handed-back object ("" when it is not an object at all), the
// reference's word for its class in that case, and whether every `return` in the body agrees. A body
// whose returns disagree — `return self` on one path, `return []` on another — is not a protocol this
// road can promise, and the caller refuses rather than lowering the path it happens to like.
func (g *irGen) iteratorHandoffClass(cls string) (hand string, kind string, ok bool) {
	fd := g.resolveMethodDef(cls, "__iter__")
	if fd == nil {
		return "", "", false
	}
	returns := iteratorReturnValues(fd.Body)
	if len(returns) == 0 {
		// A body that returns nothing FALLS OFF ITS END, which in the reference is a return of the void:
		// `iter() returned non-iterator of type 'NoneType'`. The class is known, so the sentence is quotable.
		return "", "NoneType", true
	}
	answered := ""
	for _, r := range returns {
		switch v := r.(type) {
		case *Name:
			if v == nil || v.Value != "self" {
				return "", "", false
			}
			if answered != "" && answered != cls {
				return "", "", false
			}
			answered = cls
		case *Call:
			nm, isName := v.Fn.(*Name)
			if !isName || nm == nil {
				return "", "", false
			}
			if _, isClass := g.classInfos[nm.Value]; !isClass {
				// A call to an ordinary function: what class its answer belongs to is a fact about that
				// body, not about this line.
				return "", "", false
			}
			if answered != "" && answered != nm.Value {
				return "", "", false
			}
			answered = nm.Value
		default:
			// Only a value the source SPELLS as a literal has a class the compiler may put in the
			// reference's sentence. `return self.src`, `return f()` and every other expression whose class
			// the line does not state are an opaque hand-off: a refusal naming a kind the compiler guessed
			// would be the false claim Gap R.38 files.
			if iteratorLiteralIsSpellable(r) {
				return "", iteratorValueKindWord(r), true
			}
			return "", "", false
		}
	}
	if answered == "" {
		// A body that returns nothing has no iterator to hand back.
		return "", "NoneType", true
	}
	return answered, "", true
}

// iteratorLiteralIsSpellable reports whether the hand-off value is a literal, whose class the reference
// can be quoted naming.
func iteratorLiteralIsSpellable(e Expr) bool {
	if e == nil {
		return true // `return` alone — the reference names NoneType
	}
	switch e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit, *ListLit, *DictLit, *SetLit, *Tuple:
		return true
	}
	return false
}

// iteratorValueKindWord is the word the reference puts in `iter() returned non-iterator of type 'X'`
// for a value the source wrote literally. It is the spelling the reference's own TypeError uses for
// that value, not this compiler's word for it.
func iteratorValueKindWord(e Expr) string {
	switch e.(type) {
	case *IntLit:
		return "int"
	case *FloatLit:
		return "float"
	case *StrLit:
		return "str"
	case *BoolLit:
		return "bool"
	case *NoneLit:
		return "NoneType"
	case *ListLit:
		return "list"
	case *DictLit:
		return "dict"
	case *SetLit:
		return "set"
	case *Tuple:
		return "tuple"
	}
	return "object"
}

// iteratorReturnValues collects the value of every `return` in a body, including the ones inside an
// `if`, a `while` or a `try` — the reference takes whichever return the body reaches, and a plan that
// read only the last line would lower a loop whose element kind depends on a branch it never saw.
func iteratorReturnValues(body []Stmt) []Expr {
	var out []Expr
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch s := st.(type) {
			case *ReturnStmt:
				out = append(out, s.Expr)
			case *IfStmt:
				if s == nil {
					continue
				}
				walk(s.Then)
				for _, e := range s.Elifs {
					if e != nil {
						walk([]Stmt{e}) // an `elif` is a branch of the same shape: the walk recurses
					}
				}
				walk(s.Else)
			case *WhileStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *ForStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *TryStmt:
				if s == nil {
					continue
				}
				walk(s.Body)
				for _, ex := range s.Excepts {
					if ex != nil {
						walk(ex.Body)
					}
				}
				walk(s.Finally)
			}
		}
	}
	walk(body)
	return out
}

// notIteratorSentence is the reference's words for an iterator with no `__next__`.
func notIteratorSentence(kind string) string {
	return "'" + kind + "' object is not an iterator"
}

// iteratorCensus is the program-wide answer to “which parameter of which frame does some call site
// hand an instance of a class that answers the protocol?”. A loop inside a frame reads its iterable as
// the frame named it — `for v in it:` — and the only place that says what `it` holds is the call that
// filled it. Two answers are kept apart because they get different treatments: the parameter every
// such call agrees on (the loop runs the protocol), and the one that does not (the loop refuses, since
// guessing would answer one caller's program and silently zero-iterate another's).
//
// The census is deliberately narrow: an argument counts when the source writes its construction
// (`total(R())`) or writes a name the whole program only ever binds to one construction (`r = R()` /
// `total(r)`). An argument that is itself a parameter, a field or a call is not claimed — leaving it
// unclaimed keeps the container roads beside this answering every `for v in xs:` they answer today.
type iteratorCensus struct {
	bound map[*FuncDef]map[string]string
	mixed map[*FuncDef]map[string]bool
	// seenOther remembers that some call handed a value that is not a protocol instance to this
	// parameter — the half of the mixed question that a position only becomes mixed once a protocol
	// instance has ALSO been seen there.
	seenOther map[*FuncDef]map[string]bool
}

// iteratorCensusOf builds the census once per module.
func (g *irGen) iteratorCensusOf() iteratorCensus {
	if g.iterCensus != nil {
		return *g.iterCensus
	}
	c := iteratorCensus{bound: map[*FuncDef]map[string]string{}, mixed: map[*FuncDef]map[string]bool{}, seenOther: map[*FuncDef]map[string]bool{}}
	if g.program != nil {
		names := iteratorNameBindings(g.program, g)
		record := func(fd *FuncDef, pos int, kw string, cls string) {
			if fd == nil {
				return
			}
			var pname string
			if kw != "" {
				pname = kw
			} else {
				if pos < 0 || pos >= len(fd.Params) || fd.Params[pos] == nil {
					return
				}
				pname = fd.Params[pos].Name
			}
			if pname == "" || pname == "self" {
				return
			}
			if c.bound[fd] == nil {
				c.bound[fd] = map[string]string{}
				c.mixed[fd] = map[string]bool{}
			}
			if c.seenOther == nil {
				c.seenOther = map[*FuncDef]map[string]bool{}
			}
			if c.seenOther[fd] == nil {
				c.seenOther[fd] = map[string]bool{}
			}
			// A position is answered by the protocol only when EVERY call that reaches it hands the same
			// protocol-answering class. A position that sees a protocol instance and also sees a value of
			// any other kind — a list literal, an int, a text — is one loop body asked to serve two
			// iterables, and only the caller knows which arrives. The two sets are kept apart rather than
			// folded into one, so the answer does not depend on which call site the walk happened to visit
			// first: `walk(Count())` before `walk(3)` and after it must agree.
			if cls == "" {
				c.seenOther[fd][pname] = true
				if c.bound[fd][pname] != "" {
					c.mixed[fd][pname] = true
					c.bound[fd][pname] = ""
				}
				return
			}
			if c.seenOther[fd][pname] {
				c.mixed[fd][pname] = true
				c.bound[fd][pname] = ""
				return
			}
			if seen := c.bound[fd][pname]; seen != "" && seen != cls {
				c.mixed[fd][pname] = true
				c.bound[fd][pname] = ""
				return
			}
			if c.mixed[fd][pname] {
				return
			}
			c.bound[fd][pname] = cls
		}
		for _, call := range g.programCalls() {
			if call == nil {
				continue
			}
			fd := g.iteratorCallee(call)
			if fd == nil {
				continue
			}
			for pos, a := range call.Args {
				if k, isKw := a.(*KeywordArg); isKw && k != nil {
					record(fd, -1, k.Name, g.iteratorArgumentClass(k.Value, names))
					continue
				}
				record(fd, pos, "", g.iteratorArgumentClass(a, names))
			}
		}
	}
	g.iterCensus = &c
	return c
}

// iteratorCallee names the frame a call is about to run, when the source names it: a declared function,
// or a method whose receiver's class the line states. A call this cannot answer is not a call the
// census may claim a parameter for (the same rule ADR 0340 writes for the ordering builtins' door).
func (g *irGen) iteratorCallee(call *Call) *FuncDef {
	switch fn := call.Fn.(type) {
	case *Name:
		if fn == nil {
			return nil
		}
		return g.fds[fn.Value]
	case *Attr:
		if fn == nil || fn.Name == nil {
			return nil
		}
		cls := g.instanceExpressionClass(fn.Obj)
		if cls == "" {
			return nil
		}
		return g.resolveMethodDef(cls, fn.Name.Value)
	}
	return nil
}

// iteratorArgumentClass answers which protocol-answering class an argument hands to the frame, "" for
// an argument that is not obviously an instance of one.
func (g *irGen) iteratorArgumentClass(a Expr, names map[string]string) string {
	switch v := a.(type) {
	case *Call:
		if v == nil {
			return ""
		}
		nm, isName := v.Fn.(*Name)
		if !isName || nm == nil {
			return ""
		}
		if _, isClass := g.classInfos[nm.Value]; !isClass {
			return ""
		}
		if _, answers := g.resolveMethod(nm.Value, "__next__"); !answers {
			return ""
		}
		return nm.Value
	case *Name:
		if v == nil {
			return ""
		}
		cls := names[v.Value]
		if cls == "" {
			return ""
		}
		if _, answers := g.resolveMethod(cls, "__next__"); !answers {
			return ""
		}
		return cls
	}
	return ""
}

// iteratorNameBindings collects the names the WHOLE program binds to a construction of one class —
// `r = R()` on every path, never `r = 1`. A name bound two ways is no one's class, and the census
// leaves it alone rather than choose (ADR 0351's one-name-one-owner rule, read from the caller's side).
func iteratorNameBindings(prog *Program, g *irGen) map[string]string {
	out := map[string]string{}
	spoiled := map[string]bool{}
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch s := st.(type) {
			case *AssignStmt:
				if s == nil {
					continue
				}
				if nm, ok := s.Target.(*Name); ok && nm != nil {
					cls := g.iteratorArgumentClass(s.Value, nil)
					if cls == "" {
						spoiled[nm.Value] = true
					} else if seen, had := out[nm.Value]; had && seen != cls {
						spoiled[nm.Value] = true
					} else if !spoiled[nm.Value] {
						out[nm.Value] = cls
					}
				}
			case *FuncDef:
				if s != nil {
					walk(s.Body)
				}
			case *ClassDef:
				if s != nil {
					for _, m := range s.Body {
						if fd, ok := m.(*FuncDef); ok {
							walk(fd.Body)
						}
					}
				}
			case *IfStmt:
				if s != nil {
					walk(s.Then)
					for _, e := range s.Elifs {
						if e != nil {
							walk([]Stmt{e})
						}
					}
					walk(s.Else)
				}
			case *WhileStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *ForStmt:
				if s != nil {
					walk(s.Body)
					walk(s.Else)
				}
			case *TryStmt:
				if s == nil {
					continue
				}
				walk(s.Body)
				for _, ex := range s.Excepts {
					if ex != nil {
						walk(ex.Body)
					}
				}
				walk(s.Finally)
			}
		}
	}
	if prog != nil {
		walk(prog.Stmts)
	}
	for n := range spoiled {
		delete(out, n)
	}
	return out
}

// The whole-program call list is `programCalls()` — the reflective walk this module already pays for
// once (codegen_call.go). The census reads that list rather than walking the AST a second time.

// stmtForIterator lowers the protocol loop. The shape is the counter loop's shape — cond, body, back
// edge, else, end — with the element coming from a call instead of from a counter, and the end signal
// being an exception the loop itself is the first handler for.
func (g *irGen) stmtForIterator(b *strings.Builder, n *ForStmt, p iteratorPlan) error {
	// The iterable is evaluated ONCE: `for x in R():` builds one object, and the loop asks that object
	// for its iterator. The handle travels to the iterator slot below, where the collector is told.
	recv, err := g.value(b, n.Iter)
	if err != nil {
		return err
	}
	g.heapUsed = true
	itReg := recv
	if p.iterFn != "" {
		itReg = g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s)\n", itReg, p.iterFn, recv))
		// An exception out of `__iter__` is not the loop's end signal — it belongs to whoever asked for
		// the loop, so the check leaves it to the handler in force HERE, not to this loop's.
		g.checkExn(b)
	}
	// One slot for the iterator, rooted for the whole loop: the body allocates, the collector runs at
	// the safe point below, and an unrooted handle is an object with no reference (ADR 0181).
	g.iteratorSeq++
	itName := fmt.Sprintf("_gy_iter%d", g.iteratorSeq)
	b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", itName))
	if g.allocd == nil {
		g.allocd = map[string]bool{}
	}
	g.allocd[itName] = true
	g.gcStoreHandle(b, itReg, itName)
	// The element's class is the hand-off's class, so the ordinary binding road below resolves
	// `__next__` on the object the source named rather than on whatever the allocator left in the slot.
	if g.varClasses == nil {
		g.varClasses = map[string]string{}
	}
	g.varClasses[itName] = p.iterClass

	condL := g.newLabel("iter.cond")
	endL := g.newLabel("iter.end")
	elseL := g.newLabel("iter.else")
	stopL := g.newLabel("iter.stop")
	takeL := g.newLabel("iter.take")
	riseL := g.newLabel("iter.rise")

	b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
	b.WriteString(condL + ":\n")
	// `v = it.__next__()` — the ordinary binding road, so the element carries the kind the method
	// answered with. The handler pushed for the call is what a raise reaches, and it is the ONLY thing
	// that reaches it: the pop below means a raise from inside the body belongs to an enclosing `try`.
	g.handlerStack = append(g.handlerStack, stopL)
	bindErr := g.stmt(b, &AssignStmt{Target: n.Var, Value: &Call{Fn: &Attr{Obj: &Name{Value: itName}, Name: &Name{Value: "__next__"}}}})
	g.handlerStack = g.handlerStack[:len(g.handlerStack)-1]
	if bindErr != nil {
		return bindErr
	}
	// No exception: this iteration has its element, and the body runs.
	g.loopSafePoint(b, n.Body)
	g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: condL})
	for _, s := range n.Body {
		if err := g.stmt(b, s); err != nil {
			return err
		}
	}
	g.loopStack = g.loopStack[:len(g.loopStack)-1]
	b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))

	// The call raised. `StopIteration` is the protocol's end; anything else is the program's own
	// exception and goes on its way — to an enclosing handler, or down the raise-exit.
	b.WriteString(stopL + ":\n")
	code := g.newTmp()
	b.WriteString("  " + code + " = load i32, i32* @exn_code\n")
	isStop := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", isStop, code, exnClassCode("StopIteration")))
	g.markI1(isStop)
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isStop, takeL, riseL))
	b.WriteString(takeL + ":\n")
	g.clearExn(b)
	// Reaching the end through the end signal is a NORMAL completion: the `else:` clause runs, exactly
	// as it does for a loop that counted out. A `break` left for `endL` and skipped it.
	if len(n.Else) > 0 {
		b.WriteString(fmt.Sprintf("  br label %%%s\n", elseL))
	} else {
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
	}
	b.WriteString(riseL + ":\n")
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	if err := g.runDeferredInnermost(b); err != nil {
		return err
	}
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
	if len(n.Else) > 0 {
		b.WriteString(elseL + ":\n")
		for _, s := range n.Else {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
	}
	b.WriteString(endL + ":\n")
	return nil
}

// raiseNotIterable is the loop over an object the reference says cannot be iterated: the TypeError is
// RAISED, at the line that wrote `for`, catchable by the program's own `except TypeError:` and fatal to
// a program that has none — never zero iterations at the exit code of success (roadmap Gap R.54).
//
// The class the sentence names is the one the SOURCE wrote: the iterable is a construction or a name the
// program bound to one, so `notIterableLoopSentence` quotes the same word the reference prints — and it
// travels through the ordinary raise road, which fills `@exn_msg` with the captioned report and `@exn_arg`
// with the bare message, so `except TypeError as e:` binds what CPython's `str(e)` returns rather than a
// placeholder (Gap R.113's contract, Gap R.38's rule about what a message may claim).
func (g *irGen) raiseNotIterable(b *strings.Builder, n *ForStmt, p iteratorPlan) error {
	// The iterable is still evaluated: `for v in make_thing():` runs `make_thing()` before the reference
	// decides it cannot iterate the result, and a raise that skipped the call would erase its effects.
	if _, err := g.value(b, n.Iter); err != nil {
		return err
	}
	g.heapUsed = true
	g.setExn(b, exnCode("TypeError"), "TypeError", notIterableLoopSentence(p.class), n.Src)
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
	return nil
}

// iteratorRefusal is the sentence for the shapes this road will not guess at. Each names the construct
// the reference answers, the half that is missing, and the roadmap row that owns it.
func iteratorRefusal(p iteratorPlan) string {
	switch p.state {
	case iteratorSequenceIteration:
		return fmt.Sprintf("codegen: `for` over an object of class '%s' leans on __getitem__: the reference calls it with 0, 1, 2… until IndexError, and this backend lowers neither the call nor the end test (roadmap L12.2, Gap R.514)", p.class)
	case iteratorOpaqueHandoff:
		return fmt.Sprintf("codegen: `for` over an object of class '%s' asks its __iter__ for a value whose class the source does not name, so the element dispatch would be a run-time fact about an object the loop never saw (roadmap L12.2, Gap R.515)", p.class)
	case iteratorNotAnIterator:
		return fmt.Sprintf("codegen: `for` over an object of class '%s': the reference raises TypeError: %s, and this backend has no run-time iterator test to raise it from (roadmap L12.2, Gap R.516)", p.class, p.sentence)
	case iteratorMixedHandoff:
		return fmt.Sprintf("codegen: `for` over the parameter '%s': one call site hands this frame an instance of a class that answers __next__ and another hands it something the loop cannot ask, so which protocol the loop runs is a fact about the call, not about the line (roadmap L12.2, Gap R.517)", p.param)
	case iteratorFloatAnswer:
		return fmt.Sprintf("codegen: `for` over an object of class '%s' asks __next__ for a fractional number, and the class road emits every method as an i32 function whatever its body answers: the element would arrive as the payload without the word that says it is a float, and print would read it as an integer (roadmap L12.2, Gap R.519)", p.class)
	}
	return ""
}

// iteratorFrameRefusal is the refusal the loop asks after the plan is settled — the shape that is a
// protocol loop AND an answer the caller cannot be handed.
func (g *irGen) iteratorFrameRefusal(n *ForStmt, p iteratorPlan) string {
	return g.iteratorContainerAnswerRefusal(n, p)
}
