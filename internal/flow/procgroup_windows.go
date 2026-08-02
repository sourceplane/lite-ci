//go:build windows

package flow

import "os/exec"

// setProcGroup is a no-op on Windows: CommandContext's default kill applies,
// and WaitDelay (set at the call site) unblocks the pipe reads.
func setProcGroup(cmd *exec.Cmd) {}
