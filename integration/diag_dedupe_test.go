package integration

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Diagnostic output is an agent-facing artifact, and a list that repeats itself is a list the
// consumer has to clean up before it can be trusted: "how many problems does this program have"
// stops being answerable from the length of the array. The repetition was structural — per-call-site
// return inference re-walks a callee's body once per call site — so a function called twice reported
// its body three times (roadmap Gap R.7, ADR 0202).

// jsonDiagnostic is the shape `--json` publishes for one diagnostic.
type jsonDiagnostic struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Code  string `json:"code"`
	Span  struct {
		Line int `json:"line"`
		Col  int `json:"col"`
	} `json:"span"`
}

func checkJSONDiags(t *testing.T, src string) []jsonDiagnostic {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gustyc")
	buildCLI(t, bin)
	out, _ := exec.Command(bin, "--json", "--check", src).CombinedOutput()
	var doc struct {
		Diagnostics []jsonDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("--json --check did not produce a diagnostic document:\n%s", out)
	}
	return doc.Diagnostics
}

func TestJSONDiagnosticsHaveNoRepeats(t *testing.T) {
	src := `def f(x):
    match x:
        case 1:
            return "one"


print(f(1))
print(f(2))
print(f(3))
`
	seen := map[string]int{}
	for _, d := range checkJSONDiags(t, src) {
		seen[fmt.Sprintf("%s|%s|%d:%d|%s", d.Level, d.Code, d.Span.Line, d.Span.Col, d.Msg)]++
	}
	if len(seen) == 0 {
		t.Fatal("the non-exhaustive match should have been reported at all")
	}
	for k, n := range seen {
		if n > 1 {
			t.Errorf("a diagnostic was repeated %d times in JSON output: %s", n, k)
		}
	}
}

// TestCorpusProgramsPrintNoRepeatedLine checks the human-facing rendering on the programs that
// measurably duplicated before the fix — including the one that used to appear in the roadmap as
// evidence.
func TestCorpusProgramsPrintNoRepeatedLine(t *testing.T) {
	for _, name := range []string{"match_literal", "dispatch_gc", "dispatch_nested", "gc_precise", "dispatch_gc_stress"} {
		bin := filepath.Join(t.TempDir(), "gustyc-"+name)
		buildCLI(t, bin)
		out, _ := exec.Command(bin, "--check", readProgramSrc(name)).CombinedOutput()
		seen := map[string]bool{}
		for _, ln := range strings.Split(string(out), "\n") {
			ln = strings.TrimSpace(ln)
			if ln == "" || ln == "ok" {
				continue
			}
			if seen[ln] {
				t.Errorf("%s printed the same diagnostic twice: %s", name, ln)
			}
			seen[ln] = true
		}
	}
}
