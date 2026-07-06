# Architecture & Reference

> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md). This file holds the deeper reference: data flow, the full bound-method list, the interviewer-prompt spec, and the OpenRouter / ElevenLabs API contracts.

## Architecture diagram

The backend is a **3-layer architecture**: a thin Wails binding facade, a service layer holding all business logic, and a data/infra layer (SQLite store + external API clients). The React frontend is the UI layer above them.

```
┌────────────────────────────────────────────────────────────────────────┐
│  Wails Desktop App (single binary)                                     │
│                                                                        │
│  React/TS Frontend ──── window.go.main.App.* ────►  Binding facade     │
│  (UI + mic record/play;                             (package main)     │
│   lib/wailsBridge.ts)   ◄──── Wails events ────     app.go: wiring +   │
│                              (ptt:down)             1-line delegation  │
│                                                     window.go: overlay │
│                                                     + wails runtime    │
│                                                          │             │
│                     SERVICE LAYER (internal/service)     ▼             │
│  ┌──────────────────────────────────────────────────────────────────┐ │
│  │ Interview            History          Voice         Settings     │ │
│  │ session state+mutex  debrief cache    STT/TTS       keys, prefs  │ │
│  │ send loop, company   policy, delete   resolution +  → capturer / │ │
│  │ start, meta extract  guard            fallbacks     hotkey sync  │ │
│  │                 Providers registry (live API clients, RWMutex)   │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│           │                    │                     │                 │
│           ▼ data               ▼ external clients    ▼ local infra     │
│     internal/store       internal/ai (OpenRouter)   internal/capture  │
│     (SQLite: sessions,   internal/voice (11Labs)    internal/hotkey   │
│      messages, prefs,    internal/googletts         internal/problems │
│      API keys)           internal/updater           (embedded CSV)    │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ├──► OpenRouter API (vision LLMs)
                                  ├──► ElevenLabs API (Scribe STT / Flash TTS)
                                  ├──► Google Cloud (TTS + STT)
                                  └──► GitHub Releases (update check)
```

All external API calls are centralized in the Go backend. The frontend handles UI rendering only (plus mic recording + audio playback). API keys and tokens never touch the frontend layer.

### Backend layering (why 3 layers)

- **Binding facade (`package main`)** — `App` is the single struct bound to Wails; its ~39 exported methods are the frozen TypeScript surface. Bodies are 1–3 line delegations, so `frontend/wailsjs` never churns when logic changes. `app.go` holds the struct, `NewApp` wiring, lifecycle hooks, and delegations; `window.go` is deliberately **the only file in `package main` that imports the Wails runtime** (overlay geometry, hide-for-snapshot, open-in-browser).
- **Service layer (`internal/service`)** — one file per concern: `Interview` (active-session state + mutex, the send loop, Company Practice starts, post-session meta extraction), `History` (transcript reads, delete guard, the debrief cache-then-generate policy), `Voice` (STT/TTS provider resolution with cross-fallbacks, audio base64 handling), `Settings` (keys + preferences, including propagating pref changes into the running capturer and hotkey). The `Providers` registry (RWMutex) is the one place that decides which API clients are live; Settings swaps entries on key changes and every service fetches at call time.
- **Data / infra layer** — `internal/store` (SQLite, pure CRUD) plus the client packages (`ai`, `voice`, `googletts`) and local infra (`capture`, `hotkey`, `problems`, `updater`). None of these know the layers above exist.

Services consume their dependencies through **narrow, consumer-defined interfaces** (`InterviewStore`, `HistoryStore`, `VoiceStore`, `SettingsStore`, `AI`, `TTS`/`STT`/`Speech`, `Screen`, `HotkeyApplier`) — `*store.DB` and the concrete clients satisfy them implicitly, and each interface documents exactly which data a feature touches. That buys unit tests with in-memory fakes (no SQLite file, no OS keyboard hook, no network — see `internal/service/*_test.go`) and made two latent races fixable in passing: client-pointer swaps are now mutex-guarded in the registry, and the active session is guarded by `Interview.mu` (released across the AI network call so a slow completion never blocks ending a session). The accepted trade-off: a screen of delegation boilerplate in the facade, paid once, in exchange for a stable binding surface and business logic that can be tested and changed without touching Wails.

### Frontend structure (feature folders)

Components live in `frontend/src/components/`, **one component + its own CSS each, grouped into a folder per feature (mostly a nav tab)** — so a reader opens `components/` and sees the app's surface area, not a flat 18-file list:

```
frontend/src/
├─ App.tsx        UI shell: floating pill-nav → tab views + live-session / overlay orchestration
├─ lib/           wailsBridge (the single Go-call entry point) + hooks + format / hotkey / markdown / audioToWav
└─ components/
   ├─ hub/        HubReady                                        — "Hub" tab (idle landing / start)
   ├─ company/    CompanyPractice · CompanyBanner                 — "Companies" tab
   ├─ history/    History · SessionHistoryCard · Debrief · RadarChart   — "History" tab
   ├─ settings/   Settings · ModelPicker · VoicePicker            — "Settings" tab
   ├─ session/    Chat · CapturePanel · Overlay · RegionSelector  — live interview (capture + chat + overlay)
   ├─ setup/      SetupPage                                       — first-run onboarding
   └─ common/     MessageBubble · WindowControls · UpdateBanner   — shared UI + app shell
```

Each feature folder owns its components outright; `common/` holds only genuinely cross-cutting pieces — `MessageBubble` is reused by both `session/Chat` and `history/SessionHistoryCard`, while `WindowControls` and `UpdateBanner` are app-shell chrome rendered directly by `App.tsx`. Cross-folder imports flow **into** `common/`, never sideways between feature folders.

## Data flow (core interview loop)

The app is **screen-driven** — the problem is never sent as text; it lives in the screenshot.

**Built today (typed):**
1. User codes in their own IDE or browser (VS Code, IntelliJ, terminal, LeetCode/NeetCode).
2. Go captures the selected display/region every N seconds via `kbinani/screenshot`.
3. User types a message in the chat panel.
4. Go bundles: the typed text + the **latest screenshot (base64)** + trimmed conversation history. **No problem statement is included — the screenshot carries the problem.**
5. Sends to OpenRouter with the interviewer system prompt → returns the AI's text reply.
6. Frontend renders the reply in chat (the overlay shows the latest interviewer line).
7. Session transcript is logged to SQLite.

**Built (Phase 2 — voice, non-streaming v1):** click-to-toggle mic → frontend records audio (`MediaRecorder`, `useVoiceRecorder`) → re-encoded to 16 kHz mono WAV (`audioToWav`) → base64 → Go `TranscribeAudio` → active STT provider (ElevenLabs Scribe if its key is set, else Google STT) → text → the same loop above → AI reply → (when "voice mode" is on) Go `SynthesizeSpeech` → active TTS provider (Google by default, or ElevenLabs Flash) → base64 MP3 → frontend plays the full clip (`useAudioPlayer`). Each provider is self-sufficient (one key = full voice); with both keys the default is the optimal combo, Scribe STT + Google TTS. Streaming TTS (chunked via Wails events) and streaming AI text are deferred — see [voice-integration-plan.md](voice-integration-plan.md).

**Built (Phase 3 — global voice hotkey):** in addition to the click-to-toggle mic, a configurable global hotkey **toggles** recording — press once to start, press again to stop & send (same as the mic button). A backend OS-level keyboard hook (`internal/hotkey`, `robotn/gohook`) runs on a libuiohook C thread and emits a `ptt:down` Wails event per press; `App.tsx` subscribes via `EventsOn`, toggling the existing `useVoiceRecorder` on each press and barging in over any TTS when recording starts. The hook is **passive** — it observes the keystroke but does not consume it, so the key still reaches the focused IDE (pick one it ignores; a tapped combo can type, fire shortcuts, or ring the macOS key beep). It works regardless of window focus and on both macOS and Windows; **macOS requires Input Monitoring permission** (no programmatic grant — surfaced via `GetHotkeyStatus` + `OpenInputMonitoringSettings` in Settings, and usually a relaunch). The auto-repeat debounce (one `ptt:down` per physical press) lives in a pure, unit-tested matcher; `keymap.go` maps the canonical hotkey string ↔ gohook keycodes ↔ display label. **Cross-compile caveat:** gohook needs CGO + native toolchains, so build each OS natively (or via a CI matrix) rather than cross-compiling.

**Built (Phase 3 — session history):** every session is persisted as it runs (it always has been — see *Persistence* below), and the **History** tab is the view onto it. `ListSessions` returns reverse-chronological summaries (title, difficulty, model, start/end → duration, message count); expanding a row lazy-loads its transcript via `GetSessionTranscript`; `DeleteSession` removes a session and its messages. Because the app is screen-driven (no problem text is stored), the problem **title + difficulty are AI-derived**: when `EndSession` runs it grabs the final screenshot (`capturer.Latest()`, which survives `Stop()`) and fires a best-effort background call (`ai.ExtractSessionMeta` — a **vision** OpenRouter request using `SessionMetaPrompt`) over the stored transcript + that screenshot, writing title, difficulty, **and a text transcription of the candidate's final code** back via `UpdateSessionMeta`. It never blocks ending a session, and short/failed extractions just leave the generic "Interview session" label.

**Debrief.** The expanded History card has a **Transcript / Debrief tab toggle**; the **Debrief** tab opens an AI **scorecard** (`models.Debrief`: 5-point hire verdict, a **five**-dimension 1-5 rubric — problem-solving, code quality, communication, complexity, **pace** — and strengths/improvements). `GetDebrief` generates it **lazily on first open and caches it** in `sessions.debrief` (re-opening reads from SQLite — zero tokens), using the session's own model; the card defaults to the Transcript tab so expanding never spends tokens. `ai.GenerateDebrief` (text-only, `DebriefPrompt`) reasons over the transcript **plus the captured `final_code`**, so it judges the real solution, not just the dialogue. Frontend: `History` + `SessionHistoryCard` (reusing `MessageBubble`) + `Debrief`, whose right column shows a five-bar metric list and a `RadarChart` pentagon (self-contained SVG, MD3 tokens); see [history-feature-plan.md](history-feature-plan.md).

## Persistence (SQLite)

All local state lives in one SQLite file, created on first launch (`store.Open`, [../internal/store/db.go](../internal/store/db.go)):

```
~/Library/Application Support/ai-interviewer/data.db   (+ -wal / -shm sidecars; WAL mode)
```

`migrate()` creates three tables idempotently (columns added in later versions are backfilled via `addColumnIfMissing`):

- **`sessions`** — one row per interview: `id`, `problem_id` (`""` for screen-driven; carries the company slug for Company Practice), `model`, `started_at`, `ended_at`, `problem_title` / `difficulty` (AI-derived for screen-driven; **seeded from the assigned problem** for Company Practice, and preserved by the end-of-session labeling call), the `final_code` snapshot (text transcription of the candidate's final on-screen solution, captured at session end), the cached `debrief` JSON (generated on first open), and `company` / `mode` (`"single"`/`"mock"`, set only for Company Practice so History can badge them). Written by `CreateSession` (on start) and `EndSession` (stamps `ended_at`); labeled + code-snapshotted by `UpdateSessionMeta`; company-tagged by `SetSessionCompany`; debrief cached by `SaveSessionDebrief`; removed by `DeleteSession`.
- **`messages`** — one row per turn (user + interviewer): `role`, `content`, `has_image`, `created_at`. Written by `AddMessage` for **both** turns of every `SendMessage` — this is the stored transcript. **Text only; screenshots are never persisted** (they live in memory during the live session, then are discarded).
- **`preferences`** — a generic key-value store ([../internal/store/preferences.go](../internal/store/preferences.go)) for settings **and** API keys (stored locally, unencrypted — see roadmap Phase 4).

The session/message **write** path (`CreateSession` / `AddMessage` / `EndSession`) predates the history feature — the data was always being recorded; History (plus `DeleteSession` / `UpdateSessionMeta` and the two new `sessions` columns) is what surfaces and manages it. Nothing in `data.db` is uploaded — the only network calls are the live AI/voice API requests.

## Key Go bindings (exposed to frontend)

These bound methods are callable from TypeScript as async functions via `lib/wailsBridge.ts`. Wails auto-generates the TS types from the Go structs. The bodies are thin delegations into `internal/service` (see *Backend layering* above) — the signatures below are the frozen contract, so refactoring service internals never regenerates `wailsjs`. After adding/changing a **signature**, run `wails generate module` and export it from the bridge.

```go
// Auth / keys   (OAuth PKCE is planned — Phase 4)
func (a *App) GetAuthStatus() models.AuthStatus            // openRouterConfigured / elevenLabsConfigured
func (a *App) SetAPIKey(provider, key string) error        // "openrouter" or "elevenlabs"

// Interview
func (a *App) StartSession(model string) (models.Session, error)  // no problemID — screen-driven
func (a *App) EndSession(sessionID string) error
func (a *App) SendMessage(text string) (string, error)           // text + latest screenshot → OpenRouter → reply

// Screen capture
func (a *App) StartCapture(intervalMs int) error
func (a *App) StopCapture() error
func (a *App) GetLatestScreenshot() (string, error)              // base64 PNG of the cropped region
func (a *App) ListDisplays() []capture.DisplayInfo
func (a *App) SnapshotDisplay(displayIndex int) (string, error)  // for the region picker
func (a *App) SetCaptureRegion(displayIndex int, x, y, w, h float64) error

// Window / overlay
func (a *App) EnterOverlayMode()                  // shrink, pin always-on-top, park top-center
func (a *App) ExitOverlayMode()                   // restore the full window
func (a *App) SetOverlayExpanded(expanded bool)   // grow the overlay window for the history dropdown

// Sessions (history view) — persisted in SQLite; see "Persistence" above
func (a *App) ListSessions() ([]models.SessionSummary, error)            // reverse-chron summaries: title/difficulty (AI-derived), model, startedAt/endedAt (→ duration), messageCount
func (a *App) GetSessionTranscript(id string) ([]models.Message, error)  // full transcript, lazy-loaded when a row is expanded
func (a *App) GetDebrief(id string) (models.Debrief, error)              // post-interview scorecard; generated once (transcript + final_code) then cached in sessions.debrief
func (a *App) DeleteSession(id string) error                            // deletes the session + its messages (refuses the active session)

// Company Practice (opt-in tab) — question pools from internal/problems (embedded
// metadata only). Problems are assigned by reference; the AI still reads the real
// problem off the screenshot. Both Start* return CompanySessionStart {Session,
// Company, Opening, Problems}; the opener is persisted to the transcript but NOT
// into model history (the system prompt carries the assignment).
func (a *App) ListCompanies() []models.CompanyInfo                                                          // pool summaries (name, count, mockEligible)
func (a *App) ListCompanyProblems(slug string) ([]models.Problem, error)                                    // one company's pool (filtered/sorted client-side)
func (a *App) StartCompanySession(slug string, problem models.Problem) (models.CompanySessionStart, error)  // single chosen problem
func (a *App) StartMockInterview(slug string) (models.CompanySessionStart, error)                           // two-problem draw (server-side; the picker never sees them)
func (a *App) OpenURL(url string) error                                                                     // open a LeetCode link in the real browser

// Settings
func (a *App) GetPreferences() (models.Preferences, error)
func (a *App) UpdatePreferences(prefs models.Preferences) error

// Models
func (a *App) ListAvailableModels() ([]models.Model, error)  // OpenRouter catalog for the picker (cached ~1h)

// Voice — STT and TTS each resolve a provider (Google or ElevenLabs) via the
// service layer's TTS/STT interfaces (non-streaming v1; all processing in Go,
// frontend only records/plays audio)
func (a *App) TranscribeAudio(audioBase64, mimeType string) (string, error) // active STT: Scribe if EL key set, else Google
func (a *App) SynthesizeSpeech(text string) (string, error)                 // active TTS provider → base64 MP3
func (a *App) ListVoices() ([]models.Voice, error)                          // active provider's voice catalog
func (a *App) PreviewVoice(voiceID string) (string, error)                  // synthesize a sample (providers w/o preview URLs)
// Saving a voice needs no binding — VoicePicker writes Preferences.VoiceID /
// GoogleVoiceID (and TTSProvider) via UpdatePreferences. service.Voice's
// activeTTS() picks the provider+voice, falling back to whichever key is
// configured.

// Push-to-talk (global hotkey) — a backend keyboard hook (internal/hotkey via
// robotn/gohook) emits a "ptt:down" Wails event per press; the frontend toggles
// recording on it through the same recorder path. Enable + key live
// in Preferences (PushToTalkEnabled, PushToTalkKey, default "RightAlt").
// UpdatePreferences applies them via Listener.Apply, which swaps the matched key
// on the long-lived hook — the OS hook is started once and never restarted
// (restarting libuiohook mid-run segfaults on macOS).
func (a *App) GetHotkeyStatus() hotkey.Status   // running/hookEnabled/spec/label/goos — drives the macOS permission hint
func (a *App) OpenInputMonitoringSettings()     // macOS: open System Settings → Privacy & Security → Input Monitoring
```

## AI interviewer system prompt (core behavior)

The system prompt ([../internal/ai/prompts.go](../internal/ai/prompts.go)) is the most important tunable. The interviewer must:

- Never give away the answer or key insight
- Use Socratic questioning: "What data structure could help you look things up in O(1)?"
- React to the code it sees in the screenshot without volunteering solutions
- Keep responses short (1-3 sentences) like a real interviewer
- Ask about time/space complexity when the candidate proposes an approach
- Probe edge cases: "What happens if the input is empty?"
- Only respond when spoken to — don't interrupt unprompted
- Match the tone of a senior engineer, not a cheerful chatbot

**There is no written problem statement.** A screenshot of the candidate's current screen is attached to their **latest message only** — the interviewer reads the problem and the current code from it (it may show an IDE, a LeetCode/NeetCode page, a terminal, or a browser). Earlier messages do not carry screenshots; this is intentional. Conversation history is included for continuity. If the interviewer can't yet tell what the problem is, it asks the candidate to clarify rather than guessing.

**Company Practice** builds on the same base via `BuildCompanySystemPrompt` ([../internal/ai/prompts.go](../internal/ai/prompts.go)): it appends a company persona (from `companyProfiles`) and the assignment **by reference** (title + difficulty only — never the statement). So the interviewer can "greet first" without a leading assistant turn in model history (which some models reject), the opener is a **deterministic template** — persisted to the transcript and spoken (if voice is on) — while the system prompt *encodes* that the greeting already happened; model history stays `system → user → …`. Mock interviews carry **both** problems plus the Q1→Q2 handoff rules (never name Q2 early; move on when Q1 is solved, the candidate is stuck, or they ask). See [company-practice-plan.md](company-practice-plan.md).

Two further prompts live in the same file, both **post-session only** (never fed to the live interview, so the screen-driven invariant holds): **`SessionMetaPrompt`** labels a *finished* session with a short title + difficulty **and transcribes the final on-screen code** from the end-of-session screenshot (strict-JSON, parsed by `ExtractSessionMeta`); **`DebriefPrompt`** drops the interviewer persona and returns the post-interview scorecard JSON (parsed by `GenerateDebrief`) over the transcript + captured `final_code`. Both reply in strict JSON and reuse the same brace-extraction tolerance.

## ElevenLabs API reference (Phase 2 — built, non-streaming v1)

Three endpoints are used, all called from the Go backend only (`internal/voice/client.go`). Auth is the `xi-api-key` header.

### Text-to-Speech (non-streaming)

`POST https://api.elevenlabs.io/v1/text-to-speech/{voice_id}`

Returns the full MP3 as bytes; Go base64-encodes them for the Wails boundary and the frontend plays the whole clip. Flash v2.5 keeps latency low (~75ms) and replies are short, so one-shot synthesis is fine. (The `/stream` variant + chunked Web Audio playback is a deferred follow-up.)

```json
{ "text": "Have you considered what happens with an empty array?", "model_id": "eleven_flash_v2_5" }
```

### Speech-to-Text (Scribe v2)

`POST https://api.elevenlabs.io/v1/speech-to-text`

Accepts an audio file upload, returns transcribed text. The frontend records via `MediaRecorder` (WebM/opus on Chromium, **audio/mp4 on macOS WKWebView**), sends a base64 blob + its MIME type to Go, and Go forwards it to Scribe with a matching filename extension. `scribe_v1` is deprecated (removed 2026-07-09); use `scribe_v2`.

```
Content-Type: multipart/form-data
- file: audio blob
- model_id: "scribe_v2"
```

### Voices

`GET https://api.elevenlabs.io/v1/voices` → `{ voices: [ { voice_id, name, category, preview_url } ] }`, cached ~1h. Feeds the Settings `VoicePicker`; the chosen id persists to `Preferences.VoiceID`.

## Google Cloud reference (alternate provider, default for TTS)

`internal/googletts` mirrors `internal/voice`'s surface so the two are interchangeable behind the service layer's `TTS`/`STT` interfaces (`service.Speech` = both). Google uses plain API-key auth (`?key=`), so it reuses the existing key-storage pattern.

- **Synthesize:** `POST https://texttospeech.googleapis.com/v1/text:synthesize?key=KEY` with `{ "input": { "text" }, "voice": { "languageCode": <derived from voice name>, "name": <voiceID> }, "audioConfig": { "audioEncoding": "MP3" } }`. Response is JSON `{ "audioContent": "<base64 mp3>" }` — decoded to raw bytes so it matches ElevenLabs' contract.
- **Voices:** `GET .../v1/voices?key=KEY`, cached ~1h. Filtered to English locales and the high-quality families (Neural2, Chirp3-HD, WaveNet, Studio) and sorted. **No preview URLs** — `Voice.PreviewURL` is empty and the picker uses `PreviewVoice` to synthesize a sample on demand.
- **Transcribe (STT):** `POST https://speech.googleapis.com/v1/speech:recognize?key=KEY` with `{ "config": { "encoding": "LINEAR16", "sampleRateHertz": 16000, "languageCode": "en-US", "enableAutomaticPunctuation": true }, "audio": { "content": <base64> } }`; transcripts in `results[].alternatives[0].transcript` are joined.
- Default voice: `en-US-Neural2-F`.

**Audio format for STT:** the frontend re-encodes every recording to **16 kHz mono WAV (LINEAR16)** in the browser ([`audioToWav.ts`](../frontend/src/lib/audioToWav.ts), via `decodeAudioData` + `OfflineAudioContext`) before upload. This is required because WKWebView's `MediaRecorder` emits AAC/MP4, which Google STT rejects; WAV is accepted by both Google and ElevenLabs Scribe, so one capture path serves both providers.

### Cost model

TTS is billed per character of input text; STT per minute of audio. **Google Neural2 (~$16/1M chars, with a free monthly tier) is ~10× cheaper than ElevenLabs Flash**, which is why Google is the default TTS provider; ElevenLabs is the premium option. For a typical session (30-60 min, short 1-3 sentence interviewer turns) costs are minimal either way. Keep responses short to optimize both latency and cost. **Speed** is applied client-side (`audio.playbackRate` + `preservesPitch`), not via either provider's API.

## OpenRouter API reference

Base URL: `https://openrouter.ai/api/v1/chat/completions`

Request format follows the OpenAI chat completions spec. Vision messages include `image_url` with a base64 data URI. Auth is via Bearer token (manual key today; OAuth later). Note the screenshot rides on the latest user message; the system prompt contains no problem text. Always send `max_tokens` — interviewer replies are short, and an unset cap makes OpenRouter pre-authorize credits for the model's full output limit (causes 402s on low balances). See [../internal/ai/client.go](../internal/ai/client.go).

```json
{
  "model": "anthropic/claude-sonnet-4",
  "messages": [
    { "role": "system", "content": "You are a technical interviewer..." },
    {
      "role": "user",
      "content": [
        { "type": "text", "text": "I think I should use a hashmap here" },
        { "type": "image_url", "image_url": { "url": "data:image/png;base64,..." } }
      ]
    }
  ]
}
```

To list selectable models: `GET https://openrouter.ai/api/v1/models` (see [model-picker-plan.md](model-picker-plan.md) for the schema and the planned picker).

## Operational notes

- The window runs **frameless + transparent**; the **overlay** bar floats always-on-top over the user's IDE. Enter it with the "Compact" button during a session; expand/restore from the bar controls. Frameless removes the native title bar app-wide — quit via Cmd+Q / the app menu.
- Screen capture should exclude the app's own window where possible to avoid recursive capture.
- Keep latency low: screenshot compression, conversation history trimming (last ~10 exchanges, `MaxHistoryMsgs` in `../internal/ai/client.go`). Streaming TTS playback and streaming AI responses are deferred latency wins (out of scope for the non-streaming v1).
- The frontend's only voice role: record raw mic audio (`MediaRecorder`) and play audio bytes returned from Go. All processing and API calls happen in Go.
- TTS is non-streaming in v1: Go returns the full MP3 (base64) and the frontend plays the whole clip. Chunked streaming via Wails runtime events is a later follow-up.
- For typed messages with voice mode off, skip ElevenLabs entirely — send text straight to OpenRouter and display the reply as text. Voice mode also speaks typed replies.
- **Distribution & updates** (macOS, unsigned): CI builds on every `main` push; `vX.Y.Z` tags publish a public GitHub Release; on launch the app checks for a newer release (`internal/updater`, bound via `CheckForUpdate`/`GetAppVersion`/`OpenReleasePage`) and shows a download banner. Full explainer: [ci-cd-and-auto-update.md](ci-cd-and-auto-update.md).
