//go:build !windows

package lang

import (
	"testing"
)

// TestJITBasic verifies the in-process dlopen JIT runs real machine code:
// a source program's generated `main` is compiled, linked, dlopen'd, and
// invoked, and its printf output is captured back into Go.
func TestJITBasic(t *testing.T) {
	res, err := JIT("x = 6\nprint(x * 7)\n", 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "42\n" {
		t.Fatalf("output = %q, want 42", res.Output)
	}
	if res.IR == "" {
		t.Fatal("expected emitted IR")
	}
	if len(res.Commands) != 2 {
		t.Fatalf("expected llc+cc commands, got %v", res.Commands)
	}
}

// TestJITFloat verifies float arithmetic survives the native path.
func TestJITFloat(t *testing.T) {
	res, err := JIT("x = 16.0\nprint(sqrt(x))\n", 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "4\n" {
		t.Fatalf("output = %q, want 4", res.Output)
	}
}

// TestJITLoop verifies control flow (for loop over range) executes natively.
func TestJITLoop(t *testing.T) {
	res, err := JIT("s = 0\nfor i in range(3):\n    s = s + i\nprint(s)\n", 0)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "3\n" {
		t.Fatalf("output = %q, want 3", res.Output)
	}
}

// TestJITOptimized verifies the optimizer path (opt-level 2) still runs.
func TestJITOptimized(t *testing.T) {
	res, err := JIT("print(2 + 3)\n", 2)
	if err != nil {
		t.Fatalf("JIT failed: %v", err)
	}
	if res.Output != "5\n" {
		t.Fatalf("output = %q, want 5", res.Output)
	}
}

// TestJITError reports a clean error (semantic or native-codegen) rather than
// crashing or emitting partial output.
func TestJITError(t *testing.T) {
	_, err := JIT("print(undefined_thing)\n", 0)
	if err == nil {
		t.Fatal("expected an error for an undefined name")
	}
}
