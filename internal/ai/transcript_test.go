package ai

import (
	"strings"
	"testing"

	"mogi/internal/models"
)

// TestBuildTranscript pins the "Speaker: text" format every post-session judge
// reads: assistant turns are the Interviewer, everything else the Candidate.
func TestBuildTranscript(t *testing.T) {
	got := BuildTranscript([]models.Message{
		{Role: "assistant", Content: "Walk me through it."},
		{Role: "user", Content: "I'd use a hash map."},
	})
	want := "Interviewer: Walk me through it.\nCandidate: I'd use a hash map.\n"
	if got != want {
		t.Errorf("BuildTranscript() = %q, want %q", got, want)
	}
	if BuildTranscript(nil) != "" {
		t.Error("empty transcript should be empty")
	}
}

// TestDebriefContent verifies the final code is appended under its header only
// when one was captured.
func TestDebriefContent(t *testing.T) {
	if got := debriefContent("T\n", "  \n"); got != "T\n" {
		t.Errorf("blank code: got %q, want the transcript alone", got)
	}
	got := debriefContent("T\n", "return 1")
	if !strings.HasPrefix(got, "T\n") || !strings.HasSuffix(got, "=== Candidate's final code (transcribed from their screen) ===\nreturn 1") {
		t.Errorf("with code: got %q", got)
	}
}
