// Package lang — the gusty language toolchain.
//
// bench.go implements the benchmark + profiling harness: it runs one source program
// through the one execution backend the language has — the LLVM AOT artifact built by
// pkg/lang/codegen.go, lowered by llc and run in-process by pkg/lang/jit_llvm.go — and
// reports structured, machine-readable wall-clock numbers.
//
// Since ADR 0302 there is no second engine to compare against, so the harness measures
// what the compiler actually owns: the pipeline phases (parse / analyze / codegen / llc /
// cc) and the warm execution of the artifact they produced. The interpreter leg used to
// produce a `speedup` ratio; that number was a comparison against a tree-walker, not a
// property of the compiler, and it left with the tree-walker.
package lang

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// BenchReport is the wall-clock summary over the run set. Times are milliseconds;
// BestMs is the fastest single pass, MeanMs is the average, TotalMs is the sum.
type BenchReport struct {
	TotalMs float64 `json:"total_ms"`
	MeanMs  float64 `json:"mean_ms"`
	BestMs  float64 `json:"best_ms"`
}

// PhaseTiming is a single profiling bucket. Benchmark measures the compiler pipeline
// phases once each and reports them, so a user can see where a workload's cost lives:
// in the front end, in the toolchain, or in the code the compiler produced.
type PhaseTiming struct {
	Phase string  `json:"phase"`
	Ms    float64 `json:"ms"`
}

// BenchResult is the structured outcome of benchmarking one source program: the warm
// execution numbers for the compiled artifact, the one-off build numbers, and the
// per-phase profile.
type BenchResult struct {
	Source   string `json:"source"`
	Runs     int    `json:"runs"`
	OptLevel int    `json:"opt_level"`

	// AOT is warm execution of the compiled artifact: the shared object is built once,
	// dlopen'd once, and its `main` called runs times.
	AOT BenchReport `json:"aot"`
	// Build is the one-off cost of producing that artifact (codegen + llc + cc), so a
	// slow compile is visible as data rather than as an unexplained pause (L11.8).
	Build BenchReport `json:"build"`

	Profile     []PhaseTiming `json:"profile"`
	Diagnostics []Diagnostic  `json:"diagnostics"`
}

// Benchmark compiles src and measures wall-clock execution of the compiled artifact.
// The artifact is compiled once, dlopen'd once, and its generated `main` is called runs
// times, so the execution number is warm execution — not build time. The build itself is
// measured separately and reported in Build and Profile.
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

	t0 = time.Now()
	ir, err := GenerateIR(prog)
	codegenMs := msSince(t0)
	if err != nil {
		return res, fmt.Errorf("bench: codegen: %w", err)
	}
	ir = OptimizeIR(ir, optLevel)

	t0 = time.Now()
	a, err := benchBuild(ir)
	buildMs := msSince(t0)
	if err != nil {
		res.Profile = phaseRow(parseMs, analyzeMs, codegenMs)
		return res, err
	}
	res.Build = BenchReport{TotalMs: buildMs, MeanMs: buildMs, BestMs: buildMs}
	res.Profile = append(phaseRow(parseMs, analyzeMs, codegenMs),
		PhaseTiming{Phase: "build", Ms: buildMs},
		PhaseTiming{Phase: "llvm", Ms: buildMs})

	aot, err := benchSO(a.soPath, runs)
	if err != nil {
		return res, err
	}
	res.AOT = aot
	benchCleanup(a.soPath)
	return res, nil
}

func phaseRow(parseMs, analyzeMs, codegenMs float64) []PhaseTiming {
	return []PhaseTiming{
		{Phase: "parse", Ms: parseMs},
		{Phase: "analyze", Ms: analyzeMs},
		{Phase: "codegen", Ms: codegenMs},
	}
}

// benchArtifact is a compiled program kept on disk for the harness: the shared object to
// run, and the intermediate files the measurement was produced from.
type benchArtifact struct {
	soPath string
}

// benchBuild lowers already-generated IR to an object and a shared object (LLVM IR ->
// llc -> cc). It is the compiler's own cost, reported separately from execution so a
// regression in codegen or in the toolchain leg is attributable (L11.8's diagnosability
// rule, applied to the benchmark).
func benchBuild(ir string) (benchArtifact, error) {
	dir, err := os.MkdirTemp("", "gusty-bench-")
	if err != nil {
		return benchArtifact{}, fmt.Errorf("bench: temp dir: %w", err)
	}

	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	soPath := filepath.Join(dir, "jit.so")

	if err := os.WriteFile(irPath, []byte(ir), 0o600); err != nil {
		os.RemoveAll(dir)
		return benchArtifact{}, fmt.Errorf("bench: write IR: %w", err)
	}
	if out, err := runToolStage(ToolBudget, "llc", llcCmd, "-relocation-model=pic", "-filetype=obj", irPath, "-o", objPath); err != nil {
		os.RemoveAll(dir)
		return benchArtifact{}, fmt.Errorf("bench: llc: %v\n%s", err, out)
	}
	if out, err := runToolStage(ToolBudget, "cc", ccCmd, "-shared", "-fPIC", objPath, "-o", soPath); err != nil {
		os.RemoveAll(dir)
		return benchArtifact{}, fmt.Errorf("bench: cc: %v\n%s", err, out)
	}
	return benchArtifact{soPath: soPath}, nil
}

// benchCleanup removes a build's scratch directory. The harness keeps it while it runs
// the artifact and removes it when the measurement is done.
func benchCleanup(soPath string) {
	if soPath == "" {
		return
	}
	os.RemoveAll(filepath.Dir(soPath))
}

// benchAOT compiles prog to a shared object once (LLVM IR -> llc -> cc), loads it once
// via the cgo helper in jit_llvm.go, and calls its generated `main` runs times. The shared
// object is left dlopen'd across all runs so the measurement is warm execution only.
func benchAOT(prog *Program, optLevel, runs int) (BenchReport, error) {
	ir, err := GenerateIR(prog)
	if err != nil {
		return BenchReport{}, fmt.Errorf("bench: codegen: %w", err)
	}
	ir = OptimizeIR(ir, optLevel)
	a, err := benchBuild(ir)
	if err != nil {
		return BenchReport{}, err
	}
	defer benchCleanup(a.soPath)
	return benchSO(a.soPath, runs)
}

func msSince(t0 time.Time) float64 {
	return float64(time.Since(t0).Nanoseconds()) / 1e6
}
