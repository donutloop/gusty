package lang

import (
	"math"
	"strconv"
	"strings"
)

// pyFloatRepr renders a float the way Python's str()/repr() does, which is what
// `print(x)`, `str(x)`, and an f-string `{x}` must all agree on.
//
// Two rules make the difference from a plain %g:
//
//   - shortest representation that round-trips, so 0.123456789 stays
//     0.123456789 (a 6-digit %g would print 0.123457) and 0.1 stays 0.1 (a raw
//     %.17g would print 0.10000000000000001);
//   - a float that happens to be integral still reads as a float: 2.0 prints
//     "2.0", not "2". Dropping the ".0" was how print(x * 2.0) showed an integer
//     and made a float result indistinguishable from an int one.
//
// Infinities and NaN use Python's spellings, not Go's +Inf/NaN.
func pyFloatRepr(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if math.IsNaN(f) {
		return "nan"
	}
	// Python switches to exponent notation outside 1e-4 <= |f| < 1e16; Go's 'g'
	// switches at its own digit count, which disagreed (1e15 came back "1e+15"
	// where Python says 1000000000000000.0). Pick the notation by magnitude, then
	// ask for the shortest form that round-trips in that notation.
	a := math.Abs(f)
	var s string
	if a == 0 {
		s = "0"
	} else if a >= 1e-4 && a < 1e16 {
		s = strconv.FormatFloat(f, 'f', -1, 64)
	} else {
		s = strconv.FormatFloat(f, 'e', -1, 64)
	}
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	// Zero carries no sign in the digits, so -0.0 needs its sign put back; every
	// other negative already arrived with the minus from FormatFloat.
	if math.Signbit(f) && !strings.HasPrefix(s, "-") {
		return "-" + s
	}
	return s
}
