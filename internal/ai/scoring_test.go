package ai

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
)

// fullAnswers returns a complete, well-formed set of scoring answers: the given
// verdict option, every dimension at the given weighted level, and every
// evidence check at the given yes-probability.
func fullAnswers(verdict string, level, evidence float64) map[string]DecisionAnswer {
	a := map[string]DecisionAnswer{
		verdictQuestionKey: {Type: "choice", Choice: verdict, Confidence: 0.8},
	}
	for _, key := range RubricKeys() {
		a[scoreKey(key)] = DecisionAnswer{Type: "score", Score: level, Confidence: 0.7}
		a[evidenceKey(key)] = DecisionAnswer{Type: "noul", Noul: evidence}
	}
	return a
}

// TestScoreQuestions checks the request carries the verdict plus a score and an
// evidence check per dimension, each self-explanatory (keys never reach Jev).
func TestScoreQuestions(t *testing.T) {
	qs := ScoreQuestions()
	if len(qs) != 1+2*len(RubricKeys()) {
		t.Fatalf("got %d questions, want the verdict plus two per dimension", len(qs))
	}
	v := qs[verdictQuestionKey]
	if v.Type != "choice" || len(v.Options) != len(verdictOptions) {
		t.Errorf("verdict question = %+v, want a choice over every option", v)
	}
	for _, key := range RubricKeys() {
		sc, ev := qs[scoreKey(key)], qs[evidenceKey(key)]
		if sc.Type != "score" || len(sc.Levels) != 5 {
			t.Errorf("%s score question = %+v, want a five-level score", key, sc)
		}
		if ev.Type != "noul" {
			t.Errorf("%s evidence question type = %q, want noul", key, ev.Type)
		}
		for _, q := range []DecisionQuestion{sc, ev} {
			if !strings.HasPrefix(q.Instructions, stateContext) {
				t.Errorf("%s: instructions must explain the state on their own: %q", key, q.Instructions)
			}
		}
	}
	if got := strings.Join(RubricKeys(), ","); got != "problemSolving,coding,communication,complexity,pace" {
		t.Errorf("RubricKeys() = %s, want the debrief's display order", got)
	}
}

// TestParseScores covers the answer → SessionScores mapping: verdict labels,
// the level → 1-5 conversion, the evidence gate, and malformed replies.
func TestParseScores(t *testing.T) {
	t.Run("maps verdict and levels", func(t *testing.T) {
		res := DecisionResult{Model: "typesafe/jev-1.13", Answers: fullAnswers("lean_hire", 2.5, 0.9)}
		res.Usage.InputTokens = 900
		s, err := ParseScores(res)
		if err != nil {
			t.Fatalf("ParseScores() error: %v", err)
		}
		if s.Verdict != "Lean Hire" || s.VerdictConfidence != 0.8 || s.Model != "typesafe/jev-1.13" || s.InputTokens != 900 {
			t.Errorf("scores = %+v", s)
		}
		if len(s.Dimensions) != 5 || s.Dimensions[0].Key != "problemSolving" {
			t.Fatalf("dimensions = %+v, want all five in order", s.Dimensions)
		}
		for _, d := range s.Dimensions {
			if d.Score != 3.5 || d.Confidence != 0.7 || d.Evidence != 0.9 {
				t.Errorf("%s = %+v, want level 2.5 → 3.5", d.Key, d)
			}
		}
	})

	t.Run("not enough evidence scores 0", func(t *testing.T) {
		s, err := ParseScores(DecisionResult{Answers: fullAnswers("unclear", 3, 0.2)})
		if err != nil {
			t.Fatalf("ParseScores() error: %v", err)
		}
		if s.Verdict != "" {
			t.Errorf("verdict = %q, want empty for unclear", s.Verdict)
		}
		for _, d := range s.Dimensions {
			if d.Score != 0 {
				t.Errorf("%s score = %v, want 0 below the evidence threshold", d.Key, d.Score)
			}
		}
	})

	t.Run("malformed replies quote the response", func(t *testing.T) {
		missingDim := fullAnswers("hire", 2, 1)
		delete(missingDim, scoreKey("pace"))
		for name, answers := range map[string]map[string]DecisionAnswer{
			"unknown verdict":   fullAnswers("maybe", 2, 1),
			"missing dimension": missingDim,
			"no verdict":        {},
		} {
			_, err := ParseScores(DecisionResult{Answers: answers, Raw: []byte(`{"raw":true}`)})
			if err == nil || !strings.Contains(err.Error(), `{"raw":true}`) {
				t.Errorf("%s: err = %v, want an error quoting the response", name, err)
			}
		}
	})
}

// TestLevelToRubric checks the 0..n-1 → 1..5 mapping and its clamping.
func TestLevelToRubric(t *testing.T) {
	cases := []struct {
		level  float64
		levels int
		want   float64
	}{
		{0, 5, 1}, {4, 5, 5}, {1.7, 5, 2.7}, {1, 3, 3}, {-1, 5, 1}, {9, 5, 5}, {2, 1, 0},
	}
	for _, c := range cases {
		if got := levelToRubric(c.level, c.levels); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("levelToRubric(%v, %d) = %v, want %v", c.level, c.levels, got, c.want)
		}
	}
}

// TestTrimMiddle verifies oversized states keep their opening and ending, mark
// the cut, and stay within budget; small ones pass through.
func TestTrimMiddle(t *testing.T) {
	if got, trimmed := trimMiddle("short\n", 100); got != "short\n" || trimmed {
		t.Errorf("short text: got %q (trimmed %v)", got, trimmed)
	}

	var b strings.Builder
	b.WriteString("Interviewer: FIRST\n")
	for i := 0; i < 500; i++ {
		b.WriteString("Candidate: thinking out loud about the problem\n")
	}
	b.WriteString("Interviewer: LAST\n")
	got, trimmed := trimMiddle(b.String(), 2000)
	if !trimmed || len(got) > 2000 {
		t.Fatalf("trimmed=%v len=%d, want a trimmed state within budget", trimmed, len(got))
	}
	if !strings.HasPrefix(got, "Interviewer: FIRST\n") || !strings.HasSuffix(got, "Interviewer: LAST\n") || !strings.Contains(got, "lines omitted") {
		t.Errorf("trimmed state lost its ends or marker:\n%s", got)
	}

	// One giant line can't be kept whole: fall back to the tail, on a rune boundary.
	huge := strings.Repeat("é", 3000)
	got, trimmed = trimMiddle(huge, 1001)
	if !trimmed || len(got) > 1001 || !strings.HasPrefix(got, "é") {
		t.Errorf("giant line: trimmed=%v len=%d prefix=%q", trimmed, len(got), got[:2])
	}
}

// TestScoreSession runs a full scoring call against a fake decisions endpoint:
// the state is the debrief input and the answers come back mapped.
func TestScoreSession(t *testing.T) {
	answers, _ := json.Marshal(fullAnswers("hire", 3, 0.9))
	var req decisionRequest
	srv := decisionsServer(t, http.StatusOK, `{"model":"typesafe/jev-1.13","answers":`+string(answers)+`}`, &req)
	c := NewClient("test-key")
	c.SetDecisionsURL(srv.URL)

	s, err := c.ScoreSession(context.Background(), DefaultScoreModel, "Candidate: hi\n", "return 1")
	if err != nil {
		t.Fatalf("ScoreSession() error: %v", err)
	}
	if req.State != debriefContent("Candidate: hi\n", "return 1") {
		t.Errorf("state = %q, want the debrief input", req.State)
	}
	if req.Model != DefaultScoreModel || len(req.Questions) != len(ScoreQuestions()) {
		t.Errorf("request model/questions = %q/%d", req.Model, len(req.Questions))
	}
	if s.Verdict != "Hire" || s.Dimensions[0].Score != 4 || s.Trimmed {
		t.Errorf("scores = %+v", s)
	}
}
