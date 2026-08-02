//go:build unix

package flow

import (
	"os/exec"
	"syscall"
)

// setProcGroup puts the step's process in its own process group so a timeout
// can kill the WHOLE tree. Killing only the direct child (CommandContext's
// default) leaves grandchildren — a wedged `git fetch`, a runaway node —
// holding the step's stdout/stderr pipes, and Wait blocks on those pipes
// until the orphans exit on their own (a 15m step timeout observed to
// surface after 71m, live).
func setProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid targets the group: the shell AND everything it spawned.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
