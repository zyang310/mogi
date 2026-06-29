# CI/CD & Auto-Update — A Guide

> Read this top-to-bottom to understand how AI Interviewer is built, released, and
> updated. It explains the concepts first, then how this repo wires them together,
> then the trade-offs behind each decision. For the bound-method reference and data
> flow, see [architecture.md](architecture.md); for the original design notes, see
> [ci-cd-and-auto-update-plan.md](ci-cd-and-auto-update-plan.md).

## Why this exists

The app is a [Wails](https://wails.io) desktop binary. Before this system existed, the
only way to get it was to clone the repo and run `wails build` yourself — there was no
version number, no download link, and no way for an existing copy to learn that a newer
one shipped. Three gaps, three goals:

1. **Continuous builds** — every push to `main` should prove the app still compiles and
   passes its checks, and produce a runnable macOS build.
2. **Public downloads** — anyone should be able to grab the app from GitHub.
3. **In-app update awareness** — a running copy should notice when a newer version is
   out and point the user to it.

The constraints that shaped the design: **macOS only**, **no Apple Developer account**
(the app ships *unsigned*), and updates that are **"notify + download"** rather than
silent. The rest of this guide explains what those constraints mean and why they lead
to the design we have.

## Concepts, from first principles

### CI vs CD

**Continuous Integration (CI)** is the habit of automatically building and testing every
change, so a broken commit is caught in minutes instead of at release time. **Continuous
Delivery (CD)** is the next step: automatically packaging a build into something a user
can install. This repo uses both, split across two workflows:

- CI runs on **every push to `main`** ([build.yml](../.github/workflows/build.yml)).
- CD runs when you **push a version tag** ([release.yml](../.github/workflows/release.yml)).

### GitHub Actions in one paragraph

GitHub Actions runs your automation on GitHub's servers. A **workflow** is a YAML file in
`.github/workflows/`. Each workflow has **triggers** (`on: push`, `on: pull_request`,
`on: push: tags`) and one or more **jobs**. A job runs on a fresh virtual machine called a
**runner** (we use `macos-latest`, because building a macOS `.app` needs macOS) and is a
list of **steps** — either a shell command (`run:`) or a reusable **action**
(`uses: actions/checkout@v4`). When a job finishes it can keep files in one of two very
different places:

| Output | Who can download it | Lifetime | Used here for |
|---|---|---|---|
| **Artifact** (`actions/upload-artifact`) | People with **repo access** | Expires (default 90 days) | Test builds from `main` |
| **Release** (`softprops/action-gh-release`) | **Anyone**, even logged-out | Permanent | The public, downloadable app |

That distinction is the whole reason there are two workflows: a CI artifact is great for
"did this commit build?", but only a **Release** satisfies "anyone can download it."

### Versioning: tags are the source of truth

We use [Semantic Versioning](https://semver.org): `vMAJOR.MINOR.PATCH` (e.g. `v0.2.0`).
The version lives in exactly one place — a **git tag** — and flows everywhere else from
there. There is deliberately no version number hard-coded in the source: a release is
"whatever tag you pushed," which means cutting one is a single command and the version can
never drift out of sync with what was actually built.

### macOS distribution: signing, notarization, Gatekeeper

This is the concept that shapes the update experience most, so it's worth understanding.

- **Code signing** stamps an app with a certificate proving who built it. It requires an
  **Apple Developer account** ($99/year).
- **Notarization** is Apple scanning a signed app and stapling an "OK" ticket to it.
- **Gatekeeper** is the macOS gate that, on first launch of a *downloaded* app, checks for
  that signature/notarization. Anything downloaded from the internet also gets a
  **quarantine** attribute (`com.apple.quarantine`) set by the browser.

We ship **unsigned**. So when a user downloads our `.zip`, unzips it, and double-clicks,
Gatekeeper sees no signature + a quarantine flag and **refuses to open it** ("app is
damaged / from an unidentified developer"). The fix is a one-time user action:
right-click → **Open** (which adds a permanent exception), or strip the quarantine flag
with `xattr -cr "/Applications/ai-interviewer.app"`. This is normal for indie/unsigned
macOS apps — but it's the reason updates here can't be silent (more below).

### The auto-update spectrum (and why Wails v2 has none built in)

Auto-update isn't one thing; it's a spectrum from "tell the user" to "replace yourself
while running":

```
  least automatic                                            most automatic
  ───────────────────────────────────────────────────────────────────────►
  Notify + download         Self-replace binary        Framework (Sparkle/WinSparkle)
  (show a banner,           (app downloads the new      (background download, verify
   user installs)            build and swaps itself)     signature, swap, relaunch)
        ▲                                                        ▲
        │ where we are                                           │ needs code signing
```

Frameworks like **Sparkle** (the macOS standard) do the fully-silent version, but they
**verify a cryptographic signature** before applying an update and rely on the app being
signed/notarized so the swapped-in copy isn't quarantined. No signing → no safe silent
swap. Electron and Tauri ship updaters in this family; **Wails v2 ships no auto-updater at
all**, so whatever we do, we build it ourselves.

Given "unsigned," the robust choice is the **left end of the spectrum**: the app checks
GitHub, and if there's something newer it shows a banner and opens the download. The user
does the same one-time Gatekeeper step they did on first install. It's not magic, but it's
reliable and costs nothing — and the design upgrades cleanly to Sparkle later if an Apple
Developer account ever enters the picture.

## How the pipeline works, end-to-end

```
   ┌──────────────────────────────┐         ┌──────────────────────────────┐
   │  git push  →  main            │         │  git push  →  tag vX.Y.Z      │
   └───────────────┬──────────────┘         └───────────────┬──────────────┘
                   ▼                                         ▼
        .github/workflows/build.yml             .github/workflows/release.yml
        runner: macos-latest                    runner: macos-latest
        ├─ npm ci && npm run build → dist        ├─ stamp wails.json productVersion
        ├─ go build ./...                        ├─ wails build darwin/universal
        ├─ go test ./...                         │     -ldflags main.version=vX.Y.Z
        ├─ gofmt -l .                            ├─ ditto  → AI-Interviewer-vX.Y.Z.zip
        ├─ wails build darwin/universal -s       └─ softprops/action-gh-release
        │     -ldflags main.version=dev-<sha>            │
        └─ upload-artifact (repo-only, expires)          ▼
                   │                              ┌─────────────────────────────┐
                   ▼                              │  Public GitHub Release      │
        "did this commit build?"                 │  • anyone can download .zip │
                                                  │  • the updater checks this  │
                                                  └─────────────────────────────┘
```

Both workflows do the same macOS build; they differ in **trigger**, **version**, and
**where the output goes**. `main` pushes produce a throwaway `dev-<sha>` build as a
repo-only artifact (continuous proof it compiles). Tag pushes produce a real `vX.Y.Z`
build published as a public Release.

> **Why the frontend builds first.** `main.go` embeds the compiled UI with
> `//go:embed all:frontend/dist`, so that directory must exist before *any*
> `go build`/`go test` of the `main` package — otherwise the compile fails with
> `pattern all:frontend/dist: no matching files found`. `frontend/dist` is a gitignored
> build output, so CI builds the frontend up front; `wails build -s` then packages
> without rebuilding it. (`release.yml` sidesteps this by running only `wails build`,
> which builds the frontend before the Go compile anyway.)

### The version flow

A single tag fans out to two destinations:

```
   git tag v0.2.0
        │
        ├──►  -ldflags "-X main.version=v0.2.0"  ──►  the Go variable `version` (main.go)
        │                                              │
        │                                              ├──► App.GetAppVersion()  → Settings "About"
        │                                              └──► updater.Check(version) → compare vs GitHub
        │
        └──►  wails.json info.productVersion = "0.2.0" ──►  macOS Info.plist
                                                            (CFBundleShortVersionString → Finder "Get Info")
```

`-ldflags "-X main.version=..."` is a Go linker feature: it sets the value of a package
variable at **build time** without changing source. Our `main.go` declares
`var version = "dev"`; the linker overwrites `"dev"` with the tag. Local builds keep
`"dev"`, which (as we'll see) is exactly what suppresses update nags during development.

## How the in-app updater works

The check lives in the Go backend ([internal/updater/updater.go](../internal/updater/updater.go)),
consistent with the project rule that *all* external HTTP calls happen in Go — the same
pattern as [internal/ai/client.go](../internal/ai/client.go). The frontend only renders
the result.

```
  app launch
     │
     ▼
  useUpdateCheck()  ── React hook, runs once on mount (frontend/src/lib/useUpdateCheck.ts)
     │   calls →  App.CheckForUpdate()         (Wails-bound, app.go)
     │                 │
     │                 ▼
     │          updater.Check(ctx, version)    (internal/updater/updater.go)
     │                 │
     │                 ├─ version is "dev"/invalid semver?  ──► return {available:false}   (no network call)
     │                 │
     │                 ├─ GET api.github.com/repos/zyang310/ai-interviewer/releases/latest
     │                 │      (404 = no releases yet ──► {available:false}, not an error)
     │                 │
     │                 └─ semver.Compare(latestTag, version) > 0  ──► {available:true, …urls}
     │
     ▼
  available?  ──► <UpdateBanner> on the hub  ──►  [Download] ──► App.OpenReleasePage(url)
                  (idle screens only; never                       opens the .zip / release
                   over the overlay or mid-interview)             page in the browser
```

Three details worth calling out, because they're the kind of thing an interviewer probes:

- **It fails silent.** Offline, GitHub down, a dev build, or running in a plain browser
  with no Wails runtime — every failure path leaves the banner hidden. A broken update
  check must never disrupt the actual app. (`useUpdateCheck` swallows errors;
  `CheckForUpdate` returns them but the caller ignores them.)
- **Dev builds never nag.** `version` is `"dev"` locally, which isn't valid semver, so
  `updater.Check` returns "no update" *before* making any network request. That also keeps
  development off GitHub's unauthenticated rate limit.
- **The comparison is real semver, not string compare.** We use
  `golang.org/x/mod/semver` (`IsValid` + `Compare`), so `v0.10.0 > v0.9.0` (a naive string
  compare would get that wrong). The logic is a pure function (`isNewer`) with table tests
  in [updater_test.go](../internal/updater/updater_test.go).

## The unsigned trade-off, concretely

What "unsigned + notify-and-download" actually means for a user:

1. **First install:** download `.zip` → unzip → drag `ai-interviewer.app` to
   `/Applications` → right-click **Open** once (or `xattr -cr`). After that it launches
   normally forever.
2. **An update ships:** on next launch the app shows *"A new version is available."* →
   **Download** opens the new `.zip` → the user repeats step 1's drag + one-time Open.

It is **not** a silent background swap. That ceiling is set by the absence of code
signing, not by the app design. The honest framing: we traded a $99/year account and
notarization complexity for a one-time, well-understood user action per install. For a
personal project that's the right call; for a paid product you'd sign and adopt Sparkle.

## Design decisions & trade-offs

**Tag-driven releases, not "publish every push to `main`."** Publishing a release on
every commit would spam the releases page and, worse, break the updater: semver comparison
needs clean, monotonic version numbers, and GitHub's "latest release" endpoint ignores
pre-releases anyway. So `main` pushes only *verify* (artifact), and a human cuts a real
release by pushing a tag. The cost is one extra command (`git tag … && git push --tags`);
the benefit is a coherent version line and a meaningful "latest."

**Notify-and-download, not silent.** Covered above — forced by "unsigned," and the
robust option regardless.

**The update check lives in Go, not the frontend.** Every other external call in this app
is centralized in the Go backend (`internal/ai`, `internal/voice`, `internal/googletts`),
so the updater follows suit. It reuses the established `http.Client` pattern and keeps the
frontend as pure UI. It would *work* as a `fetch()` in React, but it would break the
codebase's "all network in Go" invariant for no benefit.

**A universal binary, not per-arch downloads.** `wails build -platform darwin/universal`
produces one `.app` that runs natively on both Apple Silicon and Intel (via `lipo`). One
download, no "which one do I need?" — at the cost of a slightly larger file. For a
two-architecture target that's clearly worth it.

**`golang.org/x/mod/semver`, not a hand-rolled compare.** It's a small, official,
well-tested module that correctly handles the `v` prefix and pre-release ordering. The
alternative — ~30 lines of split-and-compare — is exactly the kind of code that looks
trivial and then mishandles `v0.10.0` vs `v0.9.0`. One tiny dependency buys correctness.

**macOS-only, for now.** The app's machine target is macOS and parts of it are macOS-tuned
(the voice path re-encodes audio specifically for WKWebView). Windows would *compile*
(there's NSIS config in `build/windows/`), but the voice path would need work, so the
pipeline doesn't pretend to support it yet. Adding a Windows job later is a localized
change to the workflows.

## Operational runbook

**Cut a release**

```bash
git tag v0.2.0
git push origin v0.2.0      # triggers release.yml → builds + publishes the Release
```

Pick the version with semver: bug-fix → bump PATCH, new feature → bump MINOR, breaking →
bump MAJOR. The tag is the version; nothing else needs editing.

**Check a `main` build compiled** — open the repo's **Actions** tab, find the latest
*Build* run; its artifact (`AI-Interviewer-macos-universal`) is the test build.

**Test the update flow locally** — build the app pretending to be an old version, then
launch it; with a real release published, the banner should appear:

```bash
wails build -ldflags "-X main.version=v0.0.1"
open build/bin/ai-interviewer.app
```

**Where the version shows** — Settings → **About** (calls `GetAppVersion`), and macOS
Finder → *Get Info* (from the plist).

**If CI fails** — the failing step name says which check broke. Reproduce locally with the
same command: `go build ./...`, `go test ./...`, `gofmt -l .`,
`cd frontend && npx tsc --noEmit`, or `wails build`.

## Later: the path to silent updates

If an Apple Developer account is ever added, this design upgrades without a rewrite:

1. Add **signing + notarization** to `release.yml` (Apple certs in GitHub Secrets,
   `codesign` + `notarytool` steps). This alone removes the Gatekeeper step for users.
2. Embed **[Sparkle](https://sparkle-project.org)** in the app and publish a signed
   `appcast.xml` feed (the release assets or GitHub Pages can host it). Swap the
   notify-banner for Sparkle's background updater.

The version/tag/release plumbing in this guide stays exactly as-is — only the install
experience and the "apply" step change.
