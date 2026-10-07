package lang

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Tests for Gap R.29 (roadmap), ADR 0221: `==` between an int and a float is one question about
// two numbers. the record answered `1 == 1.0` with False while `1.0 == 1` answered True, and
// the compiled path was right — so the compiled backend disagreed, and the direction that worked was the
// one nobody thought to test.

func TestIntFloatEqualityIsSymmetric(t *testing.T) {
	cases := []struct {
		left, op, right string
		want            string
	}{
		// The four that were wrong, in both operand orders.
		{"1", "==", "1.0", "1\n"}, {"1.0", "==", "1", "1\n"},
		{"1", "!=", "1.0", ""}, {"1.0", "!=", "1", ""},
		{"0", "==", "0.0", "1\n"}, {"0.0", "==", "0", "1\n"},
		{"0", "!=", "0.0", ""}, {"-3", "==", "-3.0", "1\n"},
		{"7", "==", "7.0", "1\n"}, {"2", "==", "2.5", ""},
		{"2.5", "==", "2", ""}, {"1", "==", "2", ""},
		{"-0.0", "==", "0", "1\n"}, {"0", "==", "-0.0", "1\n"},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%s %s %s", tc.left, tc.op, tc.right)
		t.Run(name, func(t *testing.T) {
			want := strings.TrimSpace(tc.want) == "1"
			got := captureStdout(t, fmt.Sprintf("print((%s %s %s))\n", tc.left, tc.op, tc.right))
			// A verdict prints as a verdict. The two strings here used to be 1 and 0, which is
			// what this backend printed and not what the oracle did (roadmap L11.1 step 2,
			// ADR 0257).
			printed := "True\n"
			if !want {
				printed = "False\n"
			}
			if got != printed {
				t.Fatalf("(%s %s %s) printed %q, want %q", tc.left, tc.op, tc.right, got, printed)
			}
		})
	}
}

// TestCrossTypeComparisonGridBothWays is the sign-and-direction grid: every int/float pair, in both
// operand orders, through the record. 300 comparisons, checked against values computed here
// from the Python rules rather than from anything the implementation printed.
func TestCrossTypeComparisonGrid(t *testing.T) {
	ints := []string{"0", "1", "2", "-3", "7"}
	floats := []string{"0.0", "1.0", "2.5", "-3.0", "-0.0"}
	ops := []string{"==", "!=", "<", "<=", ">", ">="}
	var b strings.Builder
	type want struct {
		src    string
		expect string
	}
	var cases []want
	for _, i := range ints {
		for _, f := range floats {
			for _, op := range ops {
				for _, pair := range [][2]string{{i, f}, {f, i}} {
					src := fmt.Sprintf("print((%s %s %s))\n", pair[0], op, pair[1])
					cmp, err := pyCompareFloats(pair[0], op, pair[1])
					if err != nil {
						t.Fatalf("expectation for %s: %v", src, err)
					}
					cases = append(cases, want{src, map[bool]string{true: "True\n", false: "False\n"}[cmp]})
					b.WriteString(src)
				}
			}
		}
	}
	if len(cases) != 300 {
		t.Fatalf("the grid stopped being 300 cases (it is %d), so this test can no longer fail as designed", len(cases))
	}
	for _, c := range cases {
		if got := captureStdout(t, c.src); got != c.expect {
			t.Fatalf("%q printed %q, want %q", strings.TrimSpace(c.src), got, c.expect)
		}
	}
}

// TestNumericEqualityThroughVariables covers the same rule when the values arrive by binding: a
// literal comparison can be answered by constant folding, and folding the same rule wrongly is a
// third answer, so the variable path is asserted separately.
func TestNumericEqualityThroughVariables(t *testing.T) {
	src := "a = 1\nb = 1.0\nprint((a == b))\nprint((b == a))\nprint((a != b))\nc = 2.5\nd = 2\nprint((c == d))\nprint((c < d))\nprint((d < c))\n"
	got := captureStdout(t, src)
	want := "True\nTrue\nFalse\nFalse\nFalse\nTrue\n"
	if got != want {
		t.Fatalf("variable comparisons printed %q, want %q", got, want)
	}
}

// TestEqualityWithNonNumbersIsStillFalse keeps the new numeric coercion inside its lane: `==`
// between a number and a container or a string is False, not an error and not an accident of
// handle comparison.
func TestEqualityWithNonNumbersIsStillFalse(t *testing.T) {
	for _, src := range []string{
		"print((1 == [1]))\n",
		"print((1.0 == [1.0]))\n",
		"print(([1] == 1))\n",
		"print((\"a\" == 1.0))\n",
		"print((1.0 == \"a\"))\n",
	} {
		if got := captureStdout(t, src); got != "False\n" {
			t.Fatalf("%q printed %q, want False — a number is not equal to a container or a string", strings.TrimSpace(src), got)
		}
	}
}

// TestContainerEqualityComparesElementsAcrossTypes is the consequence the roadmap's entry predicted:
// element equality inherits the numeric rule, so a list of ints equals the list of the same values
// as floats.
func TestContainerEqualityComparesElementsAcrossTypes(t *testing.T) {
	cases := []struct{ src, want string }{
		{"print(([1] == [1.0]))\n", "True\n"},
		{"print(([1, 2] == [1, 2.0]))\n", "True\n"},
		{"print(([1, 2] == [1, 3]))\n", "False\n"},
		{"print(([] == []))\n", "True\n"},
		{"print(({\"a\": 1} == {\"a\": 1.0}))\n", "True\n"},
		{"print(({\"a\": 1} == {\"a\": 2}))\n", "False\n"},
	}
	for _, tc := range cases {
		if got := captureStdout(t, tc.src); got != tc.want {
			t.Fatalf("%q printed %q, want %q", strings.TrimSpace(tc.src), got, tc.want)
		}
	}
}

// TestIdentityIsNotEquality guards the neighbouring rule: `is` asks about identity and must not
// pick up the numeric coercion, or `x is y` would start answering a value question.
func TestIdentityIsNotEquality(t *testing.T) {
	if got := captureStdout(t, "x = 1\ny = 1.0\nprint((x is y))\n"); got != "False\n" {
		t.Fatalf("`1 is 1.0` printed %q; identity must stay identity", got)
	}
	if got := captureStdout(t, "x = 1\ny = x\nprint((x is y))\n"); got != "True\n" {
		t.Fatalf("`x is x` printed %q, want True", got)
	}
}

// pyCompareFloats is the expectation side of the grid: it parses two numeric literals and applies
// the Python rule for the operator, so the expected value is written down here rather than read off
// an emission. It returns an error for a literal it cannot parse, which is the test failing loudly
// instead of a case quietly disappearing.
func pyCompareFloats(l, op, r string) (bool, error) {
	lf, err := strconv.ParseFloat(l, 64)
	if err != nil {
		return false, fmt.Errorf("left literal %q: %w", l, err)
	}
	rf, err := strconv.ParseFloat(r, 64)
	if err != nil {
		return false, fmt.Errorf("right literal %q: %w", r, err)
	}
	switch op {
	case "==":
		return lf == rf, nil
	case "!=":
		return lf != rf, nil
	case "<":
		return lf < rf, nil
	case "<=":
		return lf <= rf, nil
	case ">":
		return lf > rf, nil
	case ">=":
		return lf >= rf, nil
	}
	return false, fmt.Errorf("unknown operator %q", op)
}
