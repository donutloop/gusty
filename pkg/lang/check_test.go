package lang

import (
	"os"
	"testing"
)

func TestCheckSourceClean(t *testing.T) {
	res, err := CheckSource("def add(a: int, b: int) -> int:\n    return a + b\nx = add(1, 2)\n")
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if !res.OK || res.Exit != 0 {
		t.Fatalf("expected clean check, got ok=%v exit=%d diags=%v", res.OK, res.Exit, res.Diagnostics)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %v", res.Diagnostics)
	}
}

func TestCheckSourceErrors(t *testing.T) {
	res, err := CheckSource("def f() -> int:\n    return \"bad\"\n")
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if res.OK || res.Exit != 1 {
		t.Fatalf("expected error exit, got ok=%v exit=%d", res.OK, res.Exit)
	}
	if !hasErrorMsg(res.Diagnostics, "return type mismatch") {
		t.Fatalf("expected return-mismatch diagnostic, got %v", res.Diagnostics)
	}
}

func TestCheckSourceParseError(t *testing.T) {
	res, err := CheckSource("def f(:")
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if res.OK || res.Exit != 1 {
		t.Fatalf("a parse failure must be reported as failing diagnostics, got ok=%v exit=%d", res.OK, res.Exit)
	}
	if len(res.Diagnostics) == 0 {
		t.Fatalf("no diagnostics for a broken source")
	}
	for _, d := range res.Diagnostics {
		if d.Code != CodeParseError {
			t.Fatalf("diagnostic %q lacks the stable parse code (got %q)", d.Msg, d.Code)
		}
	}
}

// L4.1 — the lexer recovers and the parser reports a forest, so the check
// pipeline must surface *every* recovered error with its span, not one prose
// string. This is the whole point of recovery: a broken document still tells
// you all the places that are broken.
func TestCheckSourceReportsEveryRecoveredError(t *testing.T) {
	res, err := CheckSource("a = 1\nb = $\nc = @\nd = 4\n")
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if res.OK || res.Exit != 1 {
		t.Fatalf("expected failing result, got ok=%v exit=%d", res.OK, res.Exit)
	}
	// The lexer names the offending character precisely; the parser adds its
	// own error for the statement it could not parse.
	var found int
	for _, d := range res.Diagnostics {
		if d.Span.Line == 2 && d.Span.Col == 5 && d.Msg == `unexpected character "$"` {
			found |= 1
		}
		if d.Span.Line == 3 && d.Span.Col == 5 {
			found |= 2
		}
	}
	if found != 3 {
		t.Fatalf("recovered diagnostics are incomplete: %+v", res.Diagnostics)
	}
}

// Statements that DO parse must still be checked: recovery is not "stop at the
// first bad line".
func TestCheckSourceChecksWhatParsed(t *testing.T) {
	res, err := CheckSource("a = 1\nb = $\ndef f(x: int) -> int:\n    return \"s\"\n")
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if !hasErrorMsg(res.Diagnostics, "return type mismatch") {
		t.Fatalf("the statement after the broken line was not checked: %+v", res.Diagnostics)
	}
}

func TestCheckFilesAggregates(t *testing.T) {
	dir := t.TempDir()
	good := dir + "/good.gy"
	bad := dir + "/bad.gy"
	writeFile(t, good, "def add(a: int, b: int) -> int:\n    return a + b\nx = add(1, 2)\n")
	writeFile(t, bad, "def greet(name: str) -> str:\n    return name + \"!\"\ny = greet(42)\n")
	res, err := CheckFiles([]string{good, bad})
	if err != nil {
		t.Fatalf("CheckFiles: %v", err)
	}
	if res.OK || res.Exit != 1 {
		t.Fatalf("expected error exit across files, got ok=%v exit=%d", res.OK, res.Exit)
	}
	if len(res.Files) != 2 {
		t.Fatalf("expected 2 files, got %v", res.Files)
	}
	if !hasErrorMsg(res.Diagnostics, "argument") {
		t.Fatalf("expected argument-mismatch from bad.gy, got %v", res.Diagnostics)
	}
}

func writeFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
