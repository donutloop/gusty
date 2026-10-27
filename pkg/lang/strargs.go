package lang

import (
	"sort"
	"sync"
)

// String parameters in the AOT backend (roadmap Gap J.5).
//
// A string in this compiler is a compile-time global (@.strN), while a function parameter is
// an i32 slot, so `shout("hi")` used to emit `call i32 @shout(i32 @.str1)` and LLVM rejected
// the module — reporting valid code as a compiler bug. Gap I.2 put strings in a runtime
// interned table (@str_tab / @str_repr_tab, addressed by index), which makes a string
// parameter representable: the argument is interned at the call site and the callee receives
// the index.
//
// The callee has to *know* which of its parameters are string indices, so this file works out
// which parameters receive strings — from a `str` annotation, a string default, or a call
// site that passes a string. The rules mirror heapargs.go's container-parameter inference
// (and reuse its walker): annotations and defaults are authoritative, call sites add
// evidence, and classification runs to a fixed point in a deterministic (sorted) order,
// because `def outer(s): inner(s)` must not classify differently depending on Go's map
// iteration order.

// strArgKinds maps "function name" -> set of parameter indices that receive strings.
// textMethods returns the methods the text census may classify, keyed by the method's OWN name — the key
// `emitClassMethod` already asks the verdict table with (Gap R.111's bool half, ADR 0223), so the two
// tables read a callable the same way.
//
// A method is classifiable only when ONE callable in the program owns that name: two classes with a `run`
// method, or a class method and a module `def` sharing a name, means two parameter lists, and marking one
// from the other's call site would dress a number in the string table. The ambiguity rule is boolargs.go's,
// copied rather than shared because the two censuses answer different questions about the same position
// (roadmap Gap R.315, ADR 0366).
func textMethods(prog *Program, funcs map[string]*FuncDef) map[string]*FuncDef {
	out := map[string]*FuncDef{}
	ambiguous := map[string]bool{}
	for _, st := range prog.Stmts {
		cd, ok := st.(*ClassDef)
		if !ok {
			continue
		}
		for _, st2 := range cd.Body {
			m, ok := st2.(*FuncDef)
			if !ok || m == nil {
				continue
			}
			if _, dup := out[m.Name]; dup {
				ambiguous[m.Name] = true
				continue
			}
			if _, clash := funcs[m.Name]; clash {
				ambiguous[m.Name] = true
				continue
			}
			out[m.Name] = m
		}
	}
	for n := range ambiguous {
		delete(out, n)
	}
	return out
}

func strArgKinds(prog *Program) map[string]map[int]bool {
	if prog == nil {
		return nil
	}
	funcs := map[string]*FuncDef{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			funcs[fd.Name] = fd
		}
	}
	// A method's parameter is a parameter. The walk that reads the arguments a program hands a `def` now
	// reads the arguments it hands `obj.m(...)`, and a method body that builds a text out of one meets the
	// same doors a function body meets: until it did, `def fmt(self, s): return "x%s" % s` printed `x0` and
	// `def rep(self, s): return s * 2` printed `0` — the @str_tab POSITION of the text, at exit 0 — because
	// the body never learned the argument arriving was an interned text (roadmap Gap R.315, ADR 0366).
	methods := textMethods(prog, funcs)
	if len(funcs) == 0 && len(methods) == 0 {
		return nil
	}
	// The one table every loop below walks: a `def` and a method are both callables with parameters, and
	// the only difference the census honours is that a method's first parameter belongs to the receiver
	// (roadmap Gap R.315, ADR 0366).
	callables := map[string]*FuncDef{}
	for n, fd := range funcs {
		callables[n] = fd
	}
	for n, m := range methods {
		callables[n] = m
	}
	callableNames := func() []string {
		names := make([]string, 0, len(callables))
		for n := range callables {
			names = append(names, n)
		}
		sort.Strings(names)
		return names
	}

	w := heapASTWalker{}

	// The programs the census classifies arrive before this scan runs, so a name the program assigned a
	// sorted list to is already known by the time the census asks what its elements are.
	defines := map[string]bool{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			defines[fd.Name] = true
		}
	}

	// Which plain variables hold a string? A name ever assigned a non-string is dropped from
	// propagation, so a parameter called `s` cannot drag an unrelated numeric `s` along.
	strVars := map[string]bool{}
	notString := map[string]bool{}
	// Which names hold a container whose elements are ALL texts. `xs = ["café"]` / `f(xs[0])` is the same
	// text crossing the same boundary, and until the census kept it the parameter arrived unclassified:
	// `def f(s): return "é" in s` printed False at exit 0 for `f(xs[0])` because the body asked its
	// container roads about a text (roadmap L11.5, ADR 0362).
	strElemVars := map[string]bool{}
	notStrElem := map[string]bool{}
	literalTextElems := func(e Expr) (ok, bad, mine bool) {
		var elems []Expr
		switch v := e.(type) {
		case *ListLit:
			elems = v.Elems // tuple or list: an element read out of either answers the element's own kind
		case *SetLit:
			elems = v.Elems
		default:
			return false, false, false
		}
		if len(elems) == 0 {
			return false, false, true
		}
		for _, el := range elems {
			if !isStringExpr(el) {
				return false, true, true
			}
		}
		return true, false, true
	}
	// One reading of the ordering builtins, shared by every question the census asks about them. The
	// candidates are what the key will be called with, and they answer the SAME question in both shapes:
	// `sorted` hands back a CONTAINER of them, `min` and `max` hand back ONE of them — which is why this
	// returns the candidates and lets each caller decide whether it is asking about a container's
	// elements or about a single value (roadmap L11.7, Gap R.264).
	candidates := keyDoorCandidates(defines)
	allTextElems := func(e Expr) (bool, bool) {
		if ok, bad, mine := literalTextElems(e); mine {
			return ok, bad
		}
		elems, isScalar, ok := candidates(e)
		if !ok || !isScalar || len(elems) == 0 {
			return false, false
		}
		for _, el := range elems {
			if !isStringExpr(el) {
				return false, true
			}
		}
		return true, false
	}
	w.assign = func(scope, target string, value Expr, annot *Type) {
		key := scopeKey(scope, target)
		if isStringAnnotation(annot) || isStringExpr(value) {
			strVars[key] = true
			return
		}
		// `best = max(words, key=len)` answers ONE of the candidates, so the variable holds a TEXT the
		// program can ask a text door about, not a container: the same candidates, the other question
		// (roadmap L11.7, Gap R.264).
		if elems, isScalar, ok := candidates(value); ok && isScalar && len(elems) > 0 {
			for _, el := range elems {
				if !isStringExpr(el) {
					notString[key] = true
					return
				}
			}
			strVars[key] = true
			return
		}
		if ok, bad := allTextElems(value); ok {
			strElemVars[key] = true
		} else if bad {
			notStrElem[key] = true
		}
		if value != nil {
			notString[key] = true
		}
	}

	// Evidence that a parameter is *not* a string: the same helper called with a number on
	// another path. Without it `f("str")` alone would mark the parameter, and the unrelated
	// `f(7)` would then be printed through the string table — "(null)" for an out-of-range
	// index, which is worse than refusing to compile.
	notStrArg := map[string]map[int]bool{}
	out := map[string]map[int]bool{}
	mark := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		m := out[fn]
		if m == nil {
			m = map[int]bool{}
			out[fn] = m
		}
		m[idx] = true
	}
	// textFuncs is the conservative twin of strReturningFuncs: a function is in it only when EVERY
	// return of its body answers a text. The looser table decides how a call's answer prints; this one
	// decides what REPRESENTATION a parameter takes, where a wrong "yes" would print a number through
	// the string table — so it asks every path, not any path.
	textFuncs := map[string]bool{}
	var answerIsText func(fd *FuncDef, e Expr, depth int) bool
	callAnswersText := func(c *Call) bool {
		nm, ok := c.Fn.(*Name)
		if !ok {
			return false
		}
		if !defines[nm.Value] && (nm.Value == "str" || nm.Value == "repr") && len(c.Args) == 1 {
			return true // a rendering is text, and the body emits the same door for it (ADR 0258)
		}
		return textFuncs[nm.Value]
	}
	answerIsText = func(fd *FuncDef, e Expr, depth int) bool {
		if e == nil || depth > 6 {
			return false
		}
		if isStringExpr(e) {
			return true
		}
		switch v := e.(type) {
		case *Name:
			for i, p := range fd.Params {
				if p != nil && p.Name == v.Value && out[fd.Name][i] {
					return true
				}
			}
			if scopeHasString(strVars, notString, fd.Name, v.Value) {
				return true
			}
			for _, val := range bodyBindings(fd)[v.Value] {
				if answerIsText(fd, val, depth+1) {
					return true
				}
			}
			return false
		case *Call:
			return callAnswersText(v)
		case *BinOp:
			return isConcat(v)
		case *Index:
			// A character read out of a text IS a one-character text (ADR 0225), which is what
			// `def f(s): return g()[0]` hands its caller.
			return isStringExpr(v.Obj)
		}
		return false
	}
	fnOrder := make([]string, 0, len(funcs))
	for fn := range funcs {
		fnOrder = append(fnOrder, fn)
	}
	sort.Strings(fnOrder)
	computeTextFuncs := func() bool {
		changed := false
		for _, fn := range fnOrder {
			if textFuncs[fn] {
				continue
			}
			fd := funcs[fn]
			c := returnCollector{}
			c.stmts(fd.Body)
			if len(c.returns) == 0 {
				continue
			}
			all := true
			for _, r := range c.returns {
				if !answerIsText(fd, r, 0) {
					all = false
					break
				}
			}
			if all {
				textFuncs[fn] = true
				changed = true
			}
		}
		return changed
	}
	negOf := func(fn string, idx int) {
		if idx < 0 {
			return
		}
		m := notStrArg[fn]
		if m == nil {
			m = map[int]bool{}
			notStrArg[fn] = m
		}
		m[idx] = true
	}
	// What one argument CLAIMS about the parameter that receives it: a text, definitely not a text,
	// or nothing. One question, one answer, asked by both roads that hand a value to a function — the
	// call the program wrote, and the call the `key=` door writes (roadmap L11.7, Gap R.264).
	claim := func(scope, fn string, pos int, a Expr) {
		fd := funcs[fn]
		if fd == nil || pos < 0 || pos >= len(fd.Params) {
			return
		}
		switch strArgClaimOf(a, scope, strVars, notString, strElemVars, notStrElem, callAnswersText) {
		case claimString:
			mark(fn, pos)
		case claimNotAString:
			negOf(fn, pos)
		}
	}

	// claimArgs reads the arguments ONE call site hands ONE callable. `viaMethod` is true for `obj.m(x)`,
	// where the receiver fills the callable's first parameter: `def m(self, s)` called as `z.m("hi")` puts
	// "hi" in slot 1, and reading it as slot 0 would mark `self` a text and leave `s` a number — the same
	// off-by-one boolargs.go guards against (roadmap Gap R.315, ADR 0366).
	claimArgs := func(scope, fn string, args []Expr, viaMethod bool) {
		fd := funcs[fn]
		if fd == nil {
			fd = methods[fn]
		}
		if fd == nil {
			return
		}
		neg := func(idx int) { negOf(fn, idx) }
		pos := 0
		if viaMethod && len(fd.Params) > 0 && (fd.Params[0] == nil || fd.Params[0].Name == "self" || fd.Params[0].Name == "cls") {
			pos = 1
		}
		for _, a := range args {
			if kw, ok := a.(*KeywordArg); ok {
				for i, p := range fd.Params {
					if p == nil || p.Name != kw.Name {
						continue
					}
					if isStringExpr(kw.Value) {
						mark(fn, i)
					} else if definitelyNotAString(kw.Value) {
						neg(i)
					}
				}
				continue
			}
			if isStringExpr(a) {
				mark(fn, pos)
			} else if ix, ok := a.(*Index); ok {
				// xs[0] of a container the program filled with texts is a text (ADR 0225's answer is a
				// one-character text, and a slice of one is a text).
				if nm, ok2 := ix.Obj.(*Name); ok2 && strElemVars[scopeKey(scope, nm.Value)] && !notStrElem[scopeKey(scope, nm.Value)] {
					mark(fn, pos)
				} else if isStringExpr(ix.Obj) {
					mark(fn, pos)
				} else if sl, ok3 := ix.Obj.(*Slice); ok3 && isStringExpr(sl.Obj) {
					mark(fn, pos)
				}
			} else if ca, ok := a.(*Call); ok && callAnswersText(ca) {
				// `f(g())` is a text argument exactly when g's answer is one. Until that was asked, a parameter
				// receiving a call's answer carried no record of the text crossing the boundary: the body then
				// asked its container roads about a text, which is how `def f(s): return "é" in s` printed
				// False at exit 0 and `s[1]`/`s[1:3]` of the same parameter refused with a list-shaped message
				// (roadmap L11.5, ADR 0362).
				mark(fn, pos)
			} else if nm, ok := a.(*Name); ok && scopeHasString(strVars, notString, scope, nm.Value) {
				// forwarding a variable that is known to carry a string
				mark(fn, pos)
			} else if definitelyNotAString(a) {
				neg(pos)
			}
			pos++
		}
	}

	w.call = func(scope, fn string, args []Expr) {
		// A `key=` option is a call the AST never writes: the door calls the key once per element of
		// the container, so this scan is the only place in the compiler that can see those arguments.
		// Until it did, the key's own body met the doors beside `len`, `s[0]` and `s.upper()` with no
		// record of the text crossing the boundary, and a program the reference answers stopped with
		// `len of a non-string variable` (roadmap L11.7, Gap R.264; ADR 0340's key door).
		if _, isUser := funcs[fn]; !isUser {
			if keyFn, elems, container, isKey := keyDoorCallSites(funcs, fn, args); isKey {
				if elems != nil {
					for _, el := range elems {
						claim(scope, keyFn, 0, el)
					}
					return
				}
				// The container is a name the program wrote: the same census the ordinary call road
				// reads answers what the key will be handed.
				if nm, isName := container.(*Name); isName {
					k := scopeKey(scope, nm.Value)
					if strElemVars[k] && !notStrElem[k] {
						mark(keyFn, 0)
					} else if notStrElem[k] {
						negOf(keyFn, 0)
					}
				}
			}
			return
		}
		claimArgs(scope, fn, args, false)
	}
	// `obj.m("hi")` hands "hi" to a parameter exactly as `m("hi")` does. The walker already told this
	// census about attribute calls (boolargs.go has read them since Gap R.111 paid the verdict half the
	// same way); the census simply had nothing to say them to, because methods were not in its table of
	// callables (roadmap Gap R.315, ADR 0366).
	w.attrCall = func(scope, method string, args []Expr) {
		// The tail of a method's parameters that this call does not name belongs to the DEFAULTS, and a
		// default is written in the `def`, never interned at the call. Registering it as a text would
		// hand the body an @str_tab index nothing wrote; the position is recorded as evidence against,
		// which is how the census leaves that call on the door it already had (roadmap Gap R.315, ADR 0366).
		if fd := callables[method]; fd != nil {
			hasKeyword := false
			for _, a := range args {
				if _, isKw := a.(*KeywordArg); isKw {
					hasKeyword = true
				}
			}
			if !hasKeyword {
				firstArg := 0
				if len(fd.Params) > 0 && (fd.Params[0] == nil || fd.Params[0].Name == "self" || fd.Params[0].Name == "cls") {
					firstArg = 1
				}
				for i := len(args) + firstArg; i < len(fd.Params); i++ {
					// Only a DEFAULTED tail is evidence against. A parameter the signature requires and
					// this call did not supply is a program the reference rejects with a TypeError, not a
					// text the census must distrust — and `class C: def upper(self, s): return s.upper()`
					// is exactly that shape, where the call the walk reports is the TEXT operation on the
					// receiver and not a call of the user's method at all (Gap R.317's dispatch, seen from
					// the census side). And a defaulted tail whose DEFAULT is a text is evidence FOR: the
					// vector ADR 0434 fills writes that interned word into the slot, so negating it here
					// is what left `def m(self, s="hi"): return s` printing `0` (roadmap Gap R.479).
					p := fd.Params[i]
					if p == nil || p.Default == nil || isStringExpr(p.Default) {
						continue
					}
					negOf(method, i)
				}
			}
		}
		claimArgs(scope, method, args, true)
	}

	seed := func() {
		for _, fn := range callableNames() {
			fd := callables[fn]
			if fd == nil {
				continue
			}
			// A method's DEFAULT reaches the body through a call that names no argument — but since
			// ADR 0434 that call WRITES the slot: `callArgBinder` lays the arguments out over the
			// CALLEE'S `fd.Params` and fills every omitted slot from `fd.Params[i].Default` through the
			// same `g.value` road a written argument takes. The default's word IS there, so the
			// parameter earns the claim the default's own shape gives it, exactly as a module `def`'s
			// defaulted parameter does. Before the vector was filled the seed had to be WITHHELD — a
			// body told the slot held a text read a word nothing wrote, and
			// `print(C().greet())` for `def greet(self, name="world")` died by SIGSEGV (Gap R.315,
			// ADR 0366); withholding it is what left `def m(self, s="hi"): return s` printing the
			// `@str_tab` POSITION `0` (roadmap Gap R.479).
			for i, p := range fd.Params {
				if p == nil {
					continue
				}
				if isStringAnnotation(p.Annot) {
					mark(fn, i)
				} else if isStringExpr(p.Default) {
					mark(fn, i)
				}
			}
		}
		w.stmts(prog.Stmts)
	}
	// The first look at the bodies needs a table to read, and the table is grown from the bodies: seed
	// the parameter kinds once with nothing known, then let the two grow together below.
	seed()
	for pass := 0; pass < 8; pass++ {
		if !computeTextFuncs() {
			break
		}
	}
	seed()

	// Fixed point: a parameter known to receive a string makes its own *variable* a string, so
	// a forwarding call discovered later marks the callee's parameter too.
	for pass := 0; pass < 8; pass++ {
		before := snapshotStrKinds(out)
		for _, fn := range callableNames() {
			fd := callables[fn]
			if fd == nil {
				continue
			}
			m := out[fn]
			idxs := make([]int, 0, len(m))
			for i := range m {
				idxs = append(idxs, i)
			}
			sort.Ints(idxs)
			for _, i := range idxs {
				if i >= len(fd.Params) {
					continue
				}
				name := fd.Params[i].Name
				if !notString[scopeKey(fn, name)] && !notString[scopeKey("", name)] {
					strVars[scopeKey(fn, name)] = true
				}
			}
		}
		seed()
		computeTextFuncs()
		if snapshotStrKinds(out) == before {
			break
		}
	}

	// A parameter annotated as a container is never a string, and a parameter that is also
	// demonstrably used as a number stays unsupported rather than silently treating the number
	// as a string-table index.
	for fn, m := range out {
		for i := range m {
			if notStrArg[fn][i] {
				delete(m, i)
			}
		}
	}
	containerParams := heapArgKinds(prog)
	for fn, m := range out {
		fd := callables[fn]
		if fd == nil {
			delete(out, fn)
			continue
		}
		for i := range m {
			if i >= len(fd.Params) || fd.Params[i] == nil {
				continue
			}
			name := fd.Params[i].Name
			if containerParams[fn][i] != HeapNone {
				delete(m, i)
				continue
			}
			if notString[scopeKey(fn, name)] && !isStringAnnotation(fd.Params[i].Annot) {
				delete(m, i)
			}
		}
		if len(m) == 0 {
			delete(out, fn)
		}
	}
	return out
}

// strArgClaim is what one argument says about the parameter that receives it.
type strArgClaim int

const (
	claimNothing strArgClaim = iota
	claimString
	claimNotAString
)

// strArgClaimOf asks the one question every road that fills a parameter asks: does this argument
// hand the parameter a text, definitely not a text, or nothing worth recording? The ordinary call
// site and the `key=` door both ask it, so a key's parameter cannot be claimed a text by the road
// that calls it and denied one by the scan that classifies it (roadmap L11.7, Gap R.264).
func strArgClaimOf(a Expr, scope string, strVars, notString, strElemVars, notStrElem map[string]bool, callAnswersText func(*Call) bool) strArgClaim {
	switch e := a.(type) {
	case nil:
		return claimNothing
	case *Index:
		// xs[0] of a container the program filled with texts is a text (ADR 0225's answer is a
		// one-character text, and a slice of one is a text).
		if nm, ok := e.Obj.(*Name); ok && strElemVars[scopeKey(scope, nm.Value)] && !notStrElem[scopeKey(scope, nm.Value)] {
			return claimString
		}
		if isStringExpr(e.Obj) {
			return claimString
		}
		if sl, ok := e.Obj.(*Slice); ok && isStringExpr(sl.Obj) {
			return claimString
		}
		return claimNothing
	case *Call:
		// `f(g())` is a text argument exactly when g's answer is one (roadmap L11.5, ADR 0362).
		if callAnswersText != nil && callAnswersText(e) {
			return claimString
		}
	case *Name:
		// forwarding a variable that is known to carry a string
		if scopeHasString(strVars, notString, scope, e.Value) {
			return claimString
		}
	}
	if isStringExpr(a) {
		return claimString
	}
	if definitelyNotAString(a) {
		return claimNotAString
	}
	return claimNothing
}

// keyDoorCallSites reads a call to the ordering builtins the way their door reads it, and answers
// WHICH function the door is about to call, WHICH expressions it will hand that function's first
// parameter (nil when the caller must ask the container itself), and the container expression the
// elements come from. `sorted` takes the iterable alone and calls the key on each element of it;
// `min`/`max` take candidates, either written out or as one container. A door that would refuse the
// call — a container built by appending at run time, a key that is not a name the program declared,
// the identity `key=None` — answers "no call site", because the scan must not claim a parameter for
// a call that will never be emitted (roadmap L11.7, Gap R.264; ADR 0340, ADR 0345).
func keyDoorCallSites(funcs map[string]*FuncDef, fn string, args []Expr) (string, []Expr, Expr, bool) {
	switch fn {
	case "sorted", "min", "max":
	default:
		return "", nil, nil, false
	}
	pos, key, reads := keyDoorOptions(fn, args)
	if !reads || key == nil {
		return "", nil, nil, false
	}
	if _, isNone := key.(*NoneLit); isNone {
		return "", nil, nil, false // the identity key: the door never calls it
	}
	nm, isName := key.(*Name)
	if !isName {
		return "", nil, nil, false
	}
	fd, isUser := funcs[nm.Value]
	if !isUser {
		return "", nil, nil, false // a builtin key needs no parameter claim: the door asks the ordinary road
	}
	// The door calls the key with exactly one argument, and refuses or raises when the definition
	// cannot take one. The scan speaks only where the door will call.
	if !keyDoorCallsOneArgument(fd) {
		return "", nil, nil, false
	}
	switch fn {
	case "sorted":
		if len(pos) != 1 {
			return "", nil, nil, false
		}
		if lit, isList := pos[0].(*ListLit); isList {
			return nm.Value, lit.Elems, pos[0], true
		}
		return nm.Value, nil, pos[0], true
	default: // min, max
		if len(pos) == 1 {
			if lit, isList := pos[0].(*ListLit); isList {
				return nm.Value, lit.Elems, pos[0], true
			}
			if _, isName := pos[0].(*Name); isName {
				return nm.Value, nil, pos[0], true
			}
			return "", nil, nil, false
		}
		if len(pos) >= 2 {
			return nm.Value, pos, pos[0], true
		}
	}
	return "", nil, nil, false
}

// keyDoorOptions splits an ordering builtin's arguments the way the door itself splits them: the
// candidates, standing in the order the program wrote them, and the `key=`/`reverse=` options that
// are not operands at all. An option the door raises for (`cmp`) is not an operand either, and a call
// that is not one of these builtins answers "do not read me" (roadmap L11.7, Gap R.264; ADR 0340).
func keyDoorOptions(fn string, args []Expr) (pos []Expr, key Expr, ok bool) {
	switch fn {
	case "sorted", "min", "max":
	default:
		return nil, nil, false
	}
	for _, a := range args {
		kw, isKw := a.(*KeywordArg)
		if !isKw {
			pos = append(pos, a)
			continue
		}
		switch kw.Name {
		case "key":
			key = kw.Value
		case "reverse":
			// `sorted` reads it; it is not an operand and not a candidate
		default:
			return nil, nil, false // `cmp` and anything else: the door raises, so there is no call
		}
	}
	return pos, key, true
}

// keyDoorCallsOneArgument answers whether the ordering door will CALL this key at all. The reference
// calls a key with exactly one element and stops a key that cannot take one (`f() takes 0 positional
// arguments but 1 was given`) or that is missing a required one (`f() missing 1 required positional
// argument: 'y'`); a key the door raises for is not a call, so neither the parameter census nor the
// defunctionalisation pass may claim it as one. One rule, read by every caller that decides whether a
// hook calls its key (roadmap L11.7, Gap R.264, Gap R.281; ADR 0340, ADR 0233's rule against a second
// answer for one construct).
func keyDoorCallsOneArgument(fd *FuncDef) bool {
	if fd == nil {
		return false
	}
	// The parameters the SOURCE supplies: a capture parameter the defunctionalisation pass minted for a name
	// the body reads from its own frame is filled by the call road, not by the caller, so a key written
	// `lambda s: 0-len(s)+base` is a one-argument callable and not a two-argument one (roadmap L11.7 · Gap
	// R.376). Counting it would answer "this door never calls that key", and the pass would then refuse the
	// program for keeping a function value.
	supplied := callableSuppliedParams(fd)
	if len(supplied) == 0 {
		return false
	}
	required := 0
	for _, p := range supplied {
		if p != nil && p.Default == nil {
			required++
		}
	}
	return required <= 1
}

// keyDoorCandidates is the census's reading of the same three calls: the expressions the door will
// compare, and whether the call answers ONE of them (`min`, `max`) rather than a CONTAINER of them
// (`sorted`). A program that wrote its own `sorted` is not the builtin, and a container the compiler
// cannot enumerate answers "no candidates" rather than a guess — the census claims a kind only when
// every candidate says the same thing (ADR 0348's rule, roadmap L11.7, Gap R.264).
func keyDoorCandidates(defines map[string]bool) func(Expr) ([]Expr, bool, bool) {
	return func(e Expr) ([]Expr, bool, bool) {
		c, isCall := e.(*Call)
		if !isCall {
			return nil, false, false
		}
		fn, isName := c.Fn.(*Name)
		if !isName || defines[fn.Value] {
			return nil, false, false
		}
		pos, _, reads := keyDoorOptions(fn.Value, c.Args)
		if !reads {
			return nil, false, false
		}
		one := func(list []Expr) ([]Expr, bool, bool) {
			lit, isList := list[0].(*ListLit)
			if !isList || len(lit.Elems) == 0 {
				return nil, false, false
			}
			return lit.Elems, false, true
		}
		switch fn.Value {
		case "sorted":
			if len(pos) != 1 {
				return nil, false, false
			}
			elems, _, ok := one(pos)
			return elems, false, ok
		case "min", "max":
			if len(pos) == 1 {
				elems, _, ok := one(pos)
				return elems, true, ok
			}
			if len(pos) >= 2 {
				return pos, true, true
			}
		}
		return nil, false, false
	}
}

// snapshotStrKinds renders the inference result as a string so the fixed point can compare
// rounds without a bespoke deep-equal.
func snapshotStrKinds(m map[string]map[int]bool) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := ""
	for _, k := range keys {
		idxs := make([]int, 0, len(m[k]))
		for i := range m[k] {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		buf += k + "("
		for _, i := range idxs {
			buf += itoa(i) + ","
		}
		buf += ")"
	}
	return buf
}

// isStringAnnotation reports a `str` parameter annotation, the declaration that settles the
// question without looking at call sites.
func isStringAnnotation(t *Type) bool {
	return t.IsString()
}

// textReturningCallables reports the callables whose `return` yields a string — a string literal, an
// interpolated literal, a string parameter, or a call to another such function. The caller needs this to
// print `echo("yo")` as text rather than as the index the function returns, and to store `make_key()` in a
// container as an interned element (roadmap Gap J.5).
//
// It answers the two tables at once, because they are one question: a method's body emits the same doors a
// function's does, and a caller that was never told prints the @str_tab POSITION under the text. A method is
// in the second table only when one callable owns its name (textMethods' ambiguity rule), and a `Name` call
// is never answered out of it — `callAnswersText` and the `*Call` arm below read `defs` alone, so a variable
// sharing a method's name cannot inherit the verdict (roadmap Gap R.315, ADR 0366).
func textReturningCallables(prog *Program, answer *answerKindPlan) (defs, methods map[string]bool) {
	out := map[string]bool{}
	methOut := map[string]bool{}
	if prog == nil {
		return out, methOut
	}
	funcs := map[string]*FuncDef{}
	order := []string{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if _, seen := funcs[fd.Name]; !seen {
				order = append(order, fd.Name)
			}
			funcs[fd.Name] = fd
		}
	}
	// A string parameter makes `return s` a string return without any literal involved.
	strParams := strArgKinds(prog)
	// The methods, and which of these names belongs to a method rather than a `def`.
	isMethod := map[string]bool{}
	for n, m := range textMethods(prog, funcs) {
		if _, taken := funcs[n]; taken {
			continue
		}
		funcs[n] = m
		isMethod[n] = true
		order = append(order, n)
	}
	sort.Strings(order)
	// The names the program defines for itself, so a program's own `def str(x): return x + 7` is not
	// read as the builtin (Gap R.6's rule, ADR 0199: a fold through a builtin's meaning is only allowed
	// where the program did not take the name).
	defines := map[string]bool{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			defines[fd.Name] = true
		}
	}
	// Declared before its own body so the name-bound case can ask it of a binding value — a closure
	// assigned with `:=` cannot reach its own name in its initializer (roadmap Gap R.170, ADR 0286).
	// The depth is a cycle guard, not a tuning number: `x = x[1:3]` writes a binding whose slice base is
	// the name the binding itself defines, and the two arms below follow a name back to what wrote it.
	// The bodies this asks about nest a handful deep, so the bound is never reached — it is what stops a
	// self-referential binding from walking the stack away.
	var isStrExprIn func(fd *FuncDef, e Expr, depth int) bool
	isStrExprIn = func(fd *FuncDef, e Expr, depth int) bool {
		if depth > 8 {
			return false
		}
		if e == nil || isStringExpr(e) {
			return e != nil && isStringExpr(e)
		}
		switch v := e.(type) {
		case *Name:
			for i, p := range fd.Params {
				if p.Name == v.Value && strParams[fd.Name][i] {
					return true
				}
			}
			// A name the body bound to a rendering is a text too: `s = str(v)` / `return s` is the same
			// door two statements earlier, and the caller cannot tell the difference. ADR 0281 marked
			// `return str(42)` string-returning, but the body that binds the rendering to a name first
			// still answered a number to its callers — `print(f(3))` passed the interned index to `printf`
			// with `%d` and printed `0` at exit 0, and `"x" + str(v)` / `return s` printed `2` for `x3`
			// (roadmap L11.2, Gap R.170, ADR 0286). The two-hop question is what ADR 0285's number half
			// asks too: the value's kind is written by an assignment, not by the returned expression.
			for _, val := range bodyBindings(fd)[v.Value] {
				if isStrExprIn(fd, val, depth+1) {
					return true
				}
			}
		case *Call:
			if nm, ok := v.Fn.(*Name); ok {
				// `return str(42)` renders a value to text. The body already asks the statement-level
				// rendering door for it and returns the interned index (ADR 0258's `rt_str_intern2`), so the
				// callee's half was right and only this verdict was missing: the caller did not know the
				// answer is a text, and `print(g())` passed the index to `printf` with `%d` — `0` at exit 0
				// where CPython and the interpreter print `42` (roadmap L11.2, Gap R.163, ADR 0281).
				if !defines[nm.Value] && (nm.Value == "str" || nm.Value == "repr") && len(v.Args) == 1 {
					return true
				}
				return out[nm.Value]
			}
			// A method's `return str(...)` is the same verdict: the callee emits the same door, and the
			// caller's print dispatch asks `callReturnsStr`, which reads this table.
			return false
		case *Slice:
			// s[a:b] of a text is a text, bounds constant or not (ADR 0229). The arm was missing, so a body
			// that binds a slice to a name and returns it handed its caller an @str_tab index while the
			// caller, asking this table, rendered the number under it: `def f(s):
			//     x = s[1:3]
			//     return x` printed 1 where the reference prints af (roadmap L11.5, ADR 0362).
			return isStringExpr(v.Obj) || isStrExprIn(fd, v.Obj, depth+1)
		case *Index:
			// A character read out of a text is a one-character text (ADR 0225), so `return g()[0]` answers
			// text for the same reason slicing one does.
			return isStringExpr(v.Obj) || isStrExprIn(fd, v.Obj, depth+1)
		case *BinOp:
			// "a" + x  /  x + "a" is a string; the codegen refuses it until the runtime can
			// allocate, but the *intent* is a string, so the caller must not treat it as an int.
			// concat is refused by codegen until the runtime can allocate; keeping the
			// "it is a string" verdict here means the caller never treats it as an int.
			return isConcat(v)
		}
		return false
	}
	// Round until stable: `def a(s): return s` and `def b(s): return a(s)` reference each other.
	for pass := 0; pass < 6; pass++ {
		changed := false
		for _, name := range order {
			fd := funcs[name]
			if out[name] {
				continue
			}
			if !isMethod[name] && answer.carries(name) {
				// The body answers more than one kind, so there is no ONE kind to register: the first text
				// seen anywhere in it used to be enough, and the caller then rendered the arm that answered
				// a number through the text printer (roadmap Gap R.219, ADR 0368). Such a function carries
				// its answer's kind instead of this table's verdict.
				continue
			}
			w := returnCollector{}
			w.stmts(fd.Body)
			for _, r := range w.returns {
				if isStrExprIn(fd, r, 0) {
					if isMethod[name] {
						methOut[name] = true
					} else {
						out[name] = true
					}
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
	return out, methOut
}

// bodyBindings is the body's own assignment table (name -> every value ever bound to it), built once per
// question so a returned name can be traced back to what wrote it. `scanRebinds` is the same table ADR
// 0274's return convention and ADR 0285's number half read; caching it keeps a `y = y + 1` body from
// rebuilding it on every hop.
// var bodyBindingCache — guarded, because it is shared by every compile in flight.
//
// A plain map here was harmless while one process compiled one program at a time, which is all the CLI ever
// did and all the suite ever was. It is not harmless with a compile running beside another: two `go test`
// cases emitting IR at the same instant write the same map, which Go treats as a fatal error at best and a
// torn read at worst — and a torn read here is not a crash, it is the wrong answer to *what kind is this
// variable*, which codegen then writes into the module and the program pays for at run time. The lock is
// the whole fix: the values are a pure function of the key (a parsed body), every caller only reads what it
// hands back, and a memo that cannot be trusted is worth less than the scan it saves. (ADR 0343.)
var (
	bodyBindingMu    sync.Mutex
	bodyBindingCache = map[*FuncDef]map[string][]Expr{}
)

func bodyBindings(fd *FuncDef) map[string][]Expr {
	if fd == nil {
		return map[string][]Expr{}
	}
	bodyBindingMu.Lock()
	defer bodyBindingMu.Unlock()
	if m, ok := bodyBindingCache[fd]; ok {
		return m
	}
	m := map[string][]Expr{}
	scanRebinds(fd.Body, m)
	bodyBindingCache[fd] = m
	return m
}

// returnCollector gathers every `return` value in a body, including nested blocks, so a string
// returned from inside `if`/`match` is not missed.
type returnCollector struct {
	returns []Expr
}

func (c *returnCollector) stmts(ss []Stmt) {
	for _, s := range ss {
		c.stmt(s)
	}
}

func (c *returnCollector) stmt(s Stmt) {
	switch st := s.(type) {
	case *ReturnStmt:
		c.returns = append(c.returns, st.Expr)
	case *IfStmt:
		c.stmts(st.Then)
		for _, el := range st.Elifs {
			c.stmts(el.Then)
		}
		c.stmts(st.Else)
	case *ForStmt:
		c.stmts(st.Body)
	case *WhileStmt:
		c.stmts(st.Body)
	case *TryStmt:
		c.stmts(st.Body)
		for _, h := range st.Excepts {
			c.stmts(h.Body)
		}
		c.stmts(st.Finally)
	case *WithStmt:
		c.stmts(st.Body)
	}
}

// isConcat reports `a + b` where either side is a string, the shape the record supports
// and codegen refuses until the runtime can allocate a buffer.
func isConcat(n *BinOp) bool {
	if n.Op != "+" {
		return false
	}
	return isStringExpr(n.L) || isStringExpr(n.R)
}

// definitelyNotAString reports the argument shapes that can never be a string, so a second
// call site with a number cancels a string classification rather than silently reusing it.
func definitelyNotAString(e Expr) bool {
	switch v := e.(type) {
	case *IntLit, *FloatLit, *ListLit, *SetLit, *DictLit, *Comp, *Generator, *Lambda:
		return true
	case *Name:
		return v.Value == "None" || v.Value == "True" || v.Value == "False"
	}
	return false
}

// scopeHasString reports whether a variable is known to carry a string inside `scope`
// (falling back to module level), and was never assigned a non-string there.
func scopeHasString(strVars, notString map[string]bool, scope, name string) bool {
	if notString[scopeKey(scope, name)] {
		return false
	}
	if scope != "" && strVars[scopeKey(scope, name)] {
		return true
	}
	return strVars[scopeKey("", name)] && !notString[scopeKey("", name)]
}

// Element-kind bits for a container parameter: which positions of the container a function
// fills with interned strings. The caller needs this because a helper can fill a container its
// caller created (`def fill(out, v): out.append(v)`) — the caller's own scope has no way to
// know, and printing `xs[1]` would show the raw @str_tab index (roadmap Gap J.5).
const (
	EstrListElem  = 1
	EstrDictKey   = 2
	EstrDictValue = 4
	EstrSetMember = 8
)

// stringFillingParams maps function name -> parameter index -> element-kind bits.
func stringFillingParams(prog *Program) map[string]map[int]int {
	out := map[string]map[int]int{}
	if prog == nil {
		return out
	}
	allKinds := heapArgKinds(prog)
	allStrParams := strArgKinds(prog)
	for _, st := range prog.Stmts {
		fd, ok := st.(*FuncDef)
		if !ok {
			continue
		}
		kinds := allKinds[fd.Name]
		strParams := allStrParams[fd.Name]
		paramIndex := func(e Expr) int {
			nm, ok := e.(*Name)
			if !ok {
				return -1
			}
			for i, p := range fd.Params {
				if p.Name == nm.Value {
					return i
				}
			}
			return -1
		}
		isString := func(e Expr) bool {
			return isStringExpr(e) || (paramIndex(e) >= 0 && strParams[paramIndex(e)])
		}
		mark := func(idx, bit int) {
			if idx < 0 || kinds[idx] == HeapNone {
				return
			}
			m := out[fd.Name]
			if m == nil {
				m = map[int]int{}
				out[fd.Name] = m
			}
			m[idx] |= bit
		}
		coll := allStatements{}
		coll.stmts(fd.Body)
		for _, s := range coll.out {
			switch st := s.(type) {
			case *ExprStmt:
				// out.append(v) and s.add(v) put a string *into* a container parameter.
				c, ok := st.Expr.(*Call)
				if !ok {
					continue
				}
				attr, ok := c.Fn.(*Attr)
				if !ok || len(c.Args) != 1 {
					continue
				}
				idx := paramIndex(attr.Obj)
				switch attr.Name.Value {
				case "append":
					if isString(c.Args[0]) {
						mark(idx, EstrListElem)
					}
				case "add":
					if isString(c.Args[0]) {
						mark(idx, EstrSetMember)
					}
				}
			case *AssignStmt:
				// d[k] = v — item assignment, not a plain rebinding of the name.
				ix, ok := st.Target.(*Index)
				if !ok {
					continue
				}
				idx := paramIndex(ix.Obj)
				if idx < 0 {
					continue
				}
				if isString(st.Value) {
					mark(idx, EstrDictValue)
				}
				if isString(ix.Idx) {
					mark(idx, EstrDictKey)
				}
			}
		}
	}
	return out
}

// allStatements collects every statement in a body, including those nested in if/for/while/
// try/with blocks, so a container filled inside a loop still counts.
type allStatements struct {
	out []Stmt
}

func (c *allStatements) stmts(ss []Stmt) {
	for _, s := range ss {
		c.out = append(c.out, s)
		switch st := s.(type) {
		case *IfStmt:
			c.stmts(st.Then)
			for _, el := range st.Elifs {
				c.out = append(c.out, el.Then...)
				c.stmts(el.Then)
			}
			c.stmts(st.Else)
		case *ForStmt:
			c.stmts(st.Body)
			c.stmts(st.Else)
		case *WhileStmt:
			c.stmts(st.Body)
			c.stmts(st.Else)
		case *TryStmt:
			c.stmts(st.Body)
			for _, h := range st.Excepts {
				c.stmts(h.Body)
			}
			c.stmts(st.Finally)
		case *WithStmt:
			c.stmts(st.Body)
		case *FuncDef:
			c.stmts(st.Body) // nested functions share the enclosing parameter names
		case *ClassDef:
			c.stmts(st.Body)
		}
	}
}
