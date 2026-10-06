package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Gap R.17 (ADR 0211): the compiled run path forwarded a program's traceback and then
// reported success. The in-process JIT links the program, calls the generated `main`, and
// had been throwing away what `main` returned — so `gustyc --aot prog.gy` exited 0 for a
// program that died from an uncaught exception, on the very path an agent scripts a compiled
// run through. "Did it work?" was the first question asked and the one answer withheld.
//
// These tests pin the contract from the outside, where it can actually be broken: the exit
// status of a real process, per run path, plus the JSON payload that is supposed to agree
// with it.

func writeTrapCase(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTrapIsTheSameClassOnEveryRunPath(t *testing.T) {
	bin := cliBin(t)
	dir := t.TempDir()
	trap := writeTrapCase(t, dir, "trap.gy", "xs = [1, 2, 3]\nprint(xs[-4])\n")
	clean := writeTrapCase(t, dir, "clean.gy", "print(1 + 1)\n")

	// The same source, the same failure: a trap is class 3 whether the AST interpreter or
	// native code executed it. Before this cycle only the interpreter paths said so.
	for _, args := range [][]string{{"--aot", trap}, {"--file", trap}, {"--aot", trap}} {
		if code := runCode(t, bin, args...); code != 3 {
			t.Errorf("gustyc %v: exit = %d, want 3 (a trap is the runtime class on every path)", args, code)
		}
	}
	if code := runCode(t, bin, "--aot", clean); code != 0 {
		t.Errorf("a program that ran to completion exited %d, want 0", code)
	}
	// A program the compiled backend will not lower is still class 1: propagating the
	// program's status must not blur "it crashed" into "we refused to build it".
	// str * int is a shape the compiled backend still declines (roadmap Gap R.33); the
	// fixture used to be a runtime-string loop, which ADR 0229 made answerable.
	refused := writeTrapCase(t, dir, "refused.gy", "print(\"ab\" * 2)\n")
	if code := runCode(t, bin, "--aot", refused); code != 1 {
		t.Errorf("gustyc --aot on a codegen refusal: exit = %d, want 1", code)
	}
}

// TestTrapKeepsTheTracebackOffStdout is the half of the contract that outlives the exit
// code: stdout is only what the program printed, and the diagnostic belongs to the tool.
func TestTrapKeepsTheTracebackOffStdout(t *testing.T) {
	bin := cliBin(t)
	dir := t.TempDir()
	trap := writeTrapCase(t, dir, "trap.gy", "xs = [1, 2, 3]\nprint(xs[-4])\n")

	cmd := exec.Command(bin, "--aot", trap)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 3 {
		t.Fatalf("run: %v (exit %v)", err, exitOf(err))
	}
	if stdout.String() != "" {
		t.Errorf("stdout must hold only what the program printed, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "IndexError") {
		t.Errorf("the trap should be reported on stderr, got %q", stderr.String())
	}
}

func TestJITJSONExitAgreesWithTheProcessStatus(t *testing.T) {
	dir := t.TempDir()
	trap := writeTrapCase(t, dir, "trap.gy", "xs = [1, 2, 3]\nprint(xs[-4])\n")
	clean := writeTrapCase(t, dir, "clean.gy", "print(1 + 1)\n")

	out, code := cliRunCode(t, append([]string{"--json", "--aot"}, trap)...)
	var payload struct {
		Output string `json:"output"`
		Exit   int    `json:"exit"`
		Stderr string `json:"stderr"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if payload.Exit != code {
		t.Errorf("the payload says exit %d but the process exited %d — a machine caller is entitled to the same answer twice", payload.Exit, code)
	}
	if payload.Exit != 3 {
		t.Errorf("trapped program: exit = %d, want 3\n%s", payload.Exit, out)
	}
	if !strings.Contains(payload.Stderr, "IndexError") {
		t.Errorf("the JSON path must carry the diagnostic an agent cannot scrape off a terminal: %q", payload.Stderr)
	}

	out, code = cliRunCode(t, append([]string{"--json", "--aot"}, clean)...)
	var cleanPayload struct {
		Output string `json:"output"`
		Exit   int    `json:"exit"`
	}
	if err := json.Unmarshal([]byte(out), &cleanPayload); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if cleanPayload.Exit != code || cleanPayload.Exit != 0 {
		t.Errorf("clean program: payload exit %d, process %d, want 0 and 0\n%s", cleanPayload.Exit, code, out)
	}
	if cleanPayload.Output != "2\n" {
		t.Errorf("clean program output = %q, want \"2\\n\"", cleanPayload.Output)
	}
}

// The oracle leg classifies a trap the way ADR 0166 requires: a leg that did not complete is not a leg
// that answered. That distinction was unenforceable while the JIT discarded the status. There are two
// legs now — the compiled program and CPython — and both are named in the payload, because "the program
// crashed" and "the program crashed on both sides" are different verdicts for whoever is reading it.
func TestOracleRecordsATrappingCompiledLegAsFailed(t *testing.T) {
	out, code := cliRunCode(t, "--json", "--oracle", "xs = [1, 2, 3]\nprint(xs[-4])\n")
	var payload struct {
		Status string `json:"status"`
		Legs   []struct {
			Backend string `json:"backend"`
			OK      bool   `json:"ok"`
			Error   string `json:"error"`
		} `json:"legs"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("payload: %v\n%s", err, out)
	}
	if code == 0 {
		t.Fatalf("--oracle reported success for a program that crashed everywhere: %s", out)
	}
	if len(payload.Legs) != 2 {
		t.Fatalf("want two legs (aot, python), got %d: %s", len(payload.Legs), out)
	}
	if payload.Legs[0].Backend != "aot" || payload.Legs[1].Backend != "python" {
		t.Fatalf("legs out of order: %+v", payload.Legs)
	}
	aot := payload.Legs[0]
	if aot.OK {
		t.Errorf("the compiled leg trapped and is reported as ok: %+v", aot)
	}
	if !strings.Contains(aot.Error, "trapped") {
		t.Errorf("the compiled leg should say it trapped, got %q", aot.Error)
	}
}

// `llc` refusing the module our compiler emitted is the compiler-bug class (2), and the
// table said so — but only `--build` ever reached that code. The run path wrapped the same
// event in a generic error and reported "your program does not compile" (1), so one bug was
// described two ways depending on the flag in use (roadmap L11.8, ADR 0211).
func TestCompiledPathClassifiesAnLLVMRejectionAsACompilerBug(t *testing.T) {
	bin := cliBin(t)
	dir := t.TempDir()
	src := writeTrapCase(t, dir, "clean.gy", "print(1 + 1)\n")

	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "llc-20")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'invalid module: manufactured for a test' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "PATH="+stubDir+":"+os.Getenv("PATH"))

	cmd := exec.Command(bin, "--aot", src)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if exitOf(err) != 2 {
		t.Fatalf("an llc rejection through the run path: exit = %d, want 2 (the compiler-bug class)\nstdout %q\nstderr %q", exitOf(err), stdout.String(), stderr.String())
	}
	if stdout.String() != "" {
		t.Errorf("nothing was executed, so stdout must be empty, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "manufactured for a test") {
		t.Errorf("the toolchain's own words must reach the reader: %q", stderr.String())
	}

	// The machine path says the same thing the process says.
	jcmd := exec.Command(bin, "--json", "--aot", src)
	jcmd.Env = env
	jout, err := jcmd.Output()
	if exitOf(err) != 2 {
		t.Fatalf("--json llc rejection: exit = %d, want 2\n%s", exitOf(err), jout)
	}
	var payload struct {
		Error string `json:"error"`
		Exit  int    `json:"exit"`
	}
	if err := json.Unmarshal(jout, &payload); err != nil {
		t.Fatalf("payload: %v\n%s", err, jout)
	}
	if payload.Exit != 2 {
		t.Errorf("payload exit = %d, want 2 to match the process\n%s", payload.Exit, jout)
	}
	if !strings.Contains(payload.Error, "manufactured for a test") {
		t.Errorf("the JSON path must carry the toolchain output: %q", payload.Error)
	}

	// Control: the same source and the same binary reach the real toolchain and succeed,
	// so nothing above can pass by simply making every compiled run fail.
	if code := runCode(t, bin, "--aot", src); code != 0 {
		t.Errorf("control run exited %d, want 0", code)
	}
}

func runCode(t *testing.T, bin string, args ...string) int {
	t.Helper()
	err := exec.Command(bin, args...).Run()
	return exitOf(err)
}

func exitOf(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
