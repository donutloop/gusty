package lang

import (
	"fmt"
	"strings"
)

// An ordering between two kinds that do not order against each other.
//
// `<`, `<=`, `>` and `>=` are not total: a value answers them only when its type defines an
// ordering, and two types order against each other only when they share one. `None` orders against
// nothing — not against a number, not against a text, not even against itself — and a text never
// orders against a number. CPython stops every one of those with
//
//	TypeError: '<' not supported between instances of 'int' and 'NoneType'
//
// and the compiled backend answered all of them. That is possible because of how a value is
// carried: `None` is the void, whose word is 0, and a text is an index into `@str_tab`. Both are
// i32s, so the comparison road — which asks nothing but `icmp`/`fcmp` over the two words it was
// handed — was never told there was a question:
//
//	print(1 < None)      # 0 → `icmp slt i32 1, 0` → False at exit 0
//	print(None < 1)      # 0 → `icmp slt i32 0, 1` → True  at exit 0
//	print(None <= None)  # 0 → `icmp sle i32 0, 0` → True  at exit 0
//	print("a" < 1.5)     # @str_tab index → `sitofp` → `fcmp olt` → True at exit 0
//	print(True < [1])    # the right class, and a sentence naming `'int' and 'list'` for a verdict
//
// Thirty-seven of these sat in the priority-1 class this repository files rather than ships — a wrong
// value delivered at the code that means the program ran (ADR 0273), with no traceback, no refusal
// and nothing raised. Three of those thirty-seven sat with the right class and a sentence that named
// a type the program never mentioned (Gap R.202's `True < [1]` saying `'int'`).
//
// The door here asks the question the comparison road never asked: *what kind is each side, and do
// those two kinds order against each other?* It is asked BEFORE either operand is lowered, for the
// reason every other door in this file gives — lowering a text is an intern-table lookup and
// lowering `None` is a zero, and after that the two facts this door needs are gone (ADR 0329's
// index gate, ADR 0336's bound gate, ADR 0275's short-circuit doors).
//
// The three rulings, in the order the file below states them:
//
//   - a pair whose kinds share a family answers, exactly as it did — two numbers, two texts, two
//     containers of one kind. Nothing that compiles today is costed a program (ADR 0273's ladder),
//     which is what the "same family" half of the rule is for;
//   - a pair whose kinds are both visible and have no ordering raises CPython's own sentence, with
//     both type names read from the same table the printers read, through the same non-local road
//     every other compiler-detected trap takes, so `except TypeError:` reaches it and an uncaught
//     one leaves at exit 3 (ADR 0214);
//   - a side whose kind the compiler cannot see — a parameter, a slot read, a call this pass cannot
//     describe — is not an unknown kind, it is an unknown, and the door stays silent rather than
//     guessing. The tagged-slot door is the one that asks the object, and a gate that fired at an
//     operand it could not name would turn `0 < xs[1] < 3` into a raise.

// orderFamily groups the kinds the reference lets order against each other. Two operands in one
// family order (the road answers); two in different families do not (the door raises).
type orderFamily int

const (
	orderUnknown orderFamily = iota
	orderNumber
	orderText
	orderNone
	orderContainer
)

func orderFamilyOf(kind string) orderFamily {
	switch kind {
	case "int", "float", "bool":
		return orderNumber
	case "str":
		return orderText
	case "NoneType":
		return orderNone
	case "list", "dict", "set", "tuple":
		return orderContainer
	}
	return orderUnknown
}

// orderKindOf answers the reference's name for the kind of one operand of an ordering operator, and
// whether the compiler can see that kind at all. known is false for everything whose kind is a
// run-time fact — a parameter, a subscript, an attribute, a call this pass cannot describe — and the
// door reads that as "not my question", never as "no ordering".
//
// The names come from the same records the print door, the negation door (ADR 0266) and the index
// door (ADR 0329) read, on purpose: three questions about one operand — what does it print as, what
// has no sign, can it be a position — that disagree about its kind are two bugs waiting to be
// measured, and `abs_kind_test.go` already fails when two doors name one operand differently.
func (g *irGen) orderKindOf(e Expr) (kind string, known bool) {
	if g == nil || e == nil {
		return "", false
	}
	switch n := e.(type) {
	case *IntLit:
		return "int", true
	case *FloatLit:
		return "float", true
	case *BoolLit:
		return "bool", true
	case *StrLit:
		return "str", true
	case *NoneLit:
		return "NoneType", true
	case *ListLit:
		// The tuple question first, asked of the predicate that owns it: a tuple is the list's
		// layout with the immutable bit, and the reference says `'tuple'` (roadmap L11.3, ADR 0361).
		if g.isTupleExpr(n) {
			return "tuple", true
		}
		return "list", true
	case *Tuple:
		return "tuple", true
	case *DictLit:
		return "dict", true
	case *SetLit:
		return "set", true
	case *UnOp:
		// `-1` is still an int, `-(1.5)` still a float: the sign does not change the family, and the
		// negation of anything that is not a number is ADR 0266's door, asked before this one.
		if n.Op == "-" || n.Op == "+" {
			if k, ok := g.orderKindOf(n.X); ok && orderFamilyOf(k) == orderNumber {
				return k, true
			}
		}
		return "", false
	case *BinOp:
		// An arithmetic expression over two numbers is a number: `1 + 1 < None` is a question about
		// an int and a void whatever the folder decides to do about it. Only the arithmetic operators
		// qualify, and only when BOTH sides are numbers — `[1] + [2]` is a list and `"a" + "b"` a text,
		// each owned by the door that builds it.
		switch n.Op {
		case "+", "-", "*", "/", "//", "%", "**":
			lk, lok := g.orderKindOf(n.L)
			rk, rok := g.orderKindOf(n.R)
			if lok && rok && orderFamilyOf(lk) == orderNumber && orderFamilyOf(rk) == orderNumber {
				if lk == "float" || rk == "float" {
					return "float", true
				}
				return "int", true
			}
			// `+` between two texts is the concatenation, and ADR 0365's door builds it as one @str_tab
			// index: `"a" + "b" < None` is the same cross-family question the two literals are.
			if n.Op == "+" && lok && rok && lk == "str" && rk == "str" {
				return "str", true
			}
			// `*` between a text and a whole number is the repeat, and answers a text either way round.
			if n.Op == "*" && lok && rok {
				if (lk == "str" && rk == "int") || (lk == "int" && rk == "str") {
					return "str", true
				}
			}
		}
		return "", false
	case *Name:
		return g.orderKindOfName(n.Value)
	case *Call:
		// A call the module knows answers a text, a call the float road recognises answers a double,
		// and a body whose every road answers the void answers None (ADR 0368's answer direction, and
		// the same AST question the print road asks). Anything else is a run-time fact, and a run-time
		// fact is not this door's question.
		if g.callReturnsStr(n) || g.callReturnsOnlyStr(n) {
			return "str", true
		}
		if g.isFloat(n) {
			return "float", true
		}
		if nm, ok := n.Fn.(*Name); ok {
			if k, known := g.orderKindOfBuiltin(n, nm.Value); known {
				return k, true
			}
			if g.answer != nil && g.answer.answersOnlyTheVoid(nm.Value) {
				return "NoneType", true
			}
			if fd, ok := g.fds[nm.Value]; ok && callAnswersOnlyTheVoid(fd) {
				return "NoneType", true
			}
		}
		return "", false
	}
	return "", false
}

// orderKindOfBuiltin names the answer of the builtins whose return kind is a fact about the builtin
// rather than about the program: `len` counts, `int` truncates, `str` renders. A builtin whose answer
// takes its kind from an argument — `min`, `max`, `abs`, `sum` — is not named here; those have doors of
// their own (`pairfold.go`, `negation.go`) that ask the operand rather than the callee.
func (g *irGen) orderKindOfBuiltin(n *Call, name string) (string, bool) {
	if name == "abs" && len(n.Args) == 1 {
		if g.builtinCallAs(n, "abs") {
			// `abs` answers a double for a double and an int otherwise — the question ADR 0266's door asks,
			// asked of the same operand table this door reads.
			if k, ok := g.orderKindOf(n.Args[0]); ok && orderFamilyOf(k) == orderNumber {
				if k == "float" {
					return "float", true
				}
				return "int", true
			}
		}
		return "", false
	}
	one := func(builtin, kind string) (string, bool) {
		if len(n.Args) == 1 && g.builtinCallAs(n, builtin) {
			return kind, true
		}
		return "", false
	}
	for _, b := range []struct {
		name, kind string
	}{
		{"len", "int"}, {"ord", "int"}, {"int", "int"},
		{"float", "float"},
		{"str", "str"}, {"repr", "str"}, {"chr", "str"},
	} {
		if k, known := one(b.name, b.kind); known {
			return k, true
		}
	}
	return "", false
}

// callAnswersOnlyTheVoid asks the AST the question the answer direction also asks, but of the body
// rather than of the table: every `return` road is bare or returns `None` literal-ly, and the body is
// not a generator. The direction's own verdict is not enough here — a body served by the pair road is
// skipped by its scan (ADR 0273's rule that a table entry is the pair road's), and the print road has
// always read the tag that road stores. This is the read-side question the print door asks, asked one
// operator out (roadmap Gap R.228's void, seen from an ordering).
func callAnswersOnlyTheVoid(fd *FuncDef) bool {
	if fd == nil || fd.Async || len(fd.Decorators) > 0 || containsYield(fd.Body) {
		return false
	}
	returns, _ := answerBodyReturns(fd.Body)
	for _, r := range returns {
		if r.Expr == nil {
			continue // a bare `return` answers the void
		}
		if _, isNone := r.Expr.(*NoneLit); !isNone {
			return false
		}
	}
	return true
}

// orderKindOfName reads a variable's kind out of the binding records — the latest-binding tables
// ADR 0172 keeps, the same ones `nonIntIndexTypeName` and `valueWithNoSign` consult. The container
// claims come first for the reason ADR 0341 gave: a name whose slot the module built as a container
// is a container whatever a stale scalar record says about it.
func (g *irGen) orderKindOfName(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if g.listVars[name] || g.mixedLists[name] {
		return "list", true
	}
	if g.runtimeDicts[name] || g.mixedDicts[name] || g.dictVarHasKind(name) {
		return "dict", true
	}
	if g.runtimeSets[name] || g.mixedSets[name] {
		return "set", true
	}
	if g.tupleVars[name] {
		return "tuple", true
	}
	if _, isText := g.strVals[name]; isText || g.internedVars[name] {
		return "str", true
	}
	if g.noneVars[name] {
		return "NoneType", true
	}
	if g.boolVars[name] {
		return "bool", true
	}
	if g.floatVars[name] {
		return "float", true
	}
	return "", false
}

// orderPairHasNoOrdering is the whole language question: given the reference's name for each side,
// is there any pair of values of those two kinds that the reference orders? False — the door stays
// out — when the two kinds share a family, when either name is not one the table knows, or when both
// sides are containers: the ordering of two containers is the tagged door's question and it ANSWERS
// `a = [1]` / `b = [2]` / `print(a < b)`, so a gate that claimed that pair would trade an answer for
// a raise, which the ladder forbids.
func orderPairHasNoOrdering(lk, rk string) bool {
	lf, rf := orderFamilyOf(lk), orderFamilyOf(rk)
	if lf == orderUnknown || rf == orderUnknown {
		return false
	}
	// `None` has no ordering against anything, including itself.
	if lf == orderNone || rf == orderNone {
		return true
	}
	if lf == orderContainer && rf == orderContainer {
		return false
	}
	return lf != rf
}

// unsupportedOrderMessage words the one sentence the reference words for this trap. The two type
// names travel in SOURCE order — left then right — because CPython names the operand whose
// `__lt__` was tried first, and swapping them would describe a program the author did not write
// (ADR 0215: the reference's wording is the wording).
func unsupportedOrderMessage(op, leftKind, rightKind string) (string, string) {
	return "TypeError", fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, leftKind, rightKind)
}

// crossKindOrderGate stands at the comparison road and raises what the reference raises for a pair
// of kinds that do not order. It returns true when it raised, having ended the block it stood in and
// reopened a continuation, so the caller returns a value word that is never read and emission
// continues in a valid block (the shape `badIndexGate` and `badSliceBoundGate` use).
//
// It is asked before either operand is lowered and before the float road, the tagged road and the
// constant folder can see the expression: those roads lower a text to its interned index and the
// void to 0, and an ordering over the two words they were handed has nothing left to ask.
func (g *irGen) crossKindOrderGate(b *strings.Builder, n *BinOp) bool {
	if n == nil {
		return false
	}
	switch n.Op {
	case "<", "<=", ">", ">=":
	default:
		return false
	}
	lk, lok := g.orderKindOf(n.L)
	rk, rok := g.orderKindOf(n.R)
	if !lok || !rok || !orderPairHasNoOrdering(lk, rk) {
		return false
	}
	// A container operand that the tagged order door ANSWERS is not this door's question even when
	// the families differ: an ordering of a list against a number is CPython's TypeError either way,
	// but an ordering the door below answers is not something to overwrite.
	if orderFamilyOf(lk) == orderContainer && orderFamilyOf(rk) == orderContainer {
		return false
	}
	class, msg := unsupportedOrderMessage(n.Op, lk, rk)
	g.heapUsed = true
	g.raiseTo(b, exnCode(class), class, msg, n.Span())
	cont := g.newLabel("ord.cont")
	b.WriteString(fmt.Sprintf("%s:\n", cont))
	return true
}
