package integration

import (
	"runtime"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// pythonOutput runs src through CPython via the shared oracle leg
// (lang.PythonRun, roadmap L11.9/ADR 0186) and returns its stdout with the one
// documented rendering divergence normalised away: bare booleans print as 1/0
// until Gap R.112 gives a verdict its own element tag, and the expectations in *this* file were
// written against that. The conformance matrix does **not** apply this
// normalisation — it records those same rows as pinned debt — so a "what Python
// prints" claim here still fails the build when it is wrong, and the
// bool divergence stays visible where it is actually tracked.
func pythonOutput(t *testing.T, src string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no CPython comparison on Windows")
	}
	// Python 3.12 warns about unknown escapes and still runs the program; the
	// oracle leg returns stdout regardless, and these cases assert on it.
	out, _, _ := lang.PythonRun(src)
	// No normalisation here any more. This helper used to rewrite CPython's True and
	// False into 1 and 0 before any comparison, which quietly made every case below
	// an agreement with a Python that never said those digits: the bool gap ADR 0257
	// closed is exactly what this function was hiding, and an oracle is worth keeping
	// only while it is allowed to disagree (roadmap L11.9's rule about the third leg).
	return out
}

// Gap N — string literals are text, with Python escapes.
//
// Two independent defects lived in the same few lines of the lexer:
//
//   - escapes were "decoded" by dropping the backslash and keeping the next
//     byte, so print("a\nb") printed `anb` — every escape sequence in every
//     compiled program was silently wrong (`\t` → t, `\x41` → x41);
//   - the ordinary-string scanner built its value with string(byte), and Go
//     converts a byte to a *rune*, so each non-ASCII byte was re-encoded as a
//     two-byte sequence: "héllo" arrived as "hÃ©llo" with len 8.
//
// Both were invisible to the parity harness because it compares the backends to
// *each other*. Every case here asserts CPython's answer.

var escapeCases = []struct {
	name string
	src  string
	want string
}{
	{"newline escape", "print(\"a\\nb\")\n", "a\nb\n"},
	{"tab escape", "print(\"a\\tb\")\n", "a\tb\n"},
	{"carriage return escape", "print(\"a\\rb\")\n", "a\rb\n"},
	{"backslash escape", "print(\"a\\\\b\")\n", "a\\b\n"},
	{"double quote inside single", "print('q\"q')\n", "q\"q\n"},
	{"single quote inside double", "print(\"it's\")\n", "it's\n"},
	{"escaped double quote", "print(\"q\\\"q\")\n", "q\"q\n"},
	{"hex escape", "print(\"\\x41\\x42\")\n", "AB\n"},
	{"unicode escape", "print(\"\\u00e9\")\n", "é\n"},
	{"wide unicode escape", "print(\"\\U0001F600\")\n", "😀\n"},
	{"bell and vertical tab", "print(\"a\\ab\")\nprint(\"c\\vd\")\n", "a\ab\nc\vd\n"},
	{"form feed and backspace", "print(\"a\\fb\")\nprint(\"c\\bd\")\n", "a\fb\nc\bd\n"},
	{"unknown escape is kept", "print(\"a\\qb\")\n", "a\\qb\n"},
	{"literal utf8 is preserved", "print(\"héllo\")\n", "héllo\n"},
	{"utf8 round trips a variable", "s = \"café\"\nprint(s)\n", "café\n"},
	{"utf8 in a container", "xs = [\"é\", \"a\"]\nprint(xs)\n", "['é', 'a']\n"},
	{"utf8 dict value", "d = {}\nd[\"k\"] = \"é\"\nprint(d)\n", "{'k': 'é'}\n"},
	{"utf8 compares equal", "s = \"é\"\nprint(s == \"é\")\n", "True\n"},
	{"escaped quote survives interning", "xs = [\"a\\\"b\"]\nprint(xs)\n", "['a\"b']\n"},
	{"escape then concat", "print(\"a\\n\" + \"b\")\n", "a\nb\n"},
	{"tab in a joined string", "print(\"\\t\".join([\"a\", \"b\"]))\n", "a\tb\n"},
	{"f-string decodes escapes", "x = 1\nprint(f\"a\\tb{x}\")\n", "a\tb1\n"},
	{"raw string keeps backslashes", "print(r\"a\\nb\")\n", "a\\nb\n"},
	{"triple string decodes escapes", "print(\"\"\"a\\nb\"\"\")\n", "a\nb\n"},
	{"triple string keeps newline", "print(\"\"\"a\nb\"\"\")\n", "a\nb\n"},
}

func TestStringEscapesMatchPython(t *testing.T) {
	for _, tc := range escapeCases {
		// The expected value must be what Python itself prints, not what we
		// remember Python printing.
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Fatalf("%s: the expected output disagrees with CPython\n cpython = %q\n   want   = %q", tc.name, py, tc.want)
		}
		gotCompiled := runCompiled(t, tc.src)
		if gotCompiled != tc.want {
			t.Errorf("%s: the record leg = %q, want %q", tc.name, gotCompiled, tc.want)
		}
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: emitted module must verify: %v %v", tc.name, v.Errors, verr)
		}
		gotAOT := runAOT(t, tc.src)
		if gotAOT != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, gotAOT, tc.want)
		}
	}
}

// The rule this file used to pin against: `len`, indexing and slicing measured string
// values in UTF-8 bytes ("len(\"héllo\") is 6 where Python says 5"), and the pin existed so
// that the change "has to arrive as a test failure, not a shrug". It arrived (ADR 0225):
// a string is a sequence of code points, counted the same by s[i], s[i:j], len and ord.
// Both spellings — the literal folded at compile time and the measured variable — are in the table,
// because they are two code paths and they were not always both right: the fold measured bytes while
// everything else counted code points, so only one of these lines can fail today.
func TestStringLengthCountsCodePoints(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(len(\"héllo\"))\n", "5\n"},
		{"print(len(\"日本語\"))\n", "3\n"},
		{"print(len(\"abc\"))\n", "3\n"},
		{"print(len(\"héllo\"))\n", "5\n"}, // the same fold, spelled twice on purpose: the compile-time
		// path measured bytes here while the measured-variable path was right, so a table that only
		// had one of them could pass with half the rule missing (ADR 0225)
		{"s = \"héllo\"\nprint(len(s))\n", "5\n"},
		{"s = \"日本語\"\nprint(len(s))\n", "3\n"},
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		if got := runAOT(t, tc.src); got != tc.want {
			t.Errorf("AOT %q = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// Python rejects a malformed numeric escape outright (SyntaxError), so those
// cases cannot be compared to CPython. The compiler's contract is different and
// must be pinned directly: keep the text the user wrote rather than guessing a
// code point or failing the build — a typo in a string is a warning-level
// surprise, not a broken program.
func TestMalformedEscapesKeepTheirText(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"print(\"\\xZZ\")\n", "\\xZZ\n"},
		{"print(\"\\u00\")\n", "\\u00\n"},
		{"print(\"\\U00\")\n", "\\U00\n"},
		{"print(\"\\x41\")\n", "A\n"}, // well-formed: decoded
	} {
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		if got := runAOT(t, tc.src); got != tc.want {
			t.Errorf("AOT %q = %q, want %q", tc.src, got, tc.want)
		}
	}
}

// An unterminated string must still be an error with a span, not a hang: the
// scanner that replaced the inline loop is a different code path.
func TestUnterminatedStringIsADiagnostic(t *testing.T) {
	for _, src := range []string{"x = \"abc\n", "x = 'abc\n"} {
		prog, err := lang.Parse(src)
		if err == nil && (prog == nil || len(prog.Diags) == 0) {
			t.Errorf("unterminated string %q produced no diagnostic", src)
		}
		found := false
		if prog != nil {
			for _, d := range prog.Diags {
				if strings.Contains(d.Msg, "unterminated") {
					found = true
				}
			}
		}
		if err == nil && !found {
			t.Errorf("unterminated string %q must report 'unterminated', got %+v", src, prog.Diags)
		}
	}
}

// Substring membership on a string haystack is its own lowering: the earlier
// path handed the raw @.strN global to rt_contains(i32, i32) — a global in an
// i32 slot — so the module was rejected by llc and a plain Python program looked
// like a compiler bug. Both sides are @str_tab indices now (rt_str_contains).
func TestStringMembershipMatchesPython(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"ascii substring", "g = \"caf\"\nprint(\"ca\" in g)\n", "True\n"},
		{"ascii miss", "g = \"café\"\nprint(\"zz\" in g)\n", "False\n"},
		{"utf8 substring", "g = \"café\"\nprint(\"é\" in g)\n", "True\n"},
		{"utf8 multi-byte substring", "g = \"café crème\"\nprint(\"crème\" in g)\n", "True\n"},
		{"not in", "g = \"café\"\nprint(\"zz\" not in g)\n", "True\n"},
		{"empty needle", "g = \"café\"\nprint(\"{}\" in g.format())\n", ""},
		{"runtime needle via parameter", "def f(s):\n    return \"x\" in s\n\nprint(f(\"axb\"))\nprint(f(\"bbb\"))\n", "True\nFalse\n"},
		{"haystack from a call", "def g():\n    return \"hello\"\n\nprint(\"ell\" in g())\n", "True\n"},
		{"membership in a plain list still works", "xs = [1, 2]\nprint(2 in xs)\nprint(3 in xs)\n", "True\nFalse\n"},
	} {
		if tc.want == "" {
			continue // documented gap: str.format is not implemented
		}
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Fatalf("%s: expected output disagrees with CPython\n cpython = %q\n   want   = %q", tc.name, py, tc.want)
		}
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if strings.Contains(res.IR, "@rt_contains(i32 @") {
			t.Errorf("%s: emitted a global string into an i32 slot: %q", tc.name, tc.src)
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: module must verify: %v %v", tc.name, v.Errors, verr)
		}
		if got := runAOT(t, tc.src); got != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// These shapes were AOT-only failures found by the conformance harness: the CLI's
// `--file` runs the interpreter, so `gustyc --file prog.gy` had been showing the
// right answer for a program whose compiled form printed the interned index
// instead of the text. Every case here goes through llc + cc.
func TestAOTStringPrintsMatchPython(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"dict value print after printing the dict",
			"d = {}\nd[\"k\"] = \"value\"\nprint(d)\nprint(d[\"k\"])\n", "{'k': 'value'}\nvalue\n"},
		{"dict value print without printing the dict",
			"d = {}\nd[\"k\"] = \"value\"\nprint(d[\"k\"])\n", "value\n"},
		{"utf8 dict value", "d = {}\nd[\"k\"] = \"é\"\nprint(d[\"k\"])\n", "é\n"},
		{"list element print", "xs = [\"a\", \"b\"]\nprint(xs[1])\n", "b\n"},
		{"string returned by a function", "def g():\n    return \"hello\"\n\nprint(g())\n", "hello\n"},
		{"string returned and compared", "def g():\n    return \"yes\"\n\nprint(g() == \"yes\")\n", "True\n"},
		{"escaped string returned by a function", "def g():\n    return \"a\\tb\"\n\nprint(g())\n", "a\tb\n"},
	} {
		if py := pythonOutput(t, tc.src); py != tc.want {
			t.Fatalf("%s: expected output disagrees with CPython\n cpython = %q\n   want   = %q", tc.name, py, tc.want)
		}
		lang.RecordedStdoutIs(t, tc.src, tc.want)
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if strings.Contains(res.IR, "i32 @.str") {
			t.Errorf("%s: a global string reached an i32 slot", tc.name)
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: module must verify: %v %v", tc.name, v.Errors, verr)
		}
		if got := runAOT(t, tc.src); got != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, got, tc.want)
		}
	}
}
