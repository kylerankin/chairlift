package ublue

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/ubluehelper"
)

func writeCustomFakePkexec(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-pkexec")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing fake pkexec: %v", err)
	}
	return path
}

func withJournalSink(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	t.Setenv(journal.PathEnv, path)
	journal.Reset()
	t.Cleanup(journal.Reset)
	dryrun.Set(false)
	t.Cleanup(func() { dryrun.Set(false) })
	return path
}

func TestRunHelperJournalsSuccessWithDerivedCommand(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\n" +
		"echo 'chairlift-helper: exec [\"bootc\", \"switch\", \"--enforce-container-sigpolicy\", \"ghcr.io/projectbluefin/dakota:testing\"]'\n" +
		"echo 'switched to ghcr.io/projectbluefin/dakota:testing — restart to apply'\n" +
		"exit 0\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	stdout, stderr, err := runHelper(context.Background(), fakePkexec, ubluehelper.CommandChannelSwitch, "testing")
	if err != nil {
		t.Fatalf("runHelper returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if stdout != "switched to ghcr.io/projectbluefin/dakota:testing — restart to apply\n" {
		t.Errorf("stdout = %q, want stripped clean stdout", stdout)
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2 (dispatch + outcome)", len(entries))
	}

	dispatch := entries[0]
	if dispatch.Action != "channel-switch" || dispatch.Suppressed != journal.SuppressedNone {
		t.Errorf("dispatch = %+v, want action channel-switch, suppressed no", dispatch)
	}

	outcome := entries[1]
	if outcome.Action != "channel-switch" || outcome.Outcome != journal.OutcomeSucceeded {
		t.Errorf("outcome = %+v, want action channel-switch, outcome succeeded", outcome)
	}
	wantExecuted := [][]string{{"bootc", "switch", "--enforce-container-sigpolicy", "ghcr.io/projectbluefin/dakota:testing"}}
	if !reflect.DeepEqual(outcome.Executed, wantExecuted) {
		t.Errorf("outcome.Executed = %v, want %v", outcome.Executed, wantExecuted)
	}
}

func TestRunHelperJournalsMultipleDerivedCommands(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\n" +
		"echo 'chairlift-helper: exec [\"usermod\", \"-aG\", \"docker\", \"testuser\"]'\n" +
		"echo 'chairlift-helper: exec [\"systemctl\", \"enable\", \"--now\", \"docker.socket\", \"docker.service\"]'\n" +
		"echo 'docker enabled'\n" +
		"exit 0\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	stdout, stderr, err := runHelper(context.Background(), fakePkexec, ubluehelper.CommandDockerEnable)
	if err != nil {
		t.Fatalf("runHelper returned error: %v", err)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
	if stdout != "docker enabled\n" {
		t.Errorf("stdout = %q, want clean stdout", stdout)
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2 (dispatch + outcome)", len(entries))
	}

	outcome := entries[1]
	if outcome.Outcome != journal.OutcomeSucceeded {
		t.Errorf("outcome = %+v, want succeeded", outcome)
	}
	wantExecuted := [][]string{
		{"usermod", "-aG", "docker", "testuser"},
		{"systemctl", "enable", "--now", "docker.socket", "docker.service"},
	}
	if !reflect.DeepEqual(outcome.Executed, wantExecuted) {
		t.Errorf("outcome.Executed = %v, want %v", outcome.Executed, wantExecuted)
	}
}

func TestRunHelperJournalsRefusedOnExit126(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\nexit 126\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	_, _, err := runHelper(context.Background(), fakePkexec, ubluehelper.CommandChannelSwitch, "testing")
	if err == nil {
		t.Fatal("runHelper = nil, want error on exit 126")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	outcome := entries[1]
	if outcome.Action != "channel-switch" || outcome.Outcome != journal.OutcomeRefused {
		t.Errorf("outcome = %+v, want action channel-switch, outcome refused", outcome)
	}
	if outcome.ExitCode == nil || *outcome.ExitCode != 126 {
		t.Errorf("outcome.ExitCode = %v, want 126", outcome.ExitCode)
	}
}

func TestRunHelperJournalsFailedOnNonZeroExit(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\necho 'internal failure' >&2\nexit 1\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	_, _, err := runHelper(context.Background(), fakePkexec, ubluehelper.CommandChannelSwitch, "testing")
	if err == nil {
		t.Fatal("runHelper = nil, want error on exit 1")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	outcome := entries[1]
	if outcome.Action != "channel-switch" || outcome.Outcome != journal.OutcomeFailed {
		t.Errorf("outcome = %+v, want action channel-switch, outcome failed", outcome)
	}
	if outcome.ExitCode == nil || *outcome.ExitCode != 1 {
		t.Errorf("outcome.ExitCode = %v, want 1", outcome.ExitCode)
	}
}

func TestRunHelperJournalsCancelledOnContextCancel(t *testing.T) {
	journalPath := withJournalSink(t)

	script := "#!/bin/sh\nexec sleep 30\n"
	fakePkexec := writeCustomFakePkexec(t, script)

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)

	_, _, err := runHelper(ctx, fakePkexec, ubluehelper.CommandChannelSwitch, "testing")
	if err == nil {
		t.Fatal("runHelper = nil, want error on cancelled context")
	}

	entries := readJournal(t, journalPath)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	outcome := entries[1]
	if outcome.Action != "channel-switch" || outcome.Outcome != journal.OutcomeCancelled {
		t.Errorf("outcome = %+v, want action channel-switch, outcome cancelled", outcome)
	}
}
