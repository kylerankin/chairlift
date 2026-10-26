package livery

import (
	"context"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
)

func TestDamaskInstalledReportsPresenceInEitherScope(t *testing.T) {
	original := damaskScopes
	t.Cleanup(func() { damaskScopes = original })

	t.Run("present in user scope", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) {
				return []flatpak.Application{{ApplicationID: "org.other.App"}}, nil
			},
			func() ([]flatpak.Application, error) {
				return []flatpak.Application{{ApplicationID: DamaskID}}, nil
			},
		}
		if !DamaskInstalled(context.Background()) {
			t.Fatal("expected Damask reported installed from the system scope")
		}
	})

	t.Run("absent in both scopes returns false", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) { return nil, nil },
			func() ([]flatpak.Application, error) { return nil, nil },
		}
		if DamaskInstalled(context.Background()) {
			t.Fatal("expected Damask reported absent")
		}
	})

	t.Run("a query error is treated as absent, not fatal", func(t *testing.T) {
		damaskScopes = []func() ([]flatpak.Application, error){
			func() ([]flatpak.Application, error) { return nil, &flatpak.Error{Message: "flatpak not found"} },
			func() ([]flatpak.Application, error) { return nil, nil },
		}
		if DamaskInstalled(context.Background()) {
			t.Fatal("expected a failed scope query to be skipped")
		}
	})
}

func TestDamaskInstallRespectsDryRun(t *testing.T) {
	newFakeCommands(t)
	useTempDataHome(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	original := damaskScopes
	damaskScopes = []func() ([]flatpak.Application, error){
		func() ([]flatpak.Application, error) { return nil, nil },
	}
	t.Cleanup(func() { damaskScopes = original })

	if err := DamaskInstall(context.Background()); err != nil {
		t.Fatalf("dry-run install should succeed: %v", err)
	}
}

func TestDamaskOpenUsesDesktopExecution(t *testing.T) {
	newFakeCommands(t)
	useTempDataHome(t)
	original := launchDamask
	t.Cleanup(func() { launchDamask = original })

	var gotURI string
	launchDamask = func(_ context.Context) error {
		gotURI = "app://" + DamaskID
		return nil
	}
	if err := DamaskOpen(context.Background()); err != nil {
		t.Fatalf("DamaskOpen should launch via desktop execution: %v", err)
	}
	if gotURI != "app://"+DamaskID {
		t.Fatalf("launch URI = %q, want app://%s", gotURI, DamaskID)
	}
}
