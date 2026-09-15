package main

import (
	"os/exec"
	"strings"
	"testing"
)

// cli runs `go run -tags=llvm20 ./cmd/gustyc` with args and returns stdout.
func cli(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", "run", "-tags=llvm20", "./cmd/gustyc")
	cmd.Args = append([]string{"go", "run", "-tags=llvm20", "./cmd/gustyc"}, args...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gustyc %v: %v", args, err)
	}
	return string(out)
}

func TestCLIVersion(t *testing.T) {
	if got := cli(t, "--version"); !strings.Contains(got, "gustyc") {
		t.Fatalf("version output = %q", got)
	}
}

func TestCLIEval(t *testing.T) {
	got := cli(t, "--eval", "x = 2 + 3\nx")
	if got != "5\n" {
		t.Fatalf("eval got %q, want 5", got)
	}
}

func TestCLIEvalString(t *testing.T) {
	got := cli(t, "--eval", "s = \"hi\"\ns")
	if got != "hi\n" {
		t.Fatalf("string eval got %q, want hi", got)
	}
}

func TestCLILang(t *testing.T) {
	if got := cli(t, "--lang"); !strings.Contains(got, "statements:") {
		t.Fatalf("--lang output = %q", got)
	}
}

func TestCLIVerify(t *testing.T) {
	got := cli(t, "--verify", "def f(x):\n    return x * 2")
	if got != "ok\n" {
		t.Fatalf("verify got %q, want ok", got)
	}
}

func TestCLIJSON(t *testing.T) {
	got := cli(t, "--json", "--eval", "x = 1 + 2\nx")
	if !strings.Contains(got, `"result": "3"`) {
		t.Fatalf("json eval got %q", got)
	}
}
