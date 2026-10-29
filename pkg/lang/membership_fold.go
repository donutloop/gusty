package lang

import "strings"

// The membership question asked of a value the source says has no members.
//
// roadmap Gap R.521 · ADR 0452
//
// The runtime already answers this correctly. `xs = [1]` / `print(1 in xs[0])` ends at exit 3 with CPython's
// own `TypeError: argument of type 'int' is not iterable`, because there the haystack's kind is a run-time fact
// and the object is asked about it: the tagged membership road (heapargs.go) walks the kinds below and raises
// `notIterableSentence` for each, and a kind it cannot name is told it does not contain things at all. What the
// compiled leg got wrong was the operand the front end can FOLD: a spelled literal on the right of `in` never
// reaches a tag, it reaches `rt_contains(i32 5, i32 1)`, and the runtime's untagged scan answers 0 — so
// `print(1 in 5)` printed `False` at the exit code that means the program ran, where the reference raises.
// `print(1 in None)`, `print("a" in 1.5)`, `print(1 in True)` and the same question as a chain link
// (`print(1 == 1 in 5)`) answered `False` too.
//
// A fold may substitute a verdict only when the verdict IS the answer. **ADR 0266** and **ADR 0329** are the
// house's standing rule for the shape: when the compiler has the kind in hand and the kind has no members, the
// answer is not `False`, it is the raise — and the raise goes through the SAME non-local road the run-time door
// uses (`exnCode`/`raiseTo`), so the program's own `except TypeError:` reaches it. This file states the gate and
// the ONE table of kinds; the sentence comes from the door that already writes it (`notIterableSentence`,
// **ADR 0271**'s one-trap-one-wording rule), and the tagged road now walks this table too, so the compile-time
// raise and the run-time raise cannot name two different sets of values.

// notIterableKinds is the four kinds that have no members, keys, elements or characters, in the order the
// tagged membership door tests their tags. It is the same list the run-time road used to spell inline; the
// fold over a literal and the run-time question about a tag are one rule about one set of values, so they read
// one table (**ADR 0232**'s one question, and `abs_kind_test.go`'s rule that doors name an operand one way).
var notIterableKinds = []struct {
	Tag  int32
	Name string
}{
	{int32(TagInt), "int"},
	{int32(TagFloat), "float"},
	{int32(TagBool), "bool"},
	{int32(TagNone), "NoneType"},
}

// notIterableHaystackKind asks whether a membership haystack is a value whose kind the PROGRAM spells, so that
// no run-time question can change the answer. The second result is false for every shape whose kind the run
// time owns — a name (which some other statement binds, possibly to a container), a call, an attribute, a
// subscript, a container literal — and for a text, whose membership is substring membership and is answered.
// Those all keep the roads that ask the object.
func (g *irGen) notIterableHaystackKind(e Expr) (string, bool) {
	var tag int32
	switch e.(type) {
	case *IntLit:
		tag = int32(TagInt)
	case *FloatLit:
		tag = int32(TagFloat)
	case *BoolLit:
		tag = int32(TagBool)
	case *NoneLit:
		tag = int32(TagNone)
	default:
		return "", false
	}
	for _, k := range notIterableKinds {
		if k.Tag == tag {
			return k.Name, true
		}
	}
	return "", false
}

// membershipOfAValueWithNoMembers lowers `x in HAY` / `x not in HAY` where HAY is a literal of a kind the
// reference refuses membership for, and returns the second result false for every other shape so the caller
// falls through to the roads that answer them. The caller has already lowered BOTH operands, so an effect the
// needle carries is in the program before the raise — which is the order the reference uses too. The raise is
// the non-local kind and its block has no edge into whatever follows, so the word handed back here is never
// read: the same convention the instance arm of this door already answers with (codegen_binop.go).
func (g *irGen) membershipOfAValueWithNoMembers(b *strings.Builder, n *BinOp) (string, bool, error) {
	kind, ok := g.notIterableHaystackKind(n.R)
	if !ok {
		return "", false, nil
	}
	g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableSentence(kind), n.Span())
	return "0", true, nil
}
