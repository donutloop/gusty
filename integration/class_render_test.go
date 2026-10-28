package integration

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// cpythonRenderRun asks the oracle the question cpythonOut asks and keeps BOTH halves of a raise: the bytes the
// program got out before it, and the traceback it died with. A trap is an ANSWER here, not a harness failure —
// which is why this file has its own runner beside the one that treats a non-zero exit as the oracle failing.
func cpythonRenderRun(t *testing.T, path string) (stdout, stderr string, ok bool) {
	t.Helper()
	cmd := oracleCommand(t, path)
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return so.String(), se.String(), true
		}
		t.Fatalf("%s %s failed: %v", oracleBinaryName(), path, err)
	}
	return so.String(), se.String(), true
}

// Roadmap row **L12.3** / **Gap R.55** at the interface: `print`, `str()`, `repr()`, an f-string field and
// every container element consult the instance's own class `__str__` / `__repr__` before the value-form
// table (**ADR 0447**).
//
// The unit file (`pkg/lang/class_render_test.go`) judges the same shapes on the compiled leg and the record
// leg; this file asks the CLI, on both execution legs (the in-process JIT and a `cc` link-and-run), against
// a live CPython. The rows here are the ones whose answer is DETERMINIMATE — no identity form among them,
// because `<__main__.C object at 0x…>` carries an address that differs between two processes, and the
// address is an allocation accident the oracle itself normalises (`pkg/lang/oracle.go`'s
// `instanceIdentityRe`). The identity shapes are judged where the address can be folded out: the unit file.

func classRenderFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"__str__ answers print", "class C:\n    def __str__(self):\n        return \"S\"\n\nprint(C())\n", "S\n"},
		{"str() asks the class", "class C:\n    def __str__(self):\n        return \"S\"\n\nprint(str(C()))\n", "S\n"},
		{"repr() asks the class", "class C:\n    def __repr__(self):\n        return \"R\"\n\nprint(repr(C()))\n", "R\n"},
		{
			"__str__ wins print when the class binds both",
			"class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nprint(C())\n", "S\n",
		},
		{
			"a class binding only __repr__ answers print with it",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint(C())\n", "R\n",
		},
		{
			"a container element is the repr answer, unquoted",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint([C()])\n", "[R]\n",
		},
		{
			"a mixed container renders its instance element by class and its text with quotes",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint([1, C(), \"t\", None, 2.5])\n", "[1, R, 't', None, 2.5]\n",
		},
		{
			"a dict value renders by class",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\nd = {}\nprint({\"k\": C()})\n", "{'k': R}\n",
		},
		{
			"a set member renders by class",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\nprint({C()})\n", "{R}\n",
		},
		{
			"one print, three renderings",
			"class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nx = C()\nprint(x, [x], {\"k\": x})\n", "S [R] {'k': R}\n",
		},
		{
			"an interpolated field asks the class",
			"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(f\"v={C()}\")\n", "v=S\n",
		},
		{
			"the !r conversion asks the repr door",
			"class C:\n    def __str__(self):\n        return \"S\"\n\n    def __repr__(self):\n        return \"R\"\n\nprint(f\"{C()!r}\")\n", "R\n",
		},
		{
			"the answer is a text the program can concatenate",
			"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(\"<\" + str(C()) + \">\")\n", "<S>\n",
		},
		{
			"and compare",
			"class C:\n    def __str__(self):\n        return \"S\"\n\nprint(str(C()) == \"S\")\n", "True\n",
		},
		{
			"a member inherited from a base is found on the base that owns it",
			"class A:\n    def __repr__(self):\n        return \"A\"\n\nclass B(A):\n    pass\n\nprint([B()])\n", "[A]\n",
		},
		{
			"an override wins over the base's answer",
			"class A:\n    def __repr__(self):\n        return \"A\"\n\nclass B(A):\n    def __repr__(self):\n        return \"B\"\n\nprint([A(), B()])\n", "[A, B]\n",
		},
		{
			"the body may call the class's own methods",
			"class C:\n    def label(self):\n        return \"L\"\n\n    def __str__(self):\n        return f\"S:{self.label()}\"\n\nprint(C())\n", "S:L\n",
		},
		{
			"the output the body writes itself comes out first",
			"class C:\n    def __str__(self):\n        print(\"side\")\n        return \"S\"\n\nprint(C())\n", "side\nS\n",
		},
		{
			"a render trap is the reference's raise, at the reference's exit class",
			"class C:\n    def __str__(self):\n        return 7\n\nprint(C())\n", "TypeError: __str__ returned non-string (type int)\n",
		},
		{
			"and a raise out of the body propagates",
			"class C:\n    def __str__(self):\n        raise ValueError(\"nope\")\n\nprint(C())\n", "ValueError: nope\n",
		},
		{
			"a handler reaches it, which is the difference between a raise and a fault",
			"class C:\n    def __str__(self):\n        return 7\n\ntry:\n    print(C())\nexcept TypeError as e:\n    print(\"caught:\", str(e))\n", "caught: __str__ returned non-string (type int)\n",
		},
	}
}

// lastLine is how a trap is compared here: the raise's own `Class: message` line is the answer the reference
// prints on stderr, and the frames above it name positions this file does not own (the traceback rows are
// pinned in the unit file, where the module name and line are the program's own).
func renderTrapLastLine(s string) string {
	lines := []string{}
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}

// TestTheClassRenderAtTheCLIAnswersWhatTheReferenceAnswers is the reference leg, on both execution legs.
func TestTheClassRenderAtTheCLIAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range classRenderFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			trap := strings.HasPrefix(tc.want, "TypeError:") || strings.HasPrefix(tc.want, "ValueError:")
			path := writeSrc(t, t.TempDir(), "class_render.gy", tc.src)
			wantStdout, wantStderr, ok := cpythonRenderRun(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if trap {
					if code == 0 {
						t.Fatalf("%s: exit 0 where the reference raises %q (stdout %q)\n%s", engine, tc.want, stdout, tc.src)
					}
					if got := renderTrapLastLine(stderr); got != renderTrapLastLine(wantStderr) {
						t.Fatalf("%s: the raise read %q, the reference's is %q\n%s", engine, got, tc.want, tc.src)
					}
					if stdout != "" {
						t.Fatalf("%s: printed %q before the raise; the reference prints nothing\n%s", engine, stdout, tc.src)
					}
					continue
				}
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, wantStdout, stderr, tc.src)
				}
				if stdout != wantStdout {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, wantStdout, tc.src)
				}
			}
		})
	}
}

// TestWhatTheClassRenderLeavesStandingAtTheCLI is the residue at the interface: the shapes this row does not
// answer keep their honest fallback or their refusal, and the row that owns the fix is named.
func TestWhatTheClassRenderLeavesStandingAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		wantOut string
		says    string
		owner   string
	}{
		{
			// The container asks __repr__, and this class binds only __str__ — so the reference prints the
			// identity form too, with an address of its own. The row pins the SHAPE; the unit file folds the
			// address out (oracle.go's instanceIdentityRe) and compares the whole line.
			"a class binding only __str__ keeps the identity form inside a container",
			"class C:\n    def __str__(self):\n        return \"S\"\n\nprint([C()])\n",
			"[<__main__.C object at 0x", "", "**ADR 0325**'s identity form — the reference prints the same shape",
		},
		{
			"a parameter that receives an instance prints the handle it was stored as",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\ndef show(o):\n    print(o)\n\nshow(C())\n",
			"0\n", "", "Gap R.509",
		},
		{
			"the loop variable of a literal list of instances prints the handle",
			"class C:\n    def __repr__(self):\n        return \"R\"\n\nfor c in [C(), C()]:\n    print(c)\n",
			"0\n1\n", "", "Gap R.509",
		},
		{
			"a dunder concatenating a literal with a method call keeps the identity form",
			"class Loud:\n    def tag(self):\n        return \"loud\"\n\n    def __str__(self):\n        return \"Loud<\" + self.tag() + \">\"\n\nprint(Loud())\n",
			"<__main__.Loud object at 0x", "", "Gap R.510",
		},
		{
			"a dunder whose answer is an attribute the compiler cannot read keeps the identity form",
			"class C:\n    def __init__(self):\n        self.v = \"S\"\n\n    def __str__(self):\n        return self.v\n\nprint(C())\n",
			"<__main__.C object at 0x", "", "Gap R.431",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "class_render_owed.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if tc.says != "" {
					if code == 0 || !strings.Contains(stderr, tc.says) {
						t.Fatalf("%s: exit %d and %q where this row pins a refusal naming %q\n%s",
							engine, code, stderr, tc.says, tc.src)
					}
					continue
				}
				if !strings.Contains(stdout, tc.wantOut) {
					t.Fatalf("%s: printed %q (exit %d), which this row does not pin — the reference answers differently, and %s owns the fix\n%s",
						engine, stdout, code, tc.owner, tc.src)
				}
			}
		})
	}
}
