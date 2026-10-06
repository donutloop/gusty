package integration

// The CLI half of "a declared name is not a variable" (roadmap Gap R.150, Gap R.151, Gap R.168,
// ADR 0283). Before this cycle `f + 1`, `xs = [f]`, `str(f)`, `print(f)` and `print(lambda x: x)` left
// `--aot` through **exit 2** — `llc-20` rejecting a module that loaded `i32* %_f`, a slot no `def` ever
// allocated, or wrote the closure's function global into a `printf` operand. Exit 2 is the exit-code
// contract's "the compiler is broken" code (ADR 0166), and it was being spent on programs the reference
// answers or stops on in one line.
//
// Each row records what `python3` says in the failure text, so a row that stops being a refusal can be
// read against the oracle without re-deriving it by hand.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cliPercent283(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

func cpython283(t *testing.T, dir, src string) (string, int) {
	t.Helper()
	py := os.Getenv("GUSTY_PYTHON")
	if py == "" {
		py = "python3"
	}
	path := filepath.Join(dir, "oracle283.gy")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write oracle: %v", err)
	}
	cmd := exec.Command(py, path)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			return out.String(), -1
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

// TestADeclaredNameAsAValueNeverExitsTwo is the exit-2 table at the CLI.
func TestADeclaredNameAsAValueNeverExitsTwo(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src string }{
		{"a function name in arithmetic", head + "print(f + 1)\n"},
		{"a function name on the left", head + "print(1 + f)\n"},
		{"a function name in a list", head + "xs = [f]\nprint(len(xs))\n"},
		{"a function name passed to str()", head + "print(str(f))\n"},
		{"a function name as a print argument", head + "print(f)\n"},
		{"a function name compared to a number", head + "print(1 if f == 1 else 0)\n"},
		{"a module name in arithmetic", "import math\n\nprint(math + 1)\n"},
		{"a lambda printed", "print(lambda x: x)\n"},
		{"a lambda in arithmetic", "print((lambda x: x) + 1)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "fnvalue.gy", tc.src)
			py, pyCode := cpython283(t, dir, tc.src)
			out, code := cliPercent283(t, "--aot", "--file", gy)
			if code == 2 {
				t.Fatalf("--aot: exit 2, the contract's compiler-bug code, on a program the reference reports as %d (ADR 0166):\n%s\nreference said:\n%s", pyCode, out, py)
			}
			if code == 0 && !strings.Contains(py, "Error") {
				t.Logf("the compiled leg answers %q where the reference prints %q — a divergence to read, not an exit class to hide", out, py)
			}
			if code != 0 && code != 1 && code != 3 {
				t.Fatalf("--aot: exit %d, want 0 (answer), 1 (front-end refusal) or 3 (a trap the reference traps on)\noutput: %s", code, out)
			}
			if code == 1 && !strings.Contains(out, "codegen") {
				t.Errorf("a refusal must name the pass that declined, got: %s", out)
			}
			if strings.Contains(out, "use of undefined value") || strings.Contains(out, "LLVM ERROR") || strings.Contains(out, "COMPILER BUG") {
				t.Errorf("a toolchain failure reached the user:\n%s", out)
			}
		})
	}
}

// TestAFunctionOperandTrapsWithTheReferencesClassIsTheParityTable: the numeric door raises rather than
// refusing, so the compiled path print CPython's sentence with CPython's word and the program can catch it.
func TestAFunctionOperandTrapsWithTheReferencesClass(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src, want string }{
		{"negation of a def'd name", head + "print(-f)\n", "TypeError: bad operand type for unary -: 'function'"},
		{"abs of a def'd name", head + "print(abs(f))\n", "TypeError: bad operand type for abs(): 'function'"},
		{"negation of a lambda", "print(-(lambda x: x))\n", "TypeError: bad operand type for unary -: 'function'"},
		{"abs of a lambda", "print(abs(lambda x: x))\n", "TypeError: bad operand type for abs(): 'function'"},
		{"negation of a module", "import math\n\nprint(-math)\n", "TypeError: bad operand type for unary -: 'module'"},
		{"abs of a module", "import math\n\nprint(abs(math))\n", "TypeError: bad operand type for abs(): 'module'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "fnvalue.gy", tc.src)
			py, pyCode := cpython283(t, dir, tc.src)
			if !strings.Contains(py, tc.want) {
				t.Fatalf("the pinned sentence is not the reference's: python3 said\n%s", py)
			}
			if pyCode == 0 {
				t.Fatalf("the reference was expected to stop on this program")
			}
			for _, engine := range cliEngines {
				out, code := cliPercent283(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 on a program the reference traps (ADR 0166):\n%s", engine, out)
				}
				if !strings.Contains(out, tc.want) {
					t.Errorf("%s must print CPython's sentence %q, got:\n%s", engine, tc.want, out)
				}
				if strings.Contains(out, "codegen:") {
					t.Errorf("%s refused what the reference only traps on:\n%s", engine, out)
				}
			}
		})
	}
}

// TestAFunctionStillFlowsThroughAParameterIsTheNarrowing: the guards name a *declaration*, so the
// programs that pass functions around keep their answers — and one of them is the promoted probe.
func TestAFunctionStillFlowsThroughAParameter(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"a lambda called through a parameter", "def twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(twice(lambda x: x * 3, 2))\n", "12\n"},
		{"a lambda bound then called", "g = lambda x: x * 3\nprint(g(4))\n", "12\n"},
		{"a def'd name assigned then called", "def f(x):\n    return x * 2\n\ng = f\nprint(g(21))\n", "42\n"},
		{"an ordinary call is unaffected", "def f(x):\n    return x * 2\n\nprint(f(21))\nprint(f(3))\n", "42\n6\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "fnvalue.gy", tc.src)
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference was expected to answer this program\nsrc: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("the pinned expectation is not the reference's: cpython %q, table %q", py, tc.want)
			}
			// Functions as values are the surface this row grows: the reference answers, and the
			// compiled path either answers it too or refuses naming the missing half (a `def`'d name read
			// as a value is Gap R.167; calling a parameter is L11.7).
			out, code := cliPercent283(t, "--aot", "--file", gy)
			checkCompiledRow(t, out, code, tc.src, tc.want)
			_ = gy
		})
	}
}

// TestArityIsStillAnsweredIsTheLadderRowAtTheCLI: binding the def'd name routed calls through the
// value road, and the value road does not count arguments — `print(f(1, 2))` printed `2` and
// `print(f())` printed `0`, both at exit 0. Silence is the failure; any refusal is the contract.
func TestArityIsStillAnswered(t *testing.T) {
	head := "def f(x):\n    return x * 2\n\n"
	for _, tc := range []struct{ name, src string }{
		{"too many arguments", head + "print(f(1, 2))\n"},
		{"too few arguments", head + "print(f())\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			gy := writeSrc(t, dir, "fnvalue.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliPercent283(t, engine, "--file", gy)
				if code == 0 {
					t.Fatalf("%s answered a wrong-arity call at exit 0 with %q — the un-checked closure road (Gap R.168)", engine, out)
				}
				if !strings.Contains(out, "argument") {
					t.Errorf("%s refused without naming the arity question: %s", engine, out)
				}
			}
		})
	}
}
