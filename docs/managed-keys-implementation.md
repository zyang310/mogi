# Managed Test Accounts — Implementation Plan & Status

> The **step-by-step, phase-by-phase** build plan for the managed test-account
> tier. Design rationale (the *why*) lives in
> [managed-keys-plan.md](managed-keys-plan.md); this is the *how*, with a
> **Checkpoint** after each step that re-examines it for simplicity, efficiency,
> and consistency with earlier steps.
>
> **Status legend:** ✅ done · ◑ in progress · ☐ not started
>
> **Current status (2026-07-14):** **Phases 0–1 ✅ complete** — the access
> service is built and verified end-to-end locally (Phase 0); the app backend
> (store → access client → account service → mode-aware AuthStatus → bindings +
> launch refresh) is implemented and unit-tested against fakes (Phase 1). Go
> build/test/vet/gofmt, the access-service suite, and `npx tsc --noEmit` are all
> green. **Not yet done:** Phase 1's *interactive* `wails dev` backend e2e (the
> 1.10 phase gate — needs a running local access service + native window) and
> **Phases 2–3**.

## Context

Testers redeem an **invite code + email OTP** and the app auto-inserts
developer-funded API keys (per-tester $3-capped OpenRouter key, shared
TTS/STT-restricted Google key, shared STT-scoped ElevenLabs key) fetched from a
small **access service**; **BYOK stays a first-class equal mode** via a
`KeyMode` preference over two key namespaces. Pinned model server-side, voice
prefs open but catalog-filtered, launch-time key refresh for rotation/enforcement,
~$20/mo budget. Dev-mode mailer first (log OTPs); the service lives in this repo
as its own Go module at `access-service/`.

Simplifying decisions (validated in design review):
1. **Extend `models.AuthStatus`** — the three existing `*Configured` bools become
   mode-aware, so existing gates work in managed mode unchanged. New fields:
   `keyMode`, `managedActive`, `managedEmail`, `pinnedModel`. Token never crosses
   the boundary.
2. **KeyMode is a Preferences field** switched via existing `UpdatePreferences`;
   `Settings.Update` re-resolves providers only when KeyMode actually changed.
3. **Only 3 new bindings:** `RequestTestCode`, `ActivateTestAccount`,
   `SignOutTestAccount`.
4. Sign-out is **device-local** in v1 (tester-doc revocation is the server lever).

## Wire contract (as built in Phase 0; Phase 1's client mirrors it)

| Endpoint | Request | Success | Failure |
|---|---|---|---|
| `POST /activate` | `{"email","inviteCode"}` | `204` | `400` invalid/exhausted invite (**no mail sent**); `403` phase off; `429` rate-limited |
| `POST /verify` | `{"email","code"}` | `200 {"token","keys":{openrouter,google,elevenlabs,pinnedModel}}` | `400` bad/expired code or ≥5 attempts |
| `GET /keys` | `Authorization: Bearer <token>` | `200 {openrouter,google,elevenlabs,pinnedModel}` | `401` bad token; `403` revoked / phase off |
| `GET /healthz` | — | `200 ok` | — |

Invite validation runs **before** any mail (spam-relay defense); the invite use
is committed once, on successful `/verify`.

---

## Phase 0 — Access service, standalone and local-first — ✅ Done

Runs with `STORE=memory MAILER=log` (OTPs to the log, stub key minter) — zero
GCP/Resend/OpenRouter setup. Files under `access-service/` (`module mogi-access`).

- ✅ **0.1 Verify OpenRouter provisioning.** Resolved from OpenRouter's public
  docs (a live curl needs the owner's provisioning key). **Findings:** the raw
  key is returned **only at mint** (`POST /api/v1/keys/`; later reads mask it as
  `label`+`hash`); there is **no model-restriction field**; delete is
  `DELETE /api/v1/keys/{hash}`. → `Tester` stores `ORKey`+`ORKeyHash`; `Mint`
  takes no model param. **Residual (non-blocking):** confirm `limit` is USD on
  the first real mint (OpenRouter credits are 1:1 USD).
  > **Checkpoint ✓** — decisions locked before code; the store/minter shapes
  > below already reflect them.
- ✅ **0.2 Module scaffold.** `access-service/go.mod` (`module mogi-access`,
  `go 1.25.0`, zero external deps) + `main.go` (env `config`, `loadConfig`,
  graceful-shutdown `run`, `buildHandler`, `getenv*` helpers).
  > **Checkpoint ✓** — root `go build ./... && go test ./...` unchanged/green
  > (nested module doesn't descend into or pollute `mogi` — premise proven);
  > service builds; gofmt clean. All 13 env vars map to a real consumer.
- ✅ **0.3 Store interface + memory impl.** `internal/store/store.go`
  (`Invite`/`OTP`/`Tester`/`Session`/`Config`, `ErrNotFound`, **added
  `ErrInviteUnavailable`** for `ConsumeInvite`'s inactive/exhausted case, `Store`
  interface) + `memory.go` (mutex-guarded maps, `NewMemory` seeds a 100-use
  invite + config).
  > **Checkpoint ✓** — walked all 3 handlers against the interface: every method
  > has a caller, nothing missing. `ConsumeInvite` = single-doc read-modify-write
  > → maps cleanly to a Firestore transaction later.
- ✅ **0.4 Mailer.** `internal/mailer/`: `Mailer` interface, `LogMailer` (dev),
  `Resend` (prod, updater-style request idiom — **built now, exercised in 3.3**).
- ✅ **0.5 Provisioning client + stub.** `internal/openrouter/`: `Client`
  (`Mint`/`Delete`, `ai/client.go` idiom), `KeyMinter` interface, `StubMinter`
  (`sk-or-fake-…`) for keyless local runs.
  > **Checkpoint ✓ (0.4+0.5)** — both HTTP clients share the
  > `NewRequestWithContext` + wrapped-error idiom; both `Client` and `StubMinter`
  > satisfy `KeyMinter` (server stays oblivious). `Delete` kept as the documented
  > rotation-ops hook (implements the 3.6 revoke-by-hash requirement, justifies
  > storing `ORKeyHash`); revisit if no admin surface materializes.
- ✅ **0.6 HTTP server.** `internal/server/server.go` (`New`, `handleActivate`
  with the load-bearing **invite→rate-limit→mail** order, `handleVerify`,
  `handleKeys`, `GET /healthz`; helpers `normalizeEmail`/`hashString`/`genOTP`
  (unbiased crypto/rand)/`genToken`/`bearerToken`/`clientIP`; one `keysPayload`
  reused flat by `/keys` and nested by `/verify`) + `ratelimit.go` (sliding
  window). **Refinement discovered here:** the `/verify` request carries only
  `{email, code}`, so `ConsumeInvite` needs the invite code from elsewhere →
  **added `InviteCode` to the `OTP` struct** (the OTP *is* "a pending activation
  for this email via this invite").
  > **Checkpoint ✓** — all 10 store methods called, none unused; identical keys
  > JSON in both endpoints; `TestPhaseActive` enforced in both `/activate` and
  > `/keys`. `main.go` wiring updated (placeholder health check folded into the
  > server).
- ✅ **0.7 Handler tests.** `server_test.go` (white-box `package server`) over
  `NewMemory` + recording fake mailer + fake minter: bad-invite-no-mail, OTP
  round trip (+ email normalization), expiry, attempts-exhausted, invite-consumed-
  once (+ key reused on re-verify), rate-limit, keys valid/invalid token, revoked
  →403, phase-off→403 (activate **and** keys).
  > **Checkpoint ✓** — `go test ./... && go vet ./...` green; every wire-contract
  > row asserts **status and body shape** (these tests are the contract the
  > Phase 1 client is written against).
- ✅ **0.8 Local smoke + README.** Live run confirmed: activate→`204` (OTP
  logged) · bad invite→`400` (no mail for stranger) · verify→`200` (token +
  stub key + pinned model) · keys→`200` (identical payload) · bad token→`401`.
  `access-service/README.md` written (run command, endpoint table, env table,
  curl flow, deploy command, ops-notes stub).
  > **Phase gate ✓** — root suite unchanged/green; service build+test+vet+gofmt
  > green; no premature one-file packages (`ratelimit.go` stayed in `server/`).
  > **As-built notes:** `main.go` is 201 lines (over the ~100 estimate) but is
  > all config/wiring/plumbing, no logic. `mailer/resend.go`, `openrouter.Delete`,
  > and the Firestore seam are deliberately built-but-unexercised Phase 3 hooks.

**Built layout:**
```
access-service/
  go.mod  main.go  README.md
  internal/store/     store.go  memory.go
  internal/mailer/    mailer.go  log.go  resend.go
  internal/openrouter/ provisioning.go  stub.go
  internal/server/    server.go  ratelimit.go  server_test.go
```

---

## Phase 1 — App backend (store → client → service → bindings) — ✅ Done

Dependency-ordered; no step references a later artifact. Built exactly as
planned except for the small as-built refinements noted per step.

- ✅ **1.1 Managed store rows.** New `internal/store/managed.go`: consts
  (`managed_openrouter_api_key`, …, `managed_session_token`, `managed_email`,
  `managed_pinned_model`), `managedProviderKey()` mirroring `providerKey()`,
  methods on the existing `getPref/setPref/deletePref` primitives (**no schema
  change**): `GetManagedKey/SetManagedKey`, `GetManagedSession/SetManagedSession`,
  `GetManagedPinnedModel/SetManagedPinnedModel`, `DeleteManagedData()`. Extended
  `ClearAll`'s doc comment (wipe includes managed rows → full reset signs out).
  > **Checkpoint ✓** — build+gofmt clean; error-wrapping matches
  > `preferences.go`; `DeleteManagedData` loops `deletePref` (no raw SQL).
  > **As-built:** also added `GetManagedEmail/SetManagedEmail` — the plan
  > declared the `managed_email` const but omitted its accessor; it's read by
  > `AuthStatus` (1.3) and written by `Activate` (1.5), so a getter/setter pair
  > mirroring the session/pinned-model ones was required.
- ✅ **1.2 `Preferences.KeyMode`.** `models/session.go`: `KeyMode string
  \`json:"keyMode"\`` (`"byok"` default | `"managed"`); `preferences.go`: const
  `keyKeyMode`, default in `GetPreferences`, overlay read (guarded `v != ""`, so
  a stray empty write still reads back `"byok"`), one `setPref` in
  `SavePreferences`.
- ✅ **1.3 Mode-aware AuthStatus.** models: added `keyMode/managedActive/
  managedEmail/pinnedModel`. `settings.go`: `SettingsStore` gained the managed
  getters; `AuthStatus()` picks `GetAPIKey` vs `GetManagedKey` by mode and fills
  the new fields. Extended `fakeStore`; added `TestAuthStatusManagedMode`.
  > **Checkpoint (1.2+1.3) ✓** — build+test+gofmt green; `ListModels` guards on
  > the registry (unaffected); `KeyMode` defaulted in exactly one place.
  > **As-built:** `managedActive/managedEmail/pinnedModel` are reported
  > **regardless of mode** (they read the managed rows, not the active
  > namespace) — deliberately, so a user who flipped to BYOK without signing out
  > still shows as managed-active for the Phase 2 "switch back" affordance.
- ✅ **1.4 `internal/access` client.** `client.go`: `const DefaultURL`
  (placeholder → real in 3.5); `Client{baseURL, httpClient(15s)}`, `NewClient`
  (trims trailing `/`); `KeySet{OpenRouter,Google,ElevenLabs,PinnedModel}`;
  `RequestCode`/`Verify`/`Keys` over one private `do` helper; non-2xx decodes
  `{"error"}`; 401/403 wrap sentinel `ErrUnauthorized` preserving the server
  message. Added `client_test.go` (httptest) asserting the three bodies incl.
  `/verify`'s `"keys"` nesting and the 401/403→`ErrUnauthorized` wrap.
  > **Checkpoint ✓** — field-by-field diff of bodies vs the Phase 0 server; one
  > sentinel suffices. **As-built:** the sentinel is wrapped as
  > `fmt.Errorf("%w: %s", ErrUnauthorized, msg)`; the account service recovers
  > the server message for the sign-out notice by trimming that prefix
  > (`signOutNotice`).
- ✅ **1.5 Account service.** New `internal/service/account.go`: `AccountStore`,
  `AccessClient` interface (`*access.Client` satisfies), `Account{store,
  providers, client, status func() models.AuthStatus}` (status borrowed from
  `Settings.AuthStatus`, like `NewHistory` borrows `interview.ActiveID`).
  `RequestCode`; `Activate` (Verify→store keys+pin+session+email→KeyMode="managed"→
  `ApplyMode`→status); `SignOut` (local: `DeleteManagedData`, KeyMode="byok",
  re-resolve); `Refresh(ctx) (changed, notice, err)` — no-token no-op / 200 upsert
  + re-resolve / `ErrUnauthorized` purge+notice / other-error keep cached keys.
  Package-level `applyKeyMode(st, providers, prefs)` (over a tiny `keyResolver`
  interface) = the single resolution rule. Tests via `accountWith(...)` +
  `fakeAccess` over a stateful store (`statefulStore`).
  > **Checkpoint ✓** — no status logic duplicated; service→access coupling
  > matches the `AI`→`ai.ChatMessage` precedent; `ApplyMode` = `applyKeyMode` +
  > prefs-read + logging, and every mutating path (Activate/SignOut/Refresh) ends
  > by calling it. **As-built:** `Activate`/`SignOut` **return `models.AuthStatus`**
  > (not `void`) — the borrowed `status` func is right there, so the bindings hand
  > the fresh status straight back (Phase 2 can use it directly instead of a
  > follow-up `GetAuthStatus`). `Refresh`'s `changed` is precise: `true` on the
  > sign-out path, and on a 200 only when the **pinned model actually moved** (the
  > sole refresh-visible field), so launches don't emit a no-op event.
- ✅ **1.6 Settings chokepoints.** `settings.go`: (a) `Update` compares old vs new
  prefs; **only on KeyMode change** calls `applyKeyMode`. (b) `SetAPIKey`/
  `DeleteAPIKey`: always write the store, touch `providers.SetKey` only when
  `!managedMode()`. (c) `ClearAllData` comment extended (managed sign-out).
  Tests: `TestUpdateKeyModeFlipReresolves` (incl. a non-flip Update leaving the
  registry untouched), `TestSetAPIKeyInManagedModeLeavesRegistry`,
  `TestClearAllSignsOutManaged`.
  > **Checkpoint ✓** — every write path into keys/KeyMode (`SetAPIKey`,
  > `DeleteAPIKey`, `Update`, `ClearAllData`, `Account.Activate/SignOut/Refresh`,
  > `ApplyMode`) ends with registry ≡ store+mode; that invariant is
  > `applyKeyMode`'s doc comment; a non-flip `Update` touches providers zero
  > times (asserted).
- ✅ **1.7 Model pinning.** `interview.go`: `InterviewStore` gained
  `GetManagedPinnedModel`; one helper `resolveModel(requested, prefs)` (managed→
  pinned; else requested; else `prefs.Model`) at the three chokepoints (`Start`,
  `startCompanyInterview`, `extractSessionMeta` fallback). Debrief unchanged
  (frozen session model). `TestResolveModelPinning` (table): pinned wins managed
  (over a request), empty pin degrades to request, explicit wins byok, saved
  default otherwise.
  > **Checkpoint ✓** — every session-creating `prefs.Model` read flows through
  > `resolveModel`; no inline `if prefs.KeyMode` at call sites.
- ✅ **1.8 Voice guards.** `voice.go`: `managedGoogleVoiceAllowed(id)` (contains
  "neural2"/"wavenet"); `activeTTS` — managed forces Google (STT-scoped EL key
  would 4xx) and falls back to `defaultGoogleVoiceID` when the saved voice fails
  the allowlist; `Voices()` — managed filters the catalog
  (`filterManagedGoogleVoices`). `activeSTT` untouched. Tests: managed forces
  Google despite `TTSProvider=elevenlabs`, errors (no EL fallback) with no Google
  key, premium saved voice falls back, catalog filter drops Chirp/Studio.
  > **Checkpoint ✓** — `activeTTS` reads mode from its existing `GetPreferences`
  > call; default `en-US-Neural2-F` passes; backend guard is the source of truth
  > (Phase 2 tile-hiding is mirror-only). **As-built:** `Voices()` does one extra
  > (cheap, local) `GetPreferences` read for the filter branch — `activeTTS`
  > doesn't surface the mode, and `GetPreferences` is already called liberally
  > across the service layer.
- ✅ **1.9 Wiring, bindings, launch refresh.** `app.go`: `App.account`; in
  `NewApp`, `MOGI_ACCESS_URL` env (default `access.DefaultURL`) →
  `service.NewAccount(...)`; **replaced the 3-provider load loop with
  `app.account.ApplyMode()`** (loop deleted). `startup`: `go
  a.refreshManagedAccount()` (20s ctx, log, emit on change). Three 1–3-line
  bindings — `RequestTestCode`/`ActivateTestAccount`/`SignOutTestAccount` (the
  latter two return `models.AuthStatus`). `window.go`: `emitManagedChanged(notice)`
  → `EventsEmit(a.ctx, "managed:changed", notice)`.
  > **Checkpoint ✓** — `go build/vet/gofmt` green; `grep -l "pkg/runtime" *.go`
  > → only `window.go`; old loop confirmed gone. **As-built:** with no managed
  > state, `ApplyMode` resolves the BYOK namespace — byte-identical to the old
  > loop's effect (the loop's per-key read-error warning is the only dropped
  > behaviour; a bad read now just leaves that slot empty, same as before).
- ✅ **1.10 Regenerate + bridge.** Ran `wails generate module` (regenerated
  `frontend/wailsjs`, incl. the 3 bindings + the new `AuthStatus`/`Preferences`
  fields); added the 3 methods to `wailsBridge.ts`.
  > **Phase gate — automated ✓:** root `go build/test/vet` + `gofmt -l` clean,
  > access-service suite green, `npx tsc --noEmit` clean. **Interactive e2e ☐
  > (deferred):** the local-service + `MOGI_ACCESS_URL=… wails dev` devtools drive
  > (`RequestTestCode`/`ActivateTestAccount` → `GetAuthStatus()` shows managed)
  > needs a running access service + native window and has **not** been run yet.
  > It can run standalone before Phase 2, or fold into the 2.6 full local e2e.

## Phase 2 — Frontend — ☐ Not started

- ☐ **2.1 App shell plumbing.** `App.tsx`: `handleAuthChange` (setAuthStatus +
  `loadPrefs`) passed to SetupPage/Settings (stale-prefs guard); mount-scoped
  `EventsOn("managed:changed", …)` → refetch AuthStatus + prefs, surface `notice`.
  > **Checkpoint:** tsc; `handleAuthChange` is the only status setter passed
  > down; listener matches the `ptt:down` subscribe/cleanup idiom.
  > **As-built (1.9) note:** `ActivateTestAccount`/`SignOutTestAccount` **return
  > the fresh `AuthStatus`**, so let `handleAuthChange(status?)` take an optional
  > status — the button handlers pass the returned value (only `loadPrefs()` then
  > needs a round trip), while the `managed:changed` listener (no status in hand)
  > calls `GetAuthStatus()` itself. Keeps one setter, avoids a redundant fetch.
- ☐ **2.2 InviteActivation component.** New `components/setup/InviteActivation.tsx`
  + CSS: phase machine `request | verify | done`, email/invite/OTP fields,
  privacy-notice checkbox gating the request, `RequestTestCode`/
  `ActivateTestAccount`, `onActivated`/`onBack`. Reuse `.btn*`.
  > **Checkpoint:** tsc + browser preview with both bindings stubbed;
  > host-agnostic (Settings re-hosts it in 2.4).
- ☐ **2.3 SetupPage two-door fork.** Door state `choose | invite | byok`; managed
  signed-in pre-pass card; invite door hosts `InviteActivation`; byok door =
  existing form untouched + back link; gate "Already configured" hints on
  `keyMode !== "managed"`.
  > **Checkpoint:** `wails dev` fresh DB → doors → invite → OTP from log → card →
  > Continue → Hub; relaunch → pre-pass card; BYOK door byte-identical.
- ☐ **2.4 Settings account card + fork + invite entry.** New
  `components/settings/ManagedAccountCard.tsx` + CSS (email, badge, pinned-model,
  **Switch to my own keys** via `savePrefs({keyMode:"byok"})`, **Sign out** via
  `SignOutTestAccount`). `Settings.tsx`: `api-keys` pane renders the card when
  managed, else `ApiKeysSection`. `ApiKeysSection` (byok view): "Switch back"
  banner when `managedActive`; "Have an invite?" footer hosting inline
  `<InviteActivation>`.
  > **Checkpoint:** activate→card; switch to byok→key cards + banner, managed
  > rows intact (relaunch + switch back → still signed in); sign out→BYOK keys
  > untouched. Mode switch flows only through `savePrefs`→`UpdatePreferences`→
  > `Settings.Update`.
- ☐ **2.5 Pinned model + voice in Settings.** `ModelsSection`: `authStatus`
  prop; managed → static locked card (lock icon + `pinnedModel`) instead of
  `ModelPicker`. `VoiceSection`: managed → google-only tiles + `resolveProvider()
  → "google"` + BYOK-only note. `VoicePicker` unchanged (backend filters).
  > **Checkpoint:** managed → lock card + no Chirp/Studio voices; byok → full
  > picker + both tiles. Both branch on `authStatus`, not prefs.
- ☐ **2.6 Full local e2e (pre-GCP).** memory/log service + `MOGI_ACCESS_URL=…
  wails dev`: redeem → keys land, model pinned, Scribe STT + forced-Google TTS;
  kill switch (`TEST_PHASE_ACTIVE=false` restart → graceful sign-out); BYOK
  regression; sign-out leaves BYOK keys; offline grace (stop service → cached
  keys work); Clear All Data → signed out.
  > **Phase gate:** full battery. Whole-feature recount: 3 bindings, 2 new
  > components; only the deliberate Phase 3 hooks remain unexercised.

## Phase 3 — Deploy, ops, docs — ☐ Not started

- ☐ **3.1 Provider verification + shared keys** (verify-items 2+3): EL key
  STT-only + cap (**confirm the cap binds Scribe's dollar billing**); GCP key
  restricted to TTS+STT, pin quota knobs, budget alerts $10/$20.
  > **Checkpoint:** TTS with Google key succeeds; TTS with EL key **fails**
  > (STT-scoping proven — what makes the 1.8 guard correctness).
- ☐ **3.2 Firestore impl.** `access-service/internal/store/firestore.go` (deps in
  the service module only); `ConsumeInvite` via `RunTransaction`; config from a
  `config/config` doc; wire `STORE=firestore`.
  > **Checkpoint:** memory suite green with **zero interface change**; root
  > module untouched.
- ☐ **3.3 Resend** (verify-item 4): smoke via `onboarding@resend.dev`; verify
  domain (SPF/DKIM), set `MAIL_FROM`, test spam placement. GitHub device flow is
  the documented fallback.
- ☐ **3.4 Deploy.** Secrets → Secret Manager; `gcloud run deploy mogi-access
  --source access-service --max-instances 1 …`; `roles/datastore.user`; seed
  `config/config` + first invite; re-run the smoke curl against prod.
  > **Checkpoint (3.3+3.4):** prod flow green incl. real OTP mail;
  > `git grep -i "sk-or\|AIza\|re_"` clean (public repo); `--max-instances 1` set.
- ☐ **3.5 Point the app at prod.** Set `access.DefaultURL` to the Cloud Run URL;
  `go build ./... && wails build`.
- ☐ **3.6 Prod e2e + drills.** Repeat 2.6 against prod. Drills: kill switch,
  rotation, per-tester revocation (`revoked:true` + delete OR key by hash).
- ☐ **3.7 Docs.** `roadmap.md` (status), `architecture.md` (3 bindings,
  `managed:changed` event, access-service data-flow note), `CLAUDE.md` (map
  rows), flip `managed-keys-plan.md` status to implemented.
- ☐ **3.8 Cohort launch.** Mint invites, confirm budget alerts, weekly-ops glance
  list into the service README.

## Hard ordering dependencies

1. Phase 0 before 1.4 (handler tests are the executable contract the client
   mirrors). **[0 done]**
2. In Phase 1: 1.1→1.2→1.3; 1.4→1.5→1.6; everything before 1.10
   (`wails generate module` once). **[1 done]**
3. 1.10 before all of Phase 2 (bindings + regenerated models gate TSX). **[1.10
   Go/TS surface done; the interactive `wails dev` smoke is still open but does
   not block writing Phase 2 TSX.]**
4. 2.1 before 2.2–2.5 (`handleAuthChange` is the staleness guard).
5. 3.1/3.2/3.3 → 3.4 → 3.5/3.6; testers only after 3.6.

## Verification

- Per step: named checkpoint commands (root `go build ./... && go test ./... &&
  gofmt -l .`; `cd access-service && go test ./...`; `cd frontend && npx tsc
  --noEmit`; `wails generate module` after Go surface changes).
- Phase gates: **0.8 curl smoke ✓ (passed)**; **1.10 automated gate ✓ (passed:
  Go build/test/vet/gofmt, access-service suite, `npx tsc --noEmit`)** — its
  console-driven backend e2e (`wails dev`) is still ☐; 2.6 full local e2e (kill
  switch, offline grace, BYOK regression, ClearAll); 3.6 prod e2e +
  revocation/rotation drills.

## Critical files

- `access-service/internal/server/server.go` ✅ + `internal/access/client.go` ✅
  — the two sides of the wire contract
- `internal/service/account.go` ✅ — activation/refresh/sign-out + the
  `applyKeyMode` invariant
- `internal/service/settings.go` ✅ — mode-aware AuthStatus + the three chokepoints
- `internal/store/managed.go` ✅ — managed namespace on existing pref primitives
- `internal/service/interview.go` / `voice.go` ✅ — `resolveModel` + TTS/catalog guards
- `app.go` / `window.go` ✅ — wiring, 3 bindings, startup refresh, `managed:changed`
- `frontend/src/…` — `lib/wailsBridge.ts` ✅ (3 methods exported); ☐ SetupPage
  fork, InviteActivation, ManagedAccountCard,
  Settings/ApiKeysSection/ModelsSection/VoiceSection, App.tsx (Phase 2)
