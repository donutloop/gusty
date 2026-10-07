package lang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- corpus ---------------------------------------------------------------

// TestBenchCorpusLowers asserts every built-in benchmark case is a program both
// backends can actually run: a corpus case that only one backend supports would
// silently stop measuring anything.
func TestBenchCorpusLowers(t *testing.T) {
	corpus := BenchCorpus()
	if len(corpus) < 5 {
		t.Fatalf("corpus too small: %d cases", len(corpus))
	}
	seen := map[string]bool{}
	for _, c := range corpus {
		if seen[c.Name] {
			t.Errorf("duplicate corpus case %q", c.Name)
		}
		seen[c.Name] = true
		if strings.TrimSpace(c.Source) == "" {
			t.Errorf("case %q has no source", c.Name)
			continue
		}
		if _, _, err := evalGolden(t, c.Source); err != nil {
			t.Errorf("case %q fails on the interpreter: %v", c.Name, err)
		}
		res, err := Compile(c.Source)
		if err != nil {
			t.Errorf("case %q fails AOT codegen: %v", c.Name, err)
			continue
		}
		if res.IR == "" {
			t.Errorf("case %q produced empty IR", c.Name)
		}
	}
}

func TestBenchDirLoadsGyFiles(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"b.gy", "a.gy", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("print(1)\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases, err := BenchDir(dir)
	if err != nil {
		t.Fatalf("BenchDir: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("cases = %d, want 2 (.txt ignored)", len(cases))
	}
	// Deterministic order by name.
	if cases[0].Name != "a" || cases[1].Name != "b" {
		t.Errorf("cases not sorted: %v, %v", cases[0].Name, cases[1].Name)
	}
	if _, err := BenchDir(filepath.Join(dir, "nope")); err == nil {
		t.Errorf("expected an error for a missing directory")
	}
}

// --- suite ----------------------------------------------------------------

func TestBenchmarkSuiteReportsEveryCase(t *testing.T) {
	cases := []BenchCase{
		{Name: "b_loop", Source: "s = 0\nfor i in range(2000):\n    s = s + i\nprint(s)\n"},
		{Name: "a_loop", Source: "s = 0\nfor i in range(1000):\n    s = s + i\nprint(s)\n"},
		{Name: "broken", Source: "def f(x: int) -> int:\n    return x\n\nf(\"nope\")\n"},
	}
	s := BenchmarkSuite(cases, 1, 0)
	if s.SchemaVersion != BenchSchemaVersion {
		t.Errorf("schema_version = %q", s.SchemaVersion)
	}
	if len(s.Cases) != 3 {
		t.Fatalf("rows = %d, want 3 (a failed case keeps its row)", len(s.Cases))
	}
	// Sorted by name, deterministically.
	if s.Cases[0].Name != "a_loop" || s.Cases[1].Name != "b_loop" || s.Cases[2].Name != "broken" {
		t.Errorf("rows not sorted by name: %v", []string{s.Cases[0].Name, s.Cases[1].Name, s.Cases[2].Name})
	}
	if s.Cases[0].AOT.BestMs <= 0 {
		t.Errorf("healthy case reported no run timings: %+v", s.Cases[0])
	}
	if s.Cases[0].Build.TotalMs <= 0 {
		t.Errorf("healthy case reported no build timing: %+v", s.Cases[0])
	}
	if s.Cases[2].Error == "" {
		t.Errorf("broken case should record an error")
	}
	if s.Totals.Cases != 3 || s.Totals.Ran != 2 || s.Totals.Failed != 1 {
		t.Errorf("totals = %+v, want 3/2/1", s.Totals)
	}
	if s.Totals.AOTMs <= 0 || s.Totals.BuildMs <= 0 {
		t.Errorf("totals report no measured time: %+v", s.Totals)
	}
	// The artifact must be valid JSON with the documented keys.
	var back map[string]any
	if err := json.Unmarshal([]byte(s.JSON()), &back); err != nil {
		t.Fatalf("suite JSON invalid: %v", err)
	}
	for _, k := range []string{"schema_version", "generated_by", "runs", "opt_level", "cases", "totals"} {
		if _, ok := back[k]; !ok {
			t.Errorf("suite JSON missing %q", k)
		}
	}
}

// --- gate -----------------------------------------------------------------

func syntheticSuite() *BenchSuite {
	return &BenchSuite{
		SchemaVersion: BenchSchemaVersion,
		GeneratedBy:   "test",
		Runs:          3,
		OptLevel:      2,
		Cases: []BenchCaseResult{
			{Name: "hot", AOT: BenchReport{BestMs: 2}, Build: BenchReport{BestMs: 20}},
			{Name: "tiny", AOT: BenchReport{BestMs: 0.05}, Build: BenchReport{BestMs: 5}},
		},
	}
}

func TestCompareBenchSuiteGate(t *testing.T) {
	base := &BenchBaseline{Cases: []BenchBaselineCase{
		{Name: "hot", AOTMs: 2, BuildMs: 20},
		{Name: "tiny", AOTMs: 0.05, BuildMs: 5},
	}}

	// Same numbers -> no regression.
	if r, _ := CompareBenchSuite(syntheticSuite(), base, 1.25, DefaultBenchMinMs, BenchGateAOT); len(r) != 0 {
		t.Fatalf("identical suite reported regressions: %+v", r)
	}

	// A real slowdown on the AOT leg is reported with ratio + suggestion.
	s := syntheticSuite()
	s.Cases[0].AOT.BestMs = 4.0 // 2x slower
	r, _ := CompareBenchSuite(s, base, 1.25, DefaultBenchMinMs, BenchGateAOT)
	if len(r) != 1 {
		t.Fatalf("regressions = %d, want 1: %+v", len(r), r)
	}
	if r[0].Name != "hot" || r[0].Backend != "aot" || r[0].Ratio < 1.9 {
		t.Errorf("unexpected regression: %+v", r[0])
	}
	if r[0].Suggestion == "" || !strings.Contains(r[0].Suggestion, "baseline") {
		t.Errorf("regression must carry an actionable suggestion: %+v", r[0])
	}

	// Within tolerance -> clean.
	s = syntheticSuite()
	s.Cases[0].AOT.BestMs = 2.3 // 1.15x
	if r, _ := CompareBenchSuite(s, base, 1.25, DefaultBenchMinMs, BenchGateAOT); len(r) != 0 {
		t.Errorf("within tolerance reported as regression: %+v", r)
	}

	// Noise floor: a sub-millisecond case doubling is not a regression.
	s = syntheticSuite()
	s.Cases[1].AOT.BestMs = 0.11
	if r, _ := CompareBenchSuite(s, base, 1.25, DefaultBenchMinMs, BenchGateAOT); len(r) != 0 {
		t.Errorf("noise-floor case reported as regression: %+v", r)
	}

	// Missing baseline row -> reported as new, not as a failure.
	s = syntheticSuite()
	s.Cases = append(s.Cases, BenchCaseResult{Name: "brand_new", AOT: BenchReport{BestMs: 1}, Build: BenchReport{BestMs: 5}})
	r, news := CompareBenchSuite(s, base, 1.25, DefaultBenchMinMs, BenchGateAOT)
	if len(r) != 0 {
		t.Errorf("new case reported as regression: %+v", r)
	}
	if len(news) != 1 || news[0].Name != "brand_new" || news[0].Suggestion == "" {
		t.Errorf("new case not reported: %+v", news)
	}

	// A failed case is never compared.
	s = syntheticSuite()
	s.Cases[0].Error = "codegen: unsupported"
	s.Cases[0].AOT.BestMs = 999
	if r, _ := CompareBenchSuite(s, base, 1.25, DefaultBenchMinMs, BenchGateAOT); len(r) != 0 {
		t.Errorf("failed case compared: %+v", r)
	}
}

func TestBenchBaselineRoundTrip(t *testing.T) {
	s := syntheticSuite()
	b := BaselineFromSuite(s)
	if len(b.Cases) != 2 {
		t.Fatalf("baseline cases = %d", len(b.Cases))
	}
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := SaveBenchBaseline(path, b); err != nil {
		t.Fatalf("save: %v", err)
	}
	back, err := LoadBenchBaseline(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if back.Cases[0].Name != "hot" || back.Cases[0].AOTMs != 2 || back.SchemaVersion != BenchSchemaVersion {
		t.Errorf("round trip mismatch: %+v", back)
	}
	if _, err := LoadBenchBaseline(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Errorf("expected error for missing baseline")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	if _, err := LoadBenchBaseline(bad); err == nil {
		t.Errorf("expected error for malformed baseline")
	}
}

// --- real corpus ----------------------------------------------------------

// TestBenchmarkCorpusRuns is the end-to-end check: the shipped corpus measures
// on the compiled backend, and a baseline snapshotted from the same run passes its own
// gate (self-consistency, so the test cannot be flaky on absolute timings).
func TestBenchmarkCorpusRuns(t *testing.T) {
	s := BenchmarkSuite(BenchCorpus(), 1, 1)
	if s.Totals.Ran < len(BenchCorpus()) {
		for _, c := range s.Cases {
			if c.Error != "" {
				t.Errorf("corpus case %q failed: %s", c.Name, c.Error)
			}
		}
		t.Fatalf("only %d of %d corpus cases measured", s.Totals.Ran, len(BenchCorpus()))
	}
	if s.Totals.AOTMs <= 0 || s.Totals.BuildMs <= 0 {
		t.Errorf("totals not measured: %+v", s.Totals)
	}
	base := BaselineFromSuite(s)
	r, newCases := CompareBenchSuite(s, base, DefaultBenchTolerance, DefaultBenchMinMs, BenchGateAOT)
	if len(r) != 0 || len(newCases) != 0 {
		t.Errorf("a suite must pass its own baseline: %+v %+v", r, newCases)
	}
}
