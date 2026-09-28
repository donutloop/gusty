package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
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

// TestCLIJITJSON verifies the JSON output mode captures the JIT stdout, and names
// the backend that produced it (Gap M.2: an agent that asked for AOT must be able
// to see that it got AOT rather than inferring it from the flag list).
func TestCLIJITJSON(t *testing.T) {
	got := cli(t, "--jit", "--json", "--eval", "print(2 + 3)\n")
	if got != `{"output": "5\n", "backend": "aot", "exit": 0}`+"\n" {
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

// benchCLICombined is benchCLI with stderr merged, for assertions about what the
// human sees: diagnostics and the failure line both go to stderr.
func benchCLICombined(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(suiteBinPath(t), args...)
	cmd.Dir = "../.."
	out, err := cmd.CombinedOutput()
	if err != nil {
		var ee *exec.ExitError
		if ok := errors.As(err, &ee); ok {
			return string(out), ee.ExitCode()
		}
		t.Fatalf("gustyc %v: %v", args, err)
	}
	return string(out), 0
}

// TestCLIBuildFailureStatesItsReason (roadmap Gap K.7): a build that fails while the
// program also carries warnings used to print the warnings and exit 1 without ever
// saying what failed — the error was only reachable through --json.
func TestCLIBuildFailureStatesItsReason(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/prog.gy"
	// A warning (a non-exhaustive match) plus a codegen refusal (unsupported call). The
	// previous source mixed in `1 + "a"`, which is now refused by codegen as well, so the
	// warning and the failure would no longer have been independent facts.
	if err := os.WriteFile(src, []byte("n = 3\nmatch n:\n    case 1:\n        print(\"one\")\nprint(enumerate([1, 2]))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := benchCLICombined(t, "--build="+dir+"/prog", src)
	if code == exitOK {
		t.Fatalf("the build should fail (unsupported call)\n%s", out)
	}
	if !strings.Contains(out, "warning at") {
		t.Errorf("the source warning should still be shown:\n%s", out)
	}
	if !strings.Contains(out, "gustyc:") || !strings.Contains(out, "unsupported call") {
		t.Errorf("the failure reason must be printed alongside diagnostics, got:\n%s", out)
	}
	// The stage name appears once: GenerateIR's message already says "codegen:", so
	// Build must not add a second one.
	if strings.Contains(out, "codegen: codegen:") {
		t.Errorf("the failure line repeats its stage prefix:\n%s", out)
	}
}

// TestCLIBuildFailureJSONCarriesDiagnosticsOnFailure is the machine half: the partial
// BuildResult must travel with the error, so a script sees the diagnostics and the
// reason together.
func TestCLIBuildFailureJSONCarriesDiagnosticsOnFailure(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/prog.gy"
	if err := os.WriteFile(src, []byte("x = 1 + \"a\"\nprint(x)\nprint(enumerate([1, 2]))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := benchCLI(t, "--json", "--build="+dir+"/prog", src)
	if code == exitOK {
		t.Fatalf("the build should fail\n%s", out)
	}
	var rep struct {
		Diagnostics []struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		} `json:"diagnostics"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("failed build should still print the partial result as JSON: %v\n%s", err, out)
	}
	if len(rep.Diagnostics) == 0 {
		t.Fatalf("partial result should carry the source diagnostics: %s", out)
	}
	if rep.Diagnostics[0].Level != "warning" || !strings.Contains(rep.Diagnostics[0].Msg, "arithmetic") {
		t.Errorf("unexpected diagnostics: %+v", rep.Diagnostics)
	}
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
	// The doctored case must regress. Other cases may also show up when the
	// machine is loaded (their baseline is 2x a single earlier best-of-N sample, and
	// wall clock swings more than that under a parallel test binary), so this
	// asserts the gate caught the case it was doctored for rather than requiring a
	// precise count.
	saw := false
	for _, reg := range verdict.Regressions {
		if reg.Name == target {
			saw = true
		}
		if reg.Backend != "aot" || reg.Suggestion == "" {
			t.Errorf("regression must name the gated leg and carry a suggestion: %+v", reg)
		}
	}
	if !saw {
		t.Fatalf("the doctored case %q was not reported as a regression: %+v", target, verdict.Regressions)
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
	// The tolerance is widened because the baseline is 2x a sample taken earlier in
	// this test binary: the point is that a generous baseline passes, not that a
	// sub-millisecond case repeats to the microsecond on a loaded machine.
	if out, code := benchCLI(t, "--bench-suite", "--bench-runs", "3", "--bench-opt", "2", "--bench-baseline", okPath, "--bench-min-ms", "0.01", "--bench-tolerance", "8"); code != exitOK {
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
	if _, code := benchCLI(t, "--bench-suite", "--bench-runs", "1", "--bench-opt", "1", "--bench-baseline", dir+"/nope.json"); code != exitUsage {
		// A missing/corrupt baseline is a bad argument, not a slow program and not a
		// compiler bug (docs/operations.md § Exit codes).
		t.Errorf("missing baseline exit = %d, want %d (usage error)", code, exitUsage)
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

// --- L8.2: the LLVM module verifier as a CLI stage --------------------------

type irVerificationReport struct {
	OK        bool     `json:"ok"`
	Tool      string   `json:"tool"`
	Skipped   bool     `json:"skipped"`
	Pipeline  []string `json:"pipeline"`
	Errors    []string `json:"errors"`
	Note      string   `json:"note"`
	Toolchain string   `json:"toolchain"`
}

func TestCLIVerifyLLVMJSON(t *testing.T) {
	out, code := benchCLI(t, "--json", "--verify-llvm", "def f(x) -> int:\n    return x * 2\n\nprint(f(3))\n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	var rep irVerificationReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if !rep.OK || rep.Skipped {
		t.Errorf("verdict = %+v, want ok", rep)
	}
	if rep.Tool == "" || len(rep.Pipeline) == 0 {
		t.Errorf("the report must name the tool and pipeline: %+v", rep)
	}
	if !strings.Contains(rep.Toolchain, lang.PinnedLLVMVersion) {
		t.Errorf("toolchain = %q, want it to name LLVM %s", rep.Toolchain, lang.PinnedLLVMVersion)
	}
}

func TestCLIVerifyLLVMHuman(t *testing.T) {
	out, code := benchCLI(t, "--verify-llvm", "xs = [i * 2 for i in range(3)]\nprint(xs)\n")
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "verified") {
		t.Errorf("human output should say the module was verified:\n%s", out)
	}
}

// TestCLIVerifyLLVMRejectsFlagAsSource: a value-taking flag followed by another
// flag would otherwise compile the string "--json" as a program.
func TestCLIVerifyLLVMRejectsFlagAsSource(t *testing.T) {
	_, code := benchCLI(t, "--verify-llvm", "--json", "print(1)")
	if code != exitUsage {
		t.Errorf("exit = %d, want %d (usage)", code, exitUsage)
	}
}

func TestCLIBuildCarriesVerifierVerdict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.gy")
	if err := os.WriteFile(src, []byte("def add(a, b) -> int:\n    return a + b\n\nprint(add(2, 3))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := benchCLI(t, "--build", filepath.Join(dir, "p"), "--json", src)
	if code != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
	var res struct {
		Verification *irVerificationReport `json:"verification"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if res.Verification == nil || !res.Verification.OK {
		t.Errorf("a build must report the verifier verdict: %s", out)
	}

	out2, code2 := benchCLI(t, "--build", filepath.Join(dir, "p2"), "--no-verify", "--json", src)
	if code2 != 0 {
		t.Fatalf("exit = %d, want 0\n%s", code2, out2)
	}
	var res2 struct {
		Verification *irVerificationReport `json:"verification"`
	}
	if err := json.Unmarshal([]byte(out2), &res2); err != nil {
		t.Fatalf("json: %v\n%s", err, out2)
	}
	if res2.Verification != nil {
		t.Errorf("--no-verify must skip the stage entirely: %s", out2)
	}
}

var combinedBinOnce sync.Once
var combinedBinPath string
var combinedBinErr error

func combinedBin(t *testing.T) string {
	t.Helper()
	combinedBinOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gustyc-combined")
		if err != nil {
			combinedBinErr = err
			return
		}
		bin := filepath.Join(dir, "gustyc")
		cmd := exec.Command("go", "build", "-tags=llvm20", "-o", bin, "./cmd/gustyc")
		cmd.Dir = "../.." // the package directory is not the module root
		if out, err := cmd.CombinedOutput(); err != nil {
			combinedBinErr = fmt.Errorf("build CLI: %v: %s", err, out)
			return
		}
		combinedBinPath = bin
	})
	if combinedBinErr != nil {
		t.Fatal(combinedBinErr)
	}
	return combinedBinPath
}

// cliCombined runs the CLI and returns stdout+stderr together with the exit code.
func cliCombined(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(combinedBin(t), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out.String(), code
}

// The usage line has always advertised `gustyc [flags] [<src>]` and --file is documented as
// its alias, but a positional argument reached no branch at all: the CLI printed the usage
// text and exited 0 without compiling anything.
func TestCLIPositionalSourcePathRuns(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p1.gy")
	if err := os.WriteFile(src, []byte("print(21 * 2)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := cliCombined(t, src)
	if code != 0 {
		t.Fatalf("positional source exit = %d, want 0 (output %q)", code, out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("positional source did not run the program: %q", out)
	}
	// and it agrees with its documented alias
	outFile, codeFile := cliCombined(t, "--file", src)
	if codeFile != code || outFile != out {
		t.Errorf("positional and --file differ: %q/%d vs %q/%d", out, code, outFile, codeFile)
	}
}

func TestCLIPositionalSourceWithTrailingFlags(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p2.gy")
	if err := os.WriteFile(src, []byte("x = 7\nprint(x)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// flags after the program are the agent habit that flag.Parse used to swallow
	out, code := cliCombined(t, src, "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (output %q)", code, out)
	}
	if !strings.Contains(out, "result") {
		t.Errorf("--json after a positional produced no JSON: %q", out)
	}
}

// A mistyped path must not be read as a program: `gustyc prog.gy` in the wrong directory used
// to fail with a *runtime* "undefined name prog", blaming the user's code for a shell mistake.
func TestCLIMissingSourceFileIsAUsageError(t *testing.T) {
	out, code := cliCombined(t, filepath.Join(t.TempDir(), "absent.gy"))
	if code != 4 {
		t.Errorf("missing .gy path exit = %d, want 4 (output %q)", code, out)
	}
	if !strings.Contains(out, "no such file") {
		t.Errorf("missing file should say so: %q", out)
	}
}

// A positional that is not a path at all is source text, the way `--eval` is.
func TestCLIPositionalSourceTextEvaluates(t *testing.T) {
	out, code := cliCombined(t, "print(6 * 7)")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (output %q)", code, out)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("source text did not evaluate: %q", out)
	}
}

// Gap M.2 — the CLI must say which backend ran a program. `--file` quietly used the AST
// interpreter, so "I ran this program compiled" could mean "I interpreted it", and AOT-only
// bugs hid behind the default path.
func TestCLIReportsWhichBackendRan(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.gy")
	if err := os.WriteFile(src, []byte("print(6 * 7)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, code := cliCombined(t, "--json", "--file", src)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (%s)", code, got)
	}
	if !strings.Contains(got, `"backend": "interpreter"`) {
		t.Errorf("`--file` must report the interpreter as its backend: %s", got)
	}

	got, code = cliCombined(t, "--json", "--aot", "--eval", "print(6 * 7)")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (%s)", code, got)
	}
	if !strings.Contains(got, `"backend": "aot"`) {
		t.Errorf("`--aot` must report the compiled backend: %s", got)
	}
	if !strings.Contains(got, `"output": "42`) {
		t.Errorf("`--aot --json` should capture program output: %s", got)
	}

	// --jit remains the same backend under its original name.
	got, _ = cliCombined(t, "--json", "--jit", "--eval", "print(1)")
	if !strings.Contains(got, `"backend": "aot"`) {
		t.Errorf("--jit is the compiled backend: %s", got)
	}
}

// --show-backend is the human-visible form of the same fact, and it must not
// contaminate program output: stdout stays the program's, per ADR 0169.
func TestCLIShowBackendGoesToStderr(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.gy")
	if err := os.WriteFile(src, []byte("print(1 + 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := combinedBin(t)
	cmd := exec.Command(bin, "--file", src)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "2\n" {
		t.Fatalf("stdout without --show-backend = %q, want \"2\\n\"", stdout.String())
	}

	cmd = exec.Command(bin, "--file", src, "--show-backend")
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "2\n" {
		t.Errorf("program output changed: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "backend interpreter") {
		t.Errorf("--show-backend must announce the backend on stderr, got %q", stderr.String())
	}
}

// Contradictory backend requests are a usage error, not a coin flip.
func TestCLIAotAndInterpContradict(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.gy")
	if err := os.WriteFile(src, []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][]string{{"--aot", "--interp"}, {"--jit", "--interp"}} {
		args := append([]string{}, pair...)
		got, code := cliCombined(t, append(args, "--file", src)...)
		if code != 4 {
			t.Errorf("%v --file: exit = %d, want 4 (usage) — output %q", pair, code, got)
		}
		if !strings.Contains(got, "backend") {
			t.Errorf("%v: the error should name the contradiction, got %q", pair, got)
		}
	}
}

// --interp is an explicit statement of the default, and must still work.
func TestCLIExplicitInterpBackendRuns(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "p.gy")
	if err := os.WriteFile(src, []byte("xs = []\nxs.append(3)\nprint(xs[0])\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, code := cliCombined(t, "--interp", "--file", src)
	if code != 0 || !strings.HasSuffix(strings.TrimSpace(got), "3") {
		t.Fatalf("--interp run failed: exit=%d out=%q", code, got)
	}
}

// TestCLIGCStats: --gc-stats reports the collector without polluting the
// program's stdout, and carries the same numbers as data under --json (L7.2).
func TestCLIGCStats(t *testing.T) {
	prog := "acc = 0\nfor k in range(400):\n    row = [k, k]\n    acc = acc + row[1]\nprint(acc)\n"
	got, code := cliCombined(t, "--eval", prog, "--gc-stats")
	if code != 0 {
		t.Fatalf("--gc-stats exit = %d, want 0 (%q)", code, got)
	}
	if !strings.Contains(got, "79800\n") {
		t.Fatalf("program output missing under --gc-stats: %q", got)
	}
	if !strings.Contains(got, "gc: backend=interpreter collections=") || !strings.Contains(got, "total_freed=") {
		t.Fatalf("--gc-stats did not report the collector: %q", got)
	}
	// The report must not be part of the program's stdout: with stderr dropped,
	// only what the program printed survives.
	cmd := exec.Command(combinedBin(t), "--eval", prog, "--gc-stats")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stdout.String() != "79800\n" {
		t.Fatalf("--gc-stats leaked the report into stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "gc: backend=interpreter") {
		t.Fatalf("collector report belongs on stderr, got %q", stderr.String())
	}
	// Machine path: the same numbers as a JSON member.
	jsonOut := strings.TrimSpace(cli(t, "--eval", prog, "--gc-stats", "--json"))
	// The program's own print goes to stdout too; the payload is the line that
	// starts the JSON object.
	for i, ln := range strings.Split(jsonOut, "\n") {
		if strings.HasPrefix(ln, "{") {
			jsonOut = strings.Join(strings.Split(jsonOut, "\n")[i:], "\n")
			break
		}
	}
	var payload struct {
		Result  string        `json:"result"`
		Backend string        `json:"backend"`
		GC      *lang.GCStats `json:"gc"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &payload); err != nil {
		t.Fatalf("--gc-stats --json is not valid JSON: %v\n%s", err, jsonOut)
	}
	if payload.GC == nil {
		t.Fatalf("--gc-stats --json has no gc member: %s", jsonOut)
	}
	if payload.GC.Backend != "interpreter" || payload.GC.Collections == 0 {
		t.Fatalf("gc member does not describe a real collection: %+v", payload.GC)
	}
	// Without the flag the payload keeps its old shape: no gc member.
	plain := cli(t, "--eval", "x = 1\nx")
	if strings.Contains(plain, "\"gc\"") {
		t.Fatalf("gc member appeared without --gc-stats: %s", plain)
	}
}
