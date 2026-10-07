package lang

// Gap R.185's sibling: an f-string's format spec used to be cut off at parse time and thrown away,
// so every rendering road answered the PLAIN value — `f"{3.5:.2f}"` printed `3.5` where the reference
// prints `3.50`, `f"{7:05d}"` printed `7`, `f"{255:x}"` printed `255` — on the compiled backend, at exit 0.
// Parity could not see a single one because the engines agreed; only the oracle leg looked.
//
// These tables pin the shared spec engine itself. Every expectation was read from
// `format(value, spec)` on the pinned reference, and the int/float split matters: CPython's empty
// spec is repr of the value's own type, so `format(2, "")` is "2" and `format(2.0, "")` is "2.0",
// which is why the engine takes the caller's word rather than inferring from the digits.

import "testing"

func TestFormatSpecRendersWhatTheReferenceRenders(t *testing.T) {
	floats := []struct {
		v    float64
		spec string
		want string
	}{
		{3.5, "", "3.5"}, {2.0, "", "2.0"}, {0.1, "", "0.1"}, {1e20, "", "1e+20"},
		{3.5, ".2f", "3.50"}, {3.5, ".0f", "4"}, {2.5, ".0f", "2"}, {3.5, "f", "3.500000"},
		{-4.0, ".1f", "-4.0"}, {0.0, ".2f", "0.00"},
		{3.5, ">6", "   3.5"}, {3.5, "<6", "3.5   "}, {3.5, "^7", "  3.5  "},
		{3.5, "*^7", "**3.5**"}, {2.0, ">10", "       2.0"},
		{3.14159, ".3e", "3.142e+00"}, {3.14159, ".2E", "3.14E+00"}, {2.0, "e", "2.000000e+00"},
		{0.25, ".2%", "25.00%"},
		{1234567.0, ",", "1,234,567.0"}, {1e20, ",", "1e+20"},
		{3.5, "+.2f", "+3.50"}, {-3.5, " .2f", "-3.50"}, {3.5, " .2f", " 3.50"},
	}
	for _, c := range floats {
		got, ok, err := FormatNumber(c.v, c.spec)
		if err != nil {
			t.Errorf("FormatNumber(%v, %q) refused: %v", c.v, c.spec, err)
			continue
		}
		if !ok {
			t.Errorf("FormatNumber(%v, %q) declined without a reason", c.v, c.spec)
			continue
		}
		if got != c.want {
			t.Errorf("FormatNumber(%v, %q) = %q, reference answers %q", c.v, c.spec, got, c.want)
		}
	}

	ints := []struct {
		v    int64
		spec string
		want string
	}{
		{2, "", "2"}, {0, "", "0"}, {-4, "", "-4"},
		{7, "05d", "00007"}, {-4, "05d", "-0004"}, {7, ">6", "     7"}, {1234567, "08d", "01234567"},
		{255, "x", "ff"}, {255, "X", "FF"}, {5, "b", "101"}, {8, "o", "10"},
		{1234567, ",", "1,234,567"}, {255, ">4x", "  ff"}, {255, "04x", "00ff"},
		{7, "+d", "+7"},
	}
	for _, c := range ints {
		got, ok, err := FormatInt(c.v, c.spec)
		if err != nil {
			t.Errorf("FormatInt(%v, %q) refused: %v", c.v, c.spec, err)
			continue
		}
		if !ok {
			t.Errorf("FormatInt(%v, %q) declined without a reason", c.v, c.spec)
			continue
		}
		if got != c.want {
			t.Errorf("FormatInt(%v, %q) = %q, reference answers %q", c.v, c.spec, got, c.want)
		}
	}
}

// A spec the language cannot honour must REFUSE with a sentence naming it. Answering the plain value
// — which is what every one of these used to do, silently — is the failure this row exists to end.
func TestFormatSpecRefusesWhatItCannotHonour(t *testing.T) {
	for _, c := range []struct {
		v    float64
		spec string
	}{
		{2.5, "n"},   // locale-dependent, and this language has no locale
		{3.5, ";"},   // the `;` thousands separator
		{3.5, "_"},   // `_` grouping
		{3.5, "q"},   // no such presentation type
		{3.5, "."},   // a `.` with no digits after it
		{2.5, "d"},   // 'd' on a value that is not a whole number
		{2.5, "x"},   // 'x' on a value that is not a whole number
		{3.5, ">6{"}, // a nested replacement field (dynamic width)
		{3.5, "#"},   // the alternate form
	} {
		if got, ok, err := FormatNumber(c.v, c.spec); err == nil {
			t.Errorf("FormatNumber(%v, %q) answered %q instead of refusing", c.v, c.spec, got)
		} else if ok {
			t.Errorf("FormatNumber(%v, %q) refused but claimed success", c.v, c.spec)
		}
	}
}

// splitSpecConv is the one bracket-aware scan the parser now uses. It must keep the expression, the
// conversion and the spec apart even when the expression itself contains a `:` (a slice, a dict
// literal) or a quoted `:` — the old code split at the first top-level colon and dropped the tail.
func TestSplitSpecConvKeepsTheSpecItUsedToThrowAway(t *testing.T) {
	cases := []struct {
		inner, expr string
		conv        FormatConv
		spec        string
	}{
		{"3.5:.2f", "3.5", ConvNone, ".2f"},
		{"7:05d", "7", ConvNone, "05d"},
		{"x!r", "x", ConvRepr, ""},
		{"x!s", "x", ConvStr, ""},
		{"x!r:>8", "x", ConvRepr, ">8"},
		{"d['k']", "d['k']", ConvNone, ""},
		// a colon inside a string literal is not a spec separator
		{`d["a:b"]`, `d["a:b"]`, ConvNone, ""},
		// a slice's colon is at depth 1 inside brackets, so the spec still separates after them
		{"xs[1:]", "xs[1:]", ConvNone, ""},
		{`{"a":1}`, `{"a":1}`, ConvNone, ""},
		{"name", "name", ConvNone, ""},
		{`"a:b"` + ":>5", `"a:b"`, ConvNone, ">5"},
	}
	for _, c := range cases {
		expr, conv, spec, err := splitSpecConv(c.inner)
		if err != nil {
			t.Errorf("splitSpecConv(%q) refused: %v", c.inner, err)
			continue
		}
		if expr != c.expr || conv != c.conv || spec != c.spec {
			t.Errorf("splitSpecConv(%q) = (%q, %v, %q), want (%q, %v, %q)",
				c.inner, expr, conv, spec, c.expr, c.conv, c.spec)
		}
	}
}
