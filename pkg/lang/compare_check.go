// compare_check.go — the CHECKER'S half of a comparison chain (roadmap L12.1 / Gap R.53, ADR 0450).
//
// The parser has answered `a < b < c` as ONE program — a `ChainCompare` over three operands and two
// operators — since ADR 0288, and codegen has lowered it with each operand stored once since the same
// change. What was never done is the one thing that makes the node visible to the front end: nobody
// told the ANALYZER the node exists. `inferExprTy` had no arm for `*ChainCompare`, so the walk stopped
// at the chain's door and everything inside it went unvisited. Every diagnostic the operand's own road
// emits was silenced by the operator around it:
//
//	gustyc --check 'y = 1 < zzz < 3'   →  ok                          (exit 0)
//	gustyc --check 'y = 1 < zzz'       →  error: undefined name "zzz"  (exit 1)
//
// and the same for a call to a name that does not exist, an arity mistake, a comprehension over a name
// the module never bound — wrapped in a chain, none of it reached the checker. A second consequence fell
// out of the same missing arm: the chain never got a type, so `--emit-ast` wrote its operands with no
// `"inferred"` beside the node, and every rule that reads a sub-expression's type was reading `any`
// where the program had written a verdict — `y: int = 1 < 2 < 3` checked clean while `y: int = 1 < 2`
// reported `type mismatch: expected int, got bool`, and `f(1 < 2 < 3)` into an `int` parameter checked
// clean while `f(1 < 2)` refused the argument (roadmap L12.1's own definition of done: the chain is
// typed `bool`).
//
// So this file does the two halves of that one decision:
//
//   - the OPERANDS are inferred through the ordinary road, in order, so a chain reports exactly what
//     the same expressions report with one operator — no chain-specific diagnostic language, nothing a
//     plain comparison does not also say;
//   - the CHAIN is typed `bool`, because a chain answers a verdict and nothing else, and the name it
//     binds, the annotation it reaches and the parameter it is passed into must be checked against the
//     verdict the program actually produces.
//
// Beside those two, the row's owed half: a link the reference can never answer — `1 < "a" < 3` asks
// `'<' not supported between instances of 'int' and 'str'` at run time — is now NAMED by `--check`.
// Named, not refused: the program still compiles, still runs, and still raises CPython's own sentence
// through its own `except TypeError:`, because a refusal is the wrong answer to a program the reference
// runs (ADR 0166's lesson, the one Gap R.242's row quotes back at the lexer). The opinion is a warning
// with the stable code `type.compare.uncomparable`, worded in the reference's own words so an agent can
// match it against a CPython traceback.
//
// The ordering question is NOT re-stated here. `order_kind.go` already holds the house's one answer to
// "do these two kinds order against each other" — `orderFamilyOf`, `orderPairHasNoOrdering` and
// `unsupportedOrderMessage` — because the compiled leg needs it to know when to RAISE. The checker reads
// that same table, so a link the run time raises for and a link the checker warns about are the same
// sentence about the same pair, decided by the same code (ADR 0232: one question, asked once; and
// `abs_kind_test.go`'s standing rule that two doors may not name one operand differently).
package lang

import "fmt"

// CodeCompareUncomparable is the stable rule code for the checker's opinion that a comparison is written
// over two kinds the operator can never take — one link of a chain, or an ordinary comparison with the
// same two kinds. Warning-level by design: the program under it is a program the reference RUNS, by
// raising, so the compile path must keep running it (roadmap L12.1, ADR 0166, ADR 0450).
const CodeCompareUncomparable = "type.compare.uncomparable"

// inferChainCompare is the analyzer's arm for a comparison chain: every operand is inferred through the
// ordinary road, and the whole node is a verdict.
//
// Inferring the operands is not a formality. It is what makes `undefined name`,
// `function "f" expects N arguments, got M` and every other operand-level diagnostic reach a chain — the
// diagnostics a chain swallowed for as long as the node had no arm here. The walk order is left to right,
// which is the order the values are evaluated in, so a name bound between two links is bound for the
// links after it and not for the ones before.
//
// The type is `bool` and never `any`: a chain answers the conjunction of its links, which is a verdict,
// and the positions that consume a verdict — an annotated binding, a typed parameter, an arithmetic
// operand — must see the same type here that they see for the single-comparison form (ADR 0450).
func (an *SemanticAnalyzer) inferChainCompare(n *ChainCompare) *Type {
	if len(n.Ops) == 0 || len(n.Operands) != len(n.Ops)+1 {
		// The parser builds the two slices together, so this is a broken AST rather than broken source.
		// Saying so beats walking off the end of the operands slice.
		an.errorf(n.Span(), "malformed comparison chain: %d operators over %d operands", len(n.Ops), len(n.Operands))
		return TBool()
	}
	links := make([]*Type, len(n.Operands))
	for i, o := range n.Operands {
		links[i] = an.inferExpr(o)
	}
	for i, op := range n.Ops {
		// The opinion sits on the operand that makes the link a link — the right-hand side, which is the
		// one the program wrote once and the chain reads twice.
		an.compareLinkWarnsAt(operandSpanOf(n.Operands[i+1], n), "comparison chain: ", op, links[i], links[i+1])
	}
	return TBool()
}

// operandSpanOf answers where an operand sits, falling back to the chain's own span when a synthetic
// operand carries none (a chain built by a test or a rewrite pass rather than the parser).
func operandSpanOf(e Expr, n *ChainCompare) Span {
	if e != nil {
		if sp := e.Span(); sp.Line != 0 || sp.Col != 0 {
			return sp
		}
	}
	return n.Span()
}

// compareLinkWarns is the ordinary comparison road's call into the one question below; compareLinkWarnsAt
// is the chain's, with a caption so a report says which of the two shapes said it.
func (an *SemanticAnalyzer) compareLinkWarns(sp Span, op string, lt, rt *Type) {
	an.compareLinkWarnsAt(sp, "", op, lt, rt)
}

func (an *SemanticAnalyzer) compareLinkWarnsAt(sp Span, caption, op string, lt, rt *Type) {
	sentence := comparisonLinkCannotAnswer(op, lt, rt)
	if sentence == "" {
		return
	}
	an.addDiagFull(Diagnostic{
		Level: LevelWarning, Span: sp, Msg: caption + sentence,
		Code:       CodeCompareUncomparable,
		Suggestion: "compare values of the kinds the operator takes; the reference raises TypeError where this program asks",
	})
}

// comparisonLinkCannotAnswer is the ONE question a comparison asks about its two operand types: can the
// reference answer `lt op rt` at all? It returns the reference's own sentence when the answer is provably
// no, and "" when the link answers OR the checker cannot name both kinds — the gradual half of gradual
// typing says nothing about a value whose kind the program never declared (roadmap L6.1).
//
// The reference's rule is per-operator, and so is this one:
//
//   - `==`, `!=`, `is`, `is not` take ANY pair and answer a verdict. Nothing is ever provably wrong here,
//     which is why `1 == "a"` is a program rather than a defect;
//   - `in`, `not in` ask the RIGHT operand whether it carries members at all. An `int`, a `float`, a
//     `bool`, `None` or a function does not, and the reference says
//     `argument of type 'int' is not iterable`;
//   - `<`, `<=`, `>`, `>=` are the operators with a real ordering, and that table lives in
//     `order_kind.go`, which the compiled leg reads to decide when to raise. This asks it.
//
// A class instance is answered nothing about on either side: `__lt__` / `__gt__` / `__contains__` can make
// any pair order and any value carry members, and the checker does not walk a class body from here.
func comparisonLinkCannotAnswer(op string, lt, rt *Type) string {
	switch op {
	case "==", "!=", "is", "is not":
		return ""
	case "in", "not in":
		if word, ok := memberlessKind(rt); ok {
			return fmt.Sprintf("argument of type '%s' is not iterable", word)
		}
		return ""
	case "<", "<=", ">", ">=":
		return orderingLinkSentence(op, lt, rt)
	default:
		return ""
	}
}

// orderingLinkSentence asks the house's ordering table the question it already answers for the compiled
// leg, of kinds the CHECKER can name. Both sides are widened over union members and the sentence is only
// spoken when EVERY pairing fails: `int | str` against an `int` may still be answered by the member the
// run time picked, and a checker that predicted the wrong one would be inventing a defect (ADR 0202's
// rule that a diagnostic is a fact about a position, not a guess about a run).
func orderingLinkSentence(op string, lt, rt *Type) string {
	lws, oks := checkerOrderKinds(lt)
	rws, okr := checkerOrderKinds(rt)
	if !oks || !okr {
		return ""
	}
	for _, l := range lws {
		for _, r := range rws {
			// The compiled leg leaves a container against a container to the tagged order door, which
			// ANSWERS `a = [1]` / `b = [2]` / `print(a < b)`. An opinion here would trade that answer for
			// a raise, which the ladder forbids — so the pair is not this checker's question either.
			if orderFamilyOf(l) == orderContainer && orderFamilyOf(r) == orderContainer {
				return ""
			}
			if !orderPairHasNoOrdering(l, r) {
				return ""
			}
		}
	}
	_, msg := unsupportedOrderMessage(op, lws[0], rws[0])
	return msg
}

// checkerOrderKinds names the reference's kind word (or words, for a union) of a type, in the spelling
// `order_kind.go`'s table reads: "int", "float", "bool", "str", "NoneType", "list", "dict", "set",
// "tuple". It reports false — NO OPINION, never "unorderable" — for everything whose ordering some other
// mechanism decides: a dynamic value, a class instance (through `__lt__`), a callable, an iterator, a
// void, a literal the checker only knows is a number.
func checkerOrderKinds(t *Type) ([]string, bool) {
	if t == nil {
		return nil, false
	}
	if t.Kind == KindUnion {
		var words []string
		for _, m := range t.Members {
			ws, ok := checkerOrderKinds(m)
			if !ok || len(ws) == 0 {
				return nil, false
			}
			words = append(words, ws...)
		}
		if len(words) == 0 {
			return nil, false
		}
		return words, true
	}
	w, ok := checkerOrderKindWord(t)
	if !ok {
		return nil, false
	}
	return []string{w}, true
}

// checkerOrderKindWord is the single-kind case, and the same word list the membership question below
// reads, so one type is never named two ways by the two operators (ADR 0232 again).
func checkerOrderKindWord(t *Type) (string, bool) {
	if t == nil {
		return "", false
	}
	switch t.Kind {
	case KindInt, KindLiteral:
		return "int", true
	case KindFloat:
		return "float", true
	case KindBool:
		return "bool", true
	case KindString:
		return "str", true
	case KindNone:
		return "NoneType", true
	case KindList:
		return "list", true
	case KindDict:
		return "dict", true
	case KindSet:
		return "set", true
	case KindTuple:
		return "tuple", true
	default:
		// KindDynamic, KindClass, KindFunc, KindCallable, KindIterator, KindSequence, KindVoid — the
		// kinds whose ordering or membership is decided by something the checker does not walk.
		return "", false
	}
}

// memberlessKind answers `x in container` for the checker: does the container side PROVABLY carry no
// members? The words are the reference's own (`argument of type 'NoneType' is not iterable`), and an
// unknown on the container side is a "cannot tell", which reads as no opinion — the run time is the one
// that owes the answer.
func memberlessKind(container *Type) (string, bool) {
	if container == nil {
		return "", false
	}
	if container.Kind == KindUnion {
		// Only a union every member of which is memberless is provably memberless, and the sentence has
		// to name one kind, so the checker stays out unless the members agree on the name.
		var word string
		for _, m := range container.Members {
			w, ok := memberlessKind(m)
			if !ok {
				return "", false
			}
			if word != "" && word != w {
				return "", false
			}
			word = w
		}
		if word == "" {
			return "", false
		}
		return word, true
	}
	w, ok := checkerOrderKindWord(container)
	if !ok {
		return "", false
	}
	switch orderFamilyOf(w) {
	case orderContainer, orderUnknown:
		// list/dict/set/tuple carry members; a text carries characters; anything the checker cannot name
		// may carry members through a dunder it defines.
		return "", false
	case orderText:
		return "", false
	default:
		return w, true
	}
}
