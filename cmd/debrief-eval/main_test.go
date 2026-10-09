package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mogi/internal/ai"
	"mogi/internal/models"
	"mogi/internal/store"
)

// fixtureDB writes a data.db holding one comparable session and three the tool
// must skip, under a path with a space (like macOS's "Application Support"),
// and returns that path.
func fixtureDB(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Application Support")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "data.db")
	db, err := store.OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	add := func(id string, ended bool, debrief string, msgs ...string) {
		t.Helper()
		if _, err := db.CreateSession(id, "", "test/model"); err != nil {
			t.Fatal(err)
		}
		for i, content := range msgs {
			role := "assistant"
			if i%2 == 1 {
				role = "user"
			}
			msg := models.Message{ID: fmt.Sprintf("%s-%d", id, i), SessionID: id, Role: role, Content: content, CreatedAt: time.Now().Add(time.Duration(i) * time.Second)}
			if err := db.AddMessage(msg); err != nil {
				t.Fatal(err)
			}
		}
		if ended {
			if err := db.EndSession(id); err != nil {
				t.Fatal(err)
			}
		}
		if debrief != "" {
			if err := db.SaveSessionDebrief(id, debrief); err != nil {
				t.Fatal(err)
			}
		}
	}
	add("good", true, `{"verdict":"Hire","rubric":{"problemSolving":4,"coding":4,"communication":3,"complexity":4,"pace":0}}`,
		"Walk me through it.", "I'd use a hash map.")
	if err := db.UpdateSessionMeta("good", "Two Sum", "Easy", "def two_sum(nums): pass"); err != nil {
		t.Fatal(err)
	}
	add("no-debrief", true, "", "Hi.", "Hello.")
	add("live", false, `{"verdict":"Hire"}`, "Hi.", "Hello.")
	add("too-short", true, `{"verdict":"Hire"}`, "Hi.")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoadCandidates verifies the tool reads only finished, debriefed,
// long-enough sessions, counts the never-debriefed ones, and cannot write.
func TestLoadCandidates(t *testing.T) {
	db, err := store.OpenReadOnly(fixtureDB(t))
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer db.Close()

	cands, noDebrief, err := loadCandidates(db, 10)
	if err != nil {
		t.Fatalf("loadCandidates: %v", err)
	}
	if len(cands) != 1 || cands[0].summary.ID != "good" {
		t.Fatalf("candidates = %+v, want only the finished, debriefed session", cands)
	}
	if noDebrief != 1 {
		t.Errorf("noDebrief = %d, want 1", noDebrief)
	}
	c := cands[0]
	if c.transcript != "Interviewer: Walk me through it.\nCandidate: I'd use a hash map.\n" || c.finalCode != "def two_sum(nums): pass" {
		t.Errorf("candidate input = %q / %q", c.transcript, c.finalCode)
	}
	if err := db.SaveSessionDebrief("good", "{}"); err == nil {
		t.Error("a read-only store accepted a write")
	}

	if cands, _, _ := loadCandidates(db, 0); len(cands) != 0 {
		t.Errorf("limit 0 returned %d candidates", len(cands))
	}
}

// TestRunDryRun checks -dry-run prints the exact request for the newest
// candidate without a key or any network.
func TestRunDryRun(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	var out bytes.Buffer
	cfg := config{dbPath: fixtureDB(t), limit: 5, model: "test/jev", runs: 1, dryRun: true}
	if err := run(cfg, &out, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	for _, want := range []string{`"model": "test/jev"`, `"questions"`, "I'd use a hash map.", "def two_sum(nums): pass", "Two Sum (Easy)"} {
		if !strings.Contains(got, want) {
			t.Errorf("dry run output lacks %q:\n%s", want, got)
		}
	}
}

// fakeJev answers whatever it is asked: the "hire" option for a choice, level 3
// (rubric 4.0) for a score, and a confident yes for an evidence check.
func fakeJev(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var req struct {
			Questions map[string]ai.DecisionQuestion `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		answers := map[string]ai.DecisionAnswer{}
		for key, q := range req.Questions {
			switch q.Type {
			case "choice":
				answers[key] = ai.DecisionAnswer{Type: "choice", Choice: "hire", Confidence: 0.9}
			case "score":
				answers[key] = ai.DecisionAnswer{Type: "score", Score: 3, Confidence: 0.8}
			case "noul":
				answers[key] = ai.DecisionAnswer{Type: "noul", Noul: 0.95}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "typesafe/jev-test",
			"answers": answers,
			"usage":   map[string]int{"input_tokens": 100},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestRunEndToEnd scores the fixture against a fake decisions endpoint and
// checks the report: the row, verdict agreement, and Jev's run-to-run spread.
func TestRunEndToEnd(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	srv := fakeJev(t)
	var out bytes.Buffer
	cfg := config{dbPath: fixtureDB(t), limit: 5, model: "test/jev", endpoint: srv.URL, runs: 2}
	if err := run(cfg, &out, io.Discard); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Two Sum (Easy)",
		"Hire (0.90) 4.0", // Jev: hire, every dimension 4.0
		"4→4.0",           // problem solving agrees
		"3→4.0",           // communication differs by one
		"—→4.0",           // the LLM saw no evidence for pace
		"exact 1/1 (100%)",
		"Rubric mean |LLM−Jev|: Logic 0.00 · Quality 0.00 · Comm. 1.00 · Complexity 0.00",
		"verdict changed in 0 of 1 sessions",
		"typesafe/jev-test; 200 input tokens",
		"no debrief yet",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

// fakeKeys is a stored-key lookup for resolveKey.
type fakeKeys struct {
	key string
	err error
}

func (f fakeKeys) GetAPIKey(string) (string, error) { return f.key, f.err }

// TestResolveKey checks the environment wins, the stored BYOK key is the
// fallback, and having neither is an error that never leaks a key.
func TestResolveKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", " env-key ")
	if k, src, err := resolveKey(fakeKeys{key: "stored"}); err != nil || k != "env-key" || src != "OPENROUTER_API_KEY" {
		t.Errorf("env: got %q from %q (%v)", k, src, err)
	}
	t.Setenv("OPENROUTER_API_KEY", "")
	if k, src, err := resolveKey(fakeKeys{key: "stored"}); err != nil || k != "stored" || !strings.Contains(src, "data.db") {
		t.Errorf("stored: got %q from %q (%v)", k, src, err)
	}
	if _, _, err := resolveKey(fakeKeys{err: errors.New("boom")}); err == nil {
		t.Error("no key anywhere: want an error")
	}
}

// TestVerdictStep checks distances on the hire scale and the unknown case.
func TestVerdictStep(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"Hire", "Hire", 0},
		{"Strong Hire", "Lean Hire", 2},
		{"No Hire", "Hire", 2},
		{"Strong No Hire", "Strong Hire", 4},
		{"", "Hire", -1},
		{"Hire", "Maybe", -1},
	}
	for _, c := range cases {
		if got := verdictStep(c.a, c.b); got != c.want {
			t.Errorf("verdictStep(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// scores builds a SessionScores with every dimension at the given values, in
// RubricKeys order.
func scores(verdict string, vals ...float64) ai.SessionScores {
	s := ai.SessionScores{Verdict: verdict, InputTokens: 10}
	for i, k := range ai.RubricKeys() {
		s.Dimensions = append(s.Dimensions, ai.DimensionScore{Key: k, Score: vals[i]})
	}
	return s
}

// TestSummarize covers the aggregate math: verdict agreement (exact and within
// one step), per-dimension differences that skip "no evidence", big gaps, and
// both judges' run-to-run spread.
func TestSummarize(t *testing.T) {
	cached := func(verdict string, r models.DebriefRubric) candidate {
		return candidate{summary: models.SessionSummary{Debrief: &models.Debrief{Verdict: verdict, Rubric: r}}}
	}
	rerun := models.Debrief{Verdict: "Lean Hire", Rubric: models.DebriefRubric{ProblemSolving: 3, Coding: 4, Communication: 3, Complexity: 4, Pace: 2}}
	results := []result{
		{ // exact verdict; one big gap on coding; pace skipped (LLM had no evidence)
			cand:  cached("Hire", models.DebriefRubric{ProblemSolving: 4, Coding: 2, Communication: 3, Complexity: 4, Pace: 0}),
			jev:   []ai.SessionScores{scores("Hire", 4, 4.5, 3.5, 4, 3), scores("Lean Hire", 4.5, 4.5, 3.5, 4, 3)},
			rerun: &rerun,
		},
		{ // one step apart
			cand: cached("Lean Hire", models.DebriefRubric{ProblemSolving: 3, Coding: 3, Communication: 3, Complexity: 3, Pace: 3}),
			jev:  []ai.SessionScores{scores("Hire", 3, 3, 3, 3, 3)},
		},
		{ // Jev failed: counted as a session, not scored
			cand:   cached("Hire", models.DebriefRubric{}),
			jevErr: errors.New("boom"),
		},
	}
	s := summarize(results)

	if s.sessions != 3 || s.scored != 2 {
		t.Errorf("sessions/scored = %d/%d, want 3/2", s.sessions, s.scored)
	}
	if s.verdictPairs != 2 || s.verdictExact != 1 || s.verdictWithinOne != 2 {
		t.Errorf("verdicts = %d pairs, %d exact, %d within one; want 2/1/2", s.verdictPairs, s.verdictExact, s.verdictWithinOne)
	}
	if s.dimPairs["pace"] != 1 || s.dimPairs["coding"] != 2 {
		t.Errorf("pairs = %v, want pace counted once (LLM 0 skipped) and coding twice", s.dimPairs)
	}
	if math.Abs(s.dimDiff["coding"]-2.5) > 1e-9 || s.bigGaps != 1 {
		t.Errorf("coding diff = %v, big gaps = %d; want 2.5 and 1", s.dimDiff["coding"], s.bigGaps)
	}
	if s.jevVerdictRuns != 1 || s.jevFlips != 1 || s.jevSpreadPairs != 5 || math.Abs(s.jevSpread-0.5) > 1e-9 {
		t.Errorf("jev spread = %v over %d, flips %d/%d", s.jevSpread, s.jevSpreadPairs, s.jevFlips, s.jevVerdictRuns)
	}
	// LLM rerun vs cache: verdict flipped; pace skipped (cached 0); |4-3| + |2-4| = 3.
	if s.llmVerdictRuns != 1 || s.llmFlips != 1 || s.llmSpreadPairs != 4 || math.Abs(s.llmSpread-3) > 1e-9 {
		t.Errorf("llm spread = %v over %d, flips %d/%d", s.llmSpread, s.llmSpreadPairs, s.llmFlips, s.llmVerdictRuns)
	}
	if s.tokens != 30 {
		t.Errorf("tokens = %d, want 30 (three successful runs)", s.tokens)
	}
}
