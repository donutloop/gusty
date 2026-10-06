package lang

// An f-string's format spec and conversion used to be cut off at parse time and thrown away
// (roadmap Gap R.186, the f-string half of L12.8 / Gap R.60, ADR 0299): `stripFormatSpec` returned
// `src[:i]` at the first top-level `:` and no AST field kept the remainder, so every road that
// renders an interpolation answered the PLAIN value — `f"{3.5:.2f}"` printed `3.5` where CPython
// prints `3.50`, `f"{7:05d}"` printed `7`, `f"{255:x}"` printed `255`, and `f"{3.5:>6}"` printed no
// padding — on BOTH backends, at exit 0. Parity could not see any of it because the engines agreed.
//
// The spec now travels in the AST (`FStringPart.Spec` / `.Conv`) and both engines format through
// THIS file, so a spec can no longer mean one thing to `print`, another to `str()` and a third to
// the interpreter. The rules implemented are CPython's, restricted to what the language's value
// model can answer today; anything else REFUSES with a sentence naming the spec, rather than
// answering the plain value again — a wrong number at exit 0 is the failure this row exists to end.

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// FormatConv is the `!r` / `!s` / `!a` conversion of an interpolation.
type FormatConv int

const (
	ConvNone FormatConv = iota
	ConvStr
	ConvRepr
	ConvAscii
)

// SpecErr names a spec this language cannot honour. It is a front-end refusal (exit 1), not a
// runtime error: a program must never render a spec it asked for as if the spec were absent.
type SpecErr struct {
	Spec string
	Why  string
}

func (e *SpecErr) Error() string {
	return fmt.Sprintf("format spec %q cannot be honoured: %s", e.Spec, e.Why)
}

// splitSpecConv takes the inside of one `{...}` field and returns the expression source, the
// conversion (if any) and the format spec (if any). The scan is bracket-aware, so a dict literal or
// a slice inside the expression cannot have its `:` mistaken for the spec separator — the same
// question the old stripFormatSpec asked, answered once instead of twice.
func splitSpecConv(inner string) (exprSrc string, conv FormatConv, spec string, err error) {
	// Split at the first depth-0 `:` that is not inside a string literal. A `:` inside []/{}/() or
	// inside quotes belongs to the expression, which is the same bracket-aware question the old
	// stripFormatSpec asked -- the difference is that the remainder is now KEPT.
	cut := -1
	depth := 0
	var quote byte
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth > 0 {
				depth--
			}
		case '\'', '"':
			quote = c
		case ':':
			if depth == 0 {
				cut = i
			}
		}
		if cut >= 0 {
			break
		}
	}
	exprPart, specPart := inner, ""
	if cut >= 0 {
		exprPart, specPart = inner[:cut], inner[cut+1:]
	}
	// Peel a conversion off the END of the expression part, which is the only place CPython allows
	// it; a `!` anywhere else is a not-operator inside the expression. Stripping it unconditionally,
	// as an earlier version did, is what lost the spec in `x!r:>8`.
	exprPart = strings.TrimRight(exprPart, " \t")
	if len(exprPart) >= 2 && exprPart[len(exprPart)-2] == '!' {
		switch exprPart[len(exprPart)-1] {
		case 'r':
			conv = ConvRepr
		case 's':
			conv = ConvStr
		case 'a':
			return "", ConvNone, "", &SpecErr{Spec: "!a", Why: "the ascii() conversion is not in this language"}
		}
		if conv != ConvNone {
			exprPart = strings.TrimRight(exprPart[:len(exprPart)-2], " \t")
		}
	}
	return exprPart, conv, specPart, nil
}

// specParts is the parsed form of a mini-language spec.
type specParts struct {
	fill         byte
	align        byte
	sign         byte // 0, '+' or ' '
	zeroPad      bool
	width        int
	prec         int
	hasPrec      bool
	group        byte // 0 or ','
	pct          bool
	floatOperand bool // the caller says the value it holds is a float, not an integer
	ptype        byte
}

// parseSpec reads the presentation type first (CPython's rule), then the optional
// [[fill]align][sign][#][0][width][grouping][.precision][type].
func parseSpec(spec string) (*specParts, error) {
	sp := &specParts{}
	if spec == "" {
		return sp, nil
	}
	// Presentation type: the last character, when the rest is not just a sign/zero.
	t := spec[len(spec)-1]
	if (t >= 'a' && t <= 'z') || (t >= 'A' && t <= 'Z') || t == '%' {
		sp.ptype = t
		spec = spec[:len(spec)-1]
	}
	// Optional `;` trailing the type (CPython allows it); nothing else about it is supported.
	if strings.HasSuffix(spec, ";") {
		return nil, &SpecErr{Spec: spec, Why: "the `;` thousands separator is not supported"}
	}
	// Precision.
	if i := strings.LastIndexByte(spec, '.'); i >= 0 && isDigits(spec[i+1:]) {
		sp.hasPrec = true
		sp.prec, _ = strconv.Atoi(spec[i+1:])
		spec = spec[:i]
	} else if strings.Contains(spec, ".") {
		return nil, &SpecErr{Spec: spec, Why: "a `.` must be followed by digit precision"}
	}
	// Grouping.
	if strings.HasSuffix(spec, "_") {
		return nil, &SpecErr{Spec: spec, Why: "`_` digit grouping is not supported"}
	}
	if strings.HasSuffix(spec, ",") {
		sp.group = ','
		spec = spec[:len(spec)-1]
	}
	// CPython's grammar is [[fill]align][sign][#][0][width][grouping][.precision][type], read in that
	// order, so the fields are peeled from the FRONT. An align may stand alone (`>6`) or follow a fill
	// (`*^8`); a lone `0` at the front is the zero-pad flag, not a fill.
	if len(spec) >= 2 && strings.IndexByte("<>^", spec[1]) >= 0 && spec[0] != '0' {
		sp.fill = spec[0]
		sp.align = spec[1]
		spec = spec[2:]
	} else if len(spec) >= 1 && strings.IndexByte("<>^", spec[0]) >= 0 {
		sp.align = spec[0]
		sp.fill = ' '
		spec = spec[1:]
	}
	if len(spec) > 0 && (spec[0] == '+' || spec[0] == '-' || spec[0] == ' ') {
		sp.sign = spec[0]
		spec = spec[1:]
	}
	if len(spec) > 0 && spec[0] == '#' {
		return nil, &SpecErr{Spec: spec, Why: "the `#` alternate form is not supported"}
	}
	if len(spec) > 1 && spec[0] == '0' && spec[1] >= '0' && spec[1] <= '9' {
		sp.zeroPad = true
		spec = spec[1:]
	}
	j := 0
	for j < len(spec) && spec[j] >= '0' && spec[j] <= '9' {
		j++
	}
	if j > 0 {
		sp.width, _ = strconv.Atoi(spec[:j])
		spec = spec[j:]
	}
	if spec != "" {
		return nil, &SpecErr{Spec: spec, Why: "that field is not part of the format mini-language"}
	}
	return sp, nil
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// groupThousands inserts `sep` every three digits of the integer part of a rendered number.
func groupThousands(s string, sep byte) string {
	neg := strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+")
	body := s
	if neg {
		body = s[1:]
	}
	frac := ""
	if i := strings.IndexByte(body, '.'); i >= 0 {
		frac, body = body[i:], body[:i]
	}
	var out []byte
	for i, c := range []byte(body) {
		if i > 0 && (len(body)-i)%3 == 0 {
			out = append(out, sep)
		}
		out = append(out, c)
	}
	if neg {
		return s[:1] + string(out) + frac
	}
	return string(out) + frac
}

// FormatNumber renders a Go float64 the caller holds as a FLOAT -- CPython's format(value, spec) on
// a float, where the empty spec is repr, so 2.0 answers "2.0".
func FormatNumber(f float64, spec string) (string, bool, error) {
	sp, err := parseSpec(spec)
	if err != nil {
		return "", false, err
	}
	sp.floatOperand = true
	return formatWith(f, sp)
}

// FormatRawInt renders a whole number the caller holds as an INTEGER -- CPython's format on an int,
// where the empty spec is the digits, so 2 answers "2" and not "2.0".
func FormatRawInt(f float64, spec string) (string, bool, error) {
	sp, err := parseSpec(spec)
	if err != nil {
		return "", false, err
	}
	return formatWith(f, sp)
}

func formatWith(f float64, sp *specParts) (string, bool, error) {
	spec := specText(sp)
	isInt := f == math.Trunc(f) && !math.IsInf(f, 0) && !math.IsNaN(f)
	var body string
	switch sp.ptype {
	case 0:
		// The empty spec is repr of whatever the caller holds: an int's digits, a float's repr with
		// its trailing `.0`. Guessing from the value would make `f"{2}"` and `f"{2.0}"` agree when the
		// reference says they must not.
		if isInt && !sp.floatOperand {
			body = strconv.FormatInt(int64(f), 10)
		} else {
			body = pyFloatRepr(f)
			// `format(1234567.0, ",")` is "1,234,567.0": a WHOLE-valued float still has an integer part
			// to group, which the bare `g` repr hides by answering with no fraction at all. Rendering it
			// with one decimal when a grouping was asked for is what makes the digits exist.
			if sp.group == ',' && isInt && !strings.ContainsAny(body, "eE") {
				body = strconv.FormatFloat(f, 'f', 1, 64)
			}
		}
	case 'f', 'F':
		prec := 6
		if sp.hasPrec {
			prec = sp.prec
		}
		body = strconv.FormatFloat(f, 'f', prec, 64)
	case 'e':
		body = formatExp(f, sp, 'e')
	case 'E':
		body = formatExp(f, sp, 'E')
	case 'g', 'G':
		prec := 6
		if sp.hasPrec {
			prec = sp.prec
		}
		body = strconv.FormatFloat(f, byte(strings.ToLower(string(sp.ptype))[0]), prec, 64)
	case '%':
		prec := 6
		if sp.hasPrec {
			prec = sp.prec
		}
		// CPython multiplies by 100 and then formats as 'f' with the precision, so `.2%` is two
		// decimals of the scaled value and NOT the six of a bare 'f' type.
		body = strconv.FormatFloat(f*100, 'f', prec, 64) + "%"
	case 'd':
		if !isInt {
			return "", false, &SpecErr{Spec: spec, Why: "'d' needs a whole number"}
		}
		body = strconv.FormatInt(int64(f), 10)
		sp.hasPrec = false
	case 'x':
		if !isInt {
			return "", false, &SpecErr{Spec: spec, Why: "'x' needs a whole number"}
		}
		body = strconv.FormatInt(int64(f), 16)
	case 'X':
		if !isInt {
			return "", false, &SpecErr{Spec: spec, Why: "'X' needs a whole number"}
		}
		body = strings.ToUpper(strconv.FormatInt(int64(f), 16))
	case 'b':
		if !isInt {
			return "", false, &SpecErr{Spec: spec, Why: "'b' needs a whole number"}
		}
		body = strconv.FormatInt(int64(f), 2)
	case 'o':
		if !isInt {
			return "", false, &SpecErr{Spec: spec, Why: "'o' needs a whole number"}
		}
		body = strconv.FormatInt(int64(f), 8)
	case 'n':
		return "", false, &SpecErr{Spec: spec, Why: "'n' is locale-dependent and this language has no locale"}
	default:
		return "", false, &SpecErr{Spec: spec, Why: "that presentation type is not supported"}
	}
	if sp.group == ',' {
		// Grouping needs an integer part written in digits. `format(1e+20, ",")` is "1e+20" — there
		// is nothing to group in an exponent form — so an empty-spec value that rendered through the
		// float repr with an `e` in it is left alone.
		if !strings.ContainsAny(body, "eE") {
			body = groupThousands(body, ',')
		}
	}
	// A sign field decorates a NON-negative rendering; a negative body already carries its own `-`
	// and CPython leaves it alone. `format(-3.5, " .2f")` is "-3.50" and `format(3.5, " .2f")` is
	// " 3.50" -- the space is not a pad, so it must survive into the aligned text.
	if sp.sign != 0 && f >= 0 && !strings.HasPrefix(body, "+") && !strings.HasPrefix(body, " ") {
		if sp.sign == '+' {
			body = "+" + body
		} else if sp.sign == ' ' {
			body = " " + body
		}
	}
	return applyAlign(body, sp), true, nil
}

// formatExp renders scientific notation with CPython's rules: at least two exponent digits, and a
// precision of 0 still prints `e+00` rather than dropping the fraction.
func formatExp(f float64, sp *specParts, e byte) string {
	prec := 6
	if sp.hasPrec {
		prec = sp.prec
	}
	s := strconv.FormatFloat(f, e, prec, 64)
	// Go emits "1e+00"; CPython emits "1e+00" too, but pads two digits minimum the same way.
	return s
}

// FormatInt renders an integer value, which the compiled backend needs because its ints are i32
// and its floats are heap objects — the same spec, the same digits, from whichever word holds it.
func FormatInt(v int64, spec string) (string, bool, error) {
	return FormatRawInt(float64(v), spec)
}

// specText rebuilds a spec-shaped string so a refusal still quotes something shaped like what the
// program wrote rather than an internal struct.
func specText(sp *specParts) string {
	var b []byte
	if sp.fill != 0 {
		b = append(b, sp.fill)
	}
	if sp.align != 0 {
		b = append(b, sp.align)
	}
	if sp.sign != 0 {
		b = append(b, sp.sign)
	}
	if sp.zeroPad {
		b = append(b, '0')
	}
	if sp.width != 0 {
		b = append(b, strconv.Itoa(sp.width)...)
	}
	if sp.group != 0 {
		b = append(b, sp.group)
	}
	if sp.hasPrec {
		b = append(b, '.')
		b = append(b, strconv.Itoa(sp.prec)...)
	}
	if sp.ptype != 0 {
		b = append(b, sp.ptype)
	}
	return string(b)
}

func applyAlign(body string, sp *specParts) string {
	if sp.width == 0 || sp.width <= len([]rune(body)) {
		return body
	}
	if sp.zeroPad && sp.align == 0 {
		// Zero padding goes AFTER the sign and the width counts the whole field, sign included -- so
		// `format(-4, "05d")` is "-0004" (four digits) and not "-00004" (five digits plus a sign).
		sign := ""
		digits := body
		for _, s := range []string{"-", "+", " "} {
			if strings.HasPrefix(body, s) {
				sign, digits = s, body[1:]
				break
			}
		}
		pad := sp.width - len([]rune(body))
		if pad <= 0 {
			return body
		}
		return sign + strings.Repeat("0", pad) + digits
	}
	// CPython's `0` with an explicit align is a `=`-align equivalent; only the padded forms here
	// matter, and a fill with no align centres nothing: pad per the align, else right-align.
	align := sp.align
	if align == 0 {
		align = '>'
		if sp.zeroPad {
			align = '='
		}
	}
	if sp.fill == 0 {
		sp.fill = ' '
	}
	pad := sp.width - len([]rune(body))
	switch align {
	case '<':
		return body + strings.Repeat(string(sp.fill), pad)
	case '^':
		left := pad / 2
		right := pad - left
		return strings.Repeat(string(sp.fill), left) + body + strings.Repeat(string(sp.fill), right)
	case '=':
		sign := ""
		digits := body
		for _, s := range []string{"-", "+", " "} {
			if strings.HasPrefix(body, s) {
				sign, digits = s, body[1:]
				break
			}
		}
		return sign + strings.Repeat(string(sp.fill), pad) + digits
	default: // '>'
		return strings.Repeat(string(sp.fill), pad) + body
	}
}
