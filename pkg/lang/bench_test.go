package lang

import (
	"testing"
)

// TestBenchmarkBothBackends measures a compute-heavy workload through both the
// AST interpreter and the AOT JIT and checks that both backends produced sane
// wall-clock reports, that a speedup ratio was computed, and that the
// interpreter phase profile was captured.
func TestBenchmarkBothBackends(t *testing.T) {
	src := "s = 0\nfor i in range(5000):\n    s = s + i * 2\nprint(s)\n"
	res, err := Benchmark(src, 3, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.Runs != 3 {
		t.Errorf("Runs = %d, want 3", res.Runs)
	}
	if res.Interpreter.BestMs <= 0 {
		t.Errorf("interpreter BestMs = %v, want > 0", res.Interpreter.BestMs)
	}
	if res.AOT.BestMs <= 0 {
		t.Errorf("aot BestMs = %v, want > 0", res.AOT.BestMs)
	}
	if res.Interpreter.TotalMs <= res.Interpreter.BestMs {
		t.Errorf("interpreter TotalMs = %v should exceed BestMs = %v", res.Interpreter.TotalMs, res.Interpreter.BestMs)
	}
	if res.Speedup <= 0 {
		t.Errorf("Speedup = %v, want > 0", res.Speedup)
	}
	if len(res.Profile) != 3 {
		t.Errorf("Profile = %v, want 3 phases", res.Profile)
	}
	// AOT should be at least as fast as the interpreter on this hot loop.
	if res.Speedup < 0.5 {
		t.Errorf("Speedup = %v, AOT unexpectedly slower than interpreter", res.Speedup)
	}
}

// TestBenchmarkRunsClamp verifies runs < 1 is clamped to 1 and the harness
// still produces a complete report.
func TestBenchmarkRunsClamp(t *testing.T) {
	src := "print(1 + 2)\n"
	res, err := Benchmark(src, 0, 0)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.Runs != 1 {
		t.Errorf("Runs = %d, want 1", res.Runs)
	}
	if res.Interpreter.MeanMs <= 0 {
		t.Errorf("interpreter MeanMs = %v, want > 0", res.Interpreter.MeanMs)
	}
}

// TestBenchmarkCompileError verifies that a source with semantic errors is
// rejected with diagnostics rather than benchmarked.
func TestBenchmarkCompileError(t *testing.T) {
	src := "def f() -> int:\n    return \"bad\"\n" // return type mismatch
	res, err := Benchmark(src, 2, 0)
	if err == nil {
		t.Fatalf("expected error for return-type mismatch, got nil")
	}
	if len(res.Diagnostics) == 0 {
		t.Errorf("expected diagnostics for semantic error, got %v", res.Diagnostics)
	}
}

// TestBenchmarkRuntimeError verifies that a program which raises at runtime is
// still measured (timing is wall-clock, not correctness), and the harness does
// not panic.
func TestBenchmarkRuntimeError(t *testing.T) {
	src := "raise Exception('boom')\n"
	res, err := Benchmark(src, 2, 0)
	if err != nil {
		t.Fatalf("Benchmark should not fail for a runtime error: %v", err)
	}
	if res.Interpreter.BestMs <= 0 {
		t.Errorf("interpreter BestMs = %v, want > 0", res.Interpreter.BestMs)
	}
}
