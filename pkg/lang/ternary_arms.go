package lang

import (
	"fmt"
	"strings"
)

// ternary_arms.go — the arm the test rejected does not run (roadmap Gap R.340, ADR 0374).
//
// A conditional expression chooses a value. Until now the compiled backend chose only the *word*:
// ADR 0026 lowered a run-time test to one `select`, and a `select` has two operands, both of which the
// run time has already computed by the moment it picks one. Four programs, all measured on the compiled
// leg, all answered by the arm that was never supposed to run:
//
//	x = 5
//	print(1 if x > 0 else boom())        # CPython `1` · compiled printed `boom` and then `1`
//	print(boom() if x > 100 else 1 + 1)  # CPython `2` · compiled printed `boom` and then `2`
//	print(1 if x > 0 else 1 // 0)        # CPython `1` · compiled raised ZeroDivisionError
//	def f(n):                            # CPython `3` · compiled never returned: the recursive call
//	    return 0 if n == 0 else 1 + f(n)     #   is unconditional, so the base case was a value and
//	                                         #   not a stop — "root stack is full", exit 2
//
// The third and fourth faces are the same defect wearing the exit-code contract's worst hat: ADR 0166
// counts a module that cannot run as this compiler's bug, and a recursion that cannot stop is that
// class arrived through the most ordinary construct in the language.
//
// The fix is the one ADR 0275 already made for `and`/`or` (Gap R.149): the test is evaluated once in the
// block the caller stands in, the run time branches on it, and each arm is lowered ONLY in the block the
// branch enters, with one `phi` per answer in the merge. `logicSkeleton` is that shape for two operands
// that share a test; this file is that shape for a ternary, where both arms are behind the branch.
//
// The `select` is not deleted. Where neither arm can do anything but answer a value — a literal, or
// arithmetic over literals, or a read of a slot the checker proved was written — the two roads disagree
// about nothing except how many instructions the module carries, and ADR 0273's rule is that a gate must
// not cost a program that compiles today: `print(1.5 if c > 0 else 2.5)` still emits the one
// `select i1 … double …, double …` ADR 0262 was written to put there, byte for byte. The branch is taken
// when an arm can call, raise, allocate or read a slot that may still be unwritten — which is exactly
// the set of arms whose evaluation the reference skips.

// ternaryArmIsSilent asks whether lowering this arm can do anything besides answer a value. The answer
// is conservative in the direction that costs instructions and never in the direction that runs a
// program's effects: an expression whose shape this file cannot prove harmless is "may act", and the
// arm is put behind the branch.
//
// Harmless is a short list, because the ways an expression raises in this backend are many and do not
// live in the node: a subscript raises IndexError/KeyError/TypeError, a division raises
// ZeroDivisionError, an ordering of two kinds raises TypeError, a call raises whatever its body raises
// and prints whatever its body prints, a container literal allocates, a comprehension loops. A name
// read is harmless UNLESS the checker could not prove it was written — the same question
// `checkBound` asks before it emits the UnboundLocalError test (ADR 0228), asked here from the same
// tables so the two doors cannot disagree about whether a read is an event.
func (g *irGen) ternaryArmIsSilent(e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit:
		return true
	case *Name:
		return !g.readMayTrap(x.Value)
	case *UnOp:
		// `-x` and `not x` raise when the operand is not the kind they expect, and the fold that
		// declines to answer for such an operand (ADR 0266) leaves the raise to the run time. Only a
		// literal operand is provably harmless; `not` of a literal answers without asking anything.
		if x.Op == "not" {
			return ternaryLit(g, x.X)
		}
		return ternaryNumericLit(g, x.X)
	case *BinOp:
		switch x.Op {
		case "+", "-", "*", "&", "|", "^", "<<", ">>":
			return ternaryLit(g, x.L) && ternaryLit(g, x.R)
		case "==", "!=", "<", "<=", ">", ">=":
			// Two literals of one domain order without asking the runtime; mixed literals raise the
			// reference's TypeError, and `in`/`is` reach the runtime tables at all kinds.
			return ternarySameDomainLits(g, x.L, x.R)
		}
		return false // `/`, `//`, `%` (ZeroDivisionError), `and`/`or` (their own doors), `in`, `is`
	}
	return false
}

// ternaryLit is "a literal the arithmetic and comparison domains can hold with no run-time help".
// A text is included because `"a" + "b"` folds; a container is not, because building one allocates.
func ternaryLit(g *irGen, e Expr) bool {
	switch e.(type) {
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit:
		return true
	}
	if c, ok := e.(*CondExpr); ok {
		// A ternary inside an arm is harmless only when its own test is a value the source wrote:
		// then it IS the arm that runs, and the fold below reaches it through constantTestArm.
		if taken, decided := constantTestArm(c); decided {
			return ternaryLit(g, taken)
		}
		return false
	}
	if n, ok := e.(*Name); ok {
		// A name is harmless as an arithmetic operand only when reading it is harmless and the
		// value it holds is a number the folder can see; anything else and the operation is the
		// runtime's question, which is an event this door must not take off the table.
		return g.readMayTrap(n.Value) == false && g.numericNameIsLiteralNumber(n.Value)
	}
	return false
}

// ternaryNumericLit is "a number written in the source" — the set a unary minus can be answered for
// without the run time being asked what the operand is.
func ternaryNumericLit(g *irGen, e Expr) bool {
	switch x := e.(type) {
	case *IntLit, *FloatLit, *BoolLit:
		return true
	case *Name:
		return !g.readMayTrap(x.Value) && g.numericNameIsLiteralNumber(x.Value)
	}
	return false
}

// ternarySameDomainLits is the comparison case: both sides are literals, and they belong to one domain
// (both numbers, or both texts), so the ordering is decided by the fold and never reaches the per-kind
// raise ADR 0252 emits when the run time has to answer the question.
func ternarySameDomainLits(g *irGen, l, r Expr) bool {
	if !ternaryLit(g, l) || !ternaryLit(g, r) {
		return false
	}
	ln, lnok := ternaryLitIsNumber(g, l)
	rn, rnok := ternaryLitIsNumber(g, r)
	if !lnok || !rnok {
		return false
	}
	return ln == rn
}

// ternaryLitIsNumber classifies a literal as number / not-number, and says whether it is a literal at
// all. A name folded to a constant number counts as a number, which is what lets
// `MAX = 4` / `print(1 if MAX > 2 else 2)` keep the `select` it has always had.
func ternaryLitIsNumber(g *irGen, e Expr) (bool, bool) {
	switch x := e.(type) {
	case *IntLit, *FloatLit, *BoolLit:
		return true, true
	case *StrLit:
		return false, true
	case *NoneLit:
		return false, true
	case *Name:
		if !g.readMayTrap(x.Value) && g.numericNameIsLiteralNumber(x.Value) {
			return true, true
		}
		return false, false
	case *CondExpr:
		if taken, decided := constantTestArm(x); decided {
			return ternaryLitIsNumber(g, taken)
		}
	}
	return false, false
}

// readMayTrap is ADR 0228's question asked as a fact rather than as an emission: does reading this name
// carry a written-flag whose test can raise. It reads the same tables `checkBound` consults, so the
// branch decision and the emitted check cannot drift apart.
func (g *irGen) readMayTrap(name string) bool {
	if g.boundFlags == nil {
		return false
	}
	if !g.boundFlags[name] {
		return false
	}
	_, ok := g.boundSlots[name]
	return ok
}

// numericNameIsLiteralNumber is the narrow "this name is a number the folder can see" question — a name
// bound to a compile-time constant. Everything else a body might read is not provably a number, and the
// arithmetic over it is the runtime's business.
func (g *irGen) numericNameIsLiteralNumber(name string) bool {
	if _, ok := g.constBindings[name]; ok {
		return true
	}
	if folded, ok := g.curModGlobals[name]; ok && !g.curModParams[name] {
		switch folded.(type) {
		case *IntLit, *FloatLit, *BoolLit:
			return true
		}
	}
	return false
}

// ternaryArmsNeedBranch is the gate: the test is a run-time fact and one of the arms can act. A test the
// source wrote is answered by the fold in front of this door, so the gate never fires for it — a
// constant-test ternary has exactly one arm in the program and no run time to choose between them.
func (g *irGen) ternaryArmsNeedBranch(n *CondExpr) bool {
	if n == nil {
		return false
	}
	if _, decided := constantTestArm(n); decided {
		return false
	}
	return !g.ternaryArmIsSilent(n.If) || !g.ternaryArmIsSilent(n.Else)
}

// ternaryArmBranchRefusal is what the int road says when an arm will not lower inside the block the
// branch enters. It is a refusal and not a fallback: the road behind this answer is the one that lowers
// both arms beside the test, which is the defect this file exists to close, and ADR 0166 counts
// re-emitting it as shipping the bug under a different name.
func ternaryArmBranchRefusal(n *CondExpr) error {
	arms := ternaryArmNames(n)
	return fmt.Errorf("the compiled backend cannot evaluate the arm of `%s if %s else %s` inside the block its test chooses: an arm is a program the test runs, not an operand of the instruction that picks, and this arm does not lower behind a branch (roadmap Gap R.340, ADR 0374 — `select` would run the arm the test rejected, which ADR 0166 counts as this compiler's bug; write the arm as a value, or take the branch yourself with `if`/`else` and bind the answer there)", arms[0], arms[1], arms[2])
}

// ternaryBranch lowers `a if c else b` the way ADR 0275 lowers the two operators that choose an operand:
// the test once, `br i1` on its truth, each arm lowered only inside the block the branch enters, and one
// `phi` per answer in the merge.
//
// Both arms are built into a scratch builder and nothing is committed to b until both answered — the
// rule ADR 0275 learned when a half-emitted door left a module with a block that had no terminator
// (Gap R.149). Each arm's own lowering may end in a block the call road opened behind its exception
// check, which is why the arm jumps to a forwarding block the merge names: a `phi`'s incoming block has
// to be one this door controls, and the register the arm returned dominates the terminator of the path
// that reaches it either way.
func (g *irGen) ternaryBranch(b *strings.Builder, n *CondExpr, ty string, arm func(*strings.Builder, Expr) (string, error)) (string, bool, error) {
	var body strings.Builder
	cond, cerr := g.truthyValue(&body, n.Cond)
	if cerr != nil {
		return "", false, cerr
	}
	if cond == "" {
		return "", false, nil
	}
	thenBlk := g.newLabel("tern.then")
	elseBlk := g.newLabel("tern.else")
	thenFwd := g.newLabel("tern.thenfwd")
	elseFwd := g.newLabel("tern.elsefwd")
	merge := g.newLabel("tern.merge")

	fmt.Fprintf(&body, "  br i1 %s, label %%%s, label %%%s\n", cond, thenBlk, elseBlk)
	fmt.Fprintf(&body, "%s:\n", thenBlk)
	left, lerr := arm(&body, n.If)
	if lerr != nil {
		return "", false, lerr
	}
	if left == "" {
		return "", false, nil
	}
	fmt.Fprintf(&body, "  br label %%%s\n", thenFwd)
	fmt.Fprintf(&body, "%s:\n  br label %%%s\n", thenFwd, merge)
	fmt.Fprintf(&body, "%s:\n", elseBlk)
	right, rerr := arm(&body, n.Else)
	if rerr != nil {
		return "", false, rerr
	}
	if right == "" {
		return "", false, nil
	}
	fmt.Fprintf(&body, "  br label %%%s\n", elseFwd)
	fmt.Fprintf(&body, "%s:\n  br label %%%s\n", elseFwd, merge)
	fmt.Fprintf(&body, "%s:\n", merge)
	out := g.newTmp()
	fmt.Fprintf(&body, "  %s = phi %s [ %s, %%%s ], [ %s, %%%s ]\n", out, ty, left, thenFwd, right, elseFwd)
	b.WriteString(body.String())
	return out, true, nil
}
