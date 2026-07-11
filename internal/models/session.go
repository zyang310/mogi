package models

import "time"

// Session represents a single mock interview session. It mirrors the sessions
// table 1:1. ProblemTitle and Difficulty are AI-derived after the session ends
// (for the history list) and are empty until then.
type Session struct {
	ID           string     `json:"id"`
	ProblemID    string     `json:"problemId"`
	Model        string     `json:"model"`
	StartedAt    time.Time  `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	ProblemTitle string     `json:"problemTitle"`
	Difficulty   string     `json:"difficulty"`
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
// EndedAt is nil for sessions that never ended; the client computes duration
// from StartedAt/EndedAt. ProblemTitle and Difficulty are AI-derived and may be
// empty (the client falls back to a generic label). Company and Mode are set only
// for Company Practice sessions (Mode is "single" or "mock") so history can badge
// them; both are empty for default screen-driven sessions.
type SessionSummary struct {
	ID           string     `json:"id"`
	ProblemTitle string     `json:"problemTitle"`
	Difficulty   string     `json:"difficulty"`
	Model        string     `json:"model"`
	StartedAt    time.Time  `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	MessageCount int        `json:"messageCount"`
	Company      string     `json:"company"`
	Mode         string     `json:"mode"`
	// Debrief is the cached post-interview scorecard, populated straight from
	// SQLite when one already exists — nil otherwise. This never triggers
	// generation (listing sessions still costs zero AI tokens); it just lets the
	// history view show a verdict/score for sessions already reviewed at least
	// once, without a per-row round trip.
	Debrief *Debrief `json:"debrief,omitempty"`
}

// DebriefRubric scores the candidate on five interview dimensions, 1-5. A 0 means
// the model had too little evidence to score that dimension.
type DebriefRubric struct {
	ProblemSolving int `json:"problemSolving"`
	Coding         int `json:"coding"`
	Communication  int `json:"communication"`
	Complexity     int `json:"complexity"`
	Pace           int `json:"pace"`
}

// Debrief is the AI-generated post-interview feedback shown when the user opens a
// past session's debrief. It is generated once (from the transcript + captured
// final code) and cached on the session. Verdict is "" when the model could not
// reach one.
type Debrief struct {
	Verdict      string        `json:"verdict"` // one of the 5-point hire-scale labels, or ""
	Summary      string        `json:"summary"`
	Rubric       DebriefRubric `json:"rubric"`
	Strengths    []string      `json:"strengths"`
	Improvements []string      `json:"improvements"`
}

// AuthStatus reports which API providers are currently configured.
type AuthStatus struct {
	OpenRouterConfigured bool `json:"openRouterConfigured"`
	ElevenLabsConfigured bool `json:"elevenLabsConfigured"`
	GoogleConfigured     bool `json:"googleConfigured"`
}

// Preferences holds user-configurable settings persisted in SQLite.
type Preferences struct {
	CaptureIntervalMs int     `json:"captureIntervalMs"` // default 3000
	Model             string  `json:"model"`             // default "google/gemini-2.5-flash"
	VoiceSpeed        float64 `json:"voiceSpeed"`        // TTS playback rate, default 1.0 (range ~0.5–2.0)

	// Text-to-speech provider + the voice selected for each. Voices are
	// provider-specific, so each provider remembers its own choice.
	TTSProvider   string `json:"ttsProvider"`   // "google" (default) or "elevenlabs"
	VoiceID       string `json:"voiceId"`       // ElevenLabs voice_id
	GoogleVoiceID string `json:"googleVoiceId"` // Google voice name, e.g. "en-US-Neural2-F"

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

	// Global push-to-talk: hold the hotkey to record, release to send. Works
	// while the IDE (not this window) is focused, via an OS-level keyboard hook.
	PushToTalkEnabled bool   `json:"pushToTalkEnabled"` // default true
	PushToTalkKey     string `json:"pushToTalkKey"`     // canonical hotkey, e.g. "Ctrl+Space"

	// Company Practice: the last company slug the user opened and the last
	// difficulty filter they chose, so the tab resumes where they left off.
	LastCompany    string `json:"lastCompany"`    // company slug, e.g. "google"
	LastDifficulty string `json:"lastDifficulty"` // "All" | "Easy" | "Medium" | "Hard"
}
