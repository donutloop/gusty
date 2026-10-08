package lang

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	// A setting that is ignored is a worse setting than one that does not exist.
	if got := envBudget("GUSTY_TEST_BUDGET_ABSENT", 7*time.Second); got != 7*time.Second {
		t.Errorf("unset: got %s, want the default", got)
	}
	t.Setenv("GUSTY_TEST_BUDGET_SET", "45s")
	if got := envBudget("GUSTY_TEST_BUDGET_SET", 7*time.Second); got != 45*time.Second {
		t.Errorf("GUSTY_TEST_BUDGET_SET=45s: got %s, want 45s", got)
	}
	for _, bad := range []string{"90", "0s", "-3s", "soon"} {
		t.Setenv("GUSTY_TEST_BUDGET_BAD", bad)
		if got := envBudget("GUSTY_TEST_BUDGET_BAD", 7*time.Second); got != 7*time.Second {
			t.Errorf("GUSTY_TEST_BUDGET_BAD=%q: got %s, want the pinned default", bad, got)
		}
	}
	// The pinned defaults are the numbers docs/operations.md states, and a change to one is a change to
	// that page: the CI budget is derived from them.
	if ToolBudgetDefault != 5*time.Minute {
		t.Errorf("ToolBudgetDefault = %s, want the documented 5m", ToolBudgetDefault)
	}
	if OracleBudgetDefault != 2*time.Minute {
		t.Errorf("OracleBudgetDefault = %s, want the documented 2m", OracleBudgetDefault)
	}
}

func TestTheCompiledLegStillRunsWithItsBudgets(t *testing.T) {
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
