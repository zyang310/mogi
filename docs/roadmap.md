# Roadmap & Status

> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md). This file is the implementation roadmap — keep the statuses honest as the project moves.

## Current status

- **Phase 1 (screen-driven, typed core loop) is built**, and the UI has been redesigned onto a Material Design 3 dark theme (floating pill nav, idle "Ready to Begin?" hub, active capture + chat).
- The **always-on-top floating overlay** bar (a Phase 4 item) was built early — entered via the "Compact" button during a session.
- **Voice (Phase 2) is built (non-streaming v1).** Click-to-toggle mic → ElevenLabs Scribe (STT) → the normal interview loop → ElevenLabs Flash (TTS) spoken reply when "voice mode" is on. Voice selection lives in Settings; the overlay's "Live" indicator + mic are wired for real. Streaming TTS/AI-text is deferred.
- **Next up: Phase 3 — UX** (session history view, keyboard shortcuts incl. global push-to-talk).

## Implementation phases

### Phase 1 — Core loop (MVP) — ✅ Done

- [x] Wails + React-TS scaffold
- [x] Manual API key input for OpenRouter (setup screen)
- [x] **Screen-driven** — the AI reads the problem from the screen (replaced the original "single hardcoded Two Sum"; there is no problem bank)
- [x] Screen capture on a timer → base64, with display + region selection
- [x] Chat UI: typed message + screenshot → OpenRouter → display response
- [x] Interviewer system prompt tuned for Socratic, screen-reading behavior
- [x] SQLite schema: sessions + messages

### Phase 2 — Voice integration (ElevenLabs) — ✅ Done (non-streaming v1)

> Detailed, sequenced implementation breakdown: [voice-integration-plan.md](voice-integration-plan.md).

- [x] Manual API key input for ElevenLabs (collected on setup, restored into a `voice.Client`)
- [x] Frontend mic recording via MediaRecorder (click-to-toggle push-to-talk → audio blob, `useVoiceRecorder`)
- [x] Go: audio blob → ElevenLabs Scribe v2 → transcribed text (`internal/voice`, `TranscribeAudio`)
- [x] Go: AI response text → ElevenLabs TTS Flash v2.5 → audio (`SynthesizeSpeech`; non-streaming)
- [x] Frontend audio playback (`useAudioPlayer`; plays the full clip — non-streaming v1)
- [x] Voice selection UI (`ListVoices` + `VoicePicker` in Settings → persists `Preferences.VoiceID`)
- [x] Visual indicators: recording / AI-speaking / transcribing — overlay "Live" + mic wired for real
- [ ] Stream the AI text response so TTS can start sooner — **deferred** (see Out of scope in the plan)
- [ ] Streaming TTS (chunked playback) — **deferred**; non-streaming suffices for short replies

### Phase 3 — UX — ◑ Partial

- [x] Settings panel (capture interval, session time limit, key management)
- [x] Display / capture-region selection
- [x] Model picker — searchable list in Settings (vision-first, free models flagged, per-1M pricing) that persists to `Preferences.Model`; see [model-picker-plan.md](model-picker-plan.md)
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
