# Global Voice Hotkey (toggle) — Implementation Plan (Phase 3)

> **Status: implemented.** Shipped as a configurable global hotkey (default `Right ⌥ Option`, enabled by default): **press once to start recording, press again to stop and send** (a toggle, like the mic button), working while another app is focused, on macOS + Windows. Backend OS keyboard hook (`internal/hotkey` via `robotn/gohook`) → a `ptt:down` Wails event per press → the existing recorder; the frontend toggles on it. This doc is the design reference. (It originally shipped as hold-to-talk; switched to a toggle for friendlier UX — the hook now emits only `ptt:down`.)
>
> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md); phase status in [roadmap.md](roadmap.md); data flow + bindings in [architecture.md](architecture.md).

## Context

Before this, the only way to talk to the interviewer was the **click-to-toggle mic button** (`handleMicToggle` in [App.tsx](../frontend/src/App.tsx)): click to start recording, click again to stop → transcribe → send. The recorder/transcribe/send plumbing was already complete and is reused unchanged — `useVoiceRecorder` ([useVoiceRecorder.ts](../frontend/src/lib/useVoiceRecorder.ts)), `TranscribeAudio`, and `SendMessage`.

What was missing: a way to talk **hands-free while coding in your IDE**. That single requirement is what shaped (and sized) the whole feature — see the decision below.

## The scope decision: global vs in-app (why this wasn't a 30-line change)

Push-to-talk could mean two very different things, and the choice drives almost all the cost:

| | **In-app PTT** (not built) | **Global PTT** (built) |
|---|---|---|
| Fires when… | only the app window is focused | any app is focused (your IDE) |
| Implementation | a `keydown`/`keyup` listener in `App.tsx` + the pref | native OS keyboard hook, cross-platform keycodes, OS permission, backend listener + Wails events |
| New dependency | none | `robotn/gohook` (CGO) |
| Size | ~30 lines | a new package (~300 LOC incl. tests) + small touches |
| Useful here? | **No** — during a session you're typing in your IDE, so the overlay is never focused | **Yes** — matches the actual workflow |

**Decision: global.** WKWebView only delivers key events to a *focused* window, so an in-app listener would never fire during real use (you're in VS Code / LeetCode, not the overlay). Global is the version that delivers the feature; the "small" version was never viable for this app. The extra weight (a CGO dependency + a macOS permission) is the irreducible price of "works while another app is focused."

## Library: `robotn/gohook` (libuiohook)

Chosen because it provides a global keyboard hook on **macOS, Windows, and Linux** with identical Go code, and runs the hook on a **libuiohook C thread** (a pthread) — *not* Go's main thread. That last point matters: alternatives like `golang.design/x/hotkey` need the main run loop, which Wails owns, so they don't compose. CGO is already enabled (sqlite3 + screenshot), so gohook adds no new toolchain requirement.

**Confirmed gohook facts (v0.42.3, verified against the source):**
- `hook.Start() chan hook.Event` begins the hook and returns the raw event channel; `hook.End()` stops it (closes the channel). You read events directly — no need for `hook.Register`/`hook.Process`.
- `hook.Event{ Kind uint8; Keycode uint16; Rawcode uint16; … }`. Match on **`Keycode`**, not `Rawcode` — gohook's own `Process` tracks `pressed[ev.Keycode]` against the `hook.Keycode` map, so `Keycode` is the portable, cross-platform field.
- Kind constants: `KeyDown = 4`, `KeyUp = 5`, **`KeyHold = 3`** (OS auto-repeat — ignored, which *is* the debounce).
- `hook.Keycode` (`map[string]uint16`, from `vcaesar/keycode`) gives portable codes: `ctrl`=29, `shift`=42/`rshift`=54, `alt`=56/`ralt`=3640, `cmd`=3675/`rcmd`=3676, `space`=57, `f1`–`f12`=59–70, letters/digits, `` ` ``=41. **No `rctrl`** — right-Ctrl isn't separately mapped, so the `Ctrl` token is the single Ctrl key.

**It's a passive hook:** it observes keystrokes but does **not** consume them, so the held key still reaches the focused IDE. Consequence: a combo like `Ctrl+Space` also triggers IntelliJ/VS Code autocomplete (Win/Linux), the macOS input-source switcher, and — held/auto-repeating — the macOS **unhandled-key alert beep**. That's why the default is a **bare right-hand modifier** (`RightAlt` / Right ⌥ Option): held alone it types nothing, triggers no shortcut, and doesn't beep. The hotkey stays reconfigurable for anyone who wants a combo.

## Architecture

```
OS keyboard (global)
  │  libuiohook passive hook (C thread)
  ▼
internal/hotkey.Listener.loop  ── matches the combo, debounces auto-repeat
  │  runtime.EventsEmit(ctx, "ptt:down")   // one per press
  ▼
App.tsx  EventsOn("ptt:down")  ── gated on prefs.pushToTalkEnabled
  └─ press → handleMicToggle():  idle ? startRecording() (barge-in over TTS)
                                       : stopAndSend() → TranscribeAudio → handleSend
             ("ptt:up" is ignored)
```

### Backend — `internal/hotkey/`
- [keymap.go](../internal/hotkey/keymap.go): `Token`/`Spec`, `ParseSpec`/`String`/`Label(goos)`, `codesByToken` (built at `init()` from `hook.Keycode` — never hardcoded ints), `tokenForKeycode`, `DefaultSpec = "RightAlt"`.
- [listener.go](../internal/hotkey/listener.go): `Listener` (`New`/`Apply`/`Shutdown`/`Status`) over a mutex + `context.CancelFunc` + `done` channel. **The OS hook is started at most once and torn down only at `Shutdown`** — `Apply(ctx, enabled, spec, allowPrompt)` swaps guarded `enabled`/`spec` fields that the running goroutine reads, so enabling, disabling, and rebinding never restart the hook (see the crash note below); it returns whether the macOS permission dialog was shown, which backs the ask-once rule (see "macOS Accessibility"). The goroutine matches the combo via a **pure, unit-tested `matcher`** — active iff every required token is held; releasing any re-arms it; duplicate downs / `KeyHold` are no-ops (one `ptt:down` per press). The **first delivered event of any kind** flips `Status.HookEnabled` (proof the hook is live and, on macOS, that permission was granted — denied ⇒ no events at all).
- [keymap_test.go](../internal/hotkey/keymap_test.go): parse round-trips, labels, keycode lookup, and matcher edges (combo, auto-repeat, release order, reuse).

### Wiring — [app.go](../app.go)
`hotkey *hotkey.Listener` field (constructed in `NewApp`). `startHotkeyFromPrefs()` parses the saved key (falling back to `DefaultSpec`) and calls `Apply(ctx, enabled, spec)`; it runs from `startup` and at the end of `UpdatePreferences` (so toggling/rebinding takes effect live), with `Shutdown()` in `shutdown`. Two bound methods: `GetHotkeyStatus() hotkey.Status` and `OpenAccessibilitySettings()` (macOS deep-link).

### Preferences
`PushToTalkEnabled bool` (default **true**) + `PushToTalkKey string` (default `"RightAlt"`) on [session.go](../internal/models/session.go), persisted via the existing key-value pattern in [preferences.go](../internal/store/preferences.go) (bool stored as `"1"/"0"`).

### Frontend
- [App.tsx](../frontend/src/App.tsx): `handleMicToggle` (shared by the mic button and the hotkey) toggles `startRecording` / `stopAndSend`. A single `EventsOn("ptt:down")` effect, gated on `prefs.pushToTalkEnabled`, invokes it via a **latest-closure ref** (subscribe once, never stale, no re-subscribe churn). Guards: `pttBusyRef` serializes overlapping presses, session-gating + STT check inside `startRecording`, barge-in over TTS, and a 5-min safety auto-stop.
- [hotkey.ts](../frontend/src/lib/hotkey.ts): browser mirror of the Go keymap for the Settings capture UI (`comboFromKeyboardEvent` on a main-key press, `bareModifierFromCode` for a modifier alone). Uses `e.code` (physical key) to stay layout-independent and match the Go side.
- [wailsBridge.ts](../frontend/src/lib/wailsBridge.ts): re-exports `GetHotkeyStatus`, `OpenAccessibilitySettings`, and the runtime `EventsOn`/`EventsOff` (single import point).
- [Settings.tsx](../frontend/src/components/Settings.tsx): a **Voice Hotkey** section — On/Off segmented toggle, a "set hotkey" capture button (press any key/combo; Esc cancels; Reset to default), and a macOS Accessibility hint that appears when enabled but the hook isn't confirmed live.

## macOS Accessibility

libuiohook installs a `CGEventTap`, gated behind **Privacy & Security → Accessibility** — checked via `AXIsProcessTrustedWithOptions` (`internal/hotkey/permission_darwin.go`), the same call libuiohook makes internally. (Not **Input Monitoring**: that's a separate TCC permission this feature doesn't use, despite the name being the more common association with global key listening — an earlier version of this deep-link pointed there by mistake.) It can't be granted programmatically; the app summons the OS dialog (`kAXTrustedCheckOptionPrompt`) **at most once ever** — the first time the hook tries to start while untrusted — and records that in the store (`hotkey_prompted`, written by `service.Settings.ApplyHotkey` when `Listener.Apply` reports the dialog was shown). Every later check is silent (`AXIsProcessTrusted`): macOS re-shows the prompting dialog on *every* untrusted check — a denial is never remembered by the OS — so without the flag a user who denied would be re-prompted on every single launch. Granting usually needs a relaunch to take effect; after a denial the Settings hint is the only reminder (and a full data wipe clears the flag, so a reset app legitimately asks once again). Surfaced in Settings via `GetHotkeyStatus` (hint shows while `enabled && !hookEnabled` on darwin) + an "Open settings" button (`OpenAccessibilitySettings`). The click-to-toggle mic keeps working regardless (it only needs the mic permission). **Windows needs no permission.**

## Caveats
- **Passive leak** (by design): the held key also reaches the IDE. A combo can type, trigger app shortcuts, or — on macOS — ring the **unhandled-key alert beep** on every auto-repeat. Hence the bare-modifier default; combos remain available for those who want them.
- **macOS may swallow `Ctrl+Space`** for input-source switching → `ptt:down` never fires; rebind. (Another reason it isn't the default.)
- **Don't cross-compile** gohook (CGO + native toolchains) — build each OS natively or via a CI matrix.
- **Never restart the OS hook.** libuiohook keeps global state and its macOS event-tap teardown is **asynchronous**, so a Stop-then-Start cycle (e.g. on rebind) races the C layer and **segfaults** (observed: a native crash inside the `UpdatePreferences` dispatch when setting a key). The fix is the start-once design above — the hook is created on first enable and kept alive; enable/disable/rebind only swap guarded fields. Only `Shutdown` tears it down.

## Verification
- Automated: `go build/vet/test ./...` (incl. the matcher/parse tests) + `gofmt`; frontend `tsc --noEmit` + `vite build`; full `wails build` (CGO linked, frontend embedded, packaged). All green.
- Native (manual, in `wails dev`): grant Accessibility + relaunch; with another app focused and a session active, **tap** the hotkey (default Right ⌥ Option) → records; **tap again** → transcribes and sends; a fast second tap during transcription doesn't double-trigger; the mic button still toggles; barge-in stops TTS; rebinding (e.g. to `F8`) takes effect live without a crash. (The exact keycodes macOS reports can only be confirmed at runtime; the mapping follows gohook's convention.)

## Possible follow-ups
- An overlay "Press ⌥ to talk" hint while idle.
- Remaining Phase 3 shortcuts (end session, toggle capture) could share `internal/hotkey`.
- Optionally suppress the key from reaching the IDE (would require an active/blocking hook — platform-fragile; out of scope).
