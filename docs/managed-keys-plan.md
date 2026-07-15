# Managed Test Accounts (Invite-Gated Keys) — Design Doc

> A **test-phase distribution mode**: instead of every user hunting down three API keys, a tester
> redeems an **invite code + email OTP** and the app fetches developer-funded keys and inserts them
> itself — sign in, and it just works. **BYOK stays a first-class equal mode**, untouched. The
> backend is a single small "access service" whose auth/infra is deliberately the embryo of the
> eventual paid tier. **Status: Phase 0 (access service) implemented; app integration not started.**
> *No payments in this phase — the developer eats a capped ~$20/month.*
> Step-by-step build plan & progress → [managed-keys-implementation.md](managed-keys-implementation.md).

## Context

Mogi's biggest adoption blocker isn't features — it's that using it requires obtaining **three API
keys** (OpenRouter, Google Cloud, ElevenLabs) across three consoles. That's fine for the
power-user/BYOK audience; it turns away everyone else before their first interview.

The long-term shape is a familiar freemium split (Cursor/Zed/TypingMind all converged here):

- **BYOK (free, forever)** — user supplies keys, pays providers directly, full model freedom,
  maximum privacy (their data never touches developer infrastructure). Zero developer cost.
- **Paid (~$10/mo, later, if demand shows up)** — zero setup, pinned cost-efficient stack, a backend
  **proxy** holding master keys, visible "N interviews/month" cap. A whole second product: accounts,
  Stripe, metering, abuse defense, ops.

This design is the **test-phase middle step**: developer-funded keys behind an invite gate, with no
payment infrastructure. Two constraints shaped it:

1. **Distribution must not be manual.** Handing keys to each tester (even though the existing
   Settings key fields make that a zero-code option) doesn't scale past a handful of people and
   makes rotation a group-chat fire drill.
2. **The interview data path stays direct.** The access service hands out *keys*; the app keeps
   calling OpenRouter/Google/ElevenLabs **directly from the tester's machine**, exactly as BYOK
   does. Screenshots and voice audio **never transit the developer's server** — they're merely
   billed to the developer's provider accounts. (The paid tier's proxy gives that up; this phase
   doesn't have to.)

## Decisions, resolved

### Why not GCP Secret Manager as the distribution mechanism

The obvious-sounding "put keys in Secret Manager, grant testers access after email validation"
doesn't exist as a primitive: Secret Manager's gate is **GCP IAM identity**, not email validation.
Letting a tester read a secret means either granting *their Google identity* a role per-person
(manual admin relocated, plus a desktop OAuth dance), or shipping credentials that can read the
secret (which just moves the extractable secret one level up). Secret Manager's proper role is
holding the master keys **for the access service** — which is where it appears below.

### Identity: hand-rolled email OTP

Tester enters email + invite code → the service emails a 6-digit code → tester types it in.

- *Why hand-rolled:* a 6-digit emailed code with expiry and attempt limits is about as small as
  auth gets (~50 lines of service logic + a transactional mail API); it matches the "short email
  validation" product intent; it works for testers without any particular account.
- *Why not Firebase Auth:* its email-**link** flow is built for web/mobile — getting a browser
  link-click back into a desktop Wails session needs deep-link/custom-token glue that costs more
  than the managed auth saves.
- *Why not GitHub device flow:* genuinely attractive for this audience (no email infra, no
  spam-folder risk, identity = an aged GitHub account — a strong abuse signal). Passed over to keep
  the flow account-agnostic, but it is the **designated fallback** if OTP deliverability turns
  into a fight.
- **Prerequisite (the one thing consoles can't conjure):** transactional mail (Resend/SES) requires
  a **verified sending domain** with SPF/DKIM records — their test domains only mail yourself. No
  domain → buy one (~$10/yr) or fall back to GitHub device flow.

### Access: invite codes, not an allowlist, not open signup

The developer mints codes with N uses each (`MOGI-A7F2`, 10 uses) and drops them in a DM/Discord.

- *Why:* zero per-tester admin (the whole point was removing the human from the loop); revoking a
  code cuts off its cohort; codes give coarse attribution ("who invited whom").
- *Why not an email allowlist:* strictest control, but pre-registering every tester is the manual
  work again.
- *Why not open signup:* email gates are trivially passed (disposable inboxes), so open signup
  makes the spend caps carry **all** protection. Fine as a later stage, wrong as a first one.
  Structurally, open signup is just "one infinite-use invite code" — the design upgrades to it by
  flipping data, not code.
- A quiet abuse win: the service validates the invite code **before** sending any email, so the
  OTP endpoint can't be used as a spam relay by non-invitees.

### Key strategy: mint per-tester where it matters, share-and-scope where it doesn't

| Provider | Key | Guard |
|---|---|---|
| OpenRouter | **Per-tester**, minted at activation via the provisioning API, named by tester email | **$3 hard credit cap** per key; revoke one key = revoke one tester, with a name attached |
| Google Cloud | One **shared** API key | Key restricted to the TTS + STT APIs only; per-API daily quotas as abuse ceilings; budget alerts at $10/$20 |
| ElevenLabs | One **shared** key, **scoped to speech-to-text only** with a monthly credit cap | A key that can't call TTS can't burn premium-voice money, no matter who holds it |

- *Why per-tester OpenRouter but shared voice keys:* an extracted OpenRouter key is general-purpose
  LLM access — the attractive theft target — so it gets attribution and individual revocation. A
  TTS/STT-restricted Google key or an STT-only ElevenLabs key is barely worth stealing; caps and
  rotation suffice.
- *Why bundle ElevenLabs at all:* **Scribe is the cheap half of ElevenLabs** — batch transcription
  is ~$0.22/hour of audio, *cheaper than Google STT* (~$1/hr) and more accurate on technical
  vocabulary. The app already encodes this preference: `activeSTT` in
  [internal/service/voice.go](../internal/service/voice.go) prefers Scribe whenever its key is
  present. So the managed key set lands on the optimal combo (Scribe STT + Google TTS + Gemini)
  **with zero voice-code changes** — and if the Scribe cap is ever hit, the existing fallback
  drops to Google STT and voice keeps working. ElevenLabs **TTS** remains BYOK-only in every tier
  (10–50× Google's TTS price).

### Managed mode is pinned-model, open-voice

- **Model pinned server-side.** The `/keys` response carries `pinnedModel` (launch: Gemini 2.5
  Flash-class); the ModelPicker renders as a static "managed" label. *Why server-side:* the
  developer can swap/upgrade the model for the whole cohort with **no app release** — never market
  the model name, market the tier. One model also means one behavior to support and a predictable
  per-session cost. *Open question to validate before launch:* that a Flash-class model holds the
  Socratic persona ([internal/ai/prompts.go](../internal/ai/prompts.go)) without drifting into
  hint-dumping — test against a stronger model first.
- **Voice prefs stay open, catalog filtered.** Testers keep the Google voice picker and playback
  speed, but the managed-mode catalog is filtered to the Standard/Neural2 tiers (Chirp HD voices
  cost ~2× Neural2 — an open picker shouldn't be an open wallet).
- **ElevenLabs is hidden as a TTS choice in managed mode.** Nuance: `activeTTS` only falls back
  when the chosen provider has *no key* — with a managed (STT-scoped) ElevenLabs key present, the
  client is non-nil and selecting EL TTS would fail at the API with a permission error rather than
  falling back. Hiding the option is correctness, not just polish.

### BYOK coexists: one mode switch, two key namespaces

Managed keys and user keys are stored **separately** (same key store, `managed:`-prefixed provider
names), and a single preference — `KeyMode: managed | byok` — decides which set the service layer
resolves into the live `Providers` registry
([internal/service/providers.go](../internal/service/providers.go)). That one rule buys:

- **Non-destructive switching.** Pasted BYOK keys and fetched managed keys live side by side;
  flipping modes never overwrites anything — the clobber problem disappears structurally.
- **Surgical sign-out.** Deletes managed keys + the session token; BYOK keys remain as left.
- **Mode-scoped lockdown.** Pinned model and filtered voices follow `KeyMode == managed`; BYOK mode
  keeps today's full model picker and voice freedom, unchanged, in the same build.
- **v1 is strictly either/or.** Per-provider mixing ("managed base + my own ElevenLabs TTS key on
  top" — the premium-voice vision) becomes a small follow-up: resolution goes from global to
  per-provider ("user key wins for TTS, managed for the rest"). Deferred to avoid precedence
  surprises like a pasted OpenRouter key silently unpinning the model.

### The security model, stated honestly

Auto-inserting keys improves **UX, not security**. Managed keys still land in the same plaintext
SQLite store on the tester's machine and still arrive in a response that machine received — a
motivated tester can extract them. Hiding them from the UI prevents accidents, nothing more.
The actual protections are **caps** (blast radius), **attribution** (named per-tester keys),
**revocation** (per-tester, per-code, or global), and **rotation** (below). The email gate is a
speed bump; the invite allowlist is the real gate. Master keys and the provisioning key exist
**only** in Secret Manager and the service's runtime — never in the repo (public!), the binary, or
a release artifact.

### The launch-time refresh loop (rotation, enforcement, offline grace)

The app calls `GET /keys` with its session token **on every launch**:

- **200** → upsert managed keys + `pinnedModel` into the store. This is what makes **silent
  rotation** work: rotate the shared Google/EL keys or re-mint OpenRouter keys server-side any
  time; running installs heal on next start. Leaks stop being fire drills.
- **401/403 (revoked / test phase ended)** → purge managed keys, flip to the sign-in state with the
  server-supplied message. Deleting a tester doc *is* the enforcement mechanism.
- **Network failure** → keep cached keys and proceed. A backend blip must never brick a tester
  mid-interview-prep; the service being down only pauses *new* activations and rotations.

## The access service

One small Go service (`mogi-access`), deliberately boring:

- **Cloud Run** — scale-to-zero ≈ $0 at this scale, same GCP project as TTS/STT.
- **Firestore** (free tier) — collections: `invites {code, maxUses, uses, active}`,
  `otps {emailHash, codeHash, expiresAt, attempts}`,
  `testers {email, inviteCode, orKeyId, createdAt, revoked}`,
  `sessions {tokenHash, email, createdAt}`, and a `config` doc (`testPhaseActive`, `pinnedModel`).
- **Secret Manager** — master Google/ElevenLabs keys + the OpenRouter provisioning key.
- **Resend** (or SES) — OTP mail. Free tiers cover a test cohort many times over.

| Endpoint | Behavior |
|---|---|
| `POST /activate {email, inviteCode}` | Validate invite (exists, active, uses left) **before** any email; then create OTP (10-min expiry) and send it. Rate limits: 3/email/hour, 10/IP/hour. |
| `POST /verify {email, code}` | ≤5 attempts then the OTP dies. On success: consume an invite use, create the tester, **mint their OpenRouter key** ($3 cap, named by email), return a session token (stored hashed). |
| `GET /keys` (Bearer token) | `{openrouter, google, elevenlabs, pinnedModel}` — or 403 with a message when `testPhaseActive` is off / the tester is revoked. |

**Admin is the Firestore console + provider dashboards — no admin UI.** Minting an invite is
creating a document. The **kill switch** is flipping `testPhaseActive` (apps sign out gracefully on
next launch) plus, for a real emergency, bulk-revoking provisioned OpenRouter keys. Weekly ops is a
glance at the OpenRouter per-key usage graphs (attribution built in), GCP billing, and EL usage.

## Cost model (~$20/month ceiling)

Per-session sketch (order-of-magnitude, to be validated against one real logged session before
caps are finalized): Flash-class LLM ~$0.05–0.15 (screenshots per turn, short replies), Scribe STT
~$0.04 per 10 min of speech, Google Neural2 TTS ~$0.10–0.15. **Call it $0.20–0.35 typical, $0.60
heavy.**

- 10 testers × 4 interviews/month ≈ **$8–14/month expected** — inside budget with margin.
- **Exposure vs. expected:** 10–15 testers × $3 OpenRouter caps = $30–45 *worst-case lifetime
  exposure*, but caps are ceilings, not spend; expected LLM total is a few dollars.
- Backend cost ≈ $0 (Cloud Run scale-to-zero, Firestore/Resend free tiers).

## App-side changes (small by design)

Per the 3-layer rules in [CLAUDE.md](../CLAUDE.md):

- **New client package `internal/access`** — the HTTP client for the access service (activate /
  verify / fetch-keys), mirroring how `internal/updater` wraps an external HTTP API.
- **Store** — managed keys as `managed:`-prefixed rows in the existing key store; `KeyMode` +
  session token + pinned model in preferences. No schema surgery.
- **Service** — a new `internal/service/account.go` owning activation, launch refresh, sign-out,
  and mode switching; key resolution ("which namespace feeds `Providers.SetKey`") extends the
  existing settings propagation. `AuthStatus` becomes mode-aware so the SetupPage gate passes in
  managed mode. Unit-tested against fakes like every other service.
- **Bindings (thin, per house rules)** — `RequestTestCode(email, inviteCode)`,
  `ActivateTestAccount(email, otp)`, `ManagedStatus()`, `SignOutTestAccount()`,
  `SetKeyMode(mode)`; run `wails generate module`, export via
  [lib/wailsBridge.ts](../frontend/src/lib/wailsBridge.ts).
- **Frontend** — SetupPage forks into two doors ("I have an invite code" / "Use my own API keys" —
  the existing flow untouched). Settings → API Keys renders by mode: managed shows an account card
  (email, "test account", **Switch to my own keys** which flips the mode without signing out,
  **Sign out**); BYOK shows today's key fields plus a small "Have an invite?" entry point.
  ModelPicker locked to a managed label; voice catalog filtered as above.

## Privacy

Shown at activation (one line, checkbox): *"Interviews run through the developer's API accounts
(OpenRouter, Google, ElevenLabs). Your screen captures and voice audio are processed by those
providers; nothing is stored on the test server."*

What the access service ever holds: **email + hashed session token + key metadata**. Interview
content — screenshots, audio, transcripts — flows directly from the tester's machine to the
providers, never through developer infrastructure. BYOK mode remains the maximum-privacy option
(own accounts, zero developer involvement), and the public, auditable client is what makes both
claims checkable.

## Future: how this becomes the paid tier

Deliberately, nothing here is throwaway:

- **Carries over:** email auth, session tokens, Firestore accounts, invite mechanics (→ promo
  codes), the kill switch, the mode switch + key namespaces, the pinned-model-from-server pattern.
- **Changes:** the service stops *returning* keys and starts **forwarding requests** (the proxy),
  Stripe replaces invites as the gate, and caps become the subscription's "N interviews/month".
  That flip is also the moment the privacy story changes (content transits the server) — the
  paid-tier docs must say so as plainly as this one says it doesn't.

(BYOK friction still has its own future lever, independent of all this: the OpenRouter OAuth PKCE
flow already on the roadmap's Phase 4 — key acquisition in two clicks, user pays OpenRouter
directly, no developer involvement.)

## Verify before building

1. **OpenRouter provisioning keys:** confirm per-key spend limits (believed yes) and whether
   per-key **model restriction** exists (believed no — the $3 cap is the guard either way).
2. **ElevenLabs:** confirm a per-key credit cap actually applies to Scribe's per-minute dollar
   billing; if not, the guard is keeping that account's balance small.
3. **GCP:** exact quota knobs for capping TTS/STT daily usage on an API-restricted key.
4. **Resend/SES:** domain verification + deliverability from a fresh domain (the GitHub
   device-flow fallback stands by).

## Implementation phases (when built)

1. **Phase 0 — access service:** Cloud Run Go service + Firestore schema + Secret Manager wiring +
   Resend; the three endpoints with rate limits/expiries; deploy; smoke-test with curl. Mint
   real capped provider keys.
2. **Phase 1 — backend integration:** `internal/access` client; `service/account.go` + key
   resolution + mode-aware `AuthStatus`; launch refresh; store additions; bindings +
   `wails generate module`; service tests against fakes.
3. **Phase 2 — frontend:** SetupPage fork, Settings account card / mode switch, pinned-model
   label, filtered voice catalog, sign-out; loading/error state per async call as usual.
4. **Phase 3 — ops & docs:** budget alerts + quotas set; privacy notice; invite codes minted;
   update [roadmap.md](roadmap.md), [architecture.md](architecture.md) (new bindings + data flow),
   and [CLAUDE.md](../CLAUDE.md) (codebase map: `internal/access`, account service, setup fork).

**Verification:** `go build ./... && go test ./...` + `gofmt`, `npx tsc --noEmit`; end-to-end under
`wails dev`: redeem a real invite → OTP mail arrives → keys land, model pinned, Scribe+Google
resolve; kill switch flips → app signs out gracefully on relaunch; BYOK regression — paste own
keys, full picker back, managed keys untouched; sign-out leaves BYOK keys intact; `/keys`
unreachable → cached keys still work.
