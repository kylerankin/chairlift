package views

import (
	"errors"
	"log"

	"codeberg.org/puregotk/puregotk/v4/adw"
	"codeberg.org/puregotk/puregotk/v4/gdk"
	"codeberg.org/puregotk/puregotk/v4/gdkpixbuf"
	"codeberg.org/puregotk/puregotk/v4/gtk"

	sgtk "github.com/frostyard/snowkit/gtk"

	"github.com/projectbluefin/chairlift/internal/dryrun"
	"github.com/projectbluefin/chairlift/internal/flatpak"
	"github.com/projectbluefin/chairlift/internal/livery"
	"github.com/projectbluefin/chairlift/internal/views/actionmsg"
	"github.com/projectbluefin/chairlift/internal/views/pageview"
)

// ensureWallpapersGroup builds the optional Wallpapers group once and returns
// it. The group only exists when the Homebrew wallpaper collection is
// configured or Damask is installed or installable, so callers guard it with
// pageview.WallpaperGroupVisible.
func (uh *UserHome) ensureWallpapersGroup(page *adw.PreferencesPage) *adw.PreferencesGroup {
	if uh.wallpapersGroup != nil {
		return uh.wallpapersGroup
	}
	group := adw.NewPreferencesGroup()
	group.SetTitle(pageview.WallpaperGroupTitle)
	group.SetDescription(pageview.WallpaperGroupFragment)
	page.Add(group)
	uh.wallpapersGroup = group
	return group
}

// wallpaperProbe is what the background discovery learned about the host, so
// building and syncing the group never runs a subprocess on the main thread.
type wallpaperProbe struct {
	damask           pageview.DamaskState
	nativeBackground bool
}

// probeWallpaperHost runs the Damask and GNOME-background probes. It shells
// out, so callers run it off the main thread.
func probeWallpaperHost() wallpaperProbe {
	ctx, cancel := livery.DefaultContext()
	defer cancel()
	installed, err := livery.DamaskInstalled(ctx)
	if err != nil {
		log.Printf("wallpapers: %v", err)
	}
	return wallpaperProbe{
		damask:           pageview.DamaskOffer(installed, err != nil, installed || flatpak.IsInstalledCached()),
		nativeBackground: livery.NativeBackgroundAvailable(ctx),
	}
}

// publishWallpaperGroup builds the group and its always-present controls,
// returning the group for the caller to append this collection's expander row
// to. It is idempotent: the nil-guards in each builder make repeated calls a
// no-op, so the group and its controls are built a single time.
func (uh *UserHome) publishWallpaperGroup(page *adw.PreferencesPage, probe wallpaperProbe) *adw.PreferencesGroup {
	uh.wallpaperNativeBackground = probe.nativeBackground
	damask := probe.damask
	group := uh.ensureWallpapersGroup(page)
	uh.buildWallpaperChannels(group)
	// Rotation advances a channel, so without the selector there is nothing
	// for the switch to act on and it is left out rather than shown dead.
	if uh.wallpaperGrid != nil {
		uh.buildWallpaperRotation(group)
	}
	if damask != pageview.DamaskHidden {
		uh.buildWallpaperDamask(group, damask)
	}
	return group
}

// buildWallpaperChannels builds the channel thumbnail selector once. Each
// channel is one tile: its first image as a preview, its name, and a check
// that marks the confirmed channel.
func (uh *UserHome) buildWallpaperChannels(group *adw.PreferencesGroup) {
	if uh.wallpaperGrid != nil {
		return
	}
	rows := pageview.WallpaperSelectorRows(uh.liveryState.WallpaperChannel)
	if len(rows) == 0 {
		// No channel is installed, so there is nothing to select. The
		// selector is left absent rather than drawn empty, matching
		// "no fabricated channel when a pack is absent."
		return
	}
	grid := gtk.NewFlowBox()
	grid.SetSelectionMode(gtk.SelectionNoneValue)
	grid.SetMinChildrenPerLine(2)
	grid.SetMaxChildrenPerLine(5)
	grid.SetRowSpacing(6)
	grid.SetColumnSpacing(6)
	// Insensitive until the persisted state loads; syncWallpaperChannels
	// re-enables it once a selection can actually be applied.
	grid.SetSensitive(false)
	uh.wallpaperGrid = grid
	uh.wallpaperGridIDs = make([]string, 0, len(rows))
	uh.wallpaperGridImages = make(map[string]*gtk.Image, len(rows))
	uh.wallpaperGridChecks = nil
	for _, row := range rows {
		box := gtk.NewBox(gtk.OrientationVerticalValue, 6)
		box.SetMarginTop(12)
		box.SetMarginBottom(12)
		image := gtk.NewImage()
		image.SetPixelSize(64)
		box.Append(&image.Widget)
		label := gtk.NewLabel(row.Name)
		label.SetWrap(true)
		label.SetHalign(gtk.AlignCenterValue)
		box.Append(&label.Widget)
		check := gtk.NewImageFromIconName("object-select-symbolic")
		check.AddCssClass("accent")
		check.SetHalign(gtk.AlignEndValue)
		check.SetValign(gtk.AlignStartValue)
		check.SetMarginTop(6)
		check.SetMarginEnd(6)
		check.SetVisible(false)
		SetAccessibleLabel(check, pageview.LiveryFoundationSelectedLabel)
		tile := gtk.NewOverlay()
		tile.SetChild(&box.Widget)
		tile.AddOverlay(&check.Widget)
		grid.Insert(&tile.Widget, -1)
		uh.wallpaperGridIDs = append(uh.wallpaperGridIDs, row.ID)
		uh.wallpaperGridImages[row.ID] = image
		uh.wallpaperGridChecks = append(uh.wallpaperGridChecks, check)
	}
	catalog := rows
	activated := func(_ gtk.FlowBox, ptr uintptr) {
		child := gtk.FlowBoxChildNewFromInternalPtr(ptr)
		index := int(child.GetIndex())
		if index >= 0 && index < len(catalog) {
			uh.onWallpaperChannelChosen(catalog[index].ID)
		}
	}
	grid.ConnectChildActivated(&activated)
	group.Add(&grid.Widget)
	uh.syncWallpaperChannels()
	uh.prefetchWallpaperPreviews()
}

// syncWallpaperChannels marks the confirmed channel's tile, so the page
// highlights the image livery has already applied to the desktop, and enables
// the selector once the state has loaded on a desktop that owns GNOME's
// background keys.
func (uh *UserHome) syncWallpaperChannels() {
	if uh.wallpaperGrid == nil {
		return
	}
	uh.wallpaperGrid.SetSensitive(uh.liveryLoaded && uh.wallpaperNativeBackground)
	for i, id := range uh.wallpaperGridIDs {
		if i < len(uh.wallpaperGridChecks) {
			uh.wallpaperGridChecks[i].SetVisible(id == uh.liveryState.WallpaperChannel)
		}
	}
}

// syncWallpaperState reflects the loaded state on every built Wallpapers
// control. It is nil-guarded, so it is safe to call before the group is built
// or after it is torn down.
func (uh *UserHome) syncWallpaperState() {
	uh.syncWallpaperChannels()
	uh.syncWallpaperRotation()
}

// wallpaperPreviewSize is the decode size for a channel preview: twice the
// 64px tile, so the thumbnail stays sharp on a 2x display without holding the
// full-resolution wallpaper in memory.
const wallpaperPreviewSize = 128

// prefetchWallpaperPreviews decodes each channel's first image off the main
// thread, scaled to the tile, and paints it on the main thread. A channel
// with no discoverable image is left as its placeholder, so a missing pack
// never shows a fake preview.
func (uh *UserHome) prefetchWallpaperPreviews() {
	for id, image := range uh.wallpaperGridImages {
		id := id
		image := image
		go func() {
			channel, ok := livery.ChannelByID(id)
			if !ok || len(channel.Images) == 0 {
				return
			}
			path, ok := livery.ResolveImage(channel.Images[0].Path)
			if !ok {
				return
			}
			// GdkPixbuf loading is threadsafe, and the scaled load decodes
			// straight to the preview size.
			pixbuf, err := gdkpixbuf.NewPixbufFromFileAtScale(path, wallpaperPreviewSize, wallpaperPreviewSize, true)
			if err != nil {
				log.Printf("wallpapers: preview %s: %v", id, err)
				return
			}
			texture := gdk.NewTextureForPixbuf(pixbuf)
			pixbuf.Unref()
			sgtk.RunOnMainThread(func() {
				image.SetFromPaintable(texture)
				texture.Unref()
			})
		}()
	}
}

// buildWallpaperRotation builds the rotation switch once. It is gated: the
// switch is sensitive only when GNOME owns the background keys and a channel
// is selected, so it never offers a rotation that applies nothing.
func (uh *UserHome) buildWallpaperRotation(group *adw.PreferencesGroup) {
	if uh.wallpaperRotate != nil {
		return
	}
	uh.wallpaperRotateSpinner = newActivitySpinner()
	enableRow, enableSwitch := newSwitchRow(pageview.WallpaperRotationRow(livery.RunsFromStablePath()), func(enabled bool) {
		uh.onWallpaperRotateToggled(enabled)
	}, uh.wallpaperRotateSpinner)
	group.Add(&enableRow.Widget)
	uh.wallpaperRotate = enableSwitch
	uh.syncWallpaperRotation()
}

// syncWallpaperRotation reflects the persisted preference and its gate on the
// rotation switch. It runs on the main thread, after the group exists and the
// state has loaded.
func (uh *UserHome) syncWallpaperRotation() {
	if uh.wallpaperRotate == nil {
		return
	}
	available := pageview.WallpaperRotationAvailable(
		uh.wallpaperNativeBackground,
		uh.liveryState.WallpaperChannel != "",
	)
	uh.setWallpaperRotateSwitch(uh.liveryState.WallpaperRotate)
	uh.wallpaperRotate.SetSensitive(available)
}

// setWallpaperRotateSwitch moves the rotation switch to a confirmed value
// without it reading as a user toggle. GTK emits state-set for a programmatic
// SetActive, and the group can be built after the state has loaded, so
// without liverySuppress a page open with rotation on would rewrite and
// re-enable the unit.
func (uh *UserHome) setWallpaperRotateSwitch(active bool) {
	previous := uh.liverySuppress
	uh.liverySuppress = true
	defer func() { uh.liverySuppress = previous }()
	uh.wallpaperRotate.SetActive(active)
	uh.wallpaperRotate.SetState(active)
}

// buildWallpaperDamask builds the single stateful Damask control once. When
// installed it launches the manager; when absent it installs it. It is one
// action with two states, never two competing buttons. When the install probe
// failed the row says so and offers no action, because installing over an
// unknown state could collide with an app that is already present.
func (uh *UserHome) buildWallpaperDamask(group *adw.PreferencesGroup, state pageview.DamaskState) {
	if uh.wallpaperDamaskRow != nil {
		return
	}
	available := state == pageview.DamaskInstalled
	uh.wallpaperDamaskAvailable = available
	uh.wallpaperDamaskSpinner = newActivitySpinner()
	damaskRow := adw.NewActionRow()
	damaskRow.SetUseMarkup(false)
	row := pageview.DamaskRow(available)
	if state == pageview.DamaskUnknown {
		row = pageview.DamaskUnknownRow()
	}
	damaskRow.SetTitle(row.Title)
	damaskRow.SetSubtitle(row.Subtitle)
	damaskRow.AddSuffix(&uh.wallpaperDamaskSpinner.Widget)
	button := gtk.NewButtonWithLabel(pageview.DamaskActionName(available))
	button.SetValign(gtk.AlignCenterValue)
	button.SetVisible(state != pageview.DamaskUnknown)
	damaskRow.AddSuffix(&button.Widget)
	damaskRow.SetActivatable(false) // the button owns activation, not the row
	uh.wallpaperDamaskRow = damaskRow
	uh.wallpaperDamaskButton = button
	group.Add(&damaskRow.Widget)
	clicked := func(_ gtk.Button) { uh.onWallpaperDamaskActivated() }
	button.ConnectClicked(&clicked)
}

// refreshWallpaperDamask updates the Damask control after its state changes
// (an install that just succeeded), so the label and subtitle match the new
// state. It runs on the main thread.
func (uh *UserHome) refreshWallpaperDamask() {
	if uh.wallpaperDamaskRow == nil {
		return
	}
	available := uh.wallpaperDamaskAvailable
	row := pageview.DamaskRow(available)
	uh.wallpaperDamaskRow.SetTitle(row.Title)
	uh.wallpaperDamaskRow.SetSubtitle(row.Subtitle)
	uh.wallpaperDamaskButton.SetLabel(pageview.DamaskActionName(available))
}

// onWallpaperChannelChosen applies a channel selection through native
// gsettings and persists the channel, so rotation can advance it. The selection is immediate (no portal): GNOME takes the
// background key and the desktop confirms the change. Under dry-run the
// picture is not written and the preview toast says what would have happened.
func (uh *UserHome) onWallpaperChannelChosen(channelID string) {
	if !uh.liveryLoaded {
		return
	}
	decision := actionmsg.WallpaperSelection(dryrun.Enabled())
	currentImage := uh.liveryState.WallpaperImage
	go func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()
		channel, ok := livery.ChannelByID(channelID)
		if !ok || len(channel.Images) == 0 {
			uh.reportLiveryFailure("choosing a wallpaper channel", errors.New("channel has no images"))
			return
		}
		// Keep the stored image when it belongs to this channel, so
		// re-picking the channel does not snap back to its first image.
		idx := 0
		for i, img := range channel.Images {
			if img.ID == currentImage {
				idx = i
				break
			}
		}
		target := channel.Images[idx]
		path, ok := livery.ResolveImage(target.Path)
		if !ok {
			uh.reportLiveryFailure("choosing a wallpaper channel", errors.New("image not found"))
			return
		}
		if err := livery.SelectWallpaper(ctx, channelID, target.ID, path); err != nil {
			uh.reportLiveryFailure("choosing a wallpaper channel", err)
			return
		}
		sgtk.RunOnMainThread(func() {
			if !decision.MutateUI {
				// Under dry-run the stored channel never moved, so the
				// page keeps its old selection and the preview toast is
				// the only confirmation, as the other Livery selections do.
				if decision.Toast != "" {
					uh.toastAdder.ShowToast(decision.Toast)
				}
				return
			}
			uh.liveryState.WallpaperChannel = channelID
			uh.liveryState.WallpaperImage = target.ID
			uh.syncWallpaperState()
			if decision.Toast != "" {
				uh.toastAdder.ShowToast(decision.Toast)
			}
		})
	}()
}

// onWallpaperRotateToggled commits the rotation preference only after the
// user manager accepts the schedule, then mirrors the confirmed state back.
// A Gate admits one toggle at a time; a failed operation leaves the last
// persisted choice in place and springs the switch back to it.
func (uh *UserHome) onWallpaperRotateToggled(enabled bool) {
	if uh.liverySuppress || !uh.liveryLoaded || enabled == uh.liveryState.WallpaperRotate {
		return
	}
	if !uh.wallpaperRotationGate.TryStart() {
		return
	}
	decision := actionmsg.WallpaperRotation(dryrun.Enabled(), enabled)
	setActivitySpinner(uh.wallpaperRotateSpinner, true)
	desired := uh.liveryState
	desired.WallpaperRotate = enabled
	go func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()
		confirmed, err := livery.ConfigureWallpaperRotation(ctx, desired)
		sgtk.RunOnMainThread(func() {
			setActivitySpinner(uh.wallpaperRotateSpinner, false)
			uh.wallpaperRotationGate.Reset()
			if err != nil {
				uh.setWallpaperRotateSwitch(uh.liveryState.WallpaperRotate)
				uh.reportLiveryFailure("rotating the wallpaper", err)
				return
			}
			uh.liveryState.WallpaperRotate = confirmed.WallpaperRotate
			uh.setWallpaperRotateSwitch(confirmed.WallpaperRotate)
			if !decision.MutateUI {
				uh.toastAdder.ShowToast(decision.Toast)
			}
		})
	}()
}

// onWallpaperDamaskActivated runs the single Damask control's action: install
// it when absent, launch it when installed. A dry run previews without
// changing anything, and a Flatpak that vanished since the probe only shows a
// toast pointing to the software store; nothing is launched.
func (uh *UserHome) onWallpaperDamaskActivated() {
	if !uh.liveryLoaded {
		return
	}
	if !uh.wallpaperDamaskGate.TryStart() {
		return
	}
	installed := uh.wallpaperDamaskAvailable
	preview := dryrun.Enabled()
	setActivitySpinner(uh.wallpaperDamaskSpinner, true)
	// finish runs every completion step on the main thread: the spinner,
	// the Damask row, and the toast are GTK widgets.
	finish := func(update func()) {
		sgtk.RunOnMainThread(func() {
			if update != nil {
				update()
			}
			setActivitySpinner(uh.wallpaperDamaskSpinner, false)
			uh.wallpaperDamaskGate.Reset()
		})
	}
	if preview {
		finish(func() {
			if installed {
				uh.toastAdder.ShowToast(actionmsg.DamaskOpenPreview())
			} else {
				uh.toastAdder.ShowToast(actionmsg.DamaskInstall(true))
			}
		})
		return
	}
	go func() {
		ctx, cancel := livery.DefaultContext()
		defer cancel()
		var err error
		if installed {
			err = livery.DamaskOpen(ctx)
		} else {
			err = livery.DamaskInstall(ctx)
		}
		if err != nil {
			var notFound *flatpak.NotFoundError
			if errors.As(err, &notFound) {
				finish(func() { uh.toastAdder.ShowToast(actionmsg.DamaskStoreHandoff()) })
				return
			}
			uh.reportLiveryFailure(damaskVerb(installed), err)
			finish(nil)
			return
		}
		finish(func() {
			if installed {
				uh.toastAdder.ShowToast(actionmsg.DamaskOpen())
				return
			}
			uh.wallpaperDamaskAvailable = true
			uh.refreshWallpaperDamask()
			uh.toastAdder.ShowToast(actionmsg.DamaskInstall(false))
		})
	}()
}

// damaskVerb names the action for a failure toast, so install and launch
// errors are distinguishable.
func damaskVerb(installed bool) string {
	if installed {
		return "opening Damask"
	}
	return "installing Damask"
}
