package main

// This file holds every bound method whose body drives the Wails runtime —
// window/overlay geometry and opening things in the user's real browser. It is
// deliberately the ONLY file in package main that imports the runtime, keeping
// presentation-level OS calls out of app.go's wiring and delegations.

import (
	"fmt"
	"time"

	"ai-interviewer/internal/capture"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// OpenURL opens a URL in the user's real browser (not the frameless webview), so
// "Open on LeetCode" lands in Chrome/Safari rather than inside the overlay window.
func (a *App) OpenURL(url string) error {
	if url == "" {
		return fmt.Errorf("no URL to open")
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// OpenInputMonitoringSettings opens macOS System Settings at the Input
// Monitoring pane, where the user grants the permission the global hotkey needs.
func (a *App) OpenInputMonitoringSettings() {
	runtime.BrowserOpenURL(a.ctx, "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent")
}

// OpenReleasePage opens a release URL (the GitHub release page or its .zip
// asset) in the user's default browser so they can download an update. The app
// is unsigned and does not self-replace — installation is manual.
func (a *App) OpenReleasePage(url string) error {
	if url == "" {
		return fmt.Errorf("no download URL available")
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// snapshotHideDelay is how long we wait after hiding our window before grabbing
// the screen, giving the compositor time to repaint the desktop without us.
const snapshotHideDelay = 200 * time.Millisecond

// SnapshotDisplay returns a full (uncropped) screenshot of the given display as
// a base64 PNG. Used by the region selector so the user can draw a rectangle.
//
// It hides our own window for the grab (snipping-tool behaviour) so the snapshot
// shows the desktop behind the app — the user's IDE/browser — instead of the app
// covering it.
func (a *App) SnapshotDisplay(displayIndex int) (string, error) {
	runtime.WindowHide(a.ctx)
	defer runtime.WindowShow(a.ctx) // restore even if the capture fails
	time.Sleep(snapshotHideDelay)
	return capture.SnapshotDisplay(displayIndex)
}

// ---------------------------------------------------------------------------
// Window / Overlay mode
// ---------------------------------------------------------------------------

// Overlay (compact, always-on-top) window dimensions, in logical pixels.
const (
	overlayWidth  = 780 // floating bar width
	overlayBarH   = 76  // just the bar
	overlayFullH  = 400 // bar + expanded history dropdown
	restoreWidth  = 1024
	restoreHeight = 768
	overlayTopGap = 24 // distance from the top of the screen
)

// EnterOverlayMode shrinks the window to the floating bar, pins it
// always-on-top, and parks it at the top-centre of the screen so it hovers
// over the user's IDE during an interview.
func (a *App) EnterOverlayMode() {
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetSize(a.ctx, overlayWidth, overlayBarH)
	a.positionOverlayTopCenter()
}

// ExitOverlayMode restores the full window size and unpins it.
func (a *App) ExitOverlayMode() {
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.WindowSetSize(a.ctx, restoreWidth, restoreHeight)
	runtime.WindowCenter(a.ctx)
}

// SetOverlayExpanded grows the overlay window so the history dropdown has room
// (expanded) or collapses it back to just the bar.
func (a *App) SetOverlayExpanded(expanded bool) {
	h := overlayBarH
	if expanded {
		h = overlayFullH
	}
	runtime.WindowSetSize(a.ctx, overlayWidth, h)
	a.positionOverlayTopCenter()
}

// positionOverlayTopCenter centres the window horizontally near the top of the
// current screen.
func (a *App) positionOverlayTopCenter() {
	screens, err := runtime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return
	}
	width := screens[0].Size.Width
	for _, s := range screens {
		if s.IsCurrent {
			width = s.Size.Width
			break
		}
	}
	x := (width - overlayWidth) / 2
	if x < 0 {
		x = 0
	}
	runtime.WindowSetPosition(a.ctx, x, overlayTopGap)
}

// The window is frameless (so the overlay can float over the IDE), which removes
// the native titlebar buttons — the UI draws its own and calls these.

// MinimiseWindow minimises the app window to the dock/taskbar.
func (a *App) MinimiseWindow() {
	runtime.WindowMinimise(a.ctx)
}

// ToggleMaximiseWindow toggles the window between maximised and its normal size.
func (a *App) ToggleMaximiseWindow() {
	runtime.WindowToggleMaximise(a.ctx)
}

// QuitApp exits the application, running the normal shutdown (stops the hotkey
// and capturer, closes the database).
func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}
