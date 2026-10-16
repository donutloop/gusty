// codegen_builtin_text.go is the builtin road for the text names: `str`, `repr`, `chr` and
// `ord`, all four of which answer a text value (roadmap L11.5, ADR 0362).
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// builtinOrd is the road `ord` takes — the code point of a character, asked in code points
func (g *irGen) builtinOrd(b *strings.Builder, c *Call, fnName string) (string, error) {
	// ord(s) folds a constant string to the codepoint of its first byte,
	// mirroring the interpreter (int64(o.sval[0])).
	if len(c.Args) != 1 {
		return "", fmt.Errorf("ord expects one argument")
	}
	sv, ok := stringConst(c.Args[0], g.builtinShadowed)
	if !ok {
		// imported string module global (data imports): ord(mod.str)
		if attr, ok2 := c.Args[0].(*Attr); ok2 {
			if nm, ok3 := attr.Obj.(*Name); ok3 {
				if globals, ok4 := g.imports.Globals[nm.Value]; ok4 {
					if lit, ok5 := globals[attr.Name.Value]; ok5 {
						if str, ok6 := lit.(*StrLit); ok6 {
							if str.Value == "" {
								g.constantTrap(b, "TypeError", ordSentence(0), c.Span())
								return "0", nil
							}
							modRunes := []rune(str.Value)
							if len(modRunes) != 1 {
								g.constantTrap(b, "TypeError", ordSentence(len(modRunes)), c.Span())
								return "0", nil
							}
							return fmt.Sprintf("%d", int(modRunes[0])), nil
						}
					}
				}
			}
		}
	}
	if !ok {
		// ord of a string the compiler cannot read asks the table (ADR 0229). -1 means the
		// string is not exactly one code point, which is CPython's TypeError and, being a
		// raise, is catchable (ADR 0212).
		if sreg, isStr, err2 := g.strReg(b, c.Args[0]); err2 != nil {
			return "", err2
		} else if isStr {
			cp := g.rtStrCall(b, "rt_str_codepoint", "i32 "+sreg)
			g.checkStrSentinels(b, cp, "TypeError", "ord() expected a character", c.Span(), "ordcp")
			return cp, nil
		}
		return "", fmt.Errorf("ord: codegen folds only a constant string arg")
	}
	if len(sv) == 0 {
		g.constantTrap(b, "TypeError", ordSentence(0), c.Span())
		return "0", nil
	}
	// the first code point, not the first byte (ADR 0225)
	runes := []rune(sv)
	if len(runes) != 1 {
		// A constant text of two characters is the same TypeError as an empty one, quoting the length
		// the author has to fix; reading the first code point and answering it was the fold inventing an
		// answer the reference declines (roadmap Gap R.37, ADR 0326).
		g.constantTrap(b, "TypeError", ordSentence(len(runes)), c.Span())
		return "0", nil
	}
	return fmt.Sprintf("%d", int64(runes[0])), nil
}

// builtinChr is the road `chr` takes — a code point, not a byte (roadmap L11.5, ADR 0362)
func (g *irGen) builtinChr(b *strings.Builder, c *Call, fnName string) (string, error) {
	// chr(n) folds a constant codepoint to a single-character string
	// global, mirroring the interpreter's string(rune(n)).
	if len(c.Args) != 1 {
		return "", fmt.Errorf("chr expects one argument")
	}
	cn, cerr := g.constIntVal(c.Args[0])
	if cerr != nil {
		return "", fmt.Errorf("chr: codegen folds only a constant integer arg")
	}
	if cn < 0 || cn > 0x10FFFF {
		// The fold turned an out-of-range code point into a rune Go renders as a replacement glyph,
		// so `print(chr(-1))` printed a byte the program never named at the exit code of success.
		// The reference stops the program with a ValueError the author can catch (Gap R.37, ADR 0326).
		g.constantTrap(b, "ValueError", chrSentence(), c.Span())
		return "0", nil
	}
	// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
	return g.internStr(b, string(rune(cn))), nil
}

// builtinStr is the road `str` and `repr` take: the text a value prints as, which is a
// value in this language (L11.1)
func (g *irGen) builtinStr(b *strings.Builder, c *Call, fnName string) (string, error) {
	// str() and repr() are one pair, and the pair is one table (roadmap L11.2, ADR 0258).
	// The question each half asks is "how does this value write?", and the answer comes from
	// the same renderer print uses — pointed at a capture buffer instead of stdout — so the
	// two can no longer disagree about a form, the way they did while str() had a
	// number-formatter and print had a printer. Everything the pair can name by its own rules
	// answers here, in the order the forms are distinguished: a container asks its object,
	// a verdict writes its name, the void writes None, a float takes the round-tripping text,
	// a text writes itself under str and its quoted repr under repr, and a number writes its
	// digits. What none of those rules covers is refused by naming the half that is missing,
	// because a missing rendering must not become the number underneath the value: `str([1, 2])`
	// used to answer `0` and `str(None)` answered `0`, both with exit 0.
	if len(c.Args) == 0 && calleeName(c) == "str" {
		// str() is a constructor answering the empty text, and the compiler can name it: the
		// same @str_tab index the literal "" interned to. repr() has NO default and keeps
		// refusing, because the reference raises for `repr()` (Gap R.131's asymmetry).
		return g.internStr(b, ""), nil
	}
	if len(c.Args) != 1 {
		return "", fmt.Errorf("%s expects one argument", calleeName(c))
	}
	form := FormStr
	if calleeName(c) == "repr" {
		form = FormRepr
	}
	if out, handled, rerr := g.renderPair(b, c.Args[0], form, c.Span()); handled {
		return out, rerr
	}
	if calleeName(c) == "repr" {
		return "", g.renderPairRefusal(c.Args[0], form)
	}
	// str(n) folds to the decimal string of an int literal.
	// str(True) is the word "True" and not the digit "1". Both spellings are known right
	// here, so the answer is the @str_tab index the 0/1 selects — an ordinary string value
	// from here on, which is what makes `s = str(flag)` then `print(s.upper())` behave like
	// the text it is (Gap R.42's rule, roadmap L11.1 step 2, ADR 0257).
	if g.printsAsBool(c.Args[0]) {
		sv, serr := g.value(b, c.Args[0])
		if serr != nil {
			return "", serr
		}
		tt := g.internStr(b, "True")
		ff := g.internStr(b, "False")
		bt := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", bt, sv))
		sel := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", sel, bt, tt, ff))
		return sel, nil
	}
	if g.isFloat(c.Args[0]) {
		if fv, ok := g.floatEval(c.Args[0]); ok {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, pyFloatRepr(fv)), nil
		}
	}
	// str(None) is "None" and str("x") is "x" — not the int 0 and not an error.
	// The two compile-time folders agree on these texts (stringConst, and
	// irGen.stringVal for print/len/concat); this is the third site that has to,
	// because print(str(None)) lowers here rather than through an assignment
	// (ADR 0183). A string constant prints with %s, so handing back the folded
	// global is correct in that position; storing one is not, which is why the
	// assignment path interns (see the AssignStmt string branch).
	if _, ok := c.Args[0].(*NoneLit); ok {
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, "None"), nil
	}
	if sl, ok := c.Args[0].(*StrLit); ok {
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, sl.Value), nil
	}
	v, err := g.value(b, c.Args[0])
	if err != nil {
		return "", err
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		// str() of a number the compiler cannot read is digits written at run time and
		// interned, so the answer is an ordinary string value (ADR 0229). It used to be
		// reachable only for a literal argument, which made str(42) work and str(get())
		// refuse — one rule, two behaviours.
		// A float must keep refusing: the compiled backend has no float in a word-sized
		// slot yet (L11.6, ADR 0226), and truncating one to render it would be the
		// silent-truncation bug that gap exists to keep dead.
		// The run-time digits road may only be handed something the compiler can SEE is a number. It used
		// to take anything that was not a text and not a float, so `def f(v): print(str(v))` called with
		// `None` interned the digits of the None handle and printed `0` at exit 0 -- `str(None)` written
		// literally answers `None` through the renderer, and only the parameter form fell through to the
		// number underneath the value (roadmap L11.2, Gap R.171; ADR 0258's rule that a missing rendering
		// must not become a number).
		if !g.builtinShadowed("str") && len(c.Args) == 1 && !g.exprIsString(c.Args[0]) &&
			!g.isNoneExpr(c.Args[0]) && !g.printsAsInternedStr(c.Args[0]) && !g.nameIsContainerRecorded(c.Args[0]) &&
			g.strArgIsNumberish(c.Args[0]) &&
			!strings.Contains(exprTyName(c.Args[0]), "float") && !g.isFloat(c.Args[0]) {
			v, verr := g.value(b, c.Args[0])
			if verr != nil {
				return "", verr
			}
			r := g.rtStrCall(b, "rt_str_of_int", "i32 "+v)
			g.checkStrSentinels(b, r, "RuntimeError", strFullMessage, c.Span(), "stoint")
			return r, nil
		}
		// `str(v)` where v is not a number the renderer can name. The old sentence was three words —
		// `str on non-integer` — which told a reader nothing about what was missing, and an agent
		// could not act on it (ADR 0166's rule is that a refusal names the half it lacks).
		return "", fmt.Errorf("codegen: str(%s) is refused: this backend renders a text through the one str/repr table, which reaches an integer, a text, None, bool and the container literals, and has no word for this argument's kind — the reference answers str() of any value, and doing the same needs the value word that carries its own kind (roadmap L11.1/L11.2, Gap R.171)", exprSurface(c.Args[0]))
	}
	// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
	return g.internStr(b, fmt.Sprintf("%d", n)), nil
}
