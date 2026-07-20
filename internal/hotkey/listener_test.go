package hotkey

import "testing"

// TestEndHookSkipsTeardownOnDeadHook is a crash regression test for the fail-safe
// teardown. gohook's darwin hook_stop() dereferences a CFRunLoopRef that hook_run()
// assigns only once the event tap is actually up, so calling hook.End() on a hook
// that never started reaches CFRunLoopCopyCurrentMode(NULL) and kills the process
// with a SIGSEGV — in C, where recover() cannot reach it.
//
// Note this test does not fail if the guard in endHook is removed: it takes the
// whole test binary down with it. That is the point, and it is why the guard has
// to be a branch rather than a recover.
func TestEndHookSkipsTeardownOnDeadHook(t *testing.T) {
	(&Listener{}).endHook(false)
}

// TestApplyDisabledNeverStarts pins the cheap half of the fail-safe: a listener
// configured with push-to-talk off must not touch the OS hook at all, so there is
// never a hook to tear down. Shutdown on that listener must also be a no-op rather
// than blocking on a goroutine that was never launched.
func TestApplyDisabledNeverStarts(t *testing.T) {
	spec, err := ParseSpec(DefaultSpec)
	if err != nil {
		t.Fatalf("ParseSpec(%q): %v", DefaultSpec, err)
	}
	l := New()
	if l.Apply(t.Context(), false, spec, true) {
		t.Fatal("a disabled apply must never reach the permission check, let alone prompt")
	}

	if st := l.Status(); st.Running || st.HookEnabled {
		t.Fatalf("disabled listener should be idle, got running=%v hookEnabled=%v", st.Running, st.HookEnabled)
	}
	l.Shutdown() // must return, not deadlock on a nil done channel
}
