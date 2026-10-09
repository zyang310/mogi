package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"
)

// openRouterDecisionsURL is OpenRouter's endpoint for typed-decision models such
// as TypeSafe's Jev. Unlike chat/completions it takes a text "state" plus named,
// typed questions and returns typed answers with probabilities — never prose. It
// is an alpha route: the path and field names here follow OpenRouter's
// community Jev guide and third-party write-ups (October 2026), not a stable
// reference, so SetDecisionsURL can point elsewhere while it settles.
const openRouterDecisionsURL = "https://openrouter.ai/api/alpha/decisions"

// DecisionQuestion is one typed question for a decision model. Its key in the
// request map is a label for our code only — it is never shown to the model —
// so Instructions (plus the option and level descriptions) must carry all the
// meaning.
type DecisionQuestion struct {
	Type         string            `json:"type"` // "choice" | "score" | "noul" (yes/no)
	Instructions string            `json:"instructions"`
	Options      map[string]string `json:"options,omitempty"` // choice: option key → description
	Levels       []string          `json:"levels,omitempty"`  // score: ordered level descriptions, lowest first
}

// DecisionAnswer is the typed answer to one DecisionQuestion. Which fields are
// set depends on the question type: a choice sets Choice; a score sets Score
// (0..levels-1, probability-weighted); a yes/no sets Noul (the yes-probability,
// 0..1). Choice and score also carry Probabilities and Confidence — how
// concentrated the probabilities are, not whether the answer is right.
type DecisionAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Noul          float64            `json:"noul,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
}

// DecisionResult is a decision model's reply: an answer per question key, the
// exact model version that answered (worth recording when calling a "-latest"
// alias), and token usage. Raw keeps the undecoded body so callers can show it
// when an answer they expected is missing.
type DecisionResult struct {
	Model   string                    `json:"model"`
	Answers map[string]DecisionAnswer `json:"answers"`
	Usage   struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Raw []byte `json:"-"`
}

// decisionRequest is the decisions endpoint's request body.
type decisionRequest struct {
	Model     string                      `json:"model"`
	State     string                      `json:"state"`
	Questions map[string]DecisionQuestion `json:"questions"`
}

// SetDecisionsURL points Decide at a different endpoint — for the comparison
// tool while OpenRouter's alpha route settles, and for tests. An empty url
// restores the default. Call it before use; it is not synchronised with Decide.
func (c *Client) SetDecisionsURL(url string) {
	c.decisionsURL = url
}

// DecisionRequestBody returns the exact JSON Decide sends, so tools can show a
// request without making it.
func DecisionRequestBody(model, state string, questions map[string]DecisionQuestion) ([]byte, error) {
	body, err := json.Marshal(decisionRequest{Model: model, State: state, Questions: questions})
	if err != nil {
		return nil, fmt.Errorf("ai: marshal decisions request: %w", err)
	}
	return body, nil
}

// Decide asks a typed-decision model (e.g. "~typesafe/jev-latest") a batch of
// questions about one state and returns its typed answers; the model evaluates
// them in parallel, so one call answers them all. It shares the OpenRouter key
// and identity headers with Complete. Error bodies are clipped before being
// folded into the error, because the decisions API may echo the whole state
// back.
func (c *Client) Decide(ctx context.Context, model, state string, questions map[string]DecisionQuestion) (DecisionResult, error) {
	if c.apiKey == "" {
		return DecisionResult{}, fmt.Errorf("ai: OpenRouter API key is not configured")
	}
	if len(questions) == 0 {
		return DecisionResult{}, fmt.Errorf("ai: decide: no questions")
	}

	body, err := DecisionRequestBody(model, state, questions)
	if err != nil {
		return DecisionResult{}, err
	}

	url := c.decisionsURL
	if url == "" {
		url = openRouterDecisionsURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return DecisionResult{}, fmt.Errorf("ai: build decisions request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/zyang310/mogi")
	req.Header.Set("X-Title", "Mogi")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("ai: decisions http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("ai: read decisions response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return DecisionResult{}, fmt.Errorf("ai: OpenRouter decisions returned %d: %s", resp.StatusCode, clip(string(respBody), maxErrorBody))
	}
	return parseDecisionResult(respBody)
}

// parseDecisionResult decodes a decisions reply, surfacing an in-body error
// (OpenRouter's {"error":{"message"}} shape) and refusing a reply with no
// answers.
func parseDecisionResult(body []byte) (DecisionResult, error) {
	var r struct {
		DecisionResult
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return DecisionResult{}, fmt.Errorf("ai: parse decisions response: %w (body: %s)", err, clip(string(body), maxErrorBody))
	}
	if r.Error != nil {
		return DecisionResult{}, fmt.Errorf("ai: decisions API error: %s", r.Error.Message)
	}
	if len(r.Answers) == 0 {
		return DecisionResult{}, fmt.Errorf("ai: decisions response has no answers (body: %s)", clip(string(body), maxErrorBody))
	}
	res := r.DecisionResult
	res.Raw = body
	return res, nil
}

// maxErrorBody bounds how much of a response body an error message carries.
const maxErrorBody = 500

// clip shortens s to at most n bytes (on a rune boundary), marking the cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
