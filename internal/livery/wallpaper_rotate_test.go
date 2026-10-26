package livery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// wallpaperRotationState wires a fake gsettings layer (the given ChairLift
// wallpaper keys), a fixture wallpaper tree, and a live GNOME background
// schema, so RotateWallpaper sees an available native background.
func wallpaperRotationState(t *testing.T, values, files map[string]string) *fakeCommands {
	t.Helper()
	f := newFakeCommands(t)
	var b strings.Builder
	for key, value := range values {
		b.WriteString(SchemaID + " " + key + " " + value + "\n")
	}
	f.reply["gsettings list-recursively "+SchemaID] = b.String()
	// GNOME owns the background schema, so NativeBackgroundAvailable is true.
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "''"
	withWallpaperRoot(t, files)
	return f
}

func TestRotateWallpaperIsNoOpWhenDisabled(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "false", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("disabled rotation should be a no-op: %v", err)
	}
	if f.sawPrefix("gsettings set org.gnome.desktop.background") {
		t.Fatal("disabled rotation must not change the background")
	}
}

func TestRotateWallpaperAdvancesToNextImage(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "true", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("rotation failed: %v", err)
	}
	if !f.sawPrefix("gsettings set org.gnome.desktop.background picture-uri ") {
		t.Fatal("rotation should apply the next wallpaper to the background schema")
	}
	if !f.sawPrefix("gsettings set " + SchemaID + " " + KeyWallpaperImage) {
		t.Fatal("rotation should persist the new image id")
	}
}

func TestRotateWallpaperIsNoOpWithoutNativeBackground(t *testing.T) {
	f := wallpaperRotationState(t,
		map[string]string{KeyWallpaperRotate: "true", KeyWallpaperChannel: "bluefin", KeyWallpaperImage: "bluefin/aurora.png"},
		map[string]string{"bluefin/aurora.png": "b", "bluefin/moon.png": "b"},
	)
	// A missing schema makes NativeBackgroundAvailable false, so there is
	// nothing to rotate. The fake returns the gsettings error text alongside a
	// nonzero exit so isMissingSchema recognises it.
	f.reply["gsettings get org.gnome.desktop.background picture-uri"] = "No such schema"
	f.fail["gsettings get org.gnome.desktop.background picture-uri"] = errors.New("gsettings: not installed")
	if err := RotateWallpaper(context.Background()); err != nil {
		t.Fatalf("rotation without a native background should be a no-op: %v", err)
	}
	if f.sawPrefix("gsettings set org.gnome.desktop.background") {
		t.Fatal("rotation must not touch the background when the schema is absent")
	}
}

func TestConfigureWallpaperRotationRespectsDryRun(t *testing.T) {
	f := newFakeCommands(t)
	useTempDataHome(t)
	dryrun.Set(true)
	t.Cleanup(func() { dryrun.Set(false) })
	wallpaperRotationState(t, map[string]string{KeyWallpaperRotate: "false"}, map[string]string{})
	if _, err := ConfigureWallpaperRotation(context.Background(), State{WallpaperRotate: true}); err != nil {
		t.Fatalf("dry-run configure should succeed: %v", err)
	}
	if f.sawPrefix("systemctl --user enable") {
		t.Fatal("dry-run configure must not enable the timer")
	}
}
