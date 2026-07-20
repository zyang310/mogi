//go:build !darwin

package hotkey

// accessibilityTrusted reports that no up-front permission gate stands between
// this process and the global keyboard hook. Windows needs no grant, and X11
// surfaces its failures (no display, missing XRecord) through hook_run's status
// rather than a permission check. The prompt flag is meaningless here — there
// is no dialog to show. Keep this in sync with permission_darwin.go by hand —
// nothing enforces it.
func accessibilityTrusted(bool) bool { return true }
