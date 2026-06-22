package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"ai-interviewer/internal/models"
)

const (
	keyOpenRouterAPIKey    = "openrouter_api_key"
	keyElevenLabsAPIKey    = "elevenlabs_api_key"
	keyCaptureIntervalMs   = "capture_interval_ms"
	keyModel               = "model"
	keyVoiceID             = "voice_id"
	keyVoiceSpeed          = "voice_speed"
	keyCaptureDisplay      = "capture_display"
	keyRegionX             = "region_x"
	keyRegionY             = "region_y"
	keyRegionW             = "region_w"
	keyRegionH             = "region_h"
	keySessionLimitMinutes = "session_limit_minutes"
	keySoftWarningMinutes  = "soft_warning_minutes"
)

// GetAPIKey retrieves a stored API key. provider is "openrouter" or "elevenlabs".
// Returns empty string (no error) if not set.
//
// TODO(phase-4): encrypt stored keys using an OS keychain or AES-GCM before
// persisting — right now keys are stored as plain text in SQLite.
func (db *DB) GetAPIKey(provider string) (string, error) {
	key := providerKey(provider)
	if key == "" {
		return "", fmt.Errorf("store: unknown provider %q", provider)
	}
	return db.getPref(key)
}

// SetAPIKey persists an API key for the given provider.
func (db *DB) SetAPIKey(provider, value string) error {
	key := providerKey(provider)
	if key == "" {
		return fmt.Errorf("store: unknown provider %q", provider)
	}
	return db.setPref(key, value)
}

// GetPreferences returns all user preferences, using defaults for missing keys.
func (db *DB) GetPreferences() (models.Preferences, error) {
	p := models.Preferences{
		CaptureIntervalMs:   3000,
		Model:               "anthropic/claude-sonnet-4",
		VoiceSpeed:          1.0,
		SessionLimitMinutes: 30,
		SoftWarningMinutes:  25,
	}

	if v, err := db.getPref(keyCaptureIntervalMs); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.CaptureIntervalMs = n
		}
	}
	if v, err := db.getPref(keyModel); err == nil && v != "" {
		p.Model = v
	}
	if v, err := db.getPref(keyVoiceID); err == nil {
		p.VoiceID = v
	}
	// Guard > 0 so a missing/zero stored value keeps the 1.0 default.
	if v, err := db.getPref(keyVoiceSpeed); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			p.VoiceSpeed = f
		}
	}
	if v, err := db.getPref(keyCaptureDisplay); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.CaptureDisplay = n
		}
	}
	if v, err := db.getPref(keyRegionX); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.RegionX = f
		}
	}
	if v, err := db.getPref(keyRegionY); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.RegionY = f
		}
	}
	if v, err := db.getPref(keyRegionW); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.RegionW = f
		}
	}
	if v, err := db.getPref(keyRegionH); err == nil && v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			p.RegionH = f
		}
	}
	if v, err := db.getPref(keySessionLimitMinutes); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.SessionLimitMinutes = n
		}
	}
	if v, err := db.getPref(keySoftWarningMinutes); err == nil && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p.SoftWarningMinutes = n
		}
	}
	return p, nil
}

// SavePreferences persists all fields of a Preferences struct.
func (db *DB) SavePreferences(p models.Preferences) error {
	if err := db.setPref(keyCaptureIntervalMs, strconv.Itoa(p.CaptureIntervalMs)); err != nil {
		return err
	}
	if err := db.setPref(keyModel, p.Model); err != nil {
		return err
	}
	if err := db.setPref(keyVoiceID, p.VoiceID); err != nil {
		return err
	}
	if err := db.setPref(keyVoiceSpeed, strconv.FormatFloat(p.VoiceSpeed, 'f', -1, 64)); err != nil {
		return err
	}
	if err := db.setPref(keyCaptureDisplay, strconv.Itoa(p.CaptureDisplay)); err != nil {
		return err
	}
	if err := db.setPref(keyRegionX, strconv.FormatFloat(p.RegionX, 'f', -1, 64)); err != nil {
		return err
	}
	if err := db.setPref(keyRegionY, strconv.FormatFloat(p.RegionY, 'f', -1, 64)); err != nil {
		return err
	}
	if err := db.setPref(keyRegionW, strconv.FormatFloat(p.RegionW, 'f', -1, 64)); err != nil {
		return err
	}
	if err := db.setPref(keyRegionH, strconv.FormatFloat(p.RegionH, 'f', -1, 64)); err != nil {
		return err
	}
	if err := db.setPref(keySessionLimitMinutes, strconv.Itoa(p.SessionLimitMinutes)); err != nil {
		return err
	}
	return db.setPref(keySoftWarningMinutes, strconv.Itoa(p.SoftWarningMinutes))
}

// getPref fetches a single preference value by key. Returns "" if not found.
func (db *DB) getPref(key string) (string, error) {
	var value string
	err := db.conn.QueryRow(`SELECT value FROM preferences WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: get pref %q: %w", key, err)
	}
	return value, nil
}

// setPref upserts a single preference value.
func (db *DB) setPref(key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO preferences (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	if err != nil {
		return fmt.Errorf("store: set pref %q: %w", key, err)
	}
	return nil
}

func providerKey(provider string) string {
	switch provider {
	case "openrouter":
		return keyOpenRouterAPIKey
	case "elevenlabs":
		return keyElevenLabsAPIKey
	}
	return ""
}
