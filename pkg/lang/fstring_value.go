package lang

// An f-string is a TEXT, not only a printing instruction.
//
// The print road has always honoured an interpolation: it builds one printf format string and
// hands each field to the module's printers, so `print(f"x{n}")` has answered `x3` for a long
// time. What it never had was an answer for the SAME source standing where a value goes.
// `value()` refused outright — "f-string requires a constant expression (AOT backend)" — and
// with it every position that lowers an expression to a word: a binding (`s = f"x{n}"`), a
// concatenation (`f"a{n}" + "!"`), a string method (`f"v={n}".upper()`), `len`, `==`, `in`,
// a subscript, a slice, a `for` iterable, a `return`, an argument to a string parameter, a
// builtin's operand. The reference answers all of them; this backend declined them at exit 1
// while the identical text written as `f"x" + str(n)` was refused too, because the road that
// would have built it was the same missing one (roadmap L11.1, Gap R.146's value half; ADR 0359).
//
// One door answers it now, and it is the door the rest of this file's family already stands on:
//
//   - A field is a RENDERING position, so it asks `renderPair` — the same question `str()` and
//     `repr()` ask — for the @str_tab index that holds the field's text. A value's kind is the
//     value's own fact: the pair road answers a slot read, a container, a verdict, the void, a
//     float, a number; the print road's printf cannot, which is why `print(f"{xs}")` answered the
//     heap handle (`0`) and `print(f"v={None}")` answered `0` for `None` (roadmap ADR 0258's one
//     renderer, ADR 0303's one printer).
//   - Parts are joined by `rt_str_cat` — the same runtime builder `s + t` has always used — so a
//     compiled f-string is an ordinary interned text: printable, comparable, subscriptable,
//     iterable, hashable into a dict, and printed by every road that reads @str_tab. Nothing new
//     is added to the runtime.
//   - A field the compiler can read at compile time is folded into the literal around it, which
//     is what makes `f"a{1}b"` the same program as `"a1b"` for the print road, the comparison
//     folds and `len` (Gap R.173's rule for a constant-test ternary, applied to an interpolation).
//   - A field that carries a FORMAT SPEC is honoured only where the spec engine can read the
//     value, exactly as the print road refuses today: an unformatted answer at exit 0 is the
//     defect Gap R.186 exists to end, so this road refuses with the same sentence rather than
//     shipping a plain `%d` wearing the f-string's clothes.

import (
	"fmt"
	"strconv"
	"strings"
)

// fstringConstText folds an interpolation the compiler can read end to end: every literal part
// and every field whose text is a fact of the AST. It is deliberately narrower than "the
// expression is constant" — a field is folded only in the shapes the print road's own folds
// agree with, and a field carrying a spec is left to the spec engine so a fold can never answer
// `f"{3.5:.2f}"` with the unformatted `3.5` (Gap R.186, ADR 0299).
//
// It answers ONE question in ONE place because three roads ask it: `value()` (which interns the
// whole text at compile time), `stringVal` (which every constant-text road consults) and the
// container literal's kind census in heapargs.go, which reaches it through `stringConst`. When
// those three had different answers, `[f"a1", 1]` built a list whose object claimed "every
// element is an interned string" while one slot held the integer 1, and printing it segfaulted —
// exit 2, ADR 0166's forbidden class (roadmap L11.1, ADR 0359).
func (g *irGen) fstringConstText(fs *FString) (string, bool) {
	return fstringConstTextOf(fs)
}

// fstringConstTextOf is the fold itself, with no compiler state to consult: an interpolation is
// constant when its AST says so, which is a fact about the source rather than about a pass.
func fstringConstTextOf(fs *FString) (string, bool) {
	if fs == nil {
		return "", false
	}
	var sb strings.Builder
	for _, part := range fs.Parts {
		if part.Expr == nil {
			sb.WriteString(part.Lit)
			continue
		}
		txt, ok := fstringFieldConstText(part)
		if !ok {
			return "", false
		}
		sb.WriteString(txt)
	}
	return sb.String(), true
}

// fstringFieldConstText is the fold's half-field: the field's text as written, or no answer.
func fstringFieldConstText(part FStringPart) (string, bool) {
	if part.Spec != "" {
		return "", false
	}
	switch v := part.Expr.(type) {
	case *IntLit:
		return strconv.FormatInt(v.Value, 10), true
	case *FloatLit:
		return pyFloatRepr(v.Value), true
	case *StrLit:
		if part.Conv == ConvRepr {
			// Quoting a text is the PRINTER's act, not this fold's: the module's one tag-reading
			// renderer chooses the quote character (Python's preference for `'`, swapped when the text
			// contains one), and the print road already declined to splice a quoted literal into its
			// format string for exactly this reason — Gap R.192 measured `f"{'a'!r}"` answering with
			// double quotes at exit 0. A field the fold cannot state goes to the renderer, which is the
			// answer the reference gives.
			return "", false
		}
		return v.Value, true
	case *NoneLit:
		return "None", true
	case *Name:
		switch v.Value {
		case "None":
			return "None", true
		case "True":
			return "True", true
		case "False":
			return "False", true
		}
	}
	return "", false
}

// fstringFieldIsContainer answers whether a field denotes a container object. The question has
// to be asked BEFORE the rendering door is tried, because the door's last arm takes anything it
// can otherwise convince itself is a number — and a container lowered to that arm answers its
// heap handle, which is how `print(f"{xs}")` came to write `0` for `[1, 2]` at exit 0. The three
// witnesses are the ones the rest of the backend uses for "this expression names a container":
// the literal shape, the container records the assignment roads kept, and the checker's type.
func (g *irGen) fstringFieldIsContainer(e Expr) bool {
	if exprIsContainerShape(e) {
		return true
	}
	if g.nameIsContainerRecorded(e) {
		return true
	}
	if g.callAnswersContainerObject(e) {
		return true
	}
	if _, applies := g.seqBuildsContainer(e); applies {
		// `[1] + [2]` and `[1] * 3` name a container the door builds, and a field is a RENDERING position:
		// asking the object is the only honest answer, where the number arm below would write the handle
		// (roadmap Gap R.33, ADR 0365 — the same rule ADR 0359 set for an interpolation's field).
		return true
	}
	return containerKindFromTy(exprTyName(e)) != ""
}

// callAnswersContainerObject asks the whole-program scan whether a call hands back a container
// object the runtime owns — the answer a `def build(): return [1, 2]` materialises at its `ret`.
func (g *irGen) callAnswersContainerObject(e Expr) bool {
	c, ok := e.(*Call)
	if !ok {
		return false
	}
	nm, isName := c.Fn.(*Name)
	if !isName {
		return false
	}
	_, isContainer := g.containerAnswerOf(nm.Value)
	return isContainer
}

// fstringValue lowers an f-string to an i32 index into @str_tab: the language's text value.
func (g *irGen) fstringValue(b *strings.Builder, fs *FString) (string, error) {
	if txt, ok := fstringConstTextOf(fs); ok {
		return g.internStr(b, txt), nil
	}
	g.heapUsed = true
	var acc string
	for _, part := range fs.Parts {
		var reg string
		var err error
		if part.Expr == nil {
			if part.Lit == "" {
				continue
			}
			reg = g.internStr(b, part.Lit)
		} else if reg, err = g.fstringFieldReg(b, part); err != nil {
			return "", err
		}
		if reg == "" {
			continue
		}
		if acc == "" {
			acc = reg
			continue
		}
		acc = g.rtStrCall(b, "rt_str_cat", "i32 "+acc, "i32 "+reg)
	}
	if acc == "" {
		// f"" cannot be parsed (the parser refuses an empty f-string), so the only way here is a
		// source that lowered to nothing at all — an interpolation whose every part was empty text.
		return g.internStr(b, ""), nil
	}
	return acc, nil
}

// fstringFieldReg renders one field to the @str_tab index that holds its text. A spec goes to the
// spec engine and refuses where the engine cannot read the value; everything else goes to the
// door str()/repr() use, so a field, `str(field)` and `print(field)` cannot disagree about `7`
// versus `7.0` versus a heap handle (ADR 0258's one renderer, ADR 0303's one printer).
func (g *irGen) fstringFieldReg(b *strings.Builder, part FStringPart) (string, error) {
	if part.Spec != "" {
		lit, ok := g.formatFieldConst(part)
		if !ok {
			return "", fmt.Errorf("codegen: the format spec %q needs a value this backend can "+
				"read at compile time (AOT backend)", part.Spec)
		}
		return g.internStr(b, lit), nil
	}
	// A container is asked of its own object first — never of the number arm below.
	if g.fstringFieldIsContainer(part.Expr) {
		h, isContainer, cerr := g.renderPairContainer(b, part.Expr)
		if cerr != nil {
			return "", cerr
		}
		if !isContainer {
			return "", fmt.Errorf("codegen: the f-string field %s names a container this backend "+
				"cannot reach as an object, and a heap handle interpolated into a text is a number "+
				"wearing the container's clothes (roadmap L11.1, ADR 0359)", exprSurface(part.Expr))
		}
		quote := 0
		if part.Conv == ConvRepr {
			quote = 1
		}
		g.heapUsed = true
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_of_container(i32 %s, i32 %d)\n", t, h, quote))
		return t, nil
	}
	form := FormStr
	if part.Conv == ConvRepr {
		form = FormRepr
	}
	reg, handled, err := g.renderPair(b, part.Expr, form, fieldSpan(part))
	if err != nil {
		return "", err
	}
	if handled {
		return reg, nil
	}
	// A loop variable the loop bound from a number the compiler can see — a counter, or an element
	// of a container whose recorded kind is integer — has no BINDING to read (the loop wrote the
	// slot, not an assignment), which is why the str door's number arm declines it. The loop's own
	// claim answers for it: the digits are written by the same helper `str()` uses, and a name the
	// program also assigns to loses the claim, so the road cannot print the digits of a value some
	// other statement wrote (roadmap L11.1, ADR 0359; Gap R.252's per-loop rule).
	if g.fstringFieldIsLoopNumber(part.Expr) {
		v, verr := g.value(b, part.Expr)
		if verr != nil {
			return "", verr
		}
		return g.rtStrCall(b, "rt_str_of_int", "i32 "+v), nil
	}
	return "", fstringFieldRefusal(part)
}

// claimDigitLoopVar records that the loop now lowering bound this name from a number the compiler
// can see, and releaseDigitLoopVar takes the claim back when the loop ends. Both ask the map before
// touching it rather than assuming a scope was opened: a write to a nil map is a Go panic, and a
// compiler that crashes on a program is exit 2, which is the class of failure this backend files as a
// bug rather than an answer (roadmap Gap R.252's per-loop rule, ADR 0359's reader of it).
func (g *irGen) claimDigitLoopVar(name string) {
	if g.digitLoopVars == nil {
		g.digitLoopVars = map[string]bool{}
	}
	g.digitLoopVars[name] = true
}

// releaseDigitLoopVar takes the claim back: the name is what some other statement bound it to, and
// an interpolated field may not print the digits of a value this loop no longer writes.
func (g *irGen) releaseDigitLoopVar(name string) {
	if g.digitLoopVars == nil {
		return
	}
	delete(g.digitLoopVars, name)
}

// fstringFieldIsLoopNumber asks the loop's record, not the expression's spelling.
func (g *irGen) fstringFieldIsLoopNumber(e Expr) bool {
	nm, isName := e.(*Name)
	if !isName || g.digitLoopVars == nil {
		return false
	}
	if !g.digitLoopVars[nm.Value] {
		return false
	}
	// Nothing but this loop has ever written the name: an entry with a value in it is an
	// assignment (or an augmented one), and then the value is not the loop's alone.
	for _, bound := range g.strArgBindings(nm.Value) {
		if bound != nil {
			return false
		}
	}
	return true
}

// fstringFieldRefusal names the field and the half that is missing. It is the same rule as
// renderPairRefusal's — a rendering must never become the number underneath the value — phrased
// for the field's position, since here the reader is looking at an interpolation, not a call.
func fstringFieldRefusal(part FStringPart) error {
	half := "str"
	if part.Conv == ConvRepr {
		half = "repr"
	}
	return fmt.Errorf("codegen: the f-string field %s has no renderer: %s of it names no form this "+
		"backend can read — a number, a float, a verdict, None, a text or a container all say which "+
		"printer they want, and this one names none (roadmap L11.1, ADR 0359)",
		exprSurface(part.Expr), half)
}

// fieldSpan keeps the traps the rendering door raises attributable to the interpolation rather
// than to the enclosing statement.
func fieldSpan(part FStringPart) Span {
	if part.Expr != nil {
		return part.Expr.Span()
	}
	return Span{}
}

// fstringNeedsRenderer answers whether the print road can state every field of an interpolation
// in the one printf it builds. It cannot for two shapes, and both had been answering a number:
// the void, which has no digits (`f"v={None}"` wrote `v=0`), and a container, whose text is its
// slots and not its handle (`f"{xs}"` wrote `0` for `[1, 2]`). Those fields are not squeezed into
// the format string — the whole interpolation is built by the value road above and handed to the
// module's string printer, which is what keeps the print of a program and the same program with
// the text named (`t = f"{xs}"; print(t)`) one answer rather than two (roadmap L11.1, ADR 0359).
func (g *irGen) fstringNeedsRenderer(fs *FString) bool {
	for _, part := range fs.Parts {
		if part.Expr == nil {
			continue
		}
		if g.isNoneExpr(part.Expr) {
			return true
		}
		if g.fstringFieldIsContainer(part.Expr) {
			return true
		}
	}
	return false
}

// emitFStringPrint writes one interpolation argument as the text it is: build it, print it, and
// keep the writes inside this argument's chunk so Gap L.5's ordering still holds.
func (g *irGen) emitFStringPrint(b *strings.Builder, fs *FString) error {
	h, err := g.fstringValue(b, fs)
	if err != nil {
		return err
	}
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  call void @rt_print_str(i32 %s, i32 0)\n", h))
	return nil
}
