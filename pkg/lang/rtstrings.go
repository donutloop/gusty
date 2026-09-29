package lang

// Runtime string operations (ADR 0229).
//
// A compiled string value is an index into @str_tab (ADR 0224), and the table is
// content-addressed and growable at run time (rt_str_intern). That combination means an
// operation asked about at run time has somewhere to live: the helpers in the runtime blob
// take indices and return indices, so the compiled convention never changes and print, ==,
// substring tests, container slots and `for` all keep working on a string the compiler never
// saw. What this file adds is the one question each operation has to ask first — is this
// operand a string? — which used to be "…and can the compiler read its text?", a stricter
// question than the language asks.

import (
	"fmt"
	"strconv"
	"strings"
)

// strReg evaluates e to an i32 @str_tab index: the compiled shape of a string value. ok is
// false when the expression is not a string at all, which is the caller's cue to refuse with
// its own, accurate message (Gap R.38: a refusal may only assert what its gate knows).
func (g *irGen) strReg(b *strings.Builder, e Expr) (reg string, ok bool, err error) {
	if txt, known := g.stringVal(e); known {
		return g.internStr(b, txt), true, nil
	}
	if !g.exprIsString(e) {
		return "", false, nil
	}
	g.heapUsed = true
	v, err := g.value(b, e)
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// rtStrCall emits one table operation over interned indices. Every one returns an i32: an
// index for the string-building operations, a count for the measuring ones, and -1 for
// "no such position", which the caller turns into a raise — a trap the program can name has
// to be raised by the code that knows the source (ADR 0212).
func (g *irGen) rtStrCall(b *strings.Builder, fn string, args ...string) string {
	g.heapUsed = true
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, fn, strings.Join(args, ", ")))
	return t
}

// strPosReg renders a position argument as the i32 the table wants. A literal stays a
// literal; anything else is lowered like any other expression, which is the whole point.
func (g *irGen) strPosReg(b *strings.Builder, e Expr) (string, error) {
	if il, ok := e.(*IntLit); ok {
		return strconv.FormatInt(il.Value, 10), nil
	}
	return g.value(b, e)
}

// emitStrChar lowers `base[i]` where base is a string the compiler cannot read: one-character
// strings are interned, so the answer is an ordinary string value — printable, comparable,
// subscriptable again. Out of range is a raise, not a value: no index means "no character".
func (g *irGen) emitStrChar(b *strings.Builder, base Expr, idx Expr, sp Span) (string, bool, error) {
	reg, isStr, err := g.strReg(b, base)
	if err != nil || !isStr {
		return "", isStr, err
	}
	pos, err := g.strPosReg(b, idx)
	if err != nil {
		return "", true, err
	}
	ch := g.rtStrCall(b, "rt_str_char", "i32 "+reg, "i32 "+pos)
	g.checkStrSentinels(b, ch, "IndexError", "string index out of range", sp, "stridx")
	return ch, true, nil
}

// strFullMessage is what the runtime says when the string table is exhausted: a limit of the
// compiled backend, phrased as the condition rather than as a toolchain failure (ADR 0166).
const strFullMessage = "the program created too many distinct string values"

// checkStrSentinels turns the runtime's two "no value" answers into raises. -1 is the shape's
// own failure (no such character, not one code point); -2 is the shared limit. Both are typed
// raises through the ordinary unwind path, so a program can catch either (ADR 0212), and the
// class is what a handler matches on (ADR 0214) — nothing here prints a sentence and carries on.
func (g *irGen) checkStrSentinels(b *strings.Builder, val, class, kind string, sp Span, tag string) {
	full := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, -2\n", full, val))
	g.branchRaise(b, full, "RuntimeError", strFullMessage, sp, tag+".full")
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, -1\n", bad, val))
	g.branchRaise(b, bad, class, kind, sp, tag)
}

// scanStringBindings records the names that hold @str_tab indices because the program binds
// them to string-valued expressions, and the functions whose returns are string-valued. The
// question is the same one the operations ask — "is this a string?" — asked of the whole
// program before any of it is lowered, so that a use after the binding sees the kind the
// binding gave it (`s = get()` then `s[1]`), and so that a call site knows what its callee
// hands back (`print(f("abc"))` where `f` returns `s[1]`).
//
// This used to be decided only by annotations and literal returns, which is why the same
// expression printed text in one position and an index in another.
func (g *irGen) scanStringBindings(stmts []Stmt) {
	for _, st := range stmts {
		switch n := st.(type) {
		case *AssignStmt:
			if nm, ok := n.Target.(*Name); ok && g.exprIsString(n.Value) {
				g.internedVars[nm.Value] = true
			}
			for _, t := range []Expr{} {
				_ = t
			}
			if tl, ok := n.Target.(*Tuple); ok {
				for _, te := range tl.Elems {
					if nm, ok2 := te.(*Name); ok2 && g.exprIsString(n.Value) {
						g.internedVars[nm.Value] = true
					}
				}
			}
		case *ExprStmt:
			// nothing to bind; still walked for nested function definitions
		case *FuncDef:
			// An async function hands back a coroutine handle, not its eventual value, so it
			// is never "a function that returns a string index" from the call site's point of
			// view — marking one made a caller read a handle as an index and llc rejected the
			// module (the async repro, caught by the suite, ADR 0166's rule about blaming the
			// compiler). Its body is still walked, because a def inside it can be ordinary.
			if !n.Async && (methodReturnsStr(n) || returnsStringExpr(g, n)) {
				g.strFuncs[n.Name] = true
			}
			g.scanStringBindings(n.Body)
		case *IfStmt:
			g.scanStringBindings(n.Then)
			for _, el := range n.Elifs {
				g.scanStringBindings(el.Then)
			}
			g.scanStringBindings(n.Else)
		case *WhileStmt:
			g.scanStringBindings(n.Body)
			g.scanStringBindings(n.Else)
		case *ForStmt:
			g.scanStringBindings(n.Body)
			g.scanStringBindings(n.Else)
		case *TryStmt:
			g.scanStringBindings(n.Body)
			for _, ec := range n.Excepts {
				g.scanStringBindings(ec.Body)
			}
			g.scanStringBindings(n.Finally)
		case *WithStmt:
			g.scanStringBindings(n.Body)
		case *MatchStmt:
			for _, c := range n.Cases {
				g.scanStringBindings(c.Body)
			}
		}
	}
}

// returnsStringExpr answers "does this function hand back a string index?" from the shape of
// its returns, using the same predicate the operations ask. The annotation and literal cases
// are methodReturnsStr's; this is the rest — a subscript of a string, a char method, a call
// to a function already known to return one.
func returnsStringExpr(g *irGen, fd *FuncDef) bool {
	strParams := map[string]bool{}
	for _, p := range fd.Params {
		if p.Annot != nil && p.Annot.Kind == KindString {
			strParams[p.Name] = true
		}
	}
	found := false
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch n := st.(type) {
			case *ReturnStmt:
				if n.Expr == nil {
					continue
				}
				if isStrUnder(g, n.Expr, strParams) {
					found = true
				}
			case *IfStmt:
				walk(n.Then)
				for _, el := range n.Elifs {
					walk(el.Then)
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
			case *ForStmt:
				walk(n.Body)
			case *TryStmt:
				walk(n.Body)
				for _, ec := range n.Excepts {
					walk(ec.Body)
				}
			}
		}
	}
	walk(fd.Body)
	return found
}

// isStrUnder asks the string question with one extra piece of evidence: the names in `params`
// hold indices. A function's own parameters are the one set of names whose kind is known
// before internedVars has been populated for them.
func isStrUnder(g *irGen, e Expr, params map[string]bool) bool {
	switch v := e.(type) {
	case *StrLit, *FString:
		return true
	case *Name:
		if params[v.Value] || g.internedVars[v.Value] {
			return true
		}
		// A name the compiler holds as text is a string too — `def f(i): s = "abc"; return s[i]`
		// binds s to a constant, and the answer is still a one-character string.
		_, known := g.stringVal(v)
		return known
	case *Index:
		return isStrUnder(g, v.Obj, params)
	case *Call:
		if g.callReturnsStr(v) {
			return true
		}
		if at, ok := v.Fn.(*Attr); ok && isStrUnder(g, at.Obj, params) {
			switch at.Name.Value {
			case "upper", "lower":
				return true
			}
		}
		return false
	case *BinOp:
		return v.Op == "+" && isStrUnder(g, v.L, params) && isStrUnder(g, v.R, params)
	}
	return false
}
