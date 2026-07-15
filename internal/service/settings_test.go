package service

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"mogi/internal/hotkey"
	"mogi/internal/models"
)

// settingsWith builds a Settings service over fakes, returning the fakes for
// assertions.
func settingsWith(st *fakeStore) (*Settings, *Providers, *fakeScreen, *fakeHotkey) {
	p := NewProviders()
	screen := &fakeScreen{}
	hk := &fakeHotkey{}
	return NewSettings(st, p, screen, hk), p, screen, hk
}

// TestUpdatePropagates verifies the business invariant: saving preferences
// retargets the capturer and re-applies the hotkey in one call.
func TestUpdatePropagates(t *testing.T) {
	var saved models.Preferences
	st := &fakeStore{
		savePreferences: func(p models.Preferences) error { saved = p; return nil },
		// ApplyHotkey re-reads the store, so echo back what was saved.
		getPreferences: func() (models.Preferences, error) { return saved, nil },
	}
	s, _, screen, hk := settingsWith(st)

	prefs := models.Preferences{
		CaptureDisplay: 1, RegionX: 0.1, RegionY: 0.2, RegionW: 0.3, RegionH: 0.4,
		PushToTalkEnabled: true, PushToTalkKey: "F8",
	}
	if err := s.Update(context.Background(), prefs); err != nil {
		t.Fatalf("Update() error: %v", err)
	}
	if saved.CaptureDisplay != 1 {
		t.Error("preferences were not saved")
	}
	if len(screen.regions) != 1 || screen.regions[0] != [5]float64{1, 0.1, 0.2, 0.3, 0.4} {
		t.Errorf("capturer regions = %v, want the new region applied", screen.regions)
	}
	if len(hk.applies) != 1 || !hk.applies[0].enabled || hk.applies[0].spec.String() != "F8" {
		t.Errorf("hotkey applies = %+v, want enabled with the saved key", hk.applies)
	}
}

// TestApplyHotkeyFallsBackToDefault verifies an unparseable saved key degrades
// to the default spec instead of leaving the hook unbound.
func TestApplyHotkeyFallsBackToDefault(t *testing.T) {
	st := &fakeStore{getPreferences: func() (models.Preferences, error) {
		return models.Preferences{PushToTalkEnabled: true, PushToTalkKey: "NotAKey+Nope"}, nil
	}}
	s, _, _, hk := settingsWith(st)

	s.ApplyHotkey(context.Background())
	if len(hk.applies) != 1 || hk.applies[0].spec.String() != hotkey.DefaultSpec {
		t.Errorf("hotkey applies = %+v, want the default spec fallback", hk.applies)
	}
}

// TestAPIKeysFlipRegistry verifies key writes activate/deactivate the live
// client slots, and that a failed store write leaves the registry untouched.
func TestAPIKeysFlipRegistry(t *testing.T) {
	s, p, _, _ := settingsWith(&fakeStore{})

	if err := s.SetAPIKey("openrouter", "k"); err != nil || p.AI() == nil {
		t.Errorf("SetAPIKey: err=%v, want a live AI client", err)
	}
	if err := s.DeleteAPIKey("openrouter"); err != nil || p.AI() != nil {
		t.Errorf("DeleteAPIKey: err=%v, want the AI slot cleared", err)
	}

	failing := &fakeStore{setAPIKey: func(string, string) error { return errors.New("disk full") }}
	s2, p2, _, _ := settingsWith(failing)
	if err := s2.SetAPIKey("openrouter", "k"); err == nil {
		t.Error("SetAPIKey should surface the store error")
	}
	if p2.AI() != nil {
		t.Error("a failed store write must not activate the client")
	}
}

// TestClearAllData verifies the destructive wipe clears the store, deactivates
// every provider client, and re-syncs the hotkey + capture region from the
// now-default preferences — all in one call.
func TestClearAllData(t *testing.T) {
	cleared := false
	st := &fakeStore{clearAll: func() error { cleared = true; return nil }}
	s, p, screen, hk := settingsWith(st)

	// Seed live clients so we can prove they get deactivated.
	p.SetKey("openrouter", "k")
	p.SetKey("elevenlabs", "k")
	p.SetKey("google", "k")

	if err := s.ClearAllData(context.Background()); err != nil {
		t.Fatalf("ClearAllData() error: %v", err)
	}
	if !cleared {
		t.Error("ClearAllData must call store.ClearAll")
	}
	if p.AI() != nil || p.ElevenLabs() != nil || p.Google() != nil {
		t.Error("ClearAllData must deactivate every provider client")
	}
	if len(screen.regions) != 1 {
		t.Errorf("ClearAllData must re-apply the capture region, got %d applies", len(screen.regions))
	}
	if len(hk.applies) != 1 {
		t.Errorf("ClearAllData must re-apply the hotkey, got %d applies", len(hk.applies))
	}
}

// TestClearAllDataStoreError surfaces a store failure and leaves the live
// clients untouched (a failed wipe must not half-reset the app).
func TestClearAllDataStoreError(t *testing.T) {
	st := &fakeStore{clearAll: func() error { return errors.New("db locked") }}
	s, p, _, _ := settingsWith(st)
	p.SetKey("openrouter", "k")

	if err := s.ClearAllData(context.Background()); err == nil {
		t.Error("ClearAllData should surface the store error")
	}
	if p.AI() == nil {
		t.Error("a failed wipe must not deactivate clients")
	}
}

// TestAuthStatus maps the three stored keys onto the status struct.
func TestAuthStatus(t *testing.T) {
	st := &fakeStore{getAPIKey: func(provider string) (string, error) {
		if provider == "google" {
			return "gkey", nil
		}
		return "", nil
	}}
	s, _, _, _ := settingsWith(st)

	status := s.AuthStatus()
	if status.OpenRouterConfigured || status.ElevenLabsConfigured || !status.GoogleConfigured {
		t.Errorf("AuthStatus() = %+v, want only Google configured", status)
	}
}

// TestUpdateKeyModeFlipReresolves verifies a KeyMode flip re-resolves the live
// registry to the other namespace, and that an ordinary Update (no mode change)
// leaves the registry untouched.
func TestUpdateKeyModeFlipReresolves(t *testing.T) {
	st, ms := statefulStore()
	ms.byokKeys = map[string]string{"openrouter": "byok-or"}
	ms.managedKeys = map[string]string{"google": "mg-g"}
	s, p, _, _ := settingsWith(st)

	// Start resolved to the BYOK namespace, as NewApp's ApplyMode would.
	applyKeyMode(st, p, ms.prefs)
	if p.AI() == nil || p.Google() != nil {
		t.Fatal("byok resolution: want AI live from the byok key, google empty")
	}

	// Flip to managed → registry re-resolves to the managed namespace.
	managed := ms.prefs
	managed.KeyMode = "managed"
	if err := s.Update(context.Background(), managed); err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if p.AI() != nil || p.Google() == nil {
		t.Error("after flip to managed: want google live from the managed key, openrouter empty")
	}

	// A non-flip Update must not re-resolve: seed a slot the store wouldn't
	// resolve, then confirm it survives the save.
	p.SetKey("elevenlabs", "sentinel")
	if err := s.Update(context.Background(), managed); err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if p.ElevenLabs() == nil {
		t.Error("an Update without a KeyMode flip must not re-resolve the registry")
	}
}

// TestSetAPIKeyInManagedModeLeavesRegistry verifies BYOK key writes in managed
// mode persist to the store but never touch the live (managed) registry.
func TestSetAPIKeyInManagedModeLeavesRegistry(t *testing.T) {
	st, ms := statefulStore()
	ms.prefs.KeyMode = "managed"
	s, p, _, _ := settingsWith(st)

	if err := s.SetAPIKey("openrouter", "byok-or"); err != nil {
		t.Fatalf("SetAPIKey error: %v", err)
	}
	if ms.byokKeys["openrouter"] != "byok-or" {
		t.Error("SetAPIKey must persist the BYOK key even in managed mode")
	}
	if p.AI() != nil {
		t.Error("SetAPIKey in managed mode must not activate the live client")
	}

	// A live managed client must survive a BYOK delete in managed mode.
	p.SetKey("google", "managed-live")
	if err := s.DeleteAPIKey("google"); err != nil {
		t.Fatalf("DeleteAPIKey error: %v", err)
	}
	if p.Google() == nil {
		t.Error("DeleteAPIKey in managed mode must not deactivate the managed client")
	}
}

// TestClearAllSignsOutManaged verifies wiping local data signs the device out of
// the managed tier: the managed client goes dead and AuthStatus reports byok.
func TestClearAllSignsOutManaged(t *testing.T) {
	st, ms := statefulStore()
	ms.managedKeys = map[string]string{"openrouter": "sk"}
	ms.session, ms.prefs.KeyMode = "tok", "managed"
	st.clearAll = func() error { // the real ClearAll wipes the whole preferences table
		ms.managedKeys = map[string]string{}
		ms.byokKeys = map[string]string{}
		ms.session, ms.email, ms.pinned = "", "", ""
		ms.prefs = models.Preferences{KeyMode: "byok"}
		return nil
	}
	s, p, _, _ := settingsWith(st)
	p.SetKey("openrouter", "sk") // managed client was live

	if err := s.ClearAllData(context.Background()); err != nil {
		t.Fatalf("ClearAllData error: %v", err)
	}
	if p.AI() != nil {
		t.Error("ClearAllData must deactivate the managed client")
	}
	if status := s.AuthStatus(); status.ManagedActive || status.KeyMode != "byok" {
		t.Errorf("after ClearAllData, status = %+v, want signed out (byok)", status)
	}
}

// TestAuthStatusManagedMode verifies that in managed mode the *Configured bools
// reflect the managed namespace (not the empty BYOK one), and the managed
// account fields are surfaced for the UI.
func TestAuthStatusManagedMode(t *testing.T) {
	st := &fakeStore{
		getPreferences: func() (models.Preferences, error) {
			return models.Preferences{KeyMode: "managed"}, nil
		},
		// BYOK namespace is empty; only the managed namespace has keys.
		getAPIKey: func(string) (string, error) { return "", nil },
		getManagedKey: func(provider string) (string, error) {
			return "managed-" + provider, nil
		},
		getManagedSession:     func() (string, error) { return "tok", nil },
		getManagedEmail:       func() (string, error) { return "tester@example.com", nil },
		getManagedPinnedModel: func() (string, error) { return "google/gemini-2.5-flash", nil },
	}
	s, _, _, _ := settingsWith(st)

	status := s.AuthStatus()
	if !status.OpenRouterConfigured || !status.ElevenLabsConfigured || !status.GoogleConfigured {
		t.Errorf("AuthStatus() = %+v, want all providers configured from the managed namespace", status)
	}
	if status.KeyMode != "managed" || !status.ManagedActive {
		t.Errorf("AuthStatus() = %+v, want managed mode + active session", status)
	}
	if status.ManagedEmail != "tester@example.com" || status.PinnedModel != "google/gemini-2.5-flash" {
		t.Errorf("AuthStatus() = %+v, want managed email + pinned model surfaced", status)
	}
}

// TestSetCaptureRegion verifies the read-modify-write: the region fields change,
// the rest of the preferences survive, and the capturer is retargeted.
func TestSetCaptureRegion(t *testing.T) {
	var saved models.Preferences
	st := &fakeStore{
		getPreferences: func() (models.Preferences, error) {
			return models.Preferences{Model: "keep-me", SessionLimitMinutes: 30}, nil
		},
		savePreferences: func(p models.Preferences) error { saved = p; return nil },
	}
	s, _, screen, _ := settingsWith(st)

	if err := s.SetCaptureRegion(2, 0.1, 0.2, 0.3, 0.4); err != nil {
		t.Fatalf("SetCaptureRegion() error: %v", err)
	}
	if saved.Model != "keep-me" || saved.SessionLimitMinutes != 30 {
		t.Errorf("unrelated preferences were clobbered: %+v", saved)
	}
	if saved.CaptureDisplay != 2 || saved.RegionX != 0.1 || saved.RegionH != 0.4 {
		t.Errorf("region not persisted: %+v", saved)
	}
	if len(screen.regions) != 1 || screen.regions[0] != [5]float64{2, 0.1, 0.2, 0.3, 0.4} {
		t.Errorf("capturer regions = %v, want the new region", screen.regions)
	}
}

// TestApplySavedRegion verifies startup region restoration is best-effort.
func TestApplySavedRegion(t *testing.T) {
	st := &fakeStore{getPreferences: func() (models.Preferences, error) {
		return models.Preferences{CaptureDisplay: 3, RegionW: 0.5}, nil
	}}
	s, _, screen, _ := settingsWith(st)
	s.ApplySavedRegion()
	if len(screen.regions) != 1 || screen.regions[0] != [5]float64{3, 0, 0, 0.5, 0} {
		t.Errorf("regions = %v, want the saved region", screen.regions)
	}

	failing := &fakeStore{getPreferences: func() (models.Preferences, error) {
		return models.Preferences{}, errors.New("no prefs yet")
	}}
	s2, _, screen2, _ := settingsWith(failing)
	s2.ApplySavedRegion() // must not panic
	if len(screen2.regions) != 0 {
		t.Error("a failed read must not touch the capturer")
	}
}

// TestCompanyStarring verifies the star/unstar round-trip and the empty-slug
// guard.
func TestCompanyStarring(t *testing.T) {
	starred := map[string]bool{}
	st := &fakeStore{
		setCompanyStarred: func(slug string, on bool) error {
			if on {
				starred[slug] = true
			} else {
				delete(starred, slug)
			}
			return nil
		},
		listStarredCompanies: func() ([]string, error) {
			slugs := make([]string, 0, len(starred))
			for s := range starred {
				slugs = append(slugs, s)
			}
			sort.Strings(slugs)
			return slugs, nil
		},
	}
	s, _, _, _ := settingsWith(st)

	if err := s.SetCompanyStarred("meta", true); err != nil {
		t.Fatalf("star meta: %v", err)
	}
	if err := s.SetCompanyStarred("google", true); err != nil {
		t.Fatalf("star google: %v", err)
	}
	got, err := s.StarredCompanies()
	if err != nil || !reflect.DeepEqual(got, []string{"google", "meta"}) {
		t.Errorf("StarredCompanies() = %v, %v; want [google meta]", got, err)
	}

	if err := s.SetCompanyStarred("google", false); err != nil {
		t.Fatalf("unstar google: %v", err)
	}
	if got, _ := s.StarredCompanies(); !reflect.DeepEqual(got, []string{"meta"}) {
		t.Errorf("after unstar, StarredCompanies() = %v, want [meta]", got)
	}

	if err := s.SetCompanyStarred("", true); err == nil {
		t.Error("SetCompanyStarred(\"\") should reject the empty slug")
	}
	if len(starred) != 1 {
		t.Errorf("empty-slug call must not touch the store; starred = %v", starred)
	}
}
