package main

// This file holds every bound method whose body drives the Wails runtime —
// window/overlay geometry and opening things in the user's real browser. It is
// deliberately the ONLY file in package main that imports the runtime, keeping
// presentation-level OS calls out of app.go's wiring and delegations.

import (
	"fmt"
	"time"

	"mogi/internal/capture"

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
	a.startOverlayGuard()
}

// ExitOverlayMode restores the full window size and unpins it.
func (a *App) ExitOverlayMode() {
	a.stopOverlayGuard()
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

// overlayGuardInterval is how often the guard re-checks that the (user-draggable)
// overlay bar is still on-screen — responsive enough to feel instant on release,
// cheap enough to run continuously.
const overlayGuardInterval = 300 * time.Millisecond

// startOverlayGuard runs a lightweight ticker that keeps the compact overlay from
// being dragged off-screen and lost. The bar is intentionally movable (the grab
// handle uses native window drag), but nothing stops a drag past a screen edge —
// and once it's gone there are no on-window controls to bring it back. The guard
// snaps it fully back inside the screen so it always stays reachable. Stopped by
// ExitOverlayMode and shutdown.
func (a *App) startOverlayGuard() {
	a.stopOverlayGuard() // never run two at once
	stop := make(chan struct{})
	a.overlayGuardStop = stop
	go func() {
		t := time.NewTicker(overlayGuardInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				a.clampOverlayOnScreen()
			}
		}
	}()
}

// stopOverlayGuard stops the on-screen guard if it is running (idempotent).
func (a *App) stopOverlayGuard() {
	if a.overlayGuardStop != nil {
		close(a.overlayGuardStop)
		a.overlayGuardStop = nil
	}
}

// clampOverlayOnScreen nudges the overlay window fully back inside the current
// screen when a drag has pushed it (partly) past an edge. Uses the same
// screen-relative coordinates as positionOverlayTopCenter.
func (a *App) clampOverlayOnScreen() {
	sw, sh, ok := a.currentScreenSize()
	if !ok {
		return
	}
	w, h := runtime.WindowGetSize(a.ctx)
	x, y := runtime.WindowGetPosition(a.ctx)

	maxX, maxY := sw-w, sh-h
	if maxX < 0 {
		maxX = 0
	}
	if maxY < 0 {
		maxY = 0
	}
	cx, cy := clampInt(x, 0, maxX), clampInt(y, 0, maxY)
	if cx != x || cy != y {
		runtime.WindowSetPosition(a.ctx, cx, cy)
	}
}

// clampInt constrains v to the inclusive range [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// The window is frameless (so the overlay can float over the IDE), which removes
// the native titlebar buttons — the UI draws its own and calls these.

// MinimiseWindow minimises the app window to the dock/taskbar.
func (a *App) MinimiseWindow() {
	runtime.WindowMinimise(a.ctx)
}

// windowFrame captures a window's size and position so a custom zoom can put
// the window back exactly where it was on toggle-off.
type windowFrame struct {
	w, h, x, y int
}

// zoomState backs the green-button zoom toggle: whether the window is currently
// zoomed, and the frame to restore. We drive the toggle ourselves rather than
// use runtime.WindowToggleMaximise because macOS's native zoom, on a frameless
// window with no app-defined standard frame, fills from the top-left corner.
type zoomState struct {
	zoomed bool
	prev   windowFrame
}

// zoomFraction is how much of the current display a "zoomed" window fills —
// large, but with a margin so it reads as a zoom (and clears the menu bar)
// rather than a full-screen takeover.
const zoomFraction = 1

// ToggleMaximiseWindow toggles the window between a large, display-centred
// "zoom" and its previous size/position — the macOS green-button feel. It grows
// the window in place and centres it on the current display instead of jumping
// to the top-left corner (which native zoom does for a frameless window).
func (a *App) ToggleMaximiseWindow() {
	if a.winZoom.zoomed {
		p := a.winZoom.prev
		runtime.WindowSetSize(a.ctx, p.w, p.h)
		runtime.WindowSetPosition(a.ctx, p.x, p.y)
		a.winZoom.zoomed = false
		return
	}

	sw, sh, ok := a.currentScreenSize()
	if !ok {
		return // no display to size against; leave the window untouched
	}

	// Remember the current frame so the next toggle restores it exactly.
	w, h := runtime.WindowGetSize(a.ctx)
	x, y := runtime.WindowGetPosition(a.ctx)
	a.winZoom.prev = windowFrame{w: w, h: h, x: x, y: y}

	// Grow to a large fraction of the display, then centre on that display.
	runtime.WindowSetSize(a.ctx, int(float64(sw)*zoomFraction), int(float64(sh)*zoomFraction))
	runtime.WindowCenter(a.ctx)
	a.winZoom.zoomed = true
}

// currentScreenSize returns the logical size of the display the window is on,
// preferring the current screen, then the primary, then the first. ok is false
// when no screens are reported (e.g. the runtime isn't ready).
func (a *App) currentScreenSize() (w, h int, ok bool) {
	screens, err := runtime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return 0, 0, false
	}
	pick := screens[0]
	for _, s := range screens {
		if s.IsCurrent {
			return s.Size.Width, s.Size.Height, true
		}
		if s.IsPrimary {
			pick = s
		}
	}
	return pick.Size.Width, pick.Size.Height, true
}

// QuitApp exits the application, running the normal shutdown (stops the hotkey
// and capturer, closes the database).
func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}
