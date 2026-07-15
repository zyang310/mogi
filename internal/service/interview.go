package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"mogi/internal/ai"
	"mogi/internal/models"
	"mogi/internal/problems"

	"github.com/google/uuid"
)

// Screen is the capture-control surface the interview service needs.
// *capture.Capturer satisfies it; tests use a fake so nothing touches the real
// display or spawns a capture loop.
type Screen interface {
	SetRegion(displayIndex int, x, y, w, h float64)
	Start(ctx context.Context, intervalMs int)
	Stop()
	Latest() string
}

// InterviewStore is the slice of the data layer the interview service needs.
// *store.DB satisfies it.
type InterviewStore interface {
	GetPreferences() (models.Preferences, error)
	GetManagedPinnedModel() (string, error)
	CreateSession(id, problemID, model string) (models.Session, error)
	EndSession(id string) error
	AddMessage(msg models.Message) error
	GetMessages(sessionID string) ([]models.Message, error)
	GetSession(id string) (models.Session, error)
	UpdateSessionMeta(id, title, difficulty, finalCode string) error
	SetSessionCompany(id, company, mode string) error
}

// activeSession holds the in-memory state for a running interview.
// Not exported — lives only in the Go process while a session is active.
type activeSession struct {
	session models.Session
	history []ai.ChatMessage
}

// Interview is the live-session service: it owns the active session (there is
// at most one), the send loop that pairs the user's text with the latest
// screenshot, the Company Practice starts, and the best-effort post-session
// metadata extraction.
type Interview struct {
	store     InterviewStore
	providers *Providers
	screen    Screen

	// mu guards active. Wails dispatches each frontend call on its own
	// goroutine, so Start/Send/End can genuinely race. The lock is never held
	// across the AI network call — a multi-second completion must not block
	// ending the session.
	mu     sync.Mutex
	active *activeSession
}

// NewInterview wires the live-session service to its store, the live client
// registry, and the screen capturer.
func NewInterview(store InterviewStore, providers *Providers, screen Screen) *Interview {
	return &Interview{store: store, providers: providers, screen: screen}
}

// ActiveID returns the running session's id, or "" when none is active. Used
// by the history service to refuse deleting the active session.
func (s *Interview) ActiveID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return ""
	}
	return s.active.session.ID
}

// resolveModel decides which model a new session runs on. In managed mode the
// server-pinned model always wins — the tester can't pick, and the developer can
// swap the whole cohort's model server-side with no app release. In BYOK mode an
// explicit request wins, falling back to the user's saved default. A managed
// account with an (unexpectedly) empty pin degrades to the same BYOK behaviour
// rather than an empty model.
func (s *Interview) resolveModel(requested string, prefs models.Preferences) string {
	if prefs.KeyMode == "managed" {
		if pinned, _ := s.store.GetManagedPinnedModel(); pinned != "" {
			return pinned
		}
	}
	if requested != "" {
		return requested
	}
	return prefs.Model
}

// Start creates a new screen-driven interview session, initialises the
// conversation history with the system prompt, and starts screen capture.
// There is no problem to select — the AI reads the task from the captured
// screen. ctx must be the app-lifetime (Wails) context: it parents the capture
// goroutine, so a request-scoped context would kill capture when it ends.
func (s *Interview) Start(ctx context.Context, model string) (models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != nil {
		return models.Session{}, fmt.Errorf("a session is already active — end it first")
	}

	prefs, _ := s.store.GetPreferences()
	model = s.resolveModel(model, prefs)

	id := uuid.New().String()
	// problem_id is unused in the screen-driven flow; "" satisfies NOT NULL.
	session, err := s.store.CreateSession(id, "", model)
	if err != nil {
		return models.Session{}, err
	}

	s.active = &activeSession{
		session: session,
		history: []ai.ChatMessage{
			{Role: "system", Content: ai.BuildSystemPrompt()},
		},
	}

	// Apply the saved region, then auto-start screen capture.
	s.screen.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
	s.screen.Start(ctx, prefs.CaptureIntervalMs)

	return session, nil
}

// End stops the current interview and persists the end timestamp. It also
// kicks off best-effort background extraction of a problem title/difficulty
// for the history list (see extractSessionMeta).
func (s *Interview) End(sessionID string) error {
	s.screen.Stop()

	// Grab the final frame now, synchronously. Latest() survives Stop(), but a
	// later Start would overwrite it — so capture the reference here before
	// returning and hand it to the background extraction.
	finalShot := s.screen.Latest()

	if err := s.store.EndSession(sessionID); err != nil {
		return err
	}

	// Capture the session's model before clearing in-memory state — it's needed
	// for the extraction call below and isn't otherwise recoverable here.
	s.mu.Lock()
	model := ""
	if s.active != nil {
		model = s.active.session.Model
	}
	s.active = nil
	s.mu.Unlock()

	// Label the session and snapshot its final code in the background so ending
	// stays instant. The client is snapshotted here so the goroutine shares no
	// mutable state — a key deleted mid-extraction just finishes on the old
	// client.
	if aiClient := s.providers.AI(); aiClient != nil {
		go s.extractSessionMeta(aiClient, sessionID, model, finalShot)
	}
	return nil
}

// Send is the core interview loop. It captures a screenshot, sends the user's
// text plus the screenshot to OpenRouter, persists both turns, and returns the
// interviewer's response.
func (s *Interview) Send(ctx context.Context, text string) (string, error) {
	aiClient := s.providers.AI()

	s.mu.Lock()
	if s.active == nil {
		s.mu.Unlock()
		return "", fmt.Errorf("no active session — start an interview first")
	}
	if aiClient == nil {
		s.mu.Unlock()
		return "", fmt.Errorf("OpenRouter API key not configured — add it in Settings")
	}

	// Backend enforcement of the session time limit (the frontend also enforces
	// this, so this is a backstop for edge cases like clock skew).
	prefs, _ := s.store.GetPreferences()
	if prefs.SessionLimitMinutes > 0 {
		if time.Since(s.active.session.StartedAt) >= time.Duration(prefs.SessionLimitMinutes)*time.Minute {
			s.mu.Unlock()
			return "", fmt.Errorf("session time limit reached — end the interview and start a new one")
		}
	}

	// 1. Grab the latest screenshot (may be empty on first call).
	screenshot := s.screen.Latest()

	// 2. Build and record the user message.
	userMsg := ai.BuildUserMessage(text, screenshot)
	s.active.history = append(s.active.history, userMsg)

	// Snapshot everything the AI call needs, then release the lock — appends
	// only ever extend the slice, so this view stays stable while a concurrent
	// Send/End mutates the session.
	sessionID := s.active.session.ID
	model := s.active.session.Model
	history := s.active.history

	now := time.Now().UTC()
	if err := s.store.AddMessage(models.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      "user",
		Content:   text,
		HasImage:  screenshot != "",
		CreatedAt: now,
	}); err != nil {
		s.mu.Unlock()
		return "", fmt.Errorf("save user message: %w", err)
	}
	s.mu.Unlock()

	// 3. Call OpenRouter — deliberately outside the lock so a slow completion
	// never blocks ending the session.
	response, err := aiClient.Complete(ctx, model, history)
	if err != nil {
		return "", fmt.Errorf("AI request failed: %w", err)
	}

	// 4. Record the assistant message. If the session ended (or a new one
	// started) while the request was in flight, skip the in-memory append —
	// the transcript write below still preserves the reply.
	s.mu.Lock()
	if s.active != nil && s.active.session.ID == sessionID {
		s.active.history = append(s.active.history, ai.ChatMessage{Role: "assistant", Content: response})
	}
	s.mu.Unlock()

	if err := s.store.AddMessage(models.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      "assistant",
		Content:   response,
		HasImage:  false,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		return "", fmt.Errorf("save assistant message: %w", err)
	}

	return response, nil
}

// StartCompany starts a single-problem Company Practice interview. The problem
// is assigned by reference only (title + difficulty + link) — never its
// statement, preserving the screen-driven invariant. The interviewer greets the
// candidate in character (returned as Opening) and the normal capture +
// Socratic loop takes over, flavoured by the company's style profile.
func (s *Interview) StartCompany(ctx context.Context, slug string, problem models.Problem) (models.CompanySessionStart, error) {
	return s.startCompanyInterview(ctx, slug, []models.Problem{problem})
}

// StartMock starts a two-problem mock interview for a company. The pair is
// drawn server-side (easier Q1, harder Q2) so no picker UI ever sees the
// questions before the session begins — the surprise is the practice.
func (s *Interview) StartMock(ctx context.Context, slug string) (models.CompanySessionStart, error) {
	pair, err := problems.MockPair(slug)
	if err != nil {
		return models.CompanySessionStart{}, err
	}
	return s.startCompanyInterview(ctx, slug, pair[:])
}

// startCompanyInterview is the shared body behind StartCompany and StartMock.
// It mirrors Start (guard, create the row, start capture) but: seeds the
// company system prompt; persists the deterministic opener to the transcript
// (so history + debrief include it) WITHOUT adding it to model history — the
// system prompt carries the assignment and history must stay system → user → …
// for models that reject a leading assistant turn; and seeds the session's
// title/difficulty from the known problem(s).
func (s *Interview) startCompanyInterview(ctx context.Context, slug string, probs []models.Problem) (models.CompanySessionStart, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.active != nil {
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

	prefs, _ := s.store.GetPreferences()
	// Company sessions take no requested model — resolveModel pins it in managed
	// mode, else falls back to the saved default.
	model := s.resolveModel("", prefs)

	// problem_id (unused in the screen-driven flow) carries the company slug here
	// as a harmless breadcrumb; the label lives in problem_title/difficulty.
	id := uuid.New().String()
	session, err := s.store.CreateSession(id, slug, model)
	if err != nil {
		return models.CompanySessionStart{}, err
	}

	s.active = &activeSession{
		session: session,
		history: []ai.ChatMessage{
			{Role: "system", Content: ai.BuildCompanySystemPrompt(companyName, profile, probs)},
		},
	}

	// Persist the opener to the transcript only (not model history).
	if err := s.store.AddMessage(models.Message{
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
	if err := s.store.UpdateSessionMeta(id, metaTitle, metaDifficulty, ""); err != nil {
		log.Printf("company: seed session meta: %v", err)
	}

	// Tag the session with the company + mode so history can badge it.
	if err := s.store.SetSessionCompany(id, companyName, mode); err != nil {
		log.Printf("company: tag session company: %v", err)
	}

	// Apply the saved region, then auto-start screen capture (same as Start).
	s.screen.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
	s.screen.Start(ctx, prefs.CaptureIntervalMs)

	return models.CompanySessionStart{
		Session:  session,
		Company:  companyName,
		Opening:  opening,
		Problems: probs,
	}, nil
}

// extractSessionMeta asks the AI for a short problem title, difficulty, and a
// text snapshot of the candidate's final code (read from the final screenshot)
// for a finished session, and persists them for the history list and later
// debrief. Best-effort: every failure is logged and swallowed so it never
// affects the interview. Runs in its own goroutine with a fresh context (the
// request context may already be done).
func (s *Interview) extractSessionMeta(aiClient AI, sessionID, model, screenshot string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if model == "" {
		// End had no in-memory session; fall back to the resolved model (the
		// pinned model in managed mode, else the saved default).
		if prefs, err := s.store.GetPreferences(); err == nil {
			model = s.resolveModel("", prefs)
		}
		if model == "" {
			return
		}
	}

	msgs, err := s.store.GetMessages(sessionID)
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
	if existing, err := s.store.GetSession(sessionID); err == nil {
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
	if err := s.store.UpdateSessionMeta(sessionID, title, difficulty, meta.Code); err != nil {
		log.Printf("history: persist session meta: %v", err)
	}
}

// buildTranscript renders stored messages as a plain "Speaker: text" transcript
// for problem-metadata extraction and debrief generation.
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
