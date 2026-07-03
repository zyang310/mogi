# Company Practice Mode — Implementation Plan

> An **opt-in** mode where the user practices for a specific company, with two ways in:
> **pick a real interview-frequency LeetCode problem** from that company's question pool, or hit
> **Mock Interview** — the app draws **two realistic questions** (an easier one, then a harder one)
> the way a real 45-minute screen actually runs. Either way the AI greets them in character,
> assigns the problem **by reference**, and the normal screen-driven interview takes over —
> flavored by that company's interviewing style.
> Roadmap entry: [roadmap.md](roadmap.md) → Phase 6. **Status: implemented (Phases 0–5).**
> *Plan v2 (2026-07-01): new data source, Mock Interview replaces the randomize pick, storage
> decision recorded.*

## Context

Today every session is **screen-driven**: the candidate brings their own problem and the AI reads
it from the screenshot. This feature adds a second way in — pick **Google** (or any of ~650
companies), and the AI says *"Hi, I'm your interviewer at Google today. Let's work on Two Sum —
open it on LeetCode and walk me through your first thoughts."* From there the existing capture +
Socratic loop runs unchanged, but the interviewer behaves the way that company's interviewers do.

**This is additive, never a replacement.** The default Hub flow ("interview on whatever I have
up") stays the default and is untouched. Company Practice is a separate tab with two flows:

- **Browse & pick** — search the company's question pool (difficulty filter, frequency sort),
  choose a problem, start a single-question session.
- **Mock Interview** — one button. The app draws two questions the company realistically asks
  (frequency-weighted, recent when possible), **easier first, harder second**, and does not show
  them up front — like a real interview, you find out when the interviewer tells you.

## Two design tensions, resolved (the soundness core)

- **Screen-driven invariant** ([CLAUDE.md](../CLAUDE.md): *"never send a written problem statement
  — the screenshot carries it"*). Company mode assigns a problem **by reference only** — title +
  difficulty + LeetCode link — **never** the problem statement text. The candidate opens the real
  problem; the AI still reads the actual prompt and their code from the screenshot. We bundle
  **metadata only** — which also sidesteps LeetCode-content copyright.
- **"AI speaks first"** ([CLAUDE.md](../CLAUDE.md): *"the AI never speaks unless the user
  typed/spoke first"*). The opener is a **template-derived greeting** shown in the transcript and
  spoken aloud (if voice mode is on). To avoid models that reject a leading non-`user` turn, the
  opener is **not** inserted into model history — instead the company **system prompt encodes**
  *"you have just greeted the candidate and assigned {problem}; continue from there."* Model
  history stays `system → user → …`. This is a deliberate, scoped exception that fires only at the
  start of a company session the user explicitly initiated. The same trick covers the mock
  Q1→Q2 handoff: the transition happens **inside a normal reply** to something the candidate said,
  so the no-unprompted-speech rule holds.

## Data source

[github.com/snehasishroy/leetcode-companywise-interview-questions](https://github.com/snehasishroy/leetcode-companywise-interview-questions)
— actively maintained (scraped from LeetCode's premium company filter; last push May 2026). One
folder per company (**657**), each with up to five CSVs by recency window (`thirty-days`,
`three-months`, `six-months`, `more-than-six-months`, `all`). Format:

```
ID,URL,Title,Difficulty,Acceptance %,Frequency %
1,https://leetcode.com/problems/two-sum,Two Sum,Easy,57.5%,100.0%
```

Measured facts that shape the design (from the 2026-05 snapshot):

- `all.csv` exists for **654** companies — **17,641 rows**, 3,358 unique problems. Trimmed to the
  columns we need, the whole dataset is **~1.6 MB raw / ~400 KB gzipped**.
- **Algorithm problems only:** the generator filters rows against LeetCode's `algorithms` category,
  dropping SQL/database, Pandas, Shell, Concurrency and JavaScript problems (this is an algorithmic
  coding interview). The **shipped** CSV is **647 companies / 16,914 rows** (7 all-non-algorithm
  companies drop out entirely).
- Recency windows are sparse: only **217** companies have `six-months.csv` — but those are exactly
  the big targets (Google 751, Amazon 665, Meta 385 recent problems). The long tail is thin:
  **175** companies have a single recorded problem, 484 have fewer than ten, 246 have only one
  difficulty tier. Every picker and draw rule below must survive these degenerate pools.
- Differences from the previously planned source (`liquidslr/...`): **no Topics column** (dropped
  from the UI), difficulty already canonical (`Easy`/`Medium`/`Hard`, no normalization),
  percentages carry a `%` suffix (strip on ingest), plus a LeetCode problem `ID` (nice for
  display: "1. Two Sum").
- **No license file**, same as the old source. We ship **factual metadata only** (titles, links,
  frequencies — no problem text), regenerate rather than vendor the repo, and **attribute** in
  `data/SOURCE.md` and the README.

**Coverage decision: ship all ~654 companies**, not a curated top-15. The old plan curated because
CSVs were hand-authored and biased toward problems the AI "reliably knows". Both reasons are gone:
a script generates the data, and screen-driven design means the AI reads the actual problem off
the screenshot — it never needs to know the problem by name. *Pro:* the long tail (users
interviewing at non-FAANG companies) is half the value, and it costs nothing at 1.6 MB. *Con:*
tiny pools make some features meaningless there — handled by guards (Mock Interview disables below
a minimum pool) rather than by excluding companies. The **authored style profiles** stay curated
(~10–15 big names + a generic fallback) — that's persona quality, not data coverage.

## Storage: an embedded snapshot, not SQLite, not runtime fetch

The dataset is **static reference data**; the decision is where it lives. Chosen: **trim at build
time, `go:embed` the result, parse into memory on first use.** Same pattern as the frontend
(`//go:embed all:frontend/dist` in `main.go`).

- **Why not SQLite?** Not size — 1.6 MB is nothing next to the screenshots we send every turn.
  It's the wrong *layer*: `data.db` is **user** data (sessions, prefs, keys) with a migration
  path; stuffing versioned reference data in means import-on-first-run logic, schema migrations on
  every data refresh, and stale rows when the app updates. And SQLite buys nothing here — the
  whole dataset parses into a `map[company][]Problem` in ~tens of ms and is then queried at memory
  speed. Databases earn their keep when data outgrows memory or needs ad-hoc queries; 17k rows is
  neither.
- **Why not fetch from GitHub at runtime?** *Pro:* picks up the repo's weekly refreshes. *Cons
  that outweigh it:* a network dependency and failure path in a flow that otherwise works offline;
  and the upstream is an **unlicensed personal repo** — its format or existence can change any day,
  which must never break a shipped binary. Question pools shift slowly; refreshing once per app
  release is plenty. (Embed-plus-background-refresh is the eventual best-of-both — deferred to
  stretch, it doubles the data paths for marginal freshness.)
- **Mechanics:** a `go generate` tool downloads the repo tarball (~880 KB), trims it to one CSV —
  `company,id,slug,title,difficulty,frequency,acceptance,recent` — and writes it under
  `internal/problems/data/`. The file is **committed**, so CI builds never need the network (same
  lesson as the `go:embed needs dist` CI fix — embedded assets must exist at build time). Links
  are derived (`https://leetcode.com/problems/{slug}`), so we store the slug, not the URL.
- **The `recent` flag** marks problems that also appear in the company's `thirty-days` /
  `three-months` / `six-months` files. One boolean column instead of duplicated per-window pools:
  the browse list shows everything, while the mock draw prefers the recent subset — "questions
  that will realistically be asked" — when it's big enough.

## Mock Interview mode (the marquee feature)

One button per company: **Start Mock Interview**. Design goals in tension: *realism* (what this
company actually asks, in a real interview's shape) vs *variety* (not Two Sum every time) vs
*robustness* (657 pools, many degenerate).

**The draw** (`problems.MockPair`, in Go — deterministic-testable with an injected `rand`):

1. **Pool** = the company's `recent` subset if it has ≥ 20 problems, else the full pool. *Why:*
   recency is the strongest "realistically asked" signal, but only 217 companies have it — the
   threshold keeps thin recent-pools from starving variety.
2. **Q2 (the harder one) first**: frequency-weighted random draw from the pool's Medium ∪ Hard
   problems (whole pool if that's empty).
3. **Q1 (the easier one)**: frequency-weighted draw from tiers **strictly below** Q2's
   (Hard → Easy∪Medium, Medium → Easy). If no lower tier exists (266 companies have zero Easy),
   draw from Q2's own tier and order the pair by **acceptance rate, higher first** — acceptance is
   a decent proxy for perceived ease when tiers can't separate them. Q1 ≠ Q2 always.
4. **Eligibility**: pool ≥ 5, else the button is disabled with a "not enough data — pick from the
   list" hint. Below that, a "random draw" is theater; the browse list serves tiny companies.

*Pros/cons of frequency weighting:* it is the realism signal (Two Sum at 100% frequency **is**
what Google asks), but it repeats favorites across draws. Accepted for v1; repeat-avoidance
(exclude problems drawn in the user's recent mock sessions) is a cheap stretch item.

**No preview, no re-roll.** Clicking the button shows a confirm modal (company, "two questions —
easier, then harder", suggested ~45 min) that **never shows the titles**; the draw happens
server-side on start. Real interviews don't offer a re-roll — the surprise *is* the practice.
*Con:* a determined user can cancel and restart to re-draw; harmless, and cheaper than building a
re-roll we'd then have to argue about.

**Q1 → Q2 handoff — AI-led, with a natural override** (decision: AI-led + manual override):

- The mock system prompt carries **both** problems and the rules: run Q1 first; **never name Q2
  early**; move on when Q1 is solved and complexity discussed, *or* the candidate is clearly out
  of road, *or* they ask to move on; keep pacing tight enough that two questions fit the session.
- The override needs **no new mechanism**: the candidate just says *"let's move to the next
  question"* — exactly like a real interview — and the prompt instructs the AI to honor it. A
  button that injects synthetic messages was considered and dropped: more surface, less natural.
- The session banner shows Q1 (title, difficulty, LeetCode link) and Q2 as a **hidden card** —
  click to reveal the link once the interviewer brings it up. Purely a frontend affordance; it
  sends nothing. *Trade-off:* the frontend can't know the exact moment the AI transitions, so
  reveal is user-clicked rather than automatic — acceptable for v1; parsing transitions out of
  replies would couple UI state to prose.
- *Why AI-led at all?* The interviewer controlling the pace is the realistic shape, and a
  timer-driven forced transition would violate the no-unprompted-speech rule (the AI can only
  speak inside a reply). *Con:* the model may linger on Q1 — mitigated by the prompt's pacing rule
  and the spoken override.

---

## Phase 0 — Data pipeline

1. **Generator tool** — `internal/problems/gen/main.go` (a `package main`, wired via
   `//go:generate go run ./gen` in the problems package):
   - Download `https://codeload.github.com/snehasishroy/leetcode-companywise-interview-questions/tar.gz/refs/heads/master`
     and stream-read it (`archive/tar` + `compress/gzip`) — no checkout, no git dependency.
   - For each company folder: parse `all.csv` (skip the 3 folders without one); strip `%` from
     frequency/acceptance; reduce the URL to its slug; set `recent` if the slug appears in any of
     that company's `thirty-days` / `three-months` / `six-months` CSVs.
   - Write `internal/problems/data/problems.csv`
     (`company,id,slug,title,difficulty,frequency,acceptance,recent`, sorted by company then
     frequency desc) and `internal/problems/data/SOURCE.md` (attribution, snapshot date, row and
     company counts).
2. **Commit the generated CSV** (~1.6 MB — embeds must exist at CI build time; the generator is
   only run manually to refresh). Add the upstream credit to the README as well.

## Phase 1 — `internal/problems` package

3. **Models** (`internal/models`, json-tagged as usual):
   - `Problem { ID int; Title, Difficulty string; Frequency, Acceptance float64; URL string; Recent bool }`
     (URL rebuilt from the slug at parse time — the frontend never string-builds links).
   - `CompanyInfo { Slug, Name string; ProblemCount int; MockEligible bool }`.
4. **Package core** — `//go:embed data/problems.csv`; parse once into `map[slug][]Problem` behind
   `sync.Once` (mirror the lazy-cache pattern in `internal/voice`). Display names: title-case the
   slug plus a small override map for the awkward ones (`amd → AMD`, `ibm → IBM`,
   `jpmorgan → JPMorgan`, `servicenow → ServiceNow`, …).
   - API: `Companies() []models.CompanyInfo`, `Problems(slug) ([]models.Problem, error)`,
     `MockPair(slug) ([2]models.Problem, error)` implementing the draw above (internal variant
     takes a `*rand.Rand` so tests are seeded and deterministic).
   - Browse-side filtering/sorting stays **frontend-side** over `Problems` (it's responsive and
     trivial); only `MockPair` lives in Go — the weighted, fallback-laden draw is exactly the
     logic that wants unit tests.
5. **Tests** (`internal/problems`): embed parses (647 companies, ~16.9k rows, URLs derived,
   difficulties canonical); `MockPair` invariants over many seeded draws — Q1 tier < Q2 tier, or
   same tier ordered by acceptance; recent-subset preferred when ≥ 20; degenerate pools
   (single-tier, zero-Easy, 2-problem) never panic and respect ordering; pool < 5 errors;
   Q1 ≠ Q2 always.

## Phase 2 — Prompts & profiles

6. **Company profiles** — `companyProfiles` map in `internal/ai` (slug → short **authored** style
   guidance): Google = algorithmic depth + complexity rigor + clean code; Amazon = DSA plus
   Leadership-Principles behavioral probing; Meta = fast pace; generic fallback for every other
   company. Authored guidance, **not** model recall — that's what makes it reliable. (Question
   *count* is no longer a profile concern — mock mode owns that.)
7. **Prompt builders, kept DRY** — in `internal/ai/prompts.go`, extract the current
   `BuildSystemPrompt` body into a shared base (rules + TTS speaking style).
   `BuildSystemPrompt()` returns it unchanged (default mode untouched).
   `BuildCompanySystemPrompt(company, profile string, problems []models.Problem)` = base + a
   company header injecting the persona and:
   - **1 problem** → *"you have just greeted the candidate and assigned {Title} ({Link}); if it
     isn't visible on their screen yet, ask them to open it."*
   - **2 problems (mock)** → both assignments + the handoff rules from the design section
     (Q1 first, never name Q2 early, transition on solved/stuck/asked, keep pace for two
     questions).
8. **Opening turn** — deterministic templates, no AI call (and TTS-safe: plain sentences, no URLs
   or symbols — the banner carries the link):
   - Single: *"Hi, I'm your interviewer at {Company} today. We'll be working on {Title} — it's
     rated {Difficulty}. Open it on LeetCode and walk me through your first thoughts when you're
     ready."*
   - Mock: *"Hi, I'm your interviewer at {Company} today. We have two problems to get through, so
     let's pace ourselves. First up is {Q1 Title}. Open it on LeetCode and talk me through your
     approach when you're ready."* — Q2 deliberately unnamed.
9. **Tests** (`internal/ai`): company prompt contains the base rules + persona + assignment;
   mock variant contains both problems and the don't-reveal-Q2 rule; single variant never
   mentions a second problem.

## Phase 3 — Bindings (`app.go`, kept thin)

10. **Bound methods**:
    - `ListCompanies() []models.CompanyInfo`, `ListCompanyProblems(slug) ([]models.Problem, error)`.
    - `StartCompanySession(slug string, problem models.Problem) (models.CompanySessionStart, error)`
      and `StartMockInterview(slug string) (models.CompanySessionStart, error)` — two explicit
      methods (cleaner Wails TS types than a list-length-polymorphic one), both thin over one
      internal helper; the mock draw happens **inside** `StartMockInterview`, so no picker UI ever
      sees the questions pre-start. `CompanySessionStart { Session models.Session; Opening string;
      Problems []models.Problem }` — a struct return for clean binding; `Problems` feeds the
      banner + reveal card.
    - `OpenURL(url string) error` wrapping `runtime.BrowserOpenURL` so LeetCode opens in the real
      browser, not the frameless webview.
11. **The shared start helper** mirrors `StartSession`'s body (guard on `a.active`, create the
    session row, start capture) with three additions:
    - Seed history with `BuildCompanySystemPrompt` instead of the base prompt.
    - **Persist the opener to the transcript** (`db.AddMessage`, role `assistant`) so history and
      the debrief include it — but **not** into model history (the system prompt carries the
      assignment; history stays `system → user → …`).
    - **Seed session meta at start** (`db.UpdateSessionMeta`): single → problem title +
      difficulty; mock → `"Mock: {Q1} + {Q2}"`. Then make `extractSessionMeta` keep a non-empty
      existing title/difficulty (it currently overwrites all columns) while still capturing
      `final_code` — no AI label guess needed when we already know the problem.
    - `StartSession` (default mode) stays **unchanged**. Run `wails generate module`; export via
      `lib/wailsBridge.ts`.

## Phase 4 — Frontend

12. **New tab** — extend the pill-nav `view` union in [App.tsx](../frontend/src/App.tsx)
    (`"hub" | "history" | "settings"` → + `"company"`) with its own pill button; new
    `CompanyPractice` component + CSS (one file each, reuse `.btn*`, chips, and the modal shell).
    Starting a company session sets the **existing** active-session state, so the active
    Chat/overlay UI is reused as-is; keep the returned `CompanySessionStart` (company + problems)
    in App state for the banner, cleared on end. Overlay (compact mode) is untouched.
13. **Browse & pick** — searchable company list (name + problem count, from `ListCompanies`) →
    company view: difficulty chips (All/Easy/Medium/Hard), sort (Frequency default / Title /
    Difficulty), rows with title, difficulty badge, frequency, a small "recent" chip when
    `Recent`, an "Open on LeetCode" icon (→ `OpenURL`), and "Start interview". All filtering
    client-side; loading/error state per async call as usual.
14. **Mock Interview CTA** — primary button atop the company view: *"Start Mock Interview — two
    questions, drawn from what {Company} actually asks."* Disabled with the hint when
    `!MockEligible`. Click → confirm modal (no titles: "two questions — easier, then harder ·
    ~45 min suggested · questions are revealed one at a time; you can always ask to move on") →
    `StartMockInterview`.
15. **Session banner + reveal** — `CompanyBanner` component above Chat: company name; single mode
    shows the problem chip (title · difficulty · link); mock mode shows the Q1 chip plus a Q2
    card rendered face-down ("Question 2 — revealed when your interviewer brings it up"; click to
    flip and expose title + link). Purely local state; sends nothing.
16. **Start flow** — on `CompanySessionStart`: enter the active-session UI, append `Opening` as
    the first interviewer turn, and `speak()` it when voice mode is on. The default Hub start path
    is unchanged.

## Phase 5 — Polish & stretch

17. Persist last-selected company + difficulty filter in `Preferences` (existing plumbing).
    Persist company/mode on the `sessions` row (one `addColumnIfMissing` migration — helper
    already exists in `internal/store/db.go`) so History can badge company sessions.
18. Stretch, in rough value order: mock repeat-avoidance (exclude recently drawn problems);
    elapsed-time context injected into user turns so the AI can pace Q1/Q2 against the session
    timer prefs (`SessionLimitMinutes` already exists); embed-plus-background-refresh for the
    dataset; AI-generated opener behind the templated default.
19. Docs: update [architecture.md](architecture.md) (bindings, data flow), [roadmap.md](roadmap.md)
    (Phase 6 scope now includes Mock Interview), and [CLAUDE.md](../CLAUDE.md) (codebase map:
    `internal/problems` + gen tool + new components; the screen-driven invariant nuance: company
    mode assigns **by reference**, never ships problem text).

## Verification

- **Go:** `go build ./...`, `go test ./...` (problems + prompt tests), `gofmt -l .`.
- **Bindings:** `ListCompanies` / `ListCompanyProblems` / `StartCompanySession` /
  `StartMockInterview` / `OpenURL` present under `frontend/wailsjs/`.
- **Types/bundle:** `cd frontend && npx tsc --noEmit && npm run build`.
- **Draw sanity (unit-level):** run `MockPair("google")` a few hundred seeded times — Q1 easier
  than Q2 (tier, or acceptance within tier) on every draw; a 4-problem company (e.g. `anthropic`)
  reports `MockEligible == false`.
- **End-to-end (`wails dev`):**
  1. Company Practice tab → search Google → filter Medium → pick a problem → Start → the AI
     speaks first, in character, naming the problem; "Open on LeetCode" opens the real browser.
  2. Mock Interview at Google → confirm modal shows **no titles** → the opener names only Q1;
     the banner shows Q1 + a face-down Q2; solve Q1 → the AI transitions to Q2 on its own; saying
     "let's move on" mid-Q1 also triggers the handoff; reveal card exposes Q2's link.
  3. Screenshots flow throughout; the interviewer is flavored by the company profile and never
     reveals answers. Ending the session produces a debrief covering both questions; the history
     row shows the seeded title.
  4. **Regression:** the default Hub flow still starts a normal screen-driven session with no
     company context and no unprompted opener; a tiny-pool company disables Mock but allows
     browse-and-pick.
