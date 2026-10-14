package lang

// The isolated program runner: how a compiled program is actually executed.
//
// Until ADR 0343 the answer was "in this process". `dlopenRun` (jit_llvm.go) loaded the linked shared
// object with `dlopen`, called its generated `main`, and captured what it printed by `dup2`-ing a pipe
// over file descriptors 1 and 2 for the duration of the call. That is sound exactly as long as one
// program runs at a time, because descriptors are a property of the *process*, not of the call: two
// concurrent runs would each redirect the same fd 1 into their own pipe and each read back some
// unpredictable mixture of both programs' output — and neither would be able to tell that happened,
// because the failure lands as a wrong string in a test that is not about the string at all. The same
// constraint ruled out `t.Parallel()` for the whole suite (the reason `tools/testshards` exists), and it
// meant a program that segfaulted took the test binary — and every case still queued behind it — down
// with it.
//
// So a program now runs the way a program runs: as its own child process, born with its own descriptors,
// its own working directory, its own environment and its own exit status, and the harness reads its
// stdout and stderr off pipes it owns. Nothing about a run is process-global any more, which is what
// makes "every case runs beside every other case" a property of the suite rather than a wish. Three things
// come free with it:
//
//   - two programs cannot interfere, in any way, at any granularity;
//   - a crashing program is a failed case, not a crashed test binary (the exit status distinguishes a
//     trap the language reported from a signal that killed the program — see statusToCode);
//   - a program that never finishes is one case that times out and says which one it was, instead of a
//     `go test` alarm that names whichever case happened to be on the CPU when the clock ran out.
//
// The in-process `dlopen` path is kept — behind GUSTY_JIT_INPROCESS=1 and beside a parity case that keeps
// both answers equal — because it is the artifact the benchmark measures warm execution of, and because
// "the two ways of entering the same machine code agree" is a claim worth checking rather than assuming.
//
// Roadmap Gap R.253, ADR 0343.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// ProgramBudget is how long one compiled program may run before the harness stops it.
//
// A program a *user* started — `--eval`, `--file`, the REPL — is allowed to loop until its owner stops
// it, and this budget is not meant to override that person; it is the patience of a harness that asked a
// program a question and would like to report an answer, including the answer "it did not answer". It is
// deliberately the same number the reference leg gets (a program the harness waits on is a program the
// harness waits on, whether it is CPython or our own machine code) and the same override shape: set
// GUSTY_PROGRAM_TIMEOUT to change it, `off` to remove it.
var ProgramBudget = envDuration("GUSTY_PROGRAM_TIMEOUT", OracleBudgetDefault)

// envDuration reads one of the harness's clocks, reporting a value that does not parse instead of
// quietly ignoring it — the rule envBudget follows, for the same reason.
func envDuration(name string, def time.Duration) time.Duration {
	raw, ok := os.LookupEnv(name)
	if !ok {
		return def
	}
	s := strings.TrimSpace(raw)
	if s == "" || s == "off" || s == "none" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		fmt.Fprintf(os.Stderr, "gustyc: %s=%q is not a duration above zero; using the pinned %s\n", name, raw, def)
		return def
	}
	return d
}

// ProgramTimeoutError says a compiled program ran out of the harness's patience and was killed with its
// process group. It is not a trap: the program neither finished nor reported an error, it stopped
// answering, and the case that asked has to be able to say so (ADR 0211's classification rule — a reader
// chasing this looks at the program's loop, not at the compiler).
type ProgramTimeoutError struct {
	Source string        // the program's source, truncated
	Budget time.Duration // how long it was allowed to take
	Stdout string        // what it printed before it was killed
	Stderr string        // what it wrote to the tool channel before it was killed
}

func (e *ProgramTimeoutError) Error() string {
	src := e.Source
	if len(src) > 200 {
		src = src[:200] + "…"
	}
	return fmt.Sprintf("run: the compiled program gave up after %s and was killed — it did not trap, it never finished (look for an unbounded loop in: %s)", e.Budget, src)
}

// Unwrap lets errors.Is(err, context.DeadlineExceeded) answer for this failure class too.
func (e *ProgramTimeoutError) Unwrap() error { return errProgramTimeout }

// errProgramTimeout is the sentinel the Unwrap chain exposes. It is a distinct sentinel rather than
// context.DeadlineExceeded itself so that a caller asking "did the *toolchain* time out?" (which asks
// for a *ToolTimeoutError) cannot be answered by a program that merely looped.
var errProgramTimeout = fmt.Errorf("program exceeded its run budget")

// runChildOptions is what one execution of a compiled program may be told beyond the binary.
type runChildOptions struct {
	// Dir is the program's working directory. It is per-run, which is the entire point: the import-from-
	// cwd cases used to os.Chdir the test process — a process-wide act that no two cases may perform at
	// once — and now each case hands its own directory to the process that needs it.
	Dir string
	// Env is the program's environment, verbatim, when non-nil; nil means inherit this process's.
	// A case that needs a variable set no longer edits the process for everyone (the t.Setenv half of the
	// same problem: Go itself forbids Setenv on a parallel test because the environment is process state).
	Env []string
	// Stdin is what the program reads. The language has no input() today; the field is here so that when
	// it gets one, the runner is not the reason it cannot be tested concurrently.
	Stdin string
	// Source is the program's source text, carried so a timeout can name the program that timed out.
	Source string
	// Budget is how long the program may run. Zero means no budget at all.
	Budget time.Duration
}

// childRun is what one execution of a compiled program came back with.
type childRun struct {
	Stdout string
	Stderr string
	// Code is the status the program's own `main` returned, or 128+signal when it never returned one.
	Code int
	// Command is the run in the form res.Commands reports it.
	Command string
	// Signaled says the program did not return from `main` at all: a signal killed it. That is a
	// different fault from a trap. A trap is the program answering "this is not meaningful" with the
	// reference's sentence; a signal is the artifact dying — infinite recursion running off the end of
	// the stack, a store past the end of a buffer — which is the compiler's business, not the program's.
	// The caller that maps a run onto an exit code needs the difference (ADR 0164's contract: one
	// failure class, one code), and before the run was a process the distinction could not even be
	// observed: the crash was the harness's own.
	Signaled bool
	// Signal names the signal that ended the program ("SIGSEGV"), when one did.
	Signal string
}

// runChildProcess executes the linked program at exePath as a child and returns what it printed and how
// it stopped — the same answers dlopenRun produced, from a process that shares nothing with its
// neighbours.
//
// The pipes are the harness's own (never an inherited descriptor), so nothing this process has done to
// its own fd 1 or fd 2 — and nothing the `testing` package does while a parallel case is finishing —
// can land inside a program's output.
func runChildProcess(exePath string, opts *runChildOptions) (childRun, error) {
	if opts == nil {
		opts = &runChildOptions{}
	}
	budget := opts.Budget
	if budget == 0 {
		budget = ProgramBudget
	}
	var run childRun
	c := toolCommand(budget, "run", exePath)
	defer c.finish()
	if opts.Dir != "" {
		c.Cmd.Dir = opts.Dir
	}
	if opts.Env != nil {
		c.Cmd.Env = opts.Env
	}
	if opts.Stdin != "" {
		c.Cmd.Stdin = strings.NewReader(opts.Stdin)
	}
	var outB, errB bytes.Buffer
	c.Cmd.Stdout = &outB
	c.Cmd.Stderr = &errB
	runErr := c.Cmd.Run()
	run.Stdout, run.Stderr = outB.String(), errB.String()
	run.Command = exeCommandLine(exePath, opts)
	if runErr == nil {
		run.Code = statusToCode(c.Cmd.ProcessState)
		return run, nil
	}
	if c.timedOut() {
		run.Code = -1
		return run, &ProgramTimeoutError{Source: opts.Source, Budget: budget, Stdout: run.Stdout, Stderr: run.Stderr}
	}
	// A program killed by a signal is a real answer, not a harness failure: the shell's status is handed
	// back and Signaled says which of the two ways-to-stop it was. Only a failure to *start* the process
	// is the harness's own error.
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) {
		run.Code = -1
		return run, fmt.Errorf("run: cannot start the compiled program %s: %w", filepath.Base(exePath), runErr)
	}
	run.Code = statusToCode(exitErr.ProcessState)
	if st, ok := exitErr.ProcessState.Sys().(syscall.WaitStatus); ok && st.Signaled() {
		run.Signaled = true
		run.Signal = st.Signal().String()
	}
	return run, nil
}

// statusToCode turns a wait status into the number the program's own `main` returned.
//
// A compiled program reports a trap by returning non-zero; a program that died of a signal never got to
// return anything, and reporting that as 0 (which is what ProcessState.ExitCode() does on some platforms
// for a signalled child) would tell a caller the program ran to completion. The shell's convention —
// 128 + signal — is used instead, so `exit 139` reads as "killed by SIGSEGV" to anyone who has ever
// read a shell status.
func statusToCode(ps *os.ProcessState) int {
	if ps == nil {
		return -1
	}
	if ps.Exited() {
		return ps.ExitCode()
	}
	if status, ok := ps.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	if code := ps.ExitCode(); code != 0 {
		return code
	}
	return -1
}

// exeCommandLine renders the run the way res.Commands renders an llc or cc call: the command a reader
// could paste into a shell, with the working directory named because it is part of the result.
func exeCommandLine(exePath string, opts *runChildOptions) string {
	line := exePath
	if opts.Dir != "" {
		line = fmt.Sprintf("(cd %s && %s)", opts.Dir, exePath)
	}
	return line
}
