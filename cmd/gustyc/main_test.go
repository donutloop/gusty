package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	suiteBinOnce sync.Once
	suiteBin     string
	suiteBinErr  error
)

// cli runs `go run -tags=llvm20 ./cmd/gustyc` with args and returns stdout.
func cli(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("go", "run", "-tags=llvm20", "./cmd/gustyc")
	cmd.Args = append([]string{"go", "run", "-tags=llvm20", "./cmd/gustyc"}, args...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gustyc %v: %v", args, err)
	}
	return string(out)
}

func TestCLIVersion(t *testing.T) {
	if got := cli(t, "--version"); !strings.Contains(got, "gustyc") {
		t.Fatalf("version output = %q", got)
	}
}

func TestCLIEval(t *testing.T) {
	got := cli(t, "--eval", "x = 2 + 3\nx")
	if got != "5\n" {
		t.Fatalf("eval got %q, want 5", got)
	}
}

func TestCLIEvalString(t *testing.T) {
	got := cli(t, "--eval", "s = \"hi\"\ns")
	if got != "hi\n" {
		t.Fatalf("string eval got %q, want hi", got)
	}
}

func TestCLILang(t *testing.T) {
	if got := cli(t, "--lang"); !strings.Contains(got, "statements:") {
		t.Fatalf("--lang output = %q", got)
	}
}

func TestCLIVerify(t *testing.T) {
	got := cli(t, "--verify", "def f(x):\n    return x * 2")
	if got != "ok\n" {
		t.Fatalf("verify got %q, want ok", got)
	}
}

func TestCLIJSON(t *testing.T) {
	got := cli(t, "--json", "--eval", "x = 1 + 2\nx")
	if !strings.Contains(got, `"result": "3"`) {
		t.Fatalf("json eval got %q", got)
	}
}

func TestCLISchema(t *testing.T) {
	got := cli(t, "--schema")
	// The schema must be valid, self-describing JSON (draft-07).
	var v map[string]any
	if err := json.Unmarshal([]byte(got), &v); err != nil {
		t.Fatalf("--schema output is not valid JSON: %v", err)
	}
	if v["$schema"] != "http://json-schema.org/draft-07/schema#" {
		t.Fatalf("schema $schema = %v", v["$schema"])
	}
	if _, ok := v["definitions"]; !ok {
		t.Fatalf("schema has no definitions")
	}
}

func TestCLIEmitLLVMOptLevel(t *testing.T) {
	// --opt-level must come before --emit-llvm (emit-llvm is a string flag
	// that consumes the next argument as its value).
	ir0 := cli(t, "--opt-level=0", "--emit-llvm=\"hi\"")
	ir1 := cli(t, "--opt-level=1", "--emit-llvm=\"hi\"")
	// Level 1 runs the optimization pipeline: the dead string global @.strN
	// (the literal's value is unused by the body) must be pruned.
	if strings.Contains(ir1, "@.str") {
		t.Fatalf("opt-level 1 failed to prune dead string global:\n%s", ir1)
	}
	if !strings.Contains(ir0, "define i32 @main()") {
		t.Fatalf("emit-llvm produced no main():\n%s", ir0)
	}
}

func TestCLIEmitLLVMIndexCallElems(t *testing.T) {
	// Element access into list-producing call expressions in the AOT codegen:
	// keys(), values(), sorted(...), reversed(...), split(...). The emitted IR
	// must contain the exact element constant selected by the literal index.
	cases := []struct{ src, want string }{
		{`print({1: 10, 2: 20}.keys()[0])`, "i32 1"},
		{`print({1: 10, 2: 20}.values()[1])`, "i32 20"},
		{`print(sorted([3, 1, 2])[1])`, "i32 2"},
		{`print(sorted([3, 1, 2], reverse=True)[0])`, "i32 3"},
		{`print(reversed([1, 2, 3])[0])`, "i32 3"},
		{`print("a b c".split()[2])`, "@.str"},
	}
	for _, tc := range cases {
		out := cli(t, "--emit-llvm", tc.src)
		if !strings.Contains(out, tc.want) {
			t.Fatalf("emit-llvm %q: missing %q in IR:\n%s", tc.src, tc.want, out)
		}
	}
}

func TestCLIEmitLLVMScalarMinMax(t *testing.T) {
	// min/max accept a single scalar value (treated as a one-element
	// collection); the emitted IR must contain the scalar itself.
	for _, tc := range []struct{ src, want string }{
		{`print(min(5))`, "i32 5"},
		{`print(max(7))`, "i32 7"},
	} {
		out := cli(t, "--emit-llvm", tc.src)
		if !strings.Contains(out, tc.want) {
			t.Fatalf("emit-llvm %q: missing %q in IR:\n%s", tc.src, tc.want, out)
		}
	}
}

func TestCLIJSONType(t *testing.T) {
	// --json --eval emits a structured result with a dynamic type field.
	out := cli(t, "--json", "--eval=x = 42\nx")
	var doc struct {
		Result string `json:"result"`
		Type   string `json:"type"`
		Exit   int    `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json parse: %v\n%s", err, out)
	}
	if doc.Type != "int" {
		t.Fatalf("type = %q, want int", doc.Type)
	}
	if doc.Exit != 0 {
		t.Fatalf("exit = %d, want 0", doc.Exit)
	}
}

// TestCLIJIT verifies the in-process dlopen JIT path end-to-end through the
// CLI: `--jit --eval` compiles, links, dlopen's, and runs the generated
// native `main`, returning the same stdout as the AST interpreter.
func TestCLIJIT(t *testing.T) {
	got := cli(t, "--jit", "--eval", "x = 6\nprint(x * 7)\n")
	if got != "42\n" {
		t.Fatalf("jit eval output = %q, want 42", got)
	}
}

// TestCLIJITJSON verifies the JSON output mode captures the JIT stdout.
func TestCLIJITJSON(t *testing.T) {
	got := cli(t, "--jit", "--json", "--eval", "print(2 + 3)\n")
	if got != `{"output": "5\n", "exit": 0}`+"\n" {
		t.Fatalf("jit json output = %q", got)
	}
}

// cliExit runs the CLI and returns stdout plus the exit code (0/1/2) without
// failing on a non-zero exit, which check mode uses for its exit contract.
func cliExit(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"run", "-tags=llvm20", "./cmd/gustyc"}, args...)...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	ec := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			ec = ee.ExitCode()
		} else {
			t.Fatalf("cliExit: %v", err)
		}
	}
	return string(out), ec
}

func TestCheckModeExitCodes(t *testing.T) {
	// clean source -> exit 0, "ok" printed
	_, ec := cliExit(t, "--check", "def add(a: int, b: int) -> int:\n    return a + b\nx = add(1, 2)\n")
	if ec != 0 {
		t.Fatalf("clean check exit=%d, want 0", ec)
	}
	// type error -> exit 1
	_, ec = cliExit(t, "--check", "def f() -> int:\n    return \"bad\"\n")
	if ec != 1 {
		t.Fatalf("error check exit=%d, want 1", ec)
	}
	// parse error -> exit 2 from the real binary; `go run` maps it to 1,
	// so assert any non-zero error exit here.
	_, ec = cliExit(t, "--check", "def f(:")
	if ec == 0 {
		t.Fatalf("parse-error check exit=%d, want non-zero", ec)
	}
}

func TestCheckModeJSON(t *testing.T) {
	out, ec := cliExit(t, "--json", "--check", "def f() -> int:\n    return \"bad\"\n")
	if ec != 1 {
		t.Fatalf("json check exit=%d, want 1", ec)
	}
	var res struct {
		Files       []string `json:"files"`
		Diagnostics []struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		} `json:"diagnostics"`
		OK   bool `json:"ok"`
		Exit int  `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal check json: %v\nout=%s", err, out)
	}
	if res.OK || res.Exit != 1 {
		t.Fatalf("expected ok=false exit=1, got ok=%v exit=%d", res.OK, res.Exit)
	}
}

func TestCheckFilesSubcommand(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/bad.gy"
	if err := os.WriteFile(bad, []byte("def greet(name: str) -> str:\n    return name + \"!\"\ny = greet(42)\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, ec := cliExit(t, "--json", "check", bad)
	if ec != 1 {
		t.Fatalf("check files exit=%d, want 1", ec)
	}
	if !strings.Contains(out, "argument") {
		t.Fatalf("expected argument-mismatch diagnostic, out=%s", out)
	}
}

// --- benchmark suite + regression gate (L10.4) -----------------------------

// cliExit runs the CLI and returns (stdout, exit code) without failing, so the
// gate's dedicated exit code can be asserted.

// suiteBinPath builds the CLI once per test binary and returns its path. Exit codes
// must be observed through a real binary: `go run` reports 1 for any failing
// program, which would hide the gate's dedicated exit code.
func suiteBinPath(t *testing.T) string {
	t.Helper()
	suiteBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gusty-cli-")
		if err != nil {
			suiteBinErr = fmt.Errorf("temp dir: %w", err)
			return
		}
		suiteBin = filepath.Join(dir, "gustyc")
		cmd := exec.Command("go", "build", "-tags=llvm20", "-o", suiteBin, "./cmd/gustyc")
		cmd.Dir = "../.."
		if out, err := cmd.CombinedOutput(); err != nil {
			suiteBinErr = fmt.Errorf("build gustyc: %v\n%s", err, out)
		}
	})
	if suiteBinErr != nil {
		t.Fatalf("%v", suiteBinErr)
	}
	return suiteBin
}

// benchCLI runs the built CLI and returns (stdout, exit code).
func benchCLI(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(suiteBinPath(t), args...)
	cmd.Dir = "../.."
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if ok := errors.As(err, &ee); ok {
			return string(out), ee.ExitCode()
		}
		t.Fatalf("gustyc %v: %v", args, err)
	}
	return string(out), 0
}

func TestCLIBenchSuiteJSON(t *testing.T) {
	out, code := benchCLI(t, "--bench-suite", "--bench-runs", "1", "--bench-opt", "1", "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	var rep struct {
		SchemaVersion string `json:"schema_version"`
		Runs          int    `json:"runs"`
		OptLevel      int    `json:"opt_level"`
		Cases         []struct {
			Name        string `json:"name"`
			Interpreter struct {
				BestMs float64 `json:"best_ms"`
			} `json:"interpreter"`
			AOT struct {
				BestMs float64 `json:"best_ms"`
			} `json:"aot"`
			Speedup float64 `json:"speedup"`
			Error   string  `json:"error"`
		} `json:"cases"`
		Totals struct {
			Cases          int     `json:"cases"`
			Ran            int     `json:"ran"`
			Failed         int     `json:"failed"`
			GeomeanSpeedup float64 `json:"geomean_speedup"`
		} `json:"totals"`
		Regressions []any `json:"regressions"`
		NewCases    []any `json:"new_cases"`
		Exit        int   `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("suite is not valid JSON: %v\n%s", err, out)
	}
	if rep.SchemaVersion != "1.0" || rep.Runs != 1 || rep.OptLevel != 1 {
		t.Errorf("suite header wrong: %+v", rep)
	}
	if rep.Totals.Cases == 0 || rep.Totals.Ran != rep.Totals.Cases {
		t.Errorf("not every case measured: %+v", rep.Totals)
	}
	for _, c := range rep.Cases {
		if c.Error != "" {
			t.Errorf("corpus case %q failed: %s", c.Name, c.Error)
		}
		if c.AOT.BestMs <= 0 || c.Interpreter.BestMs <= 0 {
			t.Errorf("case %q reported no timings: %+v", c.Name, c)
		}
	}
	if rep.Exit != 0 {
		t.Errorf("report exit field = %d, want 0", rep.Exit)
	}
}

func TestCLIBenchSuiteBaselineAndGate(t *testing.T) {
	dir := t.TempDir()

	// Measure once and build a synthetic baseline from the observation: every
	// case gets 2x its measured time (so it can never trip the gate), except the
	// heaviest case which gets a near-zero time (so it must trip). That makes the
	// assertion about the *gate*, not about machine noise.
	out, code := benchCLI(t, "--bench-suite", "--bench-runs", "3", "--bench-opt", "2", "--json")
	if code != 0 {
		t.Fatalf("suite run exit = %d\n%s", code, out)
	}
	var measured struct {
		Cases []struct {
			Name string `json:"name"`
			AOT  struct {
				BestMs float64 `json:"best_ms"`
			} `json:"aot"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(out), &measured); err != nil {
		t.Fatalf("suite JSON: %v", err)
	}
	type bc struct {
		Name  string  `json:"name"`
		AOTMs float64 `json:"aot_best_ms"`
	}
	target, best := "", 0.0
	cases := []bc{}
	for _, c := range measured.Cases {
		if c.AOT.BestMs <= 0 {
			continue
		}
		if c.AOT.BestMs > best {
			best, target = c.AOT.BestMs, c.Name
		}
		cases = append(cases, bc{Name: c.Name, AOTMs: c.AOT.BestMs * 2})
	}
	if target == "" {
		t.Skip("no case measured a positive AOT time")
	}
	for i := range cases {
		if cases[i].Name == target {
			cases[i].AOTMs = 0.02
		}
	}
	bad, err := json.Marshal(map[string]any{"schema_version": "1.0", "cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	badPath := dir + "/doctored.json"
	if err := os.WriteFile(badPath, bad, 0o600); err != nil {
		t.Fatal(err)
	}

	got, code := benchCLI(t, "--bench-suite", "--bench-runs", "3", "--bench-opt", "2",
		"--bench-baseline", badPath, "--bench-min-ms", "0.01", "--json")
	if code != exitBenchRegression {
		t.Errorf("doctored baseline exit = %d, want %d (exitBenchRegression)", code, exitBenchRegression)
	}
	var verdict struct {
		Exit        int `json:"exit"`
		Regressions []struct {
			Name       string  `json:"name"`
			Backend    string  `json:"backend"`
			Ratio      float64 `json:"ratio"`
			Suggestion string  `json:"suggestion"`
		} `json:"regressions"`
	}
	if err := json.Unmarshal([]byte(got), &verdict); err != nil {
		t.Fatalf("gate JSON: %v\n%s", err, got)
	}
	if verdict.Exit != exitBenchRegression {
		t.Errorf("report exit field = %d, want %d", verdict.Exit, exitBenchRegression)
	}
	if len(verdict.Regressions) != 1 || verdict.Regressions[0].Name != target {
		t.Fatalf("want exactly one regression on %q, got %+v", target, verdict.Regressions)
	}
	if verdict.Regressions[0].Backend != "aot" || verdict.Regressions[0].Suggestion == "" {
		t.Errorf("regression must name the gated leg and carry a suggestion: %+v", verdict.Regressions[0])
	}

	// A baseline that is generous to every case must pass the gate cleanly. It is
	// built from this run's own measurements (x2) rather than from a second
	// measurement: best-of-N wall clock still swings ~2x for sub-millisecond
	// cases, so a self-vs-self comparison would make the test about scheduler
	// noise instead of about the gate.
	for i := range cases {
		cases[i].AOTMs = measured.Cases[i].AOT.BestMs * 2
	}
	generous, err := json.Marshal(map[string]any{"schema_version": "1.0", "cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	okPath := dir + "/generous.json"
	if err := os.WriteFile(okPath, generous, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := benchCLI(t, "--bench-suite", "--bench-runs", "3", "--bench-opt", "2", "--bench-baseline", okPath, "--bench-min-ms", "0.01"); code != exitOK {
		t.Errorf("a generous baseline should pass the gate, exit = %d\n%s", code, out)
	}

	// --bench-baseline-update writes a well-formed baseline artifact.
	basePath := dir + "/baseline.json"
	if _, code := benchCLI(t, "--bench-suite", "--bench-runs", "1", "--bench-opt", "1", "--bench-baseline-update", basePath); code != 0 {
		t.Fatalf("baseline write exit = %d", code)
	}
	raw, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatalf("baseline not written: %v", err)
	}
	var written struct {
		SchemaVersion string `json:"schema_version"`
		Runs          int    `json:"runs"`
		Cases         []struct {
			Name  string  `json:"name"`
			AOTMs float64 `json:"aot_best_ms"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatalf("baseline JSON malformed: %v\n%s", err, raw)
	}
	if written.SchemaVersion != "1.0" || len(written.Cases) == 0 || written.Runs != 1 {
		t.Errorf("baseline artifact wrong: %+v", written)
	}
	for _, c := range written.Cases {
		if c.AOTMs <= 0 {
			t.Errorf("baseline case %q has no time", c.Name)
		}
	}

	// A missing baseline is a tooling error (1), not a regression.
	if _, code := benchCLI(t, "--bench-suite", "--bench-runs", "1", "--bench-opt", "1", "--bench-baseline", dir+"/nope.json"); code != exitErr {
		t.Errorf("missing baseline exit = %d, want %d", code, exitErr)
	}
}

func TestCLIBenchDir(t *testing.T) {
	dir := t.TempDir()
	prog := dir + "/hot.gy"
	if err := os.WriteFile(prog, []byte("s = 0\nfor i in range(2000):\n    s = s + (i * 7) % 101\nprint(s)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(dir+"/notes.txt", []byte("not a program"), 0o600)
	out, code := benchCLI(t, "--bench-dir", dir, "--bench-runs", "1", "--bench-opt", "1", "--json")
	if code != 0 {
		t.Fatalf("exit = %d\n%s", code, out)
	}
	if !strings.Contains(out, `"name": "hot"`) {
		t.Errorf("directory case not measured:\n%s", out)
	}
	if strings.Contains(out, `"name": "notes"`) {
		t.Errorf("non-.gy file was benchmarked:\n%s", out)
	}
}
