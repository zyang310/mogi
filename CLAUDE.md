# CLAUDE.md — Mock Interview Desktop App

## Project overview

This is a desktop application that acts as a live AI-powered mock coding interview coach. The user codes in their own IDE or a browser tab (VS Code, IntelliJ, terminal, LeetCode, NeetCode) while the app watches their screen and provides real-time interviewer feedback through an AI assistant. The AI behaves like a real technical interviewer — Socratic, nudging, never giving away answers.

**The app is screen-driven:** there is no problem bank and no written problem statement. The interviewer reads both the problem *and* the candidate's current code directly from a screenshot of the user's screen.

> **Current status** (keep this honest as the project moves):
> - **Phase 1 (screen-driven, typed core loop) is built**, and the UI has been redesigned onto a Material Design 3 dark theme (floating pill nav, idle "Ready to Begin?" hub, active capture + chat).
> - The **always-on-top floating overlay** bar (a Phase 4 item) was built early — entered via the "Compact" button during a session.
> - **Voice (Phase 2) is NOT built yet.** The overlay's "Live" indicator and mic button are placeholders, and the transcript mirrors the latest AI text message.
> - **Next up: Phase 2 — Voice.**

## Tech stack

* Framework: Wails v2 — Go backend + web frontend in a single native binary, uses the OS webview (no Chromium). The window runs **frameless + transparent** so the overlay bar can float over the user's IDE.
* Backend: Go — screen capture, all external API calls (OpenRouter; ElevenLabs once voice lands), local storage, and window/system operations.
* Frontend: React + TypeScript + Vite — interviewer chat, screen-capture preview, settings, setup, and the floating overlay. **Styling is plain CSS using a Material Design 3 token system (CSS variables) — no Tailwind.**
* AI gateway: OpenRouter (https://openrouter.ai) — unified API for Claude, GPT, Gemini, etc. Auth today is a **manual API key**; OAuth PKCE is planned (Phase 4).
* Voice I/O (planned, Phase 2): ElevenLabs via the Go backend — TTS using Flash v2.5 (~75ms latency, streaming) and STT using Scribe v2. All voice will route through Go so API keys never touch the frontend.
* Screen capture: Go-native via `kbinani/screenshot` (+ `golang.org/x/image` for cropping/encoding) — periodic screenshots, base64-encoded, sent to the vision model.
* Local storage: SQLite via `mattn/go-sqlite3` — session history, user preferences, and API keys.

## Architecture

```
┌───────────────────────────────────────────────────────┐
│  Wails Desktop App (single binary)                    │
│                                                        │
│  ┌──────────────────────┐   ┌──────────────────────┐  │
│  │   Go Backend          │   │  React/TS Frontend   │  │
│  │                       │   │                      │  │
│  │  - Screen capture     │◄──┤  - Chat UI           │  │
│  │  - OpenRouter API     │──►│  - Capture preview   │  │
│  │  - ElevenLabs (Ph.2)  │   │  - Floating overlay  │  │
│  │  - SQLite store       │   │  - Settings / setup  │  │
│  │  - Window / overlay   │   │  - Mic record (Ph.2) │  │
│  └──────────┬────────────┘   └──────────────────────┘  │
│             │                         │                 │
│             ▼                         ▼                 │
│        OS native APIs          OS native webview        │
└───────────────────────────────────────────────────────┘
              │
              ├──► OpenRouter API ──► Claude / GPT / Gemini (vision)
              │
              └──► ElevenLabs API ──► TTS (Flash v2.5) / STT (Scribe v2)   [planned]
```

All external API calls are centralized in the Go backend. The frontend handles UI rendering only (and, in Phase 2, mic recording + audio playback). API keys and tokens never touch the frontend layer.

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

**Planned (Phase 2 — voice):** push-to-talk → frontend records audio (MediaRecorder) → Go → ElevenLabs Scribe v2 (STT) → text → the same loop above → AI reply → ElevenLabs Flash v2.5 (streaming TTS) → audio chunks pushed to the frontend via Wails runtime events → Web Audio API playback.

## Project structure

```
ai-interviewer/
├── CLAUDE.md
├── wails.json                  # Wails project config
├── main.go                     # Entry point; frameless + transparent window options
├── app.go                      # Core App struct + all bound methods (kept thin)
├── go.mod / go.sum
├── internal/
│   ├── ai/
│   │   ├── client.go           # OpenRouter client (chat completions + vision)
│   │   ├── prompts.go          # Interviewer system prompt (screen-driven)
│   │   └── strip_test.go
│   ├── capture/
│   │   └── screen.go           # Screen capture + region cropping (kbinani/screenshot)
│   ├── models/
│   │   └── session.go          # Session, Message, SessionSummary, AuthStatus, Preferences
│   └── store/
│       ├── db.go               # SQLite init + migrations
│       ├── sessions.go         # Session / message CRUD
│       └── preferences.go      # User settings + API-key storage
├── frontend/
│   ├── index.html              # Loads Geist, JetBrains Mono, Material Symbols
│   ├── src/
│   │   ├── App.tsx             # Shell: pill nav; idle hub vs active session vs overlay
│   │   ├── main.tsx
│   │   ├── style.css           # Global reset + MD3 design tokens (:root CSS variables)
│   │   ├── App.css
│   │   ├── components/
│   │   │   ├── SetupPage.tsx       # First-run welcome + API key entry
│   │   │   ├── HubReady.tsx        # Idle "Ready to Begin?" hub
│   │   │   ├── CapturePanel.tsx    # "What the AI sees" preview (active session)
│   │   │   ├── RegionSelector.tsx  # Pick a display / crop a capture region
│   │   │   ├── Chat.tsx            # Interviewer chat panel
│   │   │   ├── MessageBubble.tsx   # Individual message
│   │   │   ├── Overlay.tsx         # Always-on-top floating bar (Compact mode)
│   │   │   ├── Settings.tsx        # Keys, capture interval, session time limit
│   │   │   └── *.css               # One CSS file per component (MD3 tokens)
│   │   └── lib/
│   │       └── wailsBridge.ts  # Single import point for bound Go methods + models
│   ├── wailsjs/                # Auto-generated bindings (do not hand-edit; `wails generate module`)
│   ├── tsconfig.json
│   ├── vite.config.ts
│   └── package.json
└── build/                      # Wails build output

# Planned, not yet present:
#   internal/voice/       (Phase 2 — ElevenLabs TTS/STT)
#   internal/auth/        (Phase 4 — OAuth PKCE)
#   frontend/src/hooks/   (useAudioRecorder, useAudioPlayback — Phase 2)
```

## Key Go bindings (exposed to frontend)

These `app.go` methods are callable from TypeScript as async functions via `lib/wailsBridge.ts`. Wails auto-generates the TS types from the Go structs.

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

// Sessions
func (a *App) ListSessions() ([]models.SessionSummary, error)
func (a *App) GetSessionTranscript(id string) ([]models.Message, error)

// Settings
func (a *App) GetPreferences() (models.Preferences, error)
func (a *App) UpdatePreferences(prefs models.Preferences) error

// Voice — ElevenLabs (PLANNED, Phase 2; all processing in Go, frontend only records/plays audio)
// func (a *App) TranscribeAudio(audioBase64 string) (string, error)  // Scribe v2
// func (a *App) SynthesizeSpeech(text string) (string, error)        // Flash v2.5 (stream via Wails events)
// func (a *App) ListVoices() ([]Voice, error)
// func (a *App) SetVoice(voiceID string) error
```

## AI interviewer system prompt (core behavior)

The system prompt (`internal/ai/prompts.go`) is the most important tunable. The interviewer must:

* Never give away the answer or key insight
* Use Socratic questioning: "What data structure could help you look things up in O(1)?"
* React to the code it sees in the screenshot without volunteering solutions
* Keep responses short (1-3 sentences) like a real interviewer
* Ask about time/space complexity when the candidate proposes an approach
* Probe edge cases: "What happens if the input is empty?"
* Only respond when spoken to — don't interrupt unprompted
* Match the tone of a senior engineer, not a cheerful chatbot

**There is no written problem statement.** A screenshot of the candidate's current screen is attached to their **latest message only** — the interviewer reads the problem and the current code from it (it may show an IDE, a LeetCode/NeetCode page, a terminal, or a browser). Earlier messages do not carry screenshots; this is intentional. Conversation history is included for continuity. If the interviewer can't yet tell what the problem is, it asks the candidate to clarify rather than guessing.

## Coding conventions

### Go

* Standard `gofmt` formatting
* Error handling: return errors, don't panic. Wrap with `fmt.Errorf("context: %w", err)`
* Structs that cross the Wails boundary need `json:"fieldName"` tags
* Use `context.Context` for cancellable operations (API calls, screen capture loops)
* Keep `app.go` thin — delegate to `internal/` packages

### TypeScript / React

* Functional components with hooks only
* State management: React state + props (no Redux). Session / overlay / view state lives in `App.tsx`
* **Styling: plain CSS with the Material Design 3 token system — do not add Tailwind** (see Design system)
* All Wails-bound Go calls go through `lib/wailsBridge.ts` for a single import point
* Handle loading/error states for every async Go call

### Design system

* Material Design 3 dark theme. Tokens are CSS variables in `frontend/src/style.css` `:root` — e.g. `--background:#111317`, `--primary-container:#4d8eff`, `--secondary:#4edea3` (green accent), `--on-surface:#e2e2e8`, and `--level-1/2` glass surfaces.
* Fonts: **Geist** (body), **JetBrains Mono** (timers/mono/code), **Material Symbols Outlined** (icons) — loaded in `frontend/index.html`.
* Each component has its own `.css` file referencing `var(--token)`; `SetupPage.css` is the reference for established conventions.
* The window is transparent, so full-screen views (`.app`, `.setup-root`) paint their own opaque background; the overlay (`.overlay-root`) stays transparent so the bar floats over the IDE.
* Mockups come from Google Stitch (which emits Tailwind + an MD3 token config). Port them to these CSS variables — never add the Tailwind toolchain.

### General

* Commit messages: conventional commits (`feat:`, `fix:`, `chore:`, `docs:`)
* No hardcoded API keys anywhere — always runtime config (stored in SQLite) or OAuth tokens
* Screen capture runs on a configurable interval (default 3 seconds)
* All user data stays local (SQLite). No telemetry, no cloud sync.

## Development workflow

```bash
# Dev mode (hot reload frontend + Go rebuild)
wails dev

# Build production binary
wails build

# Regenerate TS bindings after adding/changing a bound Go method
wails generate module

# Frontend only (for UI work — Wails runtime calls no-op in the browser)
cd frontend && npm run dev
```

## Implementation phases

### Phase 1 — Core loop (MVP) — ✅ Done

* [x] Wails + React-TS scaffold
* [x] Manual API key input for OpenRouter (setup screen)
* [x] **Screen-driven** — the AI reads the problem from the screen (replaced the original "single hardcoded Two Sum"; there is no problem bank)
* [x] Screen capture on a timer → base64, with display + region selection
* [x] Chat UI: typed message + screenshot → OpenRouter → display response
* [x] Interviewer system prompt tuned for Socratic, screen-reading behavior
* [x] SQLite schema: sessions + messages

### Phase 2 — Voice integration (ElevenLabs) — ⏳ Next up (not started)

* [ ] Manual API key input for ElevenLabs (already collected on the setup screen; unused so far)
* [ ] Frontend mic recording via MediaRecorder (push-to-talk → audio blob)
* [ ] Go: audio blob → ElevenLabs Scribe v2 → transcribed text
* [ ] Go: AI response text → ElevenLabs TTS Flash v2.5 (streaming) → audio
* [ ] Frontend audio playback via Web Audio API (play chunks as they arrive)
* [ ] Voice selection UI (fetch + display available ElevenLabs voices)
* [ ] Visual indicators: recording / AI-speaking / transcribing — wire the overlay's "Live" + mic for real
* [ ] (Pairs naturally) stream the AI text response so TTS can start sooner

### Phase 3 — UX — ◑ Partial

* [x] Settings panel (capture interval, session time limit, key management)
* [x] Display / capture-region selection
* [ ] Model picker (fetch available models from OpenRouter)
* [ ] Session history view (bindings `ListSessions` / `GetSessionTranscript` exist; the History tab is a placeholder)
* [ ] Keyboard shortcuts (push-to-talk, end session, toggle capture)
* ~~Problem bank with JSON seed + problem selector~~ — **dropped (screen-driven design)**

### Phase 4 — Auth and polish — ◑ Partial

* [x] Always-on-top floating **overlay** mode (manual "Compact" toggle) + frameless/transparent window
* [ ] OpenRouter OAuth PKCE flow
* [ ] Encrypted token/key persistence (keys are stored in SQLite today, unencrypted)
* [ ] Post-interview debrief mode (AI drops the interviewer persona, gives direct feedback)
* [ ] Session export (markdown transcript with timestamps)
* [ ] (Overlay follow-ups) auto-collapse on window blur; true see-through is in place, custom min/close controls optional

### Phase 5 — Stretch goals

* [ ] Difficulty adaptation (AI adjusts hint level based on progress)
* [ ] Timer / time pressure mode
* [ ] Multi-problem interview sets (simulate a full interview round)
* [ ] ElevenLabs voice cloning (user uploads an interviewer voice sample)

## Key dependencies

### Go

```
github.com/wailsapp/wails/v2     # Wails framework
github.com/kbinani/screenshot    # Cross-platform screen capture
golang.org/x/image               # Image cropping / encoding for capture
github.com/mattn/go-sqlite3      # SQLite driver
github.com/google/uuid           # Session IDs
# ElevenLabs (Phase 2) will be added when voice lands — direct HTTP or a client lib
```

### Frontend (npm)

```
react, react-dom                 # UI framework
typescript                       # Type safety
vite                             # Build tool (Wails template)
# No Tailwind — plain CSS with MD3 tokens (see Design system)
```

## ElevenLabs API reference (Phase 2 — planned)

Two endpoints will be used, both called from the Go backend only.

### Text-to-Speech (streaming)

`POST https://api.elevenlabs.io/v1/text-to-speech/{voice_id}/stream`

Returns raw audio bytes (MP3) via chunked transfer encoding. The frontend plays chunks as they arrive for minimal perceived latency. Use Flash v2.5 for lowest latency (~75ms).

```json
{
  "text": "Have you considered what happens with an empty array?",
  "model_id": "eleven_flash_v2_5",
  "voice_settings": { "stability": 0.5, "similarity_boost": 0.75 }
}
```

Auth: `xi-api-key` header with the ElevenLabs API key.

### Speech-to-Text (Scribe v2)

`POST https://api.elevenlabs.io/v1/speech-to-text`

Accepts an audio file upload (WAV, MP3, WebM), returns transcribed text. The frontend records audio via MediaRecorder as WebM/opus, sends a base64 blob to Go, and Go forwards it to Scribe.

```
Content-Type: multipart/form-data
- file: audio blob
- model_id: "scribe_v2"
```

### Cost model

TTS is billed per character of input text; STT per minute of audio. For a typical session (30-60 min, short 1-3 sentence interviewer turns) costs are minimal. Keep responses short to optimize both latency and cost.

## OpenRouter API reference

Base URL: `https://openrouter.ai/api/v1/chat/completions`

Request format follows the OpenAI chat completions spec. Vision messages include `image_url` with a base64 data URI. Auth is via Bearer token (manual key today; OAuth later). Note the screenshot rides on the latest user message; the system prompt contains no problem text.

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

## Notes

* The window runs **frameless + transparent**; the **overlay** bar floats always-on-top over the user's IDE. Enter it with the "Compact" button during a session; expand/restore from the bar controls. Frameless removes the native title bar app-wide — quit via Cmd+Q / the app menu.
* The app is **screen-driven** — the AI reads the problem and code from the screenshot; no problem statement is sent.
* Screen capture should exclude the app's own window where possible to avoid recursive capture.
* The AI should not respond unless the user has typed (or, in Phase 2, spoken) first — no unprompted interruptions.
* Keep latency low: screenshot compression, conversation history trimming (last ~10 exchanges), and — in Phase 2 — streaming TTS playback and streaming AI responses.
* All API keys (OpenRouter token, ElevenLabs key) are stored and used exclusively in the Go backend (SQLite) — the frontend never sees them.
* (Phase 2) The frontend's only voice role: record raw mic audio (MediaRecorder) and play audio bytes from Go (Web Audio API). All processing and API calls happen in Go.
* (Phase 2) Push TTS audio chunks from Go to the frontend incrementally via Wails runtime events — start playing as soon as the first chunks arrive.
* For typed messages, skip ElevenLabs entirely — send text straight to OpenRouter and display the reply as text, with an optional "read aloud" button later.
```
