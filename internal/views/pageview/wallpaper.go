package pageview

import (
	"github.com/projectbluefin/chairlift/internal/branding"
	"github.com/projectbluefin/chairlift/internal/livery"
)

// WallpaperGroupTitle labels the optional Wallpapers group.
const WallpaperGroupTitle = "Wallpapers"

// WallpaperGroupFragment introduces what the group can do. It names the
// channel selector, the rotation switch, and Damask together, because the
// group only exists when at least one of them is usable.
const WallpaperGroupFragment = "Choose a wallpaper channel, rotate it at login, or manage live feeds with Damask."

// WallpaperRotationRow is the Wallpapers group's rotation switch text.
//
// A source build gets a different subtitle, because the login unit it writes
// points at the binary's current path — the same reason the Livery rotation
// switch carries one. That path is stable for an installed build and
// disposable for a source one.
func WallpaperRotationRow(systemInstall bool) Row {
	if !systemInstall {
		return Row{
			Title:    "Rotate Wallpaper at Login",
			Subtitle: "Stops working if this copy of " + branding.AppName + " is moved or reinstalled.",
		}
	}
	return Row{
		Title:    "Rotate Wallpaper at Login",
		Subtitle: "Show the next image in the channel each time you log in.",
	}
}

// WallpaperRotationAvailable reports whether the rotation switch may operate.
//
// Rotation writes GNOME's background keys, so it needs a desktop that owns
// them, and it advances a channel's images, so a channel must be selected.
// Without either, leaving the switch sensitive would offer a setting that
// applies nothing.
func WallpaperRotationAvailable(gnomeAvailable, channelSelected bool) bool {
	return gnomeAvailable && channelSelected
}

// WallpaperSelectedChannelRow describes the currently selected channel. An
// empty selection names the action rather than leaving the row blank.
func WallpaperSelectedChannelRow(channelID string) Row {
	if channelID == "" {
		return Row{Title: "Channel", Subtitle: "Choose a wallpaper channel"}
	}
	if channel, ok := livery.ChannelByID(channelID); ok {
		return Row{Title: "Channel", Subtitle: channel.Name}
	}
	return Row{Title: "Channel", Subtitle: "Choose a wallpaper channel"}
}

// WallpaperSelectorItem is one channel in the thumbnail selector.
type WallpaperSelectorItem struct {
	ID       string
	Name     string
	Selected bool
}

// WallpaperSelectorRows is the thumbnail selector's model: every discovered
// channel in catalog order, with the confirmed selection marked. An empty
// catalog yields no rows, so the selector is hidden rather than drawn empty.
func WallpaperSelectorRows(selectedID string) []WallpaperSelectorItem {
	out := make([]WallpaperSelectorItem, 0, len(livery.WallpaperChannels()))
	for _, channel := range livery.WallpaperChannels() {
		out = append(out, WallpaperSelectorItem{
			ID:       channel.ID,
			Name:     channel.Name,
			Selected: channel.ID == selectedID,
		})
	}
	return out
}

// WallpaperSelectorEmpty reports whether no channel is discoverable, so the
// page can hide the selector rather than draw an empty row of nothing.
func WallpaperSelectorEmpty() bool {
	return len(livery.WallpaperChannels()) == 0
}

// DamaskRow describes the single stateful Damask control.
//
// Damask is an external Flatpak, not a ChairLift action, so the row's title
// never changes — only what activating it does. When installed the control
// launches the manager; when absent it installs it. The subtitle says which,
// and the action row's label (pageview.DamaskActionName) says what clicking
// does.
func DamaskRow(installed bool) Row {
	if installed {
		return Row{Title: "Damask", Subtitle: "Manage live wallpaper feeds from Bing, Wallhaven, and more."}
	}
	return Row{Title: "Damask", Subtitle: "Install a live wallpaper manager."}
}

// DamaskActionName is the label the single Damask control shows. It is
// "Open" when Damask is installed and "Install" when it is not, so the
// control is one action with two states rather than two competing buttons.
func DamaskActionName(installed bool) string {
	if installed {
		return "Open"
	}
	return "Install"
}

// DamaskState is what the Wallpapers group can offer for Damask on this host.
type DamaskState int

const (
	// DamaskHidden means Damask is neither installed nor installable
	// (no Flatpak), so the group shows no Damask control at all.
	DamaskHidden DamaskState = iota
	// DamaskInstallable means Flatpak is present but Damask is not.
	DamaskInstallable
	// DamaskInstalled means the Damask Flatpak is installed.
	DamaskInstalled
)

// DamaskOffer reports which Damask control, if any, the group may show. A
// host without Flatpak can neither install nor run Damask, so it gets none.
func DamaskOffer(installed, flatpakAvailable bool) DamaskState {
	switch {
	case installed:
		return DamaskInstalled
	case flatpakAvailable:
		return DamaskInstallable
	default:
		return DamaskHidden
	}
}

// WallpaperGroupVisible reports whether the optional Wallpapers group is
// built: only when the Homebrew wallpaper collection is configured or Damask
// is installed. Inside a visible group the installed channels, the rotation
// switch and the Damask install control (where Flatpak is present) appear as
// usable; none of them alone surfaces the group, so a host with neither a
// collection nor Damask keeps Livery without an empty Wallpapers group.
func WallpaperGroupVisible(collection, damaskInstalled bool) bool {
	return collection || damaskInstalled
}
