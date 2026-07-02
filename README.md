# AI Interviewer

A desktop app that acts as a live AI-powered mock coding interview coach. You code in your own IDE while the app captures your screen and provides real-time Socratic interviewer feedback.

## Download & Install (macOS)

Grab the latest build from the [**Releases**](https://github.com/zyang310/ai-interviewer/releases) page — download the `.zip`, unzip it, and drag **ai-interviewer.app** into your **Applications** folder. It's a **universal binary** that runs natively on both Apple Silicon and Intel Macs.

> **First launch (one time):** the app is **unsigned**, so macOS Gatekeeper blocks it the first time. Either **right-click the app → Open → Open**, or run:
> ```bash
> xattr -cr "/Applications/ai-interviewer.app"
> ```
> After doing this once, it opens normally — this is expected for unsigned apps.

Once installed, the app **checks for updates on launch** and shows a banner when a newer version is out; click **Download** and repeat the drag-to-Applications step. For how releases and updates actually work, see [docs/ci-cd-and-auto-update.md](docs/ci-cd-and-auto-update.md).

## API keys

Keys are entered in the app's first-run **Setup** screen (or later under **Settings → API Keys**). There's no `.env` file — keys are stored locally in SQLite and only leave your machine as the auth header on requests to that provider.

| Key | Required? | Powers |
|---|---|---|
| **OpenRouter** | **Yes** | The AI interviewer that reads your screen and replies |
| **Google Cloud** | Optional | Low-cost voice (spoken replies + mic transcription) |
| **ElevenLabs** | Optional | Premium voice (higher-quality speech + transcription) |

You only need **OpenRouter** to use the app. Add a voice key (**Google _or_ ElevenLabs**) only if you want to talk with the interviewer out loud — with both, the app automatically uses the best combo (ElevenLabs transcription + Google speech).

### OpenRouter (required)

One gateway to many models (Claude, GPT, Gemini, …); the app uses it for the vision model that reads your screen.

1. Sign in at **https://openrouter.ai** (Google/GitHub login works).
2. Add a few dollars of credit under **Settings → Credits** — interviewer replies are short, so it lasts a long time.
3. Go to **https://openrouter.ai/keys** → **Create Key** → name it (e.g. "AI Interviewer") → **Create**.
4. Copy the key (starts with `sk-or-...`) and paste it into the app.

> The key is shown only once. Lose it? Just create another.

### Google Cloud (optional — low-cost voice)

This is the fiddly one, so here's the full walkthrough. It's ~10× cheaper than ElevenLabs with a free monthly tier. Three parts: **make a project → enable two APIs → create a key.**

**1 · Create a project**

- Open **https://console.cloud.google.com** and accept the terms if it's your first time.
- Top bar → **project dropdown → New Project** → name it (e.g. "ai-interviewer") → **Create**. After a few seconds, select that project in the dropdown.
- **Billing:** Google requires a billing account (a card) even for the free tier under **Billing**. You won't be charged within the free monthly limits, but it must be set up. Prefer not to add a card? Use ElevenLabs instead.

**2 · Enable the two voice APIs** _(this is the step people miss)_

- **Text-to-Speech** (lets the interviewer speak): open **https://console.cloud.google.com/apis/library/texttospeech.googleapis.com** → **Enable**.
- **Speech-to-Text** (lets you reply with the mic): open **https://console.cloud.google.com/apis/library/speech.googleapis.com** → **Enable**.
- Both should read "API Enabled" for your project. _(If you'll only listen and type your answers, Text-to-Speech alone is enough — but then skip enabling Speech-to-Text and just don't use the mic.)_

**3 · Create the API key**

- Go to **https://console.cloud.google.com/apis/credentials**.
- **+ Create Credentials → API key** → copy the key (starts with `AIza...`) → paste it into the app.
- _(Recommended)_ Click **Edit API key → Restrict key → API restrictions** and limit it to **Cloud Text-to-Speech API** and **Cloud Speech-to-Text API**, so a leaked key can't reach anything else.

> **"API key not valid" or a 403?** Almost always means the API isn't enabled on the **selected** project, or the key is restricted to the wrong APIs. Re-check step 2, confirm the key belongs to the **same project** where you enabled the APIs, and re-check the restriction in step 3.

### ElevenLabs (optional — premium voice)

Higher-quality voices and transcription (Scribe). Pricier than Google, but has a free tier to try.

1. Sign up at **https://elevenlabs.io**.
2. Open **https://elevenlabs.io/app/settings/api-keys** (or **Profile → API Keys**).
3. **Create API Key** → name it → copy it → paste into the app.

> The free tier is enough to test spoken interviews; heavier use needs a paid plan.

---

The sections below are for **building from source** instead.

## Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.23+ | https://go.dev/dl |
| Node.js | 18+ | https://nodejs.org |
| Wails CLI | v2 | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| Xcode Command Line Tools | any | `xcode-select --install` (not the full Xcode IDE) |

> **Why Xcode Command Line Tools?** Three dependencies use CGO (C bindings) on macOS: Wails itself binds to `WKWebView` (the OS webview), `go-sqlite3` embeds a C build of SQLite, and `kbinani/screenshot` calls CoreGraphics for screen capture. The CLT provides `clang` and the macOS SDK headers — the full Xcode IDE is not needed.

Verify everything is in place:

```bash
wails doctor
```

## Setup

```bash
# Install frontend dependencies
cd frontend && npm install && cd ..

# Download Go dependencies
go mod tidy
```

## Running in Development

```bash
wails dev
```

This starts hot-reload for both the Go backend and the React frontend. The app window opens automatically. Frontend changes reflect instantly; Go changes trigger a rebuild.

To work on the frontend UI only (no Go backend, faster iteration):

```bash
cd frontend && npm run dev
```

## Building for Production

```bash
wails build
```

Output: `build/bin/ai-interviewer.app` (macOS). Double-click to run, or:

```bash
open build/bin/ai-interviewer.app
```

## Project Structure

```
ai-interviewer/
├── main.go              # Entry point — Wails app config
├── app.go               # Go methods exposed to the frontend
├── internal/            # Backend packages (added in later phases)
├── frontend/
│   ├── src/             # React + TypeScript UI
│   ├── wailsjs/         # Auto-generated Wails bindings (do not edit)
│   └── package.json
├── build/               # App icons and platform metadata
└── wails.json           # Wails project config
```

## How the Wails Bridge Works

Go methods on the `App` struct in `app.go` are automatically available in the frontend as async TypeScript functions under `window.go.main.App.*`. The generated bindings live in `frontend/wailsjs/go/main/App.d.ts` — re-run `wails dev` or `wails generate module` to regenerate them after changing Go method signatures.

## Verifying the Build (No GUI)

```bash
# Compile Go only
go build ./...

# Compile frontend only
cd frontend && npm run build
```

Both should exit cleanly with no errors.

## Configuration

API keys are entered in the app's **Settings → API Keys** panel (or the first-run setup screen) — no `.env` file. See [**API keys**](#api-keys) above for step-by-step instructions on getting each one. All keys are stored locally in SQLite and never leave your machine except as the auth header on requests to that provider.

## Credits

Company Practice mode's question pools are generated from [snehasishroy/leetcode-companywise-interview-questions](https://github.com/snehasishroy/leetcode-companywise-interview-questions) (scraped from LeetCode's premium company-frequency filter). We ship **factual metadata only** — problem titles, difficulties, frequencies, and links — never problem statements. See [`internal/problems/data/SOURCE.md`](internal/problems/data/SOURCE.md) for the snapshot date and how to regenerate.
