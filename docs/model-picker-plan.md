# Model Picker — Implementation Plan (Phase 3)

> **Status: planned — not yet implemented.** This is the execution spec for the Phase 3 model picker. Pick it up in a fresh session and work top to bottom.

## Context

The OpenRouter model is hard-defaulted to `anthropic/claude-sonnet-4` ([internal/store/preferences.go:53](../internal/store/preferences.go)) and there is **no UI to change it**. The wiring around it already exists:
- `StartSession("")` falls back to `prefs.Model` ([app.go:123](../app.go)).
- `SendMessage` passes `a.active.session.Model` to `ai.Client.Complete` ([app.go:199](../app.go)).
- `Preferences.Model` already round-trips through SQLite via `keyModel` ([preferences.go:63,112](../internal/store/preferences.go)).

So the **data layer is done**. What's missing: (a) a way to **list** OpenRouter models, and (b) a **UI** to pick one and write it to `Preferences.Model`.

Why now:
- The app is **screen-driven** (sends screenshots), so only **vision-capable** models work — the picker must default to vision models.
- The OpenRouter **402 (insufficient credits)** issue: surfacing **free** models and **per-token pricing** lets you choose a cost-free / cheaper model deliberately. (The `max_tokens: 1024` cap in `internal/ai/client.go` already reduced the credit pre-auth; the picker is the complementary fix.)

**Outcome:** a searchable model picker in Settings — vision-first, free models flagged, pricing shown — that persists to `Preferences.Model`, which every later session uses automatically.

## Confirmed OpenRouter `/models` schema

`GET https://openrouter.ai/api/v1/models` (public; auth optional). Returns `{ "data": [ {entry}, … ] }`. Fields we use per entry:

```
id                 string    e.g. "anthropic/claude-sonnet-4"
name               string    e.g. "Anthropic: Claude Sonnet 4"
description        string
context_length     number
architecture: {
  input_modalities  string[]  // VISION ⇔ contains "image"
  output_modalities string[]
  modality          string    // legacy, e.g. "text+image->text"
}
pricing: {
  prompt      string  // USD per INPUT token, e.g. "0.000003" ("0" ⇒ free)
  completion  string  // USD per OUTPUT token ("0" ⇒ free)
  image       string  // USD per image (may be "0"/absent)
  request     string
}
top_provider: { max_completion_tokens number|null }
supported_parameters string[]   // e.g. "max_tokens", "tools"
```

- **Vision-capable** ⇔ `architecture.input_modalities` includes `"image"`.
- **Free** ⇔ `pricing.prompt == "0" && pricing.completion == "0"` (these ids usually end in `":free"`).
- Prices are **per token** — multiply by 1e6 for a readable `$ / 1M tokens`.
- Sanity-check live before wiring: `curl -s https://openrouter.ai/api/v1/models | jq '.data[0]'`.

## Backend (Go)

### 1. New DTO — `internal/models/model.go`
```go
package models

// Model is a selectable OpenRouter model (subset of /models, for the picker UI).
type Model struct {
    ID              string  `json:"id"`
    Name            string  `json:"name"`
    Description     string  `json:"description"`
    ContextLength   int     `json:"contextLength"`
    SupportsVision  bool    `json:"supportsVision"`
    IsFree          bool    `json:"isFree"`
    PromptPrice     float64 `json:"promptPrice"`     // USD per 1M input tokens
    CompletionPrice float64 `json:"completionPrice"` // USD per 1M output tokens
}
```

### 2. `internal/ai/client.go` — add `ListModels`
Reuse the existing `Client.httpClient`, `apiKey`, and header pattern from `Complete`.
```go
const openRouterModelsURL = "https://openrouter.ai/api/v1/models"

func (c *Client) ListModels(ctx context.Context) ([]models.Model, error)
```
- GET with `Authorization: Bearer <key>`, `HTTP-Referer`, `X-Title` (same as `Complete`, [client.go:79-82](../internal/ai/client.go)).
- Unmarshal into a private struct mirroring the schema (string prices), then map → `[]models.Model`:
  - `strconv.ParseFloat(prompt, 64) * 1e6` → `PromptPrice` (same for completion).
  - `SupportsVision = slices.Contains(arch.InputModalities, "image")`.
  - `IsFree = pricing.Prompt == "0" && pricing.Completion == "0"`.
- **In-memory cache** on `Client`: add `cachedModels []models.Model`, `cachedAt time.Time`, `mu sync.Mutex`; re-fetch only if empty or older than ~1h (300+ models; avoid re-pulling on every Settings open).
- Import note: `ai` will import `internal/models`. No cycle — `internal/models` imports only `time` ([session.go:3](../internal/models/session.go)).

### 3. `app.go` — bind `ListAvailableModels`
```go
func (a *App) ListAvailableModels() ([]models.Model, error) {
    if a.aiClient == nil {
        return nil, fmt.Errorf("set an OpenRouter API key first")
    }
    return a.aiClient.ListModels(a.ctx)
}
```
Add a `// Models` section near Preferences. **Saving the choice needs NO new binding** — the picker writes `Preferences.Model` through the existing `UpdatePreferences` ([app.go:300](../app.go)).

### 4. Regenerate bindings + export
- `wails generate module` → adds `ListAvailableModels` + `models.Model` to `frontend/wailsjs/`.
- Add `ListAvailableModels` to the App re-export in [frontend/src/lib/wailsBridge.ts](../frontend/src/lib/wailsBridge.ts) (`models` is already re-exported).

## Frontend (React)

### 5. New — `frontend/src/components/ModelPicker.tsx` (+ `ModelPicker.css`)
Searchable combobox. Props:
```ts
interface Props {
  currentModelId: string;
  onSelect: (modelId: string) => void;  // parent persists via UpdatePreferences
}
```
- On mount: `ListAvailableModels()` → state; loading + error states (Wails call no-ops in browser preview — wrap in try/catch).
- **Filters (client-side):** search box (match `name`/`id`); toggle **"Vision only"** (default **ON** — screen-driven); toggle **"Free only"** (default OFF).
- **Sort:** free first, then by name; pin the currently-selected model to the top.
- **Each row:** name, `id` (mono), context length, `$prompt / $completion per 1M`, and badges — 🟢 **Free**, 👁 **Vision**. When "Vision only" is on, hide non-vision rows.
- Selecting a row → `onSelect(model.id)`; highlight the active one.
- Style with MD3 tokens (mirror [Settings.css](../frontend/src/components/Settings.css) / [SetupPage.css](../frontend/src/components/SetupPage.css)): `--surface-container`, `--outline-variant`, selected = `--primary`, Free badge = `--secondary`, mono via `--font-mono`.

### 6. `frontend/src/components/Settings.tsx` — add a "Model" section
Mirror the existing save pattern exactly (`saveInterval`, [Settings.tsx:40-56](../frontend/src/components/Settings.tsx)):
- Settings already loads `prefs` and has `setPrefs`.
- Render `<ModelPicker currentModelId={prefs?.model ?? ""} onSelect={saveModel} />` (place above "Capture interval").
- `saveModel(modelId)`:
  ```ts
  const updated = new models.Preferences({ ...prefs, model: modelId });
  await UpdatePreferences(updated);
  setPrefs(updated);
  setSuccess("Model saved.");
  ```

### 7. Optional — surface the active model
Show `prefs.model` as a read-only chip on the idle hub ([HubReady.tsx](../frontend/src/components/HubReady.tsx), near the "Display 1 · full display" chip) and/or the active session bar. Clicking opens Settings. Mark optional; Settings-only is fine for v1.

## Files touched (summary)
- **New:** `internal/models/model.go`, `frontend/src/components/ModelPicker.tsx`, `frontend/src/components/ModelPicker.css`.
- **Edit:** `internal/ai/client.go` (+`ListModels` + cache), `app.go` (+`ListAvailableModels`), `frontend/src/lib/wailsBridge.ts` (export), `frontend/src/components/Settings.tsx` (+Model section).
- **Generated:** `frontend/wailsjs/**` (`wails generate module`).
- **Docs:** tick Phase 3 "Model picker" in `CLAUDE.md`.
- **Optional cleanup:** delete now-unused `internal/models/problem.go` (flagged dead in CLAUDE.md).

## Verification (next session)
1. `go build ./...` compiles.
2. `wails generate module`, then `cd frontend && npx tsc --noEmit` — bindings + types clean.
3. **Browser preview** (preview tool, `npm run dev`): stub `window.go.main.App.ListAvailableModels` to return a vision/free/paid mix and stub `UpdatePreferences`; drive to Settings → open the picker → verify render, search, the Vision-only/Free-only toggles, sort, and that selecting calls `UpdatePreferences` with the chosen `model`. (List + persistence are browser-verifiable; only the live fetch needs desktop.)
4. **Desktop** (`wails dev`, real key): Settings → pick a **free vision** model → start session → send a message → confirm **no 402** and a sensible reply. Reopen the app → confirm the choice persisted (SQLite `keyModel`).

## Open decisions (defaults chosen; change while building if desired)
- **Vision-only default ON** — recommended (screen-driven app; non-vision models can't read the screenshot).
- **Picker placement** — inline expanding searchable list inside Settings (300+ models need search), not a plain `<select>`.
- **Model on hub** — Settings-only for v1; the hub chip is a later nice-to-have.
