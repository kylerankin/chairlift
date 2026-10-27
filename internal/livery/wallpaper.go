package livery

import (
	"context"
	"log"
	"net/url"
	"path/filepath"
	"strconv"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// NativeBackgroundSchema is the GNOME schema that owns the desktop picture.
// ChairLift does not declare it — it is GNOME's — so writing it requires the
// schema to be installed on the host. That is exactly why selection is
// offered only where NativeBackgroundAvailable reports true: a schema that is
// absent would turn a selection into a silent no-op.
const NativeBackgroundSchema = "org.gnome.desktop.background"

const (
	// pictureURIKey and pictureURIDarkKey are GNOME's light/dark pairs of
	// the same key. Both must move together, or a user whose session flips
	// between colour schemes would see one state updated and the other not.
	pictureURIKey     = "picture-uri"
	pictureURIDarkKey = "picture-uri-dark"
)

// NativeBackgroundAvailable reports whether GNOME owns the desktop-background
// schema on this host. A missing schema returns false, so the page offers
// wallpaper selection only where it can actually apply the picture.
func NativeBackgroundAvailable(ctx context.Context) bool {
	_, err := gsettingsGet(ctx, NativeBackgroundSchema, pictureURIKey)
	return err == nil
}

// ApplyNativeBackground writes file to GNOME's picture-uri and picture-uri-dark
// so the selected collection image becomes the desktop picture in both colour
// schemes. It writes neither key when dry-run is enabled. A missing schema
// surfaces as ErrExtensionMissing, which gsettingsSet maps it to, so the page
// can say so rather than pretending the picture changed.
//
// Both keys hold URIs, not paths: GNOME resolves them with
// g_filename_from_uri, which rejects a bare path, so the absolute path is
// written as a file:// URI quoted as a GVariant string.
func ApplyNativeBackground(ctx context.Context, file string) error {
	value := strconv.Quote(BackgroundURI(file))
	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would set %s %s and %s %s to %s",
			NativeBackgroundSchema, pictureURIKey,
			NativeBackgroundSchema, pictureURIDarkKey, value)
		return nil
	}
	if err := gsettingsSet(ctx, NativeBackgroundSchema, pictureURIKey, value); err != nil {
		return err
	}
	return gsettingsSet(ctx, NativeBackgroundSchema, pictureURIDarkKey, value)
}

// BackgroundURI returns the file:// URI GNOME's background keys expect for an
// absolute path, percent-encoding any character a URI cannot carry verbatim.
func BackgroundURI(file string) string {
	return (&url.URL{Scheme: "file", Path: filepath.Clean(file)}).String()
}

// SelectWallpaper applies one channel image to the desktop and records the
// channel and image, so the headless rotation pass (RotateWallpaper) knows
// which channel to advance and where it left off. Under dry-run nothing is
// written: ApplyNativeBackground only logs, and the choice is not persisted.
func SelectWallpaper(ctx context.Context, channelID, imageID, file string) error {
	if err := ApplyNativeBackground(ctx, file); err != nil {
		return err
	}
	if dryrun.Enabled() {
		return nil
	}
	if err := SetString(ctx, KeyWallpaperChannel, channelID); err != nil {
		return err
	}
	return SetString(ctx, KeyWallpaperImage, imageID)
}
