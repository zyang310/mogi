# Custom Question Sets — Design Notes

> Company Practice add-on: the user curates a **named subset of a company's question pool**
> (e.g. a "My Google 50") and runs interviews that draw from exactly those questions — a
> **set mock** (two-question draw, easier first, Q2 hidden) or a **single-question start** on
> any set member. Builds on [company-practice-plan.md](company-practice-plan.md).
> **Status: implemented (2026-08).**

## Why

The built-in mock draws 2 from a company's *entire* pool (Google: ~2k). That is great for
realistic surprise, but useless for deliberate practice — "drill the 50 questions I keep
failing" was impossible. Sets make the draw pool user-curated while keeping everything else
(persona, opener, hidden Q2, history badging) identical to a pool mock.

## Decisions & trade-offs

- **Per-company sets, not standalone.** A set belongs to the company whose pool it was picked
  from, so a set mock reuses the company persona, banner, templated openers, and history
  labels wholesale — `StartSetMock` funnels into the same `startCompanyInterview` body as pool
  mocks, and *nothing downstream changed*. Standalone cross-company sets would have needed a
  new persona story ("interviewer at *Arrays drill*"?) for little gain.
- **Questions are picked from the pool only — no manual entry.** Every set item carries real
  metadata (id, difficulty, frequency, LeetCode URL), so frequency weighting, difficulty
  badges, and the hardcoded "Open it on LeetCode" opener wording all stay truthful. Manual
  entry would reopen every one of those assumptions for marginal value.
- **Sets store full `models.Problem` snapshots, not references.** The dataset CSV is refreshed
  biweekly and rows can drop or renumber; snapshots keep saved sets working forever. The
  editor renders the "in this set" list from the snapshots (not from pool matches), so a
  question that vanished from the pool is still visible and removable.
- **Storage: one `question_sets` table with a JSON `questions` column** (not a second items
  table). The UI always edits and saves a whole set (≤200 items), so per-item rows buy
  nothing but FK delete ordering; the JSON-blob-in-a-TEXT-column approach mirrors the
  `sessions.debrief` precedent. A corrupt blob degrades to an empty set on read (list keeps
  rendering; the min-2 draw guard then errors politely) instead of bricking the section.
- **Draw rules** (`problems.MockPairFrom`, sharing the tiered weighted core `drawPair` with
  `MockPair`):
  - **Minimum 2 distinct questions** — not `mockMinPool` (5). A curated set is deliberate;
    two questions already make a meaningful pair. The company draw keeps its 5-floor.
  - **No recent-subset narrowing.** The pool draw prefers the "recently asked" subset when
    it's big enough; a set *is* the user's explicit pool, so nothing is silently filtered.
  - **Dedupe by URL before drawing** (the URL is a question's identity key). The built-in
    draw dedups positionally and a stored repeat could have produced Q1 == Q2; the service
    also dedupes on save, so this is defense in depth.
- **Deleting a set never touches sessions.** Sessions are tagged with the company display
  name + mode at start (never the set id), so history survives set deletion by construction.
- **Validation lives in the service** (`internal/service/sets.go`): trimmed non-empty name
  (≤80 chars), owning company slug, every question needs title + URL, dedupe, 1–200 after
  dedupe, backend-assigned UUID on first save. Errors are user-readable — they surface
  verbatim in the editor.

## Data shape

```go
// internal/models/problem.go
type QuestionSet struct {
    ID          string    // UUID, backend-assigned on first save
    CompanySlug string    // owning pool, e.g. "google"
    Name        string
    Questions   []Problem // full snapshots, never nil
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

SQLite: `question_sets(id PK, company_slug, name, questions TEXT/JSON, created_at,
updated_at)`; wiped by Settings → Clear all local data like every other table.

## Surface

- **Bindings** (`app.go`, 1-line delegations): `ListQuestionSets(slug)`,
  `SaveQuestionSet(set)` (upsert, returns the stored set), `DeleteQuestionSet(id)`,
  `StartSetMockInterview(setID)`. Single-question starts reuse the existing
  `StartCompanySession(slug, problem)` — the frontend already passes a whole `Problem`.
- **UI** (company detail view): a "My sets" band between the Mock banner and the browse
  list — `QuestionSetCard` (expandable rows, per-question single start, inline two-step
  delete) and `SetEditor` (modal: name + snapshot selection + search-the-loaded-pool picker;
  no backend call to search). A second instance of the mock confirm modal fronts the set
  draw.

## Files

`internal/problems/problems.go` (`MockPairFrom` + `drawPair` split) ·
`internal/service/sets.go` (+ `Interview.StartSetMock`) · `internal/store/question_sets.go`
(+ `db.go` DDL/ClearAll) · `internal/models/problem.go` ·
`frontend/src/components/company/{QuestionSetCard,SetEditor}.{tsx,css}` + CompanyPractice
integration.
