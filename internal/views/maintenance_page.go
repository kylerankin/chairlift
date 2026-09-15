package views

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/homebrew"
	"github.com/projectbluefin/chairlift/internal/journal"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"

	sgtk "github.com/frostyard/snowkit/gtk"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gtk"
)

// maintenanceWaitDelay bounds how long Wait blocks after the process group has
// been signalled. A configured maintenance script can spawn children (work
// started through pkexec, for example) that inherit its pipes, so a straggler
// could otherwise hold cmd.Run open forever even though the whole group is
// already dead.
const maintenanceWaitDelay = 5 * time.Second

// runMaintenanceCommand runs a configured maintenance script under ctx. The
// script runs in its own process group and cancellation kills the whole
// group, so children the script spawns (including via pkexec) are reaped too
// rather than orphaned and left running after ChairLift reports the timeout.
func runMaintenanceCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = maintenanceWaitDelay
	return cmd.Run()
}

// buildMaintenancePage builds the Maintenance page content
func (uh *UserHome) buildMaintenancePage() {
	page := uh.maintenancePrefsPage
	if page == nil {
		return
	}

	// Cleanup group
	if uh.config.IsGroupEnabled("maintenance_page", "maintenance_cleanup_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("System Cleanup")
		group.SetDescription("Clean up system files and free disk space")

		groupCfg := uh.config.GetGroupConfig("maintenance_page", "maintenance_cleanup_group")
		if groupCfg != nil {
			for _, action := range groupCfg.Actions {
				row := adw.NewActionRow()
				row.SetTitle(action.Title)
				row.SetSubtitle(action.Script)

				if action.Sudo {
					sudoIcon := gtk.NewImageFromIconName("dialog-password-symbolic")
					row.AddPrefix(&sudoIcon.Widget)
				}

				button := gtk.NewButtonWithLabel("Run")
				button.SetValign(gtk.AlignCenterValue)
				button.AddCssClass("suggested-action")

				script := action.Script
				sudo := action.Sudo
				title := action.Title
				btn := button
				clickedCb := func(_ gtk.Button) {
					uh.runMaintenanceAction(title, script, sudo, btn)
				}
				button.ConnectClicked(&clickedCb)

				row.AddSuffix(&button.Widget)
				group.Add(&row.Widget)
			}
		}

		page.Add(group)
	}

	// Homebrew Cleanup group
	if uh.config.IsGroupEnabled("maintenance_page", "maintenance_brew_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Homebrew Cleanup")
		group.SetDescription("Checking Homebrew availability...")
		uh.maintenanceBrewGroup = group

		row := adw.NewActionRow()
		row.SetTitle("Clean Up Homebrew")
		row.SetSubtitle("Remove outdated downloads and old package versions")

		icon := gtk.NewImageFromIconName("user-trash-symbolic")
		row.AddPrefix(&icon.Widget)

		button := gtk.NewButtonWithLabel("Clean Up")
		button.SetValign(gtk.AlignCenterValue)
		button.AddCssClass("suggested-action")

		clickedCb := func(btn gtk.Button) {
			uh.onBrewCleanupClicked(button)
		}
		button.ConnectClicked(&clickedCb)

		row.AddSuffix(&button.Widget)
		group.Add(&row.Widget)

		page.Add(group)

		go func() {
			if !homebrew.IsInstalledCached() {
				sgtk.RunOnMainThread(func() {
					uh.maintenanceBrewGroup.SetVisible(false)
				})
			} else {
				sgtk.RunOnMainThread(func() {
					uh.maintenanceBrewGroup.SetDescription("Remove old versions and clear Homebrew cache")
				})
			}
		}()
	}

	// Flatpak Cleanup group
	if uh.config.IsGroupEnabled("maintenance_page", "maintenance_flatpak_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("Flatpak Cleanup")
		group.SetDescription("Checking Flatpak availability...")
		uh.maintenanceFlatpakGroup = group

		row := adw.NewActionRow()
		row.SetTitle("Remove Unused Runtimes")
		row.SetSubtitle("Uninstall unused Flatpak runtimes and extensions")

		icon := gtk.NewImageFromIconName("user-trash-symbolic")
		row.AddPrefix(&icon.Widget)

		button := gtk.NewButtonWithLabel("Clean Up")
		button.SetValign(gtk.AlignCenterValue)
		button.AddCssClass("suggested-action")

		clickedCb := func(btn gtk.Button) {
			uh.onFlatpakCleanupClicked(button)
		}
		button.ConnectClicked(&clickedCb)

		row.AddSuffix(&button.Widget)
		group.Add(&row.Widget)

		page.Add(group)

		go func() {
			if !flatpak.IsInstalledCached() {
				sgtk.RunOnMainThread(func() {
					uh.maintenanceFlatpakGroup.SetVisible(false)
				})
			} else {
				sgtk.RunOnMainThread(func() {
					uh.maintenanceFlatpakGroup.SetDescription("Remove unused Flatpak runtimes and extensions")
				})
			}
		}()
	}

	// Optimization group
	if uh.config.IsGroupEnabled("maintenance_page", "maintenance_optimization_group") {
		group := adw.NewPreferencesGroup()
		group.SetTitle("System Optimization")
		group.SetDescription("Optimize system performance")

		// Placeholder for optimization features
		row := adw.NewActionRow()
		row.SetTitle("Optimization tools")
		row.SetSubtitle("Coming soon")
		group.Add(&row.Widget)

		page.Add(group)
	}

	// Reset group: irreversible actions, disabled by default in config.yml —
	// see reset.go.
	if uh.config.IsGroupEnabled("maintenance_page", "reset_group") {
		uh.buildResetGroup(page)
	}
}

// onBrewCleanupClicked handles the Homebrew cleanup button click
func (uh *UserHome) onBrewCleanupClicked(button *gtk.Button) {
	button.SetSensitive(false)
	button.SetLabel("Cleaning...")

	go func() {
		output, err := homebrew.Cleanup()

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel("Clean Up")

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Homebrew cleanup failed: %v", err))
				return
			}

			uh.toastAdder.ShowToast(actionmsg.Cleanup(dryrun.Enabled(), "Homebrew", output))
		})
	}()
}

// onFlatpakCleanupClicked handles the Flatpak cleanup button click
func (uh *UserHome) onFlatpakCleanupClicked(button *gtk.Button) {
	button.SetSensitive(false)
	button.SetLabel("Cleaning...")

	go func() {
		output, err := flatpak.UninstallUnused()

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel("Clean Up")

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Flatpak cleanup failed: %v", err))
				return
			}

			uh.toastAdder.ShowToast(actionmsg.Cleanup(dryrun.Enabled(), "Flatpak", output))
		})
	}()
}

// onBrewBundleDumpClicked handles the Homebrew bundle dump button click
func (uh *UserHome) onBrewBundleDumpClicked() {
	go func() {
		homeDir, _ := os.UserHomeDir()
		path := homeDir + "/Brewfile"
		if err := homebrew.BundleDump(path, true); err != nil {
			sgtk.RunOnMainThread(func() {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("Bundle dump failed: %v", err))
			})
			return
		}
		sgtk.RunOnMainThread(func() {
			uh.toastAdder.ShowToast(actionmsg.BundleDump(dryrun.Enabled(), path))
		})
	}()
}

// runMaintenanceAction runs a maintenance action script
func (uh *UserHome) runMaintenanceAction(title, script string, sudo bool, button *gtk.Button) {
	log.Printf("Running action: %s (script: %s, sudo: %v)", title, script, sudo)

	decision := actionmsg.MaintenanceScript(dryrun.Enabled(), title)

	button.SetSensitive(false)
	button.SetLabel("Running...")

	go func() {
		var err error

		// One command value serves both branches, so the journalled argv is
		// the argv a real run executes rather than a separately formatted
		// preview string that could drift from it.
		command := pageview.MaintenanceCommand(script, sudo)
		wouldRun := append([]string{command.Name}, command.Args...)
		suppressed := journal.SuppressedNone
		if !decision.Execute {
			suppressed = journal.SuppressedDryRun
		}
		journal.Record(
			path.Base(script),
			map[string]string{"title": title, "sudo": strconv.FormatBool(sudo)},
			wouldRun,
			suppressed,
		)

		if decision.Execute {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			err = runMaintenanceCommand(ctx, command.Name, command.Args...)
		} else {
			log.Printf("[DRY-RUN] Would execute: %s", strings.Join(wouldRun, " "))
		}

		sgtk.RunOnMainThread(func() {
			button.SetSensitive(true)
			button.SetLabel("Run")

			if err != nil {
				uh.toastAdder.ShowErrorToast(fmt.Sprintf("%s failed: %v", title, err))
				return
			}

			uh.toastAdder.ShowToast(decision.Toast)
		})
	}()
}
