package integration

import (
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The benchmark harness, end to end, through the pipeline a user's `gustyc --bench` runs.
//
// These two cases used to measure the AST interpreter and the compiled artifact and assert a ratio
// between them. ADR 0302 removed the engine the ratio was taken against, and the claim it carried —
// "the compiler is the product" — does not need it: what a compiled language promises is that the
// program you ship runs far cheaper than the compile that produced it, and that claim is checkable
// against the one backend, directly.

const benchSrc = "s = 0\nfor i in range(20000):\n    s = s + i\nprint(s)\n"

// TestBenchmarkCompiledBackend runs a compute-heavy program through the compiled backend and checks
// the report is a complete one: warm timings, the one-off build timing, and the pipeline profile.
func TestBenchmarkCompiledBackend(t *testing.T) {
	res, err := lang.Benchmark(benchSrc, 3, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.Runs != 3 {
		t.Errorf("Runs = %d, want 3", res.Runs)
	}
	if res.AOT.BestMs <= 0 || res.AOT.MeanMs < res.AOT.BestMs {
		t.Errorf("run timings make no sense: %+v", res.AOT)
	}
	if res.Build.TotalMs <= 0 {
		t.Errorf("the build leg reported no time: the compile cost is part of what --bench owes")
	}
	if len(res.Profile) != 5 {
		t.Errorf("Profile = %v, want the five pipeline phases (parse, analyze, codegen, build, llvm)", res.Profile)
	}
}

// TestBenchmarkSteadyStateCheaperThanCompiling is the claim the benchmark exists to make. A hot loop
// executed by the compiled artifact costs a small fraction of the compilation that produced it, so
// an iteration loop, a REPL turn or a long-running process pays the compiler once and the program
// many times over. If the run ever approaches the build, the optimisation pipeline has stopped
// earning its keep and this case says so.
func TestBenchmarkSteadyStateCheaperThanCompiling(t *testing.T) {
	res, err := lang.Benchmark(benchSrc, 3, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if res.AOT.BestMs >= res.Build.BestMs {
		t.Errorf("the hot loop ran in %v ms against a %v ms build — the artifact is not cheaper to run than it is to compile",
			res.AOT.BestMs, res.Build.BestMs)
	}
}

// TestBenchmarkIsDeterministic guards the property the gate compares baselines on: the same program
// on the same machine produces the same module, so a regression report means the code changed and
// not that the harness drifted.
func TestBenchmarkIsDeterministic(t *testing.T) {
	first, err := lang.Benchmark(benchSrc, 2, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	second, err := lang.Benchmark(benchSrc, 2, 2)
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}
	if len(first.Profile) != len(second.Profile) {
		t.Fatalf("the two runs profiled different phases: %v vs %v", first.Profile, second.Profile)
	}
	for i := range first.Profile {
		if first.Profile[i].Phase != second.Profile[i].Phase {
			t.Errorf("phase %d is %q in one run and %q in the other", i, first.Profile[i].Phase, second.Profile[i].Phase)
		}
	}
	if first.OptLevel != second.OptLevel || first.Runs != second.Runs {
		t.Errorf("the two runs disagree about what was asked of them: %+v vs %+v", first, second)
	}
}
