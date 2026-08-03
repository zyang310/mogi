package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"mogi/internal/models"
)

// setQ builds a minimal valid set question for tests.
func setQ(title, url string) models.Problem {
	return models.Problem{Title: title, Difficulty: "Medium", URL: url}
}

// TestSetsSaveValidation walks every rejection rule with a store that must
// never be written to.
func TestSetsSaveValidation(t *testing.T) {
	tooMany := make([]models.Problem, maxSetQuestions+1)
	for i := range tooMany {
		tooMany[i] = setQ(fmt.Sprintf("P%d", i), fmt.Sprintf("https://leetcode.com/problems/p%d", i))
	}

	cases := map[string]models.QuestionSet{
		"empty name": {
			CompanySlug: "google", Name: "   ",
			Questions: []models.Problem{setQ("A", "https://leetcode.com/problems/a")},
		},
		"name too long": {
			CompanySlug: "google", Name: strings.Repeat("x", maxSetNameLen+1),
			Questions: []models.Problem{setQ("A", "https://leetcode.com/problems/a")},
		},
		"missing company": {
			Name:      "Drill",
			Questions: []models.Problem{setQ("A", "https://leetcode.com/problems/a")},
		},
		"no questions": {CompanySlug: "google", Name: "Drill"},
		"question missing url": {
			CompanySlug: "google", Name: "Drill",
			Questions: []models.Problem{{Title: "A", Difficulty: "Easy"}},
		},
		"question missing title": {
			CompanySlug: "google", Name: "Drill",
			Questions: []models.Problem{{URL: "https://leetcode.com/problems/a"}},
		},
		"over the cap": {CompanySlug: "google", Name: "Drill", Questions: tooMany},
	}

	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			saved := false
			svc := NewSets(&fakeStore{
				saveQuestionSet: func(models.QuestionSet) error { saved = true; return nil },
			})
			if _, err := svc.Save(set); err == nil {
				t.Error("Save should error")
			}
			if saved {
				t.Error("an invalid set must never reach the store")
			}
		})
	}
}

// TestSetsSaveHappyPath covers the write path: the name is trimmed, questions
// are deduped by URL preserving order, a UUID is assigned when the id is empty
// (and a provided id survives), and the caller gets the re-read stored set.
func TestSetsSaveHappyPath(t *testing.T) {
	a := setQ("A", "https://leetcode.com/problems/a")
	dupA := setQ("A again", "https://leetcode.com/problems/a") // same URL → dropped
	b := setQ("B", "https://leetcode.com/problems/b")
	storedAt := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	var savedSet models.QuestionSet
	svc := NewSets(&fakeStore{
		saveQuestionSet: func(s models.QuestionSet) error { savedSet = s; return nil },
		getQuestionSet: func(id string) (models.QuestionSet, error) {
			if id != savedSet.ID {
				t.Errorf("re-read id %q, want the saved id %q", id, savedSet.ID)
			}
			out := savedSet
			out.CreatedAt = storedAt // prove the caller gets the stored row
			return out, nil
		},
	})

	got, err := svc.Save(models.QuestionSet{
		CompanySlug: "google",
		Name:        "  Drill  ",
		Questions:   []models.Problem{a, dupA, b},
	})
	if err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	if savedSet.Name != "Drill" {
		t.Errorf("saved name = %q, want it trimmed", savedSet.Name)
	}
	if savedSet.ID == "" {
		t.Error("Save must assign an id when none is provided")
	}
	if len(savedSet.Questions) != 2 || savedSet.Questions[0].Title != "A" || savedSet.Questions[1].Title != "B" {
		t.Errorf("saved questions = %+v, want deduped [A B] in order", savedSet.Questions)
	}
	if !got.CreatedAt.Equal(storedAt) {
		t.Error("Save must return the re-read stored set, not its input")
	}

	// A provided id is an update — it must survive untouched.
	if _, err := svc.Save(models.QuestionSet{
		ID: "keep-me", CompanySlug: "google", Name: "Drill",
		Questions: []models.Problem{a},
	}); err != nil {
		t.Fatalf("update Save() error: %v", err)
	}
	if savedSet.ID != "keep-me" {
		t.Errorf("update saved id = %q, want keep-me", savedSet.ID)
	}
}

// TestSetsGuardsAndErrors pins the argument guards and that store failures
// propagate to the caller.
func TestSetsGuardsAndErrors(t *testing.T) {
	svc := NewSets(&fakeStore{})
	if _, err := svc.List(""); err == nil {
		t.Error(`List("") should error`)
	}
	if err := svc.Delete("  "); err == nil {
		t.Error("Delete(blank) should error")
	}

	boom := errors.New("boom")
	svc = NewSets(&fakeStore{saveQuestionSet: func(models.QuestionSet) error { return boom }})
	if _, err := svc.Save(models.QuestionSet{
		CompanySlug: "google", Name: "Drill",
		Questions: []models.Problem{setQ("A", "https://leetcode.com/problems/a")},
	}); !errors.Is(err, boom) {
		t.Errorf("Save error = %v, want the store's error", err)
	}

	svc = NewSets(&fakeStore{listQuestionSets: func(string) ([]models.QuestionSet, error) { return nil, boom }})
	if _, err := svc.List("google"); !errors.Is(err, boom) {
		t.Errorf("List error = %v, want the store's error", err)
	}
}
