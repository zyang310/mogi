package store

import "fmt"

// Managed test-account rows. These live in the same preferences table as the
// BYOK keys but under a separate "managed_" namespace, so developer-funded keys
// fetched from the access service and the user's own pasted keys sit side by
// side and never clobber each other (see docs/managed-keys-plan.md). A single
// KeyMode preference decides which namespace the service layer resolves into the
// live client registry.
const (
	keyManagedOpenRouterAPIKey = "managed_openrouter_api_key"
	keyManagedElevenLabsAPIKey = "managed_elevenlabs_api_key"
	keyManagedGoogleAPIKey     = "managed_google_api_key"
	keyManagedSessionToken     = "managed_session_token"
	keyManagedEmail            = "managed_email"
	keyManagedPinnedModel      = "managed_pinned_model"
)

// GetManagedKey retrieves a managed API key. provider is "openrouter",
// "elevenlabs", or "google". Returns "" (no error) if not set.
func (db *DB) GetManagedKey(provider string) (string, error) {
	key := managedProviderKey(provider)
	if key == "" {
		return "", fmt.Errorf("store: unknown provider %q", provider)
	}
	return db.getPref(key)
}

// SetManagedKey persists a managed API key for the given provider.
func (db *DB) SetManagedKey(provider, value string) error {
	key := managedProviderKey(provider)
	if key == "" {
		return fmt.Errorf("store: unknown provider %q", provider)
	}
	return db.setPref(key, value)
}

// GetManagedSession returns the stored access-service session token, or "" when
// the device isn't signed in to the managed tier.
func (db *DB) GetManagedSession() (string, error) {
	return db.getPref(keyManagedSessionToken)
}

// SetManagedSession persists the access-service session token used by the
// launch-time key refresh.
func (db *DB) SetManagedSession(token string) error {
	return db.setPref(keyManagedSessionToken, token)
}

// GetManagedEmail returns the email the tester activated with, shown in the
// managed-account card. "" when not signed in.
func (db *DB) GetManagedEmail() (string, error) {
	return db.getPref(keyManagedEmail)
}

// SetManagedEmail persists the tester's email for display.
func (db *DB) SetManagedEmail(email string) error {
	return db.setPref(keyManagedEmail, email)
}

// GetManagedPinnedModel returns the server-pinned model for the managed tier, or
// "" when not signed in. The interview service pins new sessions to it.
func (db *DB) GetManagedPinnedModel() (string, error) {
	return db.getPref(keyManagedPinnedModel)
}

// SetManagedPinnedModel persists the server-pinned model returned by the access
// service; the launch refresh can update it when the developer swaps the model.
func (db *DB) SetManagedPinnedModel(model string) error {
	return db.setPref(keyManagedPinnedModel, model)
}

// DeleteManagedData removes every managed-account row — the fetched keys, the
// session token, the tester email, and the pinned model — signing the device out
// of the managed tier while leaving BYOK keys and all other preferences intact.
// Used by sign-out and by the launch refresh's revocation path. Loops
// deletePref rather than issuing raw SQL, so it stays on the shared primitives.
func (db *DB) DeleteManagedData() error {
	for _, key := range []string{
		keyManagedOpenRouterAPIKey,
		keyManagedElevenLabsAPIKey,
		keyManagedGoogleAPIKey,
		keyManagedSessionToken,
		keyManagedEmail,
		keyManagedPinnedModel,
	} {
		if err := db.deletePref(key); err != nil {
			return err
		}
	}
	return nil
}

// managedProviderKey maps a provider name to its managed-namespace preference
// key, mirroring providerKey for the BYOK namespace.
func managedProviderKey(provider string) string {
	switch provider {
	case "openrouter":
		return keyManagedOpenRouterAPIKey
	case "elevenlabs":
		return keyManagedElevenLabsAPIKey
	case "google":
		return keyManagedGoogleAPIKey
	}
	return ""
}
