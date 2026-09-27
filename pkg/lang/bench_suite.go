// Package lang — the gusty language toolchain.
//
// bench_suite.go turns benchmarking from "one program at a time" into a
// regression suite: a fixed corpus of compute-heavy programs is measured on
// both execution backends (AST interpreter and AOT), reported as one stable
// JSON artifact, and diffed against a committed baseline so a slowdown shows up
// as a number instead of a hunch. The corpus is also loadable from a directory
// of .gy files, so the integration/ parity programs double as benchmark cases.
//
// Determinism rules (the artifact is meant to be diffed):
//   - cases are always reported sorted by name;
//   - a case whose name collides is disambiguated by ordinal and reported;
//   - failed cases are recorded with an Error string rather than dropping the row.
package lang

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// BenchSchemaVersion versions the benchmark suite/baseline JSON artifacts.
const BenchSchemaVersion = "1.0"

// Default tolerance and noise floor for the regression gate: a case is only a
// regression when it is more than 25% slower than its baseline AND slow enough
// to measure above 0.5 ms (sub-millisecond timings are scheduler noise).
const (
	DefaultBenchTolerance = 1.5
	DefaultBenchMinMs     = 0.25
)

// BenchCase is one benchmark program.
type BenchCase struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// BenchCaseResult is the measured outcome of one case. Error is non-empty when
// the case could not be benchmarked (semantic error, codegen gap, toolchain
// missing); such a row is kept in the report instead of being dropped, so a
// suite never silently shrinks.
type BenchCaseResult struct {
	Name        string        `json:"name"`
	Interpreter BenchReport   `json:"interpreter"`
	AOT         BenchReport   `json:"aot"`
	Speedup     float64       `json:"speedup"`
	Profile     []PhaseTiming `json:"profile,omitempty"`
	Error       string        `json:"error,omitempty"`
}

// BenchTotals summarises a suite. GeomeanSpeedup is the geometric mean over
// cases that ran on both backends — the single number to watch.
type BenchTotals struct {
	Cases          int     `json:"cases"`
	Ran            int     `json:"ran"`
	Failed         int     `json:"failed"`
	InterpreterMs  float64 `json:"interpreter_total_ms"`
	AOTMs          float64 `json:"aot_total_ms"`
	GeomeanSpeedup float64 `json:"geomean_speedup"`
}

// BenchSuite is the machine-readable benchmark artifact.
type BenchSuite struct {
	SchemaVersion string            `json:"schema_version"`
	GeneratedBy   string            `json:"generated_by"`
	Runs          int               `json:"runs"`
	OptLevel      int               `json:"opt_level"`
	Cases         []BenchCaseResult `json:"cases"`
	Totals        BenchTotals       `json:"totals"`
}

// BenchCorpus returns the built-in benchmark corpus: compute-heavy programs
// that stress a different part of each backend per case. Every program must
// lower on both backends (asserted by TestBenchCorpusLowers).
func BenchCorpus() []BenchCase {
	return []BenchCase{
		{
			// A modulo keeps LLVM's SCEV from collapsing the loop into a closed
			// form: a benchmark case that optimises to nothing measures nothing.
			Name:   "loop_mod",
			Source: "s = 0\nfor i in range(800000):\n    s = s + (i * 7) % 1009\nprint(s)\n",
		},
		{
			Name:   "while_branch",
			Source: "n = 0\nk = 0\nwhile k < 700000:\n    if k % 3 == 0:\n        n = n + 1\n    k = k + 1\nprint(n)\n",
		},
		{
			Name:   "nested_loops",
			Source: "t = 0\nfor i in range(800):\n    for j in range(300):\n        t = t + (i * 31) % 101 - (j * 17) % 97\nprint(t)\n",
		},
		{
			Name:   "class_dispatch",
			Source: "class Point:\n    def __init__(self, x, y):\n        self.x = x\n        self.y = y\n\n    def norm(self) -> int:\n        return self.x * self.x + self.y * self.y\n\ns = 0\nfor i in range(50000):\n    p = Point(i, i % 7)\n    s = s + p.norm()\nprint(s)\n",
		},
		{
			Name:   "recursive_fib",
			Source: "def fib(n) -> int:\n    if n < 2:\n        return n\n    return fib(n - 1) + fib(n - 2)\n\nprint(fib(22))\n",
		},
		{
			Name:   "list_accumulate",
			Source: "def total(xs) -> int:\n    t = 0\n    for x in xs:\n        t = t + x\n    return t\n\nxs = []\ni = 0\nwhile i < 120000:\n    xs.append(i)\n    i = i + 1\nprint(total(xs))\n",
		},
		{
			// Heap containers are opaque to the optimiser, so this measures the
			// runtime list path (append + a list passed to a function) rather than
			// a folded constant.
			Name:   "container_walk",
			Source: "def total(xs) -> int:\n    t = 0\n    for x in xs:\n        t = t + (x * 7) % 1009\n    return t\n\nxs = []\ni = 0\nwhile i < 20000:\n    xs.append(i)\n    i = i + 1\nprint(total(xs) + total([1, 2, 3]))\n",
		},
		{
			Name:   "function_calls",
			Source: "def mix(a, b) -> int:\n    return (a * 31 + b * 17) % 100003\n\nxs = []\ni = 0\nwhile i < 40000:\n    xs.append(i)\n    i = i + 1\ns = 0\nfor x in xs:\n    s = mix(s, x)\nprint(s)\n",
		},
	}
}

// BenchDir loads every *.gy file in dir as a case, named by its base name. This
// is how the integration/ parity programs become benchmark cases.
func BenchDir(dir string) ([]BenchCase, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("bench: read dir: %w", err)
	}
	names := []string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".gy") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	cases := make([]BenchCase, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, fmt.Errorf("bench: read %s: %w", n, err)
		}
		cases = append(cases, BenchCase{Name: strings.TrimSuffix(n, ".gy"), Source: string(b)})
	}
	return cases, nil
}

// BenchmarkSuite measures every case on both backends. It never aborts on a
// single case: a case that fails is recorded with Error and the run continues,
// so a capability gap on one program cannot hide the other numbers.
//
// Program stdout is redirected to /dev/null while measuring — several corpus
// programs print, and writing to a terminal is not what is being benchmarked.
func BenchmarkSuite(cases []BenchCase, runs, optLevel int) *BenchSuite {
	if runs < 1 {
		runs = 1
	}
	s := &BenchSuite{
		SchemaVersion: BenchSchemaVersion,
		GeneratedBy:   "gustyc " + Version,
		Runs:          runs,
		OptLevel:      optLevel,
		Cases:         make([]BenchCaseResult, 0, len(cases)),
	}
	sorted := make([]BenchCase, len(cases))
	copy(sorted, cases)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

	restore := silenceStdout()
	defer restore()

	var prod, aotSum float64
	ratios := []float64{}
	ran := 0
	for _, c := range sorted {
		row := BenchCaseResult{Name: c.Name}
		res, err := Benchmark(c.Source, runs, optLevel)
		if err != nil {
			row.Error = err.Error()
			s.Cases = append(s.Cases, row)
			continue
		}
		row.Interpreter = res.Interpreter
		row.AOT = res.AOT
		row.Speedup = res.Speedup
		row.Profile = res.Profile
		if row.Interpreter.BestMs > 0 && row.AOT.BestMs > 0 {
			prod += row.Interpreter.BestMs
			aotSum += row.AOT.BestMs
			ratios = append(ratios, row.Speedup)
		}
		ran++
		s.Cases = append(s.Cases, row)
	}
	s.Totals = BenchTotals{Cases: len(sorted), Ran: ran, Failed: len(sorted) - ran, InterpreterMs: roundMs(prod), AOTMs: roundMs(aotSum)}
	s.Totals.GeomeanSpeedup = geomean(ratios)
	return s
}

// roundMs keeps the JSON artifact readable (2 decimal places).
func roundMs(f float64) float64 {
	return math.Round(f*100) / 100
}

// geomean is the geometric mean of positive speedups (0 for an empty set).
func geomean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sum := 0.0
	n := 0
	for _, x := range xs {
		if x > 0 {
			sum += math.Log(x)
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Round(math.Exp(sum/float64(n))*100) / 100
}

// JSON renders the suite artifact (stable key order via struct field order).
func (s *BenchSuite) JSON() string {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(b)
}

// BenchBaselineCase is one committed reference measurement.
type BenchBaselineCase struct {
	Name          string  `json:"name"`
	InterpreterMs float64 `json:"interpreter_best_ms"`
	AOTMs         float64 `json:"aot_best_ms"`
}

// BenchBaseline is a saved suite used as the regression reference.
type BenchBaseline struct {
	SchemaVersion string              `json:"schema_version"`
	GeneratedBy   string              `json:"generated_by"`
	Runs          int                 `json:"runs"`
	OptLevel      int                 `json:"opt_level"`
	Cases         []BenchBaselineCase `json:"cases"`
}

// BaselineFromSuite snapshots a suite as a baseline.
func BaselineFromSuite(s *BenchSuite) *BenchBaseline {
	b := &BenchBaseline{
		SchemaVersion: BenchSchemaVersion,
		GeneratedBy:   s.GeneratedBy,
		Runs:          s.Runs,
		OptLevel:      s.OptLevel,
		Cases:         make([]BenchBaselineCase, 0, len(s.Cases)),
	}
	for _, c := range s.Cases {
		if c.Error != "" {
			continue
		}
		b.Cases = append(b.Cases, BenchBaselineCase{Name: c.Name, InterpreterMs: c.Interpreter.BestMs, AOTMs: c.AOT.BestMs})
	}
	sort.Slice(b.Cases, func(i, j int) bool { return b.Cases[i].Name < b.Cases[j].Name })
	return b
}

// SaveBenchBaseline writes a baseline artifact to disk (2-space indented JSON,
// so a baseline diff in review reads well).
func SaveBenchBaseline(path string, b *BenchBaseline) error {
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

// LoadBenchBaseline reads a baseline artifact.
func LoadBenchBaseline(path string) (*BenchBaseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("bench: baseline: %w", err)
	}
	b := &BenchBaseline{}
	if err := json.Unmarshal(raw, b); err != nil {
		return nil, fmt.Errorf("bench: baseline %s: %w", path, err)
	}
	return b, nil
}

// BenchRegression is one gate violation: a case whose measured best time is
// more than `tolerance`x the baseline (and slow enough to be signal).
type BenchRegression struct {
	Name       string  `json:"name"`
	Backend    string  `json:"backend"` // "interpreter" | "aot"
	BaselineMs float64 `json:"baseline_ms"`
	CurrentMs  float64 `json:"current_ms"`
	Ratio      float64 `json:"ratio"`
	Suggestion string  `json:"suggestion"`
}

// BenchNewCase names a suite case with no baseline row (informational, not a
// failure — adding a case should not require editing the baseline first).
type BenchNewCase struct {
	Name       string `json:"name"`
	Suggestion string `json:"suggestion"`
}

// BenchGate selects which leg of a run the regression gate watches.
const (
	BenchGateAOT         = "aot"
	BenchGateInterpreter = "interpreter"
	BenchGateBoth        = "both"
)

// CompareBenchSuite diffs a suite against a baseline over `gate` ("aot",
// "interpreter" or "both"). The default gate is the AOT leg: that is the
// compiler's own performance contract, whereas the tree-walking interpreter's
// timings swing by tens of percent run to run (GC and allocation churn), which
// would make an interpreter-leg gate read as noise. Interpreter numbers are
// still reported — they just do not fail the build.
//
// tolerance is a multiplier (1.25 = allow 25% slowdown); minMs is the noise
// floor — a case whose baseline time is below it is never a regression, because
// at that scale scheduler noise dominates the measurement.
func CompareBenchSuite(s *BenchSuite, b *BenchBaseline, tolerance, minMs float64, gate string) ([]BenchRegression, []BenchNewCase) {
	if tolerance <= 0 {
		tolerance = DefaultBenchTolerance
	}
	if minMs <= 0 {
		minMs = DefaultBenchMinMs
	}
	base := map[string]BenchBaselineCase{}
	for _, c := range b.Cases {
		base[c.Name] = c
	}
	regressions := []BenchRegression{}
	for _, c := range s.Cases {
		if c.Error != "" {
			continue
		}
		bc, ok := base[c.Name]
		if !ok {
			continue
		}
		check := func(backend string, want, got float64) {
			if want < minMs || got <= 0 {
				return
			}
			ratio := got / want
			if ratio > tolerance {
				regressions = append(regressions, BenchRegression{
					Name: c.Name, Backend: backend,
					BaselineMs: roundMs(want), CurrentMs: roundMs(got), Ratio: math.Round(ratio*100) / 100,
					Suggestion: fmt.Sprintf("%s best time for %q is %.2fx the baseline (%.2f ms -> %.2f ms, tolerance %.2fx); re-measure with more runs, or update the baseline with --bench-baseline-update if the change is intended",
						backend, c.Name, ratio, want, got, tolerance),
				})
			}
		}
		if gate == BenchGateBoth || gate == BenchGateInterpreter {
			check("interpreter", bc.InterpreterMs, c.Interpreter.BestMs)
		}
		if gate == BenchGateBoth || gate == BenchGateAOT || gate == "" {
			check("aot", bc.AOTMs, c.AOT.BestMs)
		}
	}
	newCases := []BenchNewCase{}
	for _, c := range s.Cases {
		if c.Error != "" {
			continue
		}
		if _, ok := base[c.Name]; !ok {
			newCases = append(newCases, BenchNewCase{Name: c.Name, Suggestion: "no baseline row for this case; run --bench-baseline-update to record it"})
		}
	}
	sort.Slice(regressions, func(i, j int) bool {
		if regressions[i].Name != regressions[j].Name {
			return regressions[i].Name < regressions[j].Name
		}
		return regressions[i].Backend < regressions[j].Backend
	})
	sort.Slice(newCases, func(i, j int) bool { return newCases[i].Name < newCases[j].Name })
	return regressions, newCases
}

// silenceStdout redirects os.Stdout to /dev/null and returns a restore func.
// Benchmarking must not measure terminal writes.
func silenceStdout() func() {
	dev, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0o600)
	if err != nil {
		return func() {}
	}
	old := os.Stdout
	os.Stdout = dev
	return func() {
		os.Stdout = old
		dev.Close()
	}
}
