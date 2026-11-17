package livery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/deskenv"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestKickoffAppletGroupsFindsEveryKickoffInstance(t *testing.T) {
	data := []byte(`[Containments][1]
t plugin=org.kde.plasma.folder
[Containments][1][Applets][2]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][2][Configuration]
icon=old-icon
[Containments][3][Applets][4]
plugin=org.kde.plasma.kickoff
[Containments][3][Applets][5]
plugin=org.kde.plasma.taskmanager
[Containments][6][Applets][7]
plugin=org.kde.plasma.kickoff
`)
	got := kickoffAppletGroups(data)
	want := [][]string{
		{"Containments", "1", "Applets", "2", "Configuration", "General"},
		{"Containments", "3", "Applets", "4", "Configuration", "General"},
		{"Containments", "6", "Applets", "7", "Configuration", "General"},
	}
	if len(got) != len(want) {
		t.Fatalf("kickoffAppletGroups() = %v, want %v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], "/") != strings.Join(want[i], "/") {
			t.Errorf("group %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestKickoffAppletGroupsRejectsMalformedGroupPaths(t *testing.T) {
	data := []byte(`[Containments][../../etc][Applets][2]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][2][Configuration]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][2]
plugin=org.kde.plasma.taskmanager
`)
	if got := kickoffAppletGroups(data); len(got) != 0 {
		t.Errorf("kickoffAppletGroups() = %v, want no groups", got)
	}
}

func TestAppGridAvailabilityRequiresKickoffOnKDE(t *testing.T) {
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })

	available, err := AppGridAvailable()
	if err != nil || available {
		t.Fatalf("AppGridAvailable() without applet = %t, %v; want false, nil", available, err)
	}

	if err := os.WriteFile(config, []byte("[Containments][1][Applets][2]\nplugin=org.kde.plasma.kickoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	available, err = AppGridAvailable()
	if err != nil || !available {
		t.Fatalf("AppGridAvailable() with Kickoff = %t, %v; want true, nil", available, err)
	}
}

func TestAppGridAvailabilityKeepsGNOMESurface(t *testing.T) {
	useDesktop(t, deskenv.GNOME)
	available, err := AppGridAvailable()
	if err != nil || !available {
		t.Fatalf("AppGridAvailable() on GNOME = %t, %v; want true, nil", available, err)
	}
}

func TestApplyKDEAppGridWritesEveryKickoffIcon(t *testing.T) {
	dir := useTempDataHome(t)
	fake := newFakeCommands(t)
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })
	if err := os.WriteFile(config, []byte(`[Containments][1][Applets][2]
plugin=org.kde.plasma.kickoff
[Containments][3][Applets][4]
plugin=org.kde.plasma.kickoff
`), 0o600); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(t.TempDir(), "mark.svg")
	if err := os.WriteFile(art, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Apply(context.Background(), AppGrid, Source{Kind: FromFile, Value: art}); err != nil {
		t.Fatalf("Apply(KDE, AppGrid): %v", err)
	}

	iconPath := filepath.Join(dir, "icons", "hicolor", "scalable", "apps", KickoffIconName+".svg")
	if _, err := os.Stat(iconPath); err != nil {
		t.Fatalf("Apply did not install the Kickoff icon at %s: %v", iconPath, err)
	}
	for _, call := range []string{
		"kwriteconfig6 --file " + config + " --group Containments --group 1 --group Applets --group 2 --group Configuration --group General --key icon " + KickoffIconName,
		"kwriteconfig6 --file " + config + " --group Containments --group 3 --group Applets --group 4 --group Configuration --group General --key icon " + KickoffIconName,
	} {
		if !fake.sawPrefix(call) {
			t.Errorf("Apply did not run %q; calls: %v", call, fake.calls)
		}
	}
}

func TestApplyKDEAppGridReportsMissingKickoff(t *testing.T) {
	useTempDataHome(t)
	fake := newFakeCommands(t)
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })

	err := Apply(context.Background(), AppGrid, Source{Kind: FromFile, Value: filepath.Join(t.TempDir(), "missing.svg")})
	if !errors.Is(err, ErrKickoffUnavailable) {
		t.Fatalf("Apply without Kickoff error = %v, want ErrKickoffUnavailable", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("Apply without Kickoff ran commands: %s", strings.Join(fake.calls, "; "))
	}
}

func TestApplyKDEAppGridDryRunDoesNotWrite(t *testing.T) {
	dir := useTempDataHome(t)
	fake := newFakeCommands(t)
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })
	if err := os.WriteFile(config, []byte("[Containments][1][Applets][2]\nplugin=org.kde.plasma.kickoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(t.TempDir(), "mark.svg")
	if err := os.WriteFile(art, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o600); err != nil {
		t.Fatal(err)
	}
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := Apply(context.Background(), AppGrid, Source{Kind: FromFile, Value: art}); err != nil {
		t.Fatalf("Apply dry-run: %v", err)
	}
	if fake.sawPrefix("kwriteconfig6 ") {
		t.Errorf("dry-run executed kwriteconfig6: %v", fake.calls)
	}
	iconPath := filepath.Join(dir, "icons", "hicolor", "scalable", "apps", KickoffIconName+".svg")
	if _, err := os.Stat(iconPath); !os.IsNotExist(err) {
		t.Errorf("dry-run installed %s", iconPath)
	}
}

func TestApplyKDEAppGridReportsKwriteconfigFailure(t *testing.T) {
	useTempDataHome(t)
	fake := newFakeCommands(t)
	fake.fail["kwriteconfig6"] = errors.New("write rejected")
	fake.reply["kwriteconfig6"] = "permission denied"
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })
	if err := os.WriteFile(config, []byte("[Containments][1][Applets][2]\nplugin=org.kde.plasma.kickoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(t.TempDir(), "mark.svg")
	if err := os.WriteFile(art, []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`), 0o600); err != nil {
		t.Fatal(err)
	}

	err := Apply(context.Background(), AppGrid, Source{Kind: FromFile, Value: art})
	if err == nil || !strings.Contains(err.Error(), "permission denied") || !strings.Contains(err.Error(), "Containments/1/Applets/2/Configuration/General") {
		t.Fatalf("Apply error = %v, want command failure and applet group", err)
	}
}

func TestKwriteconfig6IsInTheClosedCommandSet(t *testing.T) {
	if !allowedCommands["kwriteconfig6"] {
		t.Fatal("kwriteconfig6 is missing from the livery command allowlist")
	}
}

func TestClearKDEAppGridDeletesKickoffIcon(t *testing.T) {
	useTempDataHome(t)
	fake := newFakeCommands(t)
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })

	// Applet 2 has KickoffIconName -> should be deleted.
	// Applet 4 has a custom icon -> should NOT be deleted.
	// Applet 6 has no icon -> should NOT be deleted.
	// Applet 8 is folder plugin, has KickoffIconName -> should NOT be deleted.
	data := `[Containments][1][Applets][2]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][2][Configuration][General]
icon=` + KickoffIconName + `
[Containments][1][Applets][4]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][4][Configuration][General]
icon=user-custom-icon
[Containments][1][Applets][6]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][8]
plugin=org.kde.plasma.folder
[Containments][1][Applets][8][Configuration][General]
icon=` + KickoffIconName + `
`
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Clear(context.Background(), AppGrid); err != nil {
		t.Fatalf("Clear(AppGrid): %v", err)
	}

	wantCall := "kwriteconfig6 --file " + config + " --group Containments --group 1 --group Applets --group 2 --group Configuration --group General --key icon --delete"
	if !fake.sawPrefix(wantCall) {
		t.Errorf("Clear did not run %q; calls: %v", wantCall, fake.calls)
	}

	unwantedCall := "kwriteconfig6 --file " + config + " --group Containments --group 1 --group Applets --group 4"
	if fake.sawPrefix(unwantedCall) {
		t.Errorf("Clear modified applet 4 which has custom icon; calls: %v", fake.calls)
	}
}

func TestClearKDEAppGridDryRunDoesNotDeleteKickoffIcon(t *testing.T) {
	useTempDataHome(t)
	fake := newFakeCommands(t)
	useDesktop(t, deskenv.KDE)
	config := filepath.Join(t.TempDir(), "plasma-org.kde.plasma.desktop-appletsrc")
	original := kickoffConfigFile
	kickoffConfigFile = func() (string, error) { return config, nil }
	t.Cleanup(func() { kickoffConfigFile = original })

	data := `[Containments][1][Applets][2]
plugin=org.kde.plasma.kickoff
[Containments][1][Applets][2][Configuration][General]
icon=` + KickoffIconName + `
`
	if err := os.WriteFile(config, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}

	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })

	if err := Clear(context.Background(), AppGrid); err != nil {
		t.Fatalf("Clear(AppGrid) dry-run: %v", err)
	}

	if fake.sawPrefix("kwriteconfig6 ") {
		t.Errorf("dry-run executed kwriteconfig6: %v", fake.calls)
	}
}
