package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"ai-interviewer/internal/ai"
	"ai-interviewer/internal/models"
)

// interviewWith builds an Interview service over fakes, seeding the registry's
// AI slot directly (in-package access, same rationale as voiceWith).
func interviewWith(st *fakeStore, aiClient AI, screen *fakeScreen) *Interview {
	p := NewProviders()
	p.ai = aiClient
	return NewInterview(st, p, screen)
}

// TestStartGuardsDoubleStart verifies at most one session runs at a time and
// that starting applies the saved region and interval to the capturer.
func TestStartGuardsDoubleStart(t *testing.T) {
	st := &fakeStore{getPreferences: func() (models.Preferences, error) {
		return models.Preferences{Model: "m1", CaptureDisplay: 2, RegionX: 0.1, RegionW: 0.5, CaptureIntervalMs: 1234}, nil
	}}
	screen := &fakeScreen{}
	s := interviewWith(st, &fakeAI{}, screen)

	sess, err := s.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if sess.Model != "m1" {
		t.Errorf("Start(\"\") model = %q, want the preferences default", sess.Model)
	}
	if s.ActiveID() != sess.ID {
		t.Errorf("ActiveID() = %q, want %q", s.ActiveID(), sess.ID)
	}
	if len(screen.starts) != 1 || screen.starts[0] != 1234 {
		t.Errorf("screen starts = %v, want one start at the preferences interval", screen.starts)
	}
	if len(screen.regions) != 1 || screen.regions[0] != [5]float64{2, 0.1, 0, 0.5, 0} {
		t.Errorf("screen regions = %v, want the saved region applied", screen.regions)
	}

	if _, err := s.Start(context.Background(), "m2"); err == nil {
		t.Error("second Start() should fail while a session is active")
	}
}

// TestSendGuards covers the two precondition errors: no active session, and no
// AI provider configured.
func TestSendGuards(t *testing.T) {
	s := interviewWith(&fakeStore{}, &fakeAI{}, &fakeScreen{})
	if _, err := s.Send(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "no active session") {
		t.Errorf("Send() without a session: err = %v, want no-active-session error", err)
	}

	s = interviewWith(&fakeStore{}, nil, &fakeScreen{})
	if _, err := s.Start(context.Background(), "m"); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if _, err := s.Send(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "OpenRouter API key") {
		t.Errorf("Send() without an AI key: err = %v, want key-not-configured error", err)
	}
}

// TestSendTimeLimitBackstop verifies the backend refuses turns once the session
// exceeds the configured limit (the frontend enforces it too; this is the
// backstop).
func TestSendTimeLimitBackstop(t *testing.T) {
	st := &fakeStore{
		getPreferences: func() (models.Preferences, error) {
			return models.Preferences{SessionLimitMinutes: 1}, nil
		},
		createSession: func(id, problemID, model string) (models.Session, error) {
			return models.Session{ID: id, Model: model, StartedAt: time.Now().Add(-2 * time.Minute)}, nil
		},
	}
	aiCalled := false
	s := interviewWith(st, &fakeAI{complete: func(string, []ai.ChatMessage) (string, error) {
		aiCalled = true
		return "reply", nil
	}}, &fakeScreen{})

	if _, err := s.Start(context.Background(), "m"); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if _, err := s.Send(context.Background(), "hi"); err == nil || !strings.Contains(err.Error(), "time limit") {
		t.Errorf("Send() past the limit: err = %v, want time-limit error", err)
	}
	if aiCalled {
		t.Error("Send() past the limit must not call the AI")
	}
}

// TestSendHappyPath walks one full turn: screenshot attached, both turns
// persisted, model history grown, reply returned.
func TestSendHappyPath(t *testing.T) {
	var persisted []models.Message
	st := &fakeStore{addMessage: func(m models.Message) error {
		persisted = append(persisted, m)
		return nil
	}}
	var gotModel string
	var gotMsgs []ai.ChatMessage
	aiClient := &fakeAI{complete: func(model string, msgs []ai.ChatMessage) (string, error) {
		gotModel, gotMsgs = model, msgs
		return "what's the complexity?", nil
	}}
	screen := &fakeScreen{latest: "shot-b64"}
	s := interviewWith(st, aiClient, screen)

	if _, err := s.Start(context.Background(), "claude-x"); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	reply, err := s.Send(context.Background(), "I'd use a hashmap")
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if reply != "what's the complexity?" {
		t.Errorf("Send() = %q, want the AI reply", reply)
	}

	if gotModel != "claude-x" {
		t.Errorf("AI called with model %q, want the session's model", gotModel)
	}
	if len(gotMsgs) != 2 || gotMsgs[0].Role != "system" || gotMsgs[1].Role != "user" {
		t.Fatalf("AI called with %d messages, want system + user", len(gotMsgs))
	}

	if len(persisted) != 2 {
		t.Fatalf("persisted %d messages, want user + assistant", len(persisted))
	}
	if persisted[0].Role != "user" || persisted[0].Content != "I'd use a hashmap" || !persisted[0].HasImage {
		t.Errorf("user turn persisted wrong: %+v (HasImage must reflect the attached screenshot)", persisted[0])
	}
	if persisted[1].Role != "assistant" || persisted[1].Content != reply || persisted[1].HasImage {
		t.Errorf("assistant turn persisted wrong: %+v", persisted[1])
	}

	// In-memory model history now holds system + user + assistant.
	if got := len(s.active.history); got != 3 {
		t.Errorf("history length = %d, want 3", got)
	}
}

// TestSendSurvivesEndDuringFlight ends the session while the AI request is in
// flight: the reply must still be persisted to the transcript, but the
// in-memory session stays gone (no resurrection).
func TestSendSurvivesEndDuringFlight(t *testing.T) {
	var persisted []models.Message
	st := &fakeStore{addMessage: func(m models.Message) error {
		persisted = append(persisted, m)
		return nil
	}}
	screen := &fakeScreen{}

	var s *Interview
	var sessID string
	aiClient := &fakeAI{complete: func(string, []ai.ChatMessage) (string, error) {
		// Send released its lock for the network call, so ending mid-flight
		// must neither deadlock nor lose the reply.
		if err := s.End(sessID); err != nil {
			t.Errorf("End() during in-flight Send: %v", err)
		}
		return "late reply", nil
	}}
	s = interviewWith(st, aiClient, screen)

	sess, err := s.Start(context.Background(), "m")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	sessID = sess.ID

	reply, err := s.Send(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Send() error: %v", err)
	}
	if reply != "late reply" {
		t.Errorf("Send() = %q, want the reply despite the ended session", reply)
	}
	if s.ActiveID() != "" {
		t.Error("session must stay ended — the in-flight reply must not resurrect it")
	}
	last := persisted[len(persisted)-1]
	if last.Role != "assistant" || last.Content != "late reply" {
		t.Errorf("assistant turn not persisted after racing End: %+v", last)
	}
}

// TestEndStopsAndClears verifies End's contract: capture stopped, end timestamp
// persisted, in-memory state cleared.
func TestEndStopsAndClears(t *testing.T) {
	ended := ""
	st := &fakeStore{endSession: func(id string) error {
		ended = id
		return nil
	}}
	screen := &fakeScreen{}
	s := interviewWith(st, nil, screen) // nil AI: no extraction goroutine spawns

	sess, err := s.Start(context.Background(), "m")
	if err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if err := s.End(sess.ID); err != nil {
		t.Fatalf("End() error: %v", err)
	}
	if ended != sess.ID {
		t.Errorf("store.EndSession got %q, want %q", ended, sess.ID)
	}
	if screen.stops != 1 {
		t.Errorf("screen stops = %d, want 1", screen.stops)
	}
	if s.ActiveID() != "" {
		t.Error("ActiveID() should be empty after End")
	}
}

// TestExtractSessionMeta calls the unexported extraction directly (no goroutine,
// no flakiness) and covers: preserving company-seeded labels, filling unseeded
// ones, and the early returns for short transcripts and useless extractions.
func TestExtractSessionMeta(t *testing.T) {
	twoMsgs := []models.Message{{Role: "user", Content: "q"}, {Role: "assistant", Content: "a"}}

	t.Run("preserves seeded title and difficulty", func(t *testing.T) {
		var updated []string
		st := &fakeStore{
			getMessages: func(string) ([]models.Message, error) { return twoMsgs, nil },
			getSession: func(id string) (models.Session, error) {
				return models.Session{ID: id, ProblemTitle: "Two Sum", Difficulty: "Easy"}, nil
			},
			updateSessionMeta: func(id, title, difficulty, finalCode string) error {
				updated = []string{id, title, difficulty, finalCode}
				return nil
			},
		}
		aiClient := &fakeAI{extractMeta: func(string, string, string) (ai.SessionMeta, error) {
			return ai.SessionMeta{Title: "AI Guess", Difficulty: "Hard", Code: "def solve(): ..."}, nil
		}}
		s := interviewWith(st, aiClient, &fakeScreen{})

		s.extractSessionMeta(aiClient, "sid", "m", "shot")
		want := []string{"sid", "Two Sum", "Easy", "def solve(): ..."}
		if len(updated) != 4 || updated[0] != want[0] || updated[1] != want[1] || updated[2] != want[2] || updated[3] != want[3] {
			t.Errorf("UpdateSessionMeta got %v, want %v (seeded labels kept, code filled)", updated, want)
		}
	})

	t.Run("fills unseeded labels from the AI", func(t *testing.T) {
		var gotTitle, gotDifficulty string
		st := &fakeStore{
			getMessages: func(string) ([]models.Message, error) { return twoMsgs, nil },
			getSession:  func(id string) (models.Session, error) { return models.Session{ID: id}, nil },
			updateSessionMeta: func(_, title, difficulty, _ string) error {
				gotTitle, gotDifficulty = title, difficulty
				return nil
			},
		}
		aiClient := &fakeAI{extractMeta: func(string, string, string) (ai.SessionMeta, error) {
			return ai.SessionMeta{Title: "Median of Two Sorted Arrays", Difficulty: "Hard"}, nil
		}}
		interviewWith(st, aiClient, &fakeScreen{}).extractSessionMeta(aiClient, "sid", "m", "shot")
		if gotTitle != "Median of Two Sorted Arrays" || gotDifficulty != "Hard" {
			t.Errorf("got %q/%q, want the AI labels", gotTitle, gotDifficulty)
		}
	})

	t.Run("skips short transcripts without calling the AI", func(t *testing.T) {
		aiCalled, updated := false, false
		st := &fakeStore{
			getMessages: func(string) ([]models.Message, error) {
				return []models.Message{{Role: "user", Content: "q"}}, nil
			},
			updateSessionMeta: func(_, _, _, _ string) error { updated = true; return nil },
		}
		aiClient := &fakeAI{extractMeta: func(string, string, string) (ai.SessionMeta, error) {
			aiCalled = true
			return ai.SessionMeta{}, nil
		}}
		interviewWith(st, aiClient, &fakeScreen{}).extractSessionMeta(aiClient, "sid", "m", "shot")
		if aiCalled || updated {
			t.Errorf("short transcript: aiCalled=%v updated=%v, want neither", aiCalled, updated)
		}
	})

	t.Run("stores nothing when the extraction is empty", func(t *testing.T) {
		updated := false
		st := &fakeStore{
			getMessages:       func(string) ([]models.Message, error) { return twoMsgs, nil },
			updateSessionMeta: func(_, _, _, _ string) error { updated = true; return nil },
		}
		aiClient := &fakeAI{} // returns a zero SessionMeta
		interviewWith(st, aiClient, &fakeScreen{}).extractSessionMeta(aiClient, "sid", "m", "shot")
		if updated {
			t.Error("empty extraction must not write session meta")
		}
	})

	t.Run("gives up without a model", func(t *testing.T) {
		aiCalled := false
		aiClient := &fakeAI{extractMeta: func(string, string, string) (ai.SessionMeta, error) {
			aiCalled = true
			return ai.SessionMeta{}, nil
		}}
		// No in-memory model and no preferences default → nothing to call with.
		interviewWith(&fakeStore{}, aiClient, &fakeScreen{}).extractSessionMeta(aiClient, "sid", "", "shot")
		if aiCalled {
			t.Error("no model available: the AI must not be called")
		}
	})
}

// TestStartCompanyInterview drives the shared company-start body with fixed
// problems (no random draw) and checks the single vs mock split: opener choice,
// seeded label, mode tag, and the opener living in the transcript but not in
// model history.
func TestStartCompanyInterview(t *testing.T) {
	q1 := models.Problem{Title: "Two Sum", Difficulty: "Easy"}
	q2 := models.Problem{Title: "LRU Cache", Difficulty: "Medium"}

	type recorded struct {
		openers []models.Message
		meta    []string
		company []string
	}
	newStore := func(rec *recorded) *fakeStore {
		return &fakeStore{
			getPreferences: func() (models.Preferences, error) {
				return models.Preferences{Model: "m1", CaptureIntervalMs: 500}, nil
			},
			addMessage: func(m models.Message) error {
				rec.openers = append(rec.openers, m)
				return nil
			},
			updateSessionMeta: func(_, title, difficulty, code string) error {
				rec.meta = []string{title, difficulty, code}
				return nil
			},
			setSessionCompany: func(_, company, mode string) error {
				rec.company = []string{company, mode}
				return nil
			},
		}
	}

	t.Run("single problem", func(t *testing.T) {
		rec := &recorded{}
		screen := &fakeScreen{}
		s := interviewWith(newStore(rec), &fakeAI{}, screen)

		start, err := s.startCompanyInterview(context.Background(), "google", []models.Problem{q1})
		if err != nil {
			t.Fatalf("startCompanyInterview() error: %v", err)
		}
		if !strings.Contains(start.Opening, q1.Title) {
			t.Errorf("Opening %q should name the assigned problem", start.Opening)
		}
		if len(rec.openers) != 1 || rec.openers[0].Role != "assistant" || rec.openers[0].Content != start.Opening {
			t.Errorf("opener must be persisted to the transcript as an assistant turn: %+v", rec.openers)
		}
		// The opener must NOT enter model history — it stays system-only so
		// providers that reject a leading assistant turn keep working.
		if len(s.active.history) != 1 || s.active.history[0].Role != "system" {
			t.Errorf("model history = %+v, want exactly one system message", s.active.history)
		}
		if rec.meta[0] != q1.Title || rec.meta[1] != q1.Difficulty || rec.meta[2] != "" {
			t.Errorf("seeded meta = %v, want the problem's title/difficulty", rec.meta)
		}
		if rec.company[1] != "single" {
			t.Errorf("mode = %q, want single", rec.company[1])
		}
		if len(screen.starts) != 1 || screen.starts[0] != 500 {
			t.Errorf("capture starts = %v, want the preferences interval", screen.starts)
		}
	})

	t.Run("mock pair", func(t *testing.T) {
		rec := &recorded{}
		s := interviewWith(newStore(rec), &fakeAI{}, &fakeScreen{})

		start, err := s.startCompanyInterview(context.Background(), "google", []models.Problem{q1, q2})
		if err != nil {
			t.Fatalf("startCompanyInterview() error: %v", err)
		}
		// The opener names only Q1 — Q2 stays a surprise.
		if !strings.Contains(start.Opening, q1.Title) || strings.Contains(start.Opening, q2.Title) {
			t.Errorf("mock opening %q must name Q1 and hide Q2", start.Opening)
		}
		if want := "Mock: Two Sum + LRU Cache"; rec.meta[0] != want || rec.meta[1] != "" {
			t.Errorf("seeded meta = %v, want title %q with no difficulty", rec.meta, want)
		}
		if rec.company[1] != "mock" {
			t.Errorf("mode = %q, want mock", rec.company[1])
		}
	})

	t.Run("guards", func(t *testing.T) {
		s := interviewWith(newStore(&recorded{}), &fakeAI{}, &fakeScreen{})
		if _, err := s.startCompanyInterview(context.Background(), "google", nil); err == nil {
			t.Error("no problems: should error")
		}
		if _, err := s.startCompanyInterview(context.Background(), "google", []models.Problem{q1}); err != nil {
			t.Fatalf("first start errored: %v", err)
		}
		if _, err := s.startCompanyInterview(context.Background(), "google", []models.Problem{q2}); err == nil {
			t.Error("second start while active: should error")
		}
	})
}
