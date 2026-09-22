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
	if _, err := CheckSource("def f(:"); err == nil {
		t.Fatalf("expected a parse error")
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
