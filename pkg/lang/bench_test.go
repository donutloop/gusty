package lang

import (
	"testing"
)

// TestBenchmarkCompiledBackend measures a compute-heavy workload through the one backend and
// checks the report is complete: warm run timings, the one-off build timing, and the pipeline
// phase profile the harness exists to expose.
//
// It used to assert a `speedup` against the AST interpreter and that the compiled artifact beat
// it by some factor. ADR 0302 removed the engine the ratio was measured against; what the
// compiler is responsible for is the two numbers below, both of which are now asserted directly
// rather than inferred from a ratio.
func TestBenchmarkCompiledBackend(t *testing.T) {
	src := "s = 0\nfor i in range(5000):\n    s = s + i * 2\nprint(s)\n"
	res, err := Benchmark(src, 3, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.Runs != 3 {
		t.Errorf("Runs = %d, want 3", res.Runs)
	}
	if res.AOT.BestMs <= 0 {
		t.Errorf("AOT BestMs = %v, want > 0", res.AOT.BestMs)
	}
	if res.AOT.TotalMs <= res.AOT.BestMs {
		t.Errorf("AOT TotalMs = %v should exceed BestMs = %v", res.AOT.TotalMs, res.AOT.BestMs)
	}
	if res.Build.TotalMs <= 0 {
		t.Errorf("Build TotalMs = %v, want > 0 (the compile cost is part of the report)", res.Build.TotalMs)
	}
	want := []string{"parse", "analyze", "codegen", "build", "llvm"}
	if len(res.Profile) != len(want) {
		t.Fatalf("Profile = %v, want the %d pipeline phases %v", res.Profile, len(want), want)
	}
	for i, p := range res.Profile {
		if p.Phase != want[i] {
			t.Errorf("Profile[%d].Phase = %q, want %q", i, p.Phase, want[i])
		}
		if p.Ms < 0 {
			t.Errorf("Profile[%d].Ms = %v, want >= 0", i, p.Ms)
		}
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
	if res.AOT.MeanMs <= 0 {
		t.Errorf("AOT MeanMs = %v, want > 0", res.AOT.MeanMs)
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

// TestBenchmarkRuntimeError verifies that a program which raises at run time is still measured
// (timing is wall-clock, not correctness), and the harness does not panic.
func TestBenchmarkRuntimeError(t *testing.T) {
	src := "raise Exception('boom')\n"
	res, err := Benchmark(src, 2, 0)
	if err != nil {
		t.Fatalf("Benchmark should not fail for a runtime error: %v", err)
	}
	if res.AOT.BestMs <= 0 {
		t.Errorf("AOT BestMs = %v, want > 0", res.AOT.BestMs)
	}
}
