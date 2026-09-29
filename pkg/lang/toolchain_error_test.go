package lang

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Gap R.17 / roadmap L11.8: the failure a compiled run can die by has classes, and the run
// path had one bucket for all of them. `llc` refusing the module *we* emitted is a compiler
// bug and has its own exit code (2); the program trapping is 3; the source not compiling is
// 1. The classification is a typed error now, so callers ask with errors.As instead of
// squinting at a message.

func writeStubLLC(t *testing.T, dir, message string) string {
	t.Helper()
	path := filepath.Join(dir, "stub-llc")
	body := "#!/bin/sh\nprintf '%s\\n' '" + message + "' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	return path
}

func TestJITClassifiesAnLLVMRejection(t *testing.T) {
	dir := t.TempDir()
	stub := writeStubLLC(t, dir, "invalid module: my favourite complaint")
	oldLLC := llcCmd
	llcCmd = stub
	defer func() { llcCmd = oldLLC }()

	_, err := JIT("print(1 + 1)\n", 0)
	if err == nil {
		t.Fatal("JIT succeeded with an llc that always fails")
	}
	var rejection *ToolchainRejectionError
	if !errors.As(err, &rejection) {
		t.Fatalf("an llc refusal must be a *ToolchainRejectionError so the CLI can report the compiler-bug class; got %T: %v", err, err)
	}
	if rejection.Stage != "llc" {
		t.Errorf("stage = %q, want \"llc\"", rejection.Stage)
	}
	if !strings.Contains(rejection.Output, "my favourite complaint") {
		t.Errorf("the tool's own words must survive for diagnostics, got %q", rejection.Output)
	}
	if rejection.Err == nil {
		t.Error("the underlying process error must be kept (Unwrap)")
	}
	// The human-facing text is unchanged from before the typing, because scripts and docs
	// already quote it.
	if !strings.HasPrefix(err.Error(), "jit: llc:") {
		t.Errorf("message = %q, want the established \"jit: llc:\" prefix", err.Error())
	}
}

// A toolchain that is simply not installed is not a compiler bug, and saying so is the
// difference between a reader debugging their language build and debugging a bug that does
// not exist.
func TestAMissingToolchainIsNotReportedAsARejection(t *testing.T) {
	oldLLC := llcCmd
	llcCmd = "gusty-no-such-tool-anything"
	defer func() { llcCmd = oldLLC }()

	_, err := JIT("print(1 + 1)\n", 0)
	if err == nil {
		t.Fatal("JIT succeeded with no llc at all")
	}
	var rejection *ToolchainRejectionError
	if errors.As(err, &rejection) {
		t.Fatalf("a missing tool was reported as \"LLVM rejected our module\": %v", err)
	}
	if !strings.Contains(err.Error(), "could not be run") {
		t.Errorf("a missing tool should be named as such, got %q", err.Error())
	}
}
