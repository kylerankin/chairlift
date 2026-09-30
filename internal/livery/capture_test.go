package livery

import "testing"

// TestCapturePanelOverridesReady covers the decision that decides whether a
// panel first-enable capture advances the confirmed in-memory state. The
// handler that calls it lives in internal/views, whose puregotk imports make
// it untestable headless, so the decision itself is the extracted, testable
// unit (see CONTRIBUTING.md, "Testing constraints").
func TestCapturePanelOverridesReady(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		surface   Surface
		dryRun    bool
		savedIcon string
		savedMode string
		wantReady bool
	}{
		{
			name:      "panel first enable, live session",
			enabled:   true,
			surface:   Panel,
			dryRun:    false,
			wantReady: true,
		},
		{
			// The reported bug: a --dry-run enable must not move the
			// confirmed state, or the next real enable skips its capture.
			name:      "panel first enable, dry-run is a preview only",
			enabled:   true,
			surface:   Panel,
			dryRun:    true,
			wantReady: false,
		},
		{
			name:      "panel already captured is not first time",
			enabled:   true,
			surface:   Panel,
			dryRun:    false,
			savedIcon: "some-icon",
			wantReady: false,
		},
		{
			name:      "panel already captured by mode only",
			enabled:   true,
			surface:   Panel,
			dryRun:    false,
			savedMode: "some-mode",
			wantReady: false,
		},
		{
			name:      "dock enable never captures panel overrides",
			enabled:   true,
			surface:   Dock,
			dryRun:    false,
			wantReady: false,
		},
		{
			name:      "app grid enable never captures panel overrides",
			enabled:   true,
			surface:   AppGrid,
			dryRun:    false,
			wantReady: false,
		},
		{
			name:      "panel disable never captures",
			enabled:   false,
			surface:   Panel,
			dryRun:    false,
			wantReady: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CapturePanelOverridesReady(tc.enabled, tc.surface, tc.dryRun, tc.savedIcon, tc.savedMode)
			if got != tc.wantReady {
				t.Fatalf("CapturePanelOverridesReady(%v, %v, %v, %q, %q) = %v, want %v",
					tc.enabled, tc.surface, tc.dryRun, tc.savedIcon, tc.savedMode, got, tc.wantReady)
			}
		})
	}
}
