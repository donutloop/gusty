// Text: interned constants, the fold's answers about a text-valued expression, and the roads that print one.
// A text is measured in code points here and in the runtime block beside it (roadmap L11.5, ADR 0362).
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// strConst emits a global for a string literal operand.
func (g *irGen) strConst(s string) string {
	g.strIdx++
	name := fmt.Sprintf("@.str%d", g.strIdx)
	esc := strings.ReplaceAll(s, "\\", "\\\\")
	// A raw '"' inside the payload would terminate the LLVM string literal early and the
	// module fails to verify with a nonsense array length ("got type '[7 x i8]' but
	// expected '[31 x i8]'"). This bit me via a traceback frame (`  File "prog", …`), but
	// any program string containing a double quote hit it too.
	esc = strings.ReplaceAll(esc, "\"", "\\22")
	esc = strings.ReplaceAll(esc, "\n", "\\0A")
	esc = strings.ReplaceAll(esc, "\t", "\\09")
	esc = strings.ReplaceAll(esc, "\r", "\\0D")
	// Array size is the decoded byte count: the raw string length (newlines
	// and backslashes are single bytes) plus one trailing null.
	g.strGlobals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(s)+1, esc))
	return name
}

// emitLLVMStringGlobal writes one immutable `c"..."\00` byte array. The escaping is strConst's
// (a class name or an error template is not exempt from a quote ending the literal), extracted so
// the class table and the traceback templates cannot invent a second, less careful version.
func emitLLVMStringGlobal(out *strings.Builder, name, s string) string {
	esc := strings.ReplaceAll(s, "\\", "\\\\")
	esc = strings.ReplaceAll(esc, "\"", "\\22")
	esc = strings.ReplaceAll(esc, "\n", "\\0A")
	esc = strings.ReplaceAll(esc, "\t", "\\09")
	esc = strings.ReplaceAll(esc, "\r", "\\0D")
	fmt.Fprintf(out, "%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(s)+1, esc)
	return fmt.Sprintf("getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0)", len(s)+1, len(s)+1, name)
}

// isStringExpr reports whether e is a string-producing expression. Literal
// membership against a literal container must not be unrolled when the tested
// value is a string (the comparison would be pointer-vs-int, not valid i32 IR).
// runtimeStringErr is the ADR 0166 rule applied to strings in runtime containers. Strings
// are compile-time constants in this backend (@.strN globals) while container slots hold i32
// handles, so a string operand lowers to a global fed to an i32 parameter — LLVM rejects it
// ("global variable reference must have pointer type") and the exit-code contract then calls
// valid user code a compiler bug (exit 2). Refusing with a clear message is the honest
// alternative; the record supports strings in containers, and the plan for AOT is an
// interned string table (roadmap Gap I.2).
func runtimeStringErr(slot, op string) error {
	return fmt.Errorf("codegen: cannot %s a string as a runtime %s in the AOT backend yet; CPython answers it — a compiled container slot holds an int/bool value, and strings need the runtime string table (roadmap Gap I.2)", op, slot)
}

// rejectRuntimeString reports whether e would put (or look up) a string in a heap container
// slot. It covers every way codegen knows a value is a string: a literal/interpolated
// literal, a folded string expression (str(x), s.upper(), concatenation), and a variable
// bound to a string.
func (g *irGen) rejectRuntimeString(e Expr, slot, op string) error {
	if e == nil {
		return nil
	}
	if isStringExpr(e) {
		return runtimeStringErr(slot, op)
	}
	if _, ok := g.stringVal(e); ok {
		return runtimeStringErr(slot, op)
	}
	if nm, ok := e.(*Name); ok && g.strVals != nil {
		if _, isStr := g.strVals[nm.Value]; isStr {
			return runtimeStringErr(slot, op)
		}
	}
	return nil
}
func isStringExpr(e Expr) bool {
	switch e.(type) {
	case *StrLit, *FString:
		return true
	}
	if textDoorShapeIsText(e) {
		// A formatting and a repeat both answer a text, and this predicate is the shape question the
		// PRE-PASSES ask — the call-site census that decides whether a parameter holds a text, the assignment
		// census that decides whether a name is ever assigned a number. Without this arm
		// `def w(t): print("<" + t + ">")` called as `w("%d up" % 7)` had no record of the text crossing the
		// boundary: the concatenation refused, and plain `print(t)` printed the number under an interned
		// string (roadmap Gap R.31, L11.1, ADR 0360; the repeat's half is Gap R.33, ADR 0365).
		return true
	}
	if joinShapeIsText(e) {
		// And so does the JOIN seam: `"-".join([...])` answers the one @str_tab index its `@rt_str_cat` chain
		// interned, so the same pre-passes — the answer-kind census above all — have to see it as a text, or a
		// body with one join arm and one number arm registers `strFuncs` on the join's evidence and prints the
		// number's table position (roadmap Gap R.403, ADR 0408, beside Gap R.219 / ADR 0368).
		return true
	}
	return false
}

// joinShapeIsText is the join's half of the shape question, asked with no compiler state to consult:
// a call spelled `<a text>.join(<one argument>)` answers one interned text, because the seam is built by
// the door the concatenation road already uses and that door's answer is an index (roadmap Gap R.403,
// ADR 0408). The receiver must be a text the SHAPE can see: a name the program bound is the runtime-receiver
// road **Gap R.405** still owes, and the door refuses it rather than guess.
func joinShapeIsText(e Expr) bool {
	c, ok := e.(*Call)
	if !ok || len(c.Args) != 1 {
		return false
	}
	at, ok := c.Fn.(*Attr)
	if !ok || at.Name.Value != "join" {
		return false
	}
	return isStringExpr(at.Obj)
}

// --- the one question a built text asks ------------------------------------------------
//
// Two roads in this backend answer a text the source never spelled as one: the FORMATTING (`"%s" % v`,
// ADR 0360) and the REPEAT (`"ab" * n`, ADR 0365) — next to the INTERPOLATION (`f"a{n}"`, ADR 0359), which
// the source does spell. Each builds or interns a text and answers the `@str_tab` INDEX a slot holds, and
// every road that READS a text — the print legs, `str`/`repr`, a container literal's kind census, a
// `return`, the call-site parameter census, the pair pass — has to know that fact.
//
// For one cycle the two doors answered the question separately: the repeat's arm lived only in
// `exprIsString`/`printsAsInternedStr` while the formatting's arms were also in the pre-scan, the census and
// the pair pass. The result was the same value read two ways at different exits — `print("ab" * 2)` printed
// `abab` while `print(["ab" * 2, 1])` printed `[1, 1]` and `str("ab" * 2)` printed nothing, all at exit 0,
// which is the class every row in this file exists to keep dead. One predicate, two arms, asked by every
// road: ADR 0232's rule restated (roadmap Gap R.33, ADR 0365).

// textDoorIsText is the per-generator question: does the door that owns this operator answer an @str_tab
// text index for this expression? A caller that has a generator asks THIS; a pre-scan that has only the AST
// asks `textDoorShapeIsText`.
func (g *irGen) textDoorIsText(e Expr) bool {
	return g.percentIsTextExpr(e) || g.seqIsTextExpr(e)
}

// textDoorShapeIsText is the pre-scan's version of the same question, reading only the spelling.
func textDoorShapeIsText(e Expr) bool {
	return percentFormatShapeIsText(e) || seqShapeIsText(e)
}

// textDoorValueOperand is the operand a text door reads a VALUE out of — a formatting's argument, a
// repeat's multiplier — which is the side whose kind the pair doors may still need a tag for. The answer is
// one interned index whatever that side holds, but taking the parameter's tag away with it is how a float
// argument printed `3.00` (ADR 0360's lesson, applied to the repeat by ADR 0365).
func textDoorValueOperand(n *BinOp) Expr {
	if n == nil {
		return nil
	}
	if n.Op == "*" {
		if isStringExpr(n.L) {
			return n.R
		}
		return n.L
	}
	return n.R
}

// stringConst resolves a string literal or a chain of `+`-concatenated string
// literals to its concrete value. Returns (s, true) when the expression is a
// compile-time-known string constant.
// stringConst folds an expression to a string constant. `shadowed` reports whether the
// program defines a function of a given name; when it does, the call is the program's and
// no fold through the built-in's meaning is allowed (`str`/`chr` here) — that fold is how
// `def str(x): return x + 7` printed a string the program never returned (Gap R.6, ADR 0199).
// Callers without program context pass nil, which folds only what cannot be a call.
func stringConst(e Expr, shadowed func(string) bool) (string, bool) {
	if sl, ok := e.(*StrLit); ok {
		return sl.Value, true
	}
	if fs, ok := e.(*FString); ok {
		// An interpolation with nothing to interpolate is a constant text. The fold lives in
		// pkg/lang/fstring_value.go beside the value road's, because the container literal's kind
		// census asks the same question through this door and a census that disagrees with the
		// builder is how [f"a1", 1] came to print an interned string's index for the integer 1
		// (roadmap L11.1, ADR 0359).
		return fstringConstTextOf(fs)
	}
	if b, ok := e.(*BinOp); ok && b.Op == "+" {
		ls, lok := stringConst(b.L, shadowed)
		rs, rok := stringConst(b.R, shadowed)
		if lok && rok {
			return ls + rs, true
		}
	}
	if c, ok := e.(*Call); ok {
		attr, ok := c.Fn.(*Attr)
		if ok {
			v, ok := stringConst(attr.Obj, shadowed)
			if ok {
				switch attr.Name.Value {
				// The five case operations read the generated table (ADR 0362), and a table that will
				// not answer is a fold that did not happen: the runtime door beside it asks the same
				// artifact, so declining here cannot leave Go's answer in the module.
				case "upper":
					t2, ok2 := caseFoldFold(caseUpper, v)
					if !ok2 {
						return "", false
					}
					return t2, true
				case "capitalize":
					t2, ok2 := caseFoldFold(caseCapitalize, v)
					if !ok2 {
						return "", false
					}
					return t2, true
				case "title":
					t2, ok2 := caseFoldFold(caseTitle, v)
					if !ok2 {
						return "", false
					}
					return t2, true
				case "swapcase":
					t2, ok2 := caseFoldFold(caseSwapcase, v)
					if !ok2 {
						return "", false
					}
					return t2, true
				case "lower":
					t2, ok2 := caseFoldFold(caseLower, v)
					if !ok2 {
						return "", false
					}
					return t2, true
				case "strip", "lstrip", "rstrip":
					t2, ok3 := caseFoldTrim(caseTrimMode(attr.Name.Value), v, c.Args, func(e Expr) (string, bool) {
						return stringConst(e, shadowed)
					})
					if !ok3 {
						return "", false
					}
					return t2, true
				case "replace":
					if len(c.Args) != 2 {
						return "", false
					}
					oldv, ok := stringConst(c.Args[0], shadowed)
					if !ok {
						return "", false
					}
					newv, ok := stringConst(c.Args[1], shadowed)
					if !ok {
						return "", false
					}
					return strings.ReplaceAll(v, oldv, newv), true
				case "join":
					if len(c.Args) != 1 {
						return "", false
					}
					ll, ok := c.Args[0].(*ListLit)
					if !ok {
						return "", false
					}
					parts := []string{}
					for _, el := range ll.Elems {
						sv, ok := stringConst(el, shadowed)
						if !ok {
							return "", false
						}
						parts = append(parts, sv)
					}
					return strings.Join(parts, v), true
				}
			}
		}
		// str() over a literal folds to the text the program would print, so
		// len(str(42)) -> 2 and `s = str(None)` needs no store. None folds to "None"
		// because that is what str(None) is: the int 0 is a different value, and a
		// fold that says "0" both prints the wrong thing and hands back a string
		// constant the assignment path cannot elide (ADR 0183).
		if n, ok := c.Fn.(*Name); ok && n.Value == "str" && len(c.Args) == 1 && !nameShadowed(shadowed, n.Value) {
			switch arg := c.Args[0].(type) {
			case *IntLit:
				return strconv.FormatInt(arg.Value, 10), true
			case *NoneLit:
				return "None", true
			case *StrLit:
				return arg.Value, true
			}
			if fl, ok := c.Args[0].(*FloatLit); ok {
				return pyFloatRepr(fl.Value), true
			}
		}
		return "", false
	}
	return "", false
}

// stringConstLen is len() over a compile-time-known string constant.
func stringConstLen(e Expr, shadowed func(string) bool) (int, bool) {
	if s, ok := stringConst(e, shadowed); ok {
		// Code points, the unit every other string position question uses (ADR 0225). This fold
		// measured bytes, so `len("héllo")` answered 6 at compile time while the same program with
		// the text in a variable answered 5 -- one language, two units, chosen by where the text
		// happened to be written.
		return len([]rune(s)), true
	}
	return 0, false
}
func (g *irGen) stringVal(e Expr) (string, bool) {
	switch n := e.(type) {
	case *Attr:
		// `def.__doc__` / `Cls.__doc__` folds the docstring to a string
		// (mirrors the interpreter's doc introspection in AOT).
		if e.(*Attr).Name.Value == "__doc__" {
			if nm, ok := e.(*Attr).Obj.(*Name); ok {
				if fd, ok := g.fds[nm.Value]; ok {
					return fd.Doc, true
				}
				if ci, ok := g.classInfos[nm.Value]; ok {
					return ci.doc, true
				}
			}
		}
		// imported module global folded to a string (data imports)
		if nm, ok := e.(*Attr).Obj.(*Name); ok {
			if globals, ok := g.imports.Globals[nm.Value]; ok {
				if lit, ok := globals[e.(*Attr).Name.Value]; ok {
					if str, ok := lit.(*StrLit); ok {
						return str.Value, true
					}
				}
			}
		}
		return "", false
	case *StrLit:
		return n.Value, true
	case *FString:
		// An interpolation every of whose fields the AST already states is a constant text: `f"a{1}b"`
		// IS "a1b", and folding it is what lets the print road, the comparison folds and `len` treat it
		// as the text it is instead of lowering an interpolation the road has never seen (roadmap
		// Gap R.173's rule for a constant-test ternary, applied by ADR 0359 to an interpolation).
		if txt, ok := g.fstringConstText(n); ok {
			return txt, true
		}
		return "", false
	case *CondExpr:
		// A ternary hands back one of its arms, so a constant test settles the value: `"y" if 1 else "n"`
		// IS "y", and folding it lets the print road write the word instead of the @str_tab POSITION the
		// `value()` road produces for a text (Gap R.173, ADR 0290). Only the arm that RUNS is folded — the
		// reference does not evaluate the other one, and ADR 0262 already reads a constant-test ternary as
		// the arm that runs when it chooses the answer's word.
		if taken, ok := constantTestArm(n); ok {
			return g.stringVal(taken)
		}
		// A run-time test folds only when BOTH arms fold to the same text; two different texts under a
		// run-time test is a value the compiler cannot name, and guessing one would be a wrong answer.
		if tThen, ok := g.stringVal(n.If); ok {
			if tElse, ok2 := g.stringVal(n.Else); ok2 && tElse == tThen {
				return tThen, true
			}
		}
		return "", false
	case *Slice:
		// compile-time string slice fold: s[a:b:c] where the source and all
		// bounds are compile-time constants. Mirrors the interpreter's Python
		// slice semantics via pySliceIndices.
		src, ok := g.stringVal(n.Obj)
		if !ok {
			return "", false
		}
		low := int64(0)
		high := int64(0)
		step := int64(1)
		hasLow := false
		hasHigh := false
		// A bound written as `None` is a bound the program did not write: the reference reads
		// `s[None:None]` and `s[:]` as the same slice, so the fold reads them the same way and does
		// not ask `foldConstInt` for a value that is standing in for an absence (roadmap Gap R.233).
		if !g.sliceBoundIsAbsent(n.Low) {
			v, ok := g.foldConstInt(n.Low)
			if !ok {
				return "", false
			}
			low = v
			hasLow = true
		}
		if !g.sliceBoundIsAbsent(n.High) {
			v, ok := g.foldConstInt(n.High)
			if !ok {
				return "", false
			}
			high = v
			hasHigh = true
		}
		if !g.sliceBoundIsAbsent(n.Step) {
			v, ok := g.foldConstInt(n.Step)
			if !ok {
				return "", false
			}
			step = v
			if step == 0 {
				return "", false
			}
		}
		// A position in this language is a code point (ADR 0225), so the fold slices a text the way the
		// run-time door does — over code points, never bytes. Measuring bytes here is what made
		// `s[1:3]` of "h\u00e9llo" answer 'é' when the bounds were written and 'él' when they were computed:
		// one question, two roads, two answers, which is the defect class ADR 0229 and ADR 0232 exist to
		// close (roadmap Gap R.47, found while the printf-style door's width padding was being matched,
		// roadmap Gap R.31, ADR 0360).
		runes := []rune(src)
		length := int64(len(runes))
		start, stop, stp := pySliceIndices(low, high, step, hasLow, hasHigh, length)
		var sb strings.Builder
		if stp > 0 {
			for i := start; i < stop; i += stp {
				sb.WriteRune(runes[i])
			}
		} else {
			for i := start; i > stop; i += stp {
				sb.WriteRune(runes[i])
			}
		}
		return sb.String(), true

	case *Name:
		if g.strVals != nil {
			if s, ok := g.strVals[n.Value]; ok {
				return s, true
			}
		}
		return "", false
	case *BinOp:
		if n.Op == "+" {
			ls, lok := g.stringVal(n.L)
			rs, rok := g.stringVal(n.R)
			if lok && rok {
				return ls + rs, true
			}
		}
		return "", false
	case *Call:
		// str(int-literal) folds to its decimal string, e.g. print(str(42));
		// str(float-constant) folds to its %g decimal string (matches the
		// interpreter's Repr), so print(str(3.5)) emits a valid %s printf
		// with the string-global pointer rather than a %d printf fed an i8*.
		if name, ok := n.Fn.(*Name); ok && name.Value == "str" && len(n.Args) == 1 && !g.builtinShadowed(name.Value) {
			if il, ok := n.Args[0].(*IntLit); ok {
				return strconv.FormatInt(il.Value, 10), true
			}
			// str(None) is "None", not "0": the fold has to agree with the interpreter's
			// str()/print(), or the same program prints two different things depending on
			// which backend ran it (ADR 0183).
			if _, ok := n.Args[0].(*NoneLit); ok {
				return "None", true
			}
			if sl, ok := n.Args[0].(*StrLit); ok {
				return sl.Value, true
			}
			if fv, ok := g.floatEval(n.Args[0]); ok {
				return pyFloatRepr(fv), true
			}
		}
		if name, ok := n.Fn.(*Name); ok && name.Value == "chr" && len(n.Args) == 1 && !g.builtinShadowed(name.Value) {
			// The `str` fold beside this one already asked how many arguments there were; this one reached
			// n.Args[0] on the strength of the name alone, so `print(chr())` panicked with a Go index-out-
			// of-range trace and exit 2 (roadmap Gap R.131, ADR 0287).
			if il, ok := n.Args[0].(*IntLit); ok && il.Value >= 0 && il.Value <= 0x10FFFF {
				// The range test is the call road's, and the print fold has to make the same test or it
				// answers the replacement glyph for a code point the reference refuses with a ValueError —
				// this fold is reached first, so the trap below it never ran (roadmap Gap R.37, ADR 0326).
				return string(rune(il.Value)), true
			}
		}
		// A dict's `get` answers a TEXT when the slot it hits holds one. The fold in the call road
		// returns `g.value(...)` of that StrLit — an interned index — and if nobody has told `print`
		// the expression is a string, printf's %d prints the index: `print({1: "a"}.get(1))` said `0`
		// where the reference says `a` (roadmap Gap R.180, the same arrangement ADR 0291 set for the
		// missing-key answer). Ask the SAME lookup the fold performs, so the two can never disagree.
		if attr, isAttr := n.Fn.(*Attr); isAttr && attr.Name.Value == "get" && (len(n.Args) == 1 || len(n.Args) == 2) {
			if dl, isDict := attr.Obj.(*DictLit); isDict {
				// One lookup, the fold's own — the key scan lives in g.dictFoldSlot and nowhere else.
				if slot, found := g.dictFoldSlot(dl, n.Args[0]); found {
					if txt, isTxt := slot.(*StrLit); isTxt {
						return txt.Value, true
					}
					return "", false
				}
				// Missed: the fold answers the DEFAULT, so that is what print must render.
				if len(n.Args) == 2 {
					if txt, isTxt := n.Args[1].(*StrLit); isTxt {
						return txt.Value, true
					}
					return g.stringVal(n.Args[1])
				}
				return "", false
			}
		}
		// constant-fold string methods: `"AbC".upper()`, `.lower()`, `.strip()`.
		attr, ok := n.Fn.(*Attr)
		if !ok {
			return "", false
		}
		v, ok := g.stringVal(attr.Obj)
		if !ok {
			return "", false
		}
		switch attr.Name.Value {
		// The other fold road, asked of the same table (ADR 0362).
		case "swapcase":
			t2, ok2 := caseFoldFold(caseSwapcase, v)
			if !ok2 {
				return "", false
			}
			return t2, true
		case "title":
			t2, ok2 := caseFoldFold(caseTitle, v)
			if !ok2 {
				return "", false
			}
			return t2, true
		case "capitalize":
			t2, ok2 := caseFoldFold(caseCapitalize, v)
			if !ok2 {
				return "", false
			}
			return t2, true
		case "upper":
			t2, ok2 := caseFoldFold(caseUpper, v)
			if !ok2 {
				return "", false
			}
			return t2, true
		case "lower":
			t2, ok2 := caseFoldFold(caseLower, v)
			if !ok2 {
				return "", false
			}
			return t2, true
		case "strip", "lstrip", "rstrip":
			t2, ok3 := caseFoldTrim(caseTrimMode(attr.Name.Value), v, n.Args, g.stringVal)
			if !ok3 {
				return "", false
			}
			return t2, true
		case "replace":
			if len(n.Args) != 2 {
				return "", false
			}
			oldv, ok := g.stringVal(n.Args[0])
			if !ok {
				return "", false
			}
			newv, ok := g.stringVal(n.Args[1])
			if !ok {
				return "", false
			}
			return strings.ReplaceAll(v, oldv, newv), true
		case "join":
			if len(n.Args) != 1 {
				return "", false
			}
			ll, ok := n.Args[0].(*ListLit)
			if !ok {
				return "", false
			}
			parts := []string{}
			for _, el := range ll.Elems {
				sv, ok := g.stringVal(el)
				if !ok {
					return "", false
				}
				parts = append(parts, sv)
			}
			return strings.Join(parts, v), true
		}
		return "", false
	}
	return "", false
}

// valueText returns the i32 operand text for e, discarding any codegen error.
func (g *irGen) valueText(b *strings.Builder, e Expr) string {
	v, err := g.value(b, e)
	if err != nil {
		// This line used to discard the error and hand back "". The float path then emitted
		//     %t1 = sitofp i32  to double
		// into the module -- an instruction with no operand -- which llc rejected, so an ordinary
		// program like `print([1.5, 2])` came back as exit 2, a toolchain rejection blaming the
		// compiler for a program whose answer CPython prints in one line (roadmap Gap R.40,
		// ADR 0166). The failure is recorded instead; the module is refused at assembly. "0" keeps
		// the half-written instruction parseable purely so that the refusal, not the garbage, is
		// what a user reads -- nothing that is about to be refused may also be executed.
		g.noteUnlowered(e, err)
		return "0"
	}
	return v
}

// printsAsInternedStr reports whether an expression's value is an index into the runtime
// string table: an element read from a string container, or a loop variable walking one.
// Those print as their own text, while the container itself prints as a list of
// single-quoted strings the way Python does (roadmap Gap I.2).
// textMethodAnswersText is the print road's question about a string method: does this call hand back a
// TEXT (an @str_tab index) rather than a number, a verdict or a list? The table must be the FOLD's own —
// every method the operation path implements that returns text. Three names listed here against fourteen
// implemented there is how "ab".zfill(5) printed the intern INDEX (0) where the reference prints 000ab,
// and how the same three-name list existed twice (roadmap Gap R.183; ADR 0229's rule that print and the
// operation ask one predicate).
func (g *irGen) textMethodAnswersText(name string, argc int) bool {
	switch name {
	case "strip", "lstrip", "rstrip":
		// The cutset is an argument, not a second text: `s.strip("x")` hands back text just as `s.strip()` does
		// (roadmap L11.5, ADR 0362). Listing the trim family against its own arity is what made
		// `print(f("  café "))` print the intern index.
		return argc <= 1
	case "upper", "lower", "capitalize", "title", "swapcase", "expandtabs":
		return argc == 0
	case "replace", "removeprefix", "removesuffix", "ljust", "rjust", "zfill", "join":
		return argc == 1
	}
	return false
}
func (g *irGen) printsAsInternedStr(e Expr) bool {
	if g.textDoorIsText(e) {
		// The print road and the operation road ask ONE question about what is a text (ADR 0229), so a
		// formatted or repeated text prints as its text on the road that prints it and is a text on the road
		// that compares, binds or measures it — never one on one leg and the other on the second
		// (roadmap Gap R.31, ADR 0360; Gap R.33, ADR 0365).
		return true
	}
	switch v := e.(type) {
	case *StrLit:
		// A text literal prints as its text, via rt_print_str on the index `value()` interns it to.
		// This case looks obvious in hindsight and was missing, which is why the ternary arm below
		// could ask "are both arms text?" of two StrLits and be told no (Gap R.173, ADR 0290).
		return true
	case *ExnMessage:
		// `except T as err:` binds the name to what the raise carried, interned: the word in the
		// slot is an @str_tab index, so printing it shows the message and not the position of the
		// message in the intern table (roadmap Gap R.113, ADR 0406).
		return true
	case *FString:
		// An interpolation answers an @str_tab index — the same word a text literal answers — so a
		// name bound from one (`s = f"x{n}"`) prints as its text and not as the index underneath it
		// (roadmap L11.1, ADR 0359). The binding road records this predicate, which is the only reason
		// the binding and the direct print of one interpolation cannot disagree about which is a text.
		return true
	case *Name:
		// The name an `except ... as` clause bound prints as its text too: the print road shows the
		// message the raise carried, while the operand roads keep asking `exprIsString` and are told
		// no, which is the split the reference draws between an exception and the text it says
		// (roadmap Gap R.113, ADR 0406).
		return g.internedVars[v.Value] || g.caughtVars[v.Value]
	case *Attr:
		// `self.w = "hi"` in the class, `print(C().w)` outside it: the slot holds an
		// @str_tab index, so printing it must show the text (Gap R.42, ADR 0224).
		if cls := g.receiverClass(v.Obj); cls != "" {
			return g.strAttrs[cls+"."+v.Name.Value]
		}
		return false
	case *Call:
		// echo("yo") returns an index into @str_tab; printing it must show the text
		// (roadmap Gap J.5).
		if g.callReturnsStr(v) {
			return true
		}
		// A string method on a string receiver — `get()[1].upper()` — also prints as text. The
		// print path and the operation path ask this question of the same predicate, or one of
		// them renders the interned index as a number (ADR 0229).
		if at, ok := v.Fn.(*Attr); ok && g.exprIsString(at.Obj) && g.textMethodAnswersText(at.Name.Value, len(v.Args)) {
			return true
		}
		// min/max of texts returns one of those texts. It is still an @str_tab index; answering
		// "not text" here printed the intern table position instead of the word chosen (Gap R.73).
		if nm, ok := v.Fn.(*Name); ok && (nm.Value == "min" || nm.Value == "max") && !g.builtinShadowed(nm.Value) && g.minMaxCallReturnsText(v) {
			return true
		}
		// str(n) is text whatever n is, so printing it shows digits and not a count. repr is
		// the same answer with the quoting question asked: both halves hand back an @str_tab
		// index, and if this predicate said yes to one and no to the other, print(repr(x))
		// would write the intern table's position instead of the quoted text (roadmap L11.2,
		// ADR 0258 — the pair is one question, asked once per value).
		if g.builtinCallAs(v, "str") || g.builtinCallAs(v, "repr") {
			return true
		}
		return false
	case *BinOp:
		// `s[0] + s[2]` folds to text; every operand question is the same question.
		return g.exprIsString(v)
	case *Slice:
		// s[a:b] of a string is a string, constant bounds or not (ADR 0229) — the print path
		// asks the same question as the operation path, or a slice prints its index.
		if g.exprIsString(v.Obj) {
			return true
		}
		return false
	case *CondExpr:
		// A ternary hands back one of its arms, so its kind is the arm's kind — the same rule
		// ADR 0262 already applies to the answer's WORD (roadmap L11.6, Gap R.102), asked here of
		// the text domain. When the test is a value the source wrote, only the arm that RUNS is the
		// answer. When it is a run-time fact, both arms must agree, which is the conservatism ADR
		// 0257 already uses for a verdict's arms. Answering "not text" here printed the @str_tab
		// POSITION: `print("y" if 1 else "n")` said `0` and `print("big" if x > 2 else "small")`
		// said `0` where the reference prints the word (Gap R.173, ADR 0290).
		if taken, ok := constantTestArm(v); ok {
			return g.printsAsInternedStr(taken)
		}
		return g.printsAsInternedStr(v.If) && g.printsAsInternedStr(v.Else)
	case *Index:
		// A subscript of a string is a one-character string (ADR 0225), so printing it is a
		// text question. Answering with %d printed the interned index: `print(s[1])` said `0`.
		// The base being a runtime string changes nothing about the answer's kind (ADR 0229).
		if g.exprIsString(v.Obj) {
			return true
		}
		if _, isStr := g.stringVal(v.Obj); isStr {
			return true
		}
		if nm, ok := v.Obj.(*Name); ok {
			// Elements of a string list/set and the values of a string dict are both
			// @str_tab indices; printing one must show its text, not the index. The
			// dict case was missing, so `d["k"] = "v"; print(d["k"])` printed the
			// integer index whenever the whole dict had not been printed first (the
			// runtime @estr flag only covers printing the container itself).
			return g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictValStr[nm.Value]
		}
		if dl, isLit := v.Obj.(*DictLit); isLit {
			// One entry of a literal dict, asked as the entry the key names: printing it must show the
			// word when that entry is text and not the @str_tab position it is stored as
			// (roadmap Gap R.215, ADR 0327).
			return g.dictLiteralValueIsString(dl, v.Idx)
		}
	}
	return false
}

// strArgIsNumberish answers the narrow question the run-time digits road needs: is this operand a number
// the compiler can see, as opposed to a name whose kind the object carries, a container, a text or a void?
// A parameter is deliberately NOT assumed numeric — it is written by the caller, and `f(None)` is an
// ordinary program. A name the module bound to a number is; a name bound to anything else is not
// (roadmap L11.2, Gap R.171, ADR 0258).
func (g *irGen) strArgIsNumberish(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *BoolLit:
		return true
	case *Name:
		// A parameter keeps the digits road. It is NOT assumed numeric — rather, a parameter that
		// would arrive as a TEXT is refused at the CALL SITE by ADR 0174's argument guard ("strings
		// are not supported as function arguments"), and a container or void argument likewise never
		// reaches a word-sized parameter, so by the time `str(v)` is lowered here `v` can only hold
		// what a word can carry. Removing this road instead turned four WORKING answers into refusals
		// — `x = str(v); print(x)` with `f(3)` printed `3` before and refused after — which the ladder
		// forbids (roadmap L11.2, Gap R.170/R.171, ADR 0174).
		if _, isParam := g.params[n.Value]; isParam {
			return true
		}
		if g.nameIsContainerRecorded(n) || g.noneVars[n.Value] || g.internedVars[n.Value] {
			return false
		}
		if _, isText := g.strVals[n.Value]; isText {
			return false
		}
		// Every value the program ever wrote into this name, module-level or in the body being
		// compiled, must be something the digits road can render. One binding to a text, a container
		// or a void is enough to make the digits a lie about the value (roadmap L11.2, Gap R.171) --
		// and it must be EVERY binding, not the folded last one: `x = "abc"` then `x = 5` then
		// `print(str(x))` is answered by a fold arm above this road, and gating it on a single
		// record made that working answer refuse.
		// The value the name holds NOW -- the last binding the program wrote, which is what the
		// renderer's own fold arm reads, and what `x = "abc"` then `x = 5` then `print(str(x))` prints
		// (`5`, per the rebinding rule ADR 0274's convention pins). Asking "was it EVER a number"
		// answers a different question and printing the digits of an earlier value would be a lie.
		vals := g.strArgBindings(n.Value)
		if len(vals) == 0 {
			return false
		}
		// A name whose last binding is an expression that mentions the name itself — `s = s + x`,
		// the accumulator every loop writes — asks this predicate the same question again. The answer
		// is not provable from the bindings (the value depends on what was already there), and the
		// honest refusal is what the digits road wants. Without the guard the question recursed until
		// the process ran out of stack: the fold crashed instead of refusing, which is the one answer
		// a compiler is never allowed (roadmap Gap R.38's rule; surfaced by ADR 0302's echo, which
		// asks the pair about a snippet's final `s`).
		if g.strArgInFlight == nil {
			g.strArgInFlight = map[string]bool{}
		}
		if g.strArgInFlight[n.Value] {
			return false
		}
		g.strArgInFlight[n.Value] = true
		defer delete(g.strArgInFlight, n.Value)
		return g.strArgIsNumberish(vals[len(vals)-1])
	case *BinOp:
		switch n.Op {
		case "+", "-", "*", "//", "%":
			return g.strArgIsNumberish(n.L) && g.strArgIsNumberish(n.R)
		}
		return false // `/` answers a double, which this road must keep refusing
	case *UnOp:
		return n.Op == "-" && g.strArgIsNumberish(n.X)
	case *Index:
		return false // a slot's kind is the object's business, not this road's
	case *Call:
		// A callee's answer is readable when the body's own `return` is: `str(get())` where `def get():
		// return 7` was always meant to work (ADR 0229 widened this road precisely so a call was as
		// answerable as a literal), and a call to a function that renders, returns a text, a container or
		// nothing is not a number.
		// The callee must be a function the program defined, not a builtin by that name.
		// (`builtinShadowed` asks the opposite question — "did the program take this builtin's
		// name?" — and answers YES for an ordinary callee, which is how my first draft here
		// refused every `str(call())`; roadmap Gap R.171, recorded so nobody re-borrows it.)
		nm, isName := n.Fn.(*Name)
		if !isName {
			return false
		}
		fd, ok := g.fds[nm.Value]
		if !ok || !fdReturnsValue(fd) || g.strFuncs[nm.Value] {
			return false
		}
		w := returnCollector{}
		w.stmts(fd.Body)
		if len(w.returns) == 0 {
			return false
		}
		for _, r := range w.returns {
			if !g.strArgIsNumberish(r) {
				return false
			}
		}
		return true
	}
	return false
}

// strArgBindings collects every value the program bound to a name: module-level assignments plus the
// bindings of the body currently being compiled. scanRebinds is the table the rest of the front end
// already reads (ADR 0274's return convention, ADR 0285's number half), so this asks an existing record
// rather than inventing a fourth one.
func (g *irGen) strArgBindings(name string) []Expr {
	if name == "" {
		return nil
	}
	if g.strArgBindCache == nil {
		g.strArgBindCache = map[string][]Expr{}
	}
	if v, ok := g.strArgBindCache[name]; ok {
		return v
	}
	out := map[string][]Expr{}
	for k, vs := range g.moduleBinds {
		out[k] = append(out[k], vs...)
	}
	if fd, ok := g.fds[g.curFunc]; ok && fd != nil {
		scanRebinds(fd.Body, out)
	}
	vals := out[name]
	g.strArgBindCache[name] = vals
	return vals
}

// internStr emits the interning call that turns a compile-time-known string into its runtime
// representation, an index into @str_tab. Interning is content-addressed (rt_str_intern2 dedups by
// strcmp on the raw text), so index equality IS content equality: `x == "hi"` is an `icmp` once the
// operands are interned, which is what makes string comparison compilable at all (roadmap L11.8,
// Gap R.42, ADR 0224).
func (g *irGen) internStr(b *strings.Builder, txt string) string {
	g.heapUsed = true
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
	return t
}
