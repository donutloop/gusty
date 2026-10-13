package lang

import (
	"fmt"
	"strings"
)

// An index that is not a position.
//
// A list answers `xs[i]` by counting slots and a string answers it by counting characters, so both
// need the index to be a number. A text is not one:
//
//	xs = [1, 2]
//	print(xs["a"])   # TypeError: list indices must be integers or slices, not str
//	print(xs[None])  # TypeError: list indices must be integers or slices, not NoneType
//	print(xs[1.5])   # TypeError: list indices must be integers or slices, not float
//	s  = "ab"
//	print(s["a"])    # TypeError: string indices must be integers, not 'str'
//
// The compiled program answered all four — `1`, `1`, `2` and `b` — at exit 0. A text index is an
// index into `@str_tab`, so `xs["a"]` counted to slot 1 and printed the element sitting there: a
// number the program never wrote, delivered as a correct answer. The same shape on the store road
// (`xs["a"] = 1`) stored into slot 0. A list used as an index was worse than wrong: the list
// literal lowered to a `@.lstN` global reference and landed inside an `icmp slt`, so `llc` rejected
// the module and the toolchain exited 2 — the exit code this repo's own contract reserves for a
// compiler bug (roadmap Gap R.222, ADR 0329).
//
// `normalizeIndex` already stands at the list roads with a bounds check — it asks whether the
// position is in range and raises `IndexError` when it is not — but it was handed an i32 and never
// asked what the i32 meant. This file asks, at the two doors that subscript a container that counts,
// whether the index has a kind a position can have.
//
// The two container families divide the language's key question and each asks its own:
// a dict or a set hashes its key, so the question there is *can this be hashed* (`unhashable_key.go`,
// Gap R.217, ADR 0328); a list or a string walks, so the question here is *is this a position*. The
// classifier is deliberately the same shape for the same reason: both read the kind back from what the
// source wrote, and both stay silent where the kind is only a run-time fact.

// nonIntIndexTypeName answers the reference's name for an index that cannot be a position. ok is
// false when the index is a legal position, and false when the compiler cannot tell — a name whose
// kind is a run-time fact keeps the door it has always gone through rather than being accused.
func (g *irGen) nonIntIndexTypeName(e Expr) (string, bool) {
	switch k := e.(type) {
	case nil:
		return "", false
	case *IntLit, *BoolLit:
		return "", false // bool is an int: xs[True] is xs[1], and the reference agrees
	case *StrLit:
		return "str", true
	case *FloatLit:
		return "float", true
	case *NoneLit:
		return "NoneType", true
	case *ListLit:
		return "list", true
	case *DictLit:
		return "dict", true
	case *SetLit:
		return "set", true
	case *Tuple:
		return "tuple", true
	case *Name:
		// The same maps the print door and the membership door use to name a variable's kind:
		// a name the program last filled with a float is a float index, and one it last
		// filled with a text is a text index. Where none of them knows the name, this stays
		// silent — the bounds check keeps the answer the door always gave.
		switch {
		case g.floatVars[k.Value]:
			return "float", true
		case g.listVars[k.Value]:
			return "list", true
		case g.dictVarHasKind(k.Value):
			return "dict", true
		case g.runtimeSets[k.Value] || g.mixedSets[k.Value]:
			return "set", true
		case g.internedVars[k.Value]:
			return "str", true
		case g.noneVars[k.Value]:
			return "NoneType", true
		}
		return "", false
	default:
		return "", false
	}
}

// badIndexMessage words the two sentences the reference words. They differ: a list names the type
// bare, a string quotes it, and a reader who has seen either sentence knows which container asked.
func badIndexMessage(typeName string, containerIsString bool) string {
	if containerIsString {
		return "string indices must be integers, not '" + typeName + "'"
	}
	return "list indices must be integers or slices, not " + typeName
}

// containerCountsPositions reports whether `name` is a container the language subscripts by counting,
// which decides which of the two sentences the raise gets. A dict or a set is not — its subscript is
// a key, and `Gap R.217` owns that door.
func (g *irGen) containerCountsPositions(name string) (isList, isString bool) {
	if name == "" || g == nil {
		return false, false
	}
	if g.listVars[name] || g.mixedLists[name] {
		return true, false
	}
	if g.internedVars[name] {
		return false, true
	}
	return false, false
}

// badIndexGate asks the position question at a subscript door and raises through the same
// non-local road every other exception takes, so `except TypeError:` catches it and an uncaught one
// exits 3 with the reference's own two lines. It runs BEFORE the index is lowered — which is also
// what keeps a list-valued index from reaching the bounds check as a global reference and taking the
// whole module down with it (the llc rejection Gap R.222 measured). It ends the block it branched
// out of and reopens the continuation, so the door stays a valid module either way.
func (g *irGen) badIndexGate(b *strings.Builder, containerName string, idx Expr, sp Span) bool {
	typeName, bad := g.nonIntIndexTypeName(idx)
	if !bad {
		return false
	}
	_, isString := g.containerCountsPositions(containerName)
	if !isString {
		// A name that is not a known container still gets the list sentence when the door itself
		// is a list door; the caller only reaches here from one.
		isString = g.internedVars[containerName]
	}
	g.heapUsed = true
	g.raiseTo(b, exnCode("TypeError"), "TypeError", badIndexMessage(typeName, isString), sp)
	cont := g.newLabel("bix.cont")
	b.WriteString(fmt.Sprintf("%s:\n", cont))
	return true
}

// badIndexGateAtSubscript is the door form of the gate: it works out which container the subscript
// is asking, and whether that container counts positions at all. A dict or a set is subscripted by
// key, not by position, and is left to Gap R.217's door; a list, and a string, are the ones this
// sentence belongs to. It reports whether it raised, so the caller can return the (unreachable)
// value and let the emission continue in the reopened block.
func (g *irGen) badIndexGateAtSubscript(b *strings.Builder, n *Index) bool {
	if n == nil {
		return false
	}
	name := ""
	if nm, ok := n.Obj.(*Name); ok {
		name = nm.Value
	}
	isList, isString := g.containerCountsPositions(name)
	if !isList && !isString {
		switch n.Obj.(type) {
		case *ListLit:
			isList = true
		case *StrLit:
			isString = true
		default:
			if _, ok := g.stringVal(n.Obj); ok {
				isString = true
			}
		}
	}
	if !isList && !isString {
		return false
	}
	typeName, bad := g.nonIntIndexTypeName(n.Idx)
	if !bad {
		return false
	}
	g.heapUsed = true
	g.raiseTo(b, exnCode("TypeError"), "TypeError", badIndexMessage(typeName, isString), n.Span())
	cont := g.newLabel("bix.cont")
	b.WriteString(fmt.Sprintf("%s:\n", cont))
	return true
}
