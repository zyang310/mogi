package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"mogi/internal/access"
	"mogi/internal/models"
)

// managedState is an in-memory backing for account tests: it makes the fakeStore
// round-trip the managed keys, the session/email/pinned-model rows, the BYOK
// keys, and KeyMode, so a test can activate/sign-out/refresh and then read back
// the resulting state and AuthStatus.
type managedState struct {
	managedKeys map[string]string
	byokKeys    map[string]string
	session     string
	email       string
	pinned      string
	prefs       models.Preferences
}

// statefulStore wires a fakeStore to a managedState so managed writes are
// actually observable on the matching reads.
func statefulStore() (*fakeStore, *managedState) {
	ms := &managedState{
		managedKeys: map[string]string{},
		byokKeys:    map[string]string{},
		prefs:       models.Preferences{KeyMode: "byok"},
	}
	st := &fakeStore{
		getPreferences:        func() (models.Preferences, error) { return ms.prefs, nil },
		savePreferences:       func(p models.Preferences) error { ms.prefs = p; return nil },
		getAPIKey:             func(provider string) (string, error) { return ms.byokKeys[provider], nil },
		setAPIKey:             func(provider, value string) error { ms.byokKeys[provider] = value; return nil },
		deleteAPIKey:          func(provider string) error { delete(ms.byokKeys, provider); return nil },
		getManagedKey:         func(provider string) (string, error) { return ms.managedKeys[provider], nil },
		setManagedKey:         func(provider, value string) error { ms.managedKeys[provider] = value; return nil },
		getManagedSession:     func() (string, error) { return ms.session, nil },
		setManagedSession:     func(token string) error { ms.session = token; return nil },
		getManagedEmail:       func() (string, error) { return ms.email, nil },
		setManagedEmail:       func(email string) error { ms.email = email; return nil },
		getManagedPinnedModel: func() (string, error) { return ms.pinned, nil },
		setManagedPinnedModel: func(model string) error { ms.pinned = model; return nil },
		deleteManagedData: func() error {
			ms.managedKeys = map[string]string{}
			ms.session, ms.email, ms.pinned = "", "", ""
			return nil
		},
	}
	return st, ms
}

// accountWith builds an Account over a stateful store and fake access client,
// borrowing a real Settings.AuthStatus (over the same store) as the status
// provider — exactly the wiring NewApp uses.
func accountWith(st *fakeStore, client AccessClient) (*Account, *Providers) {
	p := NewProviders()
	settings := NewSettings(st, p, &fakeScreen{}, &fakeHotkey{})
	return NewAccount(st, p, client, settings.AuthStatus), p
}

// TestAccountRequestCode verifies the invite/email pass-through to the service.
func TestAccountRequestCode(t *testing.T) {
	var gotEmail, gotInvite string
	st, _ := statefulStore()
	acc, _ := accountWith(st, &fakeAccess{requestCode: func(email, inviteCode string) error {
		gotEmail, gotInvite = email, inviteCode
		return nil
	}})
	if err := acc.RequestCode(context.Background(), "a@b.com", "MOGI-DEV"); err != nil {
		t.Fatalf("RequestCode error: %v", err)
	}
	if gotEmail != "a@b.com" || gotInvite != "MOGI-DEV" {
		t.Errorf("RequestCode passed %q/%q, want the email + invite", gotEmail, gotInvite)
	}
}

// TestAccountActivate verifies the full sign-in: keys land in the managed
// namespace, the mode flips, the registry goes live, and the returned status
// reflects a signed-in managed account with a normalized email.
func TestAccountActivate(t *testing.T) {
	st, ms := statefulStore()
	client := &fakeAccess{verify: func(email, code string) (string, access.KeySet, error) {
		return "sess-tok", access.KeySet{OpenRouter: "sk-or", Google: "g", ElevenLabs: "el", PinnedModel: "pin"}, nil
	}}
	acc, p := accountWith(st, client)

	status, err := acc.Activate(context.Background(), "  Tester@Example.com ", "123456")
	if err != nil {
		t.Fatalf("Activate error: %v", err)
	}
	if ms.managedKeys["openrouter"] != "sk-or" || ms.managedKeys["google"] != "g" || ms.managedKeys["elevenlabs"] != "el" {
		t.Errorf("managed keys = %v, want the fetched set", ms.managedKeys)
	}
	if ms.pinned != "pin" || ms.session != "sess-tok" || ms.email != "tester@example.com" {
		t.Errorf("managed rows = pinned=%q session=%q email=%q, want stored + normalized email", ms.pinned, ms.session, ms.email)
	}
	if ms.prefs.KeyMode != "managed" {
		t.Errorf("KeyMode = %q, want managed", ms.prefs.KeyMode)
	}
	if p.AI() == nil || p.Google() == nil || p.ElevenLabs() == nil {
		t.Error("Activate must resolve the managed keys into the live registry")
	}
	if !status.ManagedActive || status.KeyMode != "managed" || status.ManagedEmail != "tester@example.com" {
		t.Errorf("returned status = %+v, want managed + active + email", status)
	}
}

// TestAccountActivateVerifyError verifies a failed OTP leaves all local state
// untouched — no half-written managed account.
func TestAccountActivateVerifyError(t *testing.T) {
	st, ms := statefulStore()
	acc, p := accountWith(st, &fakeAccess{verify: func(email, code string) (string, access.KeySet, error) {
		return "", access.KeySet{}, errors.New("incorrect code")
	}})
	if _, err := acc.Activate(context.Background(), "a@b.com", "000000"); err == nil {
		t.Fatal("Activate should surface the verify error")
	}
	if ms.session != "" || ms.prefs.KeyMode != "byok" || p.AI() != nil {
		t.Error("a failed verify must not change local state")
	}
}

// TestAccountSignOut verifies the device-local sign-out: managed data purged,
// mode back to byok, BYOK keys intact, registry re-resolved to BYOK.
func TestAccountSignOut(t *testing.T) {
	st, ms := statefulStore()
	ms.managedKeys = map[string]string{"openrouter": "sk-or", "google": "g", "elevenlabs": "el"}
	ms.byokKeys = map[string]string{"openrouter": "byok-or"}
	ms.session, ms.email, ms.pinned = "tok", "t@e.com", "pin"
	ms.prefs.KeyMode = "managed"

	acc, p := accountWith(st, &fakeAccess{})
	status, err := acc.SignOut()
	if err != nil {
		t.Fatalf("SignOut error: %v", err)
	}
	if ms.session != "" || len(ms.managedKeys) != 0 || ms.prefs.KeyMode != "byok" {
		t.Errorf("SignOut must purge managed data and flip to byok; state=%+v", ms)
	}
	if ms.byokKeys["openrouter"] != "byok-or" {
		t.Error("SignOut must leave BYOK keys untouched")
	}
	if p.AI() == nil {
		t.Error("SignOut must re-resolve the registry to the BYOK keys")
	}
	if status.ManagedActive || status.KeyMode != "byok" {
		t.Errorf("returned status = %+v, want signed out", status)
	}
}

// TestAccountRefreshNoToken verifies a BYOK device never calls the service.
func TestAccountRefreshNoToken(t *testing.T) {
	st, _ := statefulStore()
	acc, _ := accountWith(st, &fakeAccess{keys: func(string) (access.KeySet, error) {
		t.Fatal("Keys must not be called without a session token")
		return access.KeySet{}, nil
	}})
	changed, notice, err := acc.Refresh(context.Background())
	if changed || notice != "" || err != nil {
		t.Errorf("Refresh() = %v,%q,%v; want a no-op", changed, notice, err)
	}
}

// TestAccountRefreshRotates verifies a 200 upserts rotated keys and reports
// changed only when the pinned model actually moved.
func TestAccountRefreshRotates(t *testing.T) {
	st, ms := statefulStore()
	ms.session, ms.pinned, ms.prefs.KeyMode = "tok", "old-model", "managed"
	ms.managedKeys = map[string]string{"openrouter": "old"}
	acc, _ := accountWith(st, &fakeAccess{keys: func(token string) (access.KeySet, error) {
		return access.KeySet{OpenRouter: "new", Google: "g", ElevenLabs: "el", PinnedModel: "new-model"}, nil
	}})

	changed, notice, err := acc.Refresh(context.Background())
	if err != nil || notice != "" {
		t.Fatalf("Refresh() err=%v notice=%q", err, notice)
	}
	if !changed {
		t.Error("Refresh() changed=false, want true when the pinned model moved")
	}
	if ms.managedKeys["openrouter"] != "new" || ms.pinned != "new-model" {
		t.Errorf("Refresh must upsert rotated keys; state=%+v", ms)
	}
	// A second refresh with the same pinned model reports no change.
	if changed2, _, _ := acc.Refresh(context.Background()); changed2 {
		t.Error("Refresh() changed=true on an identical pinned model, want false")
	}
}

// TestAccountRefreshRevoked verifies ErrUnauthorized signs the device out and
// surfaces the server's message as the notice.
func TestAccountRefreshRevoked(t *testing.T) {
	st, ms := statefulStore()
	ms.session, ms.prefs.KeyMode = "tok", "managed"
	ms.managedKeys = map[string]string{"openrouter": "sk"}
	ms.byokKeys = map[string]string{"openrouter": "byok"}
	acc, p := accountWith(st, &fakeAccess{keys: func(token string) (access.KeySet, error) {
		return access.KeySet{}, fmt.Errorf("%w: this test account has been deactivated", access.ErrUnauthorized)
	}})

	changed, notice, err := acc.Refresh(context.Background())
	if err != nil {
		t.Fatalf("Refresh() error: %v", err)
	}
	if !changed || notice != "this test account has been deactivated" {
		t.Errorf("Refresh() changed=%v notice=%q; want signed out with the server message", changed, notice)
	}
	if ms.session != "" || ms.prefs.KeyMode != "byok" {
		t.Error("revocation must sign the device out")
	}
	if p.AI() == nil {
		t.Error("after revocation the registry must resolve the BYOK keys")
	}
}

// TestAccountRefreshTransientKeepsKeys verifies a network/service failure keeps
// the cached keys and stays signed in (offline grace).
func TestAccountRefreshTransientKeepsKeys(t *testing.T) {
	st, ms := statefulStore()
	ms.session, ms.pinned, ms.prefs.KeyMode = "tok", "model", "managed"
	ms.managedKeys = map[string]string{"openrouter": "sk"}
	acc, _ := accountWith(st, &fakeAccess{keys: func(token string) (access.KeySet, error) {
		return access.KeySet{}, errors.New("network down")
	}})

	changed, notice, err := acc.Refresh(context.Background())
	if changed || notice != "" || err != nil {
		t.Errorf("Refresh() = %v,%q,%v; want a no-op on transient failure", changed, notice, err)
	}
	if ms.session != "tok" || ms.managedKeys["openrouter"] != "sk" || ms.prefs.KeyMode != "managed" {
		t.Error("a transient failure must keep cached keys and stay signed in")
	}
}
