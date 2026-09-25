// Package printerapp implements ChairLift's printer support: rootless quadlets
// for the projectbluefin Printer Applications, one unit per printer.
//
// It is the printer port of internal/aistack. aistack writes one quadlet for
// the local-AI stack and drives it with `systemctl --user`; printerapp writes
// one quadlet per printer application and drives each the same way. Nothing
// here is privileged. Quadlet units live under the user's
// ~/.config/containers/systemd and are started with `systemctl --user`, so the
// container runs rootless in the invoking account — the same reasoning that
// keeps the AI stack and gaming mode off the pkexec path. On a bootc host that
// also means nothing is layered onto the image.
//
// A printer application is a Family (a published image, one per driver family:
// Ghostscript, HPLIP, Gutenprint, PostScript) plus a unique app name. A family
// can host several printers, and each App gets its own unit, host port, and
// state volume, so one logical device has exactly one owner and one
// advertisement rather than two units fighting over the same port and volume.
package printerapp

import (
	"context"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

const commandTimeout = 5 * time.Minute

// portBase and portRange scope the ports a printer application may publish
// into. They are chosen well above the privileged range and well clear of the
// ephemeral range, so a published printer port never collides with a system
// service or a browser's temporary connections.
const (
	portBase  = 18000
	portRange = 1000
)

// Family identifies one Printer Application family. Each publishes an
// immutable, signed, multi-architecture index at GHCR (the projectbluefin
// *-printer-app repositories). The index, pinned by an immutable
// application-version tag, is what we run — not a mutable `:latest` or `:build`
// tag, which a re-pull could change under us.
type Family struct {
	// ID is the lowercase identifier used in unit names and volume paths.
	ID string
	// DisplayName is the human-readable family name.
	DisplayName string
	// Repo is the GHCR repository path for the family's image.
	Repo string
	// Version is the immutable application-version tag pinned for this family.
	Version string
}

// Image returns the pinned immutable signed index for this family.
func (f Family) Image() string {
	return f.Repo + ":" + f.Version
}

// App is one namespaced printer application: a Family plus a unique app name.
// Multiple apps may share a family's image, but each has its own unit, port,
// and volume, so one logical device has exactly one owner.
type App struct {
	Family Family
	// Name uniquely identifies this printer within its family.
	Name string
}

// UnitName is the quadlet file ChairLift writes. It is namespaced with both the
// family and the app so two printers on the same family never collide, and it
// carries the `chairlift-` prefix so it never overwrites a unit from another
// tool.
func (a App) UnitName() string {
	return "chairlift-" + sanitize(a.Family.ID) + "-" + sanitize(a.Name) + ".container"
}

// ServiceName is the systemd unit quadlet generates from UnitName.
func (a App) ServiceName() string {
	return strings.TrimSuffix(a.UnitName(), ".container") + ".service"
}

// ContainerName is the running container's name, matched to the unit so
// `podman ps` output is recognizable.
func (a App) ContainerName() string {
	return "chairlift-" + sanitize(a.Family.ID) + "-" + sanitize(a.Name)
}

// Port is the host port the printer's IPP service is published on. It is
// derived deterministically from the family and app name, so the same printer
// always publishes the same port across restarts — one owner, one address —
// while distinct printers get distinct ports.
//
// ponytail: the port is a hash of the identity, not a registry, so a very
// large number of printers could in principle collide. A host runs a handful
// of printers at most; if that ever changes, replace the hash with a persistent
// port allocation keyed by unit name.
func (a App) Port() int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(a.Family.ID + "\x00" + a.Name))
	return portBase + int(h.Sum32()%portRange)
}

// Volume is the per-app state volume, using the systemd home escape `%h`
// that quadlet expands. It lives under the user's home so the printer's cached
// PPD/driver state and print jobs survive enable/disable, exactly as aistack's
// ai-workspaces volume survives the AI switch.
func (a App) Volume() string {
	return fmt.Sprintf("%%h/printer-workspaces/%s/%s:z", a.Family.ID, a.Name)
}

// families is the set of Printer Application families ChairLift can drive.
// Every reference was taken from the projectbluefin *-printer-app repositories,
// which publish immutable, signed, multi-architecture indexes.
var families = []Family{
	{
		ID:          "ghostscript",
		DisplayName: "Ghostscript",
		Repo:        "ghcr.io/projectbluefin/ghostscript-printer-app",
		Version:     "10.07.1-1",
	},
	{
		ID:          "hplip",
		DisplayName: "HPLIP",
		Repo:        "ghcr.io/projectbluefin/hplip-printer-app",
		Version:     "0.1.0-1",
	},
	{
		ID:          "gutenprint",
		DisplayName: "Gutenprint",
		Repo:        "ghcr.io/projectbluefin/gutenprint-printer-app",
		Version:     "0.1.0-1",
	},
	{
		ID:          "ps",
		DisplayName: "PostScript",
		Repo:        "ghcr.io/projectbluefin/ps-printer-app",
		Version:     "0.1.0-1",
	},
}

// Families returns the known Printer Application families, in display order.
func Families() []Family {
	out := make([]Family, len(families))
	copy(out, families)
	return out
}

// Select returns the default app for a family, named after the family. Callers
// that manage a specific physical printer pass their own app name.
func Select(f Family) App {
	return App{Family: f, Name: f.ID}
}

// sanitize collapses a name into the safe character set a quadlet unit name and
// volume path can carry, so an app named with arbitrary user text still yields
// a stable, collision-free unit name without reaching into the host.
func sanitize(name string) string {
	var b strings.Builder
	previousSep := true // so a leading separator is never emitted
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			previousSep = false
		} else if !previousSep {
			// Collapse runs of separators to one so "Office  Printer" and
			// "office-printer" land on the same name rather than two units.
			b.WriteRune('-')
			previousSep = true
		}
	}
	return b.String()
}

// ApplyOverrides replaces a family's pinned image from configuration. A site
// that mirrors the indexes points its families at the mirror; the mirror must
// serve the same immutable, signed index. This lives in the ordinary config
// file rather than the root-only channels.yml because the container runs
// rootless in the invoking account, so pointing it at another image grants
// nothing a user could not get by running podman themselves.
//
// An unknown family ID is an error rather than a silent no-op, since a typo'd
// key would otherwise leave the site believing its mirror was in use.
func ApplyOverrides(images map[string]string) error {
	for id, image := range images {
		if image == "" {
			return fmt.Errorf("printerapp: family %q has an empty image", id)
		}
		found := false
		for i := range families {
			if families[i].ID == id {
				families[i].Repo = repoPart(image)
				families[i].Version = versionPart(image)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("printerapp: unknown family %q", id)
		}
	}
	return nil
}

// repoPart splits an image reference into its repository path.
func repoPart(image string) string {
	if i := strings.LastIndex(image, ":"); i >= 0 {
		return image[:i]
	}
	return image
}

// versionPart splits an image reference into its tag.
func versionPart(image string) string {
	if i := strings.LastIndex(image, ":"); i >= 0 {
		return image[i+1:]
	}
	return "latest"
}

// RenderUnit returns the quadlet .container file for one printer application.
//
// The unit publishes IPP on loopback only, exactly as aistack publishes the AI
// API on loopback: an unauthenticated printer service exposed to the LAN would
// let anyone on the network drive the physical device. The state volume is
// bind-mounted read-write so the printer's cached driver state persists across
// enable/disable.
func RenderUnit(app App) string {
	var b strings.Builder

	fmt.Fprintf(&b, "[Unit]\nDescription=Printer Application (%s — %s)\nAfter=network-online.target\n\n",
		app.Family.DisplayName, app.Name)

	b.WriteString("[Container]\n")
	fmt.Fprintf(&b, "ContainerName=%s\n", app.ContainerName())
	fmt.Fprintf(&b, "Image=%s\n", app.Family.Image())
	// No Exec=: each family appliance ships its own entrypoint that starts the
	// PAPPL/CUPS service. Pinning a command here would duplicate, and could
	// drift from, the image's real one. USB passthrough is deliberately not
	// wired here either: reaching a USB printer is hardware-dependent and
	// cannot be verified without a physical device, so the unit stays rootless
	// rather than guessing at a flag or requesting --privileged. A host with a
	// USB printer supplies the correct --device= path through the appliance's
	// own entrypoint.
	fmt.Fprintf(&b, "PublishPort=127.0.0.1:%d:%d\n", app.Port(), app.Port())
	fmt.Fprintf(&b, "Volume=%s\n\n", app.Volume())

	b.WriteString("[Service]\nRestart=on-failure\nRestartSec=10\n\n")
	b.WriteString("[Install]\nWantedBy=default.target\n")

	return b.String()
}

// RenderUnits renders a set of printer applications. The units are sorted by
// name so the output is stable regardless of the order the caller passes them
// in, which keeps diffs and dry-run logs deterministic.
func RenderUnits(apps []App) string {
	sorted := make([]App, len(apps))
	copy(sorted, apps)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].UnitName() < sorted[j].UnitName()
	})

	var b strings.Builder
	for _, app := range sorted {
		b.WriteString(RenderUnit(app))
	}
	return b.String()
}

// unitDir is an injection seam for the quadlet directory, so the install and
// remove paths are testable without writing into a real home directory.
var unitDir = defaultUnitDir

func defaultUnitDir() (string, error) {
	config, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(config, "containers", "systemd"), nil
}

// runSystemctl is an injection seam for the `systemctl --user` calls.
var runSystemctl = execSystemctl

func execSystemctl(ctx context.Context, args ...string) error {
	runCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()

	full := append([]string{"--user"}, args...)
	cmd := exec.CommandContext(runCtx, "systemctl", full...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %s", strings.Join(full, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

// IsAvailable reports whether this host can run the applications at all.
// Quadlet is a Podman feature, so without Podman there is nothing to install
// into.
func IsAvailable() bool {
	_, err := exec.LookPath("podman")
	return err == nil
}

// UnitPath returns the absolute path of one printer application's quadlet file.
func UnitPath(app App) (string, error) {
	dir, err := unitDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, app.UnitName()), nil
}

// IsEnabled reports whether ChairLift's quadlet is installed. The unit file's
// presence is the state, not the container's running status: a printer whose
// container is restarting is enabled, and reading it any other way would make
// the switch flicker during a first start.
func IsEnabled(app App) bool {
	path, err := UnitPath(app)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Enable writes the quadlet for the printer application and starts it. Enabling
// only writes the unit and starts the service: the image pull happens inside
// the container runtime afterwards, so the switch must not wait on it.
func Enable(ctx context.Context, app App) error {
	path, err := UnitPath(app)
	if err != nil {
		return err
	}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would write %s for %s and start %s", path, app.Family.Image(), app.ServiceName())
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(RenderUnit(app)), 0o644); err != nil {
		return err
	}

	if err := runSystemctl(ctx, "daemon-reload"); err != nil {
		// The unit is on disk but systemd has not seen it. Take it back off
		// rather than leaving a host whose switch reads "on" and whose
		// service does not exist.
		_ = os.Remove(path)
		return err
	}
	if err := runSystemctl(ctx, "start", app.ServiceName()); err != nil {
		_ = os.Remove(path)
		_ = runSystemctl(ctx, "daemon-reload")
		return err
	}
	return nil
}

// Disable stops the printer application and removes its quadlet. The pulled
// image and the per-app state under ~/printer-workspaces are left alone: they
// are large, they are expensive to re-fetch or reconfigure, and removing them
// is a disk-space decision the user did not make by turning a switch off.
func Disable(ctx context.Context, app App) error {
	path, err := UnitPath(app)
	if err != nil {
		return err
	}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would stop %s and remove %s", app.ServiceName(), path)
		return nil
	}

	// A stop failure is not fatal: the service may already be down, and the
	// unit still has to come off disk for the switch to mean anything.
	if err := runSystemctl(ctx, "stop", app.ServiceName()); err != nil {
		log.Printf("printerapp: stopping %s: %v", app.ServiceName(), err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return runSystemctl(ctx, "daemon-reload")
}
