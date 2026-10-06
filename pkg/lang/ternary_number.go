package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// ternary_number.go — the word a ternary's answer travels in is a fact about its arms, not about the
// line it sits on (roadmap L11.6, Gap R.102; ADR 0262).
//
// `x if x > 2 else 0.0` chooses a value, and until now the compiled backend chose only the *branch*:
// the lowering emitted one instruction whatever the arms were,
//
//	%t = select i1 %c, i32 %then, i32 %els
//
// and every arm that was not an i32 arrived through `value()`, which renders a float literal by its
// truncated integer. Four shapes, all measured on the compiled leg, all answering with exit 0:
//
//	print(1 if 0 else 2.5)                      # CPython 2.5 · compiled 2
//	def f(x): return 1.5 if x > 2 else 2.5      # CPython 2.5 · compiled 2      — both arms doubles
//	def f(x): return 1 if x > 2 else 0.0        # CPython 0.0 · compiled 0      — one arm a double
//	def f(x): x = x + 1.5                       # CPython 2.5 · compiled: refused (Gap R.102)
//	   return x if x > 2 else 0.0
//
// The first two and the last are the same bug: the answer's word was chosen as if both arms were
// integers. The third is the same bug one step further, and the one the compiler cannot answer without
// the tagged value word — the two arms do not agree on a word, and which one runs is a run-time fact.
// So this file answers the question in one place, and the two lowerings that need it (`value`, which
// builds an i32, and `floatValue`, which builds a double) ask it rather than re-deriving it from the
// shape of the line, which is how ADR 0254's return-word gate and ADR 0174's parameter convention both
// work. A rule asked twice is a rule that drifts: `print` would take the float formatter while the
// arithmetic took the integer one over the same expression.
//
// The three answers:
//
//   - The test is a value the source wrote. Then one arm is the answer and the other is dead code, so
//     the ternary takes that arm's kind — including its *rendering*, which is why
//     `print(1 if 1 else 2.5)` keeps printing `1` and `print(1 if 0 else 2.5)` now prints `2.5`. This
//     is ADR 0261's `constantTestArm`, asked by the renderer there and by the word here; the two
//     questions are the same question, and a ternary is a value the source chose.
//   - Both arms are doubles. The answer is a double: `select i1 %c, double %a, double %b`. That is
//     Gap R.102's missing instruction, and it is legal LLVM the way it always was — what was missing
//     was a predicate brave enough to say the arms were doubles.
//   - Exactly one arm is a double, and which one runs is not known until it runs. Then the answer has
//     no word: a double would render the other arm `1.0` where CPython writes `1`, and an i32 truncates
//     the double arm, which is the number-shaped wrong answer ADR 0166 counts as our bug. That shape is
//     refused in words, and the tagged value word (L11.1) is the named owner of the half that needs it.

// ternaryKind is what the two arms of a ternary say about the answer's word.
type ternaryKind int

const (
	// ternaryIsInt: no arm is a double, so the answer is the i32 word it has always had.
	ternaryIsInt ternaryKind = iota
	// ternaryArmIsDouble: the test is a constant, and the arm that runs is the double.
	ternaryArmIsDouble
	// ternaryBothAreDoubles: whichever arm runs, the answer is a double.
	ternaryBothAreDoubles
	// ternaryArmDisagrees: one arm is a double and the other is not, and the test is a run-time fact.
	ternaryArmDisagrees
)

// ternaryKind asks the arms what they are. It is the only place the language decides that a ternary is
// or is not a float, and both backends' renderers and both lowerings read the same answer.
func (g *irGen) ternaryKind(n *CondExpr) ternaryKind {
	if taken, ok := constantTestArm(n); ok {
		if g.isFloat(taken) {
			return ternaryArmIsDouble
		}
		return ternaryIsInt
	}
	thenFloat := g.isFloat(n.If)
	elseFloat := g.isFloat(n.Else)
	switch {
	case thenFloat && elseFloat:
		return ternaryBothAreDoubles
	case thenFloat != elseFloat:
		return ternaryArmDisagrees
	}
	return ternaryIsInt
}

// ternaryDouble emits the answer in the double domain: the instruction Gap R.102 was filed for.
//
// An arm that is not a double is converted rather than refused, because the caller asked for a double
// and `sitofp` is what CPython's own promotion means there: inside `(1 if c else 2.5) * 2` the 1 *is*
// 1.0, and the answer comes out 2.0 either way. The refusal below belongs to the i32 domain only, where
// converting is not available and truncation is the only thing an i32 select can do.
func (g *irGen) ternaryDouble(b *strings.Builder, n *CondExpr) string {
	if taken, ok := constantTestArm(n); ok {
		return g.floatValue(b, taken)
	}
	cond, err := g.truthyValue(b, n.Cond)
	if err != nil || cond == "" {
		return ""
	}
	tThen := g.floatValue(b, n.If)
	tElse := g.floatValue(b, n.Else)
	if tThen == "" || tElse == "" {
		// An arm this backend cannot put in a double is the arm the caller will have to be told
		// about; answering 0.0 for it would be the silent truncation this file exists to end.
		return ""
	}
	t := g.newTmp()
	fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", t, cond, tThen, tElse)
	return t
}

// ternaryI32 is the i32-domain lowering: the select this backend has always emitted, minus the two
// answers it may not lawfully make.
func (g *irGen) ternaryI32(b *strings.Builder, n *CondExpr) (string, error) {
	// A test the source wrote is not a run-time question, so the arm that runs is the answer and the
	// other arm is not in the program. The reference does not evaluate the arm it does not take, and
	// neither does the emitted code — which also means nothing is lost by not emitting it: a constant
	// test is a literal, and a literal has no side effects to keep.
	if taken, ok := constantTestArm(n); ok {
		if g.isFloat(taken) {
			// The caller asked the i32 domain for a value whose arm is a double. Every caller that
			// can take a double asks `isFloat` first and comes to `ternaryDouble`; reaching here
			// means it cannot, and truncating is the answer this file was written to retire.
			return "", ternaryWordErr(n, ternaryArmIsDouble)
		}
		return g.value(b, taken)
	}
	switch k := g.ternaryKind(n); k {
	case ternaryBothAreDoubles, ternaryArmDisagrees:
		return "", ternaryWordErr(n, k)
	}
	// Two TEXT arms take the same select an int pair takes, and they need it: `value()` renders a text as
	// its @str_tab index, so without this the index was printed as a number and
	// `print("big" if x > 2 else "small")` answered `0` at exit 0 where the reference prints `big`
	// (Gap R.173, ADR 0290).
	//
	// Asking the pair together rather than each arm separately is what makes the select legal: each side
	// interns independently and both are free to allocate, so interleaving a store-bearing arm with the
	// other's test would let one branch skip the other's interning. The arms are evaluated first and
	// joined after, exactly as the number roads below do. Interning is idempotent (rt_str_intern2 returns
	// an existing slot), so whichever branch runs, both texts name the same index.
	if g.printsAsInternedStr(n.If) && g.printsAsInternedStr(n.Else) {
		cond, cerr := g.truthyValue(b, n.Cond)
		if cerr != nil {
			return "", cerr
		}
		tThen, terr := g.value(b, n.If)
		if terr != nil {
			return "", terr
		}
		tElse, eerr := g.value(b, n.Else)
		if eerr != nil {
			return "", eerr
		}
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cond, tThen, tElse))
		return t, nil
	}
	cond, err := g.truthyValue(b, n.Cond)
	if err != nil {
		return "", err
	}
	then, err := g.value(b, n.If)
	if err != nil {
		return "", err
	}
	els, err := g.value(b, n.Else)
	if err != nil {
		return "", err
	}
	// The condition is an i1 (comparison/and/or) or a bare constant LLVM infers as i1 in a select.
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cond, then, els))
	return t, nil
}

// ternaryWordErr is the refusal for the shape whose answer has no word. The two cases read differently
// because they are different programs: one arm being a double is a program that could have been written
// two ways, and the compiler's inability is about which arm runs; both arms being doubles in a context
// that stores an i32 is a context the program has to fix.
func ternaryWordErr(n *CondExpr, k ternaryKind) error {
	arms := ternaryArmNames(n)
	if k == ternaryBothAreDoubles || k == ternaryArmIsDouble {
		return fmt.Errorf("the answer of `%s if %s else %s` is a double and this context stores an i32 word: the compiled backend has a number-typed `select` (ADR 0262) but not a word that is both a double and an i32 — print it, store it, or compute with it rather than hand it to something that reads an i32 (a container slot, a call argument typed as an int, an index); roadmap L11.1's tagged value word is what makes an answer carry its own kind", arms[0], arms[1], arms[2])
	}
	return fmt.Errorf("the two arms of `%s if %s else %s` do not agree on a word — one is the double the other is an i32 — and which arm runs is not known until it runs, so the answer has no word to travel in: an i32 select would truncate the double arm to its integer, which ADR 0166 counts as this compiler's bug rather than the program's (write both arms in the same shape, `else 0` -> `else 0.0`, or take the branch with `if`/`else` and bind the value there; roadmap L11.1's tagged value word is what makes the run-time choice answerable)", arms[0], arms[1], arms[2])
}

// ternaryArmNames spells the arms and the test for a refusal message, in the surface form the program
// wrote when it is short and by kind when it is not. A message that says `a ternary` where the program
// wrote `x if n > 2 else 0.0` asks the programmer to guess which line it means.
func ternaryArmNames(n *CondExpr) [3]string {
	return [3]string{exprSurface(n.If), exprSurface(n.Cond), exprSurface(n.Else)}
}

// exprSurface renders an expression the way a diagnostic quotes it: the source text where the printer
// knows the node, the node kind where it does not. It never returns the empty string, because it is
// always written inside backquotes.
func exprSurface(e Expr) string {
	switch x := e.(type) {
	case nil:
		return "…"
	case *Name:
		return x.Value
	case *IntLit:
		return strconv.FormatInt(x.Value, 10)
	case *FloatLit:
		// The spelling the program wrote, not the value re-rendered: a refusal that quotes `0` for the
		// arm the source wrote `0.0` describes a program the author did not type, and the whole point of
		// the message is that the two arms should agree on a word.
		if x.Text != "" {
			return x.Text
		}
		return floatSurface(x.Value)
	case *StrLit:
		return quoteString(x.Value)
	case *BoolLit:
		if x.Value {
			return "True"
		}
		return "False"
	case *NoneLit:
		return "None"
	case *UnOp:
		return x.Op + exprSurface(x.X)
	case *BinOp:
		return exprSurface(x.L) + " " + x.Op + " " + exprSurface(x.R)
	case *CondExpr:
		return exprSurface(x.If) + " if " + exprSurface(x.Cond) + " else " + exprSurface(x.Else)
	}
	if s, ok := exprSourceText(e); ok {
		return s
	}
	return fmt.Sprintf("a %T", e)
}

// exprSourceText asks the canonical formatter for an expression, and refuses to quote it when the
// formatter does not know the node: its fallback is `None`, which in a diagnostic would be a lie about
// the program.
func exprSourceText(e Expr) (string, bool) {
	var sb strings.Builder
	writeExpr(&sb, e, 0)
	s := sb.String()
	if s == "" || (s == "None" && !isNoneLit(e)) {
		return "", false
	}
	return s, true
}

// floatSurface renders a double the way the language prints one: always with a `.` or an exponent, so a
// diagnostic never shows `2` for the arm that is the double 2.0.
func floatSurface(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}

func isNoneLit(e Expr) bool {
	_, ok := e.(*NoneLit)
	return ok
}

// returnedTernary finds the ternary a function's `return` hands back, for the refusal that has to say
// which arms disagree. A body with more than one such return is refused on the first, in source order.
func returnedTernary(fd *FuncDef) *CondExpr {
	if fd == nil {
		return nil
	}
	var found *CondExpr
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			if found != nil {
				return
			}
			switch s := st.(type) {
			case *ReturnStmt:
				if c, ok := s.Expr.(*CondExpr); ok && !ternaryTestIsKnown(c) {
					found = c
				}
			case *IfStmt:
				walk(s.Then)
				for _, e := range s.Elifs {
					walk(e.Then)
				}
				walk(s.Else)
			case *WhileStmt:
				walk(s.Body)
			case *TryStmt:
				walk(s.Body)
				for _, arm := range s.Excepts {
					walk(arm.Body)
				}
				walk(s.Finally)
			}
		}
	}
	walk(fd.Body)
	return found
}

// ternaryTestIsKnown is the ternary's own "which arm runs is already settled" question. The refusal
// quotes only a ternary whose test is a run-time fact: a constant-test ternary is answered by the arm
// that runs and never reaches this gate, so blaming it for disagreeing arms would be blaming a program
// the compiler already handles.
func ternaryTestIsKnown(c *CondExpr) bool {
	_, ok := constantTestArm(c)
	return ok
}
