package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decisionsServer stands in for OpenRouter's decisions endpoint: it records the
// last request body and replies with the given status and body.
func decisionsServer(t *testing.T, status int, reply string, got *decisionRequest) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("Authorization = %q, want the bearer key", auth)
		}
		body, _ := io.ReadAll(r.Body)
		if got != nil {
			if err := json.Unmarshal(body, got); err != nil {
				t.Errorf("request body is not JSON: %v", err)
			}
		}
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDecide covers the request shape and the reply handling: answers decoded,
// non-200 and in-body errors surfaced, an answerless reply refused.
func TestDecide(t *testing.T) {
	questions := map[string]DecisionQuestion{
		"team": {Type: "choice", Instructions: "Which team?", Options: map[string]string{"billing": "Billing"}},
	}

	t.Run("sends the state and typed questions, decodes answers", func(t *testing.T) {
		var req decisionRequest
		srv := decisionsServer(t, http.StatusOK,
			`{"model":"typesafe/jev-1.13","answers":{"team":{"type":"choice","choice":"billing","probabilities":{"billing":0.95},"confidence":0.9}},"usage":{"input_tokens":42}}`,
			&req)
		c := NewClient("test-key")
		c.SetDecisionsURL(srv.URL)

		res, err := c.Decide(context.Background(), "~typesafe/jev-latest", "the state", questions)
		if err != nil {
			t.Fatalf("Decide() error: %v", err)
		}
		if req.Model != "~typesafe/jev-latest" || req.State != "the state" || req.Questions["team"].Type != "choice" {
			t.Errorf("request = %+v, want model, state, and the typed question", req)
		}
		if a := res.Answers["team"]; a.Choice != "billing" || a.Confidence != 0.9 || a.Probabilities["billing"] != 0.95 {
			t.Errorf("answer = %+v", a)
		}
		if res.Model != "typesafe/jev-1.13" || res.Usage.InputTokens != 42 || len(res.Raw) == 0 {
			t.Errorf("result = %+v, want model, usage, and the raw body", res)
		}
	})

	cases := []struct {
		name, reply, wantErr string
		status               int
	}{
		{"non-200 folds in the clipped body", `{"error":{"message":"bad"}}` + strings.Repeat("x", 2000), "returned 400", http.StatusBadRequest},
		{"in-body error", `{"error":{"message":"model not found"}}`, "model not found", http.StatusOK},
		{"no answers", `{"model":"m","answers":{}}`, "no answers", http.StatusOK},
		{"not JSON", `<html>`, "parse decisions response", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := decisionsServer(t, tc.status, tc.reply, nil)
			c := NewClient("test-key")
			c.SetDecisionsURL(srv.URL)
			_, err := c.Decide(context.Background(), "m", "s", questions)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
			if len(err.Error()) > maxErrorBody+200 {
				t.Errorf("error is %d bytes; bodies must be clipped", len(err.Error()))
			}
		})
	}

	t.Run("guards", func(t *testing.T) {
		if _, err := NewClient("").Decide(context.Background(), "m", "s", questions); err == nil {
			t.Error("no key: want error")
		}
		if _, err := NewClient("k").Decide(context.Background(), "m", "s", nil); err == nil {
			t.Error("no questions: want error")
		}
	})
}

// TestClip verifies clipping keeps short strings and never splits a rune.
func TestClip(t *testing.T) {
	if clip("short", 10) != "short" {
		t.Error("short strings must pass through")
	}
	if got := clip("ééé", 3); got != "é…" {
		t.Errorf("clip(ééé, 3) = %q, want a whole rune plus the marker", got)
	}
}
