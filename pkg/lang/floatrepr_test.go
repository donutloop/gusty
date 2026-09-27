package lang

import (
	"math"
	"testing"
)

// Gap P — Python's float rendering (ADR 0180).
func TestPyFloatRepr(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want string
	}{
		{2.0, "2.0"},
		{0.0, "0.0"},
		{math.Copysign(0, -1), "-0.0"}, // Python prints negative zero as -0.0
		{3.5, "3.5"},
		{0.1, "0.1"},
		{0.123456789, "0.123456789"},
		{1e15, "1000000000000000.0"},
		{123456.0, "123456.0"},
		{-2.5, "-2.5"},
		{1e20, "1e+20"},
	} {
		if got := pyFloatRepr(tc.in); got != tc.want {
			t.Errorf("pyFloatRepr(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
