# Architecture & Reference

> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md). This file holds the deeper reference: data flow, the full bound-method list, the interviewer-prompt spec, and the OpenRouter / ElevenLabs API contracts.

## Architecture diagram

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

## Key Go bindings (exposed to frontend)

These `app.go` methods are callable from TypeScript as async functions via `lib/wailsBridge.ts`. Wails auto-generates the TS types from the Go structs. After adding/changing one, run `wails generate module` and export it from the bridge.

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

// Models
func (a *App) ListAvailableModels() ([]models.Model, error)  // OpenRouter catalog for the picker (cached ~1h)

// Voice — ElevenLabs (PLANNED, Phase 2; all processing in Go, frontend only records/plays audio)
// func (a *App) TranscribeAudio(audioBase64 string) (string, error)  // Scribe v2
// func (a *App) SynthesizeSpeech(text string) (string, error)        // Flash v2.5 (stream via Wails events)
// func (a *App) ListVoices() ([]Voice, error)
// func (a *App) SetVoice(voiceID string) error
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
- Keep latency low: screenshot compression, conversation history trimming (last ~10 exchanges, `MaxHistoryMsgs` in `../internal/ai/client.go`), and — in Phase 2 — streaming TTS playback and streaming AI responses.
- (Phase 2) The frontend's only voice role: record raw mic audio (MediaRecorder) and play audio bytes from Go (Web Audio API). All processing and API calls happen in Go.
- (Phase 2) Push TTS audio chunks from Go to the frontend incrementally via Wails runtime events — start playing as soon as the first chunks arrive.
- For typed messages, skip ElevenLabs entirely — send text straight to OpenRouter and display the reply as text, with an optional "read aloud" button later.
