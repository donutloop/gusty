package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestShadowedBuiltinsAgreeOnEveryPath is the three-leg contract for Gap R.6 (ADR 0199).
//
// A built-in call name is a name, not a keyword. `def str`, `def float`, `def len`, `def abs`,
// `def min`, `def round` are legal, and afterwards `str(1)` means the program's function — that
// is what the interpreter did and what CPython does. The compiled path had read the call by name
// through the built-in's meaning instead, which showed up two ways: `float(1)` folded to the
// conversion (1.0 for a function returning x + 7), and `str(1)` / `chr(1)` were emitted as the
// program's call and then *used* as the built-in's string result, so llc rejected the module.
func TestShadowedBuiltinsAgreeOnEveryPath(t *testing.T) {
	src := readProgramSrc("shadowed_builtins")
	want := "8\n9\n8\n9.5\n3\n4\n9\n3\n10\n"

	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	pyOut, pyErr, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skipf("no usable oracle: %v\n%s", perr, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// TestShadowedBuiltinsCheckClean: the front end must not be the last holdout — a program that
// shadows a built-in is legal, so it earns no diagnostic.
func TestShadowedBuiltinsCheckClean(t *testing.T) {
	prog, err := lang.Parse(readProgramSrc("shadowed_builtins"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range lang.Analyze(prog) {
		if d.Level == lang.LevelError {
			t.Errorf("shadowing a built-in earned an error: %v", d)
		}
	}
}

// TestShadowedBuiltinBatteryCompiles runs the single-name shapes, one name per module, which is
// where the fold paths were individually reachable — a fold in one helper does not imply another.
func TestShadowedBuiltinBatteryCompiles(t *testing.T) {
	for _, name := range []string{"str", "float", "chr", "int", "ord", "round", "abs", "sum", "len", "min", "max", "sorted", "reversed", "sqrt", "floor", "ceil", "any", "all"} {
		src := "def " + name + "(x):\n    return x + 7\n\nprint(" + name + "(1))\n"
		built, err := runAOTWithTimeout(t, src, 60*time.Second)
		if err != nil {
			t.Errorf("shadowing %s: compiled run failed: %v", name, err)
			continue
		}
		if strings.TrimSpace(built) != "8" {
			t.Errorf("shadowing %s: compiled printed %q, want 8 — the program's own definition", name, built)
		}
	}
}
