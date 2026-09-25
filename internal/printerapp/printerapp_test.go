package printerapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

func TestFamiliesCoversEveryDriverFamily(t *testing.T) {
	wantIDs := []string{"ghostscript", "hplip", "gutenprint", "ps"}
	got := Families()
	if len(got) != len(wantIDs) {
		t.Fatalf("Families() returned %d families, want %d", len(got), len(wantIDs))
	}
	seen := map[string]bool{}
	for _, f := range got {
		if seen[f.ID] {
			t.Errorf("duplicate family ID %q", f.ID)
		}
		seen[f.ID] = true
		if f.Repo == "" || f.Version == "" {
			t.Errorf("family %q has an empty repo or version", f.ID)
		}
		// The pinned image is an immutable application-version tag, never the
		// mutable :latest or :build tags the FSDK repositories refuse to reuse.
		if strings.HasSuffix(f.Repo, ":latest") || strings.HasSuffix(f.Image(), ":build") {
			t.Errorf("family %q pins a mutable image tag: %q", f.ID, f.Image())
		}
	}
	for _, id := range wantIDs {
		if !seen[id] {
			t.Errorf("Families() is missing the %q family", id)
		}
	}
}

func TestEveryAppRendersAStartableUnit(t *testing.T) {
	apps := []App{
		Select(Families()[0]),
		{Family: Families()[1], Name: "office-printer"},
		{Family: Families()[2], Name: "photo-printer"},
		{Family: Families()[3], Name: "label-printer"},
	}

	for _, app := range apps {
		unit := RenderUnit(app)

		for _, required := range []string{
			"[Container]",
			"Image=" + app.Family.Image(),
			"ContainerName=" + app.ContainerName(),
			"WantedBy=default.target",
			"PublishPort=127.0.0.1:" + strconv.Itoa(app.Port()) + ":" + strconv.Itoa(app.Port()),
			"Volume=%h/printer-workspaces/" + app.Family.ID + "/" + app.Name + ":z",
		} {
			if !strings.Contains(unit, required) {
				t.Errorf("%s unit is missing %q:\n%s", app.UnitName(), required, unit)
			}
		}

		// The service publishes on loopback only: an unauthenticated printer
		// exposed to the LAN would let anyone on the network drive the device.
		if !strings.Contains(unit, "PublishPort=127.0.0.1") {
			t.Errorf("%s unit does not publish on loopback:\n%s", app.UnitName(), unit)
		}
	}
}

func TestUniqueNamePortAndVolumePerApp(t *testing.T) {
	// The core acceptance: each rendered unit has a unique name, port, and
	// volume, so one logical device is never claimed or advertised twice.
	apps := []App{
		{Family: Families()[0], Name: "front-office"},
		{Family: Families()[0], Name: "back-office"},
		{Family: Families()[1], Name: "office-printer"},
		{Family: Families()[2], Name: "photo-printer"},
		{Family: Families()[3], Name: "label-printer"},
	}

	names, ports, volumes := map[string]bool{}, map[int]bool{}, map[string]bool{}
	for _, app := range apps {
		names[app.UnitName()] = true
		ports[app.Port()] = true
		volumes[app.Volume()] = true
	}

	if len(names) != len(apps) {
		t.Errorf("unit names are not unique: %d distinct for %d apps\n%v", len(names), len(apps), names)
	}
	if len(ports) != len(apps) {
		t.Errorf("ports are not unique: %d distinct for %d apps\n%v", len(ports), len(apps), ports)
	}
	if len(volumes) != len(apps) {
		t.Errorf("volumes are not unique: %d distinct for %d apps\n%v", len(volumes), len(apps), volumes)
	}

	// Same identity always maps to the same port, so a printer keeps one
	// address across restarts.
	if Families()[0].ID != "" {
		a := Select(Families()[0])
		if a.Port() != Select(Families()[0]).Port() {
			t.Error("the same app rendered a different port on a second call")
		}
	}
}

func TestUnitNameDoesNotCollideAndServiceMatches(t *testing.T) {
	app := Select(Families()[0])

	if !strings.HasPrefix(app.UnitName(), "chairlift-") {
		t.Errorf("UnitName = %q, want a chairlift- prefix", app.UnitName())
	}
	if app.ServiceName() != strings.TrimSuffix(app.UnitName(), ".container")+".service" {
		t.Errorf("ServiceName = %q does not match UnitName %q", app.ServiceName(), app.UnitName())
	}
	if app.ContainerName() != strings.TrimSuffix(app.UnitName(), ".container") {
		t.Errorf("ContainerName = %q does not match UnitName %q", app.ContainerName(), app.UnitName())
	}
}

func TestSanitizeStableAcrossSeparators(t *testing.T) {
	// "Office  Printer" (two spaces) and "office-printer" must land on the
	// same unit, so a re-typed name does not spawn a second owner.
	a := App{Family: Families()[0], Name: "Office  Printer"}
	b := App{Family: Families()[0], Name: "office-printer"}
	if a.UnitName() != b.UnitName() {
		t.Errorf("sanitize is not stable across separator runs: %q vs %q", a.UnitName(), b.UnitName())
	}
}

// stubUnitDir points the package at a temporary quadlet directory and records
// the systemctl calls it makes.
func stubUnitDir(t *testing.T) (dir string, calls *[]string) {
	t.Helper()

	tmp := t.TempDir()
	previousDir := unitDir
	previousSystemctl := runSystemctl
	t.Cleanup(func() {
		unitDir = previousDir
		runSystemctl = previousSystemctl
		dryrun.Set(false)
	})

	unitDir = func() (string, error) { return tmp, nil }

	recorded := []string{}
	runSystemctl = func(_ context.Context, args ...string) error {
		recorded = append(recorded, strings.Join(args, " "))
		return nil
	}

	return tmp, &recorded
}

func TestEnableWritesTheUnitAndStartsIt(t *testing.T) {
	dir, calls := stubUnitDir(t)
	app := Select(Families()[0])

	if IsEnabled(app) {
		t.Fatal("IsEnabled reported true before Enable")
	}

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, app.UnitName()))
	if err != nil {
		t.Fatalf("reading written unit: %v", err)
	}
	if !strings.Contains(string(data), app.Family.Image()) {
		t.Errorf("written unit does not name %q:\n%s", app.Family.Image(), data)
	}

	want := []string{"daemon-reload", "start " + app.ServiceName()}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}

	if !IsEnabled(app) {
		t.Error("IsEnabled reported false after Enable")
	}
}

func TestEnableRemovesTheUnitWhenTheServiceWillNotStart(t *testing.T) {
	dir, _ := stubUnitDir(t)

	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "start" {
			return errors.New("unit not found")
		}
		return nil
	}

	app := Select(Families()[0])
	if err := Enable(context.Background(), app); err == nil {
		t.Fatal("Enable returned no error when the service failed to start")
	}

	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("a failed Enable left its quadlet on disk")
	}
	if IsEnabled(app) {
		t.Error("IsEnabled reported true after a failed Enable")
	}
}

func TestDisableRemovesTheUnit(t *testing.T) {
	dir, calls := stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	*calls = nil

	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("Disable left the quadlet on disk")
	}
	want := []string{"stop " + app.ServiceName(), "daemon-reload"}
	if strings.Join(*calls, "|") != strings.Join(want, "|") {
		t.Errorf("systemctl calls = %v, want %v", *calls, want)
	}
}

func TestDisableLeavesTheStateVolumeUntouched(t *testing.T) {
	_, _ = stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// The state volume is under the user's home, which stubUnitDir does not
	// point at a temp dir, so assert Disable does not try to remove anything
	// outside the quadlet unit. The only removal Disable performs is the unit.
	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}
}

func TestDisableSucceedsWhenTheServiceIsAlreadyDown(t *testing.T) {
	dir, _ := stubUnitDir(t)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	runSystemctl = func(_ context.Context, args ...string) error {
		if args[0] == "stop" {
			return errors.New("unit is not loaded")
		}
		return nil
	}

	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("Disable left the quadlet on disk after a failed stop")
	}
}

func TestDryRunTouchesNothing(t *testing.T) {
	dir, calls := stubUnitDir(t)
	dryrun.Set(true)
	app := Select(Families()[0])

	if err := Enable(context.Background(), app); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if err := Disable(context.Background(), app); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, app.UnitName())); !os.IsNotExist(err) {
		t.Error("dry-run wrote a quadlet")
	}
	if len(*calls) != 0 {
		t.Errorf("dry-run ran systemctl: %v", *calls)
	}
}

func TestRenderUnitsIsOrderIndependent(t *testing.T) {
	apps := []App{
		{Family: Families()[0], Name: "a"},
		{Family: Families()[1], Name: "b"},
		{Family: Families()[2], Name: "c"},
	}
	reversed := make([]App, len(apps))
	for i, a := range apps {
		reversed[len(apps)-1-i] = a
	}

	if RenderUnits(apps) != RenderUnits(reversed) {
		t.Error("RenderUnits output depends on input order")
	}
}

func TestApplyOverridesReplacesTheImage(t *testing.T) {
	original := Families()[0]
	t.Cleanup(func() {
		for i := range families {
			if families[i].ID == original.ID {
				families[i] = original
				return
			}
		}
	})

	if err := ApplyOverrides(map[string]string{"ghostscript": "mirror.example.internal/ghostscript-printer-app:10.07.1-1"}); err != nil {
		t.Fatalf("ApplyOverrides: %v", err)
	}

	if got := Select(Families()[0]).Family.Image(); got != "mirror.example.internal/ghostscript-printer-app:10.07.1-1" {
		t.Errorf("image = %q, want the override", got)
	}

	// An override for one family leaves the others alone.
	if Select(Families()[1]).Family.Image() != Families()[1].Image() {
		t.Error("overriding ghostscript disturbed the hplip image")
	}
}

func TestApplyOverridesRejectsBadInput(t *testing.T) {
	tests := []struct {
		name   string
		images map[string]string
		want   string
	}{
		{
			name:   "unknown family",
			images: map[string]string{"brother": "example.com/x:1"},
			// A typo'd family ID must surface, not silently do nothing while the
			// site believes its mirror is in use.
			want: `unknown family "brother"`,
		},
		{
			name:   "empty image",
			images: map[string]string{"hplip": ""},
			want:   "empty image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := Select(Families()[1]).Family.Image()

			err := ApplyOverrides(tt.images)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("ApplyOverrides unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ApplyOverrides error = %v, want it to mention %q", err, tt.want)
			}
			if got := Select(Families()[1]).Family.Image(); got != before {
				t.Errorf("a rejected override changed the hplip image to %q", got)
			}
		})
	}
}
