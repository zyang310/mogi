package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"strings"
	"time"

	"ai-interviewer/internal/ai"
	"ai-interviewer/internal/capture"
	"ai-interviewer/internal/models"
	"ai-interviewer/internal/store"
	"ai-interviewer/internal/voice"

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
	ctx         context.Context
	db          *store.DB
	capturer    *capture.Capturer
	aiClient    *ai.Client
	voiceClient *voice.Client
	active      *activeSession
}

// NewApp initialises the application: opens the database, creates the screen
// capturer, and restores the AI client from a persisted API key (if any).
func NewApp() (*App, error) {
	db, err := store.Open()
	if err != nil {
		return nil, fmt.Errorf("app: open database: %w", err)
	}

	app := &App{
		db:       db,
		capturer: capture.NewCapturer(),
	}

	// Restore the AI client from the persisted OpenRouter key (if any).
	if key, err := db.GetAPIKey("openrouter"); err != nil {
		log.Printf("warning: could not read OpenRouter key: %v", err)
	} else if key != "" {
		app.aiClient = ai.NewClient(key)
	}

	// Restore the voice client from the persisted ElevenLabs key (if any).
	if key, err := db.GetAPIKey("elevenlabs"); err != nil {
		log.Printf("warning: could not read ElevenLabs key: %v", err)
	} else if key != "" {
		app.voiceClient = voice.NewClient(key)
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
}

// shutdown is called by Wails when the application is closing.
func (a *App) shutdown(ctx context.Context) {
	a.capturer.Stop()
	if err := a.db.Close(); err != nil {
		log.Printf("warning: closing database: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

// SetAPIKey stores an API key for the given provider ("openrouter" or
// "elevenlabs") and activates it immediately. No restart required.
func (a *App) SetAPIKey(provider, key string) error {
	if err := a.db.SetAPIKey(provider, key); err != nil {
		return err
	}
	switch provider {
	case "openrouter":
		a.aiClient = ai.NewClient(key)
	case "elevenlabs":
		a.voiceClient = voice.NewClient(key)
	}
	return nil
}

// GetAuthStatus reports which API providers currently have keys configured.
func (a *App) GetAuthStatus() models.AuthStatus {
	dbKey, _ := a.db.GetAPIKey("openrouter")
	elKey, _ := a.db.GetAPIKey("elevenlabs")
	return models.AuthStatus{
		OpenRouterConfigured: dbKey != "",
		ElevenLabsConfigured: elKey != "",
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

// EndSession stops the current interview and persists the end timestamp.
func (a *App) EndSession(sessionID string) error {
	a.capturer.Stop()

	if err := a.db.EndSession(sessionID); err != nil {
		return err
	}
	a.active = nil
	return nil
}

// SendMessage is the core interview loop. It captures a screenshot, sends the
// user's text plus the screenshot to OpenRouter, persists both turns, and
// returns the interviewer's response.
func (a *App) SendMessage(text string) (string, error) {
	if a.active == nil {
		return "", fmt.Errorf("no active session — start an interview first")
	}
	if a.aiClient == nil {
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
	response, err := a.aiClient.Complete(a.ctx, a.active.session.Model, a.active.history)
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
	return nil
}

// ---------------------------------------------------------------------------
// Models
// ---------------------------------------------------------------------------

// ListAvailableModels returns the OpenRouter model catalog for the Settings
// picker. Saving a choice needs no binding here — the picker writes the selected
// id to Preferences.Model through UpdatePreferences.
func (a *App) ListAvailableModels() ([]models.Model, error) {
	if a.aiClient == nil {
		return nil, fmt.Errorf("set an OpenRouter API key first")
	}
	return a.aiClient.ListModels(a.ctx)
}

// ---------------------------------------------------------------------------
// Voice (ElevenLabs)
// ---------------------------------------------------------------------------

// defaultVoiceID is ElevenLabs' long-stable "Rachel" premade voice, used as a
// fallback so spoken replies work before the user picks a voice in Settings.
const defaultVoiceID = "21m00Tcm4TlvDq8ikWAM"

// TranscribeAudio converts recorded mic audio (base64, optionally a data URI)
// into text via ElevenLabs Scribe. mimeType is the recorder's output type, used
// only to label the upload. The frontend feeds the result into the normal
// SendMessage loop, so voice and typed input share one path.
func (a *App) TranscribeAudio(audioBase64, mimeType string) (string, error) {
	if a.voiceClient == nil {
		return "", fmt.Errorf("ElevenLabs API key not configured — add it in Settings")
	}

	// Accept either a raw base64 string or a full data URI ("data:audio/...;base64,...").
	if i := strings.Index(audioBase64, ","); strings.HasPrefix(audioBase64, "data:") && i != -1 {
		audioBase64 = audioBase64[i+1:]
	}
	audio, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		return "", fmt.Errorf("decode audio: %w", err)
	}

	return a.voiceClient.Transcribe(a.ctx, audio, mimeType)
}

// SynthesizeSpeech converts interviewer text into spoken audio via ElevenLabs
// Flash, using the voice saved in preferences (or the default). It returns the
// MP3 as base64 so it crosses the Wails boundary as a string, matching how
// screenshots are passed; the frontend plays it via the Web Audio API.
func (a *App) SynthesizeSpeech(text string) (string, error) {
	if a.voiceClient == nil {
		return "", fmt.Errorf("ElevenLabs API key not configured — add it in Settings")
	}

	prefs, _ := a.db.GetPreferences()
	voiceID := prefs.VoiceID
	if voiceID == "" {
		voiceID = defaultVoiceID
	}

	audio, err := a.voiceClient.Synthesize(a.ctx, voiceID, text)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(audio), nil
}

// ListVoices returns the account's available ElevenLabs voices for the Settings
// picker. Saving a choice needs no binding here — the picker writes the selected
// id to Preferences.VoiceID through UpdatePreferences.
func (a *App) ListVoices() ([]models.Voice, error) {
	if a.voiceClient == nil {
		return nil, fmt.Errorf("set an ElevenLabs API key first")
	}
	return a.voiceClient.ListVoices(a.ctx)
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
