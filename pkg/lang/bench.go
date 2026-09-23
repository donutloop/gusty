// Package lang — the gusty language toolchain.
//
// bench.go implements a benchmark + profiling harness that runs one source
// program through both supported execution backends — the AST interpreter
// (pkg/lang/jit.go) and the in-process AOT JIT (pkg/lang/jit_llvm.go) — and
// reports structured, machine-readable wall-clock numbers. It supports the
// "compiler is the product" principle: a user can see, on their own machine,
// how much faster the AOT-compiled artifact is than the interpreter for a
// given workload.
package lang

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// BenchReport is the per-backend wall-clock summary over the run set. Times
// are milliseconds; BestMs is the fastest single pass, MeanMs is the average,
// TotalMs is the sum across all runs.
type BenchReport struct {
	TotalMs float64 `json:"total_ms"`
	MeanMs  float64 `json:"mean_ms"`
	BestMs  float64 `json:"best_ms"`
}

// PhaseTiming is a single profiling bucket. Benchmark measures the interpreter
// pipeline phases (parse / analyze / exec) once and reports them so users can
// see where a workload's cost lives before paying for codegen.
type PhaseTiming struct {
	Phase string  `json:"phase"`
	Ms    float64 `json:"ms"`
}

// BenchResult is the structured outcome of benchmarking one source program
// through both backends. Speedup is interpreterBestMs / aotBestMs — a value
// > 1 means the AOT artifact was faster than the interpreter on its best run.
type BenchResult struct {
	Source      string        `json:"source"`
	Runs        int           `json:"runs"`
	OptLevel    int           `json:"opt_level"`
	Interpreter BenchReport   `json:"interpreter"`
	AOT         BenchReport   `json:"aot"`
	Speedup     float64       `json:"speedup"`
	Profile     []PhaseTiming `json:"profile"`
	Diagnostics []Diagnostic  `json:"diagnostics"`
}

// Benchmark compiles src and measures wall-clock execution on both backends:
// the AST interpreter and the AOT-compiled in-process shared object. The AOT
// artifact is compiled once, dlopen'd once, and its generated `main` is called
// runs times, so the AOT number is warm execution — not build time. The
// interpreter likewise runs the program runs times through fresh evaluators.
func Benchmark(src string, runs, optLevel int) (*BenchResult, error) {
	if runs < 1 {
		runs = 1
	}
	res := &BenchResult{Source: src, Runs: runs, OptLevel: optLevel}

	t0 := time.Now()
	prog, err := parseProgram(src)
	parseMs := msSince(t0)
	if err != nil {
		return res, err
	}

	t0 = time.Now()
	diags := Analyze(prog)
	analyzeMs := msSince(t0)
	if anyErr(diags) {
		res.Diagnostics = diags
		res.Profile = []PhaseTiming{
			{Phase: "parse", Ms: parseMs},
			{Phase: "analyze", Ms: analyzeMs},
		}
		return res, fmt.Errorf("bench: %d error(s) in source", nErrs(diags))
	}

	// One warm interpreter pass for the phase profile.
	ev := NewEvaluator()
	t0 = time.Now()
	ev.EvalProgram(prog) // runtime result is discarded; we only care about timing
	execMs := msSince(t0)
	res.Profile = []PhaseTiming{
		{Phase: "parse", Ms: parseMs},
		{Phase: "analyze", Ms: analyzeMs},
		{Phase: "exec", Ms: execMs},
	}

	res.Interpreter = benchInterpreter(prog, runs)

	aot, err := benchAOT(prog, optLevel, runs)
	if err != nil {
		return res, err
	}
	res.AOT = aot
	if aot.BestMs > 0 {
		res.Speedup = res.Interpreter.BestMs / aot.BestMs
	}
	return res, nil
}

// benchInterpreter runs prog through a fresh AST evaluator runs times.
func benchInterpreter(prog *Program, runs int) BenchReport {
	var total float64
	best := math.Inf(1)
	for i := 0; i < runs; i++ {
		ev := NewEvaluator()
		t0 := time.Now()
		ev.EvalProgram(prog) // result discarded; timing only
		ms := msSince(t0)
		total += ms
		if ms < best {
			best = ms
		}
	}
	if math.IsInf(best, 1) {
		best = 0
	}
	return BenchReport{TotalMs: total, MeanMs: total / float64(runs), BestMs: best}
}

// benchAOT compiles prog to a shared object once (LLVM IR -> llc -> cc), loads
// it once via the cgo helper in jit_llvm.go, and calls its generated `main`
// runs times. The shared object is left dlopen'd across all runs so the
// measurement is warm execution only.
func benchAOT(prog *Program, optLevel, runs int) (BenchReport, error) {
	ir, err := GenerateIR(prog)
	if err != nil {
		return BenchReport{}, fmt.Errorf("bench: codegen: %w", err)
	}
	ir = OptimizeIR(ir, optLevel)

	dir, err := os.MkdirTemp("", "gusty-bench-")
	if err != nil {
		return BenchReport{}, fmt.Errorf("bench: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	soPath := filepath.Join(dir, "jit.so")

	if err := os.WriteFile(irPath, []byte(ir), 0o600); err != nil {
		return BenchReport{}, fmt.Errorf("bench: write IR: %w", err)
	}
	if out, err := exec.Command(llcCmd, "-relocation-model=pic", "-filetype=obj", irPath, "-o", objPath).CombinedOutput(); err != nil {
		return BenchReport{}, fmt.Errorf("bench: llc: %v\n%s", err, out)
	}
	if out, err := exec.Command(ccCmd, "-shared", "-fPIC", objPath, "-o", soPath).CombinedOutput(); err != nil {
		return BenchReport{}, fmt.Errorf("bench: cc: %v\n%s", err, out)
	}

	return benchSO(soPath, runs)
}

func msSince(t0 time.Time) float64 {
	return float64(time.Since(t0).Nanoseconds()) / 1e6
}
