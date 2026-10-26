package livery

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
)

// DamaskID is the third-party Flatpak application id of the Damask wallpaper
// manager. ChairLift ships no artwork and no rotation for it: it only offers
// to install and launch the app, never installing software on the user's
// behalf without an explicit action. The id follows the Bluefin convention of
// reverse-domain app ids.
const DamaskID = "app.drey.Damask"

// DamaskFlag is the command-line flag that launches Damask.
const DamaskFlag = "--damask"

// damaskScopes are the Flatpak scopes DamaskInstalled consults, user first.
// It is a variable so a test can point it at a fixture list.
var damaskScopes = []func() ([]flatpak.Application, error){
	flatpak.ListUserApplications,
	flatpak.ListSystemApplications,
}

// DamaskInstalled reports whether the external Damask Flatpak is present in
// either the user or system scope. A query error — for example Flatpak not
// installed — is reported as not available rather than propagated: the
// Wallpapers group shows the Damask control only when Damask is available, and
// a missing Flatpak is simply not available.
func DamaskInstalled(ctx context.Context) bool {
	for _, list := range damaskScopes {
		apps, err := list()
		if err != nil {
			continue
		}
		for _, app := range apps {
			if app.ApplicationID == DamaskID {
				return true
			}
		}
	}
	return false
}

// DamaskInstall installs Damask into the user scope from the default remote
// (Flathub). It is a no-op when Damask is already installed, and it respects
// the dry-run gate.
func DamaskInstall(ctx context.Context) error {
	if DamaskInstalled(ctx) {
		log.Printf("livery: %s already installed", DamaskID)
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would install %s from Flathub (user scope)", DamaskID)
		return nil
	}
	return flatpak.Install(DamaskID, true)
}

// launchDamask runs the desktop execution that opens Damask. It is a variable
// so a test can assert the app:// URI without a real launcher on PATH.
var launchDamask = func(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "xdg-open", "app://"+DamaskID)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("livery: launching %s: %w: %s", DamaskID, err, string(out))
	}
	return nil
}

// DamaskOpen launches Damask via desktop execution. The Flatpak publishes an
// app:// URI the desktop environment handles, so ChairLift hands off to
// xdg-open rather than exec'ing the sandboxed binary directly. It is a no-op
// under the dry-run gate.
func DamaskOpen(ctx context.Context) error {
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] Would launch %s", DamaskID)
		return nil
	}
	return launchDamask(ctx)
}
