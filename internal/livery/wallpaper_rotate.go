package livery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// WallpaperRotateFlag is the command-line flag that runs one wallpaper
// rotation pass. It is the service unit's entire ExecStart, mirroring
// RotateFlag for the icon rotation.
const WallpaperRotateFlag = "--rotate-wallpaper"

// WallpaperServiceName and WallpaperTimerName are the systemd user units that
// perform rotation. The service is a oneshot that runs one pass; the timer
// fires it once a day and once after boot, so a picture changes whether or
// not the user logs in at midnight.
const (
	WallpaperServiceName = "chairlift-livery-wallpaper-rotate.service"
	WallpaperTimerName   = "chairlift-livery-wallpaper-rotate.timer"
)

var (
	wallpaperServiceTemplate = `[Unit]
Description=%s Wallpaper rotation
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=oneshot
%sExecStart=%s %s

[Install]
WantedBy=graphical-session.target
`
	wallpaperTimerTemplate = `[Unit]
Description=%s Wallpaper rotation timer

[Timer]
OnCalendar=daily
OnBootSec=1min
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`
)

// restorePrior restores one previously-existing unit file, or removes the path
// when there was nothing there to begin with. It is the inverse of the write
// an install or remove performed, so a failed step leaves the disk as the
// user had it.
func restorePrior(ctx context.Context, path string, existed bool, data []byte) error {
	if !existed {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writeFileAtomically(path, data)
}

// wallpaperUnitPath returns the absolute path of the wallpaper service unit.
func wallpaperUnitPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", WallpaperServiceName), nil
}

// wallpaperTimerPath returns the absolute path of the wallpaper timer unit.
func wallpaperTimerPath() (string, error) {
	dir, err := configHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "systemd", "user", WallpaperTimerName), nil
}

// readPrior reads one unit file, reporting whether it existed.
func readPrior(path string) (data []byte, existed bool, err error) {
	data, err = os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return data, true, nil
}

// renderWallpaperUnits returns the paths and bytes InstallWallpaperRotation
// would write for the service and timer units.
func renderWallpaperUnits() (servicePath, timerPath, serviceUnit, timerUnit string, err error) {
	exe, err := unitExecutable()
	if err != nil {
		return "", "", "", "", fmt.Errorf("livery: locating the application executable: %w", err)
	}
	servicePath, err = wallpaperUnitPath()
	if err != nil {
		return "", "", "", "", err
	}
	timerPath, err = wallpaperTimerPath()
	if err != nil {
		return "", "", "", "", err
	}
	var env string
	if dir := os.Getenv("GSETTINGS_SCHEMA_DIR"); dir != "" {
		quoted, err := systemdQuote("GSETTINGS_SCHEMA_DIR=" + dir)
		if err != nil {
			return "", "", "", "", fmt.Errorf("livery: carrying GSETTINGS_SCHEMA_DIR into the rotation unit: %w", err)
		}
		env = "Environment=" + quoted + "\n"
	}
	quotedExe, err := systemdQuote(exe)
	if err != nil {
		return "", "", "", "", fmt.Errorf("livery: writing the rotation unit's ExecStart: %w", err)
	}
	serviceUnit = fmt.Sprintf(wallpaperServiceTemplate, branding.AppName, env, quotedExe, WallpaperRotateFlag)
	timerUnit = fmt.Sprintf(wallpaperTimerTemplate, branding.AppName, WallpaperServiceName)
	return servicePath, timerPath, serviceUnit, timerUnit, nil
}

// RotateWallpaper advances the selected image within the active channel and
// writes it to GNOME's background schema.
//
// It is a no-op when rotation is off, when no channel has been chosen, or when
// GNOME does not own the background schema on this host — in each case there
// is nothing to rotate, and failing would only make the unit exit non-zero at
// every run. It advances the catalog only: a channel with no resolvable image
// is skipped, and the session token is recorded regardless so a permanently
// unavailable image cannot re-advance on every retry.
func RotateWallpaper(ctx context.Context) error {
	state, err := Load(ctx)
	if err != nil {
		return err
	}
	if !state.WallpaperRotate {
		return nil
	}
	if !NativeBackgroundAvailable(ctx) {
		log.Printf("livery: skipping wallpaper rotation: GNOME background schema is not available")
		return nil
	}
	if state.WallpaperChannel == "" {
		return nil
	}

	token := sessionToken(ctx)
	if token != "" && token == state.RotationToken {
		return nil
	}

	channel, ok := ChannelByID(state.WallpaperChannel)
	if !ok || len(channel.Images) == 0 {
		return nil
	}

	var failures []string
	nextID := NextImage(channel.ID, state.WallpaperImage)
	image, ok := ImageByID(channel, nextID)
	if !ok {
		return nil
	}
	file, ok := ResolveImage(image.Path)
	if ok {
		if err := ApplyNativeBackground(ctx, file); err != nil {
			failures = append(failures, err.Error())
		} else if err := SetString(ctx, KeyWallpaperImage, nextID); err != nil {
			failures = append(failures, err.Error())
		}
	} else {
		log.Printf("livery: skipping wallpaper rotation: %q is not available", image.Path)
	}

	// The token is recorded even on failure, so a pass that already ran for
	// this session does not advance again.
	if token != "" {
		if err := SetString(ctx, KeyRotationToken, token); err != nil {
			failures = append(failures, err.Error())
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("livery: wallpaper rotation: %s", strings.Join(failures, "; "))
	}
	return nil
}

// InstallWallpaperRotation writes and enables the rotation timer. It is
// idempotent: rewriting an identical unit and re-enabling an enabled timer are
// both no-ops as far as the user can observe. A failure restores whatever was
// on disk before.
func InstallWallpaperRotation(ctx context.Context) (result error) {
	servicePath, timerPath, serviceUnit, timerUnit, err := renderWallpaperUnits()
	if err != nil {
		return err
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s and %s and enable them", servicePath, timerPath)
		return nil
	}

	type prior struct {
		data    []byte
		existed bool
	}
	priors := map[string]prior{
		servicePath: func() prior { d, e, _ := readPrior(servicePath); return prior{d, e} }(),
		timerPath:   func() prior { d, e, _ := readPrior(timerPath); return prior{d, e} }(),
	}
	defer func() {
		if result != nil {
			if err := restorePrior(ctx, servicePath, priors[servicePath].existed, priors[servicePath].data); err != nil {
				result = errors.Join(result, err)
			}
			if err := restorePrior(ctx, timerPath, priors[timerPath].existed, priors[timerPath].data); err != nil {
				result = errors.Join(result, err)
			}
			_, _ = runCommand(ctx, "systemctl", "--user", "daemon-reload")
		}
	}()

	writes := []struct {
		path string
		data []byte
	}{
		{servicePath, []byte(serviceUnit)},
		{timerPath, []byte(timerUnit)},
	}
	for _, w := range writes {
		if err := writeFileAtomically(w.path, w.data); err != nil {
			return fmt.Errorf("livery: installing %s: %w", filepath.Base(w.path), err)
		}
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "enable", WallpaperTimerName); err != nil {
		return fmt.Errorf("livery: enabling wallpaper rotation: %w: %s", err, strings.TrimSpace(out))
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "start", WallpaperTimerName); err != nil {
		return fmt.Errorf("livery: starting wallpaper rotation: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// RemoveWallpaperRotation disables and deletes the rotation units. A failure
// restores whatever was on disk before.
func RemoveWallpaperRotation(ctx context.Context) (result error) {
	servicePath, err := wallpaperUnitPath()
	if err != nil {
		return err
	}
	timerPath, err := wallpaperTimerPath()
	if err != nil {
		return err
	}

	serviceData, serviceExisted, serviceErr := readPrior(servicePath)
	timerData, timerExisted, timerErr := readPrior(timerPath)
	if serviceErr != nil {
		return serviceErr
	}
	if timerErr != nil {
		return timerErr
	}
	if !serviceExisted && !timerExisted {
		return nil
	}
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would disable and remove %s and %s", servicePath, timerPath)
		return nil
	}

	defer func() {
		if result != nil {
			if err := restorePrior(ctx, servicePath, serviceExisted, serviceData); err != nil {
				result = errors.Join(result, err)
			}
			if err := restorePrior(ctx, timerPath, timerExisted, timerData); err != nil {
				result = errors.Join(result, err)
			}
			_, _ = runCommand(ctx, "systemctl", "--user", "daemon-reload")
		}
	}()

	for _, name := range []string{WallpaperTimerName, WallpaperServiceName} {
		if out, err := runCommand(ctx, "systemctl", "--user", "disable", name); err != nil {
			return fmt.Errorf("livery: disabling %s: %w: %s", name, err, strings.TrimSpace(out))
		}
	}
	for _, path := range []string{servicePath, timerPath} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("livery: removing %s: %w", filepath.Base(path), err)
		}
	}
	if out, err := runCommand(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("livery: reloading user units: %w: %s", err, strings.TrimSpace(out))
	}
	return nil
}

// SyncWallpaperTimer installs the timer when rotation is desired and removes
// it when it is not, so the timer's presence always matches the switch.
func SyncWallpaperTimer(ctx context.Context, s State) error {
	if s.WallpaperRotate {
		return InstallWallpaperRotation(ctx)
	}
	return RemoveWallpaperRotation(ctx)
}

// ConfigureWallpaperRotation commits the rotation preference only after the
// user manager accepted the schedule. A failed operation preserves the last
// persisted choice.
func ConfigureWallpaperRotation(ctx context.Context, desired State) (State, error) {
	previous, err := Load(ctx)
	if err != nil {
		return State{}, err
	}
	if err := SyncWallpaperTimer(ctx, desired); err != nil {
		return previous, err
	}
	if err := SetBool(ctx, KeyWallpaperRotate, desired.WallpaperRotate); err != nil {
		return previous, errors.Join(err, SyncWallpaperTimer(ctx, previous))
	}
	return desired, nil
}
