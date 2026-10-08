//go:build windows

package lang

import (
	"os/exec"
	"syscall"
)

// Windows has no process groups for a tool like `cc` to own, so the kill is the one os/exec would
// do anyway: the budget, the timeout class and the message are the portable part of this design, the
// group signal is a Unix refinement.
func toolProcAttr() *syscall.SysProcAttr { return nil }

// killToolGroup signals the call's own process.
func killToolGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
