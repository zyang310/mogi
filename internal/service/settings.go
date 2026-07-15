package service

import (
	"context"
	"fmt"

	"mogi/internal/hotkey"
	"mogi/internal/models"
)

// SettingsStore is the slice of the data layer the settings service needs.
// *store.DB satisfies it.
type SettingsStore interface {
	GetAPIKey(provider string) (string, error)
	SetAPIKey(provider, value string) error
	DeleteAPIKey(provider string) error
	GetManagedKey(provider string) (string, error)
	GetManagedSession() (string, error)
	GetManagedEmail() (string, error)
	GetManagedPinnedModel() (string, error)
	GetPreferences() (models.Preferences, error)
	SavePreferences(p models.Preferences) error
	ListStarredCompanies() ([]string, error)
	SetCompanyStarred(slug string, starred bool) error
	ClearAll() error
}

// HotkeyApplier is the control surface of the global push-to-talk hook.
// *hotkey.Listener satisfies it; tests use a fake so no OS keyboard hook is
// ever installed.
type HotkeyApplier interface {
	Apply(ctx context.Context, enabled bool, spec hotkey.Spec)
}

// Settings owns API keys and preferences — including their propagation into
// running infrastructure. "Saving prefs must retarget the capturer and
// re-apply the hotkey" is a business invariant, so it lives here once rather
// than in each caller.
type Settings struct {
	store     SettingsStore
	providers *Providers
	screen    Screen
	hotkey    HotkeyApplier
}

// NewSettings wires the settings service to the key/preference store, the live
// client registry, and the infrastructure it keeps in sync.
func NewSettings(store SettingsStore, providers *Providers, screen Screen, hk HotkeyApplier) *Settings {
	return &Settings{store: store, providers: providers, screen: screen, hotkey: hk}
}

// SetAPIKey stores a BYOK API key for the given provider ("openrouter",
// "elevenlabs", or "google"). In BYOK mode it activates the key immediately (no
// restart). In managed mode the live registry is fed by the managed namespace,
// so the key is persisted — ready for when the user switches back — without
// disturbing the running managed clients.
func (s *Settings) SetAPIKey(provider, key string) error {
	if err := s.store.SetAPIKey(provider, key); err != nil {
		return err
	}
	if !s.managedMode() {
		s.providers.SetKey(provider, key)
	}
	return nil
}

// DeleteAPIKey removes the stored BYOK key for the given provider. In BYOK mode
// it deactivates the client immediately, so STT/TTS provider resolution falls
// back to whatever remains configured. In managed mode the registry is fed by
// the managed namespace, so only the stored BYOK key is dropped; the running
// managed clients are left alone.
func (s *Settings) DeleteAPIKey(provider string) error {
	if err := s.store.DeleteAPIKey(provider); err != nil {
		return err
	}
	if !s.managedMode() {
		s.providers.SetKey(provider, "") // empty key deactivates the slot
	}
	return nil
}

// managedMode reports whether KeyMode is "managed" — i.e. the live registry is
// fed by the managed namespace, so BYOK key writes must not touch it.
func (s *Settings) managedMode() bool {
	prefs, _ := s.store.GetPreferences()
	return prefs.KeyMode == "managed"
}

// ClearAllData wipes every piece of local state — sessions, transcripts,
// preferences, API keys (both BYOK and managed), and starred companies —
// returning the app to a first-run state. Because the wipe drops the managed
// rows and resets KeyMode to the "byok" default, it also signs the device out
// of the managed tier. It then deactivates every provider client (their keys are
// gone) and re-applies the now-default hotkey and capture region, so the running
// app matches the reset store without a restart. Emptying the registry is the
// correct post-wipe state: byok mode with no keys. Destructive and irreversible;
// callers gate it behind an explicit confirmation.
func (s *Settings) ClearAllData(ctx context.Context) error {
	if err := s.store.ClearAll(); err != nil {
		return err
	}
	// Both key namespaces lived in the wiped preferences table — drop the live
	// clients too (managed and BYOK are both empty now, and the mode is byok).
	for _, provider := range []string{"openrouter", "elevenlabs", "google"} {
		s.providers.SetKey(provider, "")
	}
	// Preferences now read back as defaults; re-sync the infra they drive.
	s.ApplySavedRegion()
	s.ApplyHotkey(ctx)
	return nil
}

// AuthStatus reports which API providers currently have keys configured, plus
// the managed test-account state. It reads the key store — the source of truth —
// rather than the live registry. In managed mode the three *Configured bools
// reflect the managed namespace, so the SetupPage gate passes on the fetched
// keys; the managed session/email/pinned-model fields are reported regardless of
// mode so the UI can surface "signed in to the test account" even after a switch
// back to BYOK.
func (s *Settings) AuthStatus() models.AuthStatus {
	prefs, _ := s.store.GetPreferences()

	getKey := s.store.GetAPIKey
	if prefs.KeyMode == "managed" {
		getKey = s.store.GetManagedKey
	}
	orKey, _ := getKey("openrouter")
	elKey, _ := getKey("elevenlabs")
	googleKey, _ := getKey("google")

	token, _ := s.store.GetManagedSession()
	email, _ := s.store.GetManagedEmail()
	pinnedModel, _ := s.store.GetManagedPinnedModel()

	return models.AuthStatus{
		OpenRouterConfigured: orKey != "",
		ElevenLabsConfigured: elKey != "",
		GoogleConfigured:     googleKey != "",
		KeyMode:              prefs.KeyMode,
		ManagedActive:        token != "",
		ManagedEmail:         email,
		PinnedModel:          pinnedModel,
	}
}

// Preferences returns the user's settings.
func (s *Settings) Preferences() (models.Preferences, error) {
	return s.store.GetPreferences()
}

// StarredCompanies returns the slugs of the companies the user starred in the
// Company Practice picker, alphabetically.
func (s *Settings) StarredCompanies() ([]string, error) {
	return s.store.ListStarredCompanies()
}

// SetCompanyStarred stars (true) or unstars (false) a company in the picker.
// Idempotent. Slugs are kept even if a later dataset refresh drops the company —
// the UI simply ignores slugs it can't resolve.
func (s *Settings) SetCompanyStarred(slug string, starred bool) error {
	if slug == "" {
		return fmt.Errorf("settings: star company: empty slug")
	}
	return s.store.SetCompanyStarred(slug, starred)
}

// Update persists updated settings and propagates them into the running
// infrastructure. ctx must be the Wails context — the hotkey listener retains
// it for emitting ptt events.
func (s *Settings) Update(ctx context.Context, prefs models.Preferences) error {
	old, _ := s.store.GetPreferences()
	if err := s.store.SavePreferences(prefs); err != nil {
		return err
	}
	// A KeyMode flip is the one preference change that swaps which key namespace
	// feeds the live registry (this is how the frontend's "switch to my own keys"
	// / "switch back" toggles land). Re-resolve only then, so an ordinary settings
	// save never touches the providers.
	if old.KeyMode != prefs.KeyMode {
		applyKeyMode(s.store, s.providers, prefs)
	}
	// Keep the capturer in sync with any region/display change.
	s.screen.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
	// Enable/disable/re-key the global push-to-talk hook to match the new prefs.
	s.ApplyHotkey(ctx)
	return nil
}

// ApplyHotkey applies the saved push-to-talk preferences to the global hook.
// The hook starts on first enable and is never restarted — enabling, disabling,
// and rebinding all flow through Apply, which swaps guarded fields on the
// running hook. Best-effort — a bad/empty key falls back to the default.
func (s *Settings) ApplyHotkey(ctx context.Context) {
	prefs, err := s.store.GetPreferences()
	if err != nil {
		return
	}
	spec, perr := hotkey.ParseSpec(prefs.PushToTalkKey)
	if perr != nil {
		spec, _ = hotkey.ParseSpec(hotkey.DefaultSpec)
	}
	s.hotkey.Apply(ctx, prefs.PushToTalkEnabled, spec)
}

// ApplySavedRegion loads the persisted capture display/region and applies it to
// the capturer, so on-demand captures honour it before any session starts.
// Best-effort: falls back to the full primary display on any error.
func (s *Settings) ApplySavedRegion() {
	prefs, err := s.store.GetPreferences()
	if err != nil {
		return
	}
	s.screen.SetRegion(prefs.CaptureDisplay, prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH)
}

// SetCaptureRegion persists the chosen display and sub-region (fractions 0..1
// of the display; a zero width means full display) and applies it to the
// capturer.
func (s *Settings) SetCaptureRegion(displayIndex int, x, y, w, h float64) error {
	prefs, err := s.store.GetPreferences()
	if err != nil {
		return err
	}
	prefs.CaptureDisplay = displayIndex
	prefs.RegionX, prefs.RegionY, prefs.RegionW, prefs.RegionH = x, y, w, h
	if err := s.store.SavePreferences(prefs); err != nil {
		return err
	}
	s.screen.SetRegion(displayIndex, x, y, w, h)
	return nil
}

// ListModels returns the OpenRouter model catalog for the Settings picker.
// Saving a choice needs no service call — the picker writes the selected id to
// Preferences.Model through Update.
func (s *Settings) ListModels(ctx context.Context) ([]models.Model, error) {
	aiClient := s.providers.AI()
	if aiClient == nil {
		return nil, fmt.Errorf("set an OpenRouter API key first")
	}
	return aiClient.ListModels(ctx)
}
