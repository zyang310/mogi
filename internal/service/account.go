package service

import (
	"context"
	"errors"
	"log"
	"strings"

	"mogi/internal/access"
	"mogi/internal/models"
)

// AccountStore is the slice of the data layer the account service needs: the
// managed key namespace, the session token/email/pinned-model rows, and the
// preferences (for the KeyMode flip). *store.DB satisfies it.
type AccountStore interface {
	GetPreferences() (models.Preferences, error)
	SavePreferences(p models.Preferences) error
	GetAPIKey(provider string) (string, error)     // BYOK namespace, for applyKeyMode
	GetManagedKey(provider string) (string, error) // managed namespace, for applyKeyMode
	SetManagedKey(provider, value string) error
	GetManagedSession() (string, error)
	SetManagedSession(token string) error
	SetManagedEmail(email string) error
	GetManagedPinnedModel() (string, error)
	SetManagedPinnedModel(model string) error
	DeleteManagedData() error
}

// AccessClient is the access-service surface the account service consumes.
// *access.Client satisfies it; tests substitute a fake so no HTTP happens.
type AccessClient interface {
	RequestCode(ctx context.Context, email, inviteCode string) error
	Verify(ctx context.Context, email, code string) (string, access.KeySet, error)
	Keys(ctx context.Context, token string) (access.KeySet, error)
}

// Account owns the managed test-account lifecycle: redeeming an invite,
// refreshing keys on launch (silent rotation + revocation enforcement), and
// signing out. It resolves keys into the live registry through the shared
// applyKeyMode rule, so managed and BYOK keys never clobber each other. status
// is borrowed from Settings.AuthStatus (as History borrows Interview.ActiveID)
// so "what the frontend sees" has a single implementation.
type Account struct {
	store     AccountStore
	providers *Providers
	client    AccessClient
	status    func() models.AuthStatus
}

// NewAccount wires the account service to its store, the live client registry,
// the access-service client, and the AuthStatus provider it returns to callers.
func NewAccount(store AccountStore, providers *Providers, client AccessClient, status func() models.AuthStatus) *Account {
	return &Account{store: store, providers: providers, client: client, status: status}
}

// RequestCode validates the invite and triggers the OTP email. Nothing changes
// locally until Activate — this is a thin pass-through to the access service.
func (a *Account) RequestCode(ctx context.Context, email, inviteCode string) error {
	return a.client.RequestCode(ctx, email, inviteCode)
}

// Activate verifies the OTP and, on success, stores the returned managed keys,
// pinned model, session token, and email; flips KeyMode to "managed"; and
// resolves the managed keys into the live registry — signing the device in
// without a restart. It returns the fresh AuthStatus so the frontend can
// re-render immediately. The keys never leave the backend.
func (a *Account) Activate(ctx context.Context, email, code string) (models.AuthStatus, error) {
	token, keys, err := a.client.Verify(ctx, email, code)
	if err != nil {
		return models.AuthStatus{}, err
	}
	if err := a.storeKeys(keys); err != nil {
		return models.AuthStatus{}, err
	}
	if err := a.store.SetManagedSession(token); err != nil {
		return models.AuthStatus{}, err
	}
	if err := a.store.SetManagedEmail(normalizeEmail(email)); err != nil {
		return models.AuthStatus{}, err
	}
	if err := a.setKeyMode("managed"); err != nil {
		return models.AuthStatus{}, err
	}
	a.ApplyMode()
	return a.status(), nil
}

// SignOut signs the device out of the managed tier: it deletes the managed keys,
// session token, email, and pinned model, flips KeyMode back to "byok", and
// re-resolves the registry to the user's own keys. Any BYOK keys the user pasted
// are untouched. Device-local in v1 — the server lever for real revocation is
// deleting the tester doc, which the launch refresh then enforces.
func (a *Account) SignOut() (models.AuthStatus, error) {
	if err := a.store.DeleteManagedData(); err != nil {
		return models.AuthStatus{}, err
	}
	if err := a.setKeyMode("byok"); err != nil {
		return models.AuthStatus{}, err
	}
	a.ApplyMode()
	return a.status(), nil
}

// Refresh re-fetches the managed key set on launch, implementing rotation,
// revocation enforcement, and offline grace:
//   - no session token → not a managed device, no-op.
//   - 200 → upsert the (possibly rotated) keys + pinned model and re-resolve the
//     registry, so running installs heal on next start. changed is true only if
//     the pinned model actually moved (the sole refresh-visible field).
//   - ErrUnauthorized (revoked / test phase ended) → purge managed data, flip to
//     BYOK, re-resolve, and return the server's message as the notice.
//   - any other error (network/service down) → keep the cached keys and proceed;
//     a backend blip must never brick a tester mid-prep.
//
// It returns whether something the UI cares about changed (so the caller emits
// an event only then) and a user-facing notice (non-empty only on sign-out).
func (a *Account) Refresh(ctx context.Context) (changed bool, notice string, err error) {
	token, err := a.store.GetManagedSession()
	if err != nil {
		return false, "", err
	}
	if token == "" {
		return false, "", nil // BYOK device — nothing to refresh.
	}

	keys, err := a.client.Keys(ctx, token)
	if err != nil {
		if errors.Is(err, access.ErrUnauthorized) {
			notice := signOutNotice(err)
			if _, serr := a.SignOut(); serr != nil {
				return false, "", serr
			}
			return true, notice, nil
		}
		// Transient failure — keep cached keys and carry on.
		log.Printf("account: launch key refresh failed, using cached keys: %v", err)
		return false, "", nil
	}

	oldPinned, _ := a.store.GetManagedPinnedModel()
	if err := a.storeKeys(keys); err != nil {
		return false, "", err
	}
	a.ApplyMode()
	return keys.PinnedModel != oldPinned, "", nil
}

// ApplyMode resolves the live provider registry from the stored keys for the
// current KeyMode. NewApp calls it once at startup — replacing the old
// three-provider restore loop — so a returning managed user comes up with their
// fetched keys live and a BYOK user comes up exactly as before. Activate,
// SignOut, and Refresh call it after they change the stored keys or mode.
func (a *Account) ApplyMode() {
	prefs, err := a.store.GetPreferences()
	if err != nil {
		log.Printf("account: read preferences for key resolution: %v", err)
		return
	}
	applyKeyMode(a.store, a.providers, prefs)
}

// storeKeys writes a fetched managed key set into the managed namespace. The
// pinned model rides along in the same set and is stored here too, so the
// interview service can read it back for model pinning.
func (a *Account) storeKeys(keys access.KeySet) error {
	if err := a.store.SetManagedKey("openrouter", keys.OpenRouter); err != nil {
		return err
	}
	if err := a.store.SetManagedKey("elevenlabs", keys.ElevenLabs); err != nil {
		return err
	}
	if err := a.store.SetManagedKey("google", keys.Google); err != nil {
		return err
	}
	return a.store.SetManagedPinnedModel(keys.PinnedModel)
}

// setKeyMode persists just the KeyMode preference via read-modify-write, leaving
// every other preference as the user left it.
func (a *Account) setKeyMode(mode string) error {
	prefs, err := a.store.GetPreferences()
	if err != nil {
		return err
	}
	prefs.KeyMode = mode
	return a.store.SavePreferences(prefs)
}

// keyResolver is the store slice applyKeyMode needs: read a provider's key from
// either namespace. Both AccountStore and SettingsStore satisfy it.
type keyResolver interface {
	GetAPIKey(provider string) (string, error)
	GetManagedKey(provider string) (string, error)
}

// applyKeyMode makes the live Providers registry reflect the store: it loads the
// key namespace the given preferences select (the managed keys when KeyMode is
// "managed", the user's BYOK keys otherwise) and pushes all three provider slots
// into the registry. It is the single key-resolution rule: every write path into
// keys or KeyMode — Settings.SetAPIKey/DeleteAPIKey/Update/ClearAllData and
// Account.Activate/SignOut/Refresh/ApplyMode — ends here, so the invariant
// "registry ≡ stored keys for the active mode" holds in exactly one place.
func applyKeyMode(st keyResolver, providers *Providers, prefs models.Preferences) {
	get := st.GetAPIKey
	if prefs.KeyMode == "managed" {
		get = st.GetManagedKey
	}
	for _, provider := range []string{"openrouter", "elevenlabs", "google"} {
		key, _ := get(provider)
		providers.SetKey(provider, key)
	}
}

// normalizeEmail canonicalizes an email for storage/display, matching the access
// service's own normalization so the stored email is the tester's identity.
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// signOutNotice recovers the server's human-readable message from an
// ErrUnauthorized-wrapped error, for display when the launch refresh signs the
// device out. Falls back to a generic line if the message can't be recovered.
func signOutNotice(err error) string {
	msg := strings.TrimPrefix(err.Error(), access.ErrUnauthorized.Error()+": ")
	if msg == "" || msg == err.Error() {
		return "Your test account is no longer active."
	}
	return msg
}
