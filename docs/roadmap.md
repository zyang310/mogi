# Roadmap & Status

> Rules and the codebase map live in [CLAUDE.md](../CLAUDE.md). This file is the implementation roadmap — keep the statuses honest as the project moves.

## Current status

- **Phase 1 (screen-driven, typed core loop) is built**, and the UI has been redesigned onto a Material Design 3 dark theme (floating pill nav, idle "Ready to Begin?" hub, active capture + chat).
- The **always-on-top floating overlay** bar (a Phase 4 item) was built early — entered via the "Compact" button during a session.
- **Voice (Phase 2) is built (non-streaming v1).** Click-to-toggle mic → ElevenLabs Scribe (STT) → the normal interview loop → ElevenLabs Flash (TTS) spoken reply when "voice mode" is on. Voice selection lives in Settings; the overlay's "Live" indicator + mic are wired for real. Streaming TTS/AI-text is deferred.
- **Phase 3 — UX** is in progress: **a global voice hotkey is built** (configurable, default `Right ⌥ Option`; press to start, press again to stop) and the **session history view is built** (expandable past-session list with full transcripts + delete, plus AI-derived problem title + difficulty).
- **Distribution + auto-update (macOS) is built:** GitHub Actions builds on every push to `main` and publishes a public, downloadable Release on each `vX.Y.Z` tag; the app checks GitHub on launch and shows an "update available" banner. Unsigned (notify-and-download, not silent). See [ci-cd-and-auto-update.md](ci-cd-and-auto-update.md).
- **Company Practice mode (Phase 6) is built:** a separate "Companies" tab where you pick a real interview-frequency LeetCode problem from ~654 companies, or hit **Mock Interview** for a two-problem draw (easier, then harder). The AI greets you in character and assigns the problem **by reference** (title + difficulty + link, never the statement), then the normal screen-driven loop runs, flavored by the company's style. The default Hub flow is unchanged. See [company-practice-plan.md](company-practice-plan.md).

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
- [x] Session history view — History tab lists past sessions (problem title, difficulty, date, duration, model), expands to the full transcript, and supports delete. Problem **title + difficulty are AI-derived** at session end (best-effort, async) since the app is screen-driven; sessions from before this feature show a generic label. Bindings: `ListSessions` / `GetSessionTranscript` / `DeleteSession`. See [history-feature-plan.md](history-feature-plan.md).
- ◑ Keyboard shortcuts — **global voice hotkey built**: press a configurable hotkey (default `Right ⌥ Option`) to start recording, press again to stop & send — works while the IDE is focused. Backend OS hook (`internal/hotkey` via `robotn/gohook`) → `ptt:down`/`ptt:up` Wails events (frontend toggles on down); config + macOS Input Monitoring hint in Settings → Voice Hotkey. End-session / toggle-capture shortcuts still TODO.
- ~~Problem bank with JSON seed + problem selector~~ — **dropped (screen-driven design)**

### Phase 4 — Auth and polish — ◑ Partial

- [x] Always-on-top floating **overlay** mode (manual "Compact" toggle) + frameless/transparent window
- [ ] OpenRouter OAuth PKCE flow
- [ ] Encrypted token/key persistence (keys are stored in SQLite today, unencrypted)
- [x] Post-interview debrief mode (AI drops the interviewer persona, gives direct feedback) — the History card has a **Transcript / Debrief tab toggle**; the Debrief tab opens a **structured scorecard** (5-point hire verdict, a **five**-dimension 1-5 rubric incl. **pace**, strengths/improvements) shown as metric bars + a radar chart. Generated **once and cached** in `sessions.debrief` (re-opening costs no tokens), using the session's own model. To judge the real solution (not just the chat), the end-of-session labeling call was **upgraded to a vision call** that also transcribes the final on-screen code into `sessions.final_code`, which feeds the debrief. Binding: `GetDebrief`. See [history-feature-plan.md](history-feature-plan.md).
- [ ] Session export (markdown transcript with timestamps)
- [ ] (Overlay follow-ups) auto-collapse on window blur; custom min/close controls optional

### Distribution & auto-update (macOS) — ✅ Done

> Concepts + design trade-offs: [ci-cd-and-auto-update.md](ci-cd-and-auto-update.md).

- [x] CI on every push to `main` ([build.yml](../.github/workflows/build.yml)): `go build`/`test`/`gofmt` + `tsc`, then a universal `wails build` uploaded as a run artifact.
- [x] Tag-driven public releases ([release.yml](../.github/workflows/release.yml)): pushing `vX.Y.Z` builds the universal `.app`, zips it, and publishes a GitHub Release — the public download *and* the updater's source of truth.
- [x] In-app update check (`internal/updater` → GitHub releases API, semver compare) surfaced as a hub banner + Settings → About. **Unsigned** — installs are manual (notify-and-download) with a one-time Gatekeeper step.
- [ ] Code signing + notarization (Apple Developer account) → silent updates via Sparkle — **deferred**.

### Phase 5 — Stretch goals

- [ ] Difficulty adaptation (AI adjusts hint level based on progress)
- [ ] Timer / time pressure mode
- [x] Multi-problem interview sets (simulate a full interview round) — **built as Company Practice's Mock Interview** (two-problem draw); see Phase 6

### Phase 6 — Company Practice mode (opt-in, additive) — ✅ Done

> Detailed, phased plan: [company-practice-plan.md](company-practice-plan.md).

Practice for a specific company two ways: **browse & pick** a real interview-frequency LeetCode
problem, or hit **Mock Interview** for a two-problem draw (easier first, harder second). The AI
greets you in character and assigns the problem (by reference — title + difficulty + link, never the
problem text, so the screen-driven invariant holds), asks you to open it, then runs the normal
screen-driven interview flavored by that company's style. **The default Hub flow is unchanged.**

- [x] Phase 0 — data pipeline: `internal/problems/gen` downloads
      [snehasishroy/leetcode-companywise-interview-questions](https://github.com/snehasishroy/leetcode-companywise-interview-questions),
      trims it to a committed, `go:embed`-ed CSV (654 companies, 17,641 problems — factual metadata
      only), plus authored `companyProfiles`
- [x] Phase 1 — `internal/problems` package (`models.Problem`/`CompanyInfo`, lazy embed parse,
      `Companies`/`Problems`/`MockPair` frequency-weighted draw) + unit tests
- [x] Phase 2 — prompts: `BuildCompanySystemPrompt` (single + mock, encodes the AI-greeted-first
      framing) and templated openers + tests
- [x] Phase 3 — bindings: `ListCompanies` / `ListCompanyProblems` / `StartCompanySession` /
      `StartMockInterview` / `OpenURL`, returning `CompanySessionStart`
- [x] Phase 4 — "Companies" pill-nav tab: searchable company list → browse & pick (difficulty
      chips, frequency/title/difficulty sort, "Open on LeetCode"), Mock Interview CTA + confirm
      modal (no titles), session banner with a face-down Q2 reveal card; reuses the active-session UI
- [x] Phase 5 — polish: persist last company + difficulty filter, session-row company/mode
      persistence with a History badge, tests + docs
- [x] Data refresh automation (2026-07): pipeline switched to
      [liquidslr/leetcode-company-wise-problems](https://github.com/liquidslr/leetcode-company-wise-problems)
      (ids/acceptance joined from LeetCode's public algorithms API; upstream display names carried
      as a `name` column); a biweekly scheduled workflow (`refresh-problems.yml`) regenerates the
      CSV and opens a PR
- [ ] Stretch (deferred): mock repeat-avoidance, elapsed-time pacing context, embed +
      background-refresh, AI-generated opener
