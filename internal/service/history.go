package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"mogi/internal/ai"
	"mogi/internal/models"
)

// HistoryStore is the slice of the data layer the history service needs.
// *store.DB satisfies it.
type HistoryStore interface {
	ListSessions() ([]models.SessionSummary, error)
	GetMessages(sessionID string) ([]models.Message, error)
	DeleteSession(id string) error
	GetSession(id string) (models.Session, error)
	GetSessionFinalCode(id string) (string, error)
	GetSessionDebrief(id string) (string, error)
	SaveSessionDebrief(id, debrief string) error
}

// History is the past-sessions service: listing, reading transcripts, deleting
// (with an active-session guard), and the lazily generated, cached debrief
// scorecard.
type History struct {
	store     HistoryStore
	providers *Providers
	// activeID reports the running session's id ("" when idle) so Delete can
	// refuse it — injected as a func to avoid coupling History to Interview.
	activeID func() string
}

// NewHistory wires the past-sessions service.
func NewHistory(store HistoryStore, providers *Providers, activeID func() string) *History {
	return &History{store: store, providers: providers, activeID: activeID}
}

// List returns summaries of all past sessions.
func (h *History) List() ([]models.SessionSummary, error) {
	return h.store.ListSessions()
}

// Transcript returns the full message history for a session.
func (h *History) Transcript(id string) ([]models.Message, error) {
	return h.store.GetMessages(id)
}

// Delete permanently removes a past session and its transcript. The active
// session can't be deleted — it must be ended first.
func (h *History) Delete(id string) error {
	if active := h.activeID(); active != "" && active == id {
		return fmt.Errorf("cannot delete the active session — end it first")
	}
	return h.store.DeleteSession(id)
}

// Debrief returns the post-interview feedback scorecard for a finished session.
// It is generated lazily and cached: if a debrief was already produced it is
// read straight from SQLite (zero tokens); otherwise it is generated once from
// the transcript plus the captured final code, using the session's own model,
// then persisted. Requires a configured AI client.
func (h *History) Debrief(id string) (models.Debrief, error) {
	aiClient := h.providers.AI()
	if aiClient == nil {
		return models.Debrief{}, fmt.Errorf("debrief: no AI provider configured — add an OpenRouter key in Settings")
	}

	// 1. Cached? Return it without spending any tokens.
	if cached, err := h.store.GetSessionDebrief(id); err == nil && cached != "" {
		var d models.Debrief
		if jsonErr := json.Unmarshal([]byte(cached), &d); jsonErr == nil {
			return d, nil
		}
		// A corrupt cache shouldn't block a fresh generation; fall through.
	}

	// 2. Gather the inputs: the session (for its model), the transcript, and the
	//    captured final code.
	sess, err := h.store.GetSession(id)
	if err != nil {
		return models.Debrief{}, err
	}
	msgs, err := h.store.GetMessages(id)
	if err != nil {
		return models.Debrief{}, err
	}
	if len(msgs) < 2 {
		return models.Debrief{}, fmt.Errorf("debrief: this session is too short to assess")
	}
	finalCode, err := h.store.GetSessionFinalCode(id)
	if err != nil {
		return models.Debrief{}, err
	}

	// 3. Generate (one text call), with a fresh bounded context — the Wails
	//    request context may already be done by the time this runs.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	debrief, err := aiClient.GenerateDebrief(ctx, sess.Model, ai.BuildTranscript(msgs), finalCode)
	if err != nil {
		return models.Debrief{}, err
	}

	// 4. Cache it (best-effort — a failed write just means we regenerate next time).
	if raw, mErr := json.Marshal(debrief); mErr == nil {
		if sErr := h.store.SaveSessionDebrief(id, string(raw)); sErr != nil {
			log.Printf("debrief: persist for %s: %v", id, sErr)
		}
	}
	return debrief, nil
}
