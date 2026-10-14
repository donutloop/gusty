package lang

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A toolchain call that stops answering.
//
// These are the cases the CI panic made real: a subprocess call whose wait never ended, and a suite
// that could only report the failure by dying ten minutes later with the name of an innocent case in
// the message. What a budget has to buy is three things — the call ends, the error says which tool ran
// out of patience, and nothing else about the pipeline moves — so each is pinned here, plus the two
// shapes a budget must NOT mistake for a hang.

// stubTool writes a program that mimics one of the ways a toolchain call misbehaves: it either answers
// (and exits), or it never will (and its caller has to decide when to stop waiting). The file is named
// for the behaviour under test, because the name is what the timeout error has to carry.
func stubTool(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAToolThatNeverReturnsIsKilledAtItsBudget(t *testing.T) {
	t.Parallel()
	stub := stubTool(t, "stub-never-answers", `echo "thinking"; while true; do :; done`)
	const budget = 300 * time.Millisecond
	start := time.Now()
	out, err := runTool(budget, stub)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("a call that never returned came back ok: %q", out)
	}
	var timeout *ToolTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("want a *ToolTimeoutError, got %T: %v", err, err)
	}
	if timeout.Budget != budget {
		t.Errorf("the error says %s, the call allowed %s — the number the reader acts on has to be the one that fired", timeout.Budget, budget)
	}
	if !strings.Contains(timeout.Error(), "stub-never-answers") {
		t.Errorf("the error does not name the tool that stopped answering: %s", timeout)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a caller asking the generic question (errors.Is DeadlineExceeded) gets no answer through the classification")
	}
	// Generous ceiling: the point is that it ended at all, not that it hit 300ms exactly. Anything near
	// the waitGrace figure means the kill did not happen and something else released the wait.
	if elapsed > 20*time.Second {
		t.Fatalf("the call took %s — the budget did not end it, something else did", elapsed)
	}
}

func TestTheKillTakesTheToolsChildrenWithIt(t *testing.T) {
	t.Parallel()
	// The hung-call-that-wont-die shape: the tool backgrounds a child, the child inherits the write end
	// of stdout, and the tool itself then stops answering. Killing only the tool would leave Wait
	// blocked on a pipe whose writer is still alive — the same hang, one layer down. This asserts the
	// call comes back at its budget rather than whenever the grandchild feels like exiting.
	stub := stubTool(t, "stub-with-children", `sleep 120 & echo started; while true; do :; done`)
	const budget = 300 * time.Millisecond
	start := time.Now()
	_, err := runTool(budget, stub)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected the call to fail")
	}
	var timeout *ToolTimeoutError
	if !errors.As(err, &timeout) {
		t.Fatalf("want a *ToolTimeoutError, got %T: %v", err, err)
	}
	if elapsed > 30*time.Second {
		t.Fatalf("the call waited %s for a grandchild: the group was not killed, only the driver", elapsed)
	}
}

func TestAMissingToolIsStillNotATimeout(t *testing.T) {
	t.Parallel()
	// The other half of the contract. A tool that is not installed never had a budget to run out of,
	// and reporting it as one would tell the reader to raise GUSTY_TOOL_TIMEOUT instead of installing
	// LLVM 20 — ADR 0211's refusal to blame the machine's setup on the compiler, applied here.
	_, err := runTool(time.Minute, "gusty-no-such-tool-xyz")
	if err == nil {
		t.Fatal("expected the missing tool to fail")
	}
	var timeout *ToolTimeoutError
	if errors.As(err, &timeout) {
		t.Fatalf("a missing tool reported as a timeout: %v", err)
	}
	if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, exec.ErrNotFound) {
		t.Errorf("the exec error was replaced rather than kept: %v", err)
	}
}

func TestASlowToolThatAnswersIsUntouched(t *testing.T) {
	t.Parallel()
	// The false-alarm guard: the budget must never fire on a call that finished. A tool that takes a
	// moment and exits 0 is the ordinary case — every single case in this suite — and a budget that
	// turned those into failures would be worse than no budget at all.
	stub := stubTool(t, "stub-answers", `echo answer; exit 0`)
	out, err := runTool(ToolBudget, stub)
	if err != nil {
		t.Fatalf("a call that answered within its budget failed: %v (%s)", err, out)
	}
	if strings.TrimSpace(string(out)) != "answer" {
		t.Fatalf("combined output = %q, want the tool's own line", out)
	}
}

func TestTheBudgetsAreTunableAndSaySo(t *testing.T) {
	t.Parallel()
	// A setting that is ignored is a worse setting than one that does not exist — so the interesting part of
	// this case is not that the number is read, it is what happens to a value that cannot be honoured: the
	// pinned default comes back AND the run says so. Asserting that from inside the process used to mean
	// t.Setenv, which a parallel case may not do and which would have retuned every other case's toolchain
	// calls with it (ADR 0343). The environment is passed in now, and the complaint is a buffer.
	const def = 7 * time.Second
	lookup := func(v string, present bool) func(string) (string, bool) {
		return func(string) (string, bool) { return v, present }
	}
	rows := []struct {
		name    string
		value   string
		present bool
		want    time.Duration
		wants   string // a substring of the complaint, when the default is being restored
	}{
		{"unset takes the pinned default", "", false, def, ""},
		{"set-but-empty takes the pinned default", "", true, def, ""},
		{"a whitespace answer takes the pinned default", "   ", true, def, ""},
		{"a real duration is honoured", "45s", true, 45 * time.Second, ""},
		{"minutes are a duration", "2m", true, 2 * time.Minute, ""},
		{"bare seconds are a number, not a duration", "90", true, def, "is not a duration above zero"},
		{"zero is not a budget", "0s", true, def, "is not a duration above zero"},
		{"negative is not a budget", "-3s", true, def, "is not a duration above zero"},
		{"prose is not a duration", "soon", true, def, "is not a duration above zero"},
	}
	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var warned strings.Builder
			if got := budgetFrom(lookup(tc.value, tc.present), "GUSTY_TEST_BUDGET", def, &warned); got != tc.want {
				t.Errorf("GUSTY_TEST_BUDGET=%q (present=%v): got %s, want %s", tc.value, tc.present, got, tc.want)
			}
			got := warned.String()
			if tc.wants == "" && got != "" {
				t.Errorf("a setting that was honoured still complained: %q", got)
			}
			if tc.wants != "" {
				if !strings.Contains(got, tc.wants) {
					t.Errorf("an ignored setting must say so, got %q", got)
				}
				if !strings.Contains(got, "GUSTY_TEST_BUDGET") || !strings.Contains(got, def.String()) {
					t.Errorf("the complaint must name the variable and the default it fell back to: %q", got)
				}
			}
		})
	}
	// The pinned defaults are the numbers docs/operations.md states, and a change to one is a change to
	// that page: the CI budget is derived from them.
	if ToolBudgetDefault != 5*time.Minute {
		t.Errorf("ToolBudgetDefault = %s, want the documented 5m", ToolBudgetDefault)
	}
	if OracleBudgetDefault != 2*time.Minute {
		t.Errorf("OracleBudgetDefault = %s, want the documented 2m", OracleBudgetDefault)
	}
	// And the whole chain, asked of a process that was actually started with the variable set: the table
	// proves the parser, this proves that the entry point every tool call goes through reads it.
	if got := envProbe(t, []string{"GUSTY_TOOL_TIMEOUT=17s"}, "tool-budget"); got != "tool-budget=17s" {
		t.Errorf("GUSTY_TOOL_TIMEOUT=17s reached the process as %q — a tunable that is ignored is worse than one that does not exist", got)
	}
	if got := envProbe(t, nil, "tool-budget"); got != "tool-budget="+ToolBudgetDefault.String() {
		t.Errorf("with nothing set the process resolved %q, want the pinned default", got)
	}
}

// envProbe runs tools/envprobe — the probe that reports the knobs a process resolved out of ITS OWN
// environment — with the environment a case wants, and returns its trimmed stdout.
//
// The knobs (`GUSTY_TOOL_TIMEOUT`, `GUSTY_ORACLE_TIMEOUT`, `GUSTY_PYTHON`) are process-wide by design; that is
// what makes them tunable from a shell. Asking the real entry points about them from inside a test used to
// mean `t.Setenv`, which a parallel case may not call and which retunes every other case running beside it.
// A subprocess answers the same question about a process nobody else is using (ADR 0343's rule: isolate the
// process-wide thing rather than queueing for it).
func envProbe(t *testing.T, env []string, keys ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the probe is run with `go run`, which is spelled differently there")
	}
	dir, err := filepath.Abs(filepath.Join("..", "..", "tools", "envprobe"))
	if err != nil {
		t.Fatalf("locate the probe: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
		t.Fatalf("the envprobe the suite depends on is missing: %v", err)
	}
	// The probe is a tool call, so it gets a tool call's budget rather than an invented number.
	ctx, cancel := context.WithTimeout(context.Background(), ToolBudget)
	defer cancel()
	argv := append([]string{"run", dir}, keys...)
	cmd := exec.CommandContext(ctx, "go", argv...)
	cmd.Env = append(os.Environ(), env...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run ./tools/envprobe %v: %v\n%s", keys, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

func TestTheCompiledLegStillRunsWithItsBudgets(t *testing.T) {
	t.Parallel()
	// End to end: the budgets are in the path every program takes through llc and cc, so this is the
	// case that says "nothing about running a program changed".
	out, err := RunSource("print(1 + 2)\nprint(\"ok\")\n")
	if err != nil {
		t.Fatalf("RunSource under the budgets: %v", err)
	}
	if out != "3\nok\n" {
		t.Fatalf("stdout = %q, want 3/ok", out)
	}
}
