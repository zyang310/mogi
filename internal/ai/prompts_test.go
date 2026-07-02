package ai

import (
	"strings"
	"testing"

	"ai-interviewer/internal/models"
)

// TestBuildSystemPromptUnchanged confirms the default screen-driven prompt is
// exactly the shared base — Company Practice must not change the Hub flow.
func TestBuildSystemPromptUnchanged(t *testing.T) {
	got := BuildSystemPrompt()
	if got != basePrompt() {
		t.Error("BuildSystemPrompt should return the base prompt unchanged")
	}
	if !strings.Contains(got, "senior software engineer running") {
		t.Error("base prompt missing the interviewer persona")
	}
}

// TestCompanyProfileFallback covers curated vs generic profiles.
func TestCompanyProfileFallback(t *testing.T) {
	if p := CompanyProfile("google"); !strings.Contains(p, "algorithmic depth") {
		t.Errorf("google profile = %q", p)
	}
	if p := CompanyProfile("some-tiny-startup"); p != genericProfile {
		t.Errorf("unknown company should get genericProfile, got %q", p)
	}
}

// TestBuildCompanySystemPromptSingle: the single-problem prompt carries the base
// rules, the persona, and the assignment — and never mentions a second problem.
func TestBuildCompanySystemPromptSingle(t *testing.T) {
	problems := []models.Problem{{Title: "Two Sum", Difficulty: "Easy", URL: "https://leetcode.com/problems/two-sum"}}
	p := BuildCompanySystemPrompt("Google", CompanyProfile("google"), problems)

	if !strings.Contains(p, "senior software engineer running") {
		t.Error("company prompt missing the base rules")
	}
	if !strings.Contains(p, "Google") || !strings.Contains(p, "algorithmic depth") {
		t.Error("company prompt missing the persona")
	}
	if !strings.Contains(p, "Two Sum") || !strings.Contains(p, "Easy") {
		t.Error("company prompt missing the assignment")
	}
	if !strings.Contains(p, "ALREADY greeted") {
		t.Error("company prompt should encode the already-greeted framing")
	}
	// Screen-driven invariant: names the title, not the statement.
	if !strings.Contains(p, "NOT its written statement") {
		t.Error("company prompt should forbid reciting the problem statement")
	}
	// Single mode must not leak a mock/second-problem concept.
	low := strings.ToLower(p)
	if strings.Contains(low, "second problem") || strings.Contains(low, "two-problem") || strings.Contains(low, "two problems") {
		t.Error("single-problem prompt should not mention a second problem")
	}
}

// TestBuildCompanySystemPromptMock: the mock prompt carries both problems and the
// don't-reveal-Q2 handoff rule.
func TestBuildCompanySystemPromptMock(t *testing.T) {
	problems := []models.Problem{
		{Title: "Two Sum", Difficulty: "Easy"},
		{Title: "LRU Cache", Difficulty: "Medium"},
	}
	p := BuildCompanySystemPrompt("Meta", CompanyProfile("meta"), problems)

	if !strings.Contains(p, "Two Sum") || !strings.Contains(p, "LRU Cache") {
		t.Error("mock prompt should contain both problems")
	}
	if !strings.Contains(p, "NEVER name it") {
		t.Error("mock prompt missing the don't-reveal-Q2 rule")
	}
	if !strings.Contains(strings.ToLower(p), "second problem") {
		t.Error("mock prompt should reference the second problem")
	}
	if !strings.Contains(p, "two-problem mock interview") {
		t.Error("mock prompt should identify itself as a mock interview")
	}
}

// TestOpenings: openers name the right things, are TTS-safe (no URLs), and the
// mock opener never reveals the second problem.
func TestOpenings(t *testing.T) {
	single := CompanyOpening("Google", models.Problem{Title: "Two Sum", Difficulty: "Easy"})
	if !strings.Contains(single, "Google") || !strings.Contains(single, "Two Sum") || !strings.Contains(single, "Easy") {
		t.Errorf("single opening missing details: %q", single)
	}
	if strings.Contains(single, "http") {
		t.Errorf("opening should carry no URL (TTS-safe): %q", single)
	}

	mock := MockOpening("Meta", models.Problem{Title: "Two Sum", Difficulty: "Easy"})
	if !strings.Contains(mock, "Meta") || !strings.Contains(mock, "Two Sum") {
		t.Errorf("mock opening missing details: %q", mock)
	}
	if strings.Contains(mock, "http") {
		t.Errorf("mock opening should carry no URL (TTS-safe): %q", mock)
	}
}
