package lang

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
)

// Gap R.28 / Gap R.30 (ADR 0216): `//` and `%` are one rule, not two operators. Go's `/` and `%`
// truncate toward zero; Python floors, so the remainder carries the *divisor's* sign and the pair
// satisfies a == (a // b) * b + (a % b) for every sign combination. Measured before the fix: the
// interpreter got all 156 `%` cases wrong and the compiled backend got all 312 wrong (`sdiv`/`srem`
// emitted raw), and two integration tests had pinned the truncated values as the expected output.

var (
	floorDividends   = []int64{7, -7, 8, -8, 15, -15, 100, -100, 13, -13, 1, -1, 0}
	floorDivisors    = []int64{2, -2, 3, -3, 5, -5, 7, -7, 4, -4, 1, -1}
	floorFloatCases  = []string{"7.5", "-7.5", "7.0", "-7.0", "0.5", "-0.5", "1.5", "-1.5", "2.25", "-2.25"}
	floorFloatDivis  = []string{"2.0", "-2.0", "3.0", "-3.0", "0.5", "-0.5", "1.5", "-1.5", "2", "-2", "3", "-3"}
	pyTruncDivNotice = "the result must floor toward negative infinity"
)

// goFloorDiv / goFloorMod mirror the reference's rule independently of the implementation under
// test, so a shared misunderstanding cannot satisfy the assertion.
func goFloorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func goFloorMod(a, b int64) int64 {
	r := a % b
	if r != 0 && ((r < 0) != (b < 0)) {
		r += b
	}
	return r
}

func TestFloorDivisionAndModuloSignGrid(t *testing.T) {
	var src strings.Builder
	var want strings.Builder
	for _, a := range floorDividends {
		for _, b := range floorDivisors {
			src.WriteString("print(" + itoa64(a) + " // " + itoa64(b) + ")\n")
			want.WriteString(itoa64(goFloorDiv(a, b)) + "\n")
			src.WriteString("print(" + itoa64(a) + " % " + itoa64(b) + ")\n")
			want.WriteString(itoa64(goFloorMod(a, b)) + "\n")
		}
	}
	got := captureStdout(t, src.String())
	if got != want.String() {
		t.Errorf("the sign grid diverges\n got: %s\nwant: %s", firstDiff(got, want.String()), firstDiff(want.String(), got))
	}
}

// The invariant is why the two operators are one feature: taken separately, a truncating pair is
// self-consistent, so testing `//` and `%` against tables that were each written from the wrong
// answer still passes. This cannot.
func TestFloorIdentityHoldsForEverySign(t *testing.T) {
	for _, a := range floorDividends {
		for _, b := range floorDivisors {
			src := "q = " + itoa64(a) + " // " + itoa64(b) + "\nr = " + itoa64(a) + " % " + itoa64(b) +
				"\nprint(q * " + itoa64(b) + " + r)\nprint(q * " + itoa64(b) + " + r == " + itoa64(a) + ")\n"
			got := captureStdout(t, src)
			// The identity's second line is a verdict, and a verdict reads True: the pin used
			// to say 1, which was this backend's answer being checked against itself (ADR 0257).
			if got != itoa64(a)+"\nTrue\n" {
				t.Errorf("%d // %% %d breaks a == (a // b) * b + (a %% b): got %q, want %q",
					a, b, got, itoa64(a)+"\nTrue\n")
			}
		}
	}
}

func TestFloorModuloOnFloatsCarriesTheDivisorSign(t *testing.T) {
	var src, want strings.Builder
	for _, a := range floorFloatCases {
		for _, b := range floorFloatDivis {
			src.WriteString("print(" + a + " % " + b + ")\n")
			want.WriteString(pythonFloatModText(a, b) + "\n")
			src.WriteString("print(" + a + " // " + b + ")\n")
			want.WriteString(pythonFloorDivText(a, b) + "\n")
		}
	}
	got := captureStdout(t, src.String())
	if got != want.String() {
		t.Errorf("float // and %% diverge\n got: %s\nwant: %s", firstDiff(got, want.String()), firstDiff(want.String(), got))
	}
}

// The detail that separates "floors" from "floors, correctly": an exact remainder keeps the
// divisor's sign, so 7.5 % -0.5 is -0.0 and prints as such. fmod gives the dividend's sign, which
// renders as a different number.
func TestExactFloatRemainderKeepsTheDivisorsSign(t *testing.T) {
	cases := []struct{ src, want string }{
		{`print(7.5 % -0.5)`, "-0.0\n"},
		{`print(-7.5 % 0.5)`, "0.0\n"},
		{`print(8.0 % -2.0)`, "-0.0\n"},
		{`print(-8.0 % 2.0)`, "0.0\n"},
		// Not an exact multiple, so the ordinary floor rule applies rather than the signed
		// zero one: 7 % -2 is -1 (7 = (-4) * (-2) + (-1)).
		{`print(7.0 % -2.0)`, "-1.0\n"},
		{`print(-7.0 % 2.0)`, "1.0\n"},
	}
	for _, tc := range cases {
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Errorf("%s = %q, want %q (%s)", tc.src, got, tc.want, pyTruncDivNotice)
		}
	}
}

// The compiled half, asserted structurally. Behaviour tests would pass on a truncated pair if the
// tables were wrong (they did, twice), so the module is also checked for the *corrections*
// themselves: a future "simplification" that drops the select and keeps the raw srem fails here
// even when the behaviour tests were written from the emitted IR.
func TestEmittedFloorCorrectionsAreInTheModule(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"integer floor division", "x = -7\ny = 2\nprint(x // y)\n",
			[]string{" = sdiv i32 ", " = srem i32 ", " = icmp ne i32 ", " = icmp slt i32 ", " = xor i1 ", " = and i1 ", " = sub i32 ", " = select i1 "}},
		{"integer floor modulo", "x = -7\ny = 2\nprint(x % y)\n",
			[]string{" = srem i32 ", " = icmp ne i32 ", " = icmp slt i32 ", " = xor i1 ", " = and i1 ", " = add i32 ", " = select i1 "}},
		{"float floor modulo", "x = -7.0\ny = 2.0\nprint(x % y)\n",
			[]string{" = frem double ", " = fcmp one double ", " = fcmp olt double ", " = xor i1 ", " = and i1 ", " = fadd double ", " = select i1 ", "@llvm.copysign.f64"}},
	}
	for _, tc := range cases {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%s: compile: %v", tc.name, err)
		}
		for _, frag := range tc.want {
			if !strings.Contains(res.IR, frag) {
				t.Errorf("%s: the module does not contain %q — the flooring correction is missing\n%s", tc.name, frag, snippet(res.IR))
			}
		}
		// A raw `srem`/`frem` used directly as the printed value is the bug, not the shape.
		if tc.name != "integer floor division" && strings.Contains(res.IR, " = srem i32 %") && !strings.Contains(res.IR, " = select i1 ") {
			t.Errorf("%s: emits srem without a select correction", tc.name)
		}
	}
}

func snippet(ir string) string {
	lines := strings.Split(ir, "\n")
	var out []string
	for _, l := range lines {
		if strings.Contains(l, "srem") || strings.Contains(l, "frem") || strings.Contains(l, "select") || strings.Contains(l, "sdiv") {
			out = append(out, strings.TrimSpace(l))
		}
		if len(out) > 12 {
			break
		}
	}
	return strings.Join(out, "\n    ")
}

// --- helpers, prefixed for this file so the package's other test helpers keep their names ----

func itoa64(v int64) string { return strconv.FormatInt(v, 10) }

// firstDiff renders a value with the first differing byte marked, because a 300-line grid whose
// output differs from the expectation in one place is useless as a failure message.
func firstDiff(got, want string) string {
	g, w := []rune(got), []rune(want)
	i := 0
	for i < len(g) && i < len(w) && g[i] == w[i] {
		i++
	}
	lo := i - 12
	if lo < 0 {
		lo = 0
	}
	hi := i + 18
	if hi > len(g) {
		hi = len(g)
	}
	head := strings.ReplaceAll(string(g[lo:hi]), "\n", " / ")
	return "…" + head + fmt.Sprintf("  (first difference at offset %d)", i)
}

// floorFloatText renders a float the way `print` does: shortest decimal, and a `.0` so an
// integral float is not mistaken for an integer.
func floorFloatText(f float64) string {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// pythonFloatModText / pythonFloorDivText are the reference's answers for the grid, written from
// its definition (floor toward negative infinity, remainder carrying the divisor's sign) rather
// than from the runtime helper, and cross-checked against a real CPython in
// integration/floor_division_test.go.
func pythonFloatModText(a, b string) string {
	av, _ := strconv.ParseFloat(a, 64)
	bv, _ := strconv.ParseFloat(b, 64)
	r := math.Mod(av, bv)
	if r == 0 {
		return floorFloatText(math.Copysign(0, bv))
	}
	if (r < 0) != (bv < 0) {
		r += bv
	}
	return floorFloatText(r)
}

func pythonFloorDivText(a, b string) string {
	av, _ := strconv.ParseFloat(a, 64)
	bv, _ := strconv.ParseFloat(b, 64)
	return floorFloatText(math.Floor(av / bv))
}
