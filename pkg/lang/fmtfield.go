package lang

// The interpreter half of honouring an f-string's format spec and conversion (roadmap Gap R.186,
// ADR 0299). The compiled backend asks the same engine the same question in codegen.go; neither one
// may answer a spec by falling back to the plain value, because that is precisely the defect: eleven
// shapes printed the unformatted number on BOTH backends at exit 0, so parity was blind to it and
// only the oracle leg could see `f"{3.5:.2f}"` answer `3.5`.

import (
	"fmt"

	"strconv"
)

// formatField renders one interpolation of an f-string under its conversion and spec.
func (e *Evaluator) formatField(v int64, part FStringPart) (string, error) {
	// The conversion decides WHICH rendering the spec is then applied to: `!r` takes repr, `!s` and
	// the default take str. CPython applies the spec to the conversion's result, so `f"{[1]!r}"`
	// formats the string "[1]" and `f"{3.5!r:>6}"` pads a repr's text. For the numbers in this
	// language str and repr are the same call (ADR 0233), so the difference shows up on containers.
	text := e.Repr(v)
	if part.Conv == ConvStr {
		if o, ok := e.heap[v]; ok && o.kind == "str" {
			text = o.sval
		}
	}
	if part.Spec == "" {
		return text, nil
	}
	// A spec over a number goes through the shared engine. The value's OWN type decides the empty
	// spec, so the float heap handle is asked, not just the payload.
	if num, isFloat, ok := e.numberForFormat(v); ok {
		out, ok2, err := numFormatOf(num, part.Spec, isFloat)
		if err != nil {
			return "", exnError("ValueError", err.Error())
		}
		if !ok2 {
			return "", exnError("ValueError", fmt.Sprintf("cannot honour format spec %q", part.Spec))
		}
		return out, nil
	}
	// A spec over anything else is applied to the rendered text. CPython routes format(value, spec)
	// through the value's own __format__, and the only types here with a real numeric __format__ are
	// the numbers: a text or a container supports width and alignment and refuses a presentation
	// type, a grouping or a precision, rather than quietly answering the unformatted value.
	sp, err := parseSpec(part.Spec)
	if err != nil {
		return "", exnError("ValueError", err.Error())
	}
	// A container with ANY spec is what the reference refuses outright: `format([1,2], "")` is
	// "[1, 2]" and `format([1,2], ">8")` is a TypeError, because a list has no __format__ beyond
	// the object default. Answering a padded "[1, 2]" would be a wrong answer the reference does not
	// give, so the whole spec is refused, not just its numeric fields (Gap R.186, ADR 0299).
	if e.isContainerValue(v) {
		return "", exnError("TypeError",
			fmt.Sprintf("unsupported format string passed to %s.__format__", e.containerTypeName(v)))
	}
	if sp.ptype != 0 || sp.group != 0 || sp.hasPrec {
		return "", exnError("ValueError",
			fmt.Sprintf("cannot honour format spec %q for a value that is not a number", part.Spec))
	}
	return applyAlign(text, sp), nil
}

// numberForFormat reports a numeric payload and the value's own kind. The kind is part of the answer
// because CPython's empty spec is repr of that kind: format(2, "") is "2" and format(2.0, "") is
// "2.0", and a road that could not tell them apart would make `f"{2}"` and `f"{2.0}"` agree when the
// reference says they must not.
func (e *Evaluator) numberForFormat(v int64) (float64, bool, bool) {
	if o, ok := e.heap[v]; ok {
		if o.kind == "float" {
			return o.fval, true, true
		}
		return 0, false, false
	}
	// Anything not in the heap is a raw int64 (the interpreter's ints are unboxed).
	if v == e.noneVal {
		return 0, false, false
	}
	return float64(v), false, true
}

// numFormatOf is the single entry both engines call: the same spec, from an int or a float, through
// the same engine, with the value's own type deciding the empty spec.
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

// isContainerValue and containerTypeName let the format road ask the reference's question of a
// container: a list, dict or set has no __format__ of its own, so any spec on it is a TypeError.
func (e *Evaluator) isContainerValue(v int64) bool {
	o, ok := e.heap[v]
	if !ok {
		return false
	}
	switch o.kind {
	case "list", "dict", "set":
		return true
	}
	return false
}

func (e *Evaluator) containerTypeName(v int64) string {
	if o, ok := e.heap[v]; ok {
		switch o.kind {
		case "dict":
			return "dict"
		case "set":
			return "set"
		}
	}
	return "list"
}
