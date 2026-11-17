package legacydesktop

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestCleanupRemovesMatchingFile(t *testing.T) {
	cases := []struct {
		name string
		exec string
	}{
		{
			name: "wrapper path",
			exec: "/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper",
		},
		{
			name: "binary path",
			exec: "/usr/bin/chairlift",
		},
		{
			name: "bare wrapper",
			exec: "chairlift-wrapper",
		},
		{
			name: "bare binary",
			exec: "chairlift",
		},
		{
			name: "quoted binary with args",
			exec: "\"chairlift-wrapper\" --some-flag",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir := t.TempDir()
			t.Setenv("XDG_DATA_HOME", tempDir)

			appsDir := filepath.Join(tempDir, "applications")
			if err := os.MkdirAll(appsDir, 0o755); err != nil {
				t.Fatalf("failed to create applications dir: %v", err)
			}

			desktopPath := filepath.Join(appsDir, LegacyDesktopFilename)
			content := "[Desktop Entry]\nName=ChairLift\nType=Application\nExec=" + tc.exec + "\n"
			if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
				t.Fatalf("failed to write desktop file: %v", err)
			}

			if err := Clean(); err != nil {
				t.Fatalf("Clean() failed: %v", err)
			}

			if _, err := os.Lstat(desktopPath); !os.IsNotExist(err) {
				t.Errorf("expected %s to be removed, but stat returned %v", desktopPath, err)
			}
		})
	}
}

func TestCleanupKeepsNonMatchingExec(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	appsDir := filepath.Join(tempDir, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("failed to create applications dir: %v", err)
	}

	desktopPath := filepath.Join(appsDir, LegacyDesktopFilename)
	content := "[Desktop Entry]\nName=OtherApp\nType=Application\nExec=/usr/bin/otherapp\n"
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write desktop file: %v", err)
	}

	if err := Clean(); err != nil {
		t.Fatalf("Clean() failed: %v", err)
	}

	if _, err := os.Lstat(desktopPath); err != nil {
		t.Errorf("expected %s to be kept, but stat returned %v", desktopPath, err)
	}
}

func TestCleanupKeepsSymlink(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	appsDir := filepath.Join(tempDir, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("failed to create applications dir: %v", err)
	}

	realFile := filepath.Join(tempDir, "target.desktop")
	content := "[Desktop Entry]\nName=ChairLift\nType=Application\nExec=chairlift-wrapper\n"
	if err := os.WriteFile(realFile, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write real file: %v", err)
	}

	symlinkPath := filepath.Join(appsDir, LegacyDesktopFilename)
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	if err := Clean(); err != nil {
		t.Fatalf("Clean() failed: %v", err)
	}

	info, err := os.Lstat(symlinkPath)
	if err != nil {
		t.Fatalf("expected symlink %s to be kept, but stat returned %v", symlinkPath, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to remain a symlink", symlinkPath)
	}
}

func TestCleanupMissingFileIsNoOp(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	if err := Clean(); err != nil {
		t.Fatalf("Clean() with missing applications dir returned error: %v", err)
	}

	appsDir := filepath.Join(tempDir, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("failed to create applications dir: %v", err)
	}

	if err := Clean(); err != nil {
		t.Fatalf("Clean() with missing desktop file returned error: %v", err)
	}
}

func TestCleanupDryRunPreservesFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	appsDir := filepath.Join(tempDir, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("failed to create applications dir: %v", err)
	}

	desktopPath := filepath.Join(appsDir, LegacyDesktopFilename)
	content := "[Desktop Entry]\nName=ChairLift\nType=Application\nExec=/home/linuxbrew/.linuxbrew/bin/chairlift-wrapper\n"
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write desktop file: %v", err)
	}

	dryrun.Set(true)
	defer dryrun.Set(false)

	if err := Clean(); err != nil {
		t.Fatalf("Clean() in dry-run mode returned error: %v", err)
	}

	if _, err := os.Lstat(desktopPath); err != nil {
		t.Errorf("expected %s to be preserved under dry-run, got %v", desktopPath, err)
	}
}

func TestCleanupKeepsNonApplicationType(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tempDir)

	appsDir := filepath.Join(tempDir, "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("failed to create applications dir: %v", err)
	}

	desktopPath := filepath.Join(appsDir, LegacyDesktopFilename)
	content := "[Desktop Entry]\nName=ChairLift\nType=Link\nURL=https://example.com\nExec=chairlift-wrapper\n"
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write desktop file: %v", err)
	}

	if err := Clean(); err != nil {
		t.Fatalf("Clean() failed: %v", err)
	}

	if _, err := os.Lstat(desktopPath); err != nil {
		t.Errorf("expected %s to be kept, but stat returned %v", desktopPath, err)
	}
}

func TestCleanupFallsBackToHomeWhenXDGUnset(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", tempDir)

	appsDir := filepath.Join(tempDir, ".local", "share", "applications")
	if err := os.MkdirAll(appsDir, 0o755); err != nil {
		t.Fatalf("failed to create applications dir: %v", err)
	}

	desktopPath := filepath.Join(appsDir, LegacyDesktopFilename)
	content := "[Desktop Entry]\nType=Application\nExec=chairlift\n"
	if err := os.WriteFile(desktopPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write desktop file: %v", err)
	}

	if err := Clean(); err != nil {
		t.Fatalf("Clean() failed: %v", err)
	}

	if _, err := os.Lstat(desktopPath); !os.IsNotExist(err) {
		t.Errorf("expected %s to be removed, got %v", desktopPath, err)
	}
}

func TestParseExecProgram(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"   ", ""},
		{"chairlift", "chairlift"},
		{"/usr/bin/chairlift", "/usr/bin/chairlift"},
		{"/usr/bin/chairlift %u", "/usr/bin/chairlift"},
		{"\"/path with spaces/chairlift-wrapper\" --arg", "/path with spaces/chairlift-wrapper"},
		{"\"chairlift\"", "chairlift"},
		{"\"unclosed", "unclosed"},
	}

	for _, tc := range cases {
		got := parseExecProgram(tc.input)
		if got != tc.want {
			t.Errorf("parseExecProgram(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
