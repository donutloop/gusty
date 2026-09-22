package lang

import "testing"

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
