package access

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRequestCode verifies /activate posts the email + invite and treats 204 as
// success, and that a non-2xx surfaces the server's error message.
func TestRequestCode(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/activate" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := NewClient(srv.URL).RequestCode(context.Background(), "a@b.com", "MOGI-DEV"); err != nil {
		t.Fatalf("RequestCode() error: %v", err)
	}
	if gotBody["email"] != "a@b.com" || gotBody["inviteCode"] != "MOGI-DEV" {
		t.Errorf("request body = %v, want email + inviteCode", gotBody)
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or exhausted invite code"})
	}))
	defer bad.Close()
	err := NewClient(bad.URL).RequestCode(context.Background(), "a@b.com", "WRONG")
	if err == nil || err.Error() != "access: invalid or exhausted invite code" {
		t.Errorf("RequestCode() err = %v, want the server message surfaced", err)
	}
}

// TestVerify verifies /verify returns the token and the nested key set.
func TestVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/verify" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"token": "sess-tok",
			"keys": KeySet{
				OpenRouter:  "sk-or",
				Google:      "g-key",
				ElevenLabs:  "el-key",
				PinnedModel: "google/gemini-2.5-flash",
			},
		})
	}))
	defer srv.Close()

	token, keys, err := NewClient(srv.URL).Verify(context.Background(), "a@b.com", "123456")
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if token != "sess-tok" {
		t.Errorf("token = %q, want sess-tok", token)
	}
	if keys.OpenRouter != "sk-or" || keys.Google != "g-key" || keys.ElevenLabs != "el-key" || keys.PinnedModel != "google/gemini-2.5-flash" {
		t.Errorf("keys = %+v, want the full set decoded from the nested payload", keys)
	}
}

// TestKeys verifies /keys sends the bearer token and decodes the flat key set,
// and that 401/403 both wrap ErrUnauthorized while preserving the message.
func TestKeys(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("Authorization = %q, want Bearer tok", got)
		}
		writeJSON(w, http.StatusOK, KeySet{OpenRouter: "sk-or", PinnedModel: "m"})
	}))
	defer srv.Close()

	ks, err := NewClient(srv.URL).Keys(context.Background(), "tok")
	if err != nil {
		t.Fatalf("Keys() error: %v", err)
	}
	if ks.OpenRouter != "sk-or" || ks.PinnedModel != "m" {
		t.Errorf("keys = %+v, want the flat set decoded", ks)
	}

	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		msg := "the Mogi test phase has ended — thanks for testing!"
		bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		}))
		_, err := NewClient(bad.URL).Keys(context.Background(), "tok")
		bad.Close()
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("status %d: err = %v, want ErrUnauthorized", status, err)
		}
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("status %d: err = %v, want the server message preserved", status, err)
		}
	}
}

// writeJSON is a tiny test helper mirroring the server's response shape.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
