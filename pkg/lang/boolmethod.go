package lang

// boolmethod.go is what a METHOD hands back, read off the AST.
//
// The verdict road asked the question of a function and never of a method: `print(is_ok())` prints
// True because `bodyReturnsOnlyBools` is consulted for a NAME callee, and `print(C().is_ok())`
// printed `1` because the same call parses as an ATTR callee and the branch that reads a body was
// never reached (roadmap Gap R.316, ADR 0365's follow-on to L15.4 — the same class of half-answer
// ADR 0299 named for the object road, one road at a time). Text needed the same table before, and
// got it in the same place: the two censuses below sit side by side in strargs.go and here, and ask
// the same two questions — which callable owns this name, and which class does this expression hand
// back.
//
// Both answers are conservative in the same direction as every other census in this package: a name
// more than one callable owns is no name at all, and a variable the module ever binds to something
// that is not an instance is not an instance.

// methodCensusT is the method-side reading of the program.
type methodCensusT struct {
	// methods maps a method name to its definition, holding only the names exactly ONE callable
	// owns. A method every class declares (`__init__`, `area`, `name`) is answered at its own call
	// site by the receiver's own dispatch, and reading it as one callable would hand the shape of
	// one class's body to a call on another's — the lie ADR 0299 was written about.
	methods map[string]*FuncDef
	// classes maps a class name to its declaration; a duplicated class name is dropped.
	classes map[string]*ClassDef
	// sites records, per callable, whether EVERY call the program makes of it passes a verdict at
	// that position: an argument that is not a verdict (or that the compiler cannot tell) retires the
	// position. This is what lets `def __init__(self, flag): self.flag = flag` be read as a verdict
	// binding — the call-site census the parameter road asks, read off the AST alone (roadmap R.302's
	// rule that a reading must not depend on a name another census happens to have reached).
	sites map[string]map[int]bool
	// sitesBroken records a callable whose call sites the census could not read as all-verdict.
	sitesBroken map[string]bool
	// fields maps a class to the field names every write of it in the program hands a verdict to —
	// the reading that makes `def held(self): return self.flag` a verdict getter (roadmap Gap R.316).
	fields map[string]map[string]bool
	// instances maps a MODULE-level variable to the class its one and only binding builds. Only
	// the module scope is read: a function-local `c = C()` is asked with no scope in hand, and a
	// name the compiler cannot place is a name it does not classify.
	instances map[string]string
}

func (c methodCensusT) lookup(recv Expr, name string) (*FuncDef, string) {
	if name == "" {
		return nil, ""
	}
	if _, ok := c.methods[name]; !ok {
		return nil, ""
	}
	cls, ok := c.receiverClass(recv)
	if !ok {
		return nil, ""
	}
	return c.lookupClass(cls, name), cls
}

// lookupClass answers the question of a named class: the class must DECLARE the method — `C().m()`
// where C has no m is an attribute the dispatch answers with a TypeError at run time, not a call
// into some other class's m.
func (c methodCensusT) lookupClass(cls, name string) *FuncDef {
	if name == "" {
		return nil
	}
	if _, ok := c.methods[name]; !ok {
		return nil
	}
	cd, ok := c.classes[cls]
	if !ok {
		return nil
	}
	for _, st := range cd.Body {
		if m, ok := st.(*FuncDef); ok && m.Name == name {
			return m
		}
	}
	return nil
}

// receiverClass answers "which class does this expression hand back?" for the three shapes the
// compiler can tell without running the program: a direct `C(...)` of a declared class, `self`
// inside no body at all (the caller's own class answers that one, and this census does not see it),
// and a module name bound once, to such a call.
func (c methodCensusT) receiverClass(recv Expr) (string, bool) {
	switch t := recv.(type) {
	case *Call:
		nm, ok := t.Fn.(*Name)
		if !ok {
			return "", false
		}
		if _, isClass := c.classes[nm.Value]; !isClass {
			return "", false
		}
		return nm.Value, true
	case *Name:
		cls, ok := c.instances[t.Value]
		return cls, ok
	}
	return "", false
}

// methodCensus reads the program once for both answers.
func methodCensus(prog *Program, paramKinds map[string]map[int]bool) methodCensusT {
	out := methodCensusT{methods: map[string]*FuncDef{}, classes: map[string]*ClassDef{}, instances: map[string]string{}, fields: map[string]map[string]bool{}, sites: map[string]map[int]bool{}, sitesBroken: map[string]bool{}}
	if prog == nil {
		return out
	}
	for _, st := range prog.Stmts {
		cd, ok := st.(*ClassDef)
		if !ok {
			continue
		}
		if _, dup := out.classes[cd.Name]; dup {
			out.classes[cd.Name] = nil // two classes, one name: neither is that class
			continue
		}
		out.classes[cd.Name] = cd
	}
	for name, cd := range out.classes {
		if cd == nil {
			delete(out.classes, name)
		}
	}
	// A module-level `def` of the same name is a second callable owning it.
	funcNames := map[string]bool{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			funcNames[fd.Name] = true
		}
	}
	for _, cd := range out.classes {
		for _, st := range cd.Body {
			m, ok := st.(*FuncDef)
			if !ok || m == nil {
				continue
			}
			if _, seen := out.methods[m.Name]; seen || funcNames[m.Name] {
				out.methods[m.Name] = nil // ambiguous: two callables, one name
				continue
			}
			out.methods[m.Name] = m
		}
	}
	for name, m := range out.methods {
		if m == nil {
			delete(out.methods, name)
		}
	}
	// `c = C()` at module scope, once — and never anywhere else either: a function that binds the
	// same name to something else is read with this same table in hand, so a name the program binds
	// more than once, in any scope, is a name the compiler cannot place.
	bound := map[string]int{}
	boundWalk := heapASTWalker{}
	boundWalk.assign = func(scope, target string, value Expr, annot *Type) { bound[target]++ }
	boundWalk.stmts(prog.Stmts)
	for _, st := range prog.Stmts {
		as, ok := st.(*AssignStmt)
		if !ok || as.Target == nil {
			continue
		}
		nm, ok := as.Target.(*Name)
		if !ok || bound[nm.Value] != 1 {
			continue // bound twice, anywhere: the compiler does not know which binding is read
		}
		call, ok := as.Value.(*Call)
		if !ok {
			continue
		}
		fn, ok := call.Fn.(*Name)
		if !ok {
			continue
		}
		if _, isClass := out.classes[fn.Value]; isClass {
			out.instances[nm.Value] = fn.Value
		}
	}
	// Which fields hold a verdict. `def held(self): return self.flag` is a verdict getter — the
	// reference prints True for it and the compiled print road printed the 0/1 a verdict is stored
	// as, because the census could read a NAME and an expression but not a field (roadmap Gap R.316,
	// the getter half). A field is a verdict only when EVERY write of that name anywhere in the
	// program hands it one: `self.ready = True` and `self.ready = a > b` agree, and one
	// `obj.ready = 3` anywhere — in the class, in a caller, through any receiver — retires the name
	// for every class, because the compiler cannot tell which object the write reached.
	// The call-site reading, from the same walk the parameter censuses use: every `C(...)` and every
	// `x.m(...)` the program makes, with each position answered by the AST alone.
	siteAsk := func(fn string, args []Expr) {
		if fn == "" || (out.methods[fn] == nil && out.classes[fn] == nil) {
			return
		}
		if out.sites[fn] == nil {
			out.sites[fn] = map[int]bool{}
		}
		for i, a := range args {
			if kw, ok := a.(*KeywordArg); ok && kw != nil {
				a = kw.Value
			}
			if a != nil && IsBoolExpr(a, BoolEnv{MethodLookup: out.lookup, SelfLookup: out.lookupClass}) {
				out.sites[fn][i] = true
				continue
			}
			out.sitesBroken[fn] = true
		}
	}
	w := heapASTWalker{}
	w.call = func(scope, fn string, args []Expr) { siteAsk(fn, args) }
	w.attrCall = func(scope, method string, args []Expr) { siteAsk(method, args) }
	w.stmts(prog.Stmts)
	siteVerdict := func(fn string, i int) bool {
		return !out.sitesBroken[fn] && out.sites[fn][i]
	}
	askVerdict := func(cls string, fd *FuncDef, value Expr) bool {
		if value == nil {
			return false
		}
		if nm, ok := value.(*Name); ok && nm != nil && fd != nil {
			for i, p := range fd.Params {
				if p == nil || p.Name != nm.Value {
					continue
				}
				if p.Annot != nil && p.Annot.IsBool() {
					return true
				}
				// A method's parameters are the call's arguments shifted by `self`, which is the
				// index the call-site census records — under the METHOD's name for `c.m(True)`, and
				// under the CLASS's name for the constructor call `C(True)` that reaches `__init__`.
				if i > 0 && (paramKinds[fd.Name][i-1] || (cls != "" && paramKinds[cls][i-1]) ||
					siteVerdict(fd.Name, i-1) || (cls != "" && siteVerdict(cls, i-1))) {
					return true
				}
				return false
			}
		}
		v := BoolEnv{MethodLookup: out.lookup, SelfLookup: out.lookupClass, SelfClass: cls}
		return IsBoolExpr(value, v)
	}
	// Every `x.f = value` write in the program, wherever it is and whatever x is.
	seen := map[string]map[string]bool{} // class -> field -> ever written with a verdict
	bad := map[string]map[string]bool{}  // class -> field -> written with something else
	noteWrite := func(field string, verdict bool) {
		for cls := range out.classes {
			if !verdict {
				if bad[cls] == nil {
					bad[cls] = map[string]bool{}
				}
				bad[cls][field] = true
				continue
			}
			if seen[cls] == nil {
				seen[cls] = map[string]bool{}
			}
			seen[cls][field] = true
		}
	}
	var walkBody func([]Stmt, string, *FuncDef)
	walkBody = func(sts []Stmt, cls string, fd *FuncDef) {
		for _, st := range sts {
			switch t := st.(type) {
			case *AssignStmt:
				if at, ok := t.Target.(*Attr); ok && at != nil && at.Name != nil {
					noteWrite(at.Name.Value, askVerdict(cls, fd, t.Value))
				}
			case *AugAssignStmt:
				if at, ok := t.Target.(*Attr); ok && at != nil && at.Name != nil {
					noteWrite(at.Name.Value, false)
				}
			case *IfStmt:
				walkBody(t.Then, cls, fd)
				for _, e := range t.Elifs {
					if e != nil {
						walkBody([]Stmt{e}, cls, fd)
					}
				}
				walkBody(t.Else, cls, fd)
			case *WhileStmt:
				walkBody(t.Body, cls, fd)
				walkBody(t.Else, cls, fd)
			case *ForStmt:
				walkBody(t.Body, cls, fd)
				walkBody(t.Else, cls, fd)
			case *TryStmt:
				walkBody(t.Body, cls, fd)
				for _, e := range t.Excepts {
					if e != nil {
						walkBody(e.Body, cls, fd)
					}
				}
				walkBody(t.Finally, cls, fd)
			case *WithStmt:
				walkBody(t.Body, cls, fd)
			case *MatchStmt:
				for _, c := range t.Cases {
					if c != nil {
						walkBody(c.Body, cls, fd)
					}
				}
			case *FuncDef:
				// A nested def captures `self` by name; its writes are writes.
				walkBody(t.Body, cls, fd)
			case *ClassDef:
				// A nested class is its own world; its `self` is not this one.
			}
		}
	}
	for _, cd := range out.classes {
		for _, st := range cd.Body {
			if m, ok := st.(*FuncDef); ok && m != nil {
				walkBody(m.Body, cd.Name, m)
			}
		}
	}
	for _, st := range prog.Stmts {
		// Module-level and function-level writes to `x.f` are invisible as to which object x is.
		switch t := st.(type) {
		case *AssignStmt:
			if at, ok := t.Target.(*Attr); ok && at != nil && at.Name != nil {
				noteWrite(at.Name.Value, false)
			}
		case *AugAssignStmt:
			if at, ok := t.Target.(*Attr); ok && at != nil && at.Name != nil {
				noteWrite(at.Name.Value, false)
			}
		case *FuncDef:
			walkBody(t.Body, "", nil)
		}
	}
	for cls := range out.classes {
		for f := range seen[cls] {
			if bad[cls][f] {
				continue
			}
			if out.fields[cls] == nil {
				out.fields[cls] = map[string]bool{}
			}
			out.fields[cls][f] = true
		}
	}
	return out
}

// fieldIsVerdict answers `self.<field>` of a class: a verdict only when every write of that name
// anywhere hands one.
func (c methodCensusT) fieldIsVerdict(class, field string) bool {
	if class == "" || field == "" {
		return false
	}
	return c.fields[class][field]
}

// methodAnswer is the codegen-side question: which method does this call dispatch to. `self` is the
// one receiver the AST cannot place on its own — inside a method body it names the class being
// lowered, which only the generator knows — so the generator's own context fills it in.
func (g *irGen) methodAnswer(recv Expr, name string) (*FuncDef, string) {
	if nm, ok := recv.(*Name); ok && nm != nil && nm.Value == "self" && g.curClassSrc != "" {
		return g.methodCensus.lookupClass(g.curClassSrc, name), g.curClassSrc
	}
	return g.methodCensus.lookup(recv, name)
}

// selfAnswer is the same question of a class the caller already knows — the class a body belongs to,
// which is what makes `self.helper()` inside it readable (roadmap Gap R.316).
func (g *irGen) selfAnswer(class, name string) *FuncDef {
	return g.methodCensus.lookupClass(class, name)
}

// fieldAnswer is the codegen-side question of a field: `self.flag` of the class the body belongs to.
func (g *irGen) fieldAnswer(class, field string) bool {
	return g.methodCensus.fieldIsVerdict(class, field)
}
