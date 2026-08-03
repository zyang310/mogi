package store

import (
	"encoding/json"
	"fmt"
	"time"

	"mogi/internal/models"
)

// ListQuestionSets returns every custom question set for a company, ordered by
// name (case-insensitive), ties broken by creation time. The slice is
// initialized (never nil) so the Wails JSON boundary yields [] rather than null.
func (db *DB) ListQuestionSets(companySlug string) ([]models.QuestionSet, error) {
	rows, err := db.conn.Query(`
		SELECT id, company_slug, name, questions, created_at, updated_at
		FROM question_sets
		WHERE company_slug = ?
		ORDER BY name COLLATE NOCASE ASC, created_at ASC
	`, companySlug)
	if err != nil {
		return nil, fmt.Errorf("store: list question sets: %w", err)
	}
	defer rows.Close()

	out := []models.QuestionSet{}
	for rows.Next() {
		set, err := scanQuestionSet(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("store: scan question set row: %w", err)
		}
		out = append(out, set)
	}
	return out, rows.Err()
}

// GetQuestionSet returns one set by id (sql.ErrNoRows wrapped when absent).
func (db *DB) GetQuestionSet(id string) (models.QuestionSet, error) {
	row := db.conn.QueryRow(`
		SELECT id, company_slug, name, questions, created_at, updated_at
		FROM question_sets WHERE id = ?
	`, id)
	set, err := scanQuestionSet(row.Scan)
	if err != nil {
		return models.QuestionSet{}, fmt.Errorf("store: get question set: %w", err)
	}
	return set, nil
}

// scanQuestionSet maps one question_sets row via the given Scan func — shared
// by the single-row and list readers.
func scanQuestionSet(scan func(dest ...any) error) (models.QuestionSet, error) {
	var set models.QuestionSet
	var questions, createdAt, updatedAt string
	if err := scan(&set.ID, &set.CompanySlug, &set.Name, &questions, &createdAt, &updatedAt); err != nil {
		return models.QuestionSet{}, err
	}
	set.Questions = decodeQuestions(questions)
	set.CreatedAt = parseDBTime(createdAt)
	set.UpdatedAt = parseDBTime(updatedAt)
	return set, nil
}

// decodeQuestions parses the questions JSON column. A corrupt blob degrades to
// an empty list rather than failing the whole read — the user can re-edit or
// delete the set (same policy as the corrupt-debrief cache in ListSessions).
func decodeQuestions(raw string) []models.Problem {
	qs := []models.Problem{}
	if raw == "" {
		return qs
	}
	if err := json.Unmarshal([]byte(raw), &qs); err != nil || qs == nil {
		return []models.Problem{}
	}
	return qs
}

// SaveQuestionSet upserts a set. An insert stamps both timestamps; an update
// refreshes name, questions, and updated_at while preserving created_at and
// company_slug (a set never moves companies). Questions marshal as [] when nil
// so the column never stores null.
func (db *DB) SaveQuestionSet(set models.QuestionSet) error {
	questions := set.Questions
	if questions == nil {
		questions = []models.Problem{}
	}
	blob, err := json.Marshal(questions)
	if err != nil {
		return fmt.Errorf("store: encode question set: %w", err)
	}
	now := time.Now().UTC()
	_, err = db.conn.Exec(`
		INSERT INTO question_sets (id, company_slug, name, questions, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name       = excluded.name,
			questions  = excluded.questions,
			updated_at = excluded.updated_at
	`, set.ID, set.CompanySlug, set.Name, string(blob), now, now)
	if err != nil {
		return fmt.Errorf("store: save question set: %w", err)
	}
	return nil
}

// DeleteQuestionSet removes a set by id. Idempotent — deleting an absent set is
// not an error.
func (db *DB) DeleteQuestionSet(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM question_sets WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete question set: %w", err)
	}
	return nil
}
