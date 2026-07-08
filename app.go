package main

import (
	"context"
	"fmt"
	"log"

	"mogi/internal/capture"
	"mogi/internal/hotkey"
	"mogi/internal/models"
	"mogi/internal/problems"
	"mogi/internal/service"
	"mogi/internal/store"
	"mogi/internal/updater"
)

// App is the Wails binding facade. Its exported methods are bound to the
// frontend and callable as async TypeScript functions; their bodies are thin —
// wiring lives in NewApp, business logic lives in internal/service, and
// anything driving the Wails runtime lives in window.go.
type App struct {
	ctx      context.Context
	db       *store.DB         // kept concrete for shutdown's Close
	capturer *capture.Capturer // kept concrete for shutdown + the thin capture bindings
	hotkey   *hotkey.Listener  // kept concrete for shutdown + GetHotkeyStatus

	interview *service.Interview // live-session service: session state, send loop, company starts
	history   *service.History   // past-sessions service: list/read/delete + debrief
	voice     *service.Voice     // speech service: STT/TTS resolution + audio conversion
	settings  *service.Settings  // keys + preferences, incl. capturer/hotkey propagation

	winZoom zoomState // custom green-button window zoom toggle (see window.go)

	overlayGuardStop chan struct{} // stops the overlay on-screen guard (see window.go)
}

// NewApp initialises the application: opens the database, creates the screen
// capturer, and restores the AI client from a persisted API key (if any).
func NewApp() (*App, error) {
	db, err := store.Open()
	if err != nil {
		return nil, fmt.Errorf("app: open database: %w", err)
	}

	// Wire the service layer: the registry decides which API clients are live;
	// each service sees only the narrow store/infra interfaces it needs.
	capturer := capture.NewCapturer()
	hook := hotkey.New()
	providers := service.NewProviders()
	interview := service.NewInterview(db, providers, capturer)
	app := &App{
		db:        db,
		capturer:  capturer,
		hotkey:    hook,
		interview: interview,
		history:   service.NewHistory(db, providers, interview.ActiveID),
		voice:     service.NewVoice(db, providers),
		settings:  service.NewSettings(db, providers, capturer, hook),
	}

	// Restore each provider's client from its persisted API key (if any).
	for _, provider := range []string{"openrouter", "elevenlabs", "google"} {
		if key, err := db.GetAPIKey(provider); err != nil {
			log.Printf("warning: could not read %s key: %v", provider, err)
		} else if key != "" {
			providers.SetKey(provider, key)
		}
	}

	// Restore the saved capture region so on-demand captures honour it too.
	app.settings.ApplySavedRegion()

	return app, nil
}

// startup is called by Wails when the application is ready.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.settings.ApplyHotkey(ctx)
}

// shutdown is called by Wails when the application is closing.
func (a *App) shutdown(ctx context.Context) {
	a.stopOverlayGuard()
	a.hotkey.Shutdown()
	a.capturer.Stop()
	if err := a.db.Close(); err != nil {
		log.Printf("warning: closing database: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// SetAPIKey stores an API key for the given provider ("openrouter",
// "elevenlabs", or "google") and activates it immediately. No restart required.
func (a *App) SetAPIKey(provider, key string) error {
	return a.settings.SetAPIKey(provider, key)
}

// DeleteAPIKey removes the stored key for the given provider ("openrouter",
// "elevenlabs", or "google") and deactivates its client immediately, so STT/TTS
// provider resolution falls back to whatever remains configured. No restart needed.
func (a *App) DeleteAPIKey(provider string) error {
	return a.settings.DeleteAPIKey(provider)
}

// GetAuthStatus reports which API providers currently have keys configured.
func (a *App) GetAuthStatus() models.AuthStatus {
	return a.settings.AuthStatus()
}

// ---------------------------------------------------------------------------
// Interview session
// ---------------------------------------------------------------------------

// StartSession creates a new screen-driven interview session and starts screen
// capture. There is no problem to select — the AI reads the task from the
// captured screen.
func (a *App) StartSession(model string) (models.Session, error) {
	return a.interview.Start(a.ctx, model)
}

// EndSession stops the current interview, persists the end timestamp, and
// kicks off best-effort background labeling for the history list.
func (a *App) EndSession(sessionID string) error {
	return a.interview.End(sessionID)
}

// SendMessage is the core interview loop: the user's text plus the latest
// screenshot go to the AI, both turns are persisted, and the interviewer's
// reply is returned.
func (a *App) SendMessage(text string) (string, error) {
	return a.interview.Send(a.ctx, text)
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
	return a.interview.StartCompany(a.ctx, slug, problem)
}

// StartMockInterview starts a two-problem mock interview for a company. The pair
// is drawn server-side (easier Q1, harder Q2) so no picker UI ever sees the
// questions before the session begins — the surprise is the practice.
func (a *App) StartMockInterview(slug string) (models.CompanySessionStart, error) {
	return a.interview.StartMock(a.ctx, slug)
}

// ListStarredCompanies returns the slugs the user starred in the company
// picker, alphabetically.
func (a *App) ListStarredCompanies() ([]string, error) {
	return a.settings.StarredCompanies()
}

// SetCompanyStarred stars (true) or unstars (false) a company in the picker.
// Idempotent.
func (a *App) SetCompanyStarred(slug string, starred bool) error {
	return a.settings.SetCompanyStarred(slug, starred)
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

// SetCaptureRegion persists the chosen display and sub-region (fractions 0..1 of
// the display; a zero width means full display) and applies it to the capturer.
func (a *App) SetCaptureRegion(displayIndex int, x, y, w, h float64) error {
	return a.settings.SetCaptureRegion(displayIndex, x, y, w, h)
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// ListSessions returns summaries of all past sessions.
func (a *App) ListSessions() ([]models.SessionSummary, error) {
	return a.history.List()
}

// GetSessionTranscript returns the full message history for a session.
func (a *App) GetSessionTranscript(id string) ([]models.Message, error) {
	return a.history.Transcript(id)
}

// DeleteSession permanently removes a past session and its transcript. The active
// session can't be deleted — it must be ended first.
func (a *App) DeleteSession(id string) error {
	return a.history.Delete(id)
}

// GetDebrief returns the post-interview feedback scorecard for a finished
// session — generated lazily on first open, then cached in SQLite.
func (a *App) GetDebrief(id string) (models.Debrief, error) {
	return a.history.Debrief(id)
}

// ---------------------------------------------------------------------------
// Preferences
// ---------------------------------------------------------------------------

// GetPreferences returns the user's settings.
func (a *App) GetPreferences() (models.Preferences, error) {
	return a.settings.Preferences()
}

// UpdatePreferences persists updated settings and propagates them to the
// running capturer and global hotkey.
func (a *App) UpdatePreferences(prefs models.Preferences) error {
	return a.settings.Update(a.ctx, prefs)
}

// GetHotkeyStatus reports the global push-to-talk hook state so the UI can
// surface the macOS Input-Monitoring permission hint when it isn't running.
func (a *App) GetHotkeyStatus() hotkey.Status {
	return a.hotkey.Status()
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// ListAvailableModels returns the OpenRouter model catalog for the Settings
// picker. Saving a choice needs no binding here — the picker writes the selected
// id to Preferences.Model through UpdatePreferences.
func (a *App) ListAvailableModels() ([]models.Model, error) {
	return a.settings.ListModels(a.ctx)
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
