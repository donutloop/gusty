package lang

// numeric_slot_door.go is the number half of roadmap L11.1's last clause (ADR 0265): what a slot can
// *do* when the compiler cannot say what the slot holds.
//
// The read itself has been answered by the object since ADR 0241, and the ordering of a slot since
// ADR 0252 — an ordering is a bool whatever arrives, so no kind had to be settled to write it.
// Arithmetic is the shape that still could not be written statically, because the answer's kind —
// `int` for `xs[0][0] + 1`, `float` for `xs[0][0] * 2` when the slot holds `7.5` — is in the object,
// and the object is only built when the program runs.
//
// The door is therefore opened only where the program can be *shown* to keep numbers in the slots the
// read reaches. That is not a hedge: it is what keeps the answer honest. `xs[0] + "a"` is answered by
// CPython with a joined string, `[1] * 2` with a repeated list, and this backend can build neither
// (Gap R.82 owns text and container arithmetic); a door that opened there would raise where the
// reference returns a value. So the pass below walks the program's own store-to-slot sites —
// `append`, `add`, `extend`, `insert`, `d[k] = v`, and every element of every container literal the
// program wrote — and demands a literal, all-numeric kind tree along the chain the read walks. A
// container fed by a call, a name, an input, a comprehension, or by text is not proven, and keeps the
// refusal it has always had. The asymmetry is the usual one (ADR 0166): proving too little refuses a
// working program, and that is the cheaper direction to be wrong.
//
// The blocking half of the gate is keyed by the container the store writes into, not by the program
// (roadmap Gap R.191, ADR 0322). It used to be one bit: `w.append("a")` anywhere closed the door for
// every slot read in the module, including `nested[0][0] * 2` in a container the text never touched —
// a refusal filed against the wrong container, on a program the reference answers. A store now blocks
// the container it writes, and every name that can denote that same object: an alias (`y = xs`), a
// loop variable (`for v in xs`), a comprehension's variable, and a parameter beside the argument that
// reached it (`f(xs)` unifies `xs` with `f`'s parameter). A store the pass cannot attribute to *any*
// name — a write through a subscript (`xs[0].append("a")`), through an attribute (`self.items.add(…)`),
// or into a loop variable whose iterable the pass cannot name — still blocks the whole program, because
// an unattributable write is exactly the case where "which container?" has no answer.

// numericSlotChains is the per-program answer: for each container variable, the literal kind tree of
// what the program can put into it, level by level. ok is false the moment a value with no spelling
// (a call, a name, a comprehension) reaches the container, because then the tree is a guess.
type numericSlotChains struct {
	ok    map[string]bool
	depth map[string][]map[string]bool // name -> kinds reachable at depth 1, 2, ...
	// blocked is the gate's blocking half, keyed by the root of an alias group: a text, or a value this
	// pass cannot see through, reaching a container closes the door for that container and every name
	// that can denote it — and for nothing else (ADR 0265 as re-keyed by ADR 0322).
	blocked map[string]bool
	// alias is the union-find that makes a block travel with the object rather than the name: `y = xs`,
	// `for v in xs` and `f(xs)` each link two spellings of one object, so a store through either
	// blocks both.
	alias map[string]string
	// opaque names a binding whose object this pass cannot point at — a loop variable over an
	// expression it cannot name. A block landing on one of those is a block about an object nobody can
	// find, so it falls back to `blockAll`.
	opaque map[string]bool
	// blockAll is the residue: a store to a slot that no name (and therefore no alias group) describes.
	// It is the old program-wide bit, kept for the shapes that genuinely cannot be attributed.
	blockAll bool
}

// root is the alias group's representative, with path compression so a chain of aliases costs one walk.
func (c *numericSlotChains) root(name string) string {
	if name == "" {
		return ""
	}
	p, seen := c.alias[name]
	if !seen {
		c.alias[name] = name
		return name
	}
	if p == name {
		return name
	}
	r := c.root(p)
	c.alias[name] = r
	return r
}

// unify links two spellings of one object. The representative is chosen by name, not by arrival order,
// so the notebook does not depend on which line the parser walked first.
func (c *numericSlotChains) unify(a, b string) {
	if a == "" || b == "" {
		return
	}
	ra, rb := c.root(a), c.root(b)
	if ra == rb {
		return
	}
	if ra > rb {
		ra, rb = rb, ra
	}
	c.alias[rb] = ra
}

// block closes the door for the container `name` denotes — and, through the alias group, for every name
// that can denote the same object.
func (c *numericSlotChains) block(name string) {
	c.blocked[c.root(name)] = true
}

// blockedUnder is the question the gate asks: is this container — or anything that can reach it —
// written with a value the arithmetic door cannot answer?
func (c *numericSlotChains) blockedUnder(name string) bool {
	return c.blockAll || c.blocked[c.root(name)]
}

// mayHoldTextOrWorse is the blocking property of one stored value: a text anywhere in its literal kind
// tree, or no kind tree at all.
func mayHoldTextOrWorse(e Expr) bool {
	kinds, sub, unsure := literalKindTree(e)
	if unsure {
		return true
	}
	if !storedKindsAreNumeric([]map[string]bool{kinds}) {
		return true
	}
	return !storedKindsAreNumeric(sub)
}

// unattributableStore records a write to a slot this pass cannot say a name about. A numeric value is
// harmless; anything else is the program-wide block, which is the only honest answer to "which
// container?" when the answer is "some object nobody names".
func (c *numericSlotChains) unattributableStore(e Expr) {
	if mayHoldTextOrWorse(e) {
		c.blockAll = true
	}
}

// storedKindsAreNumeric reports the blocking half: a text anywhere in the tree, at any depth, closes the
// door. A container's own kind is not a block — the flagship program stores a list of numbers — but a
// text at any level is, because `+` and `*` are answered by the reference with a joined text or a
// repeated container and this backend can build neither from a slot (Gap R.82).
func storedKindsAreNumeric(levels []map[string]bool) bool {
	for _, lv := range levels {
		for k := range lv {
			switch k {
			case kindInt, kindFloat, kindBool, kindNone, kindList, kindDict, kindSet, kindTuple:
			default:
				return false
			}
		}
	}
	return true
}

func newNumericSlotChains() *numericSlotChains {
	return &numericSlotChains{
		ok:      map[string]bool{},
		depth:   map[string][]map[string]bool{},
		blocked: map[string]bool{},
		alias:   map[string]string{},
		opaque:  map[string]bool{},
	}
}

// scalar kind names used by the tree. These are the tag names the runtime already carries (value.go's
// TagInt..TagTuple), spelled here so the pass does not need a boxed value to ask.
const (
	kindInt    = "int"
	kindFloat  = "float"
	kindBool   = "bool"
	kindNone   = "none"
	kindStr    = "str"
	kindList   = "list"
	kindDict   = "dict"
	kindSet    = "set"
	kindTuple  = "tuple"
	kindUnsure = "?"
)

// compute records one value stored into `name`'s slots, at depth 1, and pushes its own element kinds
// one level deeper. It is called for every store-to-slot site the walk finds.
func (c *numericSlotChains) record(name string, e Expr) {
	if name == "" || e == nil {
		return
	}
	if _, seen := c.ok[name]; !seen {
		c.ok[name] = true
	}
	kinds, sub, unsure := literalKindTree(e)
	if unsure {
		c.ok[name] = false
		// A value with no spelling — a call, a name, an input, a comprehension — is the same unknown the
		// text is, as far as an honest `+` is concerned.
		c.blockStore(name, true)
		return
	}
	c.add(name, 0, kinds)
	for d, ks := range sub {
		c.add(name, d+1, ks)
	}
	if !storedKindsAreNumeric(c.depth[name]) {
		c.blockStore(name, false)
	}
}

// blockStore blocks the container a store wrote, and falls back to the program-wide block when the
// name it was keyed by denotes an object the pass could not point at (`opaque`): the alias group of a
// loop variable over `read_rows()` is a group of one, and the object it stands for lives somewhere the
// notebook cannot follow.
func (c *numericSlotChains) blockStore(name string, unsure bool) {
	c.block(name)
	if c.opaque[name] {
		c.blockAll = true
	}
}

func (c *numericSlotChains) add(name string, d int, kinds map[string]bool) {
	for len(c.depth[name]) <= d {
		c.depth[name] = append(c.depth[name], map[string]bool{})
	}
	for k := range kinds {
		c.depth[name][d][k] = true
	}
}

// numeric reports whether every kind the *deepest* slots of the read hold is a number. `chain` is the
// number of subscripts in the read: xs[0][0] over `xs` is depth 2, and the answer is about the slots
// that final subscript opens. The levels above it must be containers — a step through a list is how the
// read gets there — and an empty level (a subscript the tree has nothing to say about) is not numeric:
// absence is refused, never assumed.
func (c *numericSlotChains) numeric(name string, chain int) bool {
	if name == "" || chain < 1 {
		return false
	}
	if c.blockedUnder(name) {
		return false
	}
	if !c.ok[name] {
		return false
	}
	levels, seen := c.depth[name]
	if !seen || len(levels) < chain {
		return false
	}
	for d := 0; d < chain-1; d++ {
		if len(levels[d]) == 0 {
			return false
		}
		for k := range levels[d] {
			switch k {
			case kindList, kindDict, kindSet, kindTuple:
			default:
				return false
			}
		}
	}
	for k := range levels[chain-1] {
		switch k {
		case kindInt, kindFloat, kindBool:
		default:
			return false
		}
	}
	return true
}

// literalKindTree spells the kinds of a value written as a literal, and returns the kinds of the
// containers one level below it in `sub` (indexed by depth below the recorded one). `unsure` says the
// value has no spelling this pass can read.
func literalKindTree(e Expr) (kinds map[string]bool, sub []map[string]bool, unsure bool) {
	kinds = map[string]bool{}
	switch n := e.(type) {
	case *IntLit:
		kinds[kindInt] = true
	case *FloatLit:
		kinds[kindFloat] = true
	case *BoolLit:
		kinds[kindBool] = true
	case *NoneLit:
		kinds[kindNone] = true
	case *StrLit, *FString:
		kinds[kindStr] = true
	case *ListLit:
		if len(n.Elems) == 0 {
			// An empty literal says nothing about what the slots hold; the door is opened by what a later
			// `append` writes, not by the `[]` the name was bound to.
			return nil, nil, true
		}
		here, below, un := containerLevels(n.Elems)
		if un {
			return nil, nil, true
		}
		return map[string]bool{kindList: true}, append([]map[string]bool{here}, below...), false
	case *Tuple:
		if len(n.Elems) == 0 {
			return nil, nil, true
		}
		here, below, un := containerLevels(n.Elems)
		if un {
			return nil, nil, true
		}
		return map[string]bool{kindTuple: true}, append([]map[string]bool{here}, below...), false
	case *SetLit:
		if len(n.Elems) == 0 {
			return nil, nil, true
		}
		here, below, un := containerLevels(n.Elems)
		if un {
			return nil, nil, true
		}
		return map[string]bool{kindSet: true}, append([]map[string]bool{here}, below...), false
	case *DictLit:
		if len(n.Vals) == 0 {
			return nil, nil, true
		}
		// A dict slot holds its value; the key is text or a number the numeric road never reads.
		here, below, un := containerLevels(n.Vals)
		if un {
			return nil, nil, true
		}
		return map[string]bool{kindDict: true}, append([]map[string]bool{here}, below...), false
	case *UnOp:
		if n.Op != "-" {
			return nil, nil, true
		}
		return literalKindTree(n.X)
	case *BinOp:
		// The literal spellings CPython accepts at the top of an expression, `-7.5` above among them.
		lk, lsub, luns := literalKindTree(n.L)
		rk, rsub, runs := literalKindTree(n.R)
		if luns || runs {
			return nil, nil, true
		}
		return unionKinds(lk, rk), append(lsub, rsub...), false
	default:
		return nil, nil, true
	}
	return kinds, nil, false
}

// containerLevels is the one-level-down view of a container literal's elements.
func containerLevels(elems []Expr) (map[string]bool, []map[string]bool, bool) {
	here := map[string]bool{}
	var below []map[string]bool
	for _, el := range elems {
		k, sub, un := literalKindTree(el)
		if un {
			return nil, nil, true
		}
		for kk := range k {
			here[kk] = true
		}
		below = append(below, sub...)
	}
	return here, below, false
}

func unionKinds(a, b map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// computeNumericSlotChains walks the whole program once and records every store into a container slot.
// Function bodies are walked too: a helper that appends a text into the same list is the same fact
// about that list, and the door has to know it before it opens.
// computeNumericSlotChains walks the whole program once and records every store into a container slot.
// Function bodies are walked too: a helper that appends a text into the same list is the same fact
// about that list, and the door has to know it before it opens.
//
// The walk also builds the alias closure the block travels along (ADR 0322): every binding that makes
// two names one object — `y = xs`, `for v in xs`, `[v for v in xs]`, `f(xs)` over `f`'s parameter, and
// `d = xs[0]` — links them in `alias`, so a text written through any spelling blocks every one of them.
// A binding whose object the pass cannot point at (`xs = read_rows()`, `xs = obj.items`, `for v in
// rows()`) marks its name opaque, which sends a block landing on it back to the program-wide answer:
// there is no group to carry it, because there is no name for the object it belongs to.
func computeNumericSlotChains(prog *Program) *numericSlotChains {
	c := newNumericSlotChains()
	if prog == nil {
		return c
	}
	params := declaredParameters(prog.Stmts)
	var walkStmt func([]Stmt)
	var walkExpr func(Expr)

	// nameable asks whether an iterable names the container its elements come out of. `for v in xs`
	// can say so; `for v in rows()` and `for v in xs[0]` cannot, and a store through such a loop
	// variable writes an object with no name in the notebook.
	nameable := func(e Expr) (string, bool) {
		switch n := e.(type) {
		case *Name:
			return n.Value, true
		case *ListLit, *SetLit, *DictLit, *Tuple:
			return "", true
		}
		return "", false
	}
	binds := func(target, iter Expr) {
		for _, vn := range loopVarNames(target) {
			nm, ok := nameable(iter)
			if !ok {
				c.opaque[vn] = true
				continue
			}
			if nm != "" {
				c.unify(vn, nm)
			}
		}
	}
	walkExpr = func(e Expr) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *Call:
			if sel, ok := n.Fn.(*Attr); ok && sel.Name != nil && containerMutators[sel.Name.Value] {
				if recv, ok := sel.Obj.(*Name); ok {
					for _, a := range n.Args {
						c.record(recv.Value, a)
					}
				} else {
					// `xs[0].append("a")`, `self.items.add(b)`: a real write to a real container that no
					// name in the notebook describes.
					for _, a := range n.Args {
						c.unattributableStore(a)
					}
				}
			}
			if id, ok := n.Fn.(*Name); ok {
				if ps, isFunc := params[id.Value]; isFunc {
					c.bindCallSite(ps, n.Args)
				} else if !isReadOnlyBuiltin(id.Value) {
					// A callee this pass cannot see could append to anything it was handed.
					for _, a := range n.Args {
						if an, isName := callArgName(a); isName {
							c.block(an.Value)
						}
					}
				}
			}
			for _, a := range n.Args {
				walkExpr(a)
			}
		case *BinOp:
			walkExpr(n.L)
			walkExpr(n.R)
		case *UnOp:
			walkExpr(n.X)
		case *Index:
			walkExpr(n.Obj)
			walkExpr(n.Idx)
		case *Slice:
			walkExpr(n.Obj)
		case *Attr:
			walkExpr(n.Obj)
		case *CondExpr:
			walkExpr(n.If)
			walkExpr(n.Cond)
			walkExpr(n.Else)
		case *AssignExpr:
			walkExpr(n.Value)
			if n.Name != nil {
				c.bindValue(n.Name.Value, n.Value)
			}
		case *Lambda:
			walkExpr(n.Body)
		case *Comp:
			for _, el := range n.Elems {
				walkExpr(el)
			}
			for i := range n.Keys {
				walkExpr(n.Keys[i])
				walkExpr(n.Vals[i])
			}
			// Each clause binds its own name FROM its own iterable — the fact this scan exists to know —
			// and a scan that read only the first left the second clause's target unclassified
			// (roadmap Gap R.352).
			for _, cl := range n.compClauses() {
				if cl.Target != nil {
					binds(cl.Target, cl.Iter)
				}
				walkExpr(cl.Iter)
				walkExpr(cl.Cond)
			}
		case *Generator:
			for _, el := range n.Elems {
				walkExpr(el)
			}
			walkExpr(n.Cond)
			if n.ForVar != nil {
				binds(n.ForVar, n.Iter)
			}
			walkExpr(n.Iter)
		case *ListLit:
			for _, el := range n.Elems {
				walkExpr(el)
			}
		case *Tuple:
			for _, el := range n.Elems {
				walkExpr(el)
			}
		case *SetLit:
			for _, el := range n.Elems {
				walkExpr(el)
			}
		case *DictLit:
			for i := range n.Keys {
				walkExpr(n.Keys[i])
				walkExpr(n.Vals[i])
			}
		}
	}
	walkStmt = func(ss []Stmt) {
		for _, s := range ss {
			switch n := s.(type) {
			case *AssignStmt:
				// xs[0] = v, d["k"] = v: the target's container is the name being written through.
				if ix, ok := n.Target.(*Index); ok {
					nm, depth := chainDepth(ix)
					if nm != nil {
						if depth == 1 {
							c.record(nm.Value, n.Value)
						} else if mayHoldTextOrWorse(n.Value) {
							// xs[0][1] = "a" writes a slot two levels down: the notebook has no level for
							// it, so no read may claim the slots below xs are all numbers (Gap R.82's shape,
							// which the program-wide block used to miss entirely).
							c.block(nm.Value)
							c.blockAll = true
						}
					} else {
						c.unattributableStore(n.Value)
					}
				} else if tn, ok := n.Target.(*Name); ok {
					c.bindValue(tn.Value, n.Value)
				} else if tt, ok := n.Target.(*Tuple); ok {
					c.bindTuple(tt, n.Value)
				}
				walkExpr(n.Value)
			case *AugAssignStmt:
				if ix, ok := n.Target.(*Index); ok {
					if nm, ok := ix.Obj.(*Name); ok {
						c.record(nm.Value, n.Value)
					}
				} else if tn, ok := n.Target.(*Name); ok {
					// xs += ["a"] extends the container without naming it as a receiver.
					c.record(tn.Value, n.Value)
				}
				walkExpr(n.Value)
			case *ExprStmt:
				walkExpr(n.Expr)
			case *IfStmt:
				walkExpr(n.Cond)
				walkStmt(n.Then)
				for _, e := range n.Elifs {
					walkStmt([]Stmt{e})
				}
				walkStmt(n.Else)
			case *WhileStmt:
				walkExpr(n.Cond)
				walkStmt(n.Body)
				walkStmt(n.Else)
			case *ForStmt:
				walkExpr(n.Iter)
				binds(n.Var, n.Iter)
				walkStmt(n.Body)
				walkStmt(n.Else)
			case *TryStmt:
				walkStmt(n.Body)
				for _, h := range n.Excepts {
					walkStmt(h.Body)
				}
				walkStmt(n.Finally)
			case *WithStmt:
				walkExpr(n.Expr)
				walkStmt(n.Body)
			case *MatchStmt:
				walkExpr(n.Subject)
				for _, cl := range n.Cases {
					walkStmt(cl.Body)
				}
			case *FuncDef:
				for _, d := range n.Decorators {
					walkExpr(d)
				}
				walkStmt(n.Body)
			case *ClassDef:
				walkStmt(n.Body)
			case *ReturnStmt:
				walkExpr(n.Expr)
			case *RaiseStmt:
				walkExpr(n.Expr)
			case *YieldStmt:
				walkExpr(n.Expr)
			case *YieldFromStmt:
				walkExpr(n.Expr)
			}
		}
	}
	walkStmt(prog.Stmts)
	return c
}

// bindValue records what a plain `x = value` line says about the container x may hold: it links the two
// names when the line makes one object of them (`y = xs`, `d = xs[0]`), and marks x opaque when the
// value names no object the notebook can point at (`xs = rows()`, `xs = obj.items`) so that a later
// store through x blocks the program rather than a group that does not describe the object.
func (c *numericSlotChains) bindValue(target string, value Expr) {
	switch v := value.(type) {
	case *Name:
		c.unify(target, v.Value)
	case *Index:
		if nm, _ := chainDepth(v); nm != nil {
			c.unify(target, nm.Value)
		}
	case *Call, *Attr, *Comp, *Generator, *Slice:
		c.opaque[target] = true
	}
}

// bindTuple links an unpacking target to the object it unpacks: `a, b = xs` binds two names to parts of
// the one container, so a store through either is a store the other's reads must not call numeric.
func (c *numericSlotChains) bindTuple(target *Tuple, value Expr) {
	if vn, ok := value.(*Name); ok {
		for _, e := range target.Elems {
			if n, isName := e.(*Name); isName {
				c.unify(n.Value, vn.Value)
			}
		}
		return
	}
	vt, isTuple := value.(*Tuple)
	if !isTuple {
		return
	}
	for i, e := range target.Elems {
		if i >= len(vt.Elems) {
			break
		}
		tn, isName := e.(*Name)
		vn, isName2 := vt.Elems[i].(*Name)
		if isName && isName2 {
			c.unify(tn.Value, vn.Value)
		}
	}
}

// bindCallSite links a call's arguments to the parameters they will be bound to, so a helper that
// appends a text through its parameter blocks the container every call site handed it.
func (c *numericSlotChains) bindCallSite(params []string, args []Expr) {
	for i, a := range args {
		an, ok := callArgName(a)
		if !ok {
			continue
		}
		if kw, isKw := a.(*KeywordArg); isKw {
			for j, p := range params {
				if p == kw.Name && j < len(args) {
					// the positional slot is irrelevant; the name is what binds
					c.unify(p, an.Value)
					break
				}
			}
			continue
		}
		if i < len(params) {
			c.unify(params[i], an.Value)
		}
	}
}

// callArgName is the name an argument hands over, through a keyword spelling if it came as one.
func callArgName(a Expr) (*Name, bool) {
	switch n := a.(type) {
	case *Name:
		return n, true
	case *KeywordArg:
		if nm, ok := n.Value.(*Name); ok {
			return nm, true
		}
	}
	return nil, false
}

// declaredParameters collects every function's parameter names, so a call site can say which of its
// arguments becomes which parameter. Nested and conditional definitions are in it: the door cannot
// know which of them runs.
func declaredParameters(ss []Stmt) map[string][]string {
	out := map[string][]string{}
	var walk func([]Stmt)
	collect := func(fd *FuncDef) {
		if fd == nil {
			return
		}
		names := make([]string, 0, len(fd.Params))
		for _, p := range fd.Params {
			if p != nil {
				names = append(names, p.Name)
			}
		}
		out[fd.Name] = names
	}
	walk = func(ss []Stmt) {
		for _, s := range ss {
			switch n := s.(type) {
			case *FuncDef:
				collect(n)
				walk(n.Body)
			case *ClassDef:
				walk(n.Body)
			case *IfStmt:
				walk(n.Then)
				for _, e := range n.Elifs {
					walk([]Stmt{e})
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
				walk(n.Else)
			case *ForStmt:
				walk(n.Body)
				walk(n.Else)
			case *TryStmt:
				walk(n.Body)
				for _, h := range n.Excepts {
					walk(h.Body)
				}
				walk(n.Finally)
			case *WithStmt:
				walk(n.Body)
			case *MatchStmt:
				for _, cl := range n.Cases {
					walk(cl.Body)
				}
			}
		}
	}
	walk(ss)
	return out
}

// chainDepth counts the subscripts in a read: xs, xs[0] and xs[0][0] are 0, 1 and 2, and the root name
// the chain walks out of is what the notebook is keyed by.
func chainDepth(e Expr) (*Name, int) {
	d := 0
	for {
		ix, ok := e.(*Index)
		if !ok {
			break
		}
		d++
		e = ix.Obj
	}
	nm, ok := e.(*Name)
	if !ok {
		return nil, d
	}
	return nm, d
}
