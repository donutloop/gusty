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

// numericSlotChains is the per-program answer: for each container variable, the literal kind tree of
// what the program can put into it, level by level. ok is false the moment a value with no spelling
// (a call, a name, a comprehension) reaches the container, because then the tree is a guess.
type numericSlotChains struct {
	ok    map[string]bool
	depth map[string][]map[string]bool // name -> kinds reachable at depth 1, 2, ...
	// blocked is the program-wide half of the gate: once any container anywhere can receive a text, or a
	// value this pass cannot see through, `+` and `*` on a slot read are refused for the whole program.
	// Coarse on purpose — saying which slot a read reaches is the notebook's job, and it does not go that
	// deep yet — and coarse here means refusing more often, never answering wrongly (ADR 0265).
	blocked bool
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
	return &numericSlotChains{ok: map[string]bool{}, depth: map[string][]map[string]bool{}}
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
		c.blocked = true
		return
	}
	c.add(name, 0, kinds)
	for d, ks := range sub {
		c.add(name, d+1, ks)
	}
	if !storedKindsAreNumeric(c.depth[name]) {
		c.blocked = true
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
func computeNumericSlotChains(prog *Program) *numericSlotChains {
	c := newNumericSlotChains()
	var walkStmt func([]Stmt)
	var walkExpr func(Expr)
	walkExpr = func(e Expr) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *Call:
			if sel, ok := n.Fn.(*Attr); ok && sel.Name != nil {
				if recv, ok := sel.Obj.(*Name); ok {
					switch sel.Name.Value {
					case "append", "add", "extend", "insert", "update":
						for _, a := range n.Args {
							c.record(recv.Value, a)
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
		case *Attr:
			walkExpr(n.Obj)
		case *CondExpr:
			walkExpr(n.If)
			walkExpr(n.Cond)
			walkExpr(n.Else)
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
					if nm, ok := ix.Obj.(*Name); ok {
						c.record(nm.Value, n.Value)
					}
				}
				walkExpr(n.Value)
			case *AugAssignStmt:
				if ix, ok := n.Target.(*Index); ok {
					if nm, ok := ix.Obj.(*Name); ok {
						c.record(nm.Value, n.Value)
					}
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
				walkStmt(n.Body)
				walkStmt(n.Else)
			case *TryStmt:
				walkStmt(n.Body)
				for _, h := range n.Excepts {
					walkStmt(h.Body)
				}
				walkStmt(n.Finally)
			case *FuncDef:
				walkStmt(n.Body)
			case *ReturnStmt:
				walkExpr(n.Expr)
			case *RaiseStmt:
				walkExpr(n.Expr)
			}
		}
	}
	walkStmt(prog.Stmts)
	return c
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
