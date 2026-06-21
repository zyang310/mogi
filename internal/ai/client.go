package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	openRouterURL  = "https://openrouter.ai/api/v1/chat/completions"
	MaxHistoryMsgs = 20 // keep last 20 messages (10 exchanges) to limit cost/latency
	httpTimeout    = 60 * time.Second
	// maxResponseTokens caps the completion length. Interviewer replies are
	// 1-3 sentences, so this is generous. It also matters for billing: without
	// it, OpenRouter defaults to the model's full output limit (e.g. 64k) and
	// pre-authorizes credits for that worst case, which 402s on low balances.
	maxResponseTokens = 1024
)

// ChatMessage is a single message in the OpenRouter request format.
// Content can be a plain string or a slice of content parts (for vision).
type ChatMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string | []ContentPart
}

// ContentPart is used for multimodal messages (text + image).
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL holds a base64 data URI for vision messages.
type ImageURL struct {
	URL string `json:"url"`
}

// Client calls the OpenRouter chat completions API.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates an AI client with the given OpenRouter API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// Complete sends the conversation history to OpenRouter and returns the
// assistant's response text. The system prompt must be the first element of
// messages. Past screenshots are stripped and history is trimmed before sending.
func (c *Client) Complete(ctx context.Context, model string, messages []ChatMessage) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("ai: OpenRouter API key is not configured")
	}

	trimmed := trimHistory(stripPastImages(messages))

	payload := map[string]any{
		"model":      model,
		"messages":   trimmed,
		"max_tokens": maxResponseTokens,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("ai: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("ai: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("HTTP-Referer", "https://github.com/zhihangyang/ai-interviewer")
	req.Header.Set("X-Title", "AI Interviewer")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai: http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("ai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai: OpenRouter returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("ai: parse response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("ai: API error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("ai: empty choices in response")
	}

	return result.Choices[0].Message.Content, nil
}

// stripPastImages returns a new message slice where every user message except the
// last one has its image_url content parts removed and is collapsed to plain
// string content. The last user message keeps its screenshot so the model sees
// the candidate's current screen. The original slice is never mutated.
func stripPastImages(messages []ChatMessage) []ChatMessage {
	if len(messages) == 0 {
		return messages
	}

	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	result := make([]ChatMessage, len(messages))
	for i, msg := range messages {
		if msg.Role != "user" || i == lastUserIdx {
			result[i] = msg
			continue
		}
		parts, ok := msg.Content.([]ContentPart)
		if !ok {
			result[i] = msg
			continue
		}
		var texts []string
		for _, p := range parts {
			if p.Type == "text" {
				texts = append(texts, p.Text)
			}
		}
		result[i] = ChatMessage{Role: "user", Content: strings.Join(texts, " ")}
	}
	return result
}

// trimHistory keeps the system prompt (index 0) and the last MaxHistoryMsgs
// non-system messages, so the context window stays bounded.
func trimHistory(messages []ChatMessage) []ChatMessage {
	if len(messages) == 0 {
		return messages
	}

	// Separate system prompt from the rest.
	system := messages[0]
	rest := messages[1:]

	if len(rest) > MaxHistoryMsgs {
		rest = rest[len(rest)-MaxHistoryMsgs:]
	}

	trimmed := make([]ChatMessage, 0, 1+len(rest))
	trimmed = append(trimmed, system)
	trimmed = append(trimmed, rest...)
	return trimmed
}

// BuildUserMessage creates a ChatMessage for a user turn. If screenshotB64 is
// non-empty, the message includes the screenshot as a vision content part.
func BuildUserMessage(text, screenshotB64 string) ChatMessage {
	if screenshotB64 == "" {
		return ChatMessage{Role: "user", Content: text}
	}

	return ChatMessage{
		Role: "user",
		Content: []ContentPart{
			{Type: "text", Text: text},
			{
				Type: "image_url",
				ImageURL: &ImageURL{
					URL: "data:image/png;base64," + screenshotB64,
				},
			},
		},
	}
}
