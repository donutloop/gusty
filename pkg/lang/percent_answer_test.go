package lang

import (
	"fmt"
	"strings"
	"testing"
)

// The two ways a formatting case is judged: the bytes it prints, or the class and sentence it raises.
// Both go through the same compiled run the record leg uses (RunSnippet, the LLVM JIT), so a row here is
// the same execution the golden harness would have judged, asked a different question of.

func percentRun(src string) (*JITResult, error) {
	return RunSnippet(src + "\n")
}

// compileAndRun answers the program's stdout, and refuses to report one for a program that trapped.
func compileAndRun(t *testing.T, src string) (string, error) {
	t.Helper()
	res, err := percentRun(src)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return res.Output, fmt.Errorf("the program trapped with exit %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return res.Output, nil
}

// percentTrap is a program that ended on a raise: the class, the sentence, and whatever it managed to
// print on the way.
type percentTrap struct {
	exit    int
	class   string
	message string
	stdout  string
}

func runPercentTrap(src string) (percentTrap, error) {
	res, err := percentRun(src)
	if err != nil {
		return percentTrap{}, err
	}
	_, class, message := ParseTrapReport(res.Stderr)
	return percentTrap{exit: res.Code, class: class, message: message, stdout: res.Output}, nil
}

// What the formatting door answers, checked against CPython 3.12 rather than against a memory of the
// printf manual. Each row is a program, the exact bytes it must print, or the class and sentence it must
// raise; every `out` and every `msg` below was read off the reference on the day the row was written.
//
// The rows are grouped by the answer they test, because the door has four answers and a test that mixes
// them cannot say which one broke: a rendered value, a padded field, a catchable trap, and the ordinary
// remainder that must keep working underneath all of it.
type percentCase struct {
	src  string
	out  string // the exact stdout, when the program answers
	exn  string // the exception class, when the program traps
	msg  string // the reference's own sentence, without the class prefix
	rest string // stdout that must appear before a trap ends the program
}

func runPercentCase(t *testing.T, c percentCase) {
	t.Helper()
	if c.out != "" {
		got, err := compileAndRun(t, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if got != c.out {
			t.Fatalf("the compiled answer to %s is %q, the reference answers %q", c.src, got, c.out)
		}
		return
	}
	trap, err := runPercentTrap(c.src)
	if err != nil {
		t.Fatalf("%s: %v", c.src, err)
	}
	if trap.exit == 0 {
		t.Fatalf("%s: the reference raises %s: %s; the compiled backend answered %q", c.src, c.exn, c.msg, trap.stdout)
	}
	if trap.class != c.exn {
		t.Fatalf("%s: trapped %s, the reference raises %s (%s)", c.src, trap.class, c.exn, c.msg)
	}
	if trap.message != c.msg {
		t.Fatalf("%s: trapped %s but said %q, the reference says %q", c.src, trap.class, trap.message, c.msg)
	}
	if c.rest != "" && trap.stdout != c.rest {
		t.Fatalf("%s: the program printed %q before trapping, want %q", c.src, trap.stdout, c.rest)
	}
}

func TestPercentTextConversions(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("hi %s" % "you")`, out: "hi you\n"},
		{src: `print("%s" % 42)`, out: "42\n"},
		{src: `print("%s" % 4.5)`, out: "4.5\n"},
		{src: `print("%s" % True)`, out: "True\n"},
		{src: `print("%s" % None)`, out: "None\n"},
		{src: `print("%s" % [1, 2])`, out: "[1, 2]\n"},
		{src: `print("%10s|" % "ab")`, out: "        ab|\n"},
		{src: `print("%-10s|" % "ab")`, out: "ab        |\n"},
		{src: `print("%.3s|" % "abcdef")`, out: "abc|\n"},
		{src: `print("%10.3s|" % "abcdef")`, out: "       abc|\n"},
		// The '0' flag does not apply to a text: the reference space-fills a %s even when asked otherwise.
		{src: `print("%010s|" % "ab")`, out: "        ab|\n"},
		// Width counts CHARACTERS, so a two-byte é takes one field position and two bytes of the answer.
		{src: "print(\"%10s|\" % \"\xc3\xa9\")\n", out: "         \xc3\xa9|\n"},
		{src: `print("%r" % None)`, out: "None\n"},
		{src: `print("%r" % [1, 2])`, out: "[1, 2]\n"},
		{src: `print("%a" % [1])`, out: "[1]\n"},
		{src: `print("%s/%s" % ("a", "b"))`, out: "a/b\n"},
		{src: `print("%s is %d years old and %.1f tall" % ("Ann", 30, 1.75))`, out: "Ann is 30 years old and 1.8 tall\n"},
	} {
		runPercentCase(t, c)
	}
}

func TestPercentIntegerConversions(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("%d" % 42)`, out: "42\n"},
		{src: `print("%d" % -42)`, out: "-42\n"},
		{src: `print("%d" % 0)`, out: "0\n"},
		{src: `print("%d" % True)`, out: "1\n"},
		// A %d takes a float and truncates toward zero; %x and %o refuse one, and say so differently.
		{src: `print("%d" % 1.5)`, out: "1\n"},
		{src: `print("%d" % -1.5)`, out: "-1\n"},
		{src: `print("%d" % -0.5)`, out: "0\n"},
		{src: `print("%i" % 7)`, out: "7\n"},
		{src: `print("%5d|" % 3)`, out: "    3|\n"},
		{src: `print("%-5d|" % 3)`, out: "3    |\n"},
		// The zero flag puts the fill between the sign and the digits, which no C library call does for a
		// negative built from the module's own digit tables.
		{src: `print("%05d|" % -3)`, out: "-0003|\n"},
		{src: `print("%03d" % 7)`, out: "007\n"},
		{src: `print("%+d" % 3)`, out: "+3\n"},
		{src: `print("%+d" % -3)`, out: "-3\n"},
		{src: `print("% d" % 3)`, out: " 3\n"},
		{src: `print("% d" % -3)`, out: "-3\n"},
		// A precision is a MINIMUM digit count, filled with zeros, and it wins over the width's fill.
		{src: `print("%5.3d" % 42)`, out: "  042\n"},
		{src: `print("%5.3d" % -4)`, out: " -004\n"},
		{src: `print("%d-%d" % (1, 2))`, out: "1-2\n"},
		{src: `print("%o" % 8)`, out: "10\n"},
		{src: `print("%o" % -8)`, out: "-10\n"},
		// The reference's alternate octal is 0o, not the C 0; and its alternate hex keeps the sign outside.
		{src: `print("%#o" % 8)`, out: "0o10\n"},
		{src: `print("%#o" % 0)`, out: "0o0\n"},
		{src: `print("%#o" % -8)`, out: "-0o10\n"},
		{src: `print("%#.4o" % 8)`, out: "0o0010\n"},
		{src: `print("%#010o" % 8)`, out: "0o00000010\n"},
		{src: `print("%#x" % 7)`, out: "0x7\n"},
		{src: `print("%#x" % 0)`, out: "0x0\n"},
		{src: `print("%#x" % -18)`, out: "-0x12\n"},
		{src: `print("%#X" % -18)`, out: "-0X12\n"},
		{src: `print("%#12.4x" % 255)`, out: "      0x00ff\n"},
		{src: `print("%0#8X" % 255)`, out: "0X0000FF\n"},
		{src: `print("%x" % True)`, out: "1\n"},
	} {
		runPercentCase(t, c)
	}
}

func TestPercentRealConversions(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("%f" % 3.5)`, out: "3.500000\n"},
		{src: `print("%.2f" % 3.14159)`, out: "3.14\n"},
		{src: `print("%10.2f|" % 3.14159)`, out: "      3.14|\n"},
		// The zeros of a zero-filled float belong between the sign and the digits, which is why the width
		// travels inside the spec instead of being padded by this module.
		{src: `print("%010.2f|" % -3.5)`, out: "-000003.50|\n"},
		{src: `print("%+010.2f" % 3.14)`, out: "+000003.14\n"},
		{src: `print("%-10.2f|" % 3.5)`, out: "3.50      |\n"},
		{src: `print("%e" % 1000.0)`, out: "1.000000e+03\n"},
		{src: `print("%E" % 1000.0)`, out: "1.000000E+03\n"},
		{src: `print("%11.2e" % 3.5)`, out: "   3.50e+00\n"},
		{src: `print("%g" % 100000.0)`, out: "100000\n"},
		{src: `print("%g" % 0.00001)`, out: "1e-05\n"},
		{src: `print("%g" % 0.0)`, out: "0\n"},
		{src: `print("%.0f" % 3.5)`, out: "4\n"},
		{src: `print("%.0f" % 2.5)`, out: "2\n"},
		{src: `print("%f" % 7)`, out: "7.000000\n"},
	} {
		runPercentCase(t, c)
	}
}

func TestPercentCharacterConversions(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("%c" % 65)`, out: "A\n"},
		{src: "print(\"%c\" % 233)\n", out: "\xc3\xa9\n"},
		{src: `print("%10c|" % 65)`, out: "         A|\n"},
		{src: `print("%-10c|" % 65)`, out: "A         |\n"},
		{src: `print("%c" % True)`, out: "\x01\n"},
	} {
		runPercentCase(t, c)
	}
}

func TestPercentStarWidths(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("%*d|" % (5, 3))`, out: "    3|\n"},
		{src: `print("%*d|" % (-5, 3))`, out: "3    |\n"},
		{src: `print("%.*f" % (2, 3.14159))`, out: "3.14\n"},
		{src: `print("%*.*f" % (8, 2, 3.14159))`, out: "    3.14\n"},
		{src: `print("%-*d|" % (5, 3))`, out: "3    |\n"},
	} {
		runPercentCase(t, c)
	}
}

func TestPercentMappingForm(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("%(a)s" % {"a": 7})`, out: "7\n"},
		{src: `print("%(a)d" % {"a": 42})`, out: "42\n"},
		// The mapping is ONE argument however many conversions read keys out of it.
		{src: `print("%(a)s %(b)s" % {"a": 1, "b": 2})`, out: "1 2\n"},
		{src: `print("%(a)s%%" % {"a": 7})`, out: "7%\n"},
		{src: `print("%(a)10s|" % {"a": "z"})`, out: "         z|\n"},
		{src: `print("%(a).2s" % {"a": "xyz"})`, out: "xy\n"},
	} {
		runPercentCase(t, c)
	}
}

func TestPercentLiteralPercent(t *testing.T) {
	for _, c := range []percentCase{
		// A text with no % operator is not a formatting: %% stays two characters, exactly as it does in
		// the reference, and the door is never asked.
		{src: `print("%%")`, out: "%%\n"},
		{src: `print("100%%")`, out: "100%%\n"},
		{src: `print("%s %%" % "a")`, out: "a %\n"},
		{src: `print("%d %%" % 5)`, out: "5 %\n"},
		{src: `print("%s%%s" % "a")`, out: "a%s\n"},
		{src: `print("%%%d" % 1)`, out: "%1\n"},
		{src: `print("%%%s%%" % "x")`, out: "%x%\n"},
	} {
		runPercentCase(t, c)
	}
}

// The traps. Each sentence is CPython's, and each is raised through the module's one non-local raise
// road, so the same program that traps uncaught also catches it (TestPercentTrapsAreCatchable).
func TestPercentTraps(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print("%d" % "x")`, exn: "TypeError", msg: "%d format: a real number is required, not str"},
		{src: `print("%d" % None)`, exn: "TypeError", msg: "%d format: a real number is required, not NoneType"},
		{src: `print("%d" % [1])`, exn: "TypeError", msg: "%d format: a real number is required, not list"},
		{src: `print("%x" % 1.5)`, exn: "TypeError", msg: "%x format: an integer is required, not float"},
		{src: `print("%o" % "x")`, exn: "TypeError", msg: "%o format: an integer is required, not str"},
		{src: `print("%e" % "x")`, exn: "TypeError", msg: "must be real number, not str"},
		{src: `print("%c" % 1.5)`, exn: "TypeError", msg: "%c requires int or char"},
		{src: `print("%c" % "ab")`, exn: "TypeError", msg: "%c requires int or char"},
		{src: `print("%c" % None)`, exn: "TypeError", msg: "%c requires int or char"},
		{src: `print("%c" % -1)`, exn: "OverflowError", msg: "%c arg not in range(0x110000)"},
		{src: `print("%c" % 1114112)`, exn: "OverflowError", msg: "%c arg not in range(0x110000)"},
		{src: `print("%z" % 1)`, exn: "ValueError", msg: "unsupported format character 'z' (0x7a) at index 1"},
		{src: `print("%5*" % 3)`, exn: "ValueError", msg: "unsupported format character '*' (0x2a) at index 2"},
		{src: `print("%d%" % 5)`, exn: "ValueError", msg: "incomplete format"},
		{src: `print("%s %s" % (1,))`, exn: "TypeError", msg: "not enough arguments for format string"},
		{src: `print("%s" % (1, 2))`, exn: "TypeError", msg: "not all arguments converted during string formatting"},
		{src: `print("%s %s" % (1, 2, 3))`, exn: "TypeError", msg: "not all arguments converted during string formatting"},
		{src: `print("%(a)s" % 5)`, exn: "TypeError", msg: "format requires a mapping"},
		{src: `print("%(a)s" % {})`, exn: "KeyError", msg: "'a'"},
		{src: `print("%*d" % ("a", 3))`, exn: "TypeError", msg: "* wants int"},
	} {
		runPercentCase(t, c)
	}
}

// A format that raises must be catchable, because the door raises through the same road a `raise`
// statement does — and the class the handler dispatches on is the code the door chose, not one the
// compiler wrote (ADR 0212, ADR 0214).
func TestPercentTrapsAreCatchable(t *testing.T) {
	for _, c := range []struct{ src, out string }{
		{`try:` + "\n" + `    print("%d" % "x")` + "\n" + `except TypeError:` + "\n" + `    print("caught")`, "caught\n"},
		{`try:` + "\n" + `    print("%c" % -1)` + "\n" + `except OverflowError:` + "\n" + `    print("overflow")`, "overflow\n"},
		{`try:` + "\n" + `    print("%(zz)s" % {"a": 1})` + "\n" + `except KeyError:` + "\n" + `    print("no key")`, "no key\n"},
		{`try:` + "\n" + `    print("%q" % 1)` + "\n" + `except ValueError:` + "\n" + `    print("bad conv")`, "bad conv\n"},
	} {
		got, err := compileAndRun(t, c.src)
		if err != nil {
			t.Fatalf("%s: %v", c.src, err)
		}
		if got != c.out {
			t.Fatalf("%s printed %q, want %q", c.src, got, c.out)
		}
	}
}

// The remainder has to survive the formatting: `%` on a number is still the remainder, and `%` on a text
// is now the formatting. The two are decided by one question (is the left operand a text), and a door
// that got the question wrong would answer one of these rows with the other's answer.
func TestPercentRemainderStillRemains(t *testing.T) {
	for _, c := range []percentCase{
		{src: `print(7 % 2)`, out: "1\n"},
		{src: `print(-7 % 2)`, out: "1\n"},
		{src: `print(7.5 % 2)`, out: "1.5\n"},
		{src: "x = 20\ny = 3\nprint(x % y)", out: "2\n"},
		{src: `def f(a, b):
    return a % b
print(f(9, 4))`, out: "1\n"},
	} {
		runPercentCase(t, c)
	}
}

// A format the compiler cannot read at compile time keeps its run-time answer: the door is asked with the
// format text as a value, so a format that arrives from a variable is formatted by the same code, and a
// format that never runs never raises.
func TestPercentFormatFromAVariable(t *testing.T) {
	got, err := compileAndRun(t, `def fmt(n):
    return "%d items" % n
print(fmt(3))
print(fmt(12))`)
	if err != nil || got != "3 items\n12 items\n" {
		t.Fatalf("got %q (%v)", got, err)
	}
	// The branch that never runs cannot raise: the format is only walked when the program reaches it.
	got, err = compileAndRun(t, `def pick(n):
    if n > 0:
        return "%d ok" % n
    return "%z bad" % n
print(pick(1))`)
	if err != nil || got != "1 ok\n" {
		t.Fatalf("got %q (%v)", got, err)
	}
}

// A formatted text is an @str_tab index, so every road that reads one takes the answer without knowing
// what built it: a binding, a container slot, a concatenation, a comparison and a print all read it.
func TestPercentAnswerIsATextValue(t *testing.T) {
	got, err := compileAndRun(t, `s = "%s!" % "hi"
print(s)
print(len(s))
print(s + "?")
print(s == "hi!")
print(s.upper())
xs = ["%d" % 7]
print(xs)
print(xs[0] == "7")
d = {"k": "%s" % 1}
print(d["k"])`)
	if err != nil {
		t.Fatalf("%v", err)
	}
	want := "hi!\n3\nhi!?\nTrue\nHI!\n['7']\nTrue\n1\n"
	// The list prints its element QUOTED because the element is a text: `"%d" % 7` renders the digits
	// and the answer is the string "7", which `[...]` reprs as '7' — the same answer the reference gives,
	// and the reason the slot had to carry a text's tag rather than the number underneath it.
	if got != want {
		t.Fatalf("a formatted text is not an ordinary text:\n got %q\nwant %q", got, want)
	}
}

// The door is asked once per formatting expression and its arguments are spent in order; a program that
// formats twice in one expression, or nests a format inside a format's argument, exercises the walk twice
// over and must not have the second conversion read the first one's state.
func TestPercentNestedAndRepeated(t *testing.T) {
	got, err := compileAndRun(t, `print(("%s %s" % ("%d" % 1, "%s" % "two")) + "|")
print("%s-%s" % ("%s" % "a", "%s" % "b"))
n = 0
for i in [1, 2, 3]:
    n = n + len("%d" % i)
print(n)`)
	if err != nil {
		t.Fatalf("%v", err)
	}
	if want := "1 two|\na-b\n3\n"; got != want {
		t.Fatalf("nested formatting:\n got %q\nwant %q", got, want)
	}
}
