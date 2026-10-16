package lang

// The formatting door's answer is one thing; the roads that carry it are many. Every row below takes the
// same rendered text one step further from `print` — bound to a name, measured, upper-cased, concatenated,
// compared, handed to a function, returned from one, stored in a container slot, kept in an instance slot —
// and asks the compiled leg for the bytes CPython prints.
//
// These are the shapes where a formatting used to go wrong, and each row is the regression for one road
// that had its own idea of what a formatting is:
//
//   - a bind of a format whose argument is a double went through the numeric road, which asked the double
//     lift for the format itself; `u = "%s" % 3.5` could not be bound at all (`isFloat` reading `%` as the
//     remainder operator — the guard in `isFloat`);
//   - a function that formats one of its parameters read the argument as one word, so `f(3.5)` rendered the
//     int beside the float: `3.00` for `3.50`, and `3` for `3.5`, both at exit 0 (the `pairSpecFor` half —
//     a string-returning body kept the string convention for its ANSWER and had to keep the tag word for its
//     PARAMETERS);
//   - a name the plan accepted while the emitter had no pair for it would print the @str_tab position, so
//     `percentArgKindKnown` and `percentArgPair` now ask the same three predicates (ADR 0229's one-predicate
//     rule, ADR 0359's version of it).
//
// Every expectation here is the reference's own output, read off CPython 3.12, not a memory of it
// (roadmap Gap R.31, L11.1, ADR 0360).

import (
	"strings"
	"testing"
)

func TestPercentAnswerSurvivesEveryRoad(t *testing.T) {
	t.Parallel()
	for _, c := range []percentCase{
		// The binding road: a format bound to a name is the text, not the index under it.
		{src: `u = "%.2f" % 3.5`, out: ""},
		{src: `u = "%.2f" % 3.5
print(u)`, out: "3.50\n"},
		{src: `u = "%.2f" % 3.5
print(u, len(u), u.upper(), u + "!", u == "3.50")`, out: "3.50 4 3.50 3.50! True\n"},
		// The parameter road: the argument's kind is the pair's, and the pair is what the door reads.
		{src: `def f(v):
    print("%.2f" % v)

f(3.5)`, out: "3.50\n"},
		{src: `def f(v):
    return "%s" % v

print(f(3.5))`, out: "3.5\n"},
		{src: `def f(v):
    return "%.2f" % v

x = 3.5
print(f(x))`, out: "3.50\n"},
		{src: `def f(v):
    return "%d items" % v

print(f(3))`, out: "3 items\n"},
		{src: `def g(name, n):
    return "%s has %d" % (name, n)

print(g("ada", 2))`, out: "ada has 2\n"},
		// The call-argument road: the format is built by the caller and received as a text.
		{src: `def w(t):
    print("<" + t + ">")

w("%d up" % 7)`, out: "<7 up>\n"},
		// The instance-slot road.
		{src: `class C:
    def tag(self):
        return "%s!" % "hi"

print(C().tag())`, out: "hi!\n"},
		// The container roads: an element and a value keep their text beside their tag.
		{src: `xs = ["%.1f" % 2.5, "%d" % 7]
print(xs)
print(xs[0] + xs[1])`, out: "['2.5', '7']\n2.57\n"},
		{src: `d = {"k": "%.2f" % 1.5}
print(d["k"])
print(len(d["k"]))`, out: "1.50\n4\n"},
		// A code point that arrives at run time is still a character.
		{src: `n = 65
print("%c" % n)`, out: "A\n"},
		// A format inside a format: the inner door finishes before the outer one stores its arguments.
		{src: `print("%s" % ("%d" % 7))`, out: "7\n"},
		{src: `print("%s %s" % ("%d" % 1, "%s" % "two"))`, out: "1 two\n"},
	} {
		if c.out == "" {
			continue // the bare bind prints nothing; it is the next row that judges it
		}
		t.Run(c.src, func(t *testing.T) {
			t.Parallel()
			runPercentCase(t, c)
		})
	}
}

// TestTheTwoRoadsOfOneFormattingAnswerTheSameThing is the reason the plan is asked once. The direct print
// and the print of a name bound from the same formatting are two different roads through codegen — one
// lowers the BinOp where it stands, the other reads a slot back — and they are allowed to disagree only if
// they disagree with the reference too, which is what the pair of rows below checks.
func TestTheTwoRoadsOfOneFormattingAnswerTheSameThing(t *testing.T) {
	t.Parallel()
	for _, format := range []struct{ fmt, arg string }{
		{"%d items", "3"},
		{"%.2f", "3.5"},
		{"%s!", `"hi"`},
		{"%05d", "42"},
		{"%#x", "255"},
		{"%r", `"text"`},
		{"%c", "8364"},
	} {
		format := format
		t.Run(format.fmt, func(t *testing.T) {
			t.Parallel()
			direct := "print(\"" + format.fmt + "\" % " + format.arg + ")"
			bound := "w = \"" + format.fmt + "\" % " + format.arg + "\nprint(w)"
			directOut, err := compileAndRun(t, direct)
			if err != nil {
				t.Fatalf("the direct print: %v", err)
			}
			boundOut, err := compileAndRun(t, bound)
			if err != nil {
				t.Fatalf("the bound print: %v", err)
			}
			if directOut != boundOut {
				t.Fatalf("the same formatting answered two ways: %s prints %q, %s prints %q",
					direct, directOut, bound, boundOut)
			}
			if strings.TrimSpace(boundOut) == "" {
				t.Fatalf("both roads printed nothing for %q", format.fmt)
			}
		})
	}
}

// TestASurrogateCodePointIsRefusedRatherThanEncoded names the one `%c` answer this backend cannot give: a
// surrogate has no UTF-8 encoding, the reference raises UnicodeEncodeError, and the class table has no such
// class yet — so the shape the compiler can see is refused in words instead of printing the bytes the
// encoder refused (roadmap Gap R.31, ADR 0360).
func TestASurrogateCodePointIsRefusedRatherThanEncoded(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		wants     []string
	}{
		{"a literal surrogate", "print(\"%c\" % 0xD800)\n", []string{"surrogate", "UnicodeEncodeError", "Gap R.31", "ADR 0360"}},
		{"the high end of the range", "print(\"%c\" % 0xDFFF)\n", []string{"surrogate", "UnicodeEncodeError"}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("the program should be refused, not compiled: %s", tc.src)
			}
			msg := err.Error()
			for _, want := range tc.wants {
				if !strings.Contains(msg, want) {
					t.Errorf("the refusal must name %q, got: %s", want, msg)
				}
			}
		})
	}
	// The neighbour that must keep working: a code point the program computed is a character.
	for _, src := range []string{"print(\"%c\" % 65)\n", "print(\"%c\" % 8364)\n"} {
		if _, err := Compile(src); err != nil {
			t.Errorf("%s should compile: %v", src, err)
		}
	}
}
