package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// updateWant rewrites a golden under expected/ from the observed output. Goldens
// encode reviewed behaviour (the exact stdout of a native build, or emitted IR),
// so they are regenerated deliberately after an intentional semantics change and
// never by hand-copying output.
//
//	go test -tags=llvm20 ./integration -run TestCLIBuild -args -update
var updateWant = flag.Bool("update", false, "rewrite integration golden files under expected/ from the observed output")

// cliBuilt caches one real gusty CLI binary for tests that must observe CLI
// behaviour (JSON shapes, exit codes, diagnostics reaching stdout/stderr) rather
// than calling library functions in-process.
var (
	cliBuiltOnce sync.Once
	cliBuiltPath string
	cliBuiltErr  error
)

func cliBin(t *testing.T) string {
	t.Helper()
	cliBuiltOnce.Do(func() {
		dir, err := os.MkdirTemp("", "gusty-cli-")
		if err != nil {
			cliBuiltErr = fmt.Errorf("temp dir: %w", err)
			return
		}
		cliBuiltPath = filepath.Join(dir, "gustyc")
		cmd := exec.Command("go", "build", "-o", cliBuiltPath, "github.com/donutloop/gusty/cmd/gustyc")
		if out, err := cmd.CombinedOutput(); err != nil {
			cliBuiltErr = fmt.Errorf("build gustyc: %v\n%s", err, out)
		}
	})
	if cliBuiltErr != nil {
		t.Fatalf("%v", cliBuiltErr)
	}
	return cliBuiltPath
}

// cliRun runs the cached CLI with args and returns stdout+stderr combined.
// cliRunCode runs the CLI and returns stdout plus the process exit code (0 when it
// succeeded), for assertions about the documented exit-code contract.
func cliRunCode(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		ee, ok := runErr.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc %v: %v", args, runErr)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

func cliRun(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	out, _ := cmd.CombinedOutput() // tests assert on the message/shape, exit code separately if needed
	return string(out)
}

// buildCLI compiles the actual gustyc binary once per test binary and returns
// its path. This lets the whole-program tests drive the real CLI command
// rather than calling library functions in-process.
func buildCLI(t *testing.T, bin string) {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", bin, "github.com/donutloop/gusty/cmd/gustyc")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build gustyc: %v\n%s", err, out)
	}
}

// writeSrc writes a gusty source file and returns its path.
func writeSrc(t *testing.T, dir, name, src string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// readProgram returns the contents of a gusty source program checked in under
// integration/programs/, so whole-program test sources live on disk rather
// than being hard-coded inline in each test.
func readProgram(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("programs", name))
	if err != nil {
		t.Fatalf("read program %s: %v", name, err)
	}
	return string(b)
}

// checkWant compares got against the golden expected/<name> and reports a clear
// mismatch, or rewrites the golden when the suite runs with -update. Goldens are
// regenerated deliberately after an intentional semantics change:
//
//	go test -tags=llvm20 ./integration -run TestCLIBuild -args -update
func checkWant(t *testing.T, name, got string) {
	t.Helper()
	if *updateWant {
		if err := os.WriteFile(filepath.Join("expected", name), []byte(got), 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
		t.Logf("updated expected/%s", name)
		return
	}
	if want := readWant(t, name); got != want {
		t.Errorf("expected/%s mismatch:\n got:\n%q\nwant:\n%q", name, got, want)
	}
}

// checkBackendParityWant asserts the interpreter and the AOT pipeline agree on a
// program (the parity contract) and then compares that shared output against the
// golden expected/<name>. With -update the golden is rewritten from the AOT run
// after the backends have been shown to agree, so a golden can never encode a
// one-sided behaviour.
func checkBackendParityWant(t *testing.T, interpOut, aotOut, name string) {
	t.Helper()
	if interpOut != aotOut {
		t.Errorf("backends disagree on %s:\n interpreter: %q\n AOT:       %q", name, interpOut, aotOut)
		return
	}
	checkWant(t, name, aotOut)
}

// readWant returns the exact expected stdout for a whole-program test, checked
// in under integration/expected/.
func readWant(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("expected", name))
	if err != nil {
		t.Fatalf("read expected %s: %v", name, err)
	}
	return string(b)
}

// TestCLIBuildWholeProgram drives the real `gustyc --build <out> <files...>`
// command end-to-end (the whole program): CLI arg parsing -> read/merge sources
// -> semantic analysis -> LLVM codegen -> llc (module verification) -> cc
// (link) -> a runnable native binary. It then executes that binary and checks
// its stdout, exactly as a user would.

// exitCode extracts the child process exit code from an exec error.
func exitCode(t *testing.T, err error) int {
	t.Helper()
	var ee *exec.ExitError
	if ok := errors.As(err, &ee); !ok {
		t.Fatalf("expected exec.ExitError, got %v", err)
	}
	return ee.ExitCode()
}

func TestCLIBuildWholeProgram(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "a.gy", readProgram(t, "whole_a.gy"))
	b := writeSrc(t, dir, "b.gy", readProgram(t, "whole_b.gy"))
	out := filepath.Join(dir, "prog")

	// --build <out> with the sources as positional args.
	build := exec.Command(bin, "--build", out, a, b)
	buildOut, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, buildOut)
	}
	if _, statErr := os.Stat(out); os.IsNotExist(statErr) {
		t.Fatalf("no binary produced at %s\n%s", out, buildOut)
	}

	// The produced binary is a real native executable.
	run := exec.Command(out)
	got, err := run.Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	checkWant(t, "whole.txt", string(got))

	// The CLI's JSON mode emits a machine-readable build result.
	js := exec.Command(bin, "--json", "--build", out, a, b)
	jsOut, err := js.Output()
	if err != nil {
		t.Fatalf("gustyc --json --build: %v\n%s", err, jsOut)
	}
	var res map[string]any
	if err := json.Unmarshal(jsOut, &res); err != nil {
		t.Fatalf("json build result not parseable: %v\n%s", err, jsOut)
	}
	if res["output"] != out {
		t.Errorf("json output = %v, want %q", res["output"], out)
	}
	if res["ir"] == "" {
		t.Errorf("json ir missing")
	}
}

// TestCLIBuildDiagnostics verifies the whole CLI surfaces a compile error in
// any source file and refuses to produce a binary.
func TestCLIBuildDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	bad := writeSrc(t, dir, "bad.gy", readProgram(t, "bad.gy"))
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, bad)
	outb, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("gustyc --build should fail on undefined name; got success\n%s", outb)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on error")
	}
}

// TestCLIBuildRequiresSource verifies the usage error when --build has no
// positional source files.
func TestCLIBuildRequiresSource(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	build := exec.Command(bin, "--build", filepath.Join(dir, "prog"))
	if err := build.Run(); err == nil {
		t.Fatalf("gustyc --build with no sources should exit non-zero")
	}
}

// TestCLIBuildSingleFile verifies the build command works with exactly one
// source file, not just a multi-file merge.
func TestCLIBuildSingleFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "s.gy", readProgram(t, "single.gy"))
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, src)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, outb)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	checkWant(t, "single.txt", string(got))
}

// TestCLIBuildFString verifies f-strings with runtime integer/float
// interpolation compile ahead-of-time and print a single combined line,
// matching the interpreter's one-string Repr.
func TestCLIBuildFString(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "fstr.gy", readProgram(t, "fstr.gy"))
	out := filepath.Join(dir, "prog")
	build := exec.Command(bin, "--build", out, src)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, outb)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built fstr program: %v", err)
	}
	checkWant(t, "fstr.txt", string(got))
}

// TestCLIBuildFloatFunction verifies a user function that returns a float
// (e.g. `return x / 2.0`) emits a `double` return and prints the exact value
// in the AOT codegen path.
func TestCLIBuildFloatFunction(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	src := writeSrc(t, dir, "floatfn.gy", readProgram(t, "floatfn.gy"))
	out := filepath.Join(dir, "prog")
	build := exec.Command(bin, "--build", out, src)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, outb)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built floatfn: %v", err)
	}
	checkWant(t, "floatfn.txt", string(got))
}

// TestCLIBuildOptLevel verifies --opt-level is honored end-to-end: the CLI
// passes it through to Build, and the produced binary still runs correctly.
func TestCLIBuildOptLevel(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "s.gy", readProgram(t, "sq.gy"))
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--opt-level", "2", "--build", out, src)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --opt-level 2 --build: %v\n%s", err, outb)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	checkWant(t, "sq.txt", string(got))
}

// TestCLIBuildMissingFile verifies a nonexistent source file fails with a
// non-zero exit and an error mentioning the missing path, without producing a
// binary.
func TestCLIBuildMissingFile(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	out := filepath.Join(dir, "prog")
	missing := filepath.Join(dir, "nope.gy")
	build := exec.Command(bin, "--build", out, missing)
	outb, err := build.CombinedOutput()
	if err == nil {
		t.Fatalf("gustyc --build should fail for missing source; got success\n%s", outb)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced")
	}
}

// TestCLIBuildExitCodes asserts the whole CLI's exit-code contract: 0 on
// success, 1 on compile/link error, 2 on usage error.
func TestCLIBuildExitCodes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	src := writeSrc(t, dir, "s.gy", readProgram(t, "print1.gy"))
	bad := writeSrc(t, dir, "bad.gy", readProgram(t, "bad.gy"))
	okOut := filepath.Join(dir, "ok")
	badOut := filepath.Join(dir, "bad")

	// success -> 0
	if err := exec.Command(bin, "--build", okOut, src).Run(); err != nil {
		t.Fatalf("success build should exit 0: %v", err)
	}
	// compile error -> 1
	if err := exec.Command(bin, "--build", badOut, bad).Run(); err == nil {
		t.Fatalf("compile-error build should exit non-zero")
	} else if code := exitCode(t, err); code != 1 {
		t.Errorf("compile error exit = %d, want 1", code)
	}
	// usage error (no sources) -> 4
	if err := exec.Command(bin, "--build", filepath.Join(dir, "u")).Run(); err == nil {
		t.Fatalf("no-sources build should exit non-zero")
	} else if code := exitCode(t, err); code != 4 {
		t.Errorf("usage error exit = %d, want 4 (docs/operations.md § Exit codes)", code)
	}
}

// TestCLIExitCodeContract pins the documented table (docs/operations.md
// § Exit codes): each failure *class* has its own code, so a script can tell "my program
// is wrong" (1) from "my program crashed" (3) from "I invoked the CLI badly" (4) — the
// distinction agents were told to rely on and could not (roadmap Gap J.3).
func TestCLIExitCodeContract(t *testing.T) {
	bin := cliBin(t)
	dir := t.TempDir()
	badSrc := filepath.Join(dir, "bad_semantics.gy")
	// A call to an undefined function: the checker rejects it, so --build must fail
	// before any toolchain runs (exit 1), not emit IR that LLVM then refuses (exit 2).
	if err := os.WriteFile(badSrc, []byte("print(nope_such_function(1))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The run-path sources for the compiled leg: a program that works, a program that
	// raises, and a program the compiled backend declines to lower.
	cleanSrc := filepath.Join(dir, "clean.gy")
	trapSrc := filepath.Join(dir, "raises.gy")
	oobSrc := filepath.Join(dir, "oob.gy")
	refuseSrc := filepath.Join(dir, "refuses.gy")
	for f, body := range map[string]string{
		cleanSrc: "print(1 + 1)\n",
		trapSrc:  "raise ValueError(\"boom\")\n",
		oobSrc:   "xs = [1]\nprint(xs[5])\n",
		// str * int still refuses (roadmap Gap R.33). The fixture used to be a runtime-string
		// loop, which ADR 0229 made answerable — and a fixture that quietly stops refusing
		// quietly stops exercising the exit-code contract, so it has to move with the gap.
		refuseSrc: "print(\"ab\" * 2)\n",
	} {
		if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		args []string
		want int
	}{
		{"success", []string{"--eval", "print(1 + 1)"}, 0},
		{"compile error: does not parse", []string{"--eval", "x = 1 +"}, 1},
		// A front-end rejection is exit 1 wherever the checker runs: --check reports the
		// diagnostic and --build refuses the program before any toolchain step.
		{"compile error: checker rejects", []string{"--check", "x: int = \"text\""}, 1},
		{"compile error: build refuses", []string{"--build", filepath.Join(dir, "b"), badSrc}, 1},
		// `--eval` is the interactive path: it parses and runs, so an undefined name is
		// an *execution* failure (3), not a front-end one — no diagnostics were emitted.
		{"runtime error: undefined name under --eval", []string{"--eval", "print(undefined_thing)"}, 3},
		{"runtime error: uncaught exception", []string{"--eval", "raise ValueError(\"boom\")"}, 3},
		{"runtime error: out of range", []string{"--eval", "xs = [1]\nprint(xs[5])"}, 3},
		// The compiled path used to be the exception: it forwarded the program's traceback
		// and then reported success, because the in-process JIT threw away the status the
		// generated main returns. One failure class, one code, whichever backend ran it
		// (roadmap Gap R.17, ADR 0211).
		{"success under --aot", []string{"--aot", cleanSrc}, 0},
		{"runtime error: uncaught exception under --aot", []string{"--aot", trapSrc}, 3},
		{"runtime error: out of range under --aot", []string{"--aot", oobSrc}, 3},
		{"compile error: codegen refuses under --aot", []string{"--aot", refuseSrc}, 1},
		{"usage error: unknown flag", []string{"--definitely-not-a-flag"}, 4},
		{"usage error: --build without sources", []string{"--build", filepath.Join(dir, "o")}, 4},
		// The oracle leg (roadmap L11.9, ADR 0186): "gusty disagrees with Python" is its
		// own class, and "the oracle could not judge" is a third one. Neither may share a
		// code with a compile error or a runtime trap.
		{"oracle: conformant program", []string{"--oracle", "print(1 + 1)"}, 0},
		// A bool handed to a function is the divergence this case needs: both backends print the
		// parameter as the number the caller's verdict was made from while CPython prints True
		// (Gap R.111 — the shape this row used before ADR 0259 paid the container version,
		// `print([True, 1])`, exactly as ADR 0257 had paid `print(True)` before that; a contract
		// row has to be pointed at a debt that is still open).
		{"oracle: divergence from CPython", []string{"--oracle", "def show(f):\n    print(f)\n\nshow(1 == 1)\nshow(True)"}, 6},
		{"oracle: no verdict (gusty-only surface)", []string{"--oracle", "async def f():\n    return 1\n\nprint(await f())"}, 7},
		{"oracle: usage error on a missing file", []string{"--oracle-file", filepath.Join(dir, "nope.gy")}, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := exec.Command(bin, tc.args...).Run()
			code := 0
			if err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("run: %v", err)
				}
				code = ee.ExitCode()
			}
			if code != tc.want {
				t.Errorf("gustyc %v: exit = %d, want %d", tc.args, code, tc.want)
			}
		})
	}
	// The machine path carries the same classification as the exit status, in the
	// documented payload shapes — including structured parse spans, so no caller has to
	// scrape `gustyc: parse error at 1:7: …` prose off stderr.
	pOut, pCode := cliRunCode(t, "--json", "--eval", "x = 1 +")
	if pCode != 1 {
		t.Errorf("--json parse failure exit = %d, want 1\n%s", pCode, pOut)
	}
	var parseRep struct {
		OK     bool `json:"ok"`
		Phase  string
		Exit   int
		Errors []struct {
			Line int `json:"line"`
			Col  int `json:"col"`
			Msg  string
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(pOut), &parseRep); err != nil {
		t.Fatalf("--json parse failure payload: %v\n%s", err, pOut)
	}
	if parseRep.OK || parseRep.Exit != 1 || parseRep.Phase != "parse" {
		t.Errorf("parse payload = %+v, want ok=false phase=parse exit=1", parseRep)
	}
	if len(parseRep.Errors) == 0 || parseRep.Errors[0].Line != 1 {
		t.Errorf("parse payload should carry spans with line/col, got %+v", parseRep.Errors)
	}

	rtOut, rtCode := cliRunCode(t, "--json", "--eval", "raise ValueError(\"boom\")")
	if rtCode != 3 {
		t.Errorf("--json runtime exit = %d, want 3\n%s", rtCode, rtOut)
	}
	if !strings.Contains(rtOut, "\"exit\": 3") {
		t.Errorf("--json runtime payload should report exit 3, got %s", rtOut)
	}
}

// TestCLIBuildReportsOptimization is Gap J.4: an un-optimized build used to be
// indistinguishable from an optimized one. The report must reach both human and JSON output,
// and flags after the source file must work (Go's flag package stops at the first
// positional, so `--build out src.gy --opt-level=2` used to fail on a file named
// "--opt-level=2").
func TestCLIBuildReportsOptimization(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	src := writeSrc(t, dir, "opt.gy", "def add(a, b):\n    return a + b\n\nprint(add(2, 3))\n")

	// flags after the positional source (this used to be a hard failure)
	outPath := filepath.Join(dir, "opt-bin")
	human, code := cliRunCode(t, "--build="+outPath, src, "--opt-level=2")
	if code != 0 {
		t.Fatalf("trailing --opt-level must work, got exit %d\n%s", code, human)
	}
	if !strings.Contains(human, "optimized by") || !strings.Contains(human, "-O2") {
		t.Errorf("human output should name the optimizer and pipeline, got:\n%s", human)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("binary should exist: %v", err)
	}

	jsonOut, code := cliRunCode(t, "--json", "--build="+outPath, src, "--opt-level=2")
	if code != 0 {
		t.Fatalf("--json build failed: %d\n%s", code, jsonOut)
	}
	var rep struct {
		Optimization *struct {
			Tool     string
			Pipeline string
			Level    int
			Applied  bool
			Fallback string
			Note     string
		} `json:"optimization"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &rep); err != nil {
		t.Fatalf("payload: %v\n%s", err, jsonOut)
	}
	if rep.Optimization == nil {
		t.Fatalf("--opt-level=2 payload must carry an optimization report, got %s", jsonOut)
	}
	o := rep.Optimization
	if !o.Applied || o.Pipeline != "-O2" || o.Level != 2 || o.Tool == "" {
		t.Errorf("optimization report = %+v, want applied -O2 level 2 with a tool name", o)
	}

	// level 0 asks for nothing: no report, and no claim of optimization
	plain, code := cliRunCode(t, "--json", "--build="+outPath, src)
	if code != 0 {
		t.Fatalf("plain build failed: %d\n%s", code, plain)
	}
	if strings.Contains(plain, `"optimization"`) {
		t.Errorf("a level-0 build should not claim an optimization stage: %s", plain)
	}
}

// TestCLIBuildTypoIsACompileErrorNotACompilerBug is the exit-code consequence of
// Gap K.10: `print(undefined_thing)` used to slip past the checker, reach LLVM as a load
// from a slot that does not exist, and be reported as "LLVM rejected the module we emitted"
// (exit 2, a compiler bug) for what is an ordinary typo.
func TestCLIBuildTypoIsACompileErrorNotACompilerBug(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	src := writeSrc(t, dir, "typo.gy", "total = 0\nprint(undefined_thing)\n")
	cmd := exec.Command(bin, "--build="+filepath.Join(dir, "out"), src)
	out, err := cmd.CombinedOutput()
	code := 1
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if code != 1 {
		t.Errorf("a typo must exit 1 (compile error), got %d\n%s", code, out)
	}
	text := string(out)
	if !strings.Contains(text, "undefined name") || !strings.Contains(text, "undefined_thing") {
		t.Errorf("the report should name the undefined identifier, got:\n%s", text)
	}
	if strings.Contains(text, "module is broken") || strings.Contains(text, "verifier") {
		t.Errorf("a source typo must not be reported as an LLVM/verifier failure:\n%s", text)
	}
}

// TestCLIBuildDuplicateFunction verifies that two files defining the same
// top-level function produce a build error (duplicate symbol) rather than a
// silently broken binary.
func TestCLIBuildDuplicateFunction(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "a.gy", readProgram(t, "dup_a.gy"))
	b := writeSrc(t, dir, "b.gy", readProgram(t, "dup_b.gy"))
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, a, b)
	if outb, err := build.CombinedOutput(); err == nil {
		t.Fatalf("duplicate def across files should fail; got success\n%s", outb)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on duplicate symbol")
	}
}

// TestCLIBuildJSONDiagnostics verifies that on a compile error, --json emits a
// parseable BuildResult carrying the diagnostics, and stderr carries the error.
func TestCLIBuildJSONDiagnostics(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	bad := writeSrc(t, dir, "bad.gy", readProgram(t, "bad.gy"))
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--json", "--build", out, bad)
	var stderr bytes.Buffer
	build.Stderr = &stderr
	outb, err := build.Output() // stdout carries the JSON BuildResult
	if err == nil {
		t.Fatalf("build should fail on undefined name; got success\n%s", outb)
	}
	var res map[string]any
	if jerr := json.Unmarshal(outb, &res); jerr != nil {
		t.Fatalf("json diagnostics not parseable: %v\n%s", jerr, outb)
	}
	if stderr.Len() == 0 {
		t.Errorf("expected human error on stderr, got none")
	}
	if _, ok := res["diagnostics"]; !ok {
		t.Errorf("diagnostics key missing in JSON result: %v", res)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no binary should be produced on error")
	}
}

// TestCLIBuildRebuildOverwrite verifies building a second time to the same
// output path succeeds and the binary still runs.
func TestCLIBuildRebuildOverwrite(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "a.gy", readProgram(t, "rebuild_a.gy"))
	b := writeSrc(t, dir, "b.gy", readProgram(t, "rebuild_b.gy"))
	out := filepath.Join(dir, "prog")

	for i := 0; i < 2; i++ {
		build := exec.Command(bin, "--build", out, a, b)
		if outb, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build #%d: %v\n%s", i+1, err, outb)
		}
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run rebuilt binary: %v", err)
	}
	checkWant(t, "rebuild.txt", string(got))
}

// TestCLIBuildAllFeatures drives the whole gustyc program against a large
// two-file source covering every AOT-supported language feature, then runs the
// produced native binary and asserts its entire stdout byte-for-byte.
func TestCLIBuildAllFeatures(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	a := writeSrc(t, dir, "features_a.gy", readProgram(t, "features_a.gy"))
	b := writeSrc(t, dir, "features_b.gy", readProgram(t, "features_b.gy"))
	out := filepath.Join(dir, "prog")

	build := exec.Command(bin, "--build", out, a, b)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build (all features): %v\n%s", err, outb)
	}

	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run all-features binary: %v", err)
	}
	checkWant(t, "features.txt", string(got))
}

// Large multi-file programs. Each file is big (many statements); the tests
// drive the real gustyc binary end-to-end and assert the produced binary's
// entire stdout against an exact expected constant.

// buildWant runs `gustyc --build` over files, then runs the produced binary
// and asserts its stdout equals want.
func buildWant(t *testing.T, bin string, files []string, wantName string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "prog")
	args := append([]string{"--build", out}, files...)
	build := exec.Command(bin, args...)
	if outb, err := build.CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build: %v\n%s", err, outb)
	}
	got, err := exec.Command(out).Output()
	if err != nil {
		t.Fatalf("run built binary: %v", err)
	}
	checkWant(t, wantName, string(got))
}

// TestCLIBuildLargeMath builds a large 3-file math program (functions defined
// in one file, called from the others) and checks the binary's exact stdout.
func TestCLIBuildLargeMath(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	buildWant(t, bin, []string{
		writeSrc(t, dir, "mathlib.gy", readProgram(t, "math_lib.gy")),
		writeSrc(t, dir, "calc.gy", readProgram(t, "math_calc.gy")),
		writeSrc(t, dir, "main.gy", readProgram(t, "math_main.gy")),
	}, "math.txt")
}

// TestCLIBuildLargeControl builds a large 3-file control-flow program (loops,
// while, break/continue, if/elif/else, nested loops) and checks exact stdout.
func TestCLIBuildLargeControl(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	buildWant(t, bin, []string{
		writeSrc(t, dir, "ctrl_a.gy", readProgram(t, "ctrl_a.gy")),
		writeSrc(t, dir, "ctrl_b.gy", readProgram(t, "ctrl_b.gy")),
		writeSrc(t, dir, "ctrl_c.gy", readProgram(t, "ctrl_c.gy")),
	}, "ctrl.txt")
}

// TestCLIBuildLargeData builds a large 3-file data program (list/dict/set
// literals, builtins, functions, loops) and checks exact stdout.
func TestCLIBuildLargeData(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	buildWant(t, bin, []string{
		writeSrc(t, dir, "data_a.gy", readProgram(t, "data_a.gy")),
		writeSrc(t, dir, "data_b.gy", readProgram(t, "data_b.gy")),
		writeSrc(t, dir, "data_c.gy", readProgram(t, "data_c.gy")),
	}, "data.txt")
}

// TestCLIEmitsExpectedIR drives the whole gustyc program's `--emit-llvm` path
// and asserts the emitted LLVM IR matches the expected code byte-for-byte.
func TestCLIEmitsExpectedIR(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	cmd := exec.Command(bin, "--emit-llvm", readProgram(t, "ir.gy"))
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("gustyc --emit-llvm: %v", err)
	}
	checkWant(t, "ir.ll", string(got))
}
