// The raise side: the exception record, the deferred bodies try/finally runs, the guard that raises, and the
// sentence each trap prints.
package lang

import (
	"fmt"
	"strings"
)

// setExn records a raise: the flag, the exception code, and a "<Type>: <message>"
// string for the uncaught path. Every raise site goes through here so the report can
// never disagree with the code the handler matches on.
func (g *irGen) setExn(b *strings.Builder, code int, typeName, msg string, sp Span) {
	g.raiseUsed = true
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	b.WriteString(fmt.Sprintf("  store i32 %d, i32* @exn_code\n", code))
	text := typeName
	if msg != "" {
		text = typeName + ": " + msg
	}
	if text == "" {
		b.WriteString("  store i8* null, i8** @exn_msg\n")
	} else {
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** @exn_msg\n", g.strConst(text)))
	}
	// the record prints one frame per stack level; the compiled report used to print the
	// raise site's own frame alone. That string is still stored — it is what rt_die falls back
	// to when the shadow stack has nothing to say about a raise whose statement carried no
	// position — and beside it the raise now leaves its line in the live frame and copies the
	// stack out, which is the half Gap K.8 owed (ADR 0354).
	if frame := g.raiseFrame(sp); frame != "" {
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** @exn_frame\n", g.strConst(frame)))
	} else {
		b.WriteString("  store i8* null, i8** @exn_frame\n")
	}
	line := 0
	if !sp.IsZero() && sp.Line > 0 {
		line = sp.Line
	}
	b.WriteString(fmt.Sprintf("  call void @rt_frame_here(i32 %d)\n", line))
	b.WriteString("  call void @rt_frames_snapshot()\n")
}

// raiseFrame renders one traceback frame the way the record does, e.g.
//
//	File "prog", line 12, in area
//
// for a raise at line 12 inside `area` (or <module> at top level). An empty span yields
// no frame rather than a fabricated line 0.
func (g *irGen) raiseFrame(sp Span) string {
	if sp.IsZero() {
		return ""
	}
	fn := "<module>"
	if g.curFnSrc != "" {
		fn = g.curFnSrc
	}
	return fmt.Sprintf("  File \"prog\", line %d, in %s", sp.Line, fn)
}

// raiseTo records a raise of an exception the *machine* detected (an out-of-range
// index, a missing key) and transfers to the innermost handler, or out of the function
// to the uncaught path. the record raises the same typed errors, so `except
// IndexError:` works on the compiled backend.
func (g *irGen) raiseTo(b *strings.Builder, code int, typeName, msg string, sp Span) {
	g.setExn(b, code, typeName, msg, sp)
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
}

// trueDivMessage and floorDivMessage pick the wording CPython uses for the same operation, so
// the two tracebacks can be compared line for line. The distinction is not cosmetic: `7 / 0`
// divides two integers and the answer is "division by zero", even though this backend lowers
// int `/` to a double division (PEP 238) and the emitted instruction is a fdiv. Naming the
// instruction would describe our codegen at the expense of describing the program.
func (g *irGen) trueDivMessage(l, r Expr) string {
	if g.isFloat(l) || g.isFloat(r) {
		return "float division by zero"
	}
	return "division by zero"
}
func (g *irGen) floorDivMessage(l, r Expr) string {
	if g.isFloat(l) || g.isFloat(r) {
		return "float floor division by zero"
	}
	return "integer division or modulo by zero"
}

// guardNonZeroInt emits the ZeroDivisionError trap in front of an integer sdiv/srem. It goes
// through raiseTo rather than a runtime abort because the trap must be catchable:
// `except ZeroDivisionError:` matches on the exception code, and a program that handles a
// division has run to completion — the same distinction ADR 0211 made for exit statuses.
func (g *irGen) guardNonZeroInt(b *strings.Builder, divisor string, sp Span, kind string) {
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isZero, divisor))
	g.markI1(isZero)
	g.branchRaise(b, isZero, "ZeroDivisionError", kind, sp, "div")
}

// guardNonZeroFloat is the same trap for the double paths. `fdiv x, 0.0` is not an error to
// LLVM — it returns ±inf, which is how the compiled backend came to print `inf` for
// `print(1 / 0)` and exit 0 — so the test is ours to emit. A NaN divisor is not zero, exactly
// as in Python, and -0.0 compares equal to 0.0, which is what Python does too.
func (g *irGen) guardNonZeroFloat(b *strings.Builder, divisor string, sp Span, kind string) string {
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = fcmp oeq double %s, 0.000000e+00\n", isZero, divisor))
	g.markI1(isZero)
	return g.branchRaise(b, isZero, "ZeroDivisionError", kind, sp, "fdiv")
}

// branchRaise emits `cond ? raise(class, kind) : continue` as its own pair of blocks, the
// shape every emitted bounds/member check already uses. The block the continuation runs in is
// returned, because a hand-written `phi` after a guard has to name *that* block as its predecessor —
// the block the guard was written in is no longer the one that branches on (ADR 0138's lesson).
func (g *irGen) branchRaise(b *strings.Builder, cond, class, kind string, sp Span, tag string) string {
	badL, okL := g.newLabel(tag+".bad"), g.newLabel(tag+".ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode(class), class, kind, sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	return okL
}

// callableSurfaceName turns a compiled function's internal name back into the name the reference
// would use in the same message. A lambda is compiled to a generated symbol (`lambda_0`) that no
// source line contains; printing it in a diagnostic blames a name the programmer never wrote, while
// the reference says `<lambda>` — and an arity sentence is exactly the message a reader acts on.
func callableSurfaceName(fnName string) string {
	if strings.HasPrefix(fnName, "lambda_") {
		return "<lambda>"
	}
	// A lambda the defunctionalisation pass gave a declaration of its own carries the binding or callee
	// it was minted from in its symbol; the reader's program has neither, and the reference names the
	// anonymous function `<lambda>` whatever it is bound to (ADR 0345 §3).
	if strings.Contains(fnName, callableLambdaInfix) {
		return "<lambda>"
	}
	// A clone the defunctionalisation pass minted is the function the source wrote, and a diagnostic about
	// it must say so: the reader's program contains `twice`, not `twice$callable$1a2b3c4d` (ADR 0345,
	// ADR 0166's rule that a refusal names the program's own spelling).
	if base := callableGeneratedBase(fnName); base != "" {
		return base
	}
	return fnName
}

// referenceMethodTrap names what the reference raises for the container methods whose whole job is to
// fail loudly, so a refusal can say what the compiled program would have to do, not merely that it
// will not. Methods that answer instead of raising return a description of their answer.
func referenceMethodTrap(name string) string {
	switch name {
	case "index":
		return "ValueError: … is not in list"
	case "pop":
		return "KeyError: <the missing key>"
	case "remove":
		return "ValueError: list.remove(x): x not in list"
	case "discard":
		return "no error (it is remove without the raise)"
	}
	return "its documented answer"
}

// raiseStmt compiles `raise Exception("msg")` / `raise ValueError("msg")`.
func (g *irGen) raiseStmt(b *strings.Builder, rs *RaiseStmt) error {
	code, typeName, msg := 0, "", ""
	if c, ok := rs.Expr.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 {
			code = exnCode(n.Value)
			typeName = n.Value
			if len(c.Args) > 0 {
				if sl, ok3 := c.Args[0].(*StrLit); ok3 {
					msg = sl.Value // `raise ValueError("boom")` -> "ValueError: boom"
				}
			}
		}
	} else if n, ok := rs.Expr.(*Name); ok {
		code = exnCode(n.Value)
		typeName = n.Value
	}
	g.setExn(b, code, typeName, msg, rs.Span())
	// A raise that leaves an arm it already accepted, or that comes out of a `finally`, has
	// deferred bodies still owed to it: they run now, before the jump hands the exception to
	// the next handler, or they would never run at all (Gap R.23, ADR 0222). A raise from an
	// ordinary `try` body does not: this `try`'s own arms may still catch it, and they run
	// before its deferred body.
	if g.handledArms > 0 && !g.emittingDeferred {
		if err := g.runDeferredInnermost(b); err != nil {
			return err
		}
	}
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
	return nil
}

// escapedTerminator reports whether the text a deferred statement emitted left the block for
// good: a `ret`, an `unreachable`, or a branch toward this function's raise-exit or a try
// handler. An `if` or a loop ends its block with an ordinary forward `br` too, and calling that
// an escape would silently stop the walk before the outer `finally` bodies -- so the test is
// about where control went, not about the opcode.
func (g *irGen) escapedTerminator(text string) bool {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "unreachable" || strings.HasPrefix(ln, "ret ") {
			return true
		}
		if strings.HasPrefix(ln, "br ") && (strings.Contains(ln, "%"+g.funcRaiseExit) || strings.Contains(ln, "raiseexit")) {
			return true
		}
	}
	return false
}

// runDeferredLevel lowers one pending `finally` body -- the one at index i -- and reports whether
// it transferred out (its own `return` or raise), in which case nothing after it may be emitted.
// Levels below i are taken off the stack for the duration, so a `return` inside this body sees
// only the *outer* pending bodies and a raise in it cannot re-enter this level.
func (g *irGen) runDeferredLevel(b *strings.Builder, i int) (escaped bool, err error) {
	if i < 0 || i >= len(g.deferred) {
		return false, nil
	}
	level := g.deferred[i]
	saved := g.deferred
	g.deferred = saved[:i]
	g.emittingDeferred = true
	for _, st := range level {
		before := b.Len()
		if err := g.stmt(b, st); err != nil {
			g.emittingDeferred = false
			g.deferred = saved
			return false, err
		}
		if g.escapedTerminator(b.String()[before:]) {
			escaped = true
			break
		}
	}
	g.emittingDeferred = false
	g.deferred = saved
	return escaped, nil
}

// runDeferred lowers every pending `finally` body, innermost first: the walk a `return`,
// `break` or `continue` owes, because that transfer leaves all of the enclosing `try`
// statements at once and nothing else is going to run them (Gap R.23, ADR 0222).
func (g *irGen) runDeferred(b *strings.Builder) error {
	for i := len(g.deferred) - 1; i >= 0; i-- {
		escaped, err := g.runDeferredLevel(b, i)
		if err != nil {
			return err
		}
		if escaped {
			return nil
		}
	}
	return nil
}

// runDeferredInnermost lowers only the innermost pending `finally` body. This is what an
// exception leaving a `try` owes: the *outer* bodies are run by those statements themselves when
// the hand-off reaches their handler blocks, so running them here would run them twice -- once
// on the way out and once again on their own straight-line path.
func (g *irGen) runDeferredInnermost(b *strings.Builder) error {
	_, err := g.runDeferredLevel(b, len(g.deferred)-1)
	return err
}

// clearExn marks the pending exception as finished. A handled exception has to leave the
// flag behind cleared, because @exn_flag is one module-wide bit: anything still reading it
// takes the exception as still in flight. That is what happened — an arm ran, the program
// continued, and the next user-function call's check branched to the handler again (or to the
// raise-exit when no handler was in scope any more), reporting an exception the program had
// already handled. the record cleared it in Gap R.21 (ADR 0213: "falling out of a `try`
// clears it"); this is the same rule on the compiled side (roadmap Gap R.21, compiled half).
func (g *irGen) clearExn(b *strings.Builder) {
	g.raiseUsed = true
	b.WriteString("  store i32 0, i32* @exn_flag\n")
}

// checkExn emits a check of @exn_flag after a user-function call.
func (g *irGen) checkExn(b *strings.Builder) {
	g.checkExnLabel(b)
}

// checkExnLabel is checkExn with the continuation block's name, for a caller inside a `switch`
// arm that has to name that block in a `phi` incoming list afterwards.
func (g *irGen) checkExnLabel(b *strings.Builder) string {
	f := g.newTmp()
	c := g.newTmp()
	cont := g.newLabel("exn.cont")
	b.WriteString("  " + f + " = load i32, i32* @exn_flag\n")
	b.WriteString("  " + c + " = icmp eq i32 " + f + ", 1\n")
	target := g.funcRaiseExit
	if len(g.handlerStack) > 0 {
		target = g.handlerStack[len(g.handlerStack)-1]
	}
	b.WriteString("  br i1 " + c + ", label %" + target + ", label %" + cont + "\n")
	b.WriteString(cont + ":\n")
	return cont
}

// tryStmt compiles a try/except/finally statement.
func (g *irGen) tryStmt(b *strings.Builder, ts *TryStmt) error {
	handler := g.newLabel("try.handler")
	finally := g.newLabel("try.finally")
	after := g.newLabel("try.after")
	g.handlerStack = append(g.handlerStack, handler)
	// This `try`'s deferred body is pending for the body and for every arm: a transfer out of
	// either one owes it a run (Gap R.23, ADR 0222). It is popped before the straight-line
	// `finally:` block below, which emits the body itself exactly once.
	if len(ts.Finally) > 0 {
		g.deferred = append(g.deferred, ts.Finally)
		defer func(saved [][]Stmt) { g.deferred = saved }(g.deferred[:len(g.deferred)-1])
	}
	for _, st := range ts.Body {
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	g.handlerStack = g.handlerStack[:len(g.handlerStack)-1]
	f := g.newTmp()
	c := g.newTmp()
	b.WriteString("  " + f + " = load i32, i32* @exn_flag\n")
	b.WriteString("  " + c + " = icmp eq i32 " + f + ", 1\n")
	b.WriteString("  br i1 " + c + ", label %" + handler + ", label %" + finally + "\n")

	b.WriteString(handler + ":\n")
	// The arms are dispatched in source order, each comparing @exn_code, a bare `except:` and
	// `except Exception:` catching everything. Only the first arm used to be emitted at all, and
	// a non-matching exception had its flag cleared and fell through to the continuation — so a
	// handler that exists ran nothing, an unhandled exception vanished with no traceback and
	// exit 0, and a nested try never reached its outer arm (roadmap Gap R.20, ADR 0213).
	for _, ec := range ts.Excepts {
		armBody := g.newLabel("try.arm")
		notThis := g.newLabel("try.next")
		catchAll := ec.Exn == nil || ec.Exn.Value == "Exception"
		if !catchAll {
			code := g.newTmp()
			m := g.newTmp()
			b.WriteString("  " + code + " = load i32, i32* @exn_code\n")
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", m, code, exnCode(ec.Exn.Value)))
			g.markI1(m)
			b.WriteString("  br i1 " + m + ", label %" + armBody + ", label %" + notThis + "\n")
		} else {
			b.WriteString("  br label %" + armBody + "\n")
		}
		b.WriteString(armBody + ":\n")
		// The arm is reached BECAUSE @exn_flag is set, and reaching it means the exception has been
		// taken. Clearing the flag only at the arm's end — which is what this lowering did — leaves the
		// whole arm running under a flag that says "an exception is in flight", and every call the arm
		// writes ends with `checkExn`, which loads that flag and jumps to the handler OUTSIDE this one
		// (the stack was popped above, so it goes to the function's raise-exit). The arm therefore ran,
		// printed, and then took the run down with the exception it had already handled — and the shape
		// where the arm's statement is `print(f())` never printed at all (roadmap Gap R.241; the same
		// fault read from the callable side as Gap R.389). An accepting edge clears the flag, so it is
		// cleared HERE, before the arm's first statement; the clear on the edge out below stays as the
		// belt-and-braces case ADR 0218 paid for, for an arm that leaves by a path this one does not see.
		g.clearExn(b)
		armed := false
		g.handledArms++
		for _, st := range ec.Body {
			if err := g.stmt(b, st); err != nil {
				g.handledArms--
				return err
			}
			if blockEndsInTerminator(b) {
				armed = true // this arm returned or raised: it left by its own path
				break
			}
		}
		g.handledArms--
		if !armed {
			// The arm took the exception, so the exception is over.
			g.clearExn(b)
			b.WriteString("  br label %" + finally + "\n")
		}
		b.WriteString(notThis + ":\n")
	}
	// Nothing matched. The exception belongs to an enclosing scope now: restore the flag and
	// hand it to the next handler out, or to the function's raise-exit — the same destination an
	// explicit `raise` with no handler in sight uses.
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	// Nobody in this statement caught it, so this `try` is being left by an exception: its
	// deferred body (and any outer one still pending) runs before the hand-off.
	if err := g.runDeferredInnermost(b); err != nil {
		return err
	}
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}

	b.WriteString(finally + ":\n")
	// Straight-line exit: the deferred body is emitted here, not from the stack, so the stack
	// is dropped first -- otherwise a `return` inside it would emit this body a second time.
	// A `try` with arms but no `finally` clause pushed nothing (the block above still runs, as
	// the single continuation label), so the pop belongs to the same guard the push does.
	if len(ts.Finally) > 0 {
		g.deferred = g.deferred[:len(g.deferred)-1]
	}
	for _, st := range ts.Finally {
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	b.WriteString("  br label %" + after + "\n")
	b.WriteString(after + ":\n")
	return nil
}
