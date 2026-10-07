package integration

// integration/module_const_test.go — a data import's constant keeps its type through the shipped CLI, on
// the compiled path, against the reference (roadmap L11.6's typed stdlib constants; the ledger's
// `probe_math_const`, ADR 0272).
//
// The Phase 11 census table recorded the row in one line: `import math; print(math.PI)` — CPython
// `3.141592653589793`, interpreter ✅, compiled `3`. The compiled backend resolved `mod.NAME` to the folded
// literal only where it *writes* the value; every predicate that decides *how* — is this a double, does it
// have a sign — saw an `*Attr` and took the integer road, so the double was poured into an `i32`. `math.PI *
// 2` printed `6`, `x = math.PI` / `print(x > 3.14)` printed `False`, and a hand-written literal on the line
// above was right, which is what made it look like a stdlib bug rather than a kind bug.
//
// The reference leg is a twin: a class with the same names and the same values, and the program's own body
// unchanged. A data module is data, so the twin is honest — it is the same question asked of CPython.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleConstTwin is the reference's spelling of the same data module.
const moduleConstTwin = `class consts:
    PI = 3.141592653589793
    E = 2.718281828459045
    NAME = "red"
    N = 7
`

// moduleConstDir writes consts.gy (gusty's data module) into a temp dir and returns the dir plus the program
// path, so `--stdlib <dir>` resolves the import exactly the way `stdlib/math.gy` resolves from a checkout.
func moduleConstDir(t *testing.T, src string) (dir, gy, twin string) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "consts.gy"), []byte(moduleConstSrc), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	gy = writeSrc(t, dir, "use_consts.gy", src)
	twin = moduleConstTwin + "\n" + strings.ReplaceAll(src, "import consts\n", "")
	return dir, gy, twin
}

// moduleConstSrc is duplicated from pkg/lang/module_const_test.go on purpose: the unit file imports a module
// in-process, this one through the shipped CLI, and the two must be able to drift only in what they assert.
const moduleConstSrc = `PI = 3.141592653589793
E = 2.718281828459045
NAME = "red"
N = 7
`

func TestADataImportConstantAnswersLikeTheReferenceOnBothLegs(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"the census row", "import consts\nprint(consts.PI)\n", "3.141592653589793\n"},
		{"the second constant", "import consts\nprint(consts.E)\n", "2.718281828459045\n"},
		{"the negation", "import consts\nprint(-consts.PI)\n", "-3.141592653589793\n"},
		{"times an integer", "import consts\nprint(consts.PI * 2)\n", "6.283185307179586\n"},
		{"true division", "import consts\nprint(consts.PI / 2)\n", "1.5707963267948966\n"},
		{"the comparison a truncated word loses", "import consts\nx = consts.PI\nprint(x > 3.14)\n", "True\n"},
		{"an integer constant", "import consts\nprint(consts.N)\n", "7\n"},
		{"a text constant", "import consts\nprint(consts.NAME)\n", "red\n"},
		{"in an f-string", "import consts\nprint(f\"{consts.PI}\")\n", "3.141592653589793\n"},
		{"appended to a container", "import consts\nys = []\nys.append(consts.PI)\nprint(ys[0])\n", "3.141592653589793\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, gy, twin := moduleConstDir(t, tc.src)
			py, ok := cpythonPlainOut(t, dir, twin)
			if !ok || py != tc.want {
				t.Fatalf("the reference said %q, want %q\nsrc: %s", py, tc.want, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliReport(t, engine, "--stdlib", dir, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Errorf("%s: stdout %q, want %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

func TestAModuleConstantTrapsTheWayTheReferenceDoes(t *testing.T) {
	for _, tc := range []struct{ name, src, kind string }{
		{"abs of a text the module declared", "import consts\nprint(abs(consts.NAME))\n", "str"},
		{"the negation of one", "import consts\nprint(-consts.NAME)\n", "str"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, gy, twin := moduleConstDir(t, tc.src)
			sentence := "TypeError: bad operand type for "
			if strings.Contains(tc.src, "abs(") {
				sentence += "abs(): '" + tc.kind + "'"
			} else {
				sentence += "unary -: '" + tc.kind + "'"
			}
			py, ok := cpythonPlainOut(t, dir, twin)
			if ok || !strings.Contains(py, sentence) {
				t.Fatalf("the reference did not trap with %q (ok %v): %q\nsrc: %s", sentence, ok, py, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliReport(t, engine, "--stdlib", dir, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: the compiler's own module was rejected (ADR 0166):\n%s", engine, out)
				}
				if code != 3 {
					t.Fatalf("%s: exit %d, want the trap exit 3 (ADR 0166)\n%s", engine, code, out)
				}
				if !strings.Contains(out, sentence) {
					t.Errorf("%s: did not say %q (reference said %q):\n%s", engine, sentence, py, out)
				}
			}
		})
	}
}

// TestTheStdlibProbeProgramAnswersOnBothLegs is `probe_math_const`, the program the ledger recorded as
// `not_applicable` for the spelling alone (gusty spells PI, the reference spells pi). Both legs now print
// what the reference prints for its own name — the reason the row stays `not_applicable` is that the twin
// cannot be the same file, not that the legs disagree.
func TestTheStdlibProbeProgramAnswersOnBothLegs(t *testing.T) {
	src := readProgram(t, "probe_math_const.gy")
	dir := t.TempDir()
	gy := writeSrc(t, dir, "probe_math_const.gy", src)
	want := "3.141592653589793\n2.718281828459045\n"
	for _, engine := range cliEngines {
		out, code := cliReport(t, engine, "--file", gy)
		if code != 0 || out != want {
			t.Errorf("%s: exit %d, stdout %q, want %q", engine, code, out, want)
		}
	}
	// The reference, spelled its own way, says the same two numbers.
	if py, ok := cpythonPlainOut(t, dir, "import math\nprint(math.pi)\nprint(math.e)\n"); !ok || py != want {
		t.Errorf("the reference said %q (ok %v), want %q", py, ok, want)
	}
}
