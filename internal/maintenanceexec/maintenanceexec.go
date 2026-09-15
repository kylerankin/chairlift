// Package maintenanceexec provides process-group-bounded execution for
// configured maintenance scripts, ensuring orphaned child processes are reaped
// upon context cancellation or timeout.
//
// It is kept puregotk-free so it can be safely unit-tested on headless CI runners.
package maintenanceexec

import (
	"context"
	"os/exec"
	"syscall"
	"time"
)

// WaitDelay bounds how long Wait blocks after the process group has
// been signalled. A configured maintenance script can spawn children (work
// started through pkexec, for example) that inherit its pipes, so a straggler
// could otherwise hold cmd.Run open forever even though the whole group is
// already dead.
const WaitDelay = 5 * time.Second

// Run runs a configured maintenance script under ctx. The
// script runs in its own process group and cancellation kills the whole
// group, so children the script spawns (including via pkexec) are reaped too
// rather than orphaned and left running after ChairLift reports the timeout.
func Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = WaitDelay
	return cmd.Run()
}
