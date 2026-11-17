// Package legacydesktop cleans up obsolete desktop launcher files left behind by
// older frostyard installs (issue #443).
package legacydesktop

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/projectbluefin/chairlift/internal/dryrun"
)

// LegacyDesktopFilename is the obsolete launcher filename from older frostyard installs.
const LegacyDesktopFilename = "org.frostyard.ChairLift.desktop"

// Clean removes the legacy launcher file if present and matching.
// Under dry-run mode, it logs what would be removed without deleting.
func Clean() error {
	dir, err := dataHome()
	if err != nil {
		log.Printf("legacy desktop: %v", err)
		return err
	}
	return CleanDataHome(dir)
}

// CleanDataHome checks the applications directory inside dataHome for the
// legacy desktop launcher and removes it if it matches.
func CleanDataHome(dataHome string) error {
	target := filepath.Join(dataHome, "applications", LegacyDesktopFilename)
	info, err := os.Lstat(target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		log.Printf("legacy desktop: stat %s: %v", target, err)
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}

	data, err := os.ReadFile(target)
	if err != nil {
		log.Printf("legacy desktop: read %s: %v", target, err)
		return err
	}

	if !isLegacyLauncherContent(data) {
		return nil
	}

	if dryrun.Enabled() {
		log.Printf("[DRY-RUN] would remove legacy desktop launcher: %s", target)
		return nil
	}

	if err := os.Remove(target); err != nil {
		log.Printf("failed to remove legacy desktop launcher %s: %v", target, err)
		return err
	}

	log.Printf("removed legacy desktop launcher: %s", target)
	return nil
}

func dataHome() (string, error) {
	if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share"), nil
}

func isLegacyLauncherContent(data []byte) bool {
	var inDesktopEntry bool
	var entryType, entryExec string

	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inDesktopEntry = (trimmed == "[Desktop Entry]")
			continue
		}
		if !inDesktopEntry {
			continue
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Type":
			entryType = value
		case "Exec":
			entryExec = value
		}
	}

	if entryType != "Application" {
		return false
	}
	prog := parseExecProgram(entryExec)
	base := filepath.Base(prog)
	return base == "chairlift" || base == "chairlift-wrapper"
}

func parseExecProgram(execLine string) string {
	execLine = strings.TrimSpace(execLine)
	if execLine == "" {
		return ""
	}
	if strings.HasPrefix(execLine, "\"") {
		end := strings.Index(execLine[1:], "\"")
		if end != -1 {
			return execLine[1 : 1+end]
		}
		return execLine[1:]
	}
	fields := strings.Fields(execLine)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
