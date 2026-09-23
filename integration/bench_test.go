package integration

import (
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestBenchmarkBackends runs a compute-heavy program through both execution
// backends and asserts both produced sane wall-clock reports, plus a speedup
// ratio and an interpreter phase profile.
func TestBenchmarkBackends(t *testing.T) {
	src := "s = 0\nfor i in range(20000):\n    s = s + i\nprint(s)\n"
	res, err := lang.Benchmark(src, 3, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.Interpreter.BestMs <= 0 {
		t.Errorf("interpreter BestMs = %v, want > 0", res.Interpreter.BestMs)
	}
	if res.AOT.BestMs <= 0 {
		t.Errorf("aot BestMs = %v, want > 0", res.AOT.BestMs)
	}
	if len(res.Profile) != 3 {
		t.Errorf("Profile = %v, want 3 phases", res.Profile)
	}
}

// TestBenchmarkAOTFaster asserts the core "compiler is the product" claim: on
// a hot numeric loop, the AOT-compiled artifact should beat the AST
// interpreter on best-run wall clock.
func TestBenchmarkAOTFaster(t *testing.T) {
	src := "s = 0\nfor i in range(20000):\n    s = s + i\nprint(s)\n"
	res, err := lang.Benchmark(src, 3, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.Speedup <= 1.0 {
		t.Fatalf("Speedup = %v, want > 1.0 (AOT should beat the interpreter on a hot loop)", res.Speedup)
	}
}
