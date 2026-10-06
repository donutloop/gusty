package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A program's stdout is only what the program printed. Echoing the last value is a courtesy for
// snippets typed at the prompt, and the guard for it used to ask the wrong question — "is the final
// statement a bare expression with a non-None value?" — instead of "did this come from a snippet?".
// The result was that `--file` appended `10` to a program ending in `f(5)`, while `--aot` and CPython
// printed nothing: the same source, two stdouts, one program (roadmap Gap R.13, ADR 0204).

// writeProgram puts a program on disk under a name the test chooses.
func writeProgram(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestProgramFileDoesNotEchoLastValue is the fix, stated as the difference between a program and a
// prompt: the same source run as a file owes no bytes, and typed as a snippet owes its value.
func TestProgramFileDoesNotEchoLastValue(t *testing.T) {
	const src = "x = 21\nprint(0)\nx * 2\n"
	path := writeProgram(t, "echo.gy", src)

	// The program's stdout is exactly what the program printed — the closing expression
	// contributes nothing, which is the whole point of ADR 0204.
	if got := cli(t, "--file", path); got != "0\n" {
		t.Errorf("--file output = %q, want the program's own line only", got)
	}
	if got := cli(t, "--aot", path); got != "0\n" {
		t.Errorf("the compiled backend printed something too: %q", got)
	}
	// The other half of the rule, on the same source: asked as a snippet, the caller is owed the
	// value of the expression it typed, and it is presented separately from the program's bytes.
	if got := cli(t, "--eval", src); got != "0\n42\n" {
		t.Errorf("--eval of the same source should echo the value, got %q", got)
	}
}

// TestEchoIsSilentForAValueWhoseKindTheCompilerCannotSee states where the compiled echo stops, so
// that "a program is never echoed" does not quietly become "a snippet is sometimes answered". The
// echo renders through the one str/repr table (ADR 0258); an expression whose kind the compiler
// cannot prove — here, the result of calling a user function, whose return type is a run-time fact —
// has no form in that table yet, and the echo stays silent rather than inventing one. The retired
// engine echoed `10` here, and the record holds that answer; the gap is the tagged value word
// (roadmap L11.1) carrying a value's kind with it, which is also what L11.1's rows name for `print`,
// `str()` and the calling side.
//
// This is an assertion about today, not an acceptance of it: the day the pair can name a form for a
// call's result, the golden record's `10` outranks this row and the case that pins the silence fails.
func TestEchoIsSilentForAValueWhoseKindTheCompilerCannotSee(t *testing.T) {
	const src = "def f(x):\n    return x * 2\n\nf(5)\n"
	if got := cli(t, "--eval", src); got != "" {
		t.Errorf("the echo answered a value the pair cannot name (%q); if the tagged value word landed, delete this row and let the record's `10` be the expectation", got)
	}
	// The program still ran: silence is not refusal.
	out, code := cliCombined(t, "--eval", src)
	if code != 0 {
		t.Fatalf("a snippet ending in a call exited %d: %s", code, out)
	}
}

// TestProgramOutputIsNotAffixed checks the case that matters for scripts and agents: a program that
// prints something and happens to end on a value-returning expression must emit exactly its own
// output, with nothing appended.
func TestProgramOutputIsNotAffixed(t *testing.T) {
	src := "def label(n):\n    return n * 10\n\nprint(1)\nlabel(2)\nprint(3)\n"
	path := writeProgram(t, "affix.gy", src)

	want := "1\n3\n"
	for _, mode := range []string{"--file", "--aot"} {
		if got := cli(t, mode, path); got != want {
			t.Errorf("%s output = %q, want %q", mode, got, want)
		}
	}
	built := cli(t, "--aot", path)
	if built != want {
		t.Errorf("compiled output = %q, want %q", built, want)
	}
	// A pipe must see the same bytes as a file comparison: no trailing surprise.
	if got := cli(t, "--file", path); strings.TrimRight(got, "\n") != "1\n3" {
		t.Errorf("piped output changed shape: %q", got)
	}
}

// TestSnippetStillEchoes is the courtesy the fix must not break: `--eval` is a prompt, and a prompt
// that swallows the value you asked for is worse than the bug being fixed.
func TestSnippetStillEchoes(t *testing.T) {
	if got := cli(t, "--eval", "x = 1 + 2\nx"); got != "3\n" {
		t.Errorf("a snippet stopped echoing its final expression: %q, want 3", got)
	}
	if got := cli(t, "--eval", "1 + 1"); got != "2\n" {
		t.Errorf("a snippet ending in an arithmetic expression was not echoed: %q", got)
	}
	// A snippet whose last statement is a call that returns None echoes nothing, as before.
	if got := cli(t, "--eval", "print(7)"); got != "7\n" {
		t.Errorf("a snippet ending in print() changed shape: %q", got)
	}
}

// TestFileJSONStillReportsTheValueAsMetadata pins the deliberate asymmetry: suppressing the echo is
// about the *program's* stdout, not about hiding information from an agent, so `--json` still reports
// the evaluated result — labelled as what it is, not as program output.
func TestFileJSONStillReportsTheValueAsMetadata(t *testing.T) {
	path := writeProgram(t, "json_echo.gy", "x = 5\nx * 2\n")
	out := cli(t, "--json", "--file", path)
	var doc struct {
		Result  *string `json:"result"`
		Type    string  `json:"type"`
		Backend string  `json:"backend"`
		Exit    int     `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("--json --file did not produce a result document: %v\n%s", err, out)
	}
	if doc.Result == nil || *doc.Result != "10" {
		t.Errorf("the machine path lost the evaluated result: %s", out)
	}
	if doc.Type != "int" {
		t.Errorf("the machine path mislabelled the result's kind: %s", out)
	}
	if doc.Backend != lang.BackendName || doc.Exit != 0 {
		t.Errorf("the machine path lost the backend or the exit code: %s", out)
	}
}
