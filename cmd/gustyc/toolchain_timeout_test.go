package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// Exit class 8: the toolchain never answered.
//
// The CI failure this family closes was a run that waited on a subprocess forever and then reported
// the wrong victim. The fix has three parts, and each is pinned here: the call ends at its budget
// (pkg/lang/tool_budget_test.go drives that), the failure keeps its own type through every wrap, and
// the CLI gives it a code that is neither "your program is wrong" (1) nor "the compiler is wrong" (2).

func TestToolchainExitKeepsSilenceApartFromRejection(t *testing.T) {
	rejection := &lang.ToolchainRejectionError{Tool: "llc-20", Stage: "llc", Err: errors.New("exit status 1"), Output: "Invalid module"}
	timeout := &lang.ToolTimeoutError{Tool: "/usr/bin/llc-20", Stage: "llc", Budget: 5 * time.Minute}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"a plain compile failure is a compile error", errors.New("codegen: cannot lower this"), exitCompileError},
		{"LLVM rejecting our module is the compiler's bug", rejection, exitIRVerify},
		{"a tool that was killed at its budget is the machine's", timeout, exitToolchainTimeout},
		{"a timeout wrapped by the stage still reads 8", errWrapText(timeout), exitToolchainTimeout},
		// A stage that wraps with %v instead of %w throws the class away, and the reader gets the
		// ordinary compile error. Pinned because it is the reason every toolchain wrap in pkg/lang uses
		// %w — and because a silent fall to class 1 is the failure mode to notice first.
		{"a timeout whose wrapper dropped the type is only a compile error", errors.New("jit: " + timeout.Error()), exitCompileError},
		{"a timeout wrapped in the pipeline's own text still reads 8", errWrapText(timeout), exitToolchainTimeout},
	}
	for _, tc := range cases {
		if got := toolchainExit(tc.err); got != tc.want {
			t.Errorf("%s: toolchainExit = %d, want %d (%v)", tc.name, got, tc.want, tc.err)
		}
	}
	// The verifier stage splits the same way, and only that way: today every LLVM verdict was 2.
	if got := verifyExitCode(timeout); got != exitToolchainTimeout {
		t.Errorf("verifyExitCode(timeout) = %d, want %d", got, exitToolchainTimeout)
	}
	if got := verifyExitCode(rejection); got != exitIRVerify {
		t.Errorf("verifyExitCode(rejection) = %d, want %d", got, exitIRVerify)
	}
	if !isToolchainTimeout(errWrapText(timeout)) || isToolchainTimeout(rejection) {
		t.Error("isToolchainTimeout must follow the type, not the sentence")
	}
}

// errWrapText wraps a failure the way a pipeline stage does — with %w, so the class survives the
// sentence the reader sees.
func errWrapText(err error) error { return &wrapped{err} }

type wrapped struct{ inner error }

func (w *wrapped) Error() string { return "build: llc: " + w.inner.Error() + "\n(nothing else)" }
func (w *wrapped) Unwrap() error { return w.inner }

func TestToolchainTimeoutExitCodeFromTheCLI(t *testing.T) {
	// Driven through a fresh process with a budget no real `llc` can satisfy: the runner stops
	// answering on purpose, and the CLI has to say which of its three toolchain classes fired.
	bin := combinedBin(t)
	run := func(override string, args ...string) (string, int) {
		cmd := exec.Command(bin, args...)
		cmd.Env = os.Environ()
		if override != "" {
			cmd.Env = append(cmd.Env, override)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		code := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("run %v: %v (%s)", args, err, stderr.String())
			}
			code = ee.ExitCode()
		}
		return stdout.String(), code
	}
	const src = "print(6 * 7)\n"
	// A budget no real `llc` can satisfy: 1ns is gone before the child is even scheduled.
	const starved = "GUSTY_TOOL_TIMEOUT=1ns"

	// The control: the same program, the pinned budget, exit 0 and the right answer. Without this the
	// assertion below could be satisfied by a CLI that reports 8 for everything.
	// The empty setting is the default, not "zero patience" — pinned here as much as in the harness.
	out, code := run("GUSTY_TOOL_TIMEOUT=", "--aot", "--eval", src)
	if code != exitOK || strings.TrimSpace(out) != "42" {
		t.Fatalf("control run: exit %d, output %q, want 0 and 42", code, out)
	}

	// The finding: a budget the tool cannot meet is class 8, and the message says so in words a
	// reader can act on (which tool, what budget, and that the machine — not the program — is the
	// suspect).
	out, code = run(starved, "--aot", "--eval", src)
	if code != exitToolchainTimeout {
		t.Fatalf("with GUSTY_TOOL_TIMEOUT=1ns: exit = %d, want %d (%s)", code, exitToolchainTimeout, out)
	}
	if strings.Contains(out, "42") {
		t.Errorf("the program ran to completion under a 1ns budget — the budget did not fire: %q", out)
	}

	// The machine path: the payload names the phase, so an agent branches on data instead of
	// scraping a sentence.
	out, code = run(starved, "--json", "--aot", "--eval", src)
	if code != exitToolchainTimeout {
		t.Fatalf("json mode: exit = %d, want %d (%s)", code, exitToolchainTimeout, out)
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
		t.Fatalf("json payload did not parse: %v\n%s", err, out)
	}
	if rec["phase"] != "toolchain" {
		t.Errorf("payload phase = %v, want \"toolchain\" — the phase is how an agent knows to look at the machine", rec["phase"])
	}
	if rec["exit"] != float64(exitToolchainTimeout) {
		t.Errorf("payload exit = %v, want %d", rec["exit"], exitToolchainTimeout)
	}
	if msg, _ := rec["error"].(string); !strings.Contains(msg, "1ns") || !strings.Contains(msg, "llc") {
		t.Errorf("payload error does not name the tool and its budget: %q", msg)
	}
}
