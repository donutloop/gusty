package lang

import (
	"math"
	"strings"
	"unicode"
)

// The answers the compiler folds with.
//
// These are the small, total functions the constant folder and the IR emitter share: CPython's
// text transforms, the slice/index arithmetic, the floor-division pair, the dunder names an
// operator desugars to, and the generator question a function body answers. They used to live
// beside the AST interpreter and be called from both paths, which meant "the two backends agree"
// was a property of two call sites rather than of one definition (ADR 0302 retires the second
// backend; the functions stay, because what they say is the language's answer, not one
// backend's). Roadmap Gaps R.28/R.30 (the floor pair), R.183 (zfill) and L11.1 (element tags)
// are each measured against this file being the only place the answer is written.

// reverseStr returns the reverse of s.
func reverseStr(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// zfillTo pads a numeric-looking text with leading zeros to width, CPython's way: after any leading
// sign, so "-42" at width 5 is "-0042" and not "00-42". Both engines had their own left-pad and agreed
// with each other rather than with the reference (roadmap Gap R.183, ADR 0297).
func zfillTo(s string, width int) string {
	if len(s) >= width {
		return s
	}
	pad := strings.Repeat("0", width-len(s))
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		return s[:1] + pad + s[1:]
	}
	return pad + s
}

// pyReprString renders a Go string as Python would repr it.
func pyReprString(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == quote || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c == '\n':
			sb.WriteString("\\n")
		case c == '\t':
			sb.WriteString("\\t")
		case c == '\r':
			sb.WriteString("\\r")
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte(quote)
	return sb.String()
}

// capitalize returns s with the first rune uppercased and the rest lowercased.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

// title returns s with the first rune of each whitespace-separated word uppercased.
func title(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		if prev == ' ' {
			prev = r
			return unicode.ToUpper(r)
		}
		prev = r
		return unicode.ToLower(r)
	}, s)
}

// swapcase returns s with each rune case swapped.
func swapcase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// pySliceIndices computes the normalized start/stop/step for a slice
// following CPython's PySlice_GetIndicesEx semantics (used by s[a:b:c]).
func pySliceIndices(low, high, step int64, hasLow, hasHigh bool, n int64) (start, stop, stp int64) {
	if step > 0 {
		if hasLow {
			if low < 0 {
				low = maxInt(n+low, 0)
			} else {
				low = minInt(low, n)
			}
		} else {
			low = 0
		}
		if hasHigh {
			if high < 0 {
				high = maxInt(n+high, 0)
			} else {
				high = minInt(high, n)
			}
		} else {
			high = n
		}
		return low, high, step
	}
	// step < 0
	if hasLow {
		if low < 0 {
			low = maxInt(n+low, -1)
		} else {
			low = minInt(low, n-1)
		}
	} else {
		low = n - 1
	}
	if hasHigh {
		if high < 0 {
			high = maxInt(n+high, -1)
		} else {
			high = minInt(high, n-1)
		}
	} else {
		high = -1
	}
	return low, high, step
}

// normPosIndex turns a positional index into an element offset, counting from the end
// when it is negative: `xs[-1]` is the last element, `s[-1]` the last character. `pop`
// and slicing already behaved this way; read and write now share the same rule
// (roadmap L11.4, ADR 0210). It is deliberately *not* applied to dict and set subscripts
// — those are keys, and `-1` is a key you can store (`d[-1] = v` works in Python too).
// The result may still be out of range; the caller bounds-checks it.
func normPosIndex(idx, length int64) int64 {
	if idx < 0 {
		return idx + length
	}
	return idx
}

// floorDiv and floorMod are the pair, defined together because they are only correct
// together. Go's `/` and `%` truncate toward zero, so mixed-sign operands break the
// identity Python guarantees — a == (a // b) * b + (a % b) — which is why `-7 // 2` must
// be -4 with `-7 % 2` equal to 1, not -3 with -1 (roadmap Gaps R.28, R.30: the compiled
// backend had both halves wrong, the interpreter only the modulo half). One definition,
// used by the interpreter, the codegen constant folder and, in IR form, the emitted
// sdiv/srem corrections, so no backend can drift toward C again.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 {
	r := a % b
	if r != 0 && ((r < 0) != (b < 0)) {
		r += b
	}
	return r
}

// floorModFloat is the same rule for floats. libm's fmod (Go's math.Mod, LLVM's frem) is
// the *truncated* remainder, so `-7.0 % 2.0` answers -1 unless it is corrected the same
// way; the result carries the sign of the divisor.
func floorModFloat(a, b float64) float64 {
	r := math.Mod(a, b)
	if r == 0 {
		// An exact remainder keeps the divisor's sign, as IEEE requires: `7.5 % -0.5` is
		// -0.0, not 0.0. fmod's zero carries the dividend's sign instead, so the two
		// languages would print different renderings of the same number.
		return math.Copysign(0, b)
	}
	if (r < 0) != (b < 0) {
		r += b
	}
	return r
}

// dunderForBinOp returns the dunder method name for a binary operator, if any.
func dunderForBinOp(op string) string {
	switch op {
	case "+":
		return "__add__"
	case "-":
		return "__sub__"
	case "*":
		return "__mul__"
	case "/":
		return "__truediv__"
	case "//":
		return "__floordiv__"
	case "%":
		return "__mod__"
	case "**":
		return "__pow__"
	case "==":
		return "__eq__"
	case "!=":
		return "__ne__"
	case "<":
		return "__lt__"
	case "<=":
		return "__le__"
	case ">":
		return "__gt__"
	case ">=":
		return "__ge__"
	}
	return ""
}

// reflectedDunder returns the reflected dunder name for a dunder method name,
// used when the left operand is not overloaded but the right operand is.
func reflectedDunder(name string) string {
	switch name {
	case "__add__":
		return "__radd__"
	case "__sub__":
		return "__rsub__"
	case "__mul__":
		return "__rmul__"
	case "__truediv__":
		return "__rtruediv__"
	case "__floordiv__":
		return "__rfloordiv__"
	case "__mod__":
		return "__rmod__"
	case "__pow__":
		return "__rpow__"
	case "__eq__":
		return "__eq__"
	case "__ne__":
		return "__ne__"
	case "__lt__":
		return "__gt__"
	case "__le__":
		return "__ge__"
	case "__gt__":
		return "__lt__"
	case "__ge__":
		return "__le__"
	}
	return ""
}

func containsYield(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
			return true
		case *YieldFromStmt:
			return true
		case *IfStmt:
			if containsYield(s.Then) {
				return true
			}
			for _, e := range s.Elifs {
				if containsYield(e.Then) {
					return true
				}
			}
			if containsYield(s.Else) {
				return true
			}
		case *WhileStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *ForStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *MatchStmt:
			for _, c := range s.Cases {
				if containsYield(c.Body) {
					return true
				}
			}
		case *FuncDef:
			if containsYield(s.Body) {
				return true
			}
		}
	}
	return false
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
