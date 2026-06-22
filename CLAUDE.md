# CLAUDE.md — AI Mock Interviewer

Rules and a map of the codebase. Roadmap/status → [docs/roadmap.md](docs/roadmap.md). Deep reference (data flow, full bindings, prompt spec, API contracts) → [docs/architecture.md](docs/architecture.md).

## Purpose (why this exists)

A desktop app that runs a **live AI mock coding interview**. The user codes in their own IDE or a browser tab (VS Code, IntelliJ, terminal, LeetCode, NeetCode); the app screenshots their screen and an AI interviewer reads the problem *and* the code from the screenshot, then nudges Socratically — never handing over the answer.

**Screen-driven:** there is no problem bank and no written problem statement. The screenshot is the problem.

## Stack (what it's built with)

- **Wails v2** — Go backend + web frontend in one native binary, OS webview (no Chromium). Window runs **frameless + transparent** so the overlay can float over the user's IDE.
- **Go backend** — screen capture, all external API calls, SQLite, window/overlay control.
- **Frontend** — React + TypeScript + Vite. **Styling is plain CSS with Material Design 3 tokens (CSS variables) — no Tailwind.**
- **AI** — OpenRouter (vision models) via the Go backend. **Voice (ElevenLabs) is planned, not built** (Phase 2).
- **Deps** — `kbinani/screenshot` + `golang.org/x/image` (capture), `mattn/go-sqlite3` (storage), `google/uuid`.

## Codebase map (where things live)

| Path | What / why |
|---|---|
| `main.go` | Entry point; Wails window options (frameless + transparent). |
| `app.go` | **All** Wails-bound methods. Kept thin — delegates to `internal/`. |
| `internal/ai/` | OpenRouter client (`client.go`) + interviewer system prompt (`prompts.go`). |
| `internal/capture/` | Screen capture + region cropping. |
| `internal/store/` | SQLite: sessions, preferences, API keys. |
| `internal/models/` | Structs that cross the Wails boundary (Session, Message, Preferences, AuthStatus). |
| `frontend/src/App.tsx` | UI shell: floating pill nav → idle hub / active session / overlay. |
| `frontend/src/components/` | One component + its own CSS each (SetupPage, HubReady, CapturePanel, RegionSelector, Chat, MessageBubble, Overlay, Settings). |
| `frontend/src/lib/wailsBridge.ts` | **Single import point** for every bound Go method + models. |
| `frontend/src/style.css` | MD3 design tokens (`:root` CSS variables) + global reset. |
| `frontend/wailsjs/` | Auto-generated bindings — **do not hand-edit**. |
| `docs/` | Roadmap, architecture reference, feature plans. |

## How to work on it

- **Toolchain is Go + npm** (not bun).
- **Run:** `wails dev` (hot reload) or `wails build` (binary). Frontend-only UI work: `cd frontend && npm run dev` — but Wails calls (`window.go.main.App.*`) no-op in a plain browser; stub them to preview.
- **Changed a bound Go method?** Run `wails generate module` (regenerates `frontend/wailsjs`), then export it from `lib/wailsBridge.ts`.
- **Verify changes:**
  - Go: `go build ./...`, `go test ./...`, `gofmt`.
  - Frontend types: `cd frontend && npx tsc --noEmit`.
  - UI behavior: browser preview with Wails calls stubbed. **Native window behavior** (overlay, always-on-top, transparency) can only be confirmed with `wails dev`.

## Rules

- **Comment for humans.** Every exported Go func and every React component/hook gets a short doc comment stating its purpose; comment the *why* for non-obvious logic. Match the existing density in `app.go` and `internal/ai/client.go`.
- **Modularize.** Keep `app.go` thin — logic lives in `internal/` packages, one concern each; a new external integration is a new package (e.g. `internal/voice`). Frontend: one component per file + its own CSS; all Go calls go only through `lib/wailsBridge.ts`.
- **Reusable UI.** Build small, single-responsibility components and compose them. Before adding markup, look for an existing component or class to reuse. Extract repeated UI into shared components/classes instead of re-implementing per screen — buttons (use the shared `.btn*` classes in `App.css`, not per-screen button styles), chips/badges, the pulsing status dot, icon buttons, the modal shell, glass panels. Reuse the MD3 tokens rather than duplicating values; lift shared behavior into hooks.
- **Screen-driven invariant.** Never send a written problem statement — the screenshot carries it. The interviewer persona lives in `internal/ai/prompts.go`.
- **Styling.** Plain CSS + MD3 CSS-variable tokens (`style.css :root`); no Tailwind. Mockups come from Google Stitch (Tailwind) — port them to the tokens. One CSS file per component.
- **Secrets.** API keys live only in the Go backend (SQLite). The frontend never sees them. The AI never speaks unless the user typed/spoke first — no unprompted interruptions.
- **AI calls.** Always set `max_tokens` (replies are short; an unset cap 402s on low OpenRouter balances). See `internal/ai/client.go`.
- **Go.** `gofmt`; return errors, don't panic; wrap with `fmt.Errorf("context: %w", err)`; `json:"..."` tags on boundary structs; `context.Context` for cancellable ops (API calls, capture loops).
- **React.** Functional components + hooks only; handle loading/error state for every async Go call.
- **Commits.** Conventional commits (`feat:`, `fix:`, `chore:`, `docs:`). No hardcoded keys, ever.

## See also

- [docs/roadmap.md](docs/roadmap.md) — phases & current status
- [docs/architecture.md](docs/architecture.md) — data flow, full bindings, prompt spec, OpenRouter/ElevenLabs API contracts
- [docs/model-picker-plan.md](docs/model-picker-plan.md) — model picker design reference (Phase 3, implemented)
