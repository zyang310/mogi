package models

import "time"

// Session represents a single mock interview session.
type Session struct {
	ID        string     `json:"id"`
	ProblemID string     `json:"problemId"`
	Model     string     `json:"model"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
}

// Message is one turn in the interview conversation.
type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"sessionId"`
	Role      string    `json:"role"` // "user" or "assistant"
	Content   string    `json:"content"`
	HasImage  bool      `json:"hasImage"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionSummary is a lightweight view used in the session history list.
type SessionSummary struct {
	ID           string    `json:"id"`
	ProblemTitle string    `json:"problemTitle"`
	Model        string    `json:"model"`
	StartedAt    time.Time `json:"startedAt"`
	MessageCount int       `json:"messageCount"`
}

// AuthStatus reports which API providers are currently configured.
type AuthStatus struct {
	OpenRouterConfigured bool `json:"openRouterConfigured"`
	ElevenLabsConfigured bool `json:"elevenLabsConfigured"`
}

// Preferences holds user-configurable settings persisted in SQLite.
type Preferences struct {
	CaptureIntervalMs int     `json:"captureIntervalMs"` // default 3000
	Model             string  `json:"model"`             // default "anthropic/claude-sonnet-4"
	VoiceID           string  `json:"voiceId"`
	VoiceSpeed        float64 `json:"voiceSpeed"` // TTS playback rate, default 1.0 (range ~0.5–2.0)

	// Capture region. Coordinates are fractions (0..1) of the chosen display;
	// a zero RegionW means "capture the full display".
	CaptureDisplay int     `json:"captureDisplay"` // display index, default 0
	RegionX        float64 `json:"regionX"`
	RegionY        float64 `json:"regionY"`
	RegionW        float64 `json:"regionW"`
	RegionH        float64 `json:"regionH"`

	// Session timer. 0 means no limit / no warning.
	SessionLimitMinutes int `json:"sessionLimitMinutes"` // default 30
	SoftWarningMinutes  int `json:"softWarningMinutes"`  // default 25
}
