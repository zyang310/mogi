package store

import (
	"database/sql"
	"fmt"
	"time"

	"ai-interviewer/internal/models"
)

// parseDBTime parses a timestamp written by the SQLite driver, which may be in
// RFC3339 or the "2006-01-02 15:04:05" datetime format. It returns the zero Time
// if neither parses (e.g. for a NULL/empty value).
func parseDBTime(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	t, _ := time.Parse("2006-01-02 15:04:05", s)
	return t
}

// CreateSession inserts a new session row and returns the populated struct.
func (db *DB) CreateSession(id, problemID, model string) (models.Session, error) {
	now := time.Now().UTC()
	_, err := db.conn.Exec(
		`INSERT INTO sessions (id, problem_id, model, started_at) VALUES (?, ?, ?, ?)`,
		id, problemID, model, now,
	)
	if err != nil {
		return models.Session{}, fmt.Errorf("store: create session: %w", err)
	}
	return models.Session{
		ID:        id,
		ProblemID: problemID,
		Model:     model,
		StartedAt: now,
	}, nil
}

// EndSession sets the ended_at timestamp on a session.
func (db *DB) EndSession(id string) error {
	now := time.Now().UTC()
	_, err := db.conn.Exec(`UPDATE sessions SET ended_at = ? WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("store: end session: %w", err)
	}
	return nil
}

// ListSessions returns a summary of all sessions, newest first. EndedAt,
// ProblemTitle, and Difficulty may be unset for in-progress or unlabeled sessions;
// Company and Mode are set only for Company Practice sessions.
func (db *DB) ListSessions() ([]models.SessionSummary, error) {
	rows, err := db.conn.Query(`
		SELECT s.id, s.model, s.started_at, s.ended_at, s.problem_title, s.difficulty,
		       s.company, s.mode, COUNT(m.id) AS msg_count
		FROM sessions s
		LEFT JOIN messages m ON m.session_id = s.id
		GROUP BY s.id
		ORDER BY s.started_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("store: list sessions: %w", err)
	}
	defer rows.Close()

	var out []models.SessionSummary
	for rows.Next() {
		var s models.SessionSummary
		var startedAt string
		var endedAt, problemTitle, difficulty, company, mode sql.NullString
		if err := rows.Scan(&s.ID, &s.Model, &startedAt, &endedAt, &problemTitle, &difficulty, &company, &mode, &s.MessageCount); err != nil {
			return nil, fmt.Errorf("store: scan session row: %w", err)
		}
		s.StartedAt = parseDBTime(startedAt)
		if endedAt.Valid && endedAt.String != "" {
			if t := parseDBTime(endedAt.String); !t.IsZero() {
				s.EndedAt = &t
			}
		}
		s.ProblemTitle = problemTitle.String
		s.Difficulty = difficulty.String
		s.Company = company.String
		s.Mode = mode.String
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetSessionCompany tags a session with the company (display name) and mode
// ("single" or "mock") it belongs to, so the history list can badge Company
// Practice sessions. Called once at session start.
func (db *DB) SetSessionCompany(id, company, mode string) error {
	_, err := db.conn.Exec(
		`UPDATE sessions SET company = ?, mode = ? WHERE id = ?`,
		company, mode, id,
	)
	if err != nil {
		return fmt.Errorf("store: set session company: %w", err)
	}
	return nil
}

// UpdateSessionMeta sets the AI-derived problem title, difficulty, and final code
// snapshot on a session so it can be labeled in the history list and later
// debriefed. Empty strings are stored as-is.
func (db *DB) UpdateSessionMeta(id, title, difficulty, finalCode string) error {
	_, err := db.conn.Exec(
		`UPDATE sessions SET problem_title = ?, difficulty = ?, final_code = ? WHERE id = ?`,
		title, difficulty, finalCode, id,
	)
	if err != nil {
		return fmt.Errorf("store: update session meta: %w", err)
	}
	return nil
}

// GetSession returns a single session row (without the large final_code/debrief
// columns, which have dedicated getters). Used by the debrief flow to recover the
// session's model after the fact.
func (db *DB) GetSession(id string) (models.Session, error) {
	var s models.Session
	var startedAt string
	var endedAt, problemTitle, difficulty sql.NullString
	err := db.conn.QueryRow(
		`SELECT id, problem_id, model, started_at, ended_at, problem_title, difficulty
		 FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.ProblemID, &s.Model, &startedAt, &endedAt, &problemTitle, &difficulty)
	if err != nil {
		return models.Session{}, fmt.Errorf("store: get session: %w", err)
	}
	s.StartedAt = parseDBTime(startedAt)
	if endedAt.Valid && endedAt.String != "" {
		if t := parseDBTime(endedAt.String); !t.IsZero() {
			s.EndedAt = &t
		}
	}
	s.ProblemTitle = problemTitle.String
	s.Difficulty = difficulty.String
	return s, nil
}

// GetSessionFinalCode returns the text snapshot of the candidate's final code,
// captured at session end. Empty when none was captured.
func (db *DB) GetSessionFinalCode(id string) (string, error) {
	var code sql.NullString
	err := db.conn.QueryRow(`SELECT final_code FROM sessions WHERE id = ?`, id).Scan(&code)
	if err != nil {
		return "", fmt.Errorf("store: get session final code: %w", err)
	}
	return code.String, nil
}

// GetSessionDebrief returns the cached post-interview debrief JSON for a session,
// or "" if none has been generated yet.
func (db *DB) GetSessionDebrief(id string) (string, error) {
	var debrief sql.NullString
	err := db.conn.QueryRow(`SELECT debrief FROM sessions WHERE id = ?`, id).Scan(&debrief)
	if err != nil {
		return "", fmt.Errorf("store: get session debrief: %w", err)
	}
	return debrief.String, nil
}

// SaveSessionDebrief caches the generated debrief JSON on a session so re-opening
// it costs no tokens.
func (db *DB) SaveSessionDebrief(id, debrief string) error {
	_, err := db.conn.Exec(`UPDATE sessions SET debrief = ? WHERE id = ?`, debrief, id)
	if err != nil {
		return fmt.Errorf("store: save session debrief: %w", err)
	}
	return nil
}

// DeleteSession removes a session and all of its messages. Messages are deleted
// first because foreign keys are enabled and the messages→sessions constraint has
// no ON DELETE CASCADE; both run in one transaction so a session is never left
// with orphaned messages.
func (db *DB) DeleteSession(id string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("store: begin delete session: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("store: delete session messages: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		tx.Rollback()
		return fmt.Errorf("store: delete session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit delete session: %w", err)
	}
	return nil
}

// AddMessage persists a single conversation turn.
func (db *DB) AddMessage(msg models.Message) error {
	_, err := db.conn.Exec(
		`INSERT INTO messages (id, session_id, role, content, has_image, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.SessionID, msg.Role, msg.Content, msg.HasImage, msg.CreatedAt.UTC(),
	)
	if err != nil {
		return fmt.Errorf("store: add message: %w", err)
	}
	return nil
}

// GetMessages returns all messages for a session in chronological order.
func (db *DB) GetMessages(sessionID string) ([]models.Message, error) {
	rows, err := db.conn.Query(
		`SELECT id, session_id, role, content, has_image, created_at
		 FROM messages WHERE session_id = ? ORDER BY created_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("store: get messages: %w", err)
	}
	defer rows.Close()

	var out []models.Message
	for rows.Next() {
		var m models.Message
		var createdAt string
		var hasImage int
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &hasImage, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan message row: %w", err)
		}
		m.HasImage = hasImage == 1
		m.CreatedAt = parseDBTime(createdAt)
		out = append(out, m)
	}
	return out, rows.Err()
}
