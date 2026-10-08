//go:build !windows

package lang

import (
	"os/exec"
	"syscall"
)

// Toolchain calls are started in their own process group so that giving up on one can kill the
// whole group. `cc` is a driver: it forks an assembler and a linker and waits on them, and killing
// only the driver leaves the child behind holding the output file open — the hung call comes back
// as a build that cannot write prog.o. Killing -pid takes the driver and its children together.
func toolProcAttr() *syscall.SysProcAttr { return &syscall.SysProcAttr{Setpgid: true} }

// killToolGroup signals every process in the call's group.
func killToolGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
