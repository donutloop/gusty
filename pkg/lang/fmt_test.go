package lang

import (
	"strings"
	"testing"
)

func TestFmtRoundTrip(t *testing.T) {
	srcs := []string{
		"x = 1\n",
		"def f(a, b):\n    return a + b\n",
		"if x > 1:\n    print(x)\nelse:\n    print(0)\n",
		"for i in range(10):\n    print(i)\n",
		"class A(B):\n    pass\n",
		"match x:\n    case 1:\n        print(1)\n",
		"try:\n    f()\nexcept:\n    pass\nfinally:\n    print(\"done\")\n",
		"with open(f) as h:\n    print(h)\n",
	}
	for _, src := range srcs {
		f, err := FormatSrc(src)
		if err != nil {
			t.Fatalf("FormatSrc(%q): %v", src, err)
		}
		// idempotence: re-parse and re-format must be stable
		f2, err := FormatSrc(f)
		if err != nil {
			t.Fatalf("reformat(%q): %v", f, err)
		}
		if f != f2 {
			t.Errorf("idempotence fail for %q: %q != %q", src, f, f2)
		}
		// round-trip: formatting must re-parse successfully
		if _, err := Parse(f); err != nil {
			t.Errorf("re-parse fail for %q: %v", f, err)
		}
	}
}

func TestFmtCanonical(t *testing.T) {
	src := "x=1\nif x>1:print(x)\n"
	f, err := FormatSrc(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "x = 1\nif x > 1:\n  print(x)"
	if f != want {
		t.Errorf("got %q want %q", f, want)
	}
}

func TestFormatDocstringRoundTrip(t *testing.T) {
	// Docstrings are extracted from the body into FuncDef.Doc, so the
	// formatter must re-emit them or round-trips would drop them.
	src := `def greet():
    "returns a greeting"
    return "hi"
class Animal:
    "an animal class"
    def speak(self):
        return self
`
	out, err := FormatSrc(src)
	if err != nil {
		t.Fatalf("FormatSrc: %v", err)
	}
	// The docstring must appear as the first statement of each body.
	if !strings.Contains(out, `"returns a greeting"`) {
		t.Fatalf("format dropped func docstring:\n%s", out)
	}
	if !strings.Contains(out, `"an animal class"`) {
		t.Fatalf("format dropped class docstring:\n%s", out)
	}
}

// TestFmtNumericLiterals verifies the canonical formatter preserves modern
// numeric-literal spelling (hex/binary/octal and digit separators) verbatim,
// so formatting is a faithful round-trip.
func TestFmtNumericLiterals(t *testing.T) {
	src := `x = 0xFF + 0b101 + 0o17 + 1_000 + 0x_FF
y = -0b1010
z = 2_5
`
	f, err := FormatSrc(src)
	if err != nil {
		t.Fatalf("FormatSrc: %v", err)
	}
	for _, lit := range []string{"0xFF", "0b101", "0o17", "1_000", "0x_FF", "-0b1010", "2_5"} {
		if !strings.Contains(f, lit) {
			t.Errorf("format lost literal %q:\n%s", lit, f)
		}
	}
}
