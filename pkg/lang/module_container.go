// The module's container state, and the one road a compiled body takes to read it.
//
// A module-level name is a binding the program can read from anywhere, and ADR 0227 gave the compiled
// backend one honest mechanism for that: a name the module rebinds lives in a module GLOBAL that `@main`
// writes and any body loads, because a frame's allocas die with the frame and module state does not.
// ADR 0227 built the mechanism for the `i32` word; ADR 0376 (this file's sibling, `module_float.go`)
// built it for the `double`. A container was left out on purpose, twice: a container's word is a heap
// handle, which is only meaningful while the collector is also told about it, and every door that reads
// one asks a name-keyed notebook — which kind, do the slots describe themselves, what do the elements
// hold — that the binding road writes and no read can invent. A body that named a module-level list was
// therefore answered with a refusal, at exit 1, for programs CPython runs:
//
//	xs = [1, 2, 3]
//	def n():
//	    return len(xs)
//	print(n())            # CPython 3; this toolchain, before ADR 0407: exit 1, Gap R.35's sentence
//
// That refusal was the right shape for a construct the backend could not lower and the wrong shape for
// this one, because the program already says what the object is: a literal, bound once, at module level.
// The door below hands such a name to the body that reads it as a binding — not a load of a foreign
// handle with a fresh opinion beside it, but the same `stmtAssign` road the program's own
// `xs = [1, 2, 3]` goes through, run in the reading frame with a copy of the module's literal. Slot,
// root registration, kind claim, element notebook, mixed bit and fold record are then written by the
// only writer they have ever had, and the twenty-odd doors that read those records by name — `print`
// loading `%_xs`, `len` asking `listVars`, the subscript asking `containerLits`, the loop asking
// `listElemStr` — ask a frame that owns what they are told about.
//
// The gate is the whole safety of the feature, because a copy is honest only where nothing writes through
// the object it copies:
//
//   - a name gets this road only when its ONE module binding is a plain assignment of a container LITERAL
//     whose every slot is itself a literal — `xs = [1, 2]`, `d = {"a": 1}`, `s = {1, 2}`, `(1, 2)`,
//     nested alike. `xs = build()` or `xs = [f(1)]` answers a container only when the program runs, and a
//     door that promises a value it cannot see is how a fabricated number reaches the screen; the
//     answer table's own slot question (`containerAnswerSlotReader`, ADR 0232's rule) is asked as well,
//     so this file never holds a second implementation of it;
//   - a name the module binds through a road this door does not write — a `for` variable, a `with … as`
//     name, a `match` capture, an unpacking target, an import, an augmented assignment, a second binding
//     of the same spelling — is refused for the whole program. A second binding is a rebinding, and a
//     copy taken per call would answer whichever literal the emitter copied rather than whichever object
//     the module holds when the call runs; ADR 0351 (roadmap Gap R.49) is the measurement that says what
//     one un-followed binder costs, and this row's own measurement (roadmap Gap R.398) says what the
//     mutation half costs. Until then it stays refused, with the sentence the whole family has used;
//   - the container must be READ-only for the whole program, under every name the literal reached:
//     `xs.append(1)`, `xs[0] = 1`, `xs += [1]`, `for row in table: row.append(1)`, and `ys = xs` /
//     `ys = xs[0]` followed by any of those, each drop the name. A write through the object would live
//     in whichever frame wrote it and be invisible to every other reader — which is a wrong answer at the
//     exit code of success, the class this house puts above any refusal;
//   - the name may not be asked an IDENTITY question either: `xs is ys`, `f() is xs` and a dict keyed by
//     one of these names all compare objects rather than values, and a copy is the right value in the
//     wrong body. Those programs keep the refusal.
//
// What the name may NOT do once it has this road is be a local anywhere it is read. A body that assigns
// the name binds it for its whole body (ADR 0220), and the prologue asks that first: `def f(): xs = [];
// print(xs)` reads its own list, and the module's literal never enters that frame.
package lang

import (
	"sort"
	"strings"
)

// moduleContainerNames chooses the module-level container names that get a `global i32` handle: every
// name some function, method or closure body reads WITHOUT binding it, that is not a folded module
// constant nor owned by either of the other two module tables, whose every module binding is a plain
// assignment of a describable container literal of one kind, and which nothing in the program writes
// through. The answer it records is the same record `containerAnswerScan` keeps for a `def` that hands
// back a container — the arms are the module's literals rather than the body's `return`s — because the
// door that fills a body's notebooks for such a name (`recordContainerAnswer`) is the door this reuses.
func moduleContainerNames(prog *Program, consts map[string]Expr, all map[string]bool, ownedInt, ownedFloat map[string]bool, containerRet map[string]containerAnswer, defines map[string]bool) map[string]containerAnswer {
	slots := map[string]containerAnswer{}
	if prog == nil {
		return slots
	}
	reads := moduleBodyReads(prog, all)
	if len(reads) == 0 {
		return slots
	}

	// What does every module binding of the name answer? A name starts eligible and any binding that is
	// not a describable container literal — or that is not a plain assignment this door writes — takes
	// that away for the whole program. One answer per name, so two bindings that disagree about the kind
	// are two objects under one spelling, which is ADR 0341's disease and not this door's prescription.
	kinds := map[string]containerAnswerKind{}
	arms := map[string][]Expr{}
	seen := map[string]bool{}
	drop := func(nms ...string) {
		for _, nm := range nms {
			if nm == "" {
				continue
			}
			seen[nm] = true
			kinds[nm] = notAContainerAnswer
		}
	}
	keep := func(nm string, e Expr) {
		k := containerLiteralKind(e)
		if !seen[nm] {
			seen[nm] = true
			kinds[nm] = k
			if k != notAContainerAnswer {
				arms[nm] = []Expr{e}
			}
			return
		}
		if k == notAContainerAnswer {
			kinds[nm] = notAContainerAnswer
			return
		}
		if kinds[nm] == notAContainerAnswer {
			kinds[nm] = k
			arms[nm] = append(arms[nm], e)
			return
		}
		if kinds[nm] != k {
			kinds[nm] = notAContainerAnswer // one spelling, two objects
			return
		}
		arms[nm] = append(arms[nm], e)
	}

	var walk func(sts []Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch n := st.(type) {
			case *FuncDef:
				drop(n.Name)
				continue // a body's bindings are local to it (ADR 0220)
			case *ClassDef:
				drop(n.Name)
				continue
			case *AssignStmt:
				nm, ok := n.Target.(*Name)
				if !ok {
					markTargetNames(n.Target, drop) // `a, b = …` binds names this door does not write
					continue
				}
				keep(nm.Value, n.Value)
			case *AugAssignStmt:
				nm, ok := n.Target.(*Name)
				if !ok {
					drop(targetName(n.Target)...)
					continue
				}
				drop(nm.Value) // `xs += [1]` writes the module's ONE object
			case *ForStmt:
				drop(loopVarNames(n.Var)...)
				walk(n.Body)
				walk(n.Else)
			case *WithStmt:
				if n.As != nil {
					drop(n.As.Value)
				}
				walk(n.Body)
			case *MatchStmt:
				for _, c := range n.Cases {
					for nm := range matchCaptureNames(c.Pattern) {
						drop(nm)
					}
					for _, or := range c.Or {
						for nm := range matchCaptureNames(or) {
							drop(nm)
						}
					}
					walk(c.Body)
				}
			case *IfStmt:
				walk(n.Then)
				for _, e := range n.Elifs {
					walk(e.Then)
					walk(e.Else)
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
				walk(n.Else)
			case *TryStmt:
				walk(n.Body)
				for _, a := range n.Excepts {
					if a.As != "" {
						drop(a.As)
					}
					walk(a.Body)
				}
				walk(n.Finally)
			case *ImportStmt:
				drop(importBoundNames(n)...)
			case *TypeAliasStmt:
				drop(n.Name)
			}
		}
	}
	walk(prog.Stmts)

	// The module is a frame too, and the answer table's slot question is asked of a frame: what each of
	// its names was bound to, what the functions' answers were, and what the program took for itself. The
	// question is asked OF THE ANSWER TABLE'S OWN READER (`containerAnswerSlotReader`, ADR 0232's "one
	// question asked once") rather than by a second implementation of it — and not by handing the door a
	// synthetic `*FuncDef`, which `bodyBindings` would cache by a pointer that outlives this compile
	// (ADR 0343's rule about state two compiles cannot share).
	moduleBinds := map[string][]Expr{}
	scanRebinds(prog.Stmts, moduleBinds)
	reader := containerAnswerSlotReader{binds: moduleBinds, out: containerRet, defines: defines}

	candidates := map[string]bool{}
	for nm := range reads {
		if _, isConst := consts[nm]; isConst {
			continue // a folded value needs no slot (ADR 0227)
		}
		if ownedInt[nm] || ownedFloat[nm] {
			continue // the `i32` table and the float table own their names; two globals is one symbol twice
		}
		if !seen[nm] || kinds[nm] == notAContainerAnswer {
			continue // never bound here, or bound by a road this door does not write
		}
		if len(arms[nm]) != 1 || arms[nm][0] == nil {
			continue // two bindings are two objects, and a copy answers one of them whatever the call reads
		}
		if !literalContainerIsSelfDescribing(arms[nm][0]) {
			// Every slot has to be a literal the road can write in ANY frame. A name or a call inside the
			// binding reads — or allocates — where the program wrote it, not where the copy is built, and a
			// body's local of the same spelling would answer for the module's name (ADR 0220 read sideways).
			continue
		}
		described := true
		for _, arm := range arms[nm] {
			if !reader.slot(arm, 0) {
				described = false
				break
			}
		}
		if !described {
			continue // a slot this table cannot describe is another door's answer, not this door's claim
		}
		candidates[nm] = true
	}
	for nm := range candidates {
		delete(slots, nm)
	}
	// The read-only question is asked of the whole program, aliases included: the object is one, so a
	// write through any name the handle reached is a write through this one. Alongside the candidates goes
	// the one thing the escape question needs and no other door can answer: the LITERAL itself, which is
	// what decides whether `return xs[0]` reads a NUMBER (and so travels) or walks the object out of the
	// frame. It is asked here, where the binding is in hand, rather than by a second reading of the AST.
	lits := map[string]Expr{}
	for nm := range candidates {
		if len(arms[nm]) == 1 {
			lits[nm] = arms[nm][0]
		}
	}
	alive := moduleContainersAreReadOnly(prog, candidates, lits)
	for nm := range candidates {
		if !alive[nm] {
			continue
		}
		slots[nm] = containerAnswer{kind: kinds[nm], arms: arms[nm]}
	}
	return slots
}

// moduleTablesFor is the wiring the emitter asks: the three module tables — the `i32` slots, the `double`
// slots and the read-only literal containers — are built from the same three reads of the program, in the
// order that order matters in (each later door is handed the names the earlier one already owns, so one
// spelling never gets two globals). It is ONE function rather than a sequence in the generator and a copy
// of it in a test, because two wirings are two answers to one question (ADR 0232) and the question this
// one answers decides programs.
func moduleTablesFor(prog *Program, consts map[string]Expr, all map[string]bool,
	containerRet map[string]containerAnswer) (map[string]string, map[string]string, map[string]containerAnswer) {
	floatSlots := moduleFloatSlotNames(prog, consts, all)
	floatOwned := map[string]bool{}
	for nm := range floatSlots {
		floatOwned[nm] = true
	}
	intSlots := moduleSlotNames(prog, consts, all, floatOwned)
	intOwned := map[string]bool{}
	for nm := range intSlots {
		intOwned[nm] = true
	}
	containers := moduleContainerNames(prog, consts, all, intOwned, floatOwned, containerRet, moduleDefineNames(prog))
	return intSlots, floatSlots, containers
}

// moduleDefineNames answers, for the whole program, which spellings the program took for itself as a
// `def` or a `class` name. The answer table asks the same question (`containerAnswerScan` builds it with
// `collectContainerFuncDefs`) because a slot of a container that happens to be spelled like a function is
// the function, not a container — so the module door asks the SAME walk rather than growing a second copy
// of the rule (ADR 0232's one question asked once).
func moduleDefineNames(prog *Program) map[string]bool {
	defines := map[string]bool{}
	if prog == nil {
		return defines
	}
	collectContainerFuncDefs(prog.Stmts, map[string]*FuncDef{}, &[]string{}, defines)
	return defines
}

// literalContainerIsSelfDescribing asks the one question a per-frame copy has to ask of the module's
// literal: is every slot spelled out of literals, with no name and no call inside it? A slot that reads a
// name reads it where the copy is BUILT — in the body that reads the container — and a body's local of
// that spelling would answer for the module's name (ADR 0220 read sideways). A slot that calls allocates
// and can hand back a container the program may write through, which is the object this door refused to
// promise. The answer table's slot question is asked beside this one and declines the same shapes; this
// is the stricter half, asked because a copy is built in another frame.
func literalContainerIsSelfDescribing(e Expr) bool {
	if lit, ok := emptyContainerLiteral(e); ok {
		e = lit
	}
	var slot func(Expr, int) bool
	slot = func(e Expr, depth int) bool {
		if depth > 4 {
			return false
		}
		if lit, ok := emptyContainerLiteral(e); ok {
			e = lit
		}
		switch n := e.(type) {
		case *IntLit, *StrLit, *BoolLit, *NoneLit, *FloatLit:
			return true
		case *UnOp:
			return n.Op == "-" && slot(n.X, depth+1)
		case *ListLit:
			for _, el := range n.Elems {
				if !slot(el, depth+1) {
					return false
				}
			}
			return true
		case *SetLit:
			for _, el := range n.Elems {
				if !slot(el, depth+1) {
					return false
				}
			}
			return true
		case *Tuple:
			for _, el := range n.Elems {
				if !slot(el, depth+1) {
					return false
				}
			}
			return true
		case *DictLit:
			for _, k := range n.Keys {
				if !slot(k, depth+1) {
					return false
				}
			}
			for _, v := range n.Vals {
				if !slot(v, depth+1) {
					return false
				}
			}
			return true
		}
		return false
	}
	return slot(e, 0)
}

// containerLiteralKind names what a module binding's value is, in the answer table's vocabulary, or
// declines. Only a literal spelling counts: the empty-container builtins answer the same object the
// empty literal does, and `emptyContainerLiteral` is the door that knows it.
func containerLiteralKind(e Expr) containerAnswerKind {
	if lit, ok := emptyContainerLiteral(e); ok {
		e = lit
	}
	switch n := e.(type) {
	case *ListLit:
		if n.IsTuple() {
			return tupleAnswer
		}
		return listAnswer
	case *SetLit:
		return setAnswer
	case *DictLit:
		return dictAnswer
	}
	return notAContainerAnswer
}

// containerLitElems lists every expression a container literal holds — its elements, and for a dict both
// the keys and the values, because a returned dict hands both halves of an entry to the caller. It is the
// one spelling of “what is inside this literal” the value question asks; a second walk over the same four
// node types is how two doors disagree about one program (ADR 0232).
func containerLitElems(e Expr) []Expr {
	switch n := exprUnwrap(e).(type) {
	case *ListLit:
		return n.Elems
	case *SetLit:
		return n.Elems
	case *Tuple:
		return n.Elems
	case *DictLit:
		out := make([]Expr, 0, len(n.Keys)+len(n.Vals))
		out = append(out, n.Keys...)
		out = append(out, n.Vals...)
		return out
	}
	return nil
}

// literalSlotAnswersInts walks the module's literal along a chain of subscripts and answers whether the
// slot at the end is an INTEGER literal — which is what decides whether `return xs[0]` hands back a number
// (a value, which travels as well as any other) or a piece of the module's own object (which does not,
// because a caller can write through it and this gate cannot follow a value out of a frame).
//
//	_xs = [1, 2, 3]      `return _xs[0]`  → true:  the answer is the number 1
//	table = [[1], [2]]   `return table[0]` → false: the answer IS the module's row
//	d = {"a": 1}         `return d["a"]`   → true:  the entry's value is a number
//	xs = ["b", "a"]      `return xs[1]`    → false: see below
//
// Integers and nothing else, because the road that carries a slot out of a body carries an integer
// honestly and no other kind: measured on the parent build beside this one, a body that returns a slot of
// its OWN list answers the reference's bytes for `7` and the wrong word for every other element kind —
// `["b","a"]` prints `1` (the text's `@str_tab` index), `[1.5, 2.5]` prints `2`, `[True, False]` prints `0`
// — which is the answer-kind family (roadmap Gap R.324, Gap R.219, Gap R.331), not this door's to open.
// A name whose slots hold those kinds therefore keeps the refusal it had, rather than trading an honest
// refusal at exit 1 for one of those numbers at exit 0 (**ADR 0166**'s ordering, restated by **ADR 0273**).
//
// Everything the walk cannot READ is false: a computed index (`rows[i]`), an out-of-range position (the
// reference raises `IndexError` there and this door will not guess which line the program meant), a dict
// key the literal does not hold, a set (which has no position to read), and a depth past four (the same
// bound `literalContainerIsSelfDescribing` keeps).
func literalSlotAnswersInts(lit Expr, chain []Expr) bool {
	if lit == nil {
		return false
	}
	if len(chain) > 4 {
		return false
	}
	if at, ok := emptyContainerLiteral(lit); ok {
		lit = at
	}
	if len(chain) == 0 {
		switch n := exprUnwrap(lit).(type) {
		case *IntLit:
			return true
		case *UnOp:
			return n.Op == "-" && literalSlotAnswersInts(n.X, nil)
		}
		return false // a text, a float, a verdict, a container: none of them travels this road yet
	}
	switch n := lit.(type) {
	case *ListLit:
		at, ok := indexLiteral(chain[0])
		if !ok {
			return false // a computed position: the slot is whatever the run time says, and this door does not
		}
		return stepInto(n.Elems, at, chain) // a list and a parenthesised tuple are the same node
	case *Tuple:
		at, ok := indexLiteral(chain[0])
		if !ok {
			return false
		}
		return stepInto(n.Elems, at, chain)
	case *DictLit:
		// `d[k]` answers an ENTRY's value; the keys are what a loop over the dict binds, and the loop rule
		// is the gate's own, asked of every loop in the program rather than of one spelling here.
		for i, k := range n.Keys {
			if i >= len(n.Vals) {
				return false
			}
			if literalsAgree(k, chain[0]) {
				return literalSlotAnswersInts(n.Vals[i], chain[1:])
			}
		}
		return false // no such entry in the literal: the reference misses here too, and this door declines
	}
	return false // a set, a text, a number: nothing to walk
}

// stepInto asks the next slot of a positional container — a list or a tuple — the same question, given a
// position the compiler read out of the source. An index outside the literal is not this door's to
// answer: the reference raises `IndexError`, and the honest shape for a program the door cannot read is
// the refusal it already gives.
func stepInto(elems []Expr, at int64, chain []Expr) bool {
	if at < 0 {
		at += int64(len(elems)) // xs[-1] is the reference's last element, spelled from the other end
	}
	if at < 0 || at >= int64(len(elems)) {
		return false
	}
	return literalSlotAnswersInts(elems[at], chain[1:])
}

// indexLiteral reads an index expression the way this gate can: a spelled integer, optionally negated.
func indexLiteral(e Expr) (int64, bool) {
	switch n := exprUnwrap(e).(type) {
	case *IntLit:
		return n.Value, true
	case *UnOp:
		if n.Op != "-" {
			return 0, false
		}
		if v, ok := indexLiteral(n.X); ok {
			return -v, true
		}
	}
	return 0, false
}

// literalsAgree compares two spelled literals for the purposes of finding a dict entry: same kind, same
// payload. A `True` key and a `1` key are the same key to the reference and are NOT compared here — the
// entry this door finds is the one the source wrote, and anything subtler than that is the hashing door's
// question (roadmap Gap R.217), not the gate's.
func literalsAgree(a, b Expr) bool {
	switch x := exprUnwrap(a).(type) {
	case *IntLit:
		y, ok := exprUnwrap(b).(*IntLit)
		return ok && x.Value == y.Value
	case *StrLit:
		y, ok := exprUnwrap(b).(*StrLit)
		return ok && x.Value == y.Value
	case *BoolLit:
		y, ok := exprUnwrap(b).(*BoolLit)
		return ok && x.Value == y.Value
	}
	return false
}

// ———————————————————————————————————————————————————————————————— the read-only question

// readOnlyBuiltins are the callees that read their arguments and keep nothing: the renderers, the
// measurers, the folds, and the three that build a fresh container out of what they were handed. None of
// them can write through an argument, so passing the module's container to one is a read.
var readOnlyBuiltins = map[string]bool{
	"print": true, "str": true, "repr": true, "len": true, "type": true, "bool": true,
	"int": true, "float": true, "abs": true, "min": true, "max": true, "sum": true,
	"sorted": true, "list": true, "dict": true, "set": true, "tuple": true, "enumerate": true,
	"round": true, "divmod": true, "hash": true, "all": true, "any": true, "ord": true,
	"chr": true, "range": true, "reversed": true, "format": true, "id": true, "ascii": true,
	"vars": true, "dir": true, "callable": true, "isinstance": true, "issubclass": true,
}

// readOnlyMethods are the container and text methods whose whole answer is a value: they walk the object
// and hand something back, never writing a slot. Anything NOT in this list is treated as a write — a
// method this file has not read the source of is exactly the case where a claim about an unmutated
// object would be a guess.
var readOnlyMethods = map[string]bool{
	"keys": true, "values": true, "items": true, "get": true, "count": true, "index": true,
	"copy": true, "upper": true, "lower": true, "strip": true, "lstrip": true, "rstrip": true,
	"split": true, "splitlines": true, "join": true, "replace": true, "startswith": true,
	"endswith": true, "find": true, "rfind": true, "format": true, "title": true, "capitalize": true,
	"swapcase": true, "isdigit": true, "isalpha": true, "isalnum": true, "isspace": true,
	"encode": true, "center": true, "ljust": true, "rjust": true, "partition": true, "removeprefix": true,
	"removesuffix": true, "titlecase": true,
}

// moduleContainersAreReadOnly answers, for each candidate, whether nothing in the program writes
// through the name or through any name the handle reached. It is the gate that keeps the notebooks
// honest: `xs.append(1)` in a body would write the module's object while the element claims stayed where
// the write was emitted, and `print(xs)` in the module would then render a text slot as a number.
//
// `lits` carries the escape question's one piece of evidence: for each candidate, the literal the module
// bound it to, which is what decides whether `return xs[0]` answers a number or hands the object (or a
// slot of it) to a frame this door no longer owns.
func moduleContainersAreReadOnly(prog *Program, candidates map[string]bool, lits map[string]Expr) map[string]bool {
	ok := map[string]bool{}
	for nm := range candidates {
		ok[nm] = true
	}
	if prog == nil || len(candidates) == 0 {
		return ok
	}
	// The write-through question is asked with the name's aliases in hand. The module's own binding
	// statements are not writes through the object — they replace it, and the gate above already decided
	// each of them is a literal this door writes — but a name is followed for the WHOLE program rather
	// than until its next rebinding: `xs = [1]` then `xs.append(2)` writes an object a body may still be
	// reading, and the claims this door writes describe the literals, not the write.
	bad := func(e Expr, aliases map[string]bool) bool { return exprTouchesNames(e, aliases) }

	// A body's `return` asks the escape question of the LITERAL this name was bound to; the walk below runs
	// once per candidate, so the binding in hand is the one the question is about.
	var curLit Expr

	// checkExpr walks an expression for the three shapes that write, and for the two that hand the
	// handle somewhere this door cannot follow.
	var checkExpr func(e Expr, aliases map[string]bool)
	checkExpr = func(e Expr, aliases map[string]bool) {
		switch n := exprUnwrap(e).(type) {
		case nil:
			return
		case *Call:
			// A method call whose receiver holds the handle writes it, unless the method is one this
			// file has read and knows only to look.
			if attr, isAttr := n.Fn.(*Attr); isAttr {
				meth := ""
				if attr.Name != nil {
					meth = attr.Name.Value
				}
				if bad(attr.Obj, aliases) && !readOnlyMethods[meth] {
					for nm := range aliases {
						ok[nm] = false
					}
				}
				for _, a := range n.Args {
					checkExpr(a, aliases)
				}
				return
			}
			if _, isLambda := n.Fn.(*Lambda); isLambda {
				// An applied `lambda` is a function the program wrote, and its body may write whatever it
				// is handed: the same question as a call to a named user function.
				for _, a := range n.Args {
					if bad(a, aliases) {
						for nm := range aliases {
							ok[nm] = false
						}
					}
				}
				checkExpr(n.Fn, aliases)
				return
			}
			fn, isName := n.Fn.(*Name)
			if isName && !readOnlyBuiltins[fn.Value] {
				// A user function holds whatever the program hands it, and this door cannot see what it
				// does with it. One such call with the handle in the argument list is a write, somewhere.
				for _, a := range n.Args {
					if bad(a, aliases) {
						for nm := range aliases {
							ok[nm] = false
						}
					}
				}
			}
			for _, a := range n.Args {
				checkExpr(a, aliases)
			}
		case *Index:
			checkExpr(n.Obj, aliases)
			checkExpr(n.Idx, aliases)
		case *Slice:
			checkExpr(n.Obj, aliases)
			checkExpr(n.Low, aliases)
			checkExpr(n.High, aliases)
			checkExpr(n.Step, aliases)
		case *Attr:
			checkExpr(n.Obj, aliases)
		case *BinOp:
			checkExpr(n.L, aliases)
			checkExpr(n.R, aliases)
			// `xs is ys`, `xs is not ys` — the single-operator spelling of the identity question, which the
			// parser cuts as a BinOp and only a chained comparison spells as a ChainCompare. A copy is the
			// right VALUE and the wrong IDENTITY: `ys = xs` then `ys is xs` in a body answers False through
			// two copies where the reference answers True, at the exit code of success. A name asked which
			// object it is keeps the refusal.
			if n.Op == "is" || n.Op == "is not" {
				if bad(n.L, aliases) || bad(n.R, aliases) {
					for nm := range aliases {
						ok[nm] = false
					}
				}
			}
		case *UnOp:
			checkExpr(n.X, aliases)
		case *CondExpr:
			checkExpr(n.Cond, aliases)
			checkExpr(n.If, aliases)
			checkExpr(n.Else, aliases)
		case *ListLit:
			for _, el := range n.Elems {
				checkExpr(el, aliases)
			}
		case *SetLit:
			for _, el := range n.Elems {
				checkExpr(el, aliases)
			}
		case *DictLit:
			for _, k := range n.Keys {
				checkExpr(k, aliases)
			}
			for _, v := range n.Vals {
				checkExpr(v, aliases)
			}
		case *Tuple:
			for _, el := range n.Elems {
				checkExpr(el, aliases)
			}
		case *FString:
			// An interpolation is a carrier, not a literal: `f"{xs.append(1)}"` writes through the handle
			// and is invisible to a walk that stops at the text. ADR 0403 is the measurement of what a
			// reader that does not descend into `FStringPart`, `MatchCase` and `ExceptClause` costs — it
			// compiled a program with those three regions cut out of it.
			for _, p := range n.Parts {
				checkExpr(p.Expr, aliases)
			}
		case *Comp:
			for _, el := range n.Elems {
				checkExpr(el, aliases)
			}
			for _, k := range n.Keys {
				checkExpr(k, aliases)
			}
			for _, v := range n.Vals {
				checkExpr(v, aliases)
			}
			for _, cl := range n.compClauses() {
				checkExpr(cl.Iter, aliases)
				checkExpr(cl.Cond, aliases)
			}
		case *ChainCompare:
			// `xs is ys` and `xs is not xs` compare OBJECTS. The road below answers a value, and an answer
			// that happens to be right for the wrong reason is the state ADR 0232 forbids — so a name asked
			// an identity question is not this door's name.
			for i, op := range n.Ops {
				if op == "is" || op == "is not" {
					if bad(n.Operands[i], aliases) || (i+1 < len(n.Operands) && bad(n.Operands[i+1], aliases)) {
						for nm := range aliases {
							ok[nm] = false
						}
					}
				}
			}
			for _, o := range n.Operands {
				checkExpr(o, aliases)
			}
		case *Lambda:
			checkExpr(n.Body, aliases)
		}
	}

	// A statement writes through an alias when it stores a slot (`xs[0] = 1`, `d["k"] = 1`,
	// `xs[k].attr = 1`) or rebinds one with an operator (`xs += [1]`), and it takes the name over when a
	// binding form gives it a value that is not the module's object any more.
	var stmts func(sts []Stmt, aliases map[string]bool)
	stmts = func(sts []Stmt, aliases map[string]bool) {
		for _, st := range sts {
			switch n := st.(type) {
			case nil:
				continue
			case *AssignStmt:
				if ix, isIx := n.Target.(*Index); isIx {
					if bad(ix.Obj, aliases) || bad(ix.Idx, aliases) {
						for nm := range aliases {
							ok[nm] = false
						}
					}
					checkExpr(ix.Obj, aliases)
					checkExpr(ix.Idx, aliases)
					checkExpr(n.Value, aliases)
					continue
				}
				if attr, isAttr := n.Target.(*Attr); isAttr {
					if bad(attr.Obj, aliases) {
						for nm := range aliases {
							ok[nm] = false
						}
					}
					checkExpr(n.Value, aliases)
					continue
				}
				nm, isName := n.Target.(*Name)
				if !isName {
					// `a, b = xs` reads the handle and binds two names to pieces of it — pieces of an
					// object the program may write through, which this door does not promise.
					if bad(n.Target, aliases) {
						for nm := range aliases {
							ok[nm] = false
						}
					}
					checkExpr(n.Value, aliases)
					continue
				}
				if lit, isLit := n.Value.(*Name); isLit && aliases[lit.Value] {
					aliases[nm.Value] = true // `ys = xs` — one object, two spellings
				}
				checkExpr(n.Value, aliases)
			case *AugAssignStmt:
				if bad(n.Target, aliases) {
					for nm := range aliases {
						ok[nm] = false
					}
				}
				checkExpr(n.Value, aliases)
			case *ExprStmt:
				checkExpr(n.Expr, aliases)
			case *ReturnStmt:
				// A body that hands the object out is handing out the handle; the caller may write
				// through it, and this door cannot follow a value out of a function. What the body hands
				// back is asked, not whether the body MENTIONED the name: `return len(xs)`, `return sum(xs)`
				// and `return len(xs) > 1` answer a number and a verdict built out of a read, and refusing
				// them for the shape of `return xs` would leave the most common body that reads a module
				// container still refused. `return xs`, `return xs + [1]` and `return pack(xs)` do carry the
				// object out of the frame, and keep the refusal. A slot of it — `return xs[0]` — is asked of
				// the literal the module bound (`intSlots`): over `[1, 2, 3]` the answer is a number and
				// travels, over `[[1], [2]]` the answer IS the module's row and the name is dropped.
				if bad(n.Expr, aliases) && !moduleContainerAnswerIsValue(n.Expr, aliases, curLit) {
					for nm := range aliases {
						ok[nm] = false
					}
				}
			case *IfStmt:
				checkExpr(n.Cond, aliases)
				stmts(n.Then, copyAliases(aliases))
				for _, e := range n.Elifs {
					checkExpr(e.Cond, aliases)
					stmts(e.Then, copyAliases(aliases))
					stmts(e.Else, copyAliases(aliases))
				}
				stmts(n.Else, copyAliases(aliases))
			case *WhileStmt:
				checkExpr(n.Cond, aliases)
				stmts(n.Body, copyAliases(aliases))
				stmts(n.Else, copyAliases(aliases))
			case *ForStmt:
				checkExpr(n.Iter, aliases)
				inner := copyAliases(aliases)
				// `for v in xs:` binds v to an ELEMENT of the object, not to the object — and an element of a
				// container of containers is itself writable (`for row in table: row.append(1)` writes the
				// module's rows), so the loop variable of a loop over the module's container joins the alias set.
				if bad(n.Iter, aliases) {
					for _, v := range loopVarNames(n.Var) {
						inner[v] = true
					}
				} else {
					for _, v := range loopVarNames(n.Var) {
						delete(inner, v)
					}
				}
				stmts(n.Body, inner)
				stmts(n.Else, copyAliases(aliases))
			case *TryStmt:
				stmts(n.Body, copyAliases(aliases))
				for _, a := range n.Excepts {
					stmts(a.Body, copyAliases(aliases))
				}
				stmts(n.Finally, copyAliases(aliases))
			case *WithStmt:
				checkExpr(n.Expr, aliases)
				inner := copyAliases(aliases)
				if n.As != nil {
					delete(inner, n.As.Value)
				}
				stmts(n.Body, inner)
			case *MatchStmt:
				checkExpr(n.Subject, aliases)
				for _, c := range n.Cases {
					stmts(c.Body, copyAliases(aliases))
				}
			case *FuncDef:
				// A body is a scope: a name it binds is its own, and the module's object reaches it only
				// as a read of the name it did not take.
				inner := copyAliases(aliases)
				for _, p := range n.Params {
					delete(inner, p.Name)
				}
				binds := map[string]bool{}
				collectLocals(n.Body, binds)
				for nm := range binds {
					delete(inner, nm)
				}
				for _, p := range n.Params {
					if p.Default != nil {
						checkExpr(p.Default, aliases)
					}
				}
				stmts(n.Body, inner)
			case *ClassDef:
				inner := copyAliases(aliases)
				binds := map[string]bool{}
				collectLocals(n.Body, binds)
				for nm := range binds {
					delete(inner, nm)
				}
				stmts(n.Body, inner)
			case *RaiseStmt:
				checkExpr(n.Expr, aliases)
			}
		}
	}

	for nm := range candidates {
		curLit = lits[nm] // the escape question asks THIS name's binding, one candidate at a time
		stmts(prog.Stmts, map[string]bool{nm: true})
	}
	return ok
}

// copyAliases is an alias set copied for a nested scope or branch: a binding in one arm does not take
// the name over in another, and a body's locals are nobody else's.
func copyAliases(a map[string]bool) map[string]bool {
	out := make(map[string]bool, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

// scalarAnswerBuiltins are the callees whose answer is a value that is NEVER the object handed to them
// and never a slot of it: the renderers, the measurers, the verdicts and the number folds. The
// container-BUILDING builtins (`list`, `set`, `dict`, `tuple`, `sorted`, `reversed`, `enumerate`) and the
// PICKERS (`min`, `max`) are deliberately absent: `sorted(xs)` answers a container whose elements came
// out of `xs` and `max(xs)` may answer an element of it outright — a slot of the module's object walking
// out of the body into a frame this door no longer owns.
var scalarAnswerBuiltins = map[string]bool{
	"len": true, "str": true, "repr": true, "ascii": true, "type": true, "bool": true,
	"int": true, "float": true, "abs": true, "sum": true, "round": true, "divmod": true,
	"hash": true, "all": true, "any": true, "ord": true, "chr": true, "format": true,
	"print": true, "id": true, "callable": true, "isinstance": true, "issubclass": true,
	"vars": true, "dir": true,
}

// moduleContainerVerdictOps are the binary operators whose answer is a verdict and never one of their
// operands. `is` and `is not` are NOT here: they compare objects, which is the one question a copy
// cannot answer — the gate's ChainCompare row drops any name asked one, and this list must not hand it
// back through the single-operator spelling.
var moduleContainerVerdictOps = map[string]bool{
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
	"in": true, "not in": true,
}

// moduleContainerAnswerIsValue answers the one question a `return` asks before its name is dropped: can
// this expression answer anything but the object, or a piece of it? A read of the container through a
// door that CANNOT hand the object back — `len(xs)`, `str(xs)`, `sum(xs)`, `len(xs) > 1`,
// `f"{len(xs)}"`, `xs == [1, 2]` — is a value, and the body that returns one still leaves the module's
// object owned by nobody but the module. Anything the gate has not read the answer of is not a value:
// the name itself, a slot of it, a container literal holding it, a method this file has not read, a
// user function handed it — each of those can hand the handle (or a slot of it) to a frame that may
// write through it, and an untracked write is a wrong answer at the exit code of success.
func moduleContainerAnswerIsValue(e Expr, aliases map[string]bool, lit Expr) bool {
	if !exprTouchesNames(e, aliases) {
		return true // the expression never mentions the object: whatever it answers, it is not this object
	}
	switch n := exprUnwrap(e).(type) {
	case *Index:
		// `return xs[0]` is the escape question one level down, and the literal the module bound answers
		// it: a slot of `[1, 2, 3]` is a NUMBER, which travels as well as any other value, while a slot of
		// `[[1], [2]]` IS a piece of the module's object. Where the answer cannot tell the two apart the
		// refusal stands, because an untracked write through a returned container is a stale list printed
		// at the exit code of success — the one answer this gate exists to keep out.
		chain := []Expr{}
		var base Expr = n
		for {
			ix, isIndex := exprUnwrap(base).(*Index)
			if !isIndex {
				break
			}
			chain = append([]Expr{ix.Idx}, chain...)
			base = ix.Obj
		}
		at, isName := exprUnwrap(base).(*Name)
		if !isName || !aliases[at.Value] {
			return false // `return pack[0]`, `return rows[i][0]`: the chain does not bottom out at the handle
		}
		for _, ix := range chain {
			if exprTouchesNames(ix, aliases) {
				return false // `return d[xs]` asks the object which slot it is
			}
		}
		return literalSlotAnswersInts(lit, chain)
	case *Call:
		// A door that cannot hand the object back answers a value whatever it was handed — `str(pack(xs))`
		// renders whatever the user body returned, `sum(f(xs))` adds whatever the mapping produced — so the
		// arguments are not asked: only the CALLEE decides whether an answer can be the object.
		fn, isName := n.Fn.(*Name)
		return isName && scalarAnswerBuiltins[fn.Value]
	case *BinOp:
		if moduleContainerVerdictOps[n.Op] {
			// A comparison or a membership test answers a verdict, never an operand — and the identity
			// operators, which ARE the question about the object, are deliberately absent: the gate's own
			// `is` row drops any name asked one, and a value predicate that answered True for `xs is ys`
			// would be the gate talking itself into the copy.
			return true
		}
		return moduleContainerAnswerIsValue(n.L, aliases, lit) && moduleContainerAnswerIsValue(n.R, aliases, lit)
	case *UnOp:
		if n.Op == "not" {
			return true // `not xs` asks the object's own count and answers True or False
		}
		return moduleContainerAnswerIsValue(n.X, aliases, lit)
	case *CondExpr:
		return moduleContainerAnswerIsValue(n.Cond, aliases, lit) &&
			moduleContainerAnswerIsValue(n.If, aliases, lit) && moduleContainerAnswerIsValue(n.Else, aliases, lit)
	case *ChainCompare:
		// A comparison answers a verdict, never an operand — and the identity operators, which are the
		// comparison that WOULD ask about the object, are handled by the gate's own `is` row above.
		return true
	case *ListLit, *SetLit, *Tuple:
		// A container the body BUILDS is not the module's object: it is allocated in the frame that wrote
		// it, and the only way the module's handle can be in it is as an element. So the literal is a value
		// exactly when every slot is one — `[xs[0], d["a"]]` answers a fresh list of two numbers, while
		// `[nested[0]]` (a slot that IS a container) or `[xs]` are the object wearing a new literal, and
		// each keeps the refusal the name has always had.
		for _, el := range containerLitElems(n) {
			if !moduleContainerAnswerIsValue(el, aliases, lit) {
				return false
			}
		}
		return true
	case *DictLit:
		for _, el := range containerLitElems(n) {
			if !moduleContainerAnswerIsValue(el, aliases, lit) {
				return false
			}
		}
		return true
	case *FString:
		// An interpolation renders what it was handed and answers a NEW text: the object it read is
		// never the answer, whatever the field was.
		return true
	}
	return false
}

// exprTouchesNames asks whether any leaf of an expression is one of the names in hand. It is deliberately
// deeper than a syntactic match: `[xs, 1]`, `f(xs)` and `xs.a` all hold the handle, and the conservative
// side of this gate is a refusal, never a wrong number.
func exprTouchesNames(e Expr, names map[string]bool) bool {
	if len(names) == 0 {
		return false
	}
	switch n := exprUnwrap(e).(type) {
	case nil:
		return false
	case *Name:
		return names[n.Value]
	case *Index:
		return exprTouchesNames(n.Obj, names) || exprTouchesNames(n.Idx, names)
	case *Slice:
		return exprTouchesNames(n.Obj, names) || exprTouchesNames(n.Low, names) ||
			exprTouchesNames(n.High, names) || exprTouchesNames(n.Step, names)
	case *Attr:
		return exprTouchesNames(n.Obj, names)
	case *BinOp:
		return exprTouchesNames(n.L, names) || exprTouchesNames(n.R, names)
	case *UnOp:
		return exprTouchesNames(n.X, names)
	case *CondExpr:
		return exprTouchesNames(n.Cond, names) || exprTouchesNames(n.If, names) || exprTouchesNames(n.Else, names)
	case *Call:
		if exprTouchesNames(n.Fn, names) {
			return true
		}
		for _, a := range n.Args {
			if kw, isKw := a.(*KeywordArg); isKw {
				if exprTouchesNames(kw.Value, names) {
					return true
				}
				continue
			}
			if exprTouchesNames(a, names) {
				return true
			}
		}
		return false
	case *ListLit:
		for _, el := range n.Elems {
			if exprTouchesNames(el, names) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if exprTouchesNames(el, names) {
				return true
			}
		}
	case *Tuple:
		for _, el := range n.Elems {
			if exprTouchesNames(el, names) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if exprTouchesNames(k, names) {
				return true
			}
		}
		for _, v := range n.Vals {
			if exprTouchesNames(v, names) {
				return true
			}
		}
	case *FString:
		for _, p := range n.Parts {
			if exprTouchesNames(p.Expr, names) {
				return true
			}
		}
	case *Comp:
		for _, el := range n.Elems {
			if exprTouchesNames(el, names) {
				return true
			}
		}
		for _, k := range n.Keys {
			if exprTouchesNames(k, names) {
				return true
			}
		}
		for _, v := range n.Vals {
			if exprTouchesNames(v, names) {
				return true
			}
		}
		for _, cl := range n.compClauses() {
			if exprTouchesNames(cl.Iter, names) || exprTouchesNames(cl.Cond, names) {
				return true
			}
		}
	case *Lambda:
		return exprTouchesNames(n.Body, names)
	}
	return false
}

// exprUnwrap looks through the wrappers that carry an expression rather than mean it.
func exprUnwrap(e Expr) Expr {
	for {
		switch n := e.(type) {
		case *KeywordArg:
			e = n.Value
			continue
		}
		return e
	}
}

// ———————————————————————————————————————————————————————————————— the door on the emitter's side

// bindModuleContainerReads emits, at the top of a body that reads the module's containers, the binding
// that hands the frame what the module holds — and it emits it by way of the ONE road that knows how a
// container gets into a frame: `stmtAssign`, the same call `xs = [1, 2]` goes through when the program
// writes it.
//
// That choice is the whole design, and it is ADR 0406's lesson applied one door over. The read is not a
// load of a shared handle with a second opinion about what the object holds: the frame gets a slot, a
// root registration, a kind claim, an element notebook, a mixed bit and a fold record, and every one of
// them is written by the binding road that has always written them. Twenty-odd doors read those records
// by name — `print` loads `%_xs` directly, `len` asks `listVars`, the subscript asks `containerLits`, the
// loop asks `listElemStr` — and a read road that answered the handle alone left all of them asking about
// a name no frame owned, which the frame-slot audit caught as `%h1 = load i32, i32* %_xs` in a `define`
// that had never allocated `%_xs`: exit 2, the code ADR 0166 reserves for this compiler's bug.
//
// What the frame therefore holds is a COPY of the module's literal, built per call. That is only honest
// because the gate refused every name the program writes through (`moduleContainersAreReadOnly`): with no
// write anywhere, one object and a copy of it answer the same program. The identity question — `xs is xs`
// through a function that hands the container back — is refused by that same gate rather than answered by
// a copy that is not the same object, and the mutation half of the row stays open as roadmap Gap R.398.
func (g *irGen) bindModuleContainerReads(b *strings.Builder, fd *FuncDef) error {
	if fd == nil || len(g.moduleContainers) == 0 {
		return nil
	}
	binds := map[string]bool{}
	collectLocals(fd.Body, binds)
	for _, p := range fd.Params {
		if p != nil {
			binds[p.Name] = true
		}
	}
	reads := map[string]bool{}
	freeNamesOfStmts(fd.Body, binds, reads)
	if len(reads) == 0 {
		return nil
	}
	// A stable order: the prologue is IR, and two compiles of one program must emit one module.
	names := make([]string, 0, len(reads))
	for nm := range reads {
		if _, ok := g.moduleContainers[nm]; !ok {
			continue
		}
		names = append(names, nm)
	}
	if len(names) == 0 {
		return nil
	}
	sort.Strings(names)
	// The record is per frame: what THIS body was handed, and nobody else's answer to the same question
	// (ADR 0343's rule about a fact two compiles — here, two frames — cannot share out of one map).
	g.moduleContainerReads = map[string]bool{}
	for _, nm := range names {
		ans := g.moduleContainers[nm]
		if len(ans.arms) != 1 || ans.arms[0] == nil {
			continue // the gate keeps exactly one binding; anything else keeps the refusal it always had
		}
		if g.allocd[nm] {
			continue // the frame already owns a slot under this spelling: not this door's
		}
		// ASKED HERE, not only by the gate. A body that takes the name for itself is a body whose reads
		// mean its own binding (ADR 0220) — every door that reads the name asks THIS frame's slot tables —
		// and a second binding of a slot the frame already minted is a load of a handle nobody owns. The
		// IR audit (`frame_audit.go`) catches exactly that and stops the build with exit 2, the code
		// ADR 0166 reserves for this compiler's bug, so the question cannot live only in the gate.
		if moduleBodyClaimsName(fd, nm) {
			continue
		}
		// And a body that WRITES through the object is a program the gate said it had not seen; if the
		// gate and this door ever disagree, the honest answer is the refusal the whole family has used,
		// never a copy that prints the literal while the program wrote the object.
		if moduleBodyWritesThrough(fd, nm) {
			if err := g.moduleStateErr(nm); err != nil {
				return err
			}
			continue
		}
		lit := copyASTExpr(ans.arms[0])
		if lit == nil {
			continue
		}
		// A body that RETURNS a slot of the container asks the emitter to name a payload it cannot tag.
		// `xs = [1.5, 2.5]` / `def one(): return xs[1]` / `print(one())` answers **`2`** for the reference's
		// `2.5`, and `[True, False]` answers `0` for `False`, because the road that carries a slot out of a
		// frame carries one untagged `i32` and prints whatever the slot's word already was (roadmap Gap
		// R.324, Gap R.219, Gap R.331). Such a name keeps the refusal it had — the module-state sentence at
		// exit 1 — rather than trading it for one of those numbers at exit 0, which is **ADR 0166**'s
		// ordering restated by **ADR 0273**: an honest refusal above a confident wrong answer.
		//
		// It declines the NAME rather than raising the family's error, and the difference matters to the
		// program around it: one unanswerable container in a body that reads three should not cost the other
		// two their answer. What the declined name then does is exactly what it did on the parent build —
		// the module's own roads reach for a slot nobody allocated, and whatever they printed then (a refusal,
		// the audit's exit 2, or one of the untagged words Gap R.324 · Gap R.219 · Gap R.331 own) is what it
		// prints now. This door does not inherit those wrongnesses into new shapes, and does not pay for them
		// with somebody else's answer either.
		if moduleBodyReturnsUnnameableSlot(fd, nm, lit) {
			continue
		}
		as := &AssignStmt{Target: &Name{Value: nm}, Value: lit, Src: fd.Src}
		if err := g.stmtAssign(b, as, as); err != nil {
			return err
		}
		g.moduleContainerReads[nm] = true
	}
	return nil
}

// moduleBodyReturnsUnnameableSlot asks one body whether it hands back, from a `return`, a slot of the
// module's container that the emitter would have to NAME — a constant read whose payload is not an integer.
//
// The question is the same one `literalSlotAnswersInts` asks, put to the body instead of to the literal, and
// it is asked here because the answer is a property of the READ and not of the object: `def row(): return
// ROWS[1][0]` reads a number out of two slots and is answered, while `def one(): return xs[1]` over a list of
// doubles reads a double and is not — same container road, same `@rt_get_elem` returning one `i32`, different
// promise about what the word means. A body that only ever asks the OBJECT (`print(xs)`, `len(xs)`, `for x in
// xs`, `x in xs`, `ys = xs`) never trips this and keeps the whole of the door.
//
// The walk follows the container through every carrier the AST hides it in, and a `return` this walk cannot
// READ (a call's answer, a comprehension, a name rebound between here and there) counts as unnameable: the
// refusal is the safe answer for a question the door could not finish asking.
func moduleBodyReturnsUnnameableSlot(fd *FuncDef, nm string, lit Expr) bool {
	if fd == nil {
		return false
	}
	aliases := map[string]bool{nm: true}
	unnameable := false
	eachBodyStmt(fd.Body, true, func(st Stmt) bool {
		switch n := st.(type) {
		case *AssignStmt:
			if t, isName := n.Target.(*Name); isName {
				if a, isName := n.Value.(*Name); isName && aliases[a.Value] {
					aliases[t.Value] = true // `ys = xs` — the same object, read under another spelling
				}
			}
		case *ReturnStmt:
			if n.Expr == nil {
				return true
			}
			// An arithmetic or concatenating answer is built FROM the object, which is the shape the
			// concatenation frame has no slot map for (Gap R.399). Refused here, at exit 1, rather than
			// emitted and stopped by the frame-slot audit at exit 2 — the audit is the compiler saying it
			// was asked to build a module it could not see, and a program the gate could read should never
			// reach it.
			if binOpBuildsFromContainer(n.Expr, aliases) {
				unnameable = true
				return false
			}
			for _, chain := range containerReadChains(n.Expr, aliases) {
				if !literalSlotAnswersInts(lit, chain) {
					unnameable = true
					return false
				}
			}
		}
		return true
	})
	return unnameable
}

// binOpBuildsFromContainer answers whether an answer is BUILT out of the container by one of the operators
// the emitter lowers as a new object — `return xs + [3]`, whose concatenation frame this pass has no slot map
// for (Gap R.399). It asks only the DIRECT operands of the top-level operators, because the shapes that read
// the object through a door of their own are the answered family and not this question: `return len(xs) == 3`
// has a call and a number as its operands, and the container is inside the call, where `len` asks the object
// for its own count.
func binOpBuildsFromContainer(e Expr, aliases map[string]bool) bool {
	n, isBin := exprUnwrap(e).(*BinOp)
	if !isBin {
		return false
	}
	// Only the operators that BUILD a new container from the object are this question. `x in xs`,
	// `xs == ys`, `xs < ys` and their siblings ask the object a question and answer something else, and
	// every one of them is on the answered family's side of the door.
	switch n.Op {
	case "+", "*":
	default:
		return false
	}
	return containerOperand(n.L, aliases) || containerOperand(n.R, aliases)
}

// containerOperand reads one operand of an operator: the container itself, a slot of it, or a parenthesised
// or signed road to either — and nothing else. Anything the walk has to open a call, a literal or a
// comprehension to reach is not an operand of the answer's own operator.
func containerOperand(e Expr, aliases map[string]bool) bool {
	n, isName := exprUnwrap(e).(*Name)
	return isName && aliases[n.Value]
}

// containerReadChains collects every read of an aliased container that a `return` reaches, as the chain of
// indices the read applies: `xs[1]` under the name `xs` is `[IntLit 1]`, `ROWS[1][0]` is `[IntLit 1, IntLit
// 0]`, and the container itself — handed back whole, or reached through a call's operand — is the empty
// chain, which `literalSlotAnswersInts` reads as "the answer IS the module's object".
//
// A read whose index is not a literal is dropped from the list rather than answered: the general road refuses
// a computed position on a name it cannot fold (`index must be a constant`), so this door has nothing to
// promise about it and nothing to refuse on its behalf.
func containerReadChains(e Expr, aliases map[string]bool) [][]Expr {
	var out [][]Expr
	// `bare` says whether the position can still BE the object: the answer itself, or an operand of the
	// answer's own operators. An argument of a call, an element of a literal, a comprehension's field and a
	// slice's bounds are not — `return len(xs)` hands back a count, and the door that answers it asks the
	// object, which is the road this file is standing in front of only when the emitter would have had to
	// NAME a payload instead.
	var walk func(e Expr, bare bool, chain []Expr)
	walk = func(e Expr, bare bool, chain []Expr) {
		e = exprUnwrap(e)
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *Index:
			walk(n.Obj, bare, append(append([]Expr{}, chain...), n.Idx))
			walk(n.Idx, false, nil)
			return
		case *Name:
			// Only a read that IS the answer is this door's question. A slot read that arrives as a call's
			// argument, an element of a literal or a comprehension's field is read by a road that asks the
			// OBJECT — `len(t[1])`, `print(xs[0])`, `[xs[1], d["a"]]` — and those roads carry the tag with
			// them, which is the whole difference between the answered family and the refused one.
			if aliases[n.Value] && bare {
				out = append(out, chain)
			}
			return
		case *Slice:
			walk(n.Obj, false, nil)
			walk(n.Low, false, nil)
			walk(n.High, false, nil)
			walk(n.Step, false, nil)
			return
		case *Attr:
			walk(n.Obj, false, nil)
			return
		case *BinOp:
			// `xs == ys`, `x in xs` and `xs + [3]` are three different promises and only the last is
			// built from the object; the gate reads which methods and operators it has written, and the
			// concatenation half of that question is the caller's, not this walk's.
			walk(n.L, false, nil)
			walk(n.R, false, nil)
			return
		case *UnOp:
			// `not xs` asks the object its own count question and answers a verdict, and `-xs` is not a
			// program — neither hands the object back, so neither is the escape this walk looks for.
			walk(n.X, false, nil)
			return
		case *CondExpr:
			walk(n.Cond, false, nil)
			walk(n.If, bare, chain)
			walk(n.Else, bare, chain)
			return
		case *Call:
			walk(n.Fn, false, nil)
			for _, a := range n.Args {
				if kw, isKw := a.(*KeywordArg); isKw {
					walk(kw.Value, false, nil)
					continue
				}
				walk(a, false, nil)
			}
			return
		case *ListLit:
			for _, el := range n.Elems {
				walk(el, false, nil)
			}
			return
		case *SetLit:
			for _, el := range n.Elems {
				walk(el, false, nil)
			}
			return
		case *Tuple:
			for _, el := range n.Elems {
				walk(el, false, nil)
			}
			return
		case *DictLit:
			for _, k := range n.Keys {
				walk(k, false, nil)
			}
			for _, v := range n.Vals {
				walk(v, false, nil)
			}
			return
		case *FString:
			for _, p := range n.Parts {
				walk(p.Expr, false, nil)
			}
			return
		}
		walkExprRest(e, func(c Expr) { walk(c, false, nil) })
	}
	walk(e, true, nil)
	return out
}

// walkExprRest hands a walker the carriers the switch above did not name, so a shape added to the AST later
// is visited by the doors in this file rather than silently skipped by them.
func walkExprRest(e Expr, mark func(Expr)) {
	if e == nil {
		return
	}
	for _, f := range exprExprFields(e) {
		mark(f)
	}
}

// exprExprFields lists the expression fields of the nodes the switch above does not read. It is the same
// carrier list `exprTouchesNames` walks, minus the cases that file has already answered.
func exprExprFields(e Expr) []Expr {
	switch n := exprUnwrap(e).(type) {
	case nil:
		return nil
	case *Comp:
		out := []Expr{n.Iter, n.Cond}
		for _, el := range n.Elems {
			out = append(out, el)
		}
		for _, k := range n.Keys {
			out = append(out, k)
		}
		for _, v := range n.Vals {
			out = append(out, v)
		}
		for _, c := range n.Clauses {
			if c != nil {
				out = append(out, c.Iter, c.Cond)
			}
		}
		return out
	case *Lambda:
		return nil // a lambda body is its own frame; its reads are asked of that frame, not of this one
	}
	return nil
}

// moduleContainerBound answers, for the doors that report on a name, whether THIS frame holds the copy
// the prologue bound. It is a fact the emitter already carries in its slot and notebook tables; the
// question exists so a diagnostic, an IR row and a test can ask it without reaching into those tables.
func (g *irGen) moduleContainerBound(name string) bool {
	return g.moduleContainerReads != nil && g.moduleContainerReads[name]
}

// exprWritesThroughCall walks an expression — through every carrier the AST hides an expression in, the
// interpolation's field and the comprehension's element among them — for a method call on the object (or
// on anything the handle reached) that is not one of the methods `readOnlyMethods` has read. It is the
// shape the gate's `checkExpr` declines the name for, asked again on the emitter's side, and a
// `f"{xs.append(1)}"` that walked past the check would be the stale-container answer at the exit code of
// success (roadmap Gap R.398).
func exprWritesThroughCall(e Expr, aliases map[string]bool) bool {
	switch n := exprUnwrap(e).(type) {
	case nil:
		return false
	case *Call:
		if at, isAttr := n.Fn.(*Attr); isAttr {
			meth := ""
			if at.Name != nil {
				meth = at.Name.Value
			}
			if !readOnlyMethods[meth] && exprTouchesNames(at.Obj, aliases) {
				return true
			}
		}
		if exprWritesThroughCall(n.Fn, aliases) {
			return true
		}
		for _, a := range n.Args {
			if exprWritesThroughCall(a, aliases) {
				return true
			}
		}
	case *Index:
		return exprWritesThroughCall(n.Obj, aliases) || exprWritesThroughCall(n.Idx, aliases)
	case *Slice:
		return exprWritesThroughCall(n.Obj, aliases) || exprWritesThroughCall(n.Low, aliases) ||
			exprWritesThroughCall(n.High, aliases) || exprWritesThroughCall(n.Step, aliases)
	case *Attr:
		return exprWritesThroughCall(n.Obj, aliases)
	case *BinOp:
		return exprWritesThroughCall(n.L, aliases) || exprWritesThroughCall(n.R, aliases)
	case *UnOp:
		return exprWritesThroughCall(n.X, aliases)
	case *CondExpr:
		return exprWritesThroughCall(n.Cond, aliases) || exprWritesThroughCall(n.If, aliases) ||
			exprWritesThroughCall(n.Else, aliases)
	case *ChainCompare:
		for _, o := range n.Operands {
			if exprWritesThroughCall(o, aliases) {
				return true
			}
		}
	case *ListLit:
		for _, el := range n.Elems {
			if exprWritesThroughCall(el, aliases) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if exprWritesThroughCall(el, aliases) {
				return true
			}
		}
	case *Tuple:
		for _, el := range n.Elems {
			if exprWritesThroughCall(el, aliases) {
				return true
			}
		}
	case *DictLit:
		for _, k := range n.Keys {
			if exprWritesThroughCall(k, aliases) {
				return true
			}
		}
		for _, v := range n.Vals {
			if exprWritesThroughCall(v, aliases) {
				return true
			}
		}
	case *FString:
		for _, p := range n.Parts {
			if exprWritesThroughCall(p.Expr, aliases) {
				return true
			}
		}
	case *Comp:
		for _, el := range n.Elems {
			if exprWritesThroughCall(el, aliases) {
				return true
			}
		}
		for _, k := range n.Keys {
			if exprWritesThroughCall(k, aliases) {
				return true
			}
		}
		for _, v := range n.Vals {
			if exprWritesThroughCall(v, aliases) {
				return true
			}
		}
		for _, cl := range n.compClauses() {
			if exprWritesThroughCall(cl.Iter, aliases) || exprWritesThroughCall(cl.Cond, aliases) {
				return true
			}
		}
	case *Lambda:
		return exprWritesThroughCall(n.Body, aliases)
	}
	return false
}

// eachBodyStmt calls fn on every statement a body reaches — the arms of its branches, the arms of its
// try, the bodies of its loops and its `match` arms — and, when `intoDefs` is set, the bodies of the
// `def`s and `class`es written inside it. ONE walk for the two questions a body is asked below: a second
// copy of this control-flow switch would be a second answer about which statements a body has, which is
// ADR 0232's rule with the arrows reversed. `fn` returns false to stop the walk.
//
// The `intoDefs` flag is the scope question, not a depth knob. Whether a body WRITES through an object is
// asked through the defs written inside it too — a nested body holds the same handle — while whether a
// body TAKES a name for itself is not: what a nested `def` binds is that `def`'s own business, exactly as
// `freeNamesOfStmts` reads the same rule for the reads (ADR 0220).
func eachBodyStmt(sts []Stmt, intoDefs bool, fn func(Stmt) bool) bool {
	for _, st := range sts {
		if st == nil {
			continue
		}
		if !fn(st) {
			return false
		}
		ok := true
		switch n := st.(type) {
		case *IfStmt:
			ok = eachBodyStmt(n.Then, intoDefs, fn) && eachBodyStmt(n.Else, intoDefs, fn)
			for _, e := range n.Elifs {
				ok = ok && eachBodyStmt(e.Then, intoDefs, fn) && eachBodyStmt(e.Else, intoDefs, fn)
			}
		case *WhileStmt:
			ok = eachBodyStmt(n.Body, intoDefs, fn) && eachBodyStmt(n.Else, intoDefs, fn)
		case *ForStmt:
			ok = eachBodyStmt(n.Body, intoDefs, fn) && eachBodyStmt(n.Else, intoDefs, fn)
		case *TryStmt:
			ok = eachBodyStmt(n.Body, intoDefs, fn) && eachBodyStmt(n.Finally, intoDefs, fn)
			for _, a := range n.Excepts {
				ok = ok && eachBodyStmt(a.Body, intoDefs, fn)
			}
		case *WithStmt:
			ok = eachBodyStmt(n.Body, intoDefs, fn)
		case *MatchStmt:
			for _, c := range n.Cases {
				ok = ok && eachBodyStmt(c.Body, intoDefs, fn)
			}
		case *FuncDef:
			ok = !intoDefs || eachBodyStmt(n.Body, intoDefs, fn)
		case *ClassDef:
			ok = !intoDefs || eachBodyStmt(n.Body, intoDefs, fn)
		}
		if !ok {
			return false
		}
	}
	return true
}

// moduleBodyClaimsName asks ONE body the question the gate asks the whole program, at the moment the copy
// would be built: does this frame take the name for itself? Every binding form counts — an assignment, an
// unpacking target, an augmented assignment, a `for` target, a `with … as`, a `match` capture, an import,
// a type alias, an `except … as`, and a nested `def`/`class` that answers to the spelling.
//
// It is asked HERE as well as by the gate because the two doors are read by different code and a body
// that binds the name has an OWNER for the slot already: minting a second binding of it is the
// already-allocated-slot shape whose symptom is a read of a handle nobody owns, which the frame-slot audit
// (`frame_audit.go`) stops with exit 2 — the code ADR 0166 reserves for this compiler's bug, and the wrong
// answer for a program. ADR 0351 (roadmap Gap R.49) is the measurement that says what one un-followed
// binder costs; this is the same lesson asked of the new door.
func moduleBodyClaimsName(fd *FuncDef, nm string) bool {
	if fd == nil {
		return false
	}
	spelled := func(e Expr) bool {
		n, ok := e.(*Name)
		return ok && n.Value == nm
	}
	claimed := false
	eachBodyStmt(fd.Body, false, func(st Stmt) bool {
		switch n := st.(type) {
		case *AssignStmt:
			if spelled(n.Target) {
				claimed = true
				return false
			}
			taken := false
			markTargetNames(n.Target, func(nms ...string) {
				for _, s := range nms {
					if s == nm {
						taken = true
					}
				}
			})
			if taken {
				claimed = true
				return false
			}
		case *AugAssignStmt:
			if spelled(n.Target) {
				claimed = true
				return false
			}
		case *ForStmt:
			for _, v := range loopVarNames(n.Var) {
				if v == nm {
					claimed = true
					return false
				}
			}
		case *WithStmt:
			if n.As != nil && n.As.Value == nm {
				claimed = true
				return false
			}
		case *MatchStmt:
			for _, c := range n.Cases {
				for mn := range matchCaptureNames(c.Pattern) {
					if mn == nm {
						claimed = true
						return false
					}
				}
				for _, or := range c.Or {
					for mn := range matchCaptureNames(or) {
						if mn == nm {
							claimed = true
							return false
						}
					}
				}
			}
		case *ImportStmt:
			for _, s := range importBoundNames(n) {
				if s == nm {
					claimed = true
					return false
				}
			}
		case *TypeAliasStmt:
			if n.Name == nm {
				claimed = true
				return false
			}
		case *TryStmt:
			for _, a := range n.Excepts {
				if a.As == nm {
					claimed = true
					return false
				}
			}
		case *FuncDef:
			if n.Name == nm {
				claimed = true
				return false
			}
		case *ClassDef:
			if n.Name == nm {
				claimed = true
				return false
			}
		}
		return true
	})
	return claimed
}

// moduleBodyWritesThrough asks ONE body whether it writes through the object under this name, or through
// any name the handle reached inside the body: the four positive shapes that write are a slot
// (`xs[i] = v`, `xs.a = v`), an augmented binding (`xs += v`) and a method the gate has not read
// (`xs.method(v)` — `readOnlyMethods` is the gate's own list, so the two doors cannot disagree about
// which methods only look). The aliases are followed one step: `ys = xs` makes `ys.append(1)` a write
// through the module's object.
//
// The gate (`moduleContainersAreReadOnly`) already asks this of the WHOLE program, so a body that reaches
// here and answers true is a gate bug rather than a program bug — which is why the caller's response is
// the family's own refusal and not a copy. Two doors reading the same rule from opposite ends is how a
// wrong answer at the exit code of success survives a review; the answer on record for the same shapes
// (roadmap Gap R.398's probes) is a refusal, and this keeps that answer.
func moduleBodyWritesThrough(fd *FuncDef, nm string) bool {
	if fd == nil {
		return false
	}
	aliases := map[string]bool{nm: true}
	wrote := false
	eachBodyStmt(fd.Body, true, func(st Stmt) bool {
		switch n := st.(type) {
		case *AssignStmt:
			switch t := n.Target.(type) {
			case *Index:
				if exprTouchesNames(t.Obj, aliases) || exprTouchesNames(t.Idx, aliases) {
					wrote = true
					return false
				}
			case *Attr:
				if exprTouchesNames(t.Obj, aliases) {
					wrote = true
					return false
				}
			case *Name:
				if a, isName := n.Value.(*Name); isName && aliases[a.Value] {
					aliases[t.Value] = true // `ys = xs` — one object, two spellings
				}
			}
		case *AugAssignStmt:
			if exprTouchesNames(n.Target, aliases) {
				wrote = true
				return false
			}
		case *ExprStmt:
			if exprWritesThroughCall(n.Expr, aliases) {
				wrote = true
				return false
			}
		}
		return true
	})
	return wrote
}

// freeNamesOfStmts collects the names a body reads without binding them itself — the same per-scope rule
// `moduleBodyReads` applies program-wide, asked of one body. A name the body binds anywhere in itself is
// not a module read at all (ADR 0220), and a nested `def` shadows what it binds exactly as a body does.
func freeNamesOfStmts(sts []Stmt, binds map[string]bool, out map[string]bool) {
	var mark func(Expr)
	mark = func(e Expr) {
		switch n := exprUnwrap(e).(type) {
		case nil:
			return
		case *Name:
			if !binds[n.Value] {
				out[n.Value] = true
			}
		case *Index:
			mark(n.Obj)
			mark(n.Idx)
		case *Slice:
			mark(n.Obj)
			mark(n.Low)
			mark(n.High)
			mark(n.Step)
		case *Attr:
			mark(n.Obj)
		case *BinOp:
			mark(n.L)
			mark(n.R)
		case *UnOp:
			mark(n.X)
		case *CondExpr:
			mark(n.Cond)
			mark(n.If)
			mark(n.Else)
		case *ChainCompare:
			for _, o := range n.Operands {
				mark(o)
			}
		case *Call:
			mark(n.Fn)
			for _, a := range n.Args {
				mark(a)
			}
		case *ListLit:
			for _, el := range n.Elems {
				mark(el)
			}
		case *SetLit:
			for _, el := range n.Elems {
				mark(el)
			}
		case *Tuple:
			for _, el := range n.Elems {
				mark(el)
			}
		case *DictLit:
			for _, k := range n.Keys {
				mark(k)
			}
			for _, v := range n.Vals {
				mark(v)
			}
		case *FString:
			for _, p := range n.Parts {
				mark(p.Expr)
			}
		case *Comp:
			// A comprehension is a carrier of expressions AND a binding form: its targets belong to the
			// comprehension (`compTargets` is the one question that names them, Gap R.352's rule), so a
			// clause that walks `xs` into `xs` is the comprehension's own name, not the module's.
			inner := copyAliases(binds)
			for _, t := range n.compTargets() {
				if t != nil {
					inner[t.Value] = true
				}
			}
			saved := binds
			binds = inner
			for _, el := range n.Elems {
				mark(el)
			}
			for _, k := range n.Keys {
				mark(k)
			}
			for _, v := range n.Vals {
				mark(v)
			}
			for _, cl := range n.compClauses() {
				mark(cl.Iter)
				mark(cl.Cond)
			}
			binds = saved
		}
	}
	var walk func(Stmt)
	var walkList func([]Stmt)
	walkList = func(list []Stmt) {
		for _, st := range list {
			walk(st)
		}
	}
	walk = func(st Stmt) {
		switch n := st.(type) {
		case nil:
			return
		case *AssignStmt:
			mark(n.Target)
			mark(n.Value)
		case *AugAssignStmt:
			mark(n.Target)
			mark(n.Value)
		case *ExprStmt:
			mark(n.Expr)
		case *ReturnStmt:
			mark(n.Expr)
		case *IfStmt:
			mark(n.Cond)
			walkList(n.Then)
			for _, e := range n.Elifs {
				mark(e.Cond)
				walkList(e.Then)
				walkList(e.Else)
			}
			walkList(n.Else)
		case *WhileStmt:
			mark(n.Cond)
			walkList(n.Body)
			walkList(n.Else)
		case *ForStmt:
			mark(n.Iter)
			walkList(n.Body)
			walkList(n.Else)
		case *TryStmt:
			walkList(n.Body)
			for _, a := range n.Excepts {
				walkList(a.Body)
			}
			walkList(n.Finally)
		case *WithStmt:
			mark(n.Expr)
			walkList(n.Body)
		case *MatchStmt:
			mark(n.Subject)
			for _, c := range n.Cases {
				walkList(c.Body)
			}
		case *FuncDef:
			inner := copyAliases(binds)
			binds2 := map[string]bool{}
			collectLocals(n.Body, binds2)
			for nm := range binds2 {
				inner[nm] = true
			}
			for _, p := range n.Params {
				inner[p.Name] = true
			}
			saved := binds
			binds = inner
			walkList(n.Body)
			binds = saved
		case *ClassDef:
			inner := copyAliases(binds)
			binds2 := map[string]bool{}
			collectLocals(n.Body, binds2)
			for nm := range binds2 {
				inner[nm] = true
			}
			saved := binds
			binds = inner
			walkList(n.Body)
			binds = saved
		}
	}
	walkList(sts)
}
