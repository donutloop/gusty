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

// `def print(x): ...` did not parse at all: `print` and `range` were in the lexer's keyword table,
// so they were keywords rather than identifiers, and a program could not name a function, a
// parameter, a keyword argument or a method after them (roadmap Gap R.9, ADR 0203). A built-in is a
// name a program owns (ADR 0199); only words that change grammar are keywords.

// TestBuiltInNamedDefinitionsAgreeOnEveryPath runs the program that uses those names everywhere.
func TestBuiltInNamedDefinitionsAgreeOnEveryPath(t *testing.T) {
	src := readProgramSrc("builtin_names_as_defs")
	want := "6\n12\n15\n9\n12\n6\n"

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

// TestNamingADefinitionPrintOrRangeIsAcceptedAtTheCLI is the CLI-level form of the gap: these
// programs used to fail before the checker ever ran.
func TestNamingADefinitionPrintOrRangeIsAcceptedAtTheCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	for _, tc := range []struct{ name, src string }{
		{"def print", "def print(x):\n    return x + 1\n\nz = print(2)\n"},
		{"def range", "def range(a):\n    return a * 2\n\nz = range(3)\n"},
		{"def with built-in parameter names", "def f(print, range, len):\n    return print\n\nz = f(1, 2, 3)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _ := exec.Command(bin, "--check", tc.src).CombinedOutput()
			text := string(out)
			if strings.Contains(text, "expected identifier") {
				t.Errorf("%s was rejected as a parse error:\n%s", tc.name, text)
			}
			if strings.Contains(text, "error") {
				t.Errorf("%s earned an error: %s", tc.name, text)
			}
		})
	}

	t.Run("real keywords are still reserved", func(t *testing.T) {
		path := filepath.Join(dir, "kw.gy")
		if err := os.WriteFile(path, []byte("def if(x):\n    return x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		out, _ := exec.Command(bin, "--check", "def if(x):\n    return x\n").CombinedOutput()
		if !strings.Contains(string(out), "expected identifier") {
			t.Errorf("a keyword was accepted as a definition name:\n%s", out)
		}
	})
}
