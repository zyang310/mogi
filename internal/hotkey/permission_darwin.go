//go:build darwin

package hotkey

// macOS gates the global keyboard hook behind the Accessibility permission.
// gohook checks it too, but only from inside its async start path, where a
// refusal is reported to stderr and then turns into a crash at teardown (see
// endHook in listener.go). Checking it up front lets the listener stay idle
// instead of installing a hook the OS has already refused.

/*
#cgo LDFLAGS: -framework ApplicationServices

#include <ApplicationServices/ApplicationServices.h>
#include <stdbool.h>

static bool mogi_accessibility_trusted(bool prompt) {
	// With prompt=true this mirrors libuiohook's own is_accessibility_enabled():
	// kAXTrustedCheckOptionPrompt makes macOS show the system "open
	// Accessibility settings" dialog while the process is untrusted (a granted
	// app never sees it). macOS re-shows that dialog on EVERY prompting check
	// while untrusted — a denial is not remembered — which is why the caller
	// passes prompt=true at most once ever and checks silently afterwards.
	if (!prompt) {
		return AXIsProcessTrusted();
	}
	const void *keys[] = {kAXTrustedCheckOptionPrompt};
	const void *values[] = {kCFBooleanTrue};
	CFDictionaryRef options = CFDictionaryCreate(
		kCFAllocatorDefault, keys, values, 1,
		&kCFCopyStringDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	bool trusted = AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
	return trusted;
}
*/
import "C"

// accessibilityTrusted reports whether macOS will let this process install a
// global event tap. With prompt=true an untrusted check also summons the system
// permission dialog; with prompt=false it is silent either way.
func accessibilityTrusted(prompt bool) bool {
	return bool(C.mogi_accessibility_trusted(C.bool(prompt)))
}
