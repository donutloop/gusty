package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// End-to-end coverage for roadmap Gap R.36 + R.39 (ADR 0228): a local read on a path that never
// assigned it. The expectation is CPython's, never the emission's — each row is run through python3
// first, and the test refuses to proceed if CPython disagrees with what the row claims. Then both
// gusty engines must agree on stdout and on the exception class, and both must exit 3 (the trap code
// from docs/operations.md), because a slot the program never wrote is allowed to produce neither a
// value nor a success status.

// unwrittenShapes: source, the stdout printed before the raise, and the class CPython raises.
var unwrittenShapes = []struct{ name, src, stdout, class string }{
	{
		"branch assignment",
		"def f(c):\n    if c:\n        x = 1\n    return x\n\nprint(f(True))\nprint(f(False))\n",
		"1\n", "UnboundLocalError",
	},
	{
		"loop that never runs",
		"def f():\n    while 0:\n        w = 1\n    return w\n\nprint(f())\n",
		"", "UnboundLocalError",
	},
	{
		"try body cut short",
		"def f():\n    try:\n        a = 1 // 0\n        b = 2\n    except ZeroDivisionError:\n        pass\n    return b\n\nprint(f())\n",
		"", "UnboundLocalError",
	},
	{
		"for body never runs",
		"def f():\n    for i in []:\n        z = 1\n    return z\n\nprint(f())\n",
		"", "UnboundLocalError",
	},
	{
		"tuple unpack on one branch",
		"def f(c):\n    if c:\n        p, q = 1, 2\n    return p\n\nprint(f(False))\n",
		"", "UnboundLocalError",
	},
	{
		"match arm that does not match",
		"def f(v):\n    match v:\n        case 1:\n            hit = 1\n    return hit\n\nprint(f(2))\n",
		"", "UnboundLocalError",
	},
	{
		"augmented assignment with nothing to augment",
		"def f(c):\n    if c:\n        t = 0\n    t += 1\n    return t\n\nprint(f(False))\n",
		"", "UnboundLocalError",
	},
	{
		"read above the write",
		"def f():\n    print(v)\n    v = 2\n\nprint(f())\n",
		"", "UnboundLocalError",
	},
	{
		"module: branch assignment",
		"if 0:\n    x = 1\nprint(x)\n",
		"", "NameError",
	},
	{
		"module: loop that never runs",
		"while 0:\n    w = 1\nprint(w)\n",
		"", "NameError",
	},
	{
		"module: try body cut short",
		"try:\n    a = 1 // 0\n    b = 2\nexcept ZeroDivisionError:\n    pass\nprint(b)\n",
		"", "NameError",
	},
	{
		"module: for body never runs",
		"for i in []:\n    z = 1\nprint(z)\n",
		"", "NameError",
	},
}

// TestUnwrittenSlotMatchesCPython: each shape, three engines. CPython defines the expectation; the
// interpreter and the compiled binary must print the same stdout, raise the same class, and exit 3.
func TestUnwrittenSlotMatchesCPython(t *testing.T) {
	for _, tc := range unwrittenShapes {
		t.Run(tc.name, func(t *testing.T) {
			wantOut, _, perr := lang.PythonRun(tc.src)
			if perr == nil {
				t.Fatalf("CPython accepted a program this table says raises %s — re-derive the expectation\n%s", tc.class, tc.src)
			}
			if wantOut != tc.stdout {
				t.Fatalf("CPython printed %q, this table claims %q — the expectation itself is wrong\n%s", wantOut, tc.stdout, tc.src)
			}

			ipath := writeSrc(t, t.TempDir(), "interp.gy", tc.src)
			iout, icode := cliRunCode(t, "--aot", ipath)
			if icode != 3 {
				t.Fatalf("interpreter exit = %d, want 3 (a trap) — stdout %q", icode, iout)
			}
			if iout != tc.stdout {
				t.Fatalf("interpreter printed %q, want %q", iout, tc.stdout)
			}
			itext := cliRun(t, "--aot", ipath)
			if !strings.Contains(itext, tc.class) {
				t.Fatalf("interpreter raised the wrong class, want %s:\n%s", tc.class, itext)
			}

			apath := writeSrc(t, t.TempDir(), "aot.gy", tc.src)
			aout, acode := cliRunCode(t, "--aot", apath)
			if acode != 3 {
				t.Fatalf("compiled exit = %d, want 3 (a trap) — stdout %q. A trap must not read as a compile error (1) or a success (0): ADR 0211", acode, aout)
			}
			if aout != tc.stdout {
				t.Fatalf("compiled printed %q, want %q — the silent zero is what this test exists to catch\n%s", aout, tc.stdout, tc.src)
			}
			atext := cliRun(t, "--aot", apath)
			if !strings.Contains(atext, tc.class) {
				t.Fatalf("compiled raised the wrong class, want %s:\n%s", tc.class, atext)
			}
		})
	}
}

// TestUnwrittenSlotIsCatchableAndNotOverEager: the flag must trap exactly the reads CPython traps.
// A name assigned on every path is not an error (and must not pay for a flag), and a handler must be
// able to catch the trap by class on the compiled path.
func TestUnwrittenSlotIsCatchableAndNotOverEager(t *testing.T) {
	definite := "def f(c):\n    if c:\n        r = 1\n    else:\n        r = 2\n    return r\n\nprint(f(True), f(False))\n"
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "definite.gy", definite)
		out, code := cliRunCode(t, engine, path)
		if code != 0 || out != "1 2\n" {
			t.Fatalf("%s printed %q with exit %d, want `1 2` and exit 0 — a read every path assigns must not trap", engine, out, code)
		}
	}
	handled := "def f(c):\n    if c:\n        x = 1\n    try:\n        return x\n    except UnboundLocalError:\n        return -1\n\nprint(f(True), f(False))\n"
	want, perrText, perr := lang.PythonRun(handled)
	if perr != nil {
		t.Fatalf("CPython disagreed with the catch expectation: %v\n%s", perr, perrText)
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "handled.gy", handled)
		out, code := cliRunCode(t, engine, path)
		if code != 0 || out != want {
			t.Fatalf("%s printed %q exit %d, want %q exit 0 — `except UnboundLocalError:` must catch the unwritten read (ADR 0212: a built-in trap is a typed raise)", engine, out, code, want)
		}
	}
}
