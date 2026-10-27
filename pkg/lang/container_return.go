package lang

// container_return.go — a container handed back by a call is a value (roadmap Gap R.67, ADR 0333).
//
// A function that hands back a container had no answer in this backend. `return [1, 2]` lowered the
// literal to its compile-time global and put that global in the `ret` — `ret i32 @.lst1` — which is not
// IR at all: `llc` rejected the module and `gustyc` exited **2**, the code the exit-code contract
// reserves for a compiler bug, on a program CPython runs and prints `[1, 2]`. `return xs` after
// `xs.append(1)` did build a module, because a run-time-built container's variable already holds a
// handle — and then `print(build())` printed `0`: the handle was the answer, nothing said so, and the
// print road had no container to ask. The same defect stood at `str`, at an f-string field, at a loop's
// iterable (which printed nothing at all) and at the truthiness test, where a non-empty list came back
// `False`.
//
// Two halves, both in this file.
//
//   - **The `return` materialises.** A literal is built by the builders a binding already uses
//     (`heapListFrom`/`heapDictFrom`/`heapSetFrom`, through `containerOperand`), so the word that crosses
//     the `ret` is a heap handle in a register and no global sits anywhere near a value position —
//     ADR 0188's rule, applied one statement further out than ADR 0163's binding rule.
//   - **The answer is read as a container.** `containerReturningFuncs` records, per function, the one
//     container kind all of its arms answer together with the arms themselves;
//     `containerAnswerOperand` lowers such a call to a fresh name holding the handle and writes the
//     container notebooks for that name — the notebooks every road already consults for a container
//     *variable* (`listVars`, `runtimeDicts`, `runtimeSets`). Re-dispatching the operand as that name is
//     what makes the print door, `str`, a condition and a loop's iterable answer the way they answer
//     `ys = build()` today, instead of each growing a private copy of the question.
//
// The gate is the row. A function is recorded only when **every** `return` in its body answers a
// container of one and the same kind, the body cannot fall off its end, and nothing about the body is
// opaque to the walk — a statement form the walk does not visit condemns the function rather than
// escaping the scan. Gap R.219 is the standing lesson: `strFuncs` registers a function as text-returning
// on the evidence of ONE arm, and the arm that returns a number then prints whatever byte string shares
// that interned table's index. A record that widens a door has to be true of every road through the body,
// because the caller sees no body at all: it holds one word and a name.
//
// The scan is pure and the answer is asked of the program, never of the emission order (ADR 0273's rule
// for the pair road's arity, applied to this table): a `def` written below its first call and a call
// written below that `def` read the same verdict.

import (
	"fmt"
	"strings"
)

// containerAnswerKind is what a container-returning function's answer is: the kind the printers, the
// length road and the element roads ask for, spelled the way `containerKindFromTy` spells it so that one
// vocabulary serves the binding road, the print road and this scan.
type containerAnswerKind int8

const (
	notAContainerAnswer containerAnswerKind = iota
	listAnswer
	setAnswer
	dictAnswer
	// tupleAnswer is the scan saying "the answer is a container, and it is the immutable one". It is a kind of
	// its own and NOT `listAnswer` even though the slots are the same shape, because everything this table's
	// list answer implies — the compile-time `@.lstN` global, the list's own printer, calling the answer a
	// `list` to a diagnostic — is a claim the tuple does not honour. A tuple answer is described and then handed
	// to the door that asks the OBJECT, which is the only place the bracket bit lives (roadmap L11.3, ADR 0361).
	tupleAnswer
)

func (k containerAnswerKind) String() string {
	switch k {
	case listAnswer:
		return "list"
	case setAnswer:
		return "set"
	case dictAnswer:
		return "dict"
	case tupleAnswer:
		return "tuple"
	}
	return ""
}

// containerAnswer is the scan's verdict about one function: the one container kind every road through its
// body answers, and the returned expressions that verdict was read from. The arms travel with the verdict
// so the caller can ask the build-site questions — does this list carry per-slot tags? — of the same
// expressions the callee's `return` was built from, with the codegen state the question needs (ADR 0232).
type containerAnswer struct {
	kind containerAnswerKind
	arms []Expr
	// fullyDescribed is set by the scan's described half alone. An entry in the opaque set — a container
	// the scan proved and the slot table declined — leaves it false, which is what makes the doors that
	// read a slot back through the COMPILE-TIME notebooks decline the answer while the doors that ask the
	// OBJECT itself take it (roadmap Gap R.261, ADR 0344).
	fullyDescribed bool
}

// containerAnswerRead is what a position means to read out of the answer a call hands back, and so what
// the answer has to BE. There are two kinds of reader, and the difference is who answers the question:
//
//   - print, str, repr, an f-string field and a binding hand the handle to a runtime entry point that walks
//     the object, and a condition asks the object's own count — none of them needs this table to have read
//     a slot, only to know which container it is; and
//   - an index read, a slice, a comparison, a builtin argument and a loop's iterable read a slot out of the
//     object using the notebooks this table writes (is the element text, does the slot carry a tag). Those
//     notebooks belong to the described answer alone: asked for an answer the table could not describe, they
//     send the reader to a word nobody wrote, which is how `for k in f()` printed `0` where the reference
//     prints the key (roadmap Gap R.261, ADR 0344).
type containerAnswerRead int

const (
	containerReadSlots   containerAnswerRead = iota // an index, a slice, a comparison, a builtin argument
	containerReadWhole                              // print(...), str(...), repr(...), an f-string field, a binding
	containerReadShape                              // a condition, `not`: is the answer empty, which is its count
	containerReadMembers                            // a membership test: the object is asked whether it holds
	containerReadWalk                               // a loop's iterable: every member, read through the notebooks
)

// takesRead asks the answer's own record whether a position may read it (roadmap Gap R.261, ADR 0344).
func (a containerAnswer) takesRead(read containerAnswerRead) bool {
	switch read {
	case containerReadWhole, containerReadShape, containerReadMembers:
		return true
	default:
		return a.fullyDescribed
	}
}

// containerReturningFuncs is the whole-program scan: which functions hand back a container, and which
// container. Default-deny in every direction — a function is absent unless the walk can prove the answer,
// and a program the walk cannot see through (a statement form it does not visit, a body that can fall off
// its end, an async or decorated definition, a generator) is absent too, keeping whatever road that
// function has always taken, refusal included.
func containerReturningFuncs(prog *Program) map[string]containerAnswer {
	answers, _ := containerAnswerScan(prog)
	return answers
}

// ContainerAnswerIsUndescribed is the scan's second answer in exported form: the functions whose `return`
// provably hands back a container, and which one, minus the ones whose slots the table could not
// describe. It is the set the number door refuses against (roadmap Gap R.261, ADR 0344).
func ContainerAnswerIsUndescribed(prog *Program, fnName string) bool {
	if prog == nil {
		return false
	}
	_, undescribed := containerAnswerScan(prog)
	_, ok := undescribed[fnName]
	return ok
}

// containerAnswerScan answers the scan's two questions in one walk. The first is the one the doors ask:
// which functions hand back a container, and which. The second is the one the *fall-through* asks, and it
// exists because the first, answered "no", used to be read as "not a container":
//
//	def g():
//	    return 5
//
//	def f():
//	    out = []
//	    out.append(g())
//	    return out
//
//	print(f())
//
// `f` hands back a list — its `return` says so, its body builds it — and only the slot table's refusal to
// follow a value written as a call (Gap R.67, which is right: a call can answer the (payload, tag) pair)
// keeps this table from promising what its slots hold. The roads that print a container consult the table;
// when it declines, `print` fell through to the number road and printed the heap handle as an integer. At
// exit 0. A decline is not an answer, and it is not evidence of a number either: the second set names the
// functions whose answer is a container this backend will not describe, so the number door can say so
// instead of printing what the allocator handed out (roadmap Gap R.261, ADR 0333's scan read a second
// time, ADR 0166's exit 1
// vocabulary).
func containerAnswerScan(prog *Program) (map[string]containerAnswer, map[string]containerAnswer) {
	out := map[string]containerAnswer{}
	undescribed := map[string]containerAnswer{}
	if prog == nil {
		return out, undescribed
	}
	funcs := map[string]*FuncDef{}
	order := []string{}
	defines := map[string]bool{}
	collectContainerFuncDefs(prog.Stmts, funcs, &order, defines)
	sites := containerAnswerCallSites(prog)

	var answerOf func(name string, fd *FuncDef, seen map[string]bool) containerAnswer
	answerOf = func(name string, fd *FuncDef, seen map[string]bool) containerAnswer {
		if containerBodyIsOpaque(fd) {
			return containerAnswer{kind: notAContainerAnswer}
		}
		returns, opaque := containerReturnsOf(fd.Body)
		if opaque || len(returns) == 0 || !bodyEndsInReturn(fd.Body) {
			return containerAnswer{kind: notAContainerAnswer}
		}
		// The call sites are asked beside the body, because a parameter is a hole in the callee's body
		// through which the caller's value becomes a slot of the answer. `f(1)` answering `[n, n + 1]` is
		// this table's program; `pack(2.5)` answering `[v, 1]` is the pair road's, and the two are told
		// apart by the word the caller wrote — the double crosses into the container as a (payload, tag)
		// pair, which is one word too many for this table and one the pair door refuses honestly.
		if containerAnswerHandsAPair(name, sites) {
			return containerAnswer{kind: notAContainerAnswer}
		}
		ask := containerKindOfIn(fd, funcs, defines, out, answerOf)
		kind := notAContainerAnswer
		for _, r := range returns {
			k := ask(r, seen)
			if k == notAContainerAnswer || (kind != notAContainerAnswer && kind != k) {
				return containerAnswer{kind: notAContainerAnswer}
			}
			kind = k
		}
		// The kind is only half the claim: the slots have to be values this table can describe. A slot that
		// is a call, a comprehension or a format is a value this scan cannot follow, and another road's to
		// refuse (roadmap Gap R.67). Refusing *that* is the case the opaque set exists for: the answer is
		// still a container, and no door may read its handle as a number.
		if !containerAnswerDescribesItsSlots(fd, returns, out, defines) {
			// The kind is recorded even though the slots are not: the doors that read the whole object —
			// print, str, repr, a binding, a condition — need the kind, and the doors that read a slot need
			// the refusal. What is not recorded is the arms list: a table that could not read the slots does
			// not get to describe them, and a loop over the answer asks only about its keys or elements,
			// which the record below answers (roadmap Gap R.261, ADR 0344).
			undescribed[name] = containerAnswer{kind: kind}
			return containerAnswer{kind: notAContainerAnswer}
		}
		return containerAnswer{kind: kind, arms: returns}
	}

	// Round until stable, the way `strReturningFuncs` does: `def a(): return b()` only becomes a container
	// answer once `b` is, and `b` may be written below `a`.
	for pass := 0; pass < 6; pass++ {
		changed := false
		for _, name := range order {
			if _, done := out[name]; done {
				continue
			}
			fd := funcs[name]
			if fd == nil {
				continue
			}
			if a := answerOf(name, fd, map[string]bool{name: true}); a.kind != notAContainerAnswer {
				out[name] = a
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	// A function the walk *proved* a container and then declined for its slots may also have been proved
	// while answering another function's forwarding call; and a function that ended up in `out` is not
	// opaque, whatever an intermediate pass said about it.
	for name := range out {
		delete(undescribed, name)
	}
	return out, undescribed
}

// containerKindOfIn is the "what container is this expression" question inside one body: a literal or a
// comprehension answers from its own shape, `list()`/`dict()`/`set()` from the builtin they are, a name
// from what the body bound it to, and a call from what the callee's own arms answer.
func containerKindOfIn(fd *FuncDef, funcs map[string]*FuncDef, defines map[string]bool, out map[string]containerAnswer, answerOf func(string, *FuncDef, map[string]bool) containerAnswer) func(Expr, map[string]bool) containerAnswerKind {
	var ask func(Expr, map[string]bool) containerAnswerKind
	ask = func(e Expr, seen map[string]bool) containerAnswerKind {
		if e == nil {
			return notAContainerAnswer
		}
		if lit, ok := emptyContainerLiteral(e); ok {
			// `xs = list()` is as run-time-built as `xs = []`: the same object, the same answer.
			e = lit
		}
		switch n := e.(type) {
		case *ListLit:
			// A tuple is a container, so the scan answers "container"; it is not the LIST road's container, so
			// it answers with its own kind. The roads that build a described list answer build the compile-time
			// `@.lstN` shape and call the printer by name, and both claims are wrong for the object whose own bit
			// says how it prints (roadmap L11.3, ADR 0361).
			if n.IsTuple() {
				return tupleAnswer
			}
			return listAnswer
		case *SetLit:
			return setAnswer
		case *DictLit:
			return dictAnswer
		case *Comp:
			switch n.Kind {
			case CompList:
				return listAnswer
			case CompSet:
				return setAnswer
			case CompDict:
				return dictAnswer
			}
			return notAContainerAnswer
		case *Call:
			nm, isName := n.Fn.(*Name)
			if !isName {
				return notAContainerAnswer
			}
			if !defines[nm.Value] {
				// A builtin, and only the three that build the empty container: Gap R.6's rule that a
				// fold through a builtin's meaning is allowed only where the program did not take the name.
				return builtinContainerKind(nm.Value, len(n.Args))
			}
			if seen[nm.Value] {
				return notAContainerAnswer
			}
			callee := funcs[nm.Value]
			if callee == nil || len(n.Args) > len(callee.Params) {
				return notAContainerAnswer
			}
			// `return build()` forwards build's answer, which is provably a container only if build's own
			// body says so. A cycle (`a` forwards `b`, `b` forwards `a`) is read through `seen` as no-answer
			// and condemns both: neither body says what the object is, and the caller would be printing a
			// handle it cannot name.
			seen[nm.Value] = true
			k := answerOf(nm.Value, callee, seen).kind
			delete(seen, nm.Value)
			return k
		case *Name:
			if fd == nil {
				return notAContainerAnswer
			}
			vals := bodyBindings(fd)[n.Value]
			if len(vals) == 0 {
				// A parameter, or a name the body never binds: the object is the caller's, and this scan
				// has not seen the caller. Handing back somebody else's container is not this row's answer
				// — that is Gap R.154's pair parameter beside an ordinary one.
				return notAContainerAnswer
			}
			k := notAContainerAnswer
			for _, v := range vals {
				vk := ask(v, seen)
				if vk == notAContainerAnswer || (k != notAContainerAnswer && k != vk) {
					return notAContainerAnswer // rebound to two kinds, or to something that is not a container
				}
				k = vk
			}
			// A mutation is evidence of the same kind: `xs = []` / `xs.append(1)` is a list before and
			// after, and a list the body both appends to and updates as a set is nothing this scan names.
			if mk := mutatedKind(fd, n.Value); mk != notAContainerAnswer && mk != k {
				return notAContainerAnswer
			}
			return k
		}
		return notAContainerAnswer
	}
	return ask
}

// collectContainerFuncDefs gathers the top-level definitions the scan may judge, and every name the
// program defines for itself so that a program's own `def list(): return [1]` is not read as the builtin
// (Gap R.6's rule, ADR 0199). Methods inside a `class` are recorded as defined names but are not judged
// here: a method's answer reaches its callers through the class dispatch table, which is not this door
// (roadmap L11.3's instance road).
func collectContainerFuncDefs(ss []Stmt, funcs map[string]*FuncDef, order *[]string, defines map[string]bool) {
	for _, st := range ss {
		switch st := st.(type) {
		case *FuncDef:
			defines[st.Name] = true
			if _, seen := funcs[st.Name]; !seen {
				*order = append(*order, st.Name)
				funcs[st.Name] = st
			}
		case *ClassDef:
			collectContainerFuncDefs(st.Body, funcs, order, defines)
		}
	}
}

// builtinContainerKind answers the zero-argument container builtins the scan has to read as objects:
// `list()`, `dict()` and `set()` build the same thing `[]`, `{}` and `set()` do. `tuple()` is not one of
// them — a tuple is L11.3's open row, and its answer is not a heap container here.
func builtinContainerKind(name string, argc int) containerAnswerKind {
	if argc != 0 {
		return notAContainerAnswer
	}
	switch name {
	case "list":
		return listAnswer
	case "set":
		return setAnswer
	case "dict":
		return dictAnswer
	}
	return notAContainerAnswer
}

// containerAnswerDescribesItsSlots asks the question that keeps this table inside its own remit: is every
// slot of the answer — and every value the body writes into it — a value the compiler can write down: a
// number, a text, a bool, None, or another container of those.
//
// A slot that is a parameter, a call or an arithmetic answer may be a (payload, tag) pair, which is the
// pair door's object: `def pack(v): return [v, 1]` / `pack(2.5)` printed `[2, 1]` for `[2.5, 1]` the once
// this table claimed it, because the pair the parameter carried was quietly read as its payload. The pair
// road was honest about refusing that program, and ADR 0273's rule for the pair door reads both ways: a
// scan that opens a door must not take a program out of the road that named what it could not carry
// (roadmap Gap R.67).
func containerAnswerDescribesItsSlots(fd *FuncDef, returns []Expr, out map[string]containerAnswer, defines map[string]bool) bool {
	if fd == nil {
		return false
	}
	params := map[string]bool{}
	for _, p := range fd.Params {
		if p != nil && p.Name != "" {
			params[p.Name] = true
		}
	}
	binds := bodyBindings(fd)
	reader := containerAnswerSlotReader{binds: binds, out: out, defines: defines}
	slot := reader.slot
	for _, arm := range returns {
		// `return build()` forwards an answer this table already judged, and the judgement included
		// build's own slots; the arguments still have to be values this body can write down, because
		// `def a(x): return b(x)` hands the caller's word to the container underneath.
		if c, isCall := arm.(*Call); isCall {
			if nm, isName := c.Fn.(*Name); isName {
				// `list()`, `dict()` and `set()` build the same object the empty literals do, and the scan
				// already reads them as the answer's kind (Gap R.6's rule: only where the program did not
				// take the name for itself).
				if !defines[nm.Value] && builtinContainerKind(nm.Value, len(c.Args)) != notAContainerAnswer {
					continue
				}
				if _, ok := out[nm.Value]; !ok {
					return false
				}
				for _, a := range c.Args {
					if kw, isKw := a.(*KeywordArg); isKw {
						if !slot(kw.Value, 1) {
							return false
						}
						continue
					}
					if !slot(a, 1) {
						return false
					}
				}
				continue
			}
			return false
		}
		if !slot(arm, 0) {
			return false
		}
	}
	// The body's own writes are the slots too: `xs = []` answers nothing about what `xs.append(v)` puts
	// in it, and a name the arms answer by is filled by the statements above them.
	writes, opaque := containerAnswerSlotWrites(fd)
	if opaque {
		return false
	}
	for _, w := range writes {
		if !slot(w, 0) {
			return false
		}
	}
	return true
}

// containerAnswerSlotReader is the slot question of the answer table, held with the three records it
// asks the question of — what the body bound each name to, what the other functions' answers were, and
// what names the program took for itself. It is a type because the question is asked by two doors: the
// full one below, and the narrower one a loop over the answer asks about its keys or elements
// (roadmap Gap R.261, ADR 0344) — one question asked once, which is ADR 0232's rule.
type containerAnswerSlotReader struct {
	binds   map[string][]Expr
	out     map[string]containerAnswer
	defines map[string]bool
}

// slot answers whether one expression is a value the answer table can write into a container's slot.
func (r containerAnswerSlotReader) slot(e Expr, depth int) bool {
	if depth > 4 {
		return false
	}
	if lit, isBuiltinEmpty := emptyContainerLiteral(e); isBuiltinEmpty {
		// `xs = set()` is the same object as `xs = set{}`, and the same answer: the call form is what
		// the body wrote, not a value that came from somewhere else (Gap R.6's builtin rule, ADR 0199).
		e = lit
	}
	switch v := e.(type) {
	case *IntLit, *StrLit, *BoolLit, *NoneLit, *FloatLit:
		return true
	case *UnOp:
		return v.Op == "-" && r.slot(v.X, depth+1)
	case *ListLit:
		for _, el := range v.Elems {
			if !r.slot(el, depth+1) {
				return false
			}
		}
		return true
	case *Comp:
		// A comprehension builds its own object, tags and all: the road that fills it is the one that
		// knows what the slots hold, and this table only has to keep the handle (roadmap Gap R.67).
		return true
	case *SetLit:
		for _, el := range v.Elems {
			if !r.slot(el, depth+1) {
				return false
			}
		}
		return true
	case *DictLit:
		for _, k := range v.Keys {
			if !r.slot(k, depth+1) {
				return false
			}
		}
		for _, val := range v.Vals {
			if !r.slot(val, depth+1) {
				return false
			}
		}
		return true
	case *Name:
		// What the body bound the name to decides; a binding the construct wrote rather than a value
		// (`for i in ...`, `with x as v`) says nothing, and an unsaid name is the caller's word — the
		// call site is asked below, where the value has a kind.
		for _, val := range r.binds[v.Value] {
			if val == nil {
				continue
			}
			if !r.slot(val, depth+1) {
				return false
			}
		}
		return true
	case *BinOp, *Index, *CondExpr:
		return true // an arithmetic or reading answer: its kind is the call site's, asked below
	}
	// A call, a comprehension, a format: a value this scan cannot follow, and the pair door or
	// another road's. The table leaves the program where that road can say what it cannot carry.
	return false
}

// containerAnswerSlotWrites collects the values the body writes into a container's slots — `xs.append(v)`,
// `xs.add(v)`, `xs[i] = v`, `d[k] = v` — and reports the statements it cannot read that mention one of
// those names. A body it cannot read is declined with everything else: an appended parameter is exactly the
// pair this table must not print as a number.
func containerAnswerSlotWrites(fd *FuncDef) ([]Expr, bool) {
	if fd == nil {
		return nil, true
	}
	// Which names the body treats as a container at all: the bindings the arms answer by, plus every name
	// the body's container methods run on.
	container := map[string]bool{}
	w := pairWalker{method: func(recv Expr, member string) {
		nm, isName := recv.(*Name)
		if !isName {
			return
		}
		switch member {
		case "append", "extend", "insert", "pop", "remove", "sort", "reverse", "add", "discard", "update":
			container[nm.Value] = true
		}
	}}
	w.walkStmts(fd.Body)
	// A subscript store names its container too, and a write the scan cannot read is a write it must not
	// claim: an appended parameter is the pair this table would print as its payload.
	var writes []Expr
	opaque := false
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch st := s.(type) {
			case *ExprStmt:
				c, isCall := st.Expr.(*Call)
				at := (*Attr)(nil)
				if isCall {
					at, _ = c.Fn.(*Attr)
				}
				mentions := map[string]bool{}
				collectNames(st.Expr, mentions)
				touching := false
				for n := range mentions {
					if container[n] {
						touching = true
					}
				}
				if !touching {
					continue
				}
				if at == nil {
					opaque = true // it writes a container in a shape this walk does not read
					continue
				}
				for _, a := range c.Args {
					if kw, isKw := a.(*KeywordArg); isKw {
						writes = append(writes, kw.Value)
						continue
					}
					writes = append(writes, a)
				}
			case *AssignStmt:
				if ix, isIdx := st.Target.(*Index); isIdx {
					if nm, isName := ix.Obj.(*Name); isName {
						container[nm.Value] = true
						if container[nm.Value] {
							writes = append(writes, ix.Idx, st.Value)
						}
					}
				}
			case *IfStmt:
				walk(st.Then)
				for _, el := range st.Elifs {
					walk(el.Then)
				}
				walk(st.Else)
			case *ForStmt:
				walk(st.Body)
				walk(st.Else)
			case *WhileStmt:
				walk(st.Body)
				walk(st.Else)
			case *TryStmt:
				walk(st.Body)
				for _, h := range st.Excepts {
					h := h
					walk(h.Body)
				}
				walk(st.Finally)
			case *WithStmt:
				walk(st.Body)
			case *MatchStmt:
				for _, c := range st.Cases {
					walk(c.Body)
				}
			}
		}
	}
	walk(fd.Body)
	// A second walk, now that the subscript stores have joined the container names: a write hidden in a
	// nested body (`for i in range(3): xs.append(i)`) is the answer's slots as much as the literal is.
	walk(fd.Body)
	return writes, opaque
}

// mutatedKind asks what a name's slots are called by the methods the body runs on it: a name only ever
// appended to is a list, and a name both appended to and updated is nothing this scan can name.
func mutatedKind(fd *FuncDef, name string) containerAnswerKind {
	if fd == nil {
		return notAContainerAnswer
	}
	kind := notAContainerAnswer
	w := pairWalker{method: func(recv Expr, member string) {
		nm, isName := recv.(*Name)
		if !isName || nm.Value != name {
			return
		}
		var k containerAnswerKind
		switch member {
		case "append", "extend", "insert", "pop", "remove", "sort", "reverse":
			k = listAnswer
		case "add", "discard":
			k = setAnswer
		case "keys", "values", "items", "get", "update":
			k = dictAnswer
		}
		if k == notAContainerAnswer {
			return
		}
		if kind != notAContainerAnswer && kind != k {
			kind = notAContainerAnswer // used as two kinds: the body is not talking about one object
			return
		}
		kind = k
	}}
	w.walkStmts(fd.Body)
	return kind
}

// containerReturnsOf collects every `return` in a body — and reports `opaque` when the walk met a
// statement form it does not know how to see into. An `if` the collector skipped would hide a
// `return 0`, and the caller would then render a number through the container printer: Gap R.219's wrong
// answer, arrived at from the scan instead of from the call site.
func containerReturnsOf(body []Stmt) (returns []Expr, opaque bool) {
	var walk func(ss []Stmt)
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch st := s.(type) {
			case *ReturnStmt:
				returns = append(returns, st.Expr)
			case *ExprStmt, *AssignStmt, *AugAssignStmt, *ImportStmt, *BreakStmt, *ContinueStmt, *PassStmt,
				*RaiseStmt:
				// No return lives in these; the body continues past them.
			case *IfStmt:
				walk(st.Then)
				for _, el := range st.Elifs {
					walk(el.Then)
				}
				walk(st.Else)
			case *ForStmt:
				walk(st.Body)
				walk(st.Else)
			case *WhileStmt:
				walk(st.Body)
				walk(st.Else)
			case *TryStmt:
				walk(st.Body)
				for _, h := range st.Excepts {
					walk(h.Body)
				}
				walk(st.Finally)
			case *WithStmt:
				walk(st.Body)
			case *MatchStmt:
				for _, c := range st.Cases {
					walk(c.Body)
				}
			default:
				opaque = true
			}
		}
	}
	walk(body)
	return
}

// bodyEndsInReturn reports the one thing the answer half needs that the collector cannot give it: that
// execution cannot reach the end of the body without returning. A body that can fall off its end answers
// None on that road, and a caller told "list" would ask the list printer about a None. Only the syntactic
// last statement is trusted, because only that one is guaranteed to run.
func bodyEndsInReturn(body []Stmt) bool {
	if len(body) == 0 {
		return false
	}
	_, isRet := body[len(body)-1].(*ReturnStmt)
	return isRet
}

// containerBodyIsOpaque reports the bodies whose answer this scan must not claim at all: a generator or
// coroutine hands back an iterator, not the thing its `return` says, and a decorated function's answer is
// whatever the decorator built (ADR 0180's decorated roads own that shape, not this one).
func containerBodyIsOpaque(fd *FuncDef) bool {
	if fd == nil || fd.Async || len(fd.Decorators) > 0 {
		return true
	}
	return bodyYields(fd.Body)
}

// ---- the caller's door ---------------------------------------------------------------------------

// methodContainerAnswerOf is the scan's question, asked of a body in a class: does every road of this
// carried method answer the SAME container, and are its slots values the compiler can write down? The one
// reading `containerAnswerScan` runs for a `def` answers both halves — the body and its arm expressions are
// the ones the plan judged, and the slot question is the one the scan asks — so a method's answer and a
// function's answer are described by the same record and read by the same doors (roadmap **Gap R.453**,
// **ADR 0430**; **ADR 0232**'s one question, **ADR 0344**'s described/undescribed line).
func (g *irGen) methodContainerAnswerOf(c *Call) (containerAnswer, bool) {
	fn, isCarried := g.carriedMethodCall(c)
	if !isCarried {
		return containerAnswer{}, false
	}
	kind, isOne := g.answer.methodContainerKind(fn)
	if !isOne {
		return containerAnswer{}, false
	}
	fd, armExprs, okReading := g.answer.methodReading(fn)
	if !okReading {
		return containerAnswer{}, false
	}
	defines := map[string]bool{}
	for nm := range g.funcs {
		defines[nm] = true
	}
	return containerAnswer{
		kind:           kind,
		arms:           armExprs,
		fullyDescribed: containerAnswerDescribesItsSlots(fd, armExprs, g.containerRet, defines),
	}, true
}

// methodContainerAnswerOperand is this door's METHOD round (roadmap **Gap R.453**, **ADR 0430**). The roads
// that walk, measure or index a container read it BY NAME, which is the one thing an answer built inside a
// method's own frame is not — so the pair the method stored beside its `ret` is read, its handle is stored
// into a fresh rooted slot, and that slot is recorded under the kind every arm of the body answers. Only a
// body with ONE such kind is taken: a method whose arms answer a list and the number 3 has no container
// kind to record, and a loop over it must not be handed the payload of whichever arm ran.
//
// It declines for everything else, exactly as the door below does, and the caller continues on the road it
// has always taken (ADR 0273's rule beside the pair door).
func (g *irGen) methodContainerAnswerOperand(b *strings.Builder, c *Call, read containerAnswerRead) (Expr, containerAnswerKind, bool) {
	ans, okAnswer := g.methodContainerAnswerOf(c)
	if !okAnswer || !ans.takesRead(read) || !g.containerAnswerCallIsCarriable(c) {
		return nil, notAContainerAnswer, false
	}
	p, _, okPair, err := g.pairCallPair(b, c)
	if err != nil || !okPair {
		return nil, notAContainerAnswer, false
	}
	g.tmp++
	name := fmt.Sprintf("__mans%d", g.tmp)
	g.allocd[name] = true
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", "%_"+name))
	g.gcReg(b, name)
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", p, "%_"+name))
	// The same record a binding of a container answer writes — the kind, and whether the object's slots carry
	// tags — because the read below this door asks exactly the questions `recordContainerAnswer` answers:
	// a dict whose slot holds a list is a MIXED dict, and reading it through the untagged get hands back the
	// raw handle (`c.m()["k"]` printed the object's number where the reference prints `[1, 2]`).
	g.recordContainerAnswer(name, ans)
	return &Name{Value: name, Src: c.Span()}, ans.kind, true
}

// containerAnswerOperand lowers a call to a container-returning function and hands the answer back as the
// thing every container road already knows how to read: a name. The call is emitted, its handle is stored
// into a fresh slot that is rooted for the collector (ADR 0181's rule for every handle-carrying store),
// and the per-name container notebooks are written for that slot — so the print door, the `str` door, the
// truthiness door and the loop road answer the answer exactly as they answer `ys = build()`, which is the
// road that has been right about this object all along.
//
// It declines for anything that is not such a call, and every caller then continues on the road it has
// always taken: this is an extra answer, never a substitute for one (ADR 0273's rule for the pair door
// beside this one).
func (g *irGen) containerAnswerOperand(b *strings.Builder, e Expr) (Expr, containerAnswerKind, bool) {
	return g.containerAnswerOperandRead(b, e, containerReadSlots)
}

// containerAnswerOperandRead is the door above with the position's question in hand: an answer the scan
// proved and the slot table declined is taken only by a position whose `read` the record can answer —
// print, str, repr, a binding and a condition can, a loop and a membership test only when its keys or
// elements are described, and an index read, a comparison or a builtin argument never (roadmap Gap R.261,
// ADR 0344). Every caller that does not say otherwise asks the strictest question, so the rule can only
// ever widen a door that was already open, never open one that was shut.
func (g *irGen) containerAnswerOperandRead(b *strings.Builder, e Expr, read containerAnswerRead) (Expr, containerAnswerKind, bool) {
	c, isCall := e.(*Call)
	if !isCall {
		return nil, notAContainerAnswer, false
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		return g.methodContainerAnswerOperand(b, c, read)
	}
	ans, ok := g.containerAnswerOf(nm.Value)
	if !ok || !g.containerAnswerCallIsCarriable(c) {
		// The scan proved a container and the slot table declined to describe it. A position that can ask
		// the object what it is — `print`, `str`, `repr`, an f-string, a condition, a binding, a loop over
		// described keys — may still take the answer, and it takes it the way the binding road does: the
		// handle goes into a rooted slot recorded as a container variable, which is the state `ys = f()`
		// reaches and `print(ys)` prints right from (roadmap Gap R.261, ADR 0344). What the table will not
		// do is let a road that reads ONE SLOT back, and calls it a number, near the word — that refusal is
		// the call road's, taken with no permission out here.
		und, isUndescribed := containerAnswer{}, false
		if g.containerRetOpaque != nil {
			und, isUndescribed = g.containerRetOpaque[nm.Value]
		}
		if !isUndescribed || und.kind == notAContainerAnswer || !und.takesRead(read) {
			return nil, notAContainerAnswer, false
		}
		kind := und.kind
		g.containerAsked[nm.Value] = true
		h, err := g.value(b, c)
		delete(g.containerAsked, nm.Value)
		if err != nil {
			return nil, notAContainerAnswer, false
		}
		g.tmp++
		name := fmt.Sprintf("__cans%d", g.tmp)
		g.allocd[name] = true
		g.heapUsed = true
		b.WriteString(fmt.Sprintf("  %s = alloca i32\n", "%_"+name))
		g.gcReg(b, name)
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", h, "%_"+name))
		g.settleContainerKind(name, func() string {
			// The scan proved a container, and for a tuple the PHYSICAL kind it has to be recorded under is the
			// list's: the slots, the tag array and the object's own kind word all say `list`, and only the
			// attribute word says tuple. Claiming a `tuple` kind here would answer `no` to every door that asks
			// "is this walkable?", and `for x in b():` would refuse where it walks (roadmap L11.3, ADR 0361).
			if kind == tupleAnswer {
				return containerKindList
			}
			return kind.String()
		}())
		if kind == tupleAnswer {
			// …and the bracket claim rides beside it, on the name the answer was rooted under.
			g.recordTupleClaim(name)
			g.tupleUsed = true
		}
		return &Name{Value: name, Src: c.Span()}, kind, true
	}
	g.containerAsked[nm.Value] = true
	h, err := g.value(b, c)
	delete(g.containerAsked, nm.Value)
	if err != nil {
		return nil, notAContainerAnswer, false
	}
	g.tmp++
	name := fmt.Sprintf("__cans%d", g.tmp)
	g.allocd[name] = true
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  %s = alloca i32\n", "%_"+name))
	g.gcReg(b, name)
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", h, "%_"+name))
	g.recordContainerAnswer(name, ans)
	return &Name{Value: name, Src: c.Span()}, ans.kind, true
}

// containerAnswerSubscript is the one judgement a subscript makes before it chooses its road: is the thing
// being subscripted a call that hands back a container? If so the subscript is rewritten to name the rooted
// slot the answer was recorded under and the caller asks the road that reads a container **variable** — the
// bounds check, the hash, the tagged read and the catchable raise all come from that road, so `build()[7]`
// and `ys = build(); ys[7]` are one program written two ways rather than an answer and a refusal (roadmap
// Gap R.214, ADR 0334).
//
// Both spellings of a subscript ride the judgement, because the door underneath them now answers both. An
// `Index` asks a slot of the answer; a `Slice` asks a run of them, and `rt_slice` copies the slots WITH their
// tags, so the copy is a list like any other — printed by the tag-aware printer, measured by `len`, asked of
// by `in`, bound to a name. Leaving the slice out would keep `print(ys[0:2])` answering `[1, 2]` while
// `print(build()[0:2])` refused for want of a word, which is the base's spelling choosing the road again
// (roadmap Gap R.214's neighbour Gap R.230, ADR 0335).
//
// It declines for anything else and the caller continues on its own road (ADR 0273's rule: an extra answer,
// never a substitute for one) — so the fold keeps every literal it already answered.
func (g *irGen) containerAnswerSubscript(b *strings.Builder, e Expr) (Expr, bool) {
	switch n := e.(type) {
	case *Index:
		name, _, ok := g.containerAnswerOperand(b, n.Obj)
		if !ok {
			return e, false
		}
		redo := *n
		redo.Obj = name
		return &redo, true
	case *Slice:
		name, _, ok := g.containerAnswerOperand(b, n.Obj)
		if !ok {
			return e, false
		}
		redo := *n
		redo.Obj = name
		return &redo, true
	}
	return e, false
}

// recordContainerSlice writes what the roads look up for a name bound to a SLICE of a container —
// `ys = xs[0:2]`, `ys = build()[0:2]`. `rt_slice` builds a fresh heap list whose slots are the base's
// payloads copied WITH their tags (ADR 0187's pairing rule, applied to the slice road by ADR 0294), so
// the object the name receives is a list like any other and has to be recorded as one: the binding used
// to store the handle and record nothing, which is why `print(ys)` answered `1`, `len(ys)` refused for
// want of a string and `for v in ys:` printed a single `0` (roadmap Gap R.230).
//
// The one judgement is whether the copy carries tags, and — the rule ADR 0232 exists to enforce — it is
// asked of the base with the same predicates that chose the builder, never guessed: a tag is a claim about
// bytes some writer wrote, and sending a reader to a tag array nobody filled prints the interned index
// where the reference prints the text.
func (g *irGen) recordContainerSlice(name string, base Expr) bool {
	kind, ok := g.staticContainerKind(base)
	if !ok || kind != "list" {
		return false // a dict or a set is not sliced (the reference raises, and those doors are elsewhere)
	}
	g.settleContainerKind(name, containerKindList)
	g.mixedLists[name] = g.containerSlotsCarryTags(base)
	if g.mixedLists[name] {
		g.listElemStr[name], g.listElemInt[name] = false, false
	}
	delete(g.containerLits, name)
	delete(g.noneVars, name)
	delete(g.boolVars, name)
	if g.strVals != nil {
		delete(g.strVals, name)
	}
	if g.dictVals != nil {
		delete(g.dictVals, name)
	}
	return true
}

// containerSlotsCarryTags asks the sliced base the question its own builder asked when it wrote the
// object: does every slot say what it holds? The answers are the build sites' predicates, imported
// rather than restated — the literal road's `literalNeedsHeap || literalNeedsTags ||
// literalHasPairElement`, the answer road's `containerAnswerCarryTags`, and for a variable the notebook
// its builder left. A base the compiler cannot describe answers `no`, which is the conservative side: the
// plain printer is what it was printed by before this row existed (roadmap Gap R.230).
func (g *irGen) containerSlotsCarryTags(base Expr) bool {
	switch n := base.(type) {
	case *ListLit:
		return g.literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n)
	case *Name:
		return g.mixedLists[n.Value]
	case *Slice:
		return g.containerSlotsCarryTags(n.Obj)
	case *Call:
		if nm, isName := n.Fn.(*Name); isName {
			if a, ok := g.containerAnswerOf(nm.Value); ok {
				return g.containerAnswerCarriesTags(a)
			}
		}
	}
	return false
}

// sliceBaseIsDescribed is the gate in front of the copy. `rt_slice` walks a heap list and copies its slots
// WITH their tags, and the two roads that read the copy — the printer, which has to be told which printer, and
// the binding, which has to record what arrived — can only be asked for a base the records describe. Handed a
// base nobody describes, the road still emitted `rt_slice` over whatever word the base lowered to and the
// caller printed that word: `print(m()["k"][0:1])` answered the new list's heap handle as the number `3` at
// exit 0 (roadmap Gap R.232, ADR 0335's filed row). A refusal that names the missing record is what an
// undescribable base gets, because a fabricated number outranks any refusal in the defect ranking.
//
// The arms are `containerSlotsCarryTags`'s own, one question asked twice rather than two questions asked
// apart (ADR 0232's rule): a base whose tags the printer can be told about is exactly a base this answers yes
// for.
func (g *irGen) sliceBaseIsDescribed(base Expr) bool {
	switch n := base.(type) {
	case *ListLit:
		return true
	case *Name:
		return g.listVars[n.Value] || g.mixedLists[n.Value] || g.listLiteralOf(n) != nil
	case *Slice:
		return g.sliceBaseIsDescribed(n.Obj)
	case *Call:
		if nm, isName := n.Fn.(*Name); isName {
			_, ok := g.containerAnswerOf(nm.Value)
			return ok
		}
	}
	return false
}

// recordContainerAnswer writes what the roads look up for a container variable: which of the three it is,
// and that no static literal describes its slots (the answer is an object the run time built, however it
// was written, so the fold roads decline it exactly as they decline a container the program appended to).
//
// The one judgement is whether the list's slots describe themselves. A list the builder wrote without
// tags — `heapListFrom`, the shape `xs.append(1)` leaves behind — is printed by the list printer, and
// making it claim per-slot tags would send the printer to read tags nobody wrote; a list built tagged
// (`heapListFromTagged`, which sets the header's bit 8 for exactly this question) has to be printed by the
// mixed printer or `[1, "a", None]` prints its interned indices. The question is asked of the callee's own
// returned expressions — the same ones its `ret` was built from — with the codegen state the question
// needs (ADR 0232's rule, kept whole).
func (g *irGen) recordContainerAnswer(name string, ans containerAnswer) {
	switch ans.kind {
	case listAnswer, tupleAnswer:
		// Every claim the name held from another kind's object is settled by the one door, which
		// then lets the line below answer the only question the door cannot: do these slots
		// describe themselves (roadmap Gap Q.1, ADR 0232's rule).
		g.settleContainerKind(name, containerKindList)
		g.mixedLists[name] = g.containerAnswerCarriesTags(ans)
		// One more attribute of the same binding: is the object the callee handed back the immutable
		// spelling? The callee wrote the parentheses and the caller never saw them, so the fact has to be
		// read off the arms the `ret` was built from — and claimed only when every arm agrees, which is
		// ADR 0172's latest-binding rule asked of a return (roadmap L11.3, ADR 0361).
		if g.tupleVars == nil {
			g.tupleVars = map[string]bool{}
		}
		if ans.kind == tupleAnswer || g.containerAnswerIsTupleSpelling(ans) {
			g.tupleVars[name] = true
			g.tupleUsed = true
		} else {
			delete(g.tupleVars, name)
		}
	case dictAnswer:
		g.settleContainerKind(name, containerKindDict)
		// The builder's own question, asked here rather than guessed: `heapDictFrom` picks the tagged put —
		// and sets the object's bit 8 — when an entry's key or value mixes kinds, and the print, binding and
		// membership roads read that fact back out of `mixedDicts` before deciding whether to ask the slot or
		// the dict. An arm the walk cannot see (`return d`, a dict the body filled in) is left alone: those
		// slots were written by the store road, which recorded what it wrote, and claiming tags nobody wrote
		// sends the reader to an empty tag array — which is how `for k in m:` / `t + m[k]` over a run-built
		// answer started refusing with Gap R.146's sentence (roadmap Gap R.214, ADR 0232's rule).
		g.mixedDicts[name] = g.containerAnswerDictCarriesTags(ans)
	case setAnswer:
		g.settleContainerKind(name, containerKindSet)
		// The same question, asked of the same predicate `heapSetFrom` uses.
		g.mixedSets[name] = g.containerAnswerSetCarriesTags(ans)
	}
	// The four kind notebooks are what tells a loop over the container that its elements are text
	// rather than the @str_tab index the slot holds. The store road writes them for a container the
	// program filled in (`xs = {}` / `xs["a"] = 1` / `for k in xs:` prints `a`); an answer written by
	// the callee never passed through that road, so `for k in build()` printed the interned index where
	// CPython prints the key. They are read off the same arms the `ret` was built from, and only when
	// every arm agrees — a container whose slots the compiler cannot describe records nothing, which
	// is the shape the run time answers for itself (roadmap Gap R.67, ADR 0333).
	g.recordContainerAnswerElementKinds(name, ans)
	// A dict or a set whose slots carry their own tags has **no** single kind, and the builder says so by
	// clearing the notebooks on the variable it builds into. The answer is built with no variable in hand,
	// so the clearing has to happen here: a notebook claiming "the values are text" over a tagged dict sends
	// the reader to the untagged road, which hands back an @str_tab index and calls it a number (ADR 0232's
	// rule, read back through ADR 0333's table).
	if g.mixedDicts[name] {
		g.dictKeyStr[name], g.dictKeyInt[name] = false, false
		g.dictValStr[name], g.dictValInt[name] = false, false
	}
	if g.mixedSets[name] {
		g.setElemStr[name], g.setElemInt[name] = false, false
	}
	// The same clearing for a list whose slots carry their own tags. `rt_print_list_mixed` is asked before
	// the notebooks (ADR 0333's printer), so the whole container is unaffected — but a slot read is not: a
	// notebook saying "the elements are text" over a tagged object sends the reader to the untagged word
	// road, which has no tag to print and refuses with Gap R.82's sentence while `print(xs[1])` of a static
	// literal prints the text. That is why `def words(): return ["hi", "ho"]` refused both at the answer and
	// at its binding, while the same list filled by `append` answered (roadmap Gap R.214, ADR 0334).
	if g.mixedLists[name] {
		g.listElemStr[name], g.listElemInt[name] = false, false
	}
	delete(g.containerLits, name)
	delete(g.noneVars, name)
	delete(g.boolVars, name)
	if g.strVals != nil {
		delete(g.strVals, name)
	}
	if g.dictVals != nil {
		delete(g.dictVals, name)
	}
}

// recordContainerAnswerElementKinds writes the four kind notebooks from the arms the answer was read
// from. A text element is an index into @str_tab, and saying which kinds the slots hold is what lets the
// loop road, the element read and the printer render the value instead of the index — the same four facts
// the store road records one `xs["a"] = 1` at a time. Every arm has to say the same thing: a function that
// answers ["a"] on one road and [1] on another leaves the notebook silent, which is the direction that
// asks the object.
func (g *irGen) recordContainerAnswerElementKinds(name string, ans containerAnswer) {
	if len(ans.arms) == 0 {
		return
	}
	kindOf := func(elems []Expr) string {
		if len(elems) == 0 {
			return ""
		}
		allText, allInt := true, true
		for _, el := range elems {
			switch el.(type) {
			case *StrLit:
				allInt = false
			case *IntLit:
				allText = false
			default:
				return ""
			}
		}
		if allText {
			return "str"
		}
		if allInt {
			return "int"
		}
		return ""
	}
	unify := func(seen map[string]bool, k string) bool {
		if k == "" {
			return true // this arm says nothing about its slots; the other arms may still agree
		}
		for prior := range seen {
			if prior != k {
				return false
			}
		}
		seen[k] = true
		return true
	}
	one := func(seen map[string]bool) string {
		for k := range seen {
			return k
		}
		return ""
	}
	switch ans.kind {
	case listAnswer, tupleAnswer:
		seen := map[string]bool{}
		for _, arm := range ans.arms {
			lit, ok := arm.(*ListLit)
			if !ok || !unify(seen, kindOf(lit.Elems)) {
				return
			}
		}
		switch one(seen) {
		case "str":
			g.listElemStr[name] = true
		case "int":
			g.listElemInt[name] = true
		}
	case setAnswer:
		seen := map[string]bool{}
		for _, arm := range ans.arms {
			lit, ok := arm.(*SetLit)
			if !ok || !unify(seen, kindOf(lit.Elems)) {
				return
			}
		}
		switch one(seen) {
		case "str":
			g.setElemStr[name] = true
		case "int":
			g.setElemInt[name] = true
		}
	case dictAnswer:
		keys, vals := map[string]bool{}, map[string]bool{}
		for _, arm := range ans.arms {
			lit, ok := arm.(*DictLit)
			if !ok {
				return
			}
			if !unify(keys, kindOf(lit.Keys)) || !unify(vals, kindOf(lit.Vals)) {
				return
			}
		}
		switch one(keys) {
		case "str":
			g.dictKeyStr[name] = true
		case "int":
			g.dictKeyInt[name] = true
		}
		switch one(vals) {
		case "str":
			g.dictValStr[name] = true
		case "int":
			g.dictValInt[name] = true
		}
	}
}

// containerAnswerCallTable is the caller's side of the scan: which arguments each function is handed, and
// which names the program ever bound to a double. A parameter is a hole in the callee's body through which
// the caller's value becomes a slot of the answer, so the kind the caller wrote is part of what this table
// may claim — the same whole-program question `strArgKinds` asks for the text door (ADR 0199).
type containerAnswerCallTable struct {
	args  map[string][]Expr // callee -> every argument every call site hands it
	binds map[string][]Expr // name -> every value the program bound to it
	float map[string]bool   // names known to hold a double
}

func containerAnswerCallSites(prog *Program) containerAnswerCallTable {
	t := containerAnswerCallTable{args: map[string][]Expr{}, binds: map[string][]Expr{}, float: map[string]bool{}}
	w := pairWalker{
		call: func(fn string, args []Expr) {
			t.args[fn] = append(t.args[fn], args...)
		},
		assign: func(target, value Expr) {
			if value == nil {
				return
			}
			nm, isName := target.(*Name)
			if !isName {
				return
			}
			if kw, isKw := value.(*KeywordArg); isKw {
				value = kw.Value
			}
			t.binds[nm.Value] = append(t.binds[nm.Value], value)
		},
	}
	w.walkStmts(prog.Stmts)
	// Three rounds is enough for the chains a program writes (`x = y`, `y = 1.5`), and the direction of
	// doubt is the same as every other claim here: a name that may hold a double is treated as one.
	for pass := 0; pass < 3; pass++ {
		changed := false
		for name, vals := range t.binds {
			if t.float[name] {
				continue
			}
			for _, v := range vals {
				if t.argCarriesPair(v, 0) {
					t.float[name] = true
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	return t
}

// argCarriesPair is the syntactic half of the pair door's question: does this value reach the callee as a
// double — by literal, by arithmetic, by true division, by the name the program last bound it to? A value
// that does crosses a parameter into a container slot as a (payload, tag) pair, which is the pair road's
// object and one word too many for this table (roadmap Gap R.67, ADR 0273's rule read the other way).
func (t containerAnswerCallTable) argCarriesPair(e Expr, depth int) bool {
	if e == nil || depth > 4 {
		return false
	}
	switch v := e.(type) {
	case *FloatLit:
		return true
	case *BinOp:
		if v.Op == "/" {
			return true // CPython's true division answers a double whatever the operands were
		}
		return t.argCarriesPair(v.L, depth+1) || t.argCarriesPair(v.R, depth+1)
	case *UnOp:
		return t.argCarriesPair(v.X, depth+1)
	case *CondExpr:
		return t.argCarriesPair(v.If, depth+1) || t.argCarriesPair(v.Else, depth+1)
	case *Index:
		return t.argCarriesPair(v.Obj, depth+1)
	case *Name:
		if t.float[v.Value] {
			return true
		}
		for _, b := range t.binds[v.Value] {
			if t.argCarriesPair(b, depth+1) {
				return true
			}
		}
		return false
	}
	return false
}

// containerAnswerHandsAPair reports that some call site hands this function a double. The whole function
// steps aside for it, rather than the one call: the answer's `ret` is compiled once, and a body that sometimes
// carries a pair through a parameter is the pair road's body.
func containerAnswerHandsAPair(name string, sites containerAnswerCallTable) bool {
	for _, a := range sites.args[name] {
		if kw, isKw := a.(*KeywordArg); isKw {
			a = kw.Value
		}
		if sites.argCarriesPair(a, 0) {
			return true
		}
	}
	return false
}

// containerAnswerCarriesTags asks whether every arm the function can hand back had its per-slot tags
// written. A list literal answers from the same predicate its build site used; anything the compiler
// cannot read is taken as carrying tags, because the safe direction is the printer that asks the object
// and not the one that assumes.
// containerAnswerIsTupleSpelling asks whether every container the callee can hand back is the immutable
// spelling: the arms are the `return` expressions the scan read the kind from, and the question is the
// same one the print road asks of an expression — the `Tuple` flag on the container node, never a guess
// from the elements (roadmap L11.3, ADR 0361). An arm the walk cannot pin to a tuple (`return xs`, a
// comprehension, a call) answers no, which retires the claim rather than print brackets on a list.
func (g *irGen) containerAnswerIsTupleSpelling(ans containerAnswer) bool {
	if len(ans.arms) == 0 {
		return false
	}
	for _, arm := range ans.arms {
		if !g.isTupleExpr(arm) {
			return false
		}
	}
	return true
}

func (g *irGen) containerAnswerCarriesTags(ans containerAnswer) bool {
	if len(ans.arms) == 0 {
		return true
	}
	for _, arm := range ans.arms {
		lit, isList := arm.(*ListLit)
		if !isList {
			return true // a name, a comprehension, another call: the object is behind a frame, so ask it
		}
		// The same question the build site asked when it chose `heapListFromTagged` over `heapListFrom`
		// (codegen's list-literal road): a literal whose slots do not fit an untagged i32 — an interned
		// index, a float box, a nested container, a verdict, a pair — carries per-slot tags, and the mixed
		// printer is the one that reads them. Asking only `literalNeedsTags` printed `[1, "a"]` as
		// `[1, 0]`, because a text's index fits an i32 and needs a tag anyway (roadmap Gap R.67, ADR 0232).
		if g.literalNeedsHeap(lit) || g.literalNeedsTags(lit) || g.literalHasPairElement(lit) {
			return true
		}
	}
	return false
}

// containerAnswerDictCarriesTags asks the dict the question `heapDictFrom` asks itself before it chooses the
// tagged put: `taggableMixedDict` (a key or a value of a kind the dict's other entries do not share) or an
// entry the pair road bound. An arm that is not a literal — `return d`, a dict the body filled slot by slot
// — answers **no**, because those slots were tagged by the store road or not at all, and this table does not
// get to claim a tag the writer did not write.
func (g *irGen) containerAnswerDictCarriesTags(ans containerAnswer) bool {
	if len(ans.arms) == 0 {
		return false
	}
	for _, arm := range ans.arms {
		dl, isDict := arm.(*DictLit)
		if !isDict {
			continue
		}
		if g.taggableMixedDict(dl) || g.literalTakesPairSlots(dl) {
			return true
		}
	}
	return false
}

// containerAnswerSetCarriesTags is the set half of the same question, asked of the predicate `heapSetFrom`
// uses: `taggableMixedSet`, or a member the pair road bound.
func (g *irGen) containerAnswerSetCarriesTags(ans containerAnswer) bool {
	if len(ans.arms) == 0 {
		return false
	}
	for _, arm := range ans.arms {
		sl, isSet := arm.(*SetLit)
		if !isSet {
			continue
		}
		if g.taggableMixedSet(sl) || g.literalTakesPairSlots(sl) {
			return true
		}
	}
	return false
}

// answerPrinter names the printer for the answer the door above just lowered, chosen by the notebooks that
// door recorded — the same three choices the print road makes for a container variable, so the answer of a
// call and the variable holding that answer cannot print differently (roadmap Gap R.67, ADR 0333).
func (g *irGen) answerPrinter(name string, kind containerAnswerKind) string {
	switch kind {
	case setAnswer:
		return "rt_set_print"
	case dictAnswer:
		return "rt_dict_print"
	}
	if g.mixedLists[name] {
		return "rt_print_list_mixed"
	}
	if g.listElemStr[name] {
		return "rt_print_list_str"
	}
	return "rt_print_list"
}

// containerAnswerCallIsCarriable is the call-site half of the claim. The callee's arms say the answer is a
// container; the arguments say whether this particular call can hand the callee what its slots need. A
// parameter is a hole in the callee's body through which the caller's value becomes a slot of the answer,
// and a float reaches the container through that hole as a (payload, tag) pair — the pair door's object,
// whose refusal this table must not speak over. `f(1)` answering [n, n + 1] is this table's program;
// `pack(2.5)` answering [v, 1] is the pair road's, and the two are distinguished by the word the caller
// wrote, not by the body (roadmap Gap R.67; ADR 0273's rule for the pair door, read the other way).
func (g *irGen) containerAnswerCallIsCarriable(e Expr) bool {
	c, isCall := e.(*Call)
	if !isCall || g == nil {
		return true
	}
	for _, a := range c.Args {
		if kw, isKw := a.(*KeywordArg); isKw {
			a = kw.Value
		}
		if !g.containerAnswerArgIsCarriable(a) {
			return false
		}
	}
	return true
}

// containerAnswerArgIsCarriable asks the same question of one argument: is this a value the container
// roads can hold in an i32 slot — a number that is not a float, a text, a verdict, None, a container of
// those, or a call whose own answer one of the tables already describes?
func (g *irGen) containerAnswerArgIsCarriable(e Expr) bool {
	if e == nil {
		return true
	}
	switch v := e.(type) {
	case *IntLit, *StrLit, *BoolLit, *NoneLit, *ListLit, *SetLit, *DictLit, *Tuple:
		return true
	case *FloatLit:
		return false // the pair door's word, and the reason this predicate exists
	case *Name:
		return !(g.isFloat(e) || g.numericPairVar(v.Value) || (g.taggedVars != nil && g.taggedVars[v.Value]))
	case *BinOp, *UnOp, *Index, *CondExpr:
		return !g.isFloat(e)
	case *Call:
		nm, isName := v.Fn.(*Name)
		if !isName {
			return false
		}
		if g.floatFuncs[nm.Value] || g.pairRetDone[nm.Value] {
			return false
		}
		if g.strFuncs[nm.Value] {
			return true // a text answer is one interned index, which a slot holds
		}
		_, ok := g.containerAnswerOf(nm.Value)
		return ok && g.containerAnswerCallIsCarriable(e)
	}
	return false
}

// containerAnswerOf asks the scan's question of one function name, and answers nothing for a function the
// program is emitting under a convention that already owns its answer word: a float-returning body's
// `ret` is a `double`, a string-returning one's is an `@str_tab` index, a generator's is an iterator.
// Those keep the road they have (roadmap L11.2's `strFuncs`, L11.6's `floatFuncs`).
func (g *irGen) containerAnswerOf(fnName string) (containerAnswer, bool) {
	if g == nil || g.containerRet == nil {
		return containerAnswer{kind: notAContainerAnswer}, false
	}
	a, ok := g.containerRet[fnName]
	if !ok || a.kind == notAContainerAnswer {
		return containerAnswer{kind: notAContainerAnswer}, false
	}
	if g.floatFuncs[fnName] || g.strFuncs[fnName] || g.genFuncs[fnName] || g.decorated[fnName] {
		return containerAnswer{kind: notAContainerAnswer}, false
	}
	return a, true
}

// containerAnswerIsAsked reports that the ordinary single-word road may lower this call right now, because
// the door above has already handed the answer to a road that can read a container. The roads that ask are
// the print door, `str`/`repr`, an f-string field, a condition, a loop's iterable, a binding, and the
// forwarding `return build()`; every other position keeps the refusal below.
func (g *irGen) containerAnswerIsAsked(fnName string) bool {
	return g != nil && g.containerAsked[fnName]
}

// containerAnswerIsUndescribed asks the second set: this function's `return` provably hands back a
// container, and the slot table declined to describe what its slots hold. The number roads must refuse
// such a call rather than print the allocator's handle; the positions that can hold a handle may take it
// with a permission (roadmap Gap R.261, ADR 0344).
func (g *irGen) containerAnswerIsUndescribed(fnName string) bool {
	if g == nil || g.containerRetOpaque == nil {
		return false
	}
	_, ok := g.containerRetOpaque[fnName]
	return ok
}

// containerAnswerReadIsRefused answers the question a position asks after the door above declined: is the
// expression it just watched decline a PROVEN container answer that this position's own `read` cannot be
// given? If so the position may not continue on the road it was about to take, because every one of those
// roads lowers the call to the one word the allocator handed back and reads it as a number — the `0` on the
// screen at exit 0 that this row exists to end (roadmap Gap R.261, ADR 0344).
func (g *irGen) containerAnswerReadIsRefused(e Expr, read containerAnswerRead) (string, bool) {
	c, isCall := e.(*Call)
	if !isCall || g == nil {
		return "", false
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		return "", false
	}
	if _, ok := g.containerAnswerOf(nm.Value); ok {
		return "", false // a described answer: the door took it, or the carriable question declined it
	}
	if g.containerRetOpaque == nil {
		return "", false
	}
	und, ok := g.containerRetOpaque[nm.Value]
	if !ok || und.kind == notAContainerAnswer || und.takesRead(read) {
		return "", false
	}
	return nm.Value, true
}

// containerAnswerUndescribedRefusal is the sentence for a call whose answer is a container this backend
// will not describe. It says which half is missing — the slots, not the object — because the reader's
// next move is different: making the appended value something the table can follow is a program change,
// and so is asking for the object through a name, which the binding road already answers.
func containerAnswerUndescribedRefusal(fnName string) error {
	return fmt.Errorf("codegen: %q hands back a container whose slots this backend will not describe: the object is built, and its `return` says list, dict or set, but a slot is filled by a value the answer table cannot follow (a call, a comprehension or a format), and reading the heap handle as a number would print what the allocator handed out where the reference prints the contents — bind it to a name and print that, or fill the slots with values the table can read (roadmap Gap R.261, Gap R.67, ADRs 0333 and 0344)", callableSurfaceName(fnName))
}

// containerAnswerPermit is one lowering's hold on the permission above — the same permission
// `containerAnswerOperand` takes while it emits the call, given to a position that can keep the handle
// itself: a binding's slot, a forwarding `return`. It is taken by name and released by hand rather than
// deferred, because the roads that need it are long `case` bodies whose function ends much later.
type containerAnswerPermit struct {
	g  *irGen
	fn string
}

// permitContainerAnswer marks the permission for `e` when `e` is a call whose answer this table says is a
// container, and returns the handle that releases it. A nil return means "nothing to permit" — the call is
// not one of these, and the ordinary road keeps the behaviour it has always had.
func (g *irGen) permitContainerAnswer(e Expr) *containerAnswerPermit {
	return g.permitContainerAnswerRead(e, containerReadWhole)
}

// permitContainerAnswerRead is the permission with the position's question in hand, beside the door
// above's own copy of the rule (roadmap Gap R.261, ADR 0344).
func (g *irGen) permitContainerAnswerRead(e Expr, read containerAnswerRead) *containerAnswerPermit {
	c, isCall := e.(*Call)
	if !isCall || g == nil {
		return nil
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		return nil
	}
	if _, ok := g.containerAnswerOf(nm.Value); !ok || !g.containerAnswerCallIsCarriable(c) {
		und, isUndescribed := containerAnswer{}, false
		if g.containerRetOpaque != nil {
			und, isUndescribed = g.containerRetOpaque[nm.Value]
		}
		if !isUndescribed || und.kind == notAContainerAnswer || !und.takesRead(read) {
			return nil
		}
		// A container the table could not describe is still a container, and a position that can hold the
		// handle may still take it — `ys = f()` is such a position, and it prints `[5]` today by asking the
		// object itself what it is (roadmap Gap R.261, ADR 0344).
		g.containerAsked[nm.Value] = true
		return &containerAnswerPermit{g: g, fn: nm.Value}
	}
	g.containerAsked[nm.Value] = true
	return &containerAnswerPermit{g: g, fn: nm.Value}
}

// Done releases the permission. Every road that takes one releases it on the same page it takes it, so a
// nested call inside an argument keeps its own refusal (ADR 0273's rule for the pair road's permission).
func (p *containerAnswerPermit) Done() {
	if p != nil && p.g != nil {
		delete(p.g.containerAsked, p.fn)
	}
}

// containerAnswerRefusal is what a position that cannot read a container says, and it is part of the row:
// the ordinary road would take the handle for the number it is not, which is how `print(f())` reached `0`
// and `[f(), 9]` reached `[1, 9]`. Naming the door that does answer, and the row that owns the rest of
// them, is ADR 0249's rule — a refusal is only honest while it says what is missing.
func containerAnswerRefusal(fnName string) error {
	return fmt.Errorf("codegen: %q hands back a container, whose word is a heap object and whose kind only this table knows: print(...), str(...), a condition, a loop's iterable and a binding ask the door that reads a container answer, and this position keeps one word for the value and has no such road (roadmap Gap R.67, ADR 0333)", callableSurfaceName(fnName))
}
