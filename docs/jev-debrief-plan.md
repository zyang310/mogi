# Jev-Scored Debriefs — Design Notes

> Let **Jev** (TypeSafe AI's typed-decision model, reached through OpenRouter) produce the
> debrief's **numbers** (the hire verdict and the five rubric scores, each with a confidence),
> while the session's own model writes the **words** (summary, strengths, improvements) *from*
> those scores. Today's all-LLM debrief stays as the fallback. Builds on the debrief in
> [history-feature-plan.md](history-feature-plan.md).
> **Status: designed, not wired into the app. The evaluation tool (`cmd/debrief-eval`) is built;
> switching waits on its results.**

## Why

Mogi already rates every session. `History.Debrief` (`internal/service/history.go`) makes one
LLM call over the transcript plus the captured final code and returns a verdict, five 1–5
scores, a summary, and strengths/improvements, cached once in `sessions.debrief`. Asking an LLM
to *also* be the scoring instrument has known weaknesses:

- **Scores drift run to run.** The same session can land a step apart on a second generation,
  so scores across sessions make a noisy progress signal.
- **No calibrated confidence.** The model can't say "I'm unsure" except by writing 0 or "".
- **Bad scores stick.** The debrief is cached and has no regenerate action, by design: it
  saves tokens.

Jev is built for exactly this shape of output. It answers **typed questions** about a text
"state": pick one of N options, place the state on N described levels, or give a
yes-probability. Each answer comes with a probability distribution and a confidence. It is fast
and cheap, and its answers can't fall outside the options declared, so there's no free-form
JSON to parse and repair. Langfuse and Promptfoo already ship it as an evaluation judge.

**Why post-session, not live.** The first idea was a live decision loop: poll Jev every couple
of seconds to make the interviewer nudge or ask on its own. It ran into the architecture three
ways:

- It reverses the "AI never speaks unprompted" rule.
- The transcript only changes when the candidate sends something, because voice is turn-based,
  so there's nothing new to poll.
- The backend has no push channel, and doesn't know when the user is talking.

Scoring a finished session has none of these problems. The debrief input is already text-only
(the final screenshot was transcribed to `final_code` at session end), which is exactly what
Jev accepts, and it's one call per session, cached.

## Decisions & trade-offs

- **Jev scores, the model explains.** Jev cannot write prose, so it can only own the numbers.
  The session model then writes the summary and strengths/improvements *given* Jev's scores,
  which keeps the words and numbers from contradicting each other ("great complexity analysis"
  next to a 2/5). The alternative was keeping today's scorecard and adding a Jev checklist
  beside it. That's simpler, but it leaves the noisy LLM scores as the headline.
- **Evaluate before switching.** Jev is a fast "System One" classifier. Judging whether a
  dynamic-programming solution is correct needs deliberate reasoning, which is where LLMs are
  strongest, so being good at fast labels doesn't make Jev a good interview judge. Bad cached
  scores would also stick. `cmd/debrief-eval` scores your real past sessions with Jev and
  compares them with the debriefs already cached. Nothing user-facing changes until the
  numbers support it.
- **Same input for both judges.** `ai.BuildTranscript` + `debriefContent` build the text both
  the LLM debrief and Jev read, byte for byte, so differences come from the judges, not the
  input.
- **Explicit "not enough evidence".** Jev always picks an answer. So:
  - The verdict question has an "unclear" option, which maps to the debrief's empty verdict.
  - Each dimension gets a yes/no "enough evidence to judge this?" check; below 0.5 it scores
    0, the debrief's existing "—".
- **Levels are situations, not degrees.** Every 1–5 level describes what happened
  ("Reached a working solution with some hints, but missed edge cases"), the form Jev places
  states on best. `DebriefPrompt` only gives one-line dimension definitions, so part of any
  disagreement is the anchors themselves. Adopting the same anchors in `DebriefPrompt` would
  help the LLM regardless (see Phases).
- **Through OpenRouter, not a new provider.** It reuses the existing OpenRouter key and
  `ai.Client`, so there's no new key slot or Settings card. Managed testers are covered by
  their capped key.

## The API (October 2026 — secondhand, verified only by the first real run)

| | Jev on OpenRouter |
|---|---|
| Model | `~typesafe/jev-latest` (alias; pinned versions such as `typesafe/jev-1.13`) |
| Endpoint | `POST https://openrouter.ai/api/alpha/decisions`, body `{model, state, questions}`. **Not** chat/completions. One source names `/api/v1/systemone` instead. |
| Question types | `choice` (≤ 255 options) · `score` (2–10 described levels → probability-weighted float `0..N-1` + per-level probabilities) · `noul` (yes-probability `0..1`) |
| Answers | typed values + probabilities + `confidence` (how concentrated the distribution is, not correctness) |
| Limits | text only; ~32K tokens for the state plus the longest question |
| Price | ~$0.04 per million input tokens, output free (vendor-reported) |

Gotchas worth knowing:
- Question **keys are never shown to the model**, so every question's instructions explain
  the state on their own (`stateContext` in `scoring.go`).
- Error responses may echo the whole state back, so `Decide` clips error bodies.

## How it's built (today)

- **`internal/ai/decisions.go`**:
  - `Decide(ctx, model, state, questions)`, a generic typed-decisions call with the same
    key and headers as `Complete`.
  - `DecisionRequestBody` (the exact bytes sent) and `SetDecisionsURL` (endpoint override).
- **`internal/ai/scoring.go`**:
  - The eleven-question set: a verdict choice, then five five-level scores and five evidence
    checks.
  - `ScoreSession` / `ParseScores` / `ScoreState`. `ScoreState` trims an oversized state from
    the middle, keeping the setup and the ending.
- **`internal/ai/transcript.go`**: `BuildTranscript` (moved from the service layer) and
  `debriefContent`.
- **`internal/store`**: `OpenAt`, `OpenReadOnly` (SQLite `mode=ro`, no migrations), and
  `DefaultPath`.
- **`cmd/debrief-eval`**: the comparison tool below. The app binary calls none of this yet.

## Evaluate first: `cmd/debrief-eval`

Run it on your Mac, where OpenRouter is reachable. It needs cgo for SQLite, which is fine
there. It opens `~/Library/Application Support/mogi/data.db` **read-only** and only looks at
finished sessions whose Debrief tab you have opened at least once, so a cached LLM debrief
exists to compare against.

```sh
go run ./cmd/debrief-eval -dry-run                     # exactly what Jev would receive; no key, no network
OPENROUTER_API_KEY=sk-or-… go run ./cmd/debrief-eval   # newest 20 debriefed sessions, 2 Jev runs each
go run ./cmd/debrief-eval -llm-rerun                   # also measure the LLM's own run-to-run spread (spends tokens)
go run ./cmd/debrief-eval -endpoint https://openrouter.ai/api/v1/systemone   # if the alpha route moved
```

- **Key:** `OPENROUTER_API_KEY`, else the BYOK key stored in `data.db`. The managed
  test-account key is never used.
- **Output:**
  - One row per session: the LLM's and Jev's verdict and headline score, how many steps
    apart the verdicts are, and LLM→Jev for each dimension.
  - Then a summary:
    - verdict agreement, exact and within one step
    - mean |LLM−Jev| per dimension
    - the number of |Δ| ≥ 2 disagreements
    - Jev's run-to-run spread, and the LLM's with `-llm-rerun`
    - the Jev version that answered, and the tokens used
- **If the first real call fails** (400/404), the error quotes the response. The likely causes
  are the path, or the question field names (`type` vs `kind`, the `options`/`levels`
  shapes), which live on `DecisionQuestion` in `internal/ai/decisions.go`.

**Decision rule.** Switch only if all three hold:
- within-one-step verdict agreement is at least ~85 %
- Jev's run-to-run spread is well below the LLM's
- reading the biggest disagreements favours Jev, or is a wash

A personal history is a small sample, so treat the numbers as directional and **read the
disagreements**; that reading is the real test.

## Design: when it ships

**On a cache miss, `History.Debrief`:**
1. Calls `ScoreSession`.
2. If Jev answers with a verdict, asks the session model for the prose only, through a new
   `GenerateDebriefFromScores` and `DebriefFromScoresPrompt`. That prompt receives the
   transcript, the code, and Jev's verdict and rubric, and returns
   `{summary, strengths, improvements}` consistent with those scores. The verdict and rubric in
   the cached debrief come from Jev, with scores rounded to the existing integer rubric.
3. Otherwise falls back to today's `GenerateDebrief`, unchanged. That covers a Jev error, an
   "unclear" verdict, or a malformed reply.

Latency is roughly today's: Jev takes 70–500 ms, and the LLM writes less.

**Boundary.**
- `models.Debrief` gains `scorer` (`"jev"`, or empty for the LLM) and `confidence` (the
  verdict's). Old cached debriefs simply lack them.
- Whether the rubric goes fractional is for the evaluation to decide. It's a Go-side change
  only: cached integers decode into floats.
- Regenerate bindings with `wails generate module` (models only).

**Service.**
- The `AI` interface gains `ScoreSession` and `GenerateDebriefFromScores`; extend `fakeAI`.
- History tests to add: the Jev path, the fallback on error, the fallback on "unclear", and
  that the cached JSON carries `scorer`.

**UI.**
- One footnote in `Debrief.tsx`: "Scores by Jev · 82% confident; feedback by <model>".
- `verdict.ts` and the radar chart are unchanged.

**Model.** `ai.DefaultScoreModel` is the alias today. Pin the version the evaluation ran
against.

**Managed mode.** Jev bills the tester's capped OpenRouter key; it's negligible per session.
Confirm managed keys can reach the model. The server-pinned interview model is unaffected.

**Privacy.** Transcript and transcribed code reach TypeSafe through OpenRouter: a new model
provider behind the same key, and still no screenshots. Note it in the Privacy section when
this ships.

## Mock interviews

- **Per-problem results need the assignment.** "Solved Q1, *Two Sum*?" as a yes/no question
  is cheap and useful. But the debrief doesn't know the assigned problems: today they live
  only in the system prompt and the seeded "Mock: A + B" title. Persist them on the session as
  a `problems` JSON column, written by `startCompanyInterview` (the `question_sets` JSON-column
  precedent).
- **Q1's code is never captured.** `final_code` comes from the last screen, which shows Q2. So
  Q1 is judged from the transcript alone. That still works for "did they solve it", because
  the interviewer states correctness explicitly (hard rule 10), but not for code quality.
  Snapshotting code at the Q1→Q2 handoff is separate work.

## Phases

0. **Evaluate (built):** run `cmd/debrief-eval` on real sessions. This also verifies the API
   shape.
1. **Backend:** Jev-first scoring with the LLM fallback in `History.Debrief`; prose from scores;
   tests.
2. **UI and boundary:** the `scorer` and `confidence` fields, the footnote, and regenerated
   bindings.
3. **Optional:**
   - a "Re-score" action that cheaply re-runs only the numbers on old debriefs, filling the
     pre-`pace` "—" gaps
   - per-problem mock results (with the `problems` column)
   - the same situation anchors in `DebriefPrompt`

## Verification

- `go test ./internal/ai/ ./cmd/debrief-eval/`:
  - request shape, against an `httptest` decisions server
  - answer mapping, including the evidence gate and "unclear"
  - middle-trimming of oversized states
  - a read-only fixture database under a path with a space
  - the end-to-end report, against a fake Jev
- `internal/service` tests need gohook's X11 headers on Linux, plus a display (`xvfb-run`).
  CI runs them on macOS.
- Live: the first `go run ./cmd/debrief-eval` against OpenRouter.

## Sources

- Jev on OpenRouter:
  [OpenRouter Jev guide](https://openrouter.ai/docs/guides/community/jev.md) ·
  [TypeSafe SDK guide](https://openrouter.ai/docs/guides/community/typesafe-sdk) ·
  [Hakky: the alpha decisions endpoint](https://book.st-hakky.com/data-science/jev-usage-openrouter-vercel) ·
  [OpenRouter listing](https://openrouter.ai/%7Etypesafe/jev-latest)
- Question types and judge usage:
  [OpenRouter: What is Jev](https://openrouter.ai/blog/insights/what-is-jev/) ·
  [Langfuse: Jev as a judge](https://langfuse.com/docs/evaluation/evaluation-methods/jev-as-a-judge) ·
  [Promptfoo TypeSafe provider](https://www.promptfoo.dev/docs/providers/typesafe/) ·
  [Choice / Score / Noul](https://opentweet.io/jev/choice-score-noul) ·
  [Drawpie explainer](https://drawpie.com/blog/what-is-jev-typesafe-ai-model/)
