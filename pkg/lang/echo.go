package lang

import (
	"fmt"
	"strings"
)

// The REPL's echo, compiled.
//
// An interactive caller asks a question — `1 + 1`, `"hi".upper()` — and wants the *value*
// back, not only the bytes the program printed. Until ADR 0302 that answer came from the AST
// interpreter, which held the value in a Go variable and could simply print it. The compiled
// backend has no such variable: the answer lives in a word inside the program it built. So the
// answer is emitted the way the backend emits every other fact about a running program — as one
// self-reporting line on the tool channel (fd 2), which the host parses and presents as the
// result. It is the same channel, and the same design, as `--gc-stats` (ADR 0179/0181) and the
// uncaught-exception report (ADR 0211): the program's stdout stays the program's, and everything
// the tool says about the run is separable from it.
//
// Two rules keep this honest:
//
//   - A *program* is never echoed. Only a snippet whose caller asked for it (`--eval`, a REPL
//     turn) is, and only when its final statement is a bare expression — the ADR 0204 rule, so
//     `gustyc --file prog.gy` and `./prog` keep printing exactly what prog prints.
//   - The rendering is `renderPair` in render.go — the one str/repr table the language has
//     (ADR 0258). The echo may not invent a third rendering, and it may not turn a snippet the
//     backend can compile into one it cannot: when no form is named for the expression, the
//     expression is still evaluated for its effects and nothing is echoed, exactly as before
//     the echo existed. A courtesy never outranks the compile.
const echoReportPrefix = "gusty: result "

// echoTarget is the statement whose value a snippet caller asked for: the program's last
// statement, and only when it is a bare expression. Everything else — a program, a snippet
// ending in a statement, an empty snippet — gets no echo.
func (g *irGen) echoTarget(prog *Program) Stmt {
	if !g.echoOn || prog == nil || len(prog.Stmts) == 0 {
		return nil
	}
	last := prog.Stmts[len(prog.Stmts)-1]
	if _, ok := last.(*ExprStmt); !ok {
		return nil
	}
	return last
}

// echoStmt lowers one snippet's final expression: render it through the str/repr pair, hand the
// text and the kind name to the module's writer, and be silent where the pair names no form.
//
// The form is str, not repr, because that is the answer the REPL gave before this existed: the
// retired engine echoed a top-level string bare (`cba`, not `'cba'`) while quoting strings inside
// containers, which is exactly what the pair's str form does. An echo that changed the REPL's
// spelling to make its own plumbing simpler would be the courtesy ruining the product.
// echoPureBuiltins are the callables whose call lowers to a value with no footprint on the world: no
// print, no allocation the program can observe, no trap the program did not already take on the road
// that built the statement. For those, the echo lowering the expression a second time duplicates
// nothing a user can see — the REPL prints one line either way — so the prompt keeps answering
// `str([1, 2])` with `[1, 2]` and `repr("hi")` with `'hi'`.
//
// The set is a list of *names*, deliberately: the moment a program shadows one (`def str(x): print(x)`)
// the callee is a user function and the check below refuses it, because a name in this table only
// counts when the compiler can see that nothing in the program binds it.
var echoPureBuiltins = map[string]bool{
	"str": true, "repr": true, "len": true, "abs": true, "min": true, "max": true,
	"int": true, "float": true, "bool": true, "ord": true, "chr": true,
	"hex": true, "bin": true, "oct": true, "sum": true, "round": true, "sorted": true,
	"list": true, "set": true, "tuple": true, "dict": true, "hash": true,
}

// echoIsPureBuiltin reports whether a call is to one of those names *as a builtin* — a plain named
// callee, no receiver, and not a name the program itself defines or binds.
func (g *irGen) echoIsPureBuiltin(c *Call) bool {
	name, ok := c.Fn.(*Name)
	if !ok || !echoPureBuiltins[name.Value] {
		return false
	}
	// A program that defines `str` itself has taken the name back, and then the call is a user call
	// with whatever footprint its body has. The compiler can see which names it binds, so the question
	// is answered rather than assumed.
	// A program that binds the name itself — `str = 3`, or `def str(x): ...` — has taken it back, and
	// then this is a user call with whatever footprint that binding's value has. Both are visible to
	// the compiler, so the question is answered rather than assumed.
	return !g.funcs[name.Value] && !g.allocd[name.Value]
}

func (g *irGen) echoStmt(b *strings.Builder, e Expr) error {
	// The void first, and on its own terms. A snippet that ends with a call handing back None has
	// no answer the reader did not already see: the REPL printed nothing for it, and `--json` said
	// `"result": null, "type": "None"`. Asking the pair for a form would render the word `None`,
	// which is what `str(None)` prints and not what a REPL shows after a call that returned
	// nothing — and, worse, the pair's container probe would lower the call a second time, so
	// `def f(): print("hi")` / `f()` announced "hi" twice on its way to announcing a None nobody
	// asked for. The effects run exactly once; the line says the value was the void.
	if g.isNoneExpr(e) || isBarePrintCall(e) {
		if _, isLit := e.(*NoneLit); !isLit {
			if _, err := g.value(b, e); err != nil {
				return err
			}
		}
		g.echoUsed = true
		kind := g.echoKindConst("None")
		empty := g.internStr(b, "")
		fmt.Fprintf(b, "  call void @rt_echo_value(i32 %s, i8* %s)\n", empty, kind)
		return nil
	}
	// A call-shaped tail gets no echo, and the reason is effects, not kinds. Rendering a value through
	// the str/repr table needs the value in hand, but this runs *after* the statement, and the call's
	// value was produced on a road that has already been paved — so rendering means calling again, and
	// a function whose body prints prints twice. It did exactly that: `show(x)` as the last line of a
	// snippet repeated its whole output (roadmap Gap R.190, found the day the echo shipped). A prompt
	// that repeats a program's effects to report what the program produced is worse than a prompt that
	// stays quiet, so a call is evaluated for its effects and nothing is announced.
	//
	// What that costs: the retired engine echoed `f(21)` as `42`. Roadmap L13.1 owes the way back —
	// hoist the final call into a slot of the callee's own declared return type and report that value
	// once, from the road the call already ran — and it needs the same tagged value word as print,
	// str() and the calling side (roadmap L11.1), because the switch below reads i32 boxes only.
	if c, isCall := e.(*Call); isCall && !g.echoIsPureBuiltin(c) {
		_, verr := g.value(b, e)
		return verr
	}
	// A value the container answers for gets its kind from the tag, at run time, from the same table the
	// operand-type messages read. The form table below can name the shape but not the kind of a slot, and
	// `gusty: result object True` is a prompt describing its own blind spot (roadmap L13.1).
	if p, t, okPair := g.pairForEcho(b, e); okPair {
		g.echoUsed = true
		fmt.Fprintf(b, "  call void @rt_echo_pair(i32 %s, i32 %s, i32 0)\n", p, t)
		return nil
	}
	out, handled, err := g.renderPair(b, e, FormStr, e.Span())
	if err != nil {
		// The pair found a form and the road to it failed (a refusal inside a container build,
		// say). That is the program's own error, and the echo may not hide it.
		return err
	}
	if !handled {
		// No form named: evaluate for effects and echo nothing. The snippet compiled yesterday
		// with no echo at all; it must not fail to compile today because a courtesy was asked for.
		_, verr := g.value(b, e)
		return verr
	}
	g.echoUsed = true
	kind := g.echoKindName(b, e)
	fmt.Fprintf(b, "  call void @rt_echo_value(i32 %s, i8* %s)\n", out, kind)
	return nil
}

// echoKindName returns the pointer to the C string naming the value's kind — the answer to
// `type(...)`, and the `type` member of the CLI's JSON result.
//
// It is the kind the *expression* can prove, in the same order renderPair distinguishes forms
// (a container first, because that is the form the number formatter used to swallow). When the
// expression proves nothing the answer is the honest one, `object`, rather than a guess at a
// family: a wrong `type` is exactly the wrong-answer-at-exit-0 the roadmap keeps filing (Gap
// R.38's rule — a diagnostic describes what is true).
func (g *irGen) echoKindName(b *strings.Builder, e Expr) string {
	kind := g.staticEchoKind(e)
	return g.echoKindConst(kind)
}

// staticEchoKind classifies an expression by what the compiler can see, in the order the pair
// table asks its questions.
func (g *irGen) staticEchoKind(e Expr) string {
	if nm, isName := e.(*Name); isName && g.numericPairVar(nm.Value) {
		// A name the arithmetic door bound carries its family in the tag, which only the run
		// time can read. Saying "number" is the truth the compiler is allowed to tell.
		return "number"
	}
	if g.isNoneExpr(e) {
		return "NoneType"
	}
	if g.printsAsBool(e) {
		return "bool"
	}
	if g.exprIsString(e) {
		return "str"
	}
	if g.isFloat(e) {
		return "float"
	}
	if kind := containerKindFromTy(exprTyName(e)); kind != "" {
		return kind
	}
	if nm, ok := e.(*Name); ok {
		switch {
		case g.listVars[nm.Value], g.mixedLists[nm.Value], g.taggedVars[nm.Value]:
			return "list"
		case g.runtimeDicts[nm.Value], g.mixedDicts[nm.Value]:
			return "dict"
		case g.runtimeSets[nm.Value], g.mixedSets[nm.Value]:
			return "set"
		}
	}
	if isContainerLiteral(e) {
		switch e.(type) {
		case *ListLit:
			return "list"
		case *DictLit:
			return "dict"
		case *SetLit:
			return "set"
		}
	}
	if _, ok := g.foldConstInt(e); ok {
		return "int"
	}
	if g.strArgIsNumberish(e) {
		return "int"
	}
	return "object"
}

// echoKindConst interns one kind name as a private global, so the module writes the kind without
// the writer having to know the vocabulary.
func (g *irGen) echoKindConst(kind string) string {
	if name, ok := g.echoKinds[kind]; ok {
		return name
	}
	g.fmtIdx++
	name := fmt.Sprintf("@.echokind%d", g.fmtIdx)
	g.strGlobals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(kind)+1, kind))
	if g.echoKinds == nil {
		g.echoKinds = map[string]string{}
	}
	g.echoKinds[kind] = name
	return name
}

// --- the host half: reading the answer back ---------------------------------------------------

// EchoResult is what a snippet's echo reported: the kind the value is, and the repr the pair
// rendered.
type EchoResult struct {
	Kind string `json:"kind"`
	Repr string `json:"repr"`
}

// ParseEchoLine finds the echo line in what the target wrote to fd 2 and returns it. The LAST
// such line wins: a snippet is one expression, and a program that somehow produced the line
// twice (a loop around an echoed call is impossible today, but a refusal to be silent about it
// is cheap) reports the final answer rather than the first.
func ParseEchoLine(stderr string) (EchoResult, bool) {
	var found EchoResult
	ok := false
	for _, line := range strings.Split(stderr, "\n") {
		if !strings.HasPrefix(line, echoReportPrefix) {
			continue
		}
		rest := line[len(echoReportPrefix):]
		kind, repr, split := strings.Cut(rest, " ")
		if !split {
			kind, repr = "object", rest
		}
		found, ok = EchoResult{Kind: kind, Repr: repr}, true
	}
	return found, ok
}

// StripEchoLine removes the echo line from a tool-channel transcript, so the answer is shown as
// a result and not also as stderr.
func StripEchoLine(stderr string) string {
	if !strings.Contains(stderr, echoReportPrefix) {
		return stderr
	}
	kept := make([]string, 0, 8)
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, echoReportPrefix) {
			continue
		}
		kept = append(kept, line)
	}
	s := strings.Join(kept, "\n")
	for strings.HasSuffix(s, "\n\n") {
		s = strings.TrimSuffix(s, "\n")
	}
	return s
}

// --- the target half: the writer in the module ------------------------------------------------

// echoRuntimeIR is the module's writer. It takes the interned repr the pair built and the kind
// name the compiler interned beside it, and writes one self-reporting line to fd 2 — the same
// descriptor `rt_die` and `rt_gc_report` use, for the same reason: the program's own stdout is
// not the tool's to write into.
const echoRuntimeIR = `
@rt.echo.hdr = private unnamed_addr constant [15 x i8] c"gusty: result \00"
@rt.echo.sp = private unnamed_addr constant [2 x i8] c" \00"
@rt.echo.nl = private unnamed_addr constant [2 x i8] c"\0A\00"

define internal void @rt_echo_value(i32 %idx, i8* %kind) {
entry:
  %p = call i8* @rt_str_ptr(i32 %idx)
  %n32 = call i32 @rt_str_len(i32 %idx)
  %n = zext i32 %n32 to i64
  %kl = call i64 @strlen(i8* %kind)
  call i64 @write(i32 2, i8* getelementptr inbounds ([15 x i8], [15 x i8]* @rt.echo.hdr, i32 0, i32 0), i64 14)
  call i64 @write(i32 2, i8* %kind, i64 %kl)
  call i64 @write(i32 2, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.echo.sp, i32 0, i32 0), i64 1)
  call i64 @write(i32 2, i8* %p, i64 %n)
  call i64 @write(i32 2, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.echo.nl, i32 0, i32 0), i64 1)
  ret void
}

; rt_echo_pair announces a value whose kind is a RUN-TIME fact: the (payload, tag) pair a container
; slot read produced. The kind is asked of the tag — the same table the operand-type messages read —
; rather than guessed by the compiler, because a slot that holds True today holds 1 tomorrow and a
; prompt that says the word object for both is reporting the compiler's blind spot instead of the
; program's value (roadmap ADR 0259's rule for what a slot IS, applied to the prompt; ADR 0302's echo).
define internal void @rt_echo_pair(i32 %v, i32 %t, i32 %quote) {
entry:
  store i32 0, i32* @rt_cap_len
  store i32 1, i32* @rt_capturing
  call void @rt_print_mixed_value(i32 %v, i32 %t, i32 %quote)
  %n = load i32, i32* @rt_cap_len
  store i32 0, i32* @rt_capturing
  %buf = bitcast [65536 x i8]* @rt_cap to i8*
  %kind = call i8* @rt_kind_name(i32 %t)
  %kl = call i64 @strlen(i8* %kind)
  %nl64 = zext i32 %n to i64
  call i64 @write(i32 2, i8* getelementptr inbounds ([15 x i8], [15 x i8]* @rt.echo.hdr, i32 0, i32 0), i64 14)
  call i64 @write(i32 2, i8* %kind, i64 %kl)
  call i64 @write(i32 2, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.echo.sp, i32 0, i32 0), i64 1)
  call i64 @write(i32 2, i8* %buf, i64 %nl64)
  call i64 @write(i32 2, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.echo.nl, i32 0, i32 0), i64 1)
  ret void
}
`

// isBarePrintCall reports that a snippet's last statement is a call to `print` written as an
// expression statement. It is the void case the reader has already seen: the program's line went to
// stdout, and the call itself hands back nothing. Classifying it here rather than exempting it in
// echoTarget is what lets `--json` still answer the question the retired engine answered — the
// program's value was the void — while the human sees nothing extra on the terminal.
//
// `print` is asked by name because that is the one builtin whose value is guaranteed to be the void
// here; a user function called at the top level still goes through the pair, which names a form for
// whatever it returns and stays silent when it cannot.
func isBarePrintCall(e Expr) bool {
	c, ok := e.(*Call)
	return ok && calleeName(c) == "print"
}
