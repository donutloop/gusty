package main

import (
	"os/exec"
	"strings"
	"testing"
)

// repl runs the gustyc REPL with the given stdin and returns combined output.
// Stdin is piped (not a TTY), so no prompts are emitted.
func repl(t *testing.T, input string) string {
	t.Helper()
	cmd := exec.Command("go", "run", "-tags=llvm20", "./cmd/gustyc", "-repl")
	cmd.Dir = "../.."
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("repl failed: %v\n%s", err, out)
	}
	return string(out)
}

// TestREPLSingleLine verifies a single-line input is evaluated immediately.
func TestREPLSingleLine(t *testing.T) {
	out := repl(t, "x = 2\nx\n")
	if !strings.Contains(out, "2") {
		t.Fatalf("expected 2, got %q", out)
	}
}

// TestREPLMultiLineFunction verifies a multi-line function definition is
// accumulated and only evaluated once its indented body is closed by a blank
// line, then the function can be called.
func TestREPLMultiLineFunction(t *testing.T) {
	input := "def f(x):\n    return x * 2\n\nf(21)\n"
	out := repl(t, input)
	if !strings.Contains(out, "42") {
		t.Fatalf("expected 42, got %q", out)
	}
}

// TestREPLMultiLineClass verifies multi-line class + method definition.
func TestREPLMultiLineClass(t *testing.T) {
	input := "class C:\n    def m(self):\n        return 7\n\nc = C()\nc.m()\n"
	out := repl(t, input)
	if !strings.Contains(out, "7") {
		t.Fatalf("expected 7, got %q", out)
	}
}

// TestREPLBlockBodyNotPremature verifies a block with two indented body lines
// is not evaluated after the first body line (it waits for the blank line).
func TestREPLBlockBodyNotPremature(t *testing.T) {
	input := "def g():\n    a = 1\n    return a + 1\n\ng()\n"
	out := repl(t, input)
	if !strings.Contains(out, "2") {
		t.Fatalf("expected 2, got %q", out)
	}
}

// TestREPLContinuesAfterError verifies a bad input reports an error but the
// session continues evaluating the next line.
func TestREPLContinuesAfterError(t *testing.T) {
	input := "1 + )\n1 + 1\n"
	out := repl(t, input)
	if !strings.Contains(out, "2") {
		t.Fatalf("expected session to continue and print 2, got %q", out)
	}
}

// TestREPLIfBlock verifies an if block entered interactively is accumulated.
func TestREPLIfBlock(t *testing.T) {
	input := "if 1 > 0:\n    x = 9\n\nx\n"
	out := repl(t, input)
	if !strings.Contains(out, "9") {
		t.Fatalf("expected 9, got %q", out)
	}
}
