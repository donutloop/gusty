package lang

// printf-style formatting on a text: `"hi %s" % name`, `"%10.2f" % v`, `"%d-%d" % (a, b)`.
//
// The operator has been refused in words since ADR 0215, and the refusal was right about the
// arithmetic and wrong about the language: a compiled text is an index into @str_tab, so `%` on one
// is not a remainder — but CPython does not do a remainder either. It formats. Refusing was the
// honest answer to a missing operation, and the missing operation is this file: a plan (what the
// format asks for, what the program handed it) and one runtime door (pkg/lang/percent_runtime.go,
// rt_format) that walks the format and renders each argument through the printers the module already
// has (roadmap Gap R.31, L11.1, ADR 0360).
//
// Three decisions are worth their words:
//
//   - The plan is asked ONCE, by the doors that decide a kind and by the road that lowers, through
//     percentPlan. A `print` that took the text path while `value()` took the numeric one is the
//     Gap R.146 family all over again, and ADR 0359's census-vs-builder lesson says the same thing
//     twice is two answers waiting to disagree.
//   - An argument is lowered through the PAIR doors — the (payload, tag) words every printer,
//     comparison and container in this runtime already reads (ADR 0232's slots-are-the-truth). A
//     `%d` and a `%s` of the same slot therefore ask the same object the same question, and a
//     `None`, a float box or a container's handle cannot arrive as a bare number the way
//     ADR 0359's f-string field found the print road doing.
//   - The argument list is a list of values, NOT a tuple standing where a value goes. Tuples are
//     still not values in this backend (L11.3), and this door does not make them one: the tuple the
//     program wrote is consumed here as the argument list and nothing else ever sees it. A tuple in
//     any other position keeps refusing exactly as it did.
//
// What the door declines, it says why: a format character this backend does not implement, a format
// whose arguments it cannot give kinds to, more arguments than the tables hold. What the door can
// only learn while the program runs, it raises — the reference's own class and sentence, catchable
// (ADR 0212, ADR 0214).

import (
	"fmt"
	"strconv"
	"strings"
)

// percentArgLimit is @rt.pct.argp's length in percent_runtime.go. One number, read by the gate and
// by the store loop, so a widening that forgets one of the two is a store past the end of a global.
const percentArgLimit = 64

// percentPlan is what this backend knows about `fmt % args` before it emits anything.
type percentPlan struct {
	applies bool   // the shape is formatting this door lowers, not a numeric remainder
	args    []Expr // the arguments, in the order the conversions will take them
	isMap   bool   // the single argument is a mapping, so %(key)s may ask it
	refusal string // why the door declines, in the words the reader gets
	dynamic bool   // the format text is not a compile-time constant
	format  string // the format text, when it is
}

// percentFormatOf asks the plan of a `%` expression. It is the ONE question: the kind doors and the
// lowering both call it, and neither keeps its own idea of what makes a format a format.
func (g *irGen) percentFormatOf(n *BinOp) percentPlan {
	if n == nil || n.Op != "%" {
		return percentPlan{}
	}
	// A text on the left is the whole question. The reference dispatches on the left operand's type,
	// and every position a compiled text answers is one of these four predicates (ADR 0229's rule
	// that print and the operation ask one question, ADR 0359's extension of it to an interpolation).
	if !g.percentOperandIsText(n.L) {
		return percentPlan{}
	}
	plan := percentPlan{applies: true, dynamic: true}
	// The right operand is either ONE value or the tuple the program wrote as an argument list. A list
	// on the right is ONE value too: the reference refuses to format against a list, and the door's
	// conversion refuses it by kind, which is the reference's answer rather than a second one.
	//
	// The tuple spelling is the `Tuple` flag on the container node (roadmap L11.3, ADR 0361) as much as
	// the pattern node it used to be: `"%d %d" % (1, 2)` spreads, and `"%s" % [1, 2]` does not, because
	// the flag is the difference between the two spellings and nothing else.
	if t, ok := n.R.(*Tuple); ok {
		plan.args = t.Elems
	} else if ln, isList := n.R.(*ListLit); isList && ln.IsTuple() {
		plan.args = ln.Elems
	} else {
		plan.args = []Expr{n.R}
		plan.isMap = g.percentArgIsMapping(n.R)
	}
	if len(plan.args) > percentArgLimit {
		plan.refusal = fmt.Sprintf("`%s` asks for %d arguments, and this door's table holds %d — the reference has no such limit (roadmap Gap R.31, ADR 0360)",
			exprSurface(n), len(plan.args), percentArgLimit)
		return plan
	}
	if txt, ok := stringConst(n.L, g.builtinShadowed); ok {
		plan.dynamic = false
		plan.format = txt
		if bad, why := percentLiteralRefusal(txt); bad != "" {
			// The character is quoted as the program wrote it — `%b`, not `b` — because that is the string
			// a reader searches the source for, and a refusal that names its subject in a different shape
			// than the source spells it is a refusal that has to be decoded first (ADR 0181's wording rule).
			plan.refusal = fmt.Sprintf("`%s` asks for the `%%%s` conversion, which this backend does not build: %s (roadmap Gap R.31, ADR 0360)",
				exprSurface(n), bad, why)
			return plan
		}
	}
	asksChar := false
	if !plan.dynamic {
		asksChar = percentAsksChar(plan.format)
	}
	for _, a := range plan.args {
		if il, isInt := a.(*IntLit); isInt && asksChar && il.Value >= 0xD800 && il.Value <= 0xDFFF {
			// A surrogate has no UTF-8 encoding, so a `%c` of one has nothing to render: the reference raises
			// UnicodeEncodeError, a class this backend's table does not hold yet. Printing the bytes the encoder
			// would have refused is the near-answer ADR 0166 counts as the compiler's bug, so the shape the
			// compiler can SEE is refused in words; a code point that only arrives at run time keeps the door's
			// answer and is the debt row (roadmap Gap R.31, ADR 0360).
			plan.refusal = fmt.Sprintf("`%s` formats the code point `\\u%04x` with %%c, and a surrogate has no UTF-8 encoding to render: the reference raises UnicodeEncodeError there, a class this backend's table does not hold yet (roadmap Gap R.31, ADR 0360)",
				exprSurface(n), il.Value)
			return plan
		}
		if !g.percentArgKindKnown(a) {
			plan.refusal = fmt.Sprintf("`%s` formats %s, whose kind this backend cannot name — a conversion renders a value, and a value whose kind nothing carries would be printed as the number underneath it (roadmap Gap R.31, L11.1, ADR 0360)",
				exprSurface(n), exprSurface(a))
			return plan
		}
	}
	return plan
}

// percentFormatOwnsBinOp is the version of the plan question that a numeric predicate can ask without
// recursing. `isFloat` of `"%s" % 3.5` reads both operands to decide, and the format operand's own text
// question comes back through `exprIsString` → `printsAsInternedStr` → the plan — the same node. This form
// settles on the format arm and the shapes the compiler can read as text without leaving this one: a text
// literal, an interpolation, and a name the binder recorded as a text (roadmap Gap R.31, ADR 0360).
func (g *irGen) percentFormatOwnsBinOp(n *BinOp) bool {
	if percentFormatShapeIsText(n) {
		return true
	}
	if n.Op != "%" {
		return false
	}
	if l, isName := n.L.(*Name); isName {
		if _, isText := g.strVals[l.Value]; isText {
			return true
		}
		return g.internedVars[l.Value]
	}
	return false
}

// percentOperandIsText is the text question for the left of `%`. It is deliberately NOT
// g.exprIsString, which now answers yes for a `%`-formatting: the left side of one is the format,
// and asking the same predicate about it would let a nested formatting ask itself.
func (g *irGen) percentOperandIsText(e Expr) bool {
	if _, ok := g.stringVal(e); ok {
		return true
	}
	if g.exprIsString(e) || g.printsAsInternedStr(e) {
		return true
	}
	if nm, ok := e.(*Name); ok {
		if _, isStr := g.strVals[nm.Value]; isStr {
			return true
		}
	}
	return false
}

// percentArgIsMapping asks whether the single argument is the dict the mapping-key form may read.
// It is asked of the records, not of a spelling: `d = {"a": 1}` and `{"a": 1}` are the same mapping
// to the reference, and the door reads %(key)s from whichever the program wrote.
func (g *irGen) percentArgIsMapping(e Expr) bool {
	if _, ok := e.(*DictLit); ok {
		return true
	}
	if kind, ok := g.staticContainerKind(e); ok {
		return kind == "dict"
	}
	return false
}

// percentArgKindKnown is the side-effect-free half of the pair question. It declines only what no
// door can answer at all — a tuple, a lambda or a generator standing where a value goes, which this
// backend has no value for (L11.3) — and leaves everything else to the pair doors themselves, each
// of which declines without emitting when it cannot name the kind. A plan that is later refused is a
// refusal either way; what must never happen is a plan that is ACCEPTED while the value goes out as
// the number underneath it (roadmap L11.1's rule, ADR 0359's version of it).
func (g *irGen) percentArgKindKnown(e Expr) bool {
	switch e.(type) {
	case *Tuple, *Lambda, *Generator:
		return false
	}
	if nm, isName := e.(*Name); isName {
		// A name is the one argument shape whose kind the source does not write, so it is the one that has to
		// be answered by a record: the pair road's, or the shape record the numeric and text roads read. A
		// name with neither is the position whose payload would be printed as the number underneath it
		// (roadmap L11.1's rule, ADR 0359's version of it, Gap R.31, ADR 0360). These are the SAME predicates
		// `percentArgPair` lowers by, so a plan a caller never declines for a reason the emitter disagrees
		// with (ADR 0229's one-predicate rule).
		if g.taggedVars[nm.Value] || g.numericPairVar(nm.Value) {
			return true
		}
		switch k := g.numericUseKind(e); k {
		case "int", "float":
			return true
		case "":
		default:
			return false
		}
		return g.percentOperandIsText(e) || g.isContainerExpr(nm)
	}
	return true
}

// percentAsksChar reports that a format holds a `%c` conversion — the one conversion whose argument is a
// code point rather than a number, and therefore the one whose argument the compiler has to ask about
// separately (roadmap Gap R.31, ADR 0360).
func percentAsksChar(format string) bool {
	for i := 0; i+1 < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		if format[i+1] == '%' {
			i++
			continue
		}
		for j := i + 1; j < len(format); j++ {
			switch format[j] {
			case '#', '0', '-', ' ', '+', '.', '*', '1', '2', '3', '4', '5', '6', '7', '8', '9', '$', 'L', 'h', 'l':
				continue
			case 'c':
				return true
			default:
				j = len(format) // this conversion is not a %c; stop scanning it
			}
		}
	}
	return false
}

// percentLiteralRefusal is the compile-time half of the honesty rule: a conversion this backend does
// not build is named in the source it was written in, rather than reaching a run-time door that
// would answer the wrong thing. A format the compiler cannot read keeps its run-time answer, because
// a refusal is a compile-time act and the program may never run that branch.
func percentLiteralRefusal(format string) (string, string) {
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i >= len(format) {
			return "", "" // the incomplete-format ValueError is the door's, at run time
		}
		if format[i] == '%' || format[i] == '(' {
			continue // a literal percent and a mapping key are both answered by the door
		}
		for i < len(format) && (strings.IndexByte("-+ #0", format[i]) >= 0) {
			i++
		}
		for i < len(format) && format[i] >= '0' && format[i] <= '9' {
			i++
		}
		if i < len(format) && format[i] == '.' {
			i++
			for i < len(format) && (format[i] == '*' || (format[i] >= '0' && format[i] <= '9')) {
				i++
			}
		}
		if i >= len(format) {
			return "", ""
		}
		switch c := format[i]; c {
		case 's', 'r', 'a', 'd', 'i', 'o', 'x', 'X', 'e', 'E', 'f', 'F', 'g', 'G', 'c':
			continue // every one of these has a renderer in percent_runtime.go
		case 'u', 'b', 'B':
			return string(c), "`%b` is binary and `%u` is a no-op the reference removed — neither has a " +
				"compiled rendering yet (`%a` does: it renders through the printer, as ascii() of the value does)"
		case 'p':
			return "p", "`%p` is not a conversion the reference implements either; the door raises its " +
				"unsupported-format-character ValueError"
		default:
			return "", "" // an unsupported character is the reference's ValueError, and the door raises it
		}
	}
	return "", ""
}

// percentFormat lowers `fmt % args`: fill the door's argument table, ask it, and raise what it
// raises when the program asked for something the format cannot give.
func (g *irGen) percentFormat(b *strings.Builder, n *BinOp) (string, error) {
	plan := g.percentFormatOf(n)
	if !plan.applies {
		return "", fmt.Errorf("codegen: `%s` is not a formatting this door lowers (roadmap Gap R.31, ADR 0360)", exprSurface(n))
	}
	if plan.refusal != "" {
		return "", fmt.Errorf("codegen: %s", plan.refusal)
	}
	g.heapUsed = true
	g.arithUsed = true
	g.percentUsed = true
	fmtReg, ok, err := g.strReg(b, n.L)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("codegen: the format of `%s` is not a text this backend can hand to the formatter (roadmap Gap R.31, ADR 0360)", exprSurface(n))
	}
	// Every argument is lowered BEFORE any of them lands in the table, because lowering one can itself
	// format: `"%s %s" % ("%d" % 1, "%s" % "two")` lowers its first argument with a call to the very door
	// the outer expression is about to call, and a store made before that call would be overwritten by it.
	// The table is module-wide state; this ordering is what keeps a nested format from spending its parent's
	// arguments (roadmap Gap R.31, found by the nested-formatting row in percent_answer_test.go).
	type percentArgWord struct{ payload, tag string }
	words := make([]percentArgWord, 0, len(plan.args))
	for _, a := range plan.args {
		payload, tag, pok := g.percentArgPair(b, a)
		if !pok {
			return "", fmt.Errorf("codegen: `%s` formats %s, whose (value, kind) pair this backend cannot build — the formatter renders a value and answers its kind, and one word alone would be the number underneath it (roadmap Gap R.31, L11.1, ADR 0360)",
				exprSurface(n), exprSurface(a))
		}
		words = append(words, percentArgWord{payload, tag})
	}
	for i, w := range words {
		fmt.Fprintf(b, "  store i32 %s, i32* getelementptr ([%d x i32], [%d x i32]* @rt.pct.argp, i32 0, i32 %d)\n",
			w.payload, percentArgLimit, percentArgLimit, i)
		fmt.Fprintf(b, "  store i32 %s, i32* getelementptr ([%d x i32], [%d x i32]* @rt.pct.argt, i32 0, i32 %d)\n",
			w.tag, percentArgLimit, percentArgLimit, i)
	}
	isMap := 0
	if plan.isMap {
		isMap = 1
	}
	res := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_format(i32 %s, i32 %d, i32 %d)\n", res, fmtReg, len(plan.args), isMap)
	g.percentChecks++
	bad := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, -1\n", bad, res)
	g.markI1(bad)
	badL, okL := g.newLabel("pctraise"), g.newLabel("pctok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseRuntimeFormat(b, n.Span())
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	return res, nil
}

// percentArgPair lowers one argument to the two words the formatter reads. It tries the pair doors
// in the order the rest of the backend tries them — a name the arithmetic door or a mixed loop
// bound carries its own tag, and anything else is a value whose kind the compiler can prove.
func (g *irGen) percentArgPair(b *strings.Builder, e Expr) (string, string, bool) {
	if p, t, ok := g.pairForEcho(b, e); ok {
		return p, t, true
	}
	if nm, isName := e.(*Name); isName {
		// A name's kind is not written in the source, so the pair road is the door that knows whether `v`
		// holds a float or a text, and the call sites are what open it. The element-kind door answers "int"
		// for every name it has no record of, and the number that guess builds is wrong rather than missing:
		// `def f(v): return "%.2f" % v` called as `f(3.5)` printed `3.00` at exit 0, and
		// `def f(v): return "%s" % v` printed `3` for `3.5` — Gap P.1's truncated half, arriving through a
		// formatting (roadmap Gap R.31, L11.1, ADR 0360).
		if g.taggedVars[nm.Value] {
			if p, t, ok := g.taggedOperand(b, e); ok {
				return p, t, true
			}
		}
		// What the records DO prove is taken: `numericUseKind` is the same shape question the arithmetic
		// road asks before it emits an `add`, so a name it calls an int is an int here too, and a name it
		// calls a float is boxed by the float arm of the pair door.
		switch k := g.numericUseKind(e); k {
		case "int", "float":
			if p, t, ok := g.taggedOperand(b, e); ok {
				return p, t, true
			}
		case "":
		default:
			return "", "", false
		}
		if g.percentOperandIsText(e) {
			// A name the records call a text answers the interned index beside the str tag — the same pair a
			// text literal answers, from the same door (roadmap Gap R.42, ADR 0224, Gap R.31).
			reg, ok, err := g.strReg(b, e)
			if err != nil || !ok {
				return "", "", false
			}
			return reg, strconv.FormatInt(int64(TagStr), 10), true
		}
		return "", "", false
	}
	if p, t, ok := g.taggedOperand(b, e); ok {
		return p, t, true
	}
	if p, t, ok := g.runtimeSlotPairDeep(b, e); ok {
		return p, t, true
	}
	return "", "", false
}

// raiseRuntimeFormat is setExn with both halves coming from the formatter: the class it chose
// (@rt.pct.err, one of the codes in exceptions.go, which is what the handler dispatch compares) and
// the sentence it wrote (@rt.pct.msg). The compiler supplies nothing about either except where the
// raise happened — which is the half only the compiler knows (ADR 0212, ADR 0214, ADR 0354's frame).
func (g *irGen) raiseRuntimeFormat(b *strings.Builder, sp Span) {
	g.raiseUsed = true
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	code := g.newTmp()
	b.WriteString("  " + code + " = load i32, i32* @rt.pct.err\n")
	b.WriteString("  store i32 " + code + ", i32* @exn_code\n")
	msg := g.newTmp()
	b.WriteString("  " + msg + " = call i8* @rt_pct_msg_ptr()\n")
	b.WriteString("  store i8* " + msg + ", i8** @exn_msg\n")
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
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
}

// percentIsTextExpr is the kind claim the print road, the binding road and the container census ask,
// written once and read by both (ADR 0229's one-predicate rule, ADR 0359's version of it). A format
// answers an @str_tab index — the same word a text literal answers — so every road that reads
// @str_tab takes it without knowing what built it.
func (g *irGen) percentIsTextExpr(e Expr) bool {
	n, ok := e.(*BinOp)
	if !ok {
		return false
	}
	plan := g.percentFormatOf(n)
	return plan.applies && plan.refusal == ""
}

// percentFormatShapeIsText is the pre-scan's version of percentIsTextExpr. The passes that register
// "this function hands back a text" and "this attribute holds a text" run before any per-function record
// exists, so they can only read the shape the source wrote: a `%` whose format is a text literal or an
// interpolation is a formatting whatever its argument turns out to be, and the door answers an @str_tab
// index for it. A format that arrives from a variable is not answered here — the pre-scan cannot name what
// a variable holds, and guessing would print a table index as a number, which is the bug this whole file
// exists to avoid (ADR 0229's one-predicate rule, ADR 0359's version of it, roadmap Gap R.31, ADR 0360).
func percentFormatShapeIsText(e Expr) bool {
	n, ok := e.(*BinOp)
	if !ok || n.Op != "%" {
		return false
	}
	switch n.L.(type) {
	case *StrLit, *FString:
		return true
	}
	return false
}
