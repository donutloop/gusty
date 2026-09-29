package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestHostSymbolNamedProgramRuns is the compiled leg of the Gap R.4 fix: a program whose
// functions are named after symbols the host ABI owns — `sync`, `write`, `read`, `open`,
// `time`, `exit`, `main` — used to be emitted under those very names, and the linker answered
// its own calls from libc. The interpreter printed the program's values, the compiled binary
// printed the C library's, and the toolchain reported success.
func TestHostSymbolNamedProgramRuns(t *testing.T) {
	src := readProgramSrc("host_symbol_names")
	want := "1\n2\n3\n4\n5\n6\n7\n28\n6\n7\n"

	interp := runInterp(t, src)
	if interp != want {
		t.Errorf("interpreted output =\n%q\nwant\n%q", interp, want)
	}
	aot, err := runAOTWithTimeout(t, src, 120*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if aot != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", aot, want)
	}
	// The oracle is the same one the conformance matrix runs against, so a name the host
	// ABI owns is checked against the language this one imitates rather than a bare
	// `python3` that might not be the pinned version.
	pyOut, pyErr, pyErr2 := lang.PythonRun(src)
	if pyErr2 != nil {
		t.Skipf("no usable oracle: %v\n%s", pyErr2, pyErr)
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}
}

// interpCapturingEval runs the evaluator alone — no front-end gate — and returns what the
// program printed. It exists to pin the difference between a program that does not run and
// a checker that will not look at it.
func interpCapturingEval(t *testing.T, src string) string {
	t.Helper()
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	old := os.Stdout
	r, w, perr := os.Pipe()
	if perr != nil {
		t.Fatalf("pipe: %v", perr)
	}
	os.Stdout = w
	ev := lang.NewEvaluator()
	_, evalErr := ev.EvalProgram(prog)
	os.Stdout = old
	w.Close()
	buf := make([]byte, 1<<20)
	n, _ := r.Read(buf)
	if evalErr != nil {
		t.Fatalf("evaluate: %v", evalErr)
	}
	return string(buf[:n])
}

// TestBuiltBinaryCarriesThePrefixedSymbols looks at the artifact rather than its stdout: the
// program's function must be a defined symbol under the link prefix, and must NOT be defined
// under the name libc already exports. That is the difference between "the program ran and
// printed" and "the program is the thing that ran".
func TestBuiltBinaryCarriesThePrefixedSymbols(t *testing.T) {
	nm, err := exec.LookPath("nm")
	if err != nil {
		t.Skip("nm not installed; the symbol-table leg is skipped")
	}
	res, err := lang.Compile(readProgramSrc("host_symbol_names"))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	dir := t.TempDir()
	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(irPath, []byte(res.IR), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(llc, "-filetype=obj", "-relocation-model=pic", irPath, "-o", objPath).CombinedOutput(); err != nil {
		t.Fatalf("llc rejected the module: %v\n%s", err, out)
	}
	if out, err := exec.Command("cc", "-no-pie", objPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("link failed: %v\n%s", err, out)
	}
	out, err := exec.Command(nm, "-defined-only", binPath).CombinedOutput()
	if err != nil {
		t.Skipf("nm could not read the binary: %v\n%s", err, out)
	}
	defined := strings.Fields(string(out))
	has := func(name string) bool {
		for _, f := range defined {
			if f == name {
				return true
			}
		}
		return false
	}
	// The program owns these; they must appear under the prefix and nowhere else.
	for _, name := range []string{"sync", "write", "read", "open", "time", "exit", "main", "apply_sync"} {
		if !has("gy_" + name) {
			t.Errorf("the binary does not define @gy_%s — the program's function is not the symbol the call can reach:\n%s", name, out)
		}
		if has(name) {
			t.Errorf("the binary defines the bare host name @%s, which is how libc's definition used to answer the program's own call", name)
		}
	}
	// And the executable still runs, which is the point of the whole exercise.
	run, err := exec.Command(binPath).CombinedOutput()
	if err != nil {
		t.Fatalf("running the binary: %v\n%s", err, run)
	}
	if got := string(run); got != "1\n2\n3\n4\n5\n6\n7\n28\n6\n7\n" {
		t.Errorf("binary output =\n%q", got)
	}
}

// TestMethodAndModuleFunctionNameClashIsKnownDebt pins the boundary of this fix: prefixing
// the link names made host-ABI collisions impossible, but the *checker* still keys functions
// by bare name, so a module `def time` and a method `Timer.time` share a key and the module
// call is read against the method's parameters. The refusal belongs to the front end alone:
// CPython runs the program, the evaluator runs it, and so does the module the codegen emits
// for it (roadmap R.8).
func TestMethodAndModuleFunctionNameClashIsKnownDebt(t *testing.T) {
	src := readProgramSrc("probe_method_function_name_clash")
	// The checker refuses it. That refusal is the defect: everything below the front end
	// disagrees with it, including the compiled binary built from this very source.
	prog, err := lang.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var refused string
	for _, d := range lang.Analyze(prog) {
		if d.Level == lang.LevelError {
			refused = d.Msg
		}
	}
	if refused == "" {
		t.Errorf("while R.8 is open the checker is expected to refuse this program; if it no longer does, close R.8 and move the program into the parity corpus")
	} else if !strings.Contains(refused, "undefined name") {
		t.Errorf("expected the refusal to be an undefined-name error, got %q", refused)
	}
	// `Compile` itself emits whatever codegen can lower; it is the build pipeline that
	// refuses to ship a module for a program the front end rejected (ADR 0177). The
	// refusal an agent sees is therefore a diagnostic, not a Go error.
	res, cerr := lang.Compile(src)
	if cerr != nil {
		t.Fatalf("compile: %v", cerr)
	}
	var buildRefused bool
	for _, d := range res.Diagnostics {
		if d.Level == lang.LevelError {
			buildRefused = true
		}
	}
	if !buildRefused {
		t.Errorf("the build is expected to be refused while R.8 is open; if the checker no longer rejects this program, close R.8 and move it into the parity corpus")
	}

	// The evaluator, run directly, executes it exactly as CPython does — the code that
	// runs is fine; what is wrong is that `time(x)` is read as the method
	// `Timer.time(self, x)`, whose `self` is not in scope at the call.
	ev := lang.NewEvaluator()
	_, err = ev.EvalProgram(prog)
	if got := interpCapturingEval(t, src); got != "6\n7\n" {
		t.Errorf("interpreted output = %q, want %q (eval error: %v)", got, "6\n7\n", err)
	}

	// And the sharpest form of the disagreement: the module codegen produces for this
	// program links and runs, printing what CPython prints. Nothing below the checker
	// agrees with the refusal.
	built, berr := runAOTWithTimeout(t, src, 90*time.Second)
	if berr != nil {
		t.Errorf("the emitted module is expected to build and run: %v", berr)
	} else if built != "6\n7\n" {
		t.Errorf("compiled output = %q, want %q", built, "6\n7\n")
	}
}
