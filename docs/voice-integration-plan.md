# Voice Integration (ElevenLabs) — Implementation Plan (Phase 2)

> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md); phase status in [roadmap.md](roadmap.md); deeper API reference in [architecture.md](architecture.md). This doc breaks Phase 2 (voice) into sequenced, independently-shippable steps.

## Context

The app today is a **typed** screen-driven mock interview: the user types in the chat, Go bundles the text + latest screenshot, OpenRouter replies, and the reply renders as text. Phase 2 adds **voice** — the candidate *speaks* to the interviewer and *hears* it reply, like a real phone screen.

Much of the groundwork already exists, which keeps this small:
- The **ElevenLabs key is already collected and stored** ([SetupPage.tsx](../frontend/src/components/SetupPage.tsx), [Settings.tsx:262](../frontend/src/components/Settings.tsx)) and `GetAuthStatus` already reports `elevenLabsConfigured` ([app.go:101](../app.go)) — it's just unused.
- **`Preferences.VoiceID` already exists** and round-trips through SQLite ([session.go:43](../internal/models/session.go), [preferences.go:65,115](../internal/store/preferences.go)) — so saving a chosen voice needs **no new binding** (same trick the model picker uses).
- The **overlay already has a placeholder mic button + "Live" indicator** ([Overlay.tsx:47,67](../frontend/src/components/Overlay.tsx)); Settings has a "Voice Calibration" placeholder tab ([Settings.tsx:304](../frontend/src/components/Settings.tsx)).
- Clear patterns to mirror: **`ai.Client`** (apiKey + httpClient + headers + cached `ListModels`, [client.go](../internal/ai/client.go)) for the voice client, and **`ModelPicker`** ([ModelPicker.tsx](../frontend/src/components/ModelPicker.tsx)) for the voice picker.

**Outcome:** click the mic → speak → ElevenLabs Scribe transcribes → the existing interview loop runs → when **voice mode** is on, the reply is spoken via ElevenLabs Flash v2.5. Voice selection lives in Settings. The typed flow is untouched.

## Confirmed API contracts (verified live, June 2026)

All calls are made from Go only; auth is the `xi-api-key` header.

| Purpose | Call | Notes |
|---|---|---|
| **STT** | `POST /v1/speech-to-text` | `multipart/form-data`: `file` + `model_id=scribe_v2`. Returns `{ "text": "..." }`. `scribe_v1` is **deprecated** (removed 2026-07-09) — use `scribe_v2`. |
| **TTS** | `POST /v1/text-to-speech/{voice_id}` | JSON `{ "text", "model_id": "eleven_flash_v2_5" }`, `Accept: audio/mpeg`. Returns raw MP3. Optional `?output_format=mp3_44100_128` (default fine). Non-streaming. |
| **Voices** | `GET /v1/voices` | Returns `{ "voices": [ { voice_id, name, category, preview_url } ] }`. |

Base host: `https://api.elevenlabs.io`.

## Decisions (confirmed)

1. **TTS = non-streaming.** Synthesize the whole reply, return it, play in one shot. Replies are 1–3 sentences so Flash v2.5 latency is small. (Streaming via Wails events deferred — see Out of scope.)
2. **"Voice mode" speaker toggle.** Session-level; while on, **every** reply is spoken (typed or spoken). Auto-enables the first time the mic is used.
3. **Click-to-toggle mic.** Click to start recording, click again to stop → transcribe → send. (Global push-to-talk hotkey deferred to Phase 3 keyboard shortcuts.)

---

## Phase 0 — Prerequisite: microphone permission (do first)

Nothing downstream works until the OS lets the webview record.

- Add `NSMicrophoneUsageDescription` to the macOS `Info.plist` (Wails `build/darwin/Info.plist`, or the `info.plist` template). Without it, `getUserMedia` fails on WKWebView.
- The user must also grant mic access on first prompt.
- **Done when:** a throwaway `navigator.mediaDevices.getUserMedia({audio:true})` in `wails dev` triggers the OS prompt and resolves.

## Phase 1 — Voice backend foundation

Headless backend; no UI yet. Mirror the `ai` package structure throughout.

### 1.1 New DTO — `internal/models/voice.go`
```go
package models

// Voice is a selectable ElevenLabs voice (subset of /v1/voices, for the picker UI).
type Voice struct {
    ID         string `json:"id"`         // voice_id
    Name       string `json:"name"`
    Category   string `json:"category"`   // "premade" | "cloned" | ...
    PreviewURL string `json:"previewUrl"` // mp3 sample for the ▶ preview button
}
```

### 1.2 New package — `internal/voice/client.go`
Mirror `ai.Client` (struct with `apiKey` + `httpClient`, `NewClient`, shared header helper, mutex-guarded cache for voices like [client.go:149](../internal/ai/client.go)).
```go
func NewClient(apiKey string) *Client
func (c *Client) Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error) // scribe_v2, multipart
func (c *Client) Synthesize(ctx context.Context, voiceID, text string) ([]byte, error)           // eleven_flash_v2_5 → MP3
func (c *Client) ListVoices(ctx context.Context) ([]models.Voice, error)                          // cached ~1h
```
- `Transcribe`: build `multipart/form-data` (`mime/multipart`), part `file` with a filename whose extension matches `mimeType` (`audio/mp4`→`.m4a`, `audio/webm`→`.webm`, `audio/mpeg`→`.mp3`), field `model_id=scribe_v2`; parse `{ "text": "..." }`.
- `Synthesize`: POST JSON to `.../text-to-speech/{voiceID}`, `Accept: audio/mpeg`; on non-200 fold the body into the error (like [client.go:117](../internal/ai/client.go)); return raw bytes.
- `ListVoices`: GET `/v1/voices` → map → `[]models.Voice`; cache like `ListModels`.
- Constants: the three URLs, `scribeModel = "scribe_v2"`, `ttsModel = "eleven_flash_v2_5"`, `httpTimeout` (keep 60s), `voicesCacheTTL = time.Hour`.
- Add a small `mimeToExt(mimeType) string` helper (unit-tested).
- No import cycle: `voice` imports `internal/models`; `models` imports only `time`.

### 1.3 `app.go` — voice client + 3 bound methods
- Add `voiceClient *voice.Client` to `App`.
- `NewApp`: restore it from the persisted `elevenlabs` key (mirror `aiClient` restore, [app.go:49-53](../app.go)).
- `SetAPIKey`: when `provider == "elevenlabs"`, `a.voiceClient = voice.NewClient(key)` (mirror [app.go:94-96](../app.go)).
- New `// Voice` section:
```go
func (a *App) TranscribeAudio(audioBase64, mimeType string) (string, error)
func (a *App) SynthesizeSpeech(text string) (string, error) // returns base64 MP3
func (a *App) ListVoices() ([]models.Voice, error)
```
  - Each guards `a.voiceClient == nil` with a "set an ElevenLabs API key first" error (shape per [app.go:329](../app.go)).
  - `SynthesizeSpeech` reads `prefs.VoiceID`; if empty, fall back to a hardcoded default premade voice id (**confirm a current default id live**), so it works before the user picks one. Returns **base64** so MP3 crosses the Wails boundary as a string (same convention as screenshots).
  - **No `SetVoice` binding** — the picker persists `Preferences.VoiceID` through the existing `UpdatePreferences` ([app.go:312](../app.go)), exactly like the model picker.

### 1.4 Regenerate bindings + export
- `wails generate module` → adds the 3 methods + `models.Voice`.
- Export `TranscribeAudio`, `SynthesizeSpeech`, `ListVoices` from [wailsBridge.ts](../frontend/src/lib/wailsBridge.ts).

**Done when:** `go build ./...` + `go test ./...` pass; bindings appear in `frontend/wailsjs`.

## Phase 2 — Voice selection in Settings (first user-visible slice)

Mirror the model picker exactly.

### 2.1 `frontend/src/lib/useAudioPlayer.ts`
`play(src, rate=1)` (base64 MP3 *or* a preview URL) via a Blob URL / `new Audio()`; exposes `speaking` (during playback, for indicators) and `stop()`. Reused by both the picker preview and TTS playback.

**Voice speed** is applied client-side via `audio.playbackRate` (with `preservesPitch`), *not* the ElevenLabs `speed` param — it's free (no re-synthesis), instant, covers ~0.5×–2.0×, and also speeds up the picker previews. The rate is stored as `Preferences.voiceSpeed` (default 1.0) and chosen from a slider in Settings → Voice Calibration.

### 2.2 `VoicePicker.tsx` (+ `.css`) — mirror `ModelPicker`
```ts
interface Props { currentVoiceId: string; onSelect: (voiceId: string) => void; }
```
- On mount `ListVoices()` → state; loading + error (wrap for browser preview).
- Rows: name, category badge, **▶ preview button** (plays `previewUrl` via `useAudioPlayer`); search box filters by name; highlight the selected voice.
- MD3 tokens, mirroring [ModelPicker.css](../frontend/src/components/ModelPicker.css).

### 2.3 `Settings.tsx` — real "Voice Calibration" section
Replace the placeholder ([Settings.tsx:304-319](../frontend/src/components/Settings.tsx)) with: ElevenLabs status + "add a key" prompt when unconfigured, then `<VoicePicker currentVoiceId={prefs?.voiceId ?? ""} onSelect={saveVoice} />`. `saveVoice` mirrors `saveModel` ([Settings.tsx:110](../frontend/src/components/Settings.tsx)) via the shared `savePrefs({ voiceId }, "Voice saved.")`.

**Done when:** in browser preview (stubbed `ListVoices`) the picker renders/filters/previews; on desktop the chosen voice persists across restarts (SQLite `voice_id`).

## Phase 3 — Speech-to-text input (speak to ask)

### 3.1 `frontend/src/lib/useVoiceRecorder.ts`
Wraps `MediaRecorder`: `getUserMedia({audio})`, pick a supported type via `MediaRecorder.isTypeSupported` (**WKWebView yields `audio/mp4`, not webm/opus — detect, don't assume**), collect chunks, `stop()` resolves `{ base64, mimeType }` (strip the `data:` prefix). Exposes `recording`, `start()`, `stop()`, `error`, `supported`; handles permission-denied gracefully.

### 3.2 `App.tsx` — mic orchestration
- New state: `recording`, `transcribing` (and `voiceMode`, used in Phase 4).
- Mic toggle: start/stop recorder; on stop → `transcribing=true` → `TranscribeAudio(b64, mime)` → text → run the **existing `handleSend(text)`** ([App.tsx:153](../frontend/src/App.tsx)); set `voiceMode=true` on first mic use. try/catch → existing error banner.

### 3.3 `Chat.tsx` — mic button
Add a mic button to the input area beside Send using shared `.btn`/icon-button classes (no new styles). New **optional** props (`recording`, `transcribing`, `onMicToggle`) so typed-only usage is unaffected. Show recording/transcribing state.

**Done when:** on desktop, click mic → speak → your words appear as a user message and get a normal reply.

## Phase 4 — Text-to-speech output (hear replies)

### 4.1 `App.tsx` — speak after reply
- Extract a `speak(text)` helper using `useAudioPlayer`.
- In `handleSend`, after `SendMessage` returns: if `voiceMode` → `SynthesizeSpeech(reply)` → `play()`. Track `speaking` for indicators. Turning voice mode off calls `player.stop()`.

### 4.2 `Chat.tsx` — voice-mode (speaker) toggle
Add a speaker toggle (optional props `voiceMode`, `onToggleVoice`) next to the mic; reflects/controls whether replies are spoken.

**Done when:** with voice mode on, replies are spoken in the chosen voice; toggling off mid-playback stops audio and silences later replies; typed-only path stays silent when off.

## Phase 5 — Overlay wiring + indicators

Replace the local `muted` placeholder in [Overlay.tsx](../frontend/src/components/Overlay.tsx) with the real handlers/state from `App`: mic button drives recording, speaker toggle drives voice mode, and the **"Live" dot + under-glow** ([Overlay.tsx:47,88](../frontend/src/components/Overlay.tsx)) reflect real `recording`/`transcribing`/`speaking`. Update the component doc comment (currently says "placeholder until ElevenLabs STT is wired").

**Done when:** the full speak→hear loop works from the compact overlay with correct indicators.

## Phase 6 — Docs & cleanup

- Tick the Phase 2 boxes in [roadmap.md:24-33](roadmap.md); flip "Voice (Phase 2) is NOT built" → built; set "Next up" to the remaining Phase 3/4 items.
- Refresh the "Voice (Ph.2)" notes in [CLAUDE.md](../CLAUDE.md) and [architecture.md](architecture.md) to reflect non-streaming v1.

---

## Files touched (summary)
- **New:** `internal/voice/client.go`, `internal/models/voice.go`, `frontend/src/lib/useVoiceRecorder.ts`, `frontend/src/lib/useAudioPlayer.ts`, `frontend/src/components/VoicePicker.tsx` (+ `.css`), `docs/voice-integration-plan.md` (this doc).
- **Edit:** `app.go`, `frontend/src/lib/wailsBridge.ts`, `App.tsx`, `Chat.tsx`, `Overlay.tsx`, `Settings.tsx`, `build/darwin/Info.plist`, `docs/roadmap.md` (+ CLAUDE.md / architecture.md notes).
- **Generated:** `frontend/wailsjs/**`.

## Verification
1. `go build ./...`, `go test ./...` (incl. `mimeToExt` test), `gofmt`.
2. `wails generate module`, then `cd frontend && npx tsc --noEmit`.
3. **Browser preview** (`npm run dev`, stub `ListVoices`/`TranscribeAudio`/`SynthesizeSpeech`): VoicePicker render/filter/preview; mic + speaker button states. (Real `getUserMedia`/audio need desktop.)
4. **Desktop** (`wails dev`, real key, mic granted):
   - Settings → pick a voice → persists across restart.
   - Session → mic → speak → message + reply.
   - Voice mode on → reply spoken in chosen voice; toggle off mid-playback → stops.
   - Repeat from the overlay; "Live"/glow reflect recording → transcribing → speaking.
   - No ElevenLabs key → mic/voice shows a clear "set a key" error; typed flow still works.

## Out of scope (flagged for later)
- **Streaming TTS** (Wails events + chunked MP3) and **streaming AI text** — non-streaming suffices for short replies.
- **Global push-to-talk hotkey** — Phase 3 keyboard-shortcuts item.
- **Persisted "speak by default" preference** — voice mode is session-level for v1.
- **Voice cloning** — Phase 5 stretch goal.
