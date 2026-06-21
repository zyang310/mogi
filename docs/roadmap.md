# Roadmap & Status

> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md). This file is the implementation roadmap — keep the statuses honest as the project moves.

## Current status

- **Phase 1 (screen-driven, typed core loop) is built**, and the UI has been redesigned onto a Material Design 3 dark theme (floating pill nav, idle "Ready to Begin?" hub, active capture + chat).
- The **always-on-top floating overlay** bar (a Phase 4 item) was built early — entered via the "Compact" button during a session.
- **Voice (Phase 2) is NOT built yet.** The overlay's "Live" indicator and mic button are placeholders, and the transcript mirrors the latest AI text message.
- **Next up: Phase 2 — Voice.**

## Implementation phases

### Phase 1 — Core loop (MVP) — ✅ Done

- [x] Wails + React-TS scaffold
- [x] Manual API key input for OpenRouter (setup screen)
- [x] **Screen-driven** — the AI reads the problem from the screen (replaced the original "single hardcoded Two Sum"; there is no problem bank)
- [x] Screen capture on a timer → base64, with display + region selection
- [x] Chat UI: typed message + screenshot → OpenRouter → display response
- [x] Interviewer system prompt tuned for Socratic, screen-reading behavior
- [x] SQLite schema: sessions + messages

### Phase 2 — Voice integration (ElevenLabs) — ⏳ Next up (not started)

- [ ] Manual API key input for ElevenLabs (already collected on the setup screen; unused so far)
- [ ] Frontend mic recording via MediaRecorder (push-to-talk → audio blob)
- [ ] Go: audio blob → ElevenLabs Scribe v2 → transcribed text
- [ ] Go: AI response text → ElevenLabs TTS Flash v2.5 (streaming) → audio
- [ ] Frontend audio playback via Web Audio API (play chunks as they arrive)
- [ ] Voice selection UI (fetch + display available ElevenLabs voices)
- [ ] Visual indicators: recording / AI-speaking / transcribing — wire the overlay's "Live" + mic for real
- [ ] (Pairs naturally) stream the AI text response so TTS can start sooner

### Phase 3 — UX — ◑ Partial

- [x] Settings panel (capture interval, session time limit, key management)
- [x] Display / capture-region selection
- [ ] Model picker (fetch available models from OpenRouter) — see [model-picker-plan.md](model-picker-plan.md)
- [ ] Session history view (bindings `ListSessions` / `GetSessionTranscript` exist; the History tab is a placeholder)
- [ ] Keyboard shortcuts (push-to-talk, end session, toggle capture)
- ~~Problem bank with JSON seed + problem selector~~ — **dropped (screen-driven design)**

### Phase 4 — Auth and polish — ◑ Partial

- [x] Always-on-top floating **overlay** mode (manual "Compact" toggle) + frameless/transparent window
- [ ] OpenRouter OAuth PKCE flow
- [ ] Encrypted token/key persistence (keys are stored in SQLite today, unencrypted)
- [ ] Post-interview debrief mode (AI drops the interviewer persona, gives direct feedback)
- [ ] Session export (markdown transcript with timestamps)
- [ ] (Overlay follow-ups) auto-collapse on window blur; custom min/close controls optional

### Phase 5 — Stretch goals

- [ ] Difficulty adaptation (AI adjusts hint level based on progress)
- [ ] Timer / time pressure mode
- [ ] Multi-problem interview sets (simulate a full interview round)
- [ ] ElevenLabs voice cloning (user uploads an interviewer voice sample)
