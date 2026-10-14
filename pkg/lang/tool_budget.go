package lang

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Every tool this compiler hands work to is a separate process, and a process can stop answering.
//
// Nothing after codegen runs inside this program: `llc` lowers the module, `cc` links it, `opt`
// optimises it, `llvm-as` assembles it, `llvm-dwarfdump` reads its line table, and the reference leg
// asks CPython. Each of those calls used to be an `exec.Command` waited on with no budget at all,
// which meant that a tool which hung did not fail the call — it *was* the call, forever. This is what
// that looked like in practice: CI died with a 10-minute `go test` timeout panic naming whichever case
// happened to be running when the alarm went off, with the stuck `lli` sitting three goroutines away
// from the message and never named. A harness that cannot say which of its own calls stopped answering
// is a harness nobody can debug, and an interactive session waits on the same silence.
//
// So every tool call carries a budget, and an expired budget is a reported class of its own. A tool
// that gives up is not a tool that said no: ToolchainRejectionError is the compiler accusing itself of
// emitting a module LLVM cannot lower, while a tool that ran out of patience on a loaded runner is the
// machine. Those are different actions for the reader — open a compiler bug, or look at the environment
// — so they are two types, reached with errors.As and never by matching message text (ADR 0211's
// classification rule, and the exit-code contract's, ADR 0164/0166).
const (
	// ToolBudgetDefault is what a build-stage tool gets. `llc`, `cc` and `opt` take milliseconds on the
	// suite's worst module (a 1200-statement function), so five minutes is not a measurement of their
	// speed — it is the point at which "still working" stops being believable. Deliberately generous:
	// killing a slow-but-progressing `cc` would turn a false alarm into a build failure, which is a
	// worse bug than the one this file closes. Override with GUSTY_TOOL_TIMEOUT.
	ToolBudgetDefault = 5 * time.Minute
	// OracleBudgetDefault is what one CPython reference run gets. Unlike the build tools, this call
	// waits on a *program* — the reference leg executes the source a case wrote — so a slow answer is
	// possible on purpose. Two minutes is the patience of the harness, not a limit the language imposes:
	// a program started by a user (`--eval`, `--file`, the REPL) runs in-process under dlopenRun, is
	// never timed at all, and may loop until its owner stops it. Override with GUSTY_ORACLE_TIMEOUT.
	OracleBudgetDefault = 2 * time.Minute
	// waitGrace is how long a killed tool is given to let its pipe copiers finish. It exists so a tool
	// that dies while a grandchild still holds the write end of stdout cannot wedge Wait forever — the
	// same hang, arriving one layer down.
	waitGrace = 10 * time.Second
)

// The budgets in force, read from the environment once, at load. The number that is right on a quiet
// machine is wrong on a loaded runner, and a harness that cannot be told "this box is slower than you
// assume" has to be recompiled to say so — which is how a timeout becomes a number nobody dares change.
var (
	ToolBudget   = envBudget("GUSTY_TOOL_TIMEOUT", ToolBudgetDefault)
	OracleBudget = envBudget("GUSTY_ORACLE_TIMEOUT", OracleBudgetDefault)
)

// envBudget reads one budget from the environment. A value that does not parse is reported rather than
// quietly ignored: someone who set GUSTY_TOOL_TIMEOUT=90 and is still waiting five minutes has to learn
// the variable never took effect (ADR 0166's rule about naming what is missing applies to the tool's own
// knobs too, and a silently ignored setting is the worst kind of missing thing).
func envBudget(name string, def time.Duration) time.Duration {
	return budgetFrom(os.LookupEnv, name, def, os.Stderr)
}

// budgetFrom is envBudget with the environment it reads and the writer it complains to handed in. The knobs
// are process-wide by design — that is what makes them tunable from a shell — but a test of a knob does not
// have to take the process's environment over to ask: ADR 0343's rule is that a fact a decision reads is a
// value the decision is given, and "what the environment says" is such a fact. The table in
// tool_budget_test.go is what that bought: the ignored-setting sentence is now asserted as text, which a
// t.Setenv case cannot do without owning the process.
func budgetFrom(lookup func(string) (string, bool), name string, def time.Duration, warn io.Writer) time.Duration {
	raw, ok := lookup(name)
	if !ok {
		return def
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		fmt.Fprintf(warn, "gustyc: %s=%q is not a duration above zero; using the pinned %s\n", name, raw, def)
		return def
	}
	return d
}

// ToolTimeoutError says a toolchain call ran out of its budget and was killed. It is not a rejection:
// the module was never accepted or refused, the tool simply stopped.
type ToolTimeoutError struct {
	Tool   string        // the binary that was killed, e.g. "llc-20"
	Stage  string        // the pipeline stage, e.g. "llc" or "cc"
	Budget time.Duration // how long it was allowed to take
}

func (e *ToolTimeoutError) Error() string {
	stage := e.Stage
	if stage == "" {
		stage = e.Tool
	}
	return fmt.Sprintf("%s: %s gave up after %s and was killed — it did not refuse the module, it stopped answering (look for a loaded machine, a full disk, or a toolchain other than the pinned LLVM %s on PATH)",
		stage, e.Tool, e.Budget, PinnedLLVMVersion)
}

// Unwrap makes errors.Is(err, context.DeadlineExceeded) true through the classification, so a caller
// that already knows how to ask "did this time out?" keeps getting an answer.
func (e *ToolTimeoutError) Unwrap() error { return context.DeadlineExceeded }

// toolCall is one toolchain call and its clock. The deadline lives here rather than only inside the
// exec.Cmd because os/exec keeps no public handle on the context a command was built with, and a call
// that cannot answer "was it the budget that ended you?" is exactly the bug this file exists to fix.
type toolCall struct {
	Cmd    *exec.Cmd
	stage  string
	budget time.Duration
	cancel context.CancelFunc
	ctx    context.Context // the budget itself, so the call can ask who ended it
	// releaseSlot hands back the concurrency governor's permit this call's process was admitted with
	// (tool_slots.go). It is never nil: with no limit in force it is a no-op.
	releaseSlot func()
}

// toolCommand builds a call that cannot outlive budget: on expiry the whole process *group* is killed
// (a tool like `cc` is a driver that leaves an `as`/`ld` behind, and killing only the driver strands
// the child holding the output file), and Wait stops waiting on the pipes a moment later.
//
// Callers that need Env, Stdin or separate Stdout/Stderr writers use this and must call finish when
// they are done. Everything else wants runTool.
func toolCommand(budget time.Duration, stage, name string, args ...string) *toolCall {
	// A budget of zero or less means the call is not timed at all: the caller said "wait as long as it
	// takes", and handing that to context.WithTimeout would kill it instantly, which is the opposite
	// answer. Every harness-shaped caller passes a positive number.
	var ctx context.Context
	var cancel context.CancelFunc
	if budget > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), budget)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	c := &toolCall{budget: budget, stage: stage, cancel: cancel, ctx: ctx}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = toolProcAttr()
	cmd.Cancel = func() error { return killToolGroup(cmd) }
	cmd.WaitDelay = waitGrace
	c.Cmd = cmd
	// One governor permit per external process, held for the process's whole life (tool_slots.go).
	// Taking it here rather than at each call site is what makes the limit the toolchain's guarantee
	// instead of each caller's courtesy — and this is the one place every toolcall is born.
	c.releaseSlot = acquireToolSlot()
	return c
}

// finish releases the call's timer and its governor slot; every caller defers it.
func (c *toolCall) finish() {
	c.releaseSlot()
	c.cancel()
}

// timedOut reports whether the call's clock, and not the tool, is what ended it.
//
// It asks the deadline itself rather than a signal copied from it, and not the kill hook either: a
// budget that expired before the child was ever started never runs Cancel at all (os/exec hands back
// ctx.Err() from Start), and a call that ended that way would otherwise be reported as "context
// deadline exceeded" with no tool, no stage and no number attached — the unhelpful message this file
// exists to replace. os/exec's Wait can return the ctx error before any derived signal is observable,
// which is why the check reads the context directly instead of a channel the goroutine closes.
func (c *toolCall) timedOut() bool {
	return errors.Is(c.ctx.Err(), context.DeadlineExceeded)
}

// failure is the error this call ended with: the timeout when the budget expired, the caller's own
// error otherwise. A tool that is not installed is never a timeout — the budget had nothing to do with
// it — so a call that never started keeps the exec error, which is what keeps a missing `llc-20` a
// machine fault rather than a compiler bug (ADR 0211).
func (c *toolCall) failure(err error) error {
	if err == nil || !c.timedOut() {
		return err
	}
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return err
	}
	return &ToolTimeoutError{Tool: c.Cmd.Path, Stage: c.stage, Budget: c.budget}
}

// runToolStage is a toolchain call whose whole interface is argv and combined output — the shape
// almost every stage wants. A call that expired its budget hands back a *ToolTimeoutError.
func runToolStage(budget time.Duration, stage, name string, args ...string) ([]byte, error) {
	c := toolCommand(budget, stage, name, args...)
	defer c.finish()
	out, err := c.Cmd.CombinedOutput()
	if err != nil {
		return out, c.failure(err)
	}
	return out, nil
}

// RunTool is runToolStage for a caller outside this package — a harness, mostly, that drives the pinned
// toolchain itself rather than going through Build or the runner.
//
// It is exported for one reason: the concurrency governor (tool_slots.go) only means something if every
// external process asks it first. A test that spawns `llc-20` with os/exec directly is not lying about
// anything except how many processes the machine is being asked to run at once, and on a suite whose
// cases now run beside each other that is the difference between a core each and a queue nobody can see.
// The budget the toolchain gives its own tools is the budget a harness's tool gets too.
func RunTool(stage, name string, args ...string) ([]byte, error) {
	return runToolStage(ToolBudget, stage, name, args...)
}

// runTool is runToolStage for a caller with nothing to add: the stage is the tool.
func runTool(budget time.Duration, name string, args ...string) ([]byte, error) {
	return runToolStage(budget, "", name, args...)
}
