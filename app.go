package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"ai-interviewer/internal/ai"
	"ai-interviewer/internal/capture"
	"ai-interviewer/internal/hotkey"
	"ai-interviewer/internal/models"
	"ai-interviewer/internal/problems"
	"ai-interviewer/internal/service"
	"ai-interviewer/internal/store"
	"ai-interviewer/internal/updater"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// activeSession holds the in-memory state for a running interview.
// Not exported — lives only in the Go process while a session is active.
type activeSession struct {
	session models.Session
	history []ai.ChatMessage
}

// App is the main application struct. Its exported methods are bound to the
// frontend via Wails and callable as async TypeScript functions.
type App struct {
	ctx       context.Context
	db        *store.DB
	capturer  *capture.Capturer
	providers *service.Providers // live API clients (OpenRouter + voice), swapped on key changes
	voice     *service.Voice     // speech service: STT/TTS resolution + audio conversion
	hotkey    *hotkey.Listener   // global push-to-talk keyboard hook
	active    *activeSession
}

// NewApp initialises the application: opens the database, creates the screen
// capturer, and restores the AI client from a persisted API key (if any).
func NewApp() (*App, error) {
	db, err := store.Open()
	if err != nil {
		return nil, fmt.Errorf("app: open database: %w", err)
	}

	providers := service.NewProviders()
	app := &App{
		db:        db,
		capturer:  capture.NewCapturer(),
		hotkey:    hotkey.New(),
		providers: providers,
		voice:     service.NewVoice(db, providers),
	}

	// Restore each provider's client from its persisted API key (if any).
	for _, provider := range []string{"openrouter", "elevenlabs", "google"} {
		if key, err := db.GetAPIKey(provider); err != nil {
			log.Printf("warning: could not read %s key: %v", provider, err)
		} else if key != "" {
			app.providers.SetKey(provider, key)
		}
	}

	// Restore the saved capture region so on-demand captures honour it too.
	app.applySavedRegion()

	return app, nil
}

// applySavedRegion loads the persisted capture display/region and applies it to
// the capturer. Best-effort: falls back to full primary display on any error.
func (a *App) applySavedRegion() {
	prefs, err := a.db.GetPreferences()
	if err != nil {
		return
	}
	a.capturer.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
}

// startup is called by Wails when the application is ready.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.startHotkeyFromPrefs()
}

// shutdown is called by Wails when the application is closing.
func (a *App) shutdown(ctx context.Context) {
	a.hotkey.Shutdown()
	a.capturer.Stop()
	if err := a.db.Close(); err != nil {
		log.Printf("warning: closing database: %v", err)
	}
}

// startHotkeyFromPrefs applies the saved push-to-talk preferences to the global
// hook. The hook starts on first enable and is never restarted — enabling,
// disabling, and rebinding all flow through Apply, which swaps guarded fields on
// the running hook. Best-effort — a bad/empty key falls back to the default.
func (a *App) startHotkeyFromPrefs() {
	prefs, err := a.db.GetPreferences()
	if err != nil {
		return
	}
	spec, perr := hotkey.ParseSpec(prefs.PushToTalkKey)
	if perr != nil {
		spec, _ = hotkey.ParseSpec(hotkey.DefaultSpec)
	}
	a.hotkey.Apply(a.ctx, prefs.PushToTalkEnabled, spec)
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// SetAPIKey stores an API key for the given provider ("openrouter",
// "elevenlabs", or "google") and activates it immediately. No restart required.
func (a *App) SetAPIKey(provider, key string) error {
	if err := a.db.SetAPIKey(provider, key); err != nil {
		return err
	}
	a.providers.SetKey(provider, key)
	return nil
}

// DeleteAPIKey removes the stored key for the given provider ("openrouter",
// "elevenlabs", or "google") and deactivates its client immediately, so STT/TTS
// provider resolution falls back to whatever remains configured. No restart needed.
func (a *App) DeleteAPIKey(provider string) error {
	if err := a.db.DeleteAPIKey(provider); err != nil {
		return err
	}
	a.providers.SetKey(provider, "") // empty key deactivates the slot
	return nil
}

// GetAuthStatus reports which API providers currently have keys configured.
func (a *App) GetAuthStatus() models.AuthStatus {
	dbKey, _ := a.db.GetAPIKey("openrouter")
	elKey, _ := a.db.GetAPIKey("elevenlabs")
	googleKey, _ := a.db.GetAPIKey("google")
	return models.AuthStatus{
		OpenRouterConfigured: dbKey != "",
		ElevenLabsConfigured: elKey != "",
		GoogleConfigured:     googleKey != "",
	}
}

// ---------------------------------------------------------------------------
// Interview session
// ---------------------------------------------------------------------------

// StartSession creates a new screen-driven interview session, initialises the
// conversation history with the system prompt, and starts screen capture. There
// is no problem to select — the AI reads the task from the captured screen.
func (a *App) StartSession(model string) (models.Session, error) {
	if a.active != nil {
		return models.Session{}, fmt.Errorf("a session is already active — end it first")
	}

	prefs, _ := a.db.GetPreferences()
	if model == "" {
		model = prefs.Model
	}

	id := uuid.New().String()
	// problem_id is unused in the screen-driven flow; "" satisfies NOT NULL.
	session, err := a.db.CreateSession(id, "", model)
	if err != nil {
		return models.Session{}, err
	}

	a.active = &activeSession{
		session: session,
		history: []ai.ChatMessage{
			{Role: "system", Content: ai.BuildSystemPrompt()},
		},
	}

	// Apply the saved region, then auto-start screen capture.
	a.capturer.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
	a.capturer.Start(a.ctx, prefs.CaptureIntervalMs)

	return session, nil
}

// EndSession stops the current interview and persists the end timestamp. It also
// kicks off best-effort background extraction of a problem title/difficulty for
// the history list (see extractSessionMeta).
func (a *App) EndSession(sessionID string) error {
	a.capturer.Stop()

	// Grab the final frame now, synchronously. Latest() survives Stop(), but a
	// later StartSession would overwrite it — so capture the reference here before
	// returning and hand it to the background extraction.
	finalShot := a.capturer.Latest()

	if err := a.db.EndSession(sessionID); err != nil {
		return err
	}

	// Capture the session's model before clearing in-memory state — it's needed
	// for the extraction call below and isn't otherwise recoverable here.
	model := ""
	if a.active != nil {
		model = a.active.session.Model
	}
	a.active = nil

	// Label the session and snapshot its final code in the background so ending
	// stays instant. Snapshot the client here so the goroutine shares no mutable
	// state — a key deleted mid-extraction just finishes on the old client.
	if aiClient := a.providers.AI(); aiClient != nil {
		go a.extractSessionMeta(aiClient, sessionID, model, finalShot)
	}
	return nil
}

// extractSessionMeta asks the AI for a short problem title, difficulty, and a text
// snapshot of the candidate's final code (read from the final screenshot) for a
// finished session, and persists them for the history list and later debrief.
// Best-effort: every failure is logged and swallowed so it never affects the
// interview. Runs in its own goroutine with a fresh context (the request context
// may already be done).
func (a *App) extractSessionMeta(aiClient service.AI, sessionID, model, screenshot string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if model == "" {
		// EndSession had no in-memory session; fall back to the default model.
		if prefs, err := a.db.GetPreferences(); err == nil {
			model = prefs.Model
		}
		if model == "" {
			return
		}
	}

	msgs, err := a.db.GetMessages(sessionID)
	if err != nil {
		log.Printf("history: load transcript for labeling: %v", err)
		return
	}
	if len(msgs) < 2 {
		return // too short to label meaningfully
	}

	meta, err := aiClient.ExtractSessionMeta(ctx, model, buildTranscript(msgs), screenshot)
	if err != nil {
		log.Printf("history: extract session meta: %v", err)
		return
	}

	// Company sessions seed the title/difficulty at start from the known problem,
	// so keep any non-empty existing values and let the AI only fill what's
	// missing. The final-code snapshot is always taken from this end-of-session
	// call. Default (screen-driven) sessions have no seeded values, so this leaves
	// their behaviour unchanged.
	title, difficulty := meta.Title, meta.Difficulty
	if existing, err := a.db.GetSession(sessionID); err == nil {
		if existing.ProblemTitle != "" {
			title = existing.ProblemTitle
		}
		if existing.Difficulty != "" {
			difficulty = existing.Difficulty
		}
	}
	if title == "" && difficulty == "" && meta.Code == "" {
		return // nothing useful to store
	}
	if err := a.db.UpdateSessionMeta(sessionID, title, difficulty, meta.Code); err != nil {
		log.Printf("history: persist session meta: %v", err)
	}
}

// buildTranscript renders stored messages as a plain "Speaker: text" transcript
// for problem-metadata extraction.
func buildTranscript(msgs []models.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		speaker := "Candidate"
		if m.Role == "assistant" {
			speaker = "Interviewer"
		}
		b.WriteString(speaker)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// SendMessage is the core interview loop. It captures a screenshot, sends the
// user's text plus the screenshot to OpenRouter, persists both turns, and
// returns the interviewer's response.
func (a *App) SendMessage(text string) (string, error) {
	if a.active == nil {
		return "", fmt.Errorf("no active session — start an interview first")
	}
	aiClient := a.providers.AI()
	if aiClient == nil {
		return "", fmt.Errorf("OpenRouter API key not configured — add it in Settings")
	}

	// Backend enforcement of the session time limit (the frontend also enforces
	// this, so this is a backstop for edge cases like clock skew).
	prefs, _ := a.db.GetPreferences()
	if prefs.SessionLimitMinutes > 0 {
		if time.Since(a.active.session.StartedAt) >= time.Duration(prefs.SessionLimitMinutes)*time.Minute {
			return "", fmt.Errorf("session time limit reached — end the interview and start a new one")
		}
	}

	// 1. Grab the latest screenshot (may be empty on first call).
	screenshot := a.capturer.Latest()

	// 2. Build and record the user message.
	userMsg := ai.BuildUserMessage(text, screenshot)
	a.active.history = append(a.active.history, userMsg)

	now := time.Now().UTC()
	if err := a.db.AddMessage(models.Message{
		ID:        uuid.New().String(),
		SessionID: a.active.session.ID,
		Role:      "user",
		Content:   text,
		HasImage:  screenshot != "",
		CreatedAt: now,
	}); err != nil {
		return "", fmt.Errorf("save user message: %w", err)
	}

	// 3. Call OpenRouter.
	response, err := aiClient.Complete(a.ctx, a.active.session.Model, a.active.history)
	if err != nil {
		return "", fmt.Errorf("AI request failed: %w", err)
	}

	// 4. Record the assistant message.
	assistantMsg := ai.ChatMessage{Role: "assistant", Content: response}
	a.active.history = append(a.active.history, assistantMsg)

	if err := a.db.AddMessage(models.Message{
		ID:        uuid.New().String(),
		SessionID: a.active.session.ID,
		Role:      "assistant",
		Content:   response,
		HasImage:  false,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return "", fmt.Errorf("save assistant message: %w", err)
	}

	return response, nil
}

// ---------------------------------------------------------------------------
// Company Practice
// ---------------------------------------------------------------------------

// ListCompanies returns every company's pool summary for the picker list.
func (a *App) ListCompanies() []models.CompanyInfo {
	return problems.Companies()
}

// ListCompanyProblems returns a company's full problem list for browse-and-pick.
// Filtering and sorting happen client-side over this list.
func (a *App) ListCompanyProblems(slug string) ([]models.Problem, error) {
	return problems.Problems(slug)
}

// StartCompanySession starts a single-problem Company Practice interview. The
// problem is assigned by reference only (title + difficulty + link) — never its
// statement, preserving the screen-driven invariant. The interviewer greets the
// candidate in character (returned as Opening) and the normal capture + Socratic
// loop takes over, flavoured by the company's style profile.
func (a *App) StartCompanySession(slug string, problem models.Problem) (models.CompanySessionStart, error) {
	return a.startCompanyInterview(slug, []models.Problem{problem})
}

// StartMockInterview starts a two-problem mock interview for a company. The pair
// is drawn server-side (easier Q1, harder Q2) so no picker UI ever sees the
// questions before the session begins — the surprise is the practice.
func (a *App) StartMockInterview(slug string) (models.CompanySessionStart, error) {
	pair, err := problems.MockPair(slug)
	if err != nil {
		return models.CompanySessionStart{}, err
	}
	return a.startCompanyInterview(slug, pair[:])
}

// startCompanyInterview is the shared body behind StartCompanySession and
// StartMockInterview. It mirrors StartSession (guard, create the row, start
// capture) but: seeds the company system prompt; persists the deterministic
// opener to the transcript (so history + debrief include it) WITHOUT adding it to
// model history — the system prompt carries the assignment and history must stay
// system → user → … for models that reject a leading assistant turn; and seeds
// the session's title/difficulty from the known problem(s).
func (a *App) startCompanyInterview(slug string, probs []models.Problem) (models.CompanySessionStart, error) {
	if a.active != nil {
		return models.CompanySessionStart{}, fmt.Errorf("a session is already active — end it first")
	}
	if len(probs) == 0 {
		return models.CompanySessionStart{}, fmt.Errorf("no problem to assign")
	}

	companyName := problems.DisplayName(slug)
	profile := ai.CompanyProfile(slug)

	// Choose the opener and the seeded history label from the problem count:
	// one problem is a single-question session, two is a mock interview.
	opening := ai.CompanyOpening(companyName, probs[0])
	metaTitle, metaDifficulty := probs[0].Title, probs[0].Difficulty
	mode := "single"
	if len(probs) >= 2 {
		opening = ai.MockOpening(companyName, probs[0])
		metaTitle, metaDifficulty = fmt.Sprintf("Mock: %s + %s", probs[0].Title, probs[1].Title), ""
		mode = "mock"
	}

	prefs, _ := a.db.GetPreferences()
	model := prefs.Model

	// problem_id (unused in the screen-driven flow) carries the company slug here
	// as a harmless breadcrumb; the label lives in problem_title/difficulty.
	id := uuid.New().String()
	session, err := a.db.CreateSession(id, slug, model)
	if err != nil {
		return models.CompanySessionStart{}, err
	}

	a.active = &activeSession{
		session: session,
		history: []ai.ChatMessage{
			{Role: "system", Content: ai.BuildCompanySystemPrompt(companyName, profile, probs)},
		},
	}

	// Persist the opener to the transcript only (not model history).
	if err := a.db.AddMessage(models.Message{
		ID:        uuid.New().String(),
		SessionID: id,
		Role:      "assistant",
		Content:   opening,
		HasImage:  false,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		log.Printf("company: persist opener: %v", err)
	}

	// Seed the session label so history shows it without an AI guess;
	// extractSessionMeta later preserves these and only fills the final code.
	if err := a.db.UpdateSessionMeta(id, metaTitle, metaDifficulty, ""); err != nil {
		log.Printf("company: seed session meta: %v", err)
	}

	// Tag the session with the company + mode so history can badge it.
	if err := a.db.SetSessionCompany(id, companyName, mode); err != nil {
		log.Printf("company: tag session company: %v", err)
	}

	// Apply the saved region, then auto-start screen capture (same as StartSession).
	a.capturer.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
	a.capturer.Start(a.ctx, prefs.CaptureIntervalMs)

	return models.CompanySessionStart{
		Session:  session,
		Company:  companyName,
		Opening:  opening,
		Problems: probs,
	}, nil
}

// OpenURL opens a URL in the user's real browser (not the frameless webview), so
// "Open on LeetCode" lands in Chrome/Safari rather than inside the overlay window.
func (a *App) OpenURL(url string) error {
	if url == "" {
		return fmt.Errorf("no URL to open")
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// ---------------------------------------------------------------------------
// Screen capture
// ---------------------------------------------------------------------------

// StartCapture begins periodic screen capture at the given interval (ms).
func (a *App) StartCapture(intervalMs int) error {
	if intervalMs <= 0 {
		intervalMs = 3000
	}
	a.capturer.Start(a.ctx, intervalMs)
	return nil
}

// StopCapture halts periodic screen capture.
func (a *App) StopCapture() error {
	a.capturer.Stop()
	return nil
}

// GetLatestScreenshot returns the most recent screenshot as a base64 PNG.
func (a *App) GetLatestScreenshot() (string, error) {
	s := a.capturer.Latest()
	if s == "" {
		return "", fmt.Errorf("no screenshot captured yet")
	}
	return s, nil
}

// ListDisplays enumerates the active displays for the region picker.
func (a *App) ListDisplays() []capture.DisplayInfo {
	return capture.ListDisplays()
}

// snapshotHideDelay is how long we wait after hiding our window before grabbing
// the screen, giving the compositor time to repaint the desktop without us.
const snapshotHideDelay = 200 * time.Millisecond

// SnapshotDisplay returns a full (uncropped) screenshot of the given display as
// a base64 PNG. Used by the region selector so the user can draw a rectangle.
//
// It hides our own window for the grab (snipping-tool behaviour) so the snapshot
// shows the desktop behind the app — the user's IDE/browser — instead of the app
// covering it.
func (a *App) SnapshotDisplay(displayIndex int) (string, error) {
	runtime.WindowHide(a.ctx)
	defer runtime.WindowShow(a.ctx) // restore even if the capture fails
	time.Sleep(snapshotHideDelay)
	return capture.SnapshotDisplay(displayIndex)
}

// SetCaptureRegion persists the chosen display and sub-region (fractions 0..1 of
// the display; a zero width means full display) and applies it to the capturer.
func (a *App) SetCaptureRegion(displayIndex int, x, y, w, h float64) error {
	prefs, err := a.db.GetPreferences()
	if err != nil {
		return err
	}
	prefs.CaptureDisplay = displayIndex
	prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH = x, y, w, h
	if err := a.db.SavePreferences(prefs); err != nil {
		return err
	}
	a.capturer.SetRegion(displayIndex, x, y, w, h)
	return nil
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// ListSessions returns summaries of all past sessions.
func (a *App) ListSessions() ([]models.SessionSummary, error) {
	return a.db.ListSessions()
}

// GetSessionTranscript returns the full message history for a session.
func (a *App) GetSessionTranscript(id string) ([]models.Message, error) {
	return a.db.GetMessages(id)
}

// DeleteSession permanently removes a past session and its transcript. The active
// session can't be deleted — it must be ended first.
func (a *App) DeleteSession(id string) error {
	if a.active != nil && a.active.session.ID == id {
		return fmt.Errorf("cannot delete the active session — end it first")
	}
	return a.db.DeleteSession(id)
}

// GetDebrief returns the post-interview feedback scorecard for a finished session.
// It is generated lazily and cached: if a debrief was already produced it is read
// straight from SQLite (zero tokens); otherwise it is generated once from the
// transcript plus the captured final code, using the session's own model, then
// persisted. Requires a configured AI client.
func (a *App) GetDebrief(id string) (models.Debrief, error) {
	aiClient := a.providers.AI()
	if aiClient == nil {
		return models.Debrief{}, fmt.Errorf("debrief: no AI provider configured — add an OpenRouter key in Settings")
	}

	// 1. Cached? Return it without spending any tokens.
	if cached, err := a.db.GetSessionDebrief(id); err == nil && cached != "" {
		var d models.Debrief
		if jsonErr := json.Unmarshal([]byte(cached), &d); jsonErr == nil {
			return d, nil
		}
		// A corrupt cache shouldn't block a fresh generation; fall through.
	}

	// 2. Gather the inputs: the session (for its model), the transcript, and the
	//    captured final code.
	sess, err := a.db.GetSession(id)
	if err != nil {
		return models.Debrief{}, err
	}
	msgs, err := a.db.GetMessages(id)
	if err != nil {
		return models.Debrief{}, err
	}
	if len(msgs) < 2 {
		return models.Debrief{}, fmt.Errorf("debrief: this session is too short to assess")
	}
	finalCode, err := a.db.GetSessionFinalCode(id)
	if err != nil {
		return models.Debrief{}, err
	}

	// 3. Generate (one text call), with a fresh bounded context.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	debrief, err := aiClient.GenerateDebrief(ctx, sess.Model, buildTranscript(msgs), finalCode)
	if err != nil {
		return models.Debrief{}, err
	}

	// 4. Cache it (best-effort — a failed write just means we regenerate next time).
	if raw, mErr := json.Marshal(debrief); mErr == nil {
		if sErr := a.db.SaveSessionDebrief(id, string(raw)); sErr != nil {
			log.Printf("debrief: persist for %s: %v", id, sErr)
		}
	}
	return debrief, nil
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

// GetPreferences returns the user's settings.
func (a *App) GetPreferences() (models.Preferences, error) {
	return a.db.GetPreferences()
}

// UpdatePreferences persists updated settings.
func (a *App) UpdatePreferences(prefs models.Preferences) error {
	if err := a.db.SavePreferences(prefs); err != nil {
		return err
	}
	// Keep the capturer in sync with any region/display change.
	a.capturer.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
	// Enable/disable/re-key the global push-to-talk hook to match the new prefs.
	a.startHotkeyFromPrefs()
	return nil
}

// GetHotkeyStatus reports the global push-to-talk hook state so the UI can
// surface the macOS Input-Monitoring permission hint when it isn't running.
func (a *App) GetHotkeyStatus() hotkey.Status {
	return a.hotkey.Status()
}

// OpenInputMonitoringSettings opens macOS System Settings at the Input
// Monitoring pane, where the user grants the permission the global hotkey needs.
func (a *App) OpenInputMonitoringSettings() {
	runtime.BrowserOpenURL(a.ctx, "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent")
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// ListAvailableModels returns the OpenRouter model catalog for the Settings
// picker. Saving a choice needs no binding here — the picker writes the selected
// id to Preferences.Model through UpdatePreferences.
func (a *App) ListAvailableModels() ([]models.Model, error) {
	aiClient := a.providers.AI()
	if aiClient == nil {
		return nil, fmt.Errorf("set an OpenRouter API key first")
	}
	return aiClient.ListModels(a.ctx)
}

// ---------------------------------------------------------------------------
// Updates
// ---------------------------------------------------------------------------

// GetAppVersion returns the running app's version (e.g. "v0.1.0"), or "dev" for
// local builds. Injected at build time via -ldflags; see main.go and
// docs/ci-cd-and-auto-update.md.
func (a *App) GetAppVersion() string {
	return version
}

// CheckForUpdate asks GitHub whether a newer release than the running build
// exists, so the frontend can surface a download prompt. Dev builds always
// report no update. The error is returned so the caller can fail silently
// (a failed check should never disrupt the app).
func (a *App) CheckForUpdate() (models.UpdateInfo, error) {
	return updater.Check(a.ctx, version)
}

// OpenReleasePage opens a release URL (the GitHub release page or its .zip
// asset) in the user's default browser so they can download an update. The app
// is unsigned and does not self-replace — installation is manual.
func (a *App) OpenReleasePage(url string) error {
	if url == "" {
		return fmt.Errorf("no download URL available")
	}
	runtime.BrowserOpenURL(a.ctx, url)
	return nil
}

// ---------------------------------------------------------------------------
// Voice (delegated to the service layer)
// ---------------------------------------------------------------------------

// TranscribeAudio converts recorded mic audio (base64 WAV, optionally a data URI)
// into text via the active STT provider (ElevenLabs Scribe if configured, else
// Google). The frontend feeds the result into the normal SendMessage loop, so
// voice and typed input share one path.
func (a *App) TranscribeAudio(audioBase64, mimeType string) (string, error) {
	return a.voice.Transcribe(a.ctx, audioBase64, mimeType)
}

// SynthesizeSpeech converts interviewer text into spoken audio via the active
// TTS provider, returned as base64 MP3 for the frontend to play.
func (a *App) SynthesizeSpeech(text string) (string, error) {
	return a.voice.Synthesize(a.ctx, text)
}

// ListVoices returns the active provider's available voices for the Settings
// picker. Saving a choice needs no binding here — the picker writes the selected
// id to Preferences (VoiceID or GoogleVoiceID) through UpdatePreferences.
func (a *App) ListVoices() ([]models.Voice, error) {
	return a.voice.Voices(a.ctx)
}

// PreviewVoice synthesizes a short sample with the given voice (or the active
// provider's default) and returns it as base64 MP3, for the picker's preview
// button.
func (a *App) PreviewVoice(voiceID string) (string, error) {
	return a.voice.Preview(a.ctx, voiceID)
}

// ---------------------------------------------------------------------------
// Window / Overlay mode
// ---------------------------------------------------------------------------

// Overlay (compact, always-on-top) window dimensions, in logical pixels.
const (
	overlayWidth  = 780 // floating bar width
	overlayBarH   = 76  // just the bar
	overlayFullH  = 400 // bar + expanded history dropdown
	restoreWidth  = 1024
	restoreHeight = 768
	overlayTopGap = 24 // distance from the top of the screen
)

// EnterOverlayMode shrinks the window to the floating bar, pins it
// always-on-top, and parks it at the top-centre of the screen so it hovers
// over the user's IDE during an interview.
func (a *App) EnterOverlayMode() {
	runtime.WindowSetAlwaysOnTop(a.ctx, true)
	runtime.WindowSetSize(a.ctx, overlayWidth, overlayBarH)
	a.positionOverlayTopCenter()
}

// ExitOverlayMode restores the full window size and unpins it.
func (a *App) ExitOverlayMode() {
	runtime.WindowSetAlwaysOnTop(a.ctx, false)
	runtime.WindowSetSize(a.ctx, restoreWidth, restoreHeight)
	runtime.WindowCenter(a.ctx)
}

// SetOverlayExpanded grows the overlay window so the history dropdown has room
// (expanded) or collapses it back to just the bar.
func (a *App) SetOverlayExpanded(expanded bool) {
	h := overlayBarH
	if expanded {
		h = overlayFullH
	}
	runtime.WindowSetSize(a.ctx, overlayWidth, h)
	a.positionOverlayTopCenter()
}

// positionOverlayTopCenter centres the window horizontally near the top of the
// current screen.
func (a *App) positionOverlayTopCenter() {
	screens, err := runtime.ScreenGetAll(a.ctx)
	if err != nil || len(screens) == 0 {
		return
	}
	width := screens[0].Size.Width
	for _, s := range screens {
		if s.IsCurrent {
			width = s.Size.Width
			break
		}
	}
	x := (width - overlayWidth) / 2
	if x < 0 {
		x = 0
	}
	runtime.WindowSetPosition(a.ctx, x, overlayTopGap)
}

// The window is frameless (so the overlay can float over the IDE), which removes
// the native titlebar buttons — the UI draws its own and calls these.

// MinimiseWindow minimises the app window to the dock/taskbar.
func (a *App) MinimiseWindow() {
	runtime.WindowMinimise(a.ctx)
}

// ToggleMaximiseWindow toggles the window between maximised and its normal size.
func (a *App) ToggleMaximiseWindow() {
	runtime.WindowToggleMaximise(a.ctx)
}

// QuitApp exits the application, running the normal shutdown (stops the hotkey
// and capturer, closes the database).
func (a *App) QuitApp() {
	runtime.Quit(a.ctx)
}
