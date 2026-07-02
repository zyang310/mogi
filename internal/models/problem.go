package models

// Problem is one LeetCode problem in a company's question pool, shaped for the
// Company Practice browse list and the mock draw. It carries factual metadata
// only — never the problem statement (the screenshot carries that). URL is
// rebuilt from the stored slug at parse time so the frontend never string-builds
// links.
type Problem struct {
	ID         int     `json:"id"`         // LeetCode problem number, e.g. 1 for "Two Sum"
	Title      string  `json:"title"`      // e.g. "Two Sum"
	Difficulty string  `json:"difficulty"` // "Easy", "Medium", or "Hard"
	Frequency  float64 `json:"frequency"`  // company interview frequency, 0-100
	Acceptance float64 `json:"acceptance"` // LeetCode acceptance rate, 0-100
	URL        string  `json:"url"`        // https://leetcode.com/problems/{slug}
	Recent     bool    `json:"recent"`     // appears in a recent-window (<= 6 months) pool
}

// CompanyInfo summarises one company's question pool for the picker list.
// MockEligible is false for pools too small for a meaningful random draw, so the
// UI can disable Mock Interview and steer the user to browse-and-pick.
type CompanyInfo struct {
	Slug         string `json:"slug"`         // folder slug, e.g. "google"
	Name         string `json:"name"`         // display name, e.g. "Google"
	ProblemCount int    `json:"problemCount"` // number of problems in the pool
	MockEligible bool   `json:"mockEligible"` // pool large enough for Mock Interview
}

// CompanySessionStart is returned when a company or mock session begins. Session
// is the created row; Company is the display name for the banner; Opening is the
// interviewer's spoken greeting (persisted to the transcript but not into model
// history); Problems is the assigned problem (one entry) or the mock pair (two,
// easier first) for the session banner.
type CompanySessionStart struct {
	Session  Session   `json:"session"`
	Company  string    `json:"company"`
	Opening  string    `json:"opening"`
	Problems []Problem `json:"problems"`
}
