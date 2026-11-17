package journal

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func withSink(t *testing.T, path string) {
	t.Helper()
	t.Setenv(PathEnv, path)
	Reset()
	t.Cleanup(Reset)
}

func readEntries(t *testing.T, path string) []Entry {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening journal %s: %v", path, err)
	}
	defer func() { _ = file.Close() }()

	var entries []Entry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry Entry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			t.Fatalf("decoding journal line %q: %v", scanner.Text(), err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning journal: %v", err)
	}
	return entries
}

// With no sink configured — the state of every ordinary run — Record must
// do nothing: no file created, no error, no panic.
func TestRecordIsANoOpWithoutASink(t *testing.T) {
	t.Setenv(PathEnv, "")
	Reset()
	t.Cleanup(Reset)

	if Enabled() {
		t.Fatal("Enabled() = true with no sink configured")
	}

	Record("channel-switch", map[string]string{"channel": "testing"}, []string{"bootc", "switch"}, SuppressedDryRun)

	if _, err := os.Stat(filepath.Join(t.TempDir(), "journal.jsonl")); err == nil {
		t.Fatal("Record created a file with no sink configured")
	}
}

func TestRecordWritesOneLinePerEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	withSink(t, path)

	Record("channel-switch", map[string]string{"channel": "testing"},
		[]string{"pkexec", "/usr/bin/chairlift-helper", "channel-switch", "testing"}, SuppressedDryRun)
	Record("restart", nil, []string{"pkexec", "/usr/bin/chairlift-helper", "restart"}, SuppressedNone)

	entries := readEntries(t, path)
	if len(entries) != 2 {
		t.Fatalf("journal has %d entries, want 2", len(entries))
	}

	first := entries[0]
	if first.Action != "channel-switch" {
		t.Errorf("first entry action = %q, want channel-switch", first.Action)
	}
	if first.Args["channel"] != "testing" {
		t.Errorf("first entry args = %v, want channel=testing", first.Args)
	}
	wantArgv := []string{"pkexec", "/usr/bin/chairlift-helper", "channel-switch", "testing"}
	if len(first.WouldRun) != len(wantArgv) {
		t.Fatalf("first entry WouldRun = %v, want %v", first.WouldRun, wantArgv)
	}
	for i, arg := range wantArgv {
		if first.WouldRun[i] != arg {
			t.Errorf("WouldRun[%d] = %q, want %q", i, first.WouldRun[i], arg)
		}
	}
	if first.Suppressed != SuppressedDryRun {
		t.Errorf("first entry suppressed = %q, want %q", first.Suppressed, SuppressedDryRun)
	}
	if first.Seq != 1 {
		t.Errorf("first entry seq = %d, want 1", first.Seq)
	}
	if entries[1].Seq != 2 {
		t.Errorf("second entry seq = %d, want 2", entries[1].Seq)
	}
	if entries[1].Suppressed != SuppressedNone {
		t.Errorf("second entry suppressed = %q, want %q (a live run)", entries[1].Suppressed, SuppressedNone)
	}
}

// Seq must stay strictly increasing under concurrent callers, since tests
// that care about ordering rely on it rather than on wall-clock time.
func TestRecordSequencesConcurrentWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	withSink(t, path)

	const writers = 20
	done := make(chan struct{})
	for i := 0; i < writers; i++ {
		go func(n int) {
			Record("concurrent", map[string]string{"n": string(rune('a' + n))}, nil, SuppressedNone)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < writers; i++ {
		<-done
	}

	entries := readEntries(t, path)
	if len(entries) != writers {
		t.Fatalf("journal has %d entries, want %d", len(entries), writers)
	}
	seen := make(map[uint64]bool, writers)
	for _, entry := range entries {
		if seen[entry.Seq] {
			t.Errorf("seq %d appears more than once", entry.Seq)
		}
		seen[entry.Seq] = true
	}
	for n := uint64(1); n <= writers; n++ {
		if !seen[n] {
			t.Errorf("seq %d is missing; sequence has a gap", n)
		}
	}
}

func TestRecordUsesAnRFC3339Timestamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	withSink(t, path)

	Record("restart", nil, []string{"systemctl", "reboot"}, SuppressedNone)

	entries := readEntries(t, path)
	if len(entries) != 1 {
		t.Fatalf("journal has %d entries, want 1", len(entries))
	}
	if _, err := time.Parse(time.RFC3339, entries[0].Timestamp); err != nil {
		t.Errorf("timestamp %q does not parse as RFC 3339: %v", entries[0].Timestamp, err)
	}
}

// A journal that cannot be written (bad path) must not panic or propagate an
// error: the caller's actual job is the privileged action, not the journal.
func TestRecordToleratesAnUnwritableSink(t *testing.T) {
	withSink(t, filepath.Join(t.TempDir(), "does-not-exist", "journal.jsonl"))

	Record("restart", nil, []string{"systemctl", "reboot"}, SuppressedNone)
	// No assertion beyond "did not panic" — that is the whole contract.
}

func TestResetClearsTheSequenceCounter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	withSink(t, path)

	Record("a", nil, nil, SuppressedNone)
	Record("b", nil, nil, SuppressedNone)

	path2 := filepath.Join(t.TempDir(), "journal2.jsonl")
	t.Setenv(PathEnv, path2)
	Reset()

	Record("c", nil, nil, SuppressedNone)

	entries := readEntries(t, path2)
	if len(entries) != 1 || entries[0].Seq != 1 {
		t.Fatalf("after Reset, sequence did not restart: %+v", entries)
	}
}

func TestRecordOutcomeAppendsEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	withSink(t, path)

	Record("channel-switch", map[string]string{"channel": "testing"},
		[]string{"pkexec", "/usr/bin/chairlift-helper", "channel-switch", "testing"}, SuppressedNone)

	executed := [][]string{{"bootc", "switch", "--enforce-container-sigpolicy", "ghcr.io/projectbluefin/dakota:testing"}}
	RecordOutcome("channel-switch", OutcomeSucceeded, nil, executed)

	code126 := 126
	Record("restart", nil, []string{"pkexec", "/usr/bin/chairlift-helper", "restart"}, SuppressedNone)
	RecordOutcome("restart", OutcomeRefused, &code126, nil)

	code1 := 1
	Record("rollback", nil, []string{"pkexec", "/usr/bin/chairlift-helper", "rollback"}, SuppressedNone)
	RecordOutcome("rollback", OutcomeFailed, &code1, [][]string{{"bootc", "rollback"}})

	Record("pin", map[string]string{"day": "20261003"}, []string{"pkexec", "/usr/bin/chairlift-helper", "pin", "20261003"}, SuppressedNone)
	RecordOutcome("pin", OutcomeTimedOut, nil, nil)

	Record("factory-reset", nil, []string{"pkexec", "/usr/bin/chairlift-helper", "factory-reset"}, SuppressedNone)
	RecordOutcome("factory-reset", OutcomeCancelled, nil, nil)

	twoCmds := [][]string{
		{"usermod", "-aG", "docker", "testuser"},
		{"systemctl", "enable", "--now", "docker.socket", "docker.service"},
	}
	Record("docker-enable", nil, []string{"pkexec", "/usr/bin/chairlift-helper", "docker-enable"}, SuppressedNone)
	RecordOutcome("docker-enable", OutcomeSucceeded, nil, twoCmds)

	entries := readEntries(t, path)
	if len(entries) != 12 {
		t.Fatalf("journal has %d entries, want 12", len(entries))
	}

	// Verify outcome entries
	out1 := entries[1]
	if out1.Seq != 2 || out1.Action != "channel-switch" || out1.Outcome != OutcomeSucceeded {
		t.Errorf("out1 = %+v, want seq 2, action channel-switch, outcome succeeded", out1)
	}
	if !reflect.DeepEqual(out1.Executed, executed) {
		t.Errorf("out1.Executed = %v, want %v", out1.Executed, executed)
	}
	if out1.ExitCode != nil {
		t.Errorf("out1.ExitCode = %v, want nil", out1.ExitCode)
	}

	out2 := entries[3]
	if out2.Seq != 4 || out2.Action != "restart" || out2.Outcome != OutcomeRefused {
		t.Errorf("out2 = %+v, want seq 4, action restart, outcome refused", out2)
	}
	if out2.ExitCode == nil || *out2.ExitCode != 126 {
		t.Errorf("out2.ExitCode = %v, want 126", out2.ExitCode)
	}

	out3 := entries[5]
	if out3.Seq != 6 || out3.Action != "rollback" || out3.Outcome != OutcomeFailed {
		t.Errorf("out3 = %+v, want seq 6, action rollback, outcome failed", out3)
	}
	if out3.ExitCode == nil || *out3.ExitCode != 1 {
		t.Errorf("out3.ExitCode = %v, want 1", out3.ExitCode)
	}
	if !reflect.DeepEqual(out3.Executed, [][]string{{"bootc", "rollback"}}) {
		t.Errorf("out3.Executed = %v, want [[bootc rollback]]", out3.Executed)
	}

	out4 := entries[7]
	if out4.Seq != 8 || out4.Action != "pin" || out4.Outcome != OutcomeTimedOut {
		t.Errorf("out4 = %+v, want seq 8, action pin, outcome timed-out", out4)
	}

	out5 := entries[9]
	if out5.Seq != 10 || out5.Action != "factory-reset" || out5.Outcome != OutcomeCancelled {
		t.Errorf("out5 = %+v, want seq 10, action factory-reset, outcome cancelled", out5)
	}

	out6 := entries[11]
	if out6.Seq != 12 || out6.Action != "docker-enable" || out6.Outcome != OutcomeSucceeded {
		t.Errorf("out6 = %+v, want seq 12, action docker-enable, outcome succeeded", out6)
	}
	if !reflect.DeepEqual(out6.Executed, twoCmds) {
		t.Errorf("out6.Executed = %v, want %v", out6.Executed, twoCmds)
	}
}

func TestRecordOutcomeWithNoSinkIsNoOp(t *testing.T) {
	t.Setenv(PathEnv, "")
	Reset()
	t.Cleanup(Reset)

	RecordOutcome("restart", OutcomeSucceeded, nil, nil)
	// No panic, no write
}

func TestRecordOutcomeToleratesAnUnwritableSink(t *testing.T) {
	withSink(t, filepath.Join(t.TempDir(), "does-not-exist", "journal.jsonl"))

	RecordOutcome("restart", OutcomeSucceeded, nil, [][]string{{"systemctl", "reboot"}})
	// No panic, no write
}
