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

// TestMissingArgumentIsRefusedWhereItShouldBe pins what Gap R.10 (ADR 0201) changed, and the
// gates that decide it.
//
// A dropped argument used to be invisible to the checker: too many arguments was reported, too
// few was not, so the parameter simply arrived unbound and the program was blamed later — an
// undefined-name error pointing inside the function that was called — or answered differently by
// the two backends. The interpreter already caught it at run time; the fix is that the *compiler*
// now says so, at the call, naming the function.
func TestMissingArgumentIsRefusedWhereItShouldBe(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc-test")
	src := `def build(a, b):
    return a + 1


print(build(1))
`
	path := filepath.Join(dir, "prog.gy")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	buildCLI(t, bin)

	t.Run("check refuses at the call", func(t *testing.T) {
		// `--check` takes source text (the same inline form an agent already uses); only the
		// build paths take a filename.
		out, _ := exec.Command(bin, "--check", src).CombinedOutput()
		text := string(out)
		if !strings.Contains(text, `function "build" expects 2 arguments, got 1`) {
			t.Fatalf("--check did not refuse the dropped argument:\n%s", text)
		}
		// The blame belongs at the call site, nowhere inside the callee.
		for _, ln := range strings.Split(text, "\n") {
			if strings.Contains(ln, "error") && strings.Contains(ln, "line 2") {
				t.Errorf("the callee's own body was blamed for the caller's mistake: %s", ln)
			}
		}
	})

	t.Run("compile refuses to emit a module for it", func(t *testing.T) {
		out, err := exec.Command(bin, "--aot", path).CombinedOutput()
		if err == nil {
			t.Fatalf("--aot accepted a call with a missing argument:\n%s", out)
		}
		if !strings.Contains(string(out), "error") {
			t.Errorf("--aot failed without an error message:\n%s", out)
		}
	})

	// The same refusal, asked of the plain `--file` path. It used to be the second engine's turn here,
	// and the sentence it wrote was its own; the checker owns arity now, and it words the finding as
	// `function "build" expects 2 arguments, got 1` — which names the callee, the expected count and
	// the count it saw. Requiring one engine's phrasing from the other would be a test of vocabulary.
	t.Run("the one backend refuses it too, on the file path", func(t *testing.T) {
		out, err := exec.Command(bin, "--file", path).CombinedOutput()
		text := string(out)
		if err == nil {
			t.Fatalf("--file ran a program with a missing argument and said nothing:\n%s", text)
		}
		if !strings.Contains(text, "expects") && !strings.Contains(text, "argument") {
			t.Errorf("the refusal should name the missing argument (callee, expected, given):\n%s", text)
		}
	})
}

// TestArgumentsAndDefaultsAgreeOnEveryPath runs the legitimate short-call shapes through all three
// engines: the new rule must refuse mistakes, not defaults.
func TestArgumentsAndDefaultsAgreeOnEveryPath(t *testing.T) {
	src := readProgramSrc("arity_defaults")
	want := "11\n6\n8\n13\n6\n33\n7\n31\n11\n14\n42\n"

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

// TestKeywordArityShapesAgree covers the keyword half of the contract, where an unfilled parameter
// is a skipped name rather than a short argument list.
func TestKeywordArityShapesAgree(t *testing.T) {
	src := `def label(a, b=2, c=3):
    return a * 100 + b * 10 + c


print(label(1))
print(label(1, 5))
print(label(c=9, a=1))
print(label(b=4, c=5, a=2))
`
	want := "123\n153\n129\n245\n"
	lang.RecordedStdoutIs(t, src, want)
	built, err := runAOTWithTimeout(t, src, 90*time.Second)
	if err != nil {
		t.Fatalf("compiled run: %v", err)
	}
	if built != want {
		t.Errorf("compiled output =\n%q\nwant\n%q", built, want)
	}
	pyOut, _, perr := lang.PythonRun(src)
	if perr != nil {
		t.Skip("no usable oracle")
	}
	if pyOut != want {
		t.Errorf("CPython output =\n%q\nwant\n%q", pyOut, want)
	}

	// And the skipped-name case is refused, by name, before anything runs.
	bad := `def label(a, b, c):
    return a


print(label(a=1))
`
	diag := false
	prog, err := lang.Parse(bad)
	if err == nil {
		for _, d := range lang.Analyze(prog) {
			if d.Level == lang.LevelError && strings.Contains(d.Msg, `missing argument "b"`) {
				diag = true
			}
		}
	}
	if !diag {
		t.Errorf("a keyword call that skipped a parameter was not named: %v", prog)
	}
}
