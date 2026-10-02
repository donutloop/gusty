package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Strings across a function boundary, compiled through LLVM (roadmap Gap J.5).
//
// A string is a compile-time global (@.strN) and a parameter slot is an i32, so
// `greet("ada")` used to lower to `call i32 @greet(i32 @.str1)` — IR LLVM rejected, which the
// exit-code contract reported as a compiler bug for valid code. Arguments are now interned
// into the runtime string table (Gap I.2) and the callee receives the index, so both backends
// produce what CPython produces.

const stringArgProgram = `def greet(name):
    print("hello", name)
    return 1

print(greet("ada"))
`

func TestStringArgumentRunsOnBothBackends(t *testing.T) {
	want := "hello ada\n1\n"
	if got := runInterp(t, stringArgProgram); got != want {
		t.Errorf("interpreter = %q, want %q", got, want)
	}
	if got := compileAndRun(t, stringArgProgram); got != want {
		t.Errorf("AOT = %q, want %q", got, want)
	}
}

var stringArgCases = []struct {
	name string
	src  string
	want string
}{
	{"print the parameter", "def shout(msg):\n    print(msg)\n\nshout(\"hi\")\n", "hi\n"},
	{"return the parameter", "def echo(s):\n    return s\n\nprint(echo(\"yo\"))\n", "yo\n"},
	{"length", "def size(s):\n    return len(s)\n\nprint(size(\"abcd\"))\n", "4\n"},
	{"compare to a literal", "def is_yes(s):\n    return s == \"yes\"\n\nprint(is_yes(\"yes\"))\nprint(is_yes(\"no\"))\n", "True\nFalse\n"},
	{"compare two parameters", "def eq2(a, b):\n    return a == b\n\nprint(eq2(\"x\", \"x\"))\nprint(eq2(\"x\", \"y\"))\n", "True\nFalse\n"},
	{"keyword argument", "def greet(name):\n    print(name)\n\ngreet(name=\"kw\")\n", "kw\n"},
	{"string default", "def greet(name=\"world\"):\n    print(name)\n\ngreet()\n", "world\n"},
	{"annotated parameter", "def greet(name: str):\n    print(name)\n\ngreet(\"ada\")\n", "ada\n"},
	{"forwarded to a helper", "def a(s):\n    print(s)\n\ndef b(s):\n    a(s)\n\nb(\"fwd\")\n", "fwd\n"},
	{"membership needle", "def has(xs, want):\n    return want in xs\n\nprint(has([\"a\", \"b\"], \"a\"))\n", "True\n"},
	{
		"a helper fills a list its caller created",
		"def fill(out, v):\n    out.append(v)\n\nxs = []\nfill(xs, \"hi\")\nprint(xs)\n",
		"['hi']\n",
	},
	{
		"a helper fills a dict its caller created",
		"def put(d, k, v):\n    d[k] = v\n\nd = {}\nput(d, \"k\", 1)\nprint(d)\n",
		"{'k': 1}\n",
	},
	{
		"a helper grows a set its caller created",
		"def ins(s, v):\n    s.add(v)\n\nt = set()\nins(t, \"q\")\nprint(t)\n",
		"{'q'}\n",
	},
	{
		"two string parameters",
		"def pair(a, b):\n    print(a)\n    print(b)\n\npair(\"x\", \"y\")\n",
		"x\ny\n",
	},
}

func TestStringParameterProgramsMatchPython(t *testing.T) {
	for _, tc := range stringArgCases {
		gotInterp := runInterp(t, tc.src)
		if gotInterp != tc.want {
			t.Errorf("%s: interpreter = %q, want %q", tc.name, gotInterp, tc.want)
		}
		res, err := lang.Compile(tc.src)
		if err != nil {
			t.Errorf("%s: compile: %v", tc.name, err)
			continue
		}
		if v, verr := lang.VerifyModuleIR(res.IR, 0); verr != nil || !v.OK {
			t.Errorf("%s: emitted module must verify: %v %v", tc.name, v.Errors, verr)
			continue
		}
		if strings.Contains(res.IR, "i32 @.str") {
			t.Errorf("%s: a string pointer was stored in an i32 slot:\n%s", tc.name, res.IR)
		}
		gotAOT := compileAndRun(t, tc.src)
		if gotAOT != tc.want {
			t.Errorf("%s: AOT = %q, want %q", tc.name, gotAOT, tc.want)
		}
	}
}

// TestUnsupportedStringUseIsADiagnosticNotBadIR: an operation the compiled backend declines must
// report itself rather than emit IR the verifier rejects (ADR 0166). Concatenation used to be the
// example here; it answers since ADR 0230, so the shape is repetition, which still has no
// lowering — the contract under test is the refusal, not this particular operator.
func TestUnsupportedStringUseIsADiagnosticNotBadIR(t *testing.T) {
	src := "def shout(s):\n    return s * 2\n\nprint(shout(\"hi\"))\n"
	res, err := lang.Compile(src)
	if err == nil {
		t.Fatalf("expected a compile diagnostic; got IR:\n%s", res.IR)
	}
	msg := err.Error()
	for _, want := range []string{"on a string", "interpreter"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q should contain %q", msg, want)
		}
	}
	// The same message reaches the CLI's machine path unchanged, so an agent can branch on it
	// instead of on an llc error.
	out := cliRun(t, "--json", "--emit-llvm", src)
	for _, want := range []string{`"ok": false`, `"phase": "compile"`, "on a string"} {
		if !strings.Contains(out, want) {
			t.Errorf("CLI json %s should contain %q", out, want)
		}
	}
}
