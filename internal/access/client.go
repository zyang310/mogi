// Package access is the app-side HTTP client for the Mogi access service — the
// small backend that hands developer-funded API keys to invited testers. It
// mirrors the wire contract in access-service/internal/server (activate →
// verify → fetch-keys), wrapping external HTTP exactly like internal/updater and
// internal/ai. Every call happens in the Go backend; fetched keys and the
// session token never cross the Wails boundary to the frontend.
package access

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultURL is the access service's base URL. It is a placeholder until the
	// Cloud Run service is deployed (Phase 3.5); local runs override it with the
	// MOGI_ACCESS_URL env var (see NewApp) to point at a dev service.
	DefaultURL = "https://mogi-access.example.com"
	// httpTimeout bounds every call. Activation sits behind a user click and the
	// launch refresh must never block startup, so keep it short.
	httpTimeout = 15 * time.Second
)

// ErrUnauthorized is a sentinel for a session the service refuses: a bad/expired
// token (HTTP 401) or a revoked tester / ended test phase (HTTP 403). The launch
// refresh branches on it with errors.Is to sign the device out; the wrapped
// error preserves the server's human-readable message for display.
var ErrUnauthorized = errors.New("access: session no longer valid")

// KeySet is the managed key bundle the service returns. It mirrors the server's
// keysPayload field-for-field — returned flat by /keys and nested under "keys"
// by /verify.
type KeySet struct {
	OpenRouter  string `json:"openrouter"`
	Google      string `json:"google"`
	ElevenLabs  string `json:"elevenlabs"`
	PinnedModel string `json:"pinnedModel"`
}

// Client calls the access service over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates an access-service client for the given base URL. A trailing
// slash is trimmed so path joins stay clean.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: httpTimeout},
	}
}

// RequestCode validates the invite and asks the service to email an OTP. A nil
// error means the code was sent (HTTP 204). The invite is checked before any
// mail is sent, so an invalid/exhausted invite returns an error here with no
// mail dispatched.
func (c *Client) RequestCode(ctx context.Context, email, inviteCode string) error {
	return c.do(ctx, http.MethodPost, "/activate",
		map[string]string{"email": email, "inviteCode": inviteCode}, nil, "")
}

// Verify submits the OTP and, on success, returns the session token and the
// managed key set. A wrong/expired code comes back as an error carrying the
// server's message.
func (c *Client) Verify(ctx context.Context, email, code string) (string, KeySet, error) {
	var out struct {
		Token string `json:"token"`
		Keys  KeySet `json:"keys"`
	}
	if err := c.do(ctx, http.MethodPost, "/verify",
		map[string]string{"email": email, "code": code}, &out, ""); err != nil {
		return "", KeySet{}, err
	}
	return out.Token, out.Keys, nil
}

// Keys re-fetches the managed key set for a session token — the launch-refresh
// call that drives silent rotation and revocation enforcement. A revoked tester
// or an ended test phase surfaces as ErrUnauthorized.
func (c *Client) Keys(ctx context.Context, token string) (KeySet, error) {
	var ks KeySet
	if err := c.do(ctx, http.MethodGet, "/keys", nil, &ks, token); err != nil {
		return KeySet{}, err
	}
	return ks, nil
}

// do executes a request against the access service: it JSON-encodes body (if
// any), attaches the bearer token (if any), and decodes a 2xx response into out
// (if any). Non-2xx responses decode the server's {"error"} message; 401/403
// wrap ErrUnauthorized so callers can tell revocation from a transient failure.
func (c *Client) do(ctx context.Context, method, path string, body, out any, token string) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("access: encode request: %w", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("access: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("access: http request: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("access: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return statusError(resp.StatusCode, data)
	}

	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("access: decode response: %w", err)
		}
	}
	return nil
}

// statusError turns a non-2xx response into an error carrying the server's
// human-readable {"error"} message. 401 and 403 additionally wrap
// ErrUnauthorized so the launch refresh can distinguish "sign out" from a
// transient failure that should keep cached keys.
func statusError(status int, body []byte) error {
	msg := decodeErrorMessage(body)
	if msg == "" {
		msg = fmt.Sprintf("request failed with status %d", status)
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return fmt.Errorf("%w: %s", ErrUnauthorized, msg)
	}
	return fmt.Errorf("access: %s", msg)
}

// decodeErrorMessage pulls the "error" field out of the service's error body,
// returning "" when the body isn't the expected shape.
func decodeErrorMessage(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Error
}
