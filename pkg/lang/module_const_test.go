package lang

// pkg/lang/module_const_test.go — a data import's constant keeps the type the module declares, on both
// backends (roadmap L11.6, the typed stdlib constants; the ledger's `probe_math_const`).
//
// `import math` / `print(math.PI)` is the most ordinary program the standard library offers. The compiled
// backend printed `3` — the double poured into the integer word — while the record, which evaluates the
// module and reads the value back, printed `3.141592653589793`, and a program that spelled the same double by
// hand (`pi = 3.141592653589793`) has always been right on both. The kind was in the source; only half the
// questions were allowed to read it. `value()` resolved `mod.NAME` through the fold `resolveImports` built,
// and every predicate that decides *how* to write the value — is this a double, does it have a sign — saw an
// `*Attr` it could not read and took the integer road.
//
// Four claims, each a different way to be wrong:
//   - the answers, on both legs, for the family of positions a constant can sit in (print, arithmetic,
//     true division, the negation, a binding then a comparison, `str`, an f-string, a container element,
//     `round`);
//   - the fold asked *as a function* — the one read in `module_const.go` must resolve, must decline for a
//     name the program binds itself, and must not invent a value for a module attribute that is not there;
//   - the kind doors: the signless door ADR 0271 built must name a text constant `'str'` for both `-x` and
//     `abs(x)`, which is the same read serving a raise rather than a print;
//   - the IR: a module constant that is a double reaches the module as a `double`, and never as the
//     truncated integer underneath it.
//
// The three-engine comparison through the shipped CLI, with `--stdlib`, lives in
// integration/module_const_test.go.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleConstSrc is the data module the tests import — one constant per kind the compiled word can hold
// wrongly, spelled so the same file reads like a stdlib module (a name, then a value per line).
const moduleConstSrc = `PI = 3.141592653589793
E = 2.718281828459045
NAME = "red"
N = 7
`

// constStdlib is a temporary standard library holding moduleConstSrc, and the search path that reads it.
//
// It used to be `SetStdlibDir(dir)` + a cleanup, which is an act on the process: for the length of the case
// it redecided which library *every* other case in the process resolved against. The fixture is a value now,
// handed to the compile, the run and the fold (ADR 0343), which is what lets four whole test functions stop
// asking to run alone.
type constStdlib struct {
	dir   string
	paths moduleSearch
}

func withStdlib(t *testing.T) constStdlib {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "consts.gy")
	if err := os.WriteFile(path, []byte(moduleConstSrc), 0o600); err != nil {
		t.Fatalf("write module: %v", err)
	}
	return constStdlib{dir: dir, paths: moduleSearch{stdlib: dir}}
}

// compile asks the compiled backend for the module's program, reading the fixture's library.
func (c constStdlib) compile(t *testing.T, src string) *CompileResult {
	t.Helper()
	res, err := CompileWith(CompileRun{StdlibDir: c.dir}, src)
	if err != nil {
		t.Fatalf("compiled leg: %v\nsrc: %s", err, src)
	}
	return res
}

// stdout asks what the program prints, with the fixture's library on the run.
func (c constStdlib) stdout(t *testing.T, src string) string {
	t.Helper()
	if _, ok := loadGolden(t)[src]; ok {
		return goldenStdoutWith(t, c.paths, src)
	}
	out, err := RunSourceWith(CompileRun{StdlibDir: c.dir}, src)
	if err != nil {
		t.Fatalf("compiled run of %q: %v", src, err)
	}
	return out
}

// trap asks which exception escapes, with the fixture's library on the run.
func (c constStdlib) trap(t *testing.T, src string) *TrapError {
	t.Helper()
	err := goldenRunErrorWith(t, c.paths, src)
	if err == nil {
		t.Fatalf("the program raised nothing where the case expects a trap:\n%s", src)
	}
	ee, ok := err.(*TrapError)
	if !ok {
		t.Fatalf("want a *TrapError, got %T: %v", err, err)
	}
	return ee
}

// defaultStdlib is the library the checked-in `stdlib/` directory is — the one a shell user's `import math`
// reads. Asking for it is now simply asking for no override, where it used to be `SetStdlibDir("")` and a
// cleanup that put the process back.
func defaultStdlib() constStdlib { return constStdlib{paths: moduleSearch{stdlib: stdlibRoot()}} }

// TestADataImportConstantKeepsItsTypeOnTheCompiledBackend is the parity family.
func TestADataImportConstantKeepsItsTypeOnTheCompiledBackend(t *testing.T) {
	t.Parallel()
	stdlib := withStdlib(t)
	for _, tc := range []struct{ name, src, want string }{
		{"the row the census recorded", "import consts\nprint(consts.PI)\n", "3.141592653589793\n"},
		{"a second constant of the same kind", "import consts\nprint(consts.E)\n", "2.718281828459045\n"},
		{"the negation of a module double", "import consts\nprint(-consts.PI)\n", "-3.141592653589793\n"},
		{"times an integer", "import consts\nprint(consts.PI * 2)\n", "6.283185307179586\n"},
		{"true division keeps the double", "import consts\nprint(consts.PI / 2)\n", "1.5707963267948966\n"},
		{"the comparison a truncated word loses", "import consts\nx = consts.PI\nprint(x > 3.14)\n", "True\n"},
		{"two constants compared", "import consts\nprint(consts.PI > consts.E)\n", "True\n"},
		{"an integer constant is still an integer", "import consts\nprint(consts.N)\nprint(consts.N + 1)\n", "7\n8\n"},
		{"a text constant prints its characters", "import consts\nprint(consts.NAME)\nprint(consts.NAME + \"!\")\n", "red\nred!\n"},
		{"a text constant is measured, not indexed", "import consts\nprint(len(consts.NAME))\n", "3\n"},
		{"through str()", "import consts\nprint(str(consts.PI))\n", "3.141592653589793\n"},
		{"in an f-string", "import consts\nprint(f\"{consts.PI}\")\n", "3.141592653589793\n"},
		{"rounded", "import consts\nprint(round(consts.PI, 2))\n", "3.14\n"},
		{"in a container literal", "import consts\nprint([consts.PI])\n", "[3.141592653589793]\n"},
		{"appended to a container the program built", "import consts\nys = []\nys.append(consts.PI)\nprint(ys[0])\n", "3.141592653589793\n"},
		{"in a condition", "import consts\nif consts.PI > 3.0:\n    print(\"big\")\n", "big\n"},
		{"floor division of a module double", "import consts\nprint(consts.PI // 1)\n", "3.0\n"},
		{"a hand-written double beside it is unchanged", "pi = 3.141592653589793\nprint(pi * 2)\n", "6.283185307179586\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := stdlib.stdout(t, tc.src)
			if out != tc.want {
				t.Errorf("compiled answer for the printed value: stdout %q, want %q\nsrc: %s", out, tc.want, tc.src)
			}
			res := stdlib.compile(t, tc.src)
			assertNoForbiddenIR(t, tc.src, res.IR)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: stdout %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestTheSignlessDoorNamesAModuleConstant is ADR 0271's door serving a raise instead of a print: a text the
// module declared is a text for `-x` and for `abs(x)`, on both legs. Before the shared read the compiled
// leg answered `abs(consts.NAME)` with the number `0` — the interned index subtracted from zero — where the
// reference stops with `bad operand type for abs(): 'str'`.
func TestTheSignlessDoorNamesAModuleConstant(t *testing.T) {
	t.Parallel()
	stdlib := withStdlib(t)
	for _, tc := range []struct{ name, src, class, message string }{
		{"abs of a text constant", "import consts\nprint(abs(consts.NAME))\n", "TypeError", "bad operand type for abs(): 'str'"},
		{"the negation of a text constant", "import consts\nprint(-consts.NAME)\n", "TypeError", "bad operand type for unary -: 'str'"},
		{"caught, because a raise is a raise", "import consts\ntry:\n    print(abs(consts.NAME))\nexcept TypeError:\n    print(\"caught\")\n", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.class == "" {
				if out := stdlib.stdout(t, tc.src); out != "caught\n" {
					t.Errorf("the compiled run did not reach the arm: stdout %q, want \"caught\\n\"", out)
				}
				res := stdlib.compile(t, tc.src)
				if out := runIR(t, res.IR); out != "caught\n" {
					t.Errorf("compiled: stdout %q, want \"caught\\n\"", out)
				}
				return
			}
			ee := stdlib.trap(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("the compiled backend raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("the compiled message = %q, want %q", ee.ExnMsg, tc.message)
			}
			res := stdlib.compile(t, tc.src)
			assertNoForbiddenIR(t, tc.src, res.IR)
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, tc.message) {
				t.Errorf("compiled did not say %q:\n%s", tc.message, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the oracle traps on:\n%s", out)
			}
		})
	}
}

// TestFoldedModuleAttrIsOneReadNotTwo asks the read in module_const.go directly. It must resolve a declared
// constant, answer "nothing folded" for anything that is not a data-import attribute, and never claim a
// value for a name the importing program owns — the guard that keeps `math = 3` meaning what the program
// wrote rather than what the module declares.
func TestFoldedModuleAttrIsOneReadNotTwo(t *testing.T) {
	t.Parallel()
	stdlib := withStdlib(t)
	prog, err := parseProgram("import consts\nprint(consts.PI)\nx = 3\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info, err := resolveImports(prog, stdlib.paths)
	if err != nil {
		t.Fatalf("resolve imports: %v", err)
	}
	g := &irGen{imports: info}

	declared := &Attr{Obj: &Name{Value: "consts"}, Name: &Name{Value: "PI"}}
	lit, ok := g.foldedModuleAttr(declared)
	if !ok {
		t.Fatalf("the read declined a constant the module declares")
	}
	f, isFloat := lit.(*FloatLit)
	if !isFloat || f.Value != 3.141592653589793 {
		t.Fatalf("folded to %#v, want the module's *FloatLit", lit)
	}
	if !g.isFloat(declared) {
		t.Errorf("isFloat did not read through the fold: print(math.PI) is the row this exists for")
	}
	if g.foldedModuleFloat(declared) != true {
		t.Errorf("foldedModuleFloat disagreed with isFloat for the same expression")
	}
	text := &Attr{Obj: &Name{Value: "consts"}, Name: &Name{Value: "NAME"}}
	if g.foldedModuleFloat(text) {
		t.Errorf("a text constant reported as a double")
	}
	if _, ok := g.foldedModuleAttr(&Attr{Obj: &Name{Value: "consts"}, Name: &Name{Value: "MISSING"}}); ok {
		t.Errorf("the read invented a value for an attribute the module does not declare")
	}
	if _, ok := g.foldedModuleAttr(&Name{Value: "x"}); ok {
		t.Errorf("a plain name is not a module attribute")
	}
	// A name the program binds itself is the program's, whatever the module says.
	own := &irGen{imports: info, moduleNames: map[string]bool{"consts": true}}
	if _, ok := own.foldedModuleAttr(declared); ok {
		t.Errorf("the fold answered for a name the program owns")
	}
	if own.isFloat(declared) {
		t.Errorf("isFloat read a module constant through a name the program binds")
	}
}

// TestTheRealStdlibModuleStillAnswers is the census row itself, against the checked-in stdlib rather than a
// temporary one: `import math` / `print(math.PI)` is the program the Phase 11 table recorded as `3`
// compiled, and `probe_math_const` is its ledger row.
func TestTheRealStdlibModuleStillAnswers(t *testing.T) {
	t.Parallel()
	stdlib := defaultStdlib()
	for _, tc := range []struct{ src, want string }{
		{"import math\nprint(math.PI)\n", "3.141592653589793\n"},
		{"import math\nprint(math.E)\n", "2.718281828459045\n"},
		{"import math\nprint(math.PI * 2)\n", "6.283185307179586\n"},
		{"import math\nprint(math.SQRT2 * math.SQRT2)\n", "2.0000000000000004\n"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			t.Parallel()
			if out := stdlib.stdout(t, tc.src); out != tc.want {
				t.Errorf("compiled: %q, want %q", out, tc.want)
			}
			res := stdlib.compile(t, tc.src)
			if got := runIR(t, res.IR); got != tc.want {
				t.Errorf("compiled: %q, want %q", got, tc.want)
			}
			if strings.Contains(res.IR, "sitofp i32 3 to double") {
				t.Errorf("the module's double reached the module as the integer underneath it:\n%s", res.IR)
			}
		})
	}
}
