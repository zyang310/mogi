package service

import (
	"fmt"
	"strings"

	"mogi/internal/models"

	"github.com/google/uuid"
)

// SetsStore is the slice of the data layer the question-set service needs.
// *store.DB satisfies it.
type SetsStore interface {
	ListQuestionSets(companySlug string) ([]models.QuestionSet, error)
	GetQuestionSet(id string) (models.QuestionSet, error)
	SaveQuestionSet(set models.QuestionSet) error
	DeleteQuestionSet(id string) error
}

const (
	// maxSetQuestions caps a set well above any sane curation — a set is a
	// focused subset, not a second copy of the pool.
	maxSetQuestions = 200
	// maxSetNameLen keeps set names presentable in cards and session labels.
	maxSetNameLen = 80
)

// Sets owns custom question-set CRUD and its validation rules. The questions
// are Problem snapshots picked from a company's built-in pool; the set-scoped
// mock draw lives on the Interview service (StartSetMock).
type Sets struct {
	store SetsStore
}

// NewSets wires the question-set service to its store.
func NewSets(store SetsStore) *Sets {
	return &Sets{store: store}
}

// List returns a company's custom question sets, name-ordered.
func (s *Sets) List(companySlug string) ([]models.QuestionSet, error) {
	if strings.TrimSpace(companySlug) == "" {
		return nil, fmt.Errorf("company is required")
	}
	return s.store.ListQuestionSets(companySlug)
}

// Save validates and upserts a set, assigning a UUID on first save, and returns
// the stored set (authoritative ID + timestamps). Validation errors are
// user-readable — they surface verbatim in the editor UI.
func (s *Sets) Save(set models.QuestionSet) (models.QuestionSet, error) {
	set.Name = strings.TrimSpace(set.Name)
	if set.Name == "" {
		return models.QuestionSet{}, fmt.Errorf("set name is required")
	}
	if len(set.Name) > maxSetNameLen {
		return models.QuestionSet{}, fmt.Errorf("set name is too long (max %d characters)", maxSetNameLen)
	}
	if strings.TrimSpace(set.CompanySlug) == "" {
		return models.QuestionSet{}, fmt.Errorf("set has no company")
	}
	// Deliberately no membership check against the embedded dataset: a set must
	// stay editable even if its company slug drops out of a future data refresh —
	// the stored snapshots are the source of truth.

	deduped := make([]models.Problem, 0, len(set.Questions))
	seen := make(map[string]bool, len(set.Questions))
	for _, q := range set.Questions {
		if strings.TrimSpace(q.URL) == "" || strings.TrimSpace(q.Title) == "" {
			return models.QuestionSet{}, fmt.Errorf("set contains an invalid question (missing title or link)")
		}
		if seen[q.URL] { // the URL is the identity key; repeats add nothing
			continue
		}
		seen[q.URL] = true
		deduped = append(deduped, q)
	}
	if len(deduped) == 0 {
		return models.QuestionSet{}, fmt.Errorf("a set needs at least one question")
	}
	if len(deduped) > maxSetQuestions {
		return models.QuestionSet{}, fmt.Errorf("a set can hold at most %d questions", maxSetQuestions)
	}
	set.Questions = deduped

	if set.ID == "" {
		set.ID = uuid.New().String()
	}
	if err := s.store.SaveQuestionSet(set); err != nil {
		return models.QuestionSet{}, err
	}
	// Re-read so the caller gets the stored row's timestamps, not our input.
	return s.store.GetQuestionSet(set.ID)
}

// Delete removes a set by id. Sessions started from the set are untouched —
// they are tagged with the company, not the set.
func (s *Sets) Delete(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("set id is required")
	}
	return s.store.DeleteQuestionSet(id)
}
