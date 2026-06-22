// Package voice wraps the ElevenLabs API: speech-to-text (Scribe), text-to-speech
// (Flash), and the voice catalog. Like internal/ai, every call lives in the Go
// backend so the API key never reaches the frontend. The client mirrors
// ai.Client (apiKey + httpClient + a small mutex-guarded cache for the catalog).
package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-interviewer/internal/models"
)

const (
	sttURL    = "https://api.elevenlabs.io/v1/speech-to-text"
	ttsURL    = "https://api.elevenlabs.io/v1/text-to-speech/" // + voiceID
	voicesURL = "https://api.elevenlabs.io/v1/voices"

	// scribeModel is ElevenLabs' current STT model. scribe_v1 is deprecated
	// (removed 2026-07-09); scribe_v2 is the supported successor.
	scribeModel = "scribe_v2"
	// ttsModel is the low-latency (~75ms) TTS model — a good fit for the short
	// 1-3 sentence interviewer replies.
	ttsModel = "eleven_flash_v2_5"

	httpTimeout = 60 * time.Second
	// voicesCacheTTL bounds how long ListVoices serves the cached catalog before
	// re-fetching. The voice list changes rarely, so an hour avoids re-pulling it
	// every time the user opens Settings (mirrors ai.Client's model cache).
	voicesCacheTTL = time.Hour
)

// Client calls the ElevenLabs API.
type Client struct {
	apiKey     string
	httpClient *http.Client

	// Cached voice catalog. ListVoices refreshes it lazily; mu guards both fields
	// since Wails may invoke bound methods from different goroutines.
	mu           sync.Mutex
	cachedVoices []models.Voice
	cachedAt     time.Time
}

// NewClient creates a voice client with the given ElevenLabs API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
	}
}

// Transcribe sends recorded audio to ElevenLabs Scribe and returns the
// transcribed text. mimeType is the recorder's output type (e.g. "audio/mp4" on
// macOS WKWebView, "audio/webm" elsewhere); it only sets the upload filename's
// extension — ElevenLabs sniffs the actual format.
func (c *Client) Transcribe(ctx context.Context, audio []byte, mimeType string) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("voice: ElevenLabs API key is not configured")
	}

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "audio"+mimeToExt(mimeType))
	if err != nil {
		return "", fmt.Errorf("voice: create form file: %w", err)
	}
	if _, err := part.Write(audio); err != nil {
		return "", fmt.Errorf("voice: write audio: %w", err)
	}
	if err := w.WriteField("model_id", scribeModel); err != nil {
		return "", fmt.Errorf("voice: write model field: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("voice: close multipart: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sttURL, &body)
	if err != nil {
		return "", fmt.Errorf("voice: build STT request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("xi-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("voice: STT http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("voice: read STT response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("voice: ElevenLabs STT returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("voice: parse STT response: %w", err)
	}
	return result.Text, nil
}

// Synthesize converts text to speech with the given voice and returns the raw
// MP3 bytes. The caller (app.go) base64-encodes them for the Wails boundary.
func (c *Client) Synthesize(ctx context.Context, voiceID, text string) ([]byte, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("voice: ElevenLabs API key is not configured")
	}
	if voiceID == "" {
		return nil, fmt.Errorf("voice: no voice selected")
	}

	payload := map[string]any{
		"text":     text,
		"model_id": ttsModel,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("voice: marshal TTS request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ttsURL+voiceID, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("voice: build TTS request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")
	req.Header.Set("xi-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice: TTS http request: %w", err)
	}
	defer resp.Body.Close()

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("voice: read TTS response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// On error the body is JSON, not audio — surface it.
		return nil, fmt.Errorf("voice: ElevenLabs TTS returned %d: %s", resp.StatusCode, string(audio))
	}
	return audio, nil
}

// ListVoices returns the account's available voices for the picker UI. Results
// are cached in-memory for voicesCacheTTL so opening Settings doesn't re-pull the
// list each time. The mutex is held across the fetch to serialize concurrent
// callers; a failed fetch leaves the cache intact so the next call retries.
func (c *Client) ListVoices(ctx context.Context) ([]models.Voice, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("voice: ElevenLabs API key is not configured")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.cachedVoices) > 0 && time.Since(c.cachedAt) < voicesCacheTTL {
		return c.cachedVoices, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, voicesURL, nil)
	if err != nil {
		return nil, fmt.Errorf("voice: build voices request: %w", err)
	}
	req.Header.Set("xi-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice: voices http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("voice: read voices response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("voice: ElevenLabs voices returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Mirror only the fields the picker needs.
	var result struct {
		Voices []struct {
			VoiceID    string `json:"voice_id"`
			Name       string `json:"name"`
			Category   string `json:"category"`
			PreviewURL string `json:"preview_url"`
		} `json:"voices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("voice: parse voices response: %w", err)
	}

	out := make([]models.Voice, 0, len(result.Voices))
	for _, v := range result.Voices {
		out = append(out, models.Voice{
			ID:         v.VoiceID,
			Name:       v.Name,
			Category:   v.Category,
			PreviewURL: v.PreviewURL,
		})
	}

	c.cachedVoices = out
	c.cachedAt = time.Now()
	return out, nil
}

// mimeToExt maps a MediaRecorder MIME type to a file extension for the STT
// upload's filename. Any codec parameter (e.g. "audio/webm;codecs=opus") is
// stripped first. Unknown types fall back to ".webm".
func mimeToExt(mimeType string) string {
	base, _, _ := strings.Cut(mimeType, ";")
	switch strings.TrimSpace(base) {
	case "audio/mp4", "audio/x-m4a", "audio/aac":
		return ".m4a"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/webm":
		return ".webm"
	default:
		return ".webm"
	}
}
