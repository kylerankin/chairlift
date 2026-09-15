package views

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRunMaintenanceCommandTerminatesProcessGroup verifies that cancelling a
// maintenance command reaps the whole group, not just the direct child: the
// script spawns a long-lived background child and waits, so if only the child
// process were killed the backgrounded child would survive the timeout.
func TestRunMaintenanceCommandTerminatesProcessGroup(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	scriptPath := filepath.Join(dir, "spawn.sh")

	script := "#!/bin/sh\n" +
		"sleep 60 &\n" +
		"echo $! > \"" + pidPath + "\"\n" +
		"wait\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	if err := runMaintenanceCommand(ctx, scriptPath); err == nil {
		t.Fatalf("expected timeout error, got nil")
	}

	raw, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("read child pid: %v", err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("parse child pid: %v", err)
	}

	// Give the kernel a moment to deliver the group-kill signal, then confirm
	// the backgrounded child is gone. A process-group kill reaps it; killing
	// only the direct child would leave it alive.
	time.Sleep(200 * time.Millisecond)
	if err := syscall.Kill(childPID, 0); err == nil {
		t.Fatalf("child process %d still alive after timeout: group was not terminated", childPID)
	}
}
