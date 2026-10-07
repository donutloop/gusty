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
// its own calls from libc. the record printed the program's values, the compiled binary
// printed the C library's, and the toolchain reported success.
func TestHostSymbolNamedProgramRuns(t *testing.T) {
	src := readProgramSrc("host_symbol_names")
	want := "1\n2\n3\n4\n5\n6\n7\n28\n6\n7\n"

	recordOut := runCompiled(t, src)
	if recordOut != want {
		t.Errorf("the record leg output =\n%q\nwant\n%q", recordOut, want)
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
