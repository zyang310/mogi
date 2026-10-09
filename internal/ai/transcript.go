package ai

import (
	"strings"

	"mogi/internal/models"
)

// BuildTranscript renders stored messages as a plain "Speaker: text" transcript.
// It is the shared input for problem-metadata extraction, the debrief, and Jev
// scoring, so every post-session judge reads the session the same way.
func BuildTranscript(msgs []models.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		speaker := "Candidate"
		if m.Role == "assistant" {
			speaker = "Interviewer"
		}
		b.WriteString(speaker)
		b.WriteString(": ")
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}

// debriefContent is what a post-session judge reads: the transcript, plus the
// candidate's final code (transcribed from their screen at session end) when one
// was captured. GenerateDebrief and ScoreSession share it so the LLM and Jev
// assess byte-identical input.
func debriefContent(transcript, finalCode string) string {
	if strings.TrimSpace(finalCode) == "" {
		return transcript
	}
	return transcript + "\n\n=== Candidate's final code (transcribed from their screen) ===\n" + finalCode
}
