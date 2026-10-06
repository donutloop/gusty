package lang

// Honouring an f-string's format spec and conversion (roadmap Gap R.186, ADR 0299). This is the
// engine; `gustyc`'s one backend asks it through formatFieldConst below. It may not answer a spec by
// falling back to the plain value, because that is precisely the defect the row exists to end: eleven
// shapes printed the unformatted number at exit 0, so only the oracle leg could see `f"{3.5:.2f}"`
// answer `3.5`.
//
// Until ADR 0302 there were two callers of this engine — the AST interpreter and the compiled
// backend — and the row's lesson ("a form added to one table and not the other") came from that
// shape. With one caller the rule still holds, because the engine and the print door are separate
// pieces of text: FormatNumber/FormatRawInt are the single answer for every spec.

import (
	"strconv"
)

// numFormatOf is the single entry the spec asks through: the same spec, from an int or a float,
// through the same engine, with the value's own type deciding the empty spec.
func numFormatOf(f float64, spec string, isFloat bool) (string, bool, error) {
	if isFloat {
		return FormatNumber(f, spec)
	}
	return FormatRawInt(f, spec)
}

// formatFieldConst is the compiled backend's half: it renders one interpolated field through the
// SAME engine the interpreter used, from whatever the AOT backend can read at compile time. Where it
// cannot read the value the caller refuses rather than emitting a plain %d, because a silent
// unformatted answer is the wrong-number-at-exit-0 this row exists to end.
func (g *irGen) formatFieldConst(part FStringPart) (string, bool) {
	text := ""
	isFloat := false
	numeric := false
	// A container interpolated into an f-string has no reading on this road at all: the value is a
	// heap object and the printf door below can only take an i32 or an i8*, which is how `f"{[1,2]}"`
	// came to emit `printf(..., i32 @.lst1)` — a global address where a heap handle belongs, rejected
	// by llc as an invalid MODULE and an exit 2 (ADR 0166's forbidden class). The plain case is
	// handled by the print door's own container arm; anything reaching HERE with a spec or a
	// conversion is refused rather than lowered (Gap R.186, ADR 0299).
	if exprIsContainerShape(part.Expr) {
		return "", false
	}
	switch v := part.Expr.(type) {
	case *FloatLit:
		numeric, isFloat = true, true
		text = pyFloatRepr(v.Value)
	case *IntLit:
		numeric = true
		text = strconv.FormatInt(v.Value, 10)
	case *StrLit:
		text = v.Value
	}
	if part.Conv == ConvStr {
		if sl, ok := part.Expr.(*StrLit); ok {
			text = sl.Value
		}
	} else if part.Conv == ConvRepr {
		switch v := part.Expr.(type) {
		case *StrLit:
			text = quoteString(v.Value)
		case *IntLit:
			text = strconv.FormatInt(v.Value, 10)
		case *FloatLit:
			text = pyFloatRepr(v.Value)
		}
	}
	if part.Spec == "" {
		return text, true
	}
	if numeric {
		out, ok, err := numFormatOf(needFloatOf(part.Expr), part.Spec, isFloat)
		if err != nil || !ok {
			return "", false
		}
		return out, true
	}
	// A spec over a text is width and alignment only; a presentation type, grouping or precision on
	// a non-number is refused, exactly as the interpreter refuses it.
	sp, err := parseSpec(part.Spec)
	if err != nil || sp.ptype != 0 || sp.group != 0 || sp.hasPrec {
		return "", false
	}
	return applyAlign(text, sp), true
}

// needFloatOf reads a constant literal's numeric value, whichever literal it turned out to be.
func needFloatOf(e Expr) float64 {
	switch v := e.(type) {
	case *FloatLit:
		return v.Value
	case *IntLit:
		return float64(v.Value)
	}
	return 0
}

// inPlaceMutations is the one list of receiver methods that CHANGE their receiver and hand back
// nothing. It is a table rather than a switch in each road because two roads must agree on it: the
// call road, which lowers the mutation and produces no value, and the print/str()/REPL road, which
// has to know that the absence of a value is the void and not a number. When they disagreed, the
// interpreter printed the container -- `[1, 2]` for `xs.append(2)` -- and the compiled leg emitted a
// printf with a missing operand, which llc rejects (roadmap Gap R.187, ADR 0300).
//
// `pop` and `popitem` are deliberately NOT here: they remove something AND answer with it, which is
// the whole reason `while xs: x = xs.pop()` exists.
var inPlaceMutations = map[string]bool{
	"append":  true,
	"extend":  true,
	"insert":  true,
	"sort":    true,
	"reverse": true,
	"add":     true,
	"discard": true,
	"remove":  true,
	"update":  true,
	"clear":   true,
}

// callIsInPlaceMutation asks whether a call is a receiver method that answers the void. It reads the
// call's SHAPE -- an attribute call on something that actually is a container -- because a program
// may define its own `add`, and a user function of that name answers whatever its body returns.
func (g *irGen) callIsInPlaceMutation(c *Call) bool {
	attr, ok := c.Fn.(*Attr)
	if !ok {
		return false
	}
	if !inPlaceMutations[attr.Name.Value] {
		return false
	}
	// A method on a container literal or a recorded container variable is the language's own mutator;
	// a method on a string, a class instance or a user object is somebody else's method entirely.
	if exprIsContainerShape(attr.Obj) {
		return true
	}
	if _, isName := attr.Obj.(*Name); isName {
		// A variable the container records know about: the same records the call road consulted to
		// decide it was a mutation at all, so the two roads cannot disagree about who answered.
		return g.nameIsContainerRecorded(attr.Obj)
	}
	return false
}
