package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"ai-interviewer/internal/ai"
	"ai-interviewer/internal/models"
)

// defaultVoiceID is ElevenLabs' long-stable "Rachel" premade voice, used as a
// fallback so spoken replies work before the user picks a voice in Settings.
const defaultVoiceID = "21m00Tcm4TlvDq8ikWAM"

// defaultGoogleVoiceID is a widely-available, low-cost Neural2 voice used as the
// Google fallback before the user picks one.
const defaultGoogleVoiceID = "en-US-Neural2-F"

// previewPhrase is spoken by Preview when auditioning a voice that has no
// hosted preview clip (Google) — kept short to minimise synthesis cost.
const previewPhrase = "Hi, let's get started with the interview."

// VoiceStore is the slice of the data layer the voice service needs: just the
// saved provider/voice preferences.
type VoiceStore interface {
	GetPreferences() (models.Preferences, error)
}

// Voice is the speech service. It resolves the active STT/TTS provider from
// the registry (with cross-provider fallbacks so audio keeps working when only
// one key is configured) and converts between the frontend's base64 audio
// strings and provider bytes. All processing and API calls happen here in Go —
// the frontend only records and plays audio.
type Voice struct {
	store     VoiceStore
	providers *Providers
}

// NewVoice wires the speech service to its preference store and the live
// client registry.
func NewVoice(store VoiceStore, providers *Providers) *Voice {
	return &Voice{store: store, providers: providers}
}

// activeSTT returns the speech-to-text provider to use for the mic. It prefers
// ElevenLabs Scribe when configured (cheaper per minute and it robustly handles
// the recorder's audio) and falls back to Google. With both keys present this
// yields the optimal combo: Scribe STT + Google TTS. STT has no user toggle —
// the Settings toggle is voice-only.
func (v *Voice) activeSTT() (STT, error) {
	if eleven := v.providers.ElevenLabs(); eleven != nil {
		return eleven, nil
	}
	if google := v.providers.Google(); google != nil {
		return google, nil
	}
	return nil, fmt.Errorf("no voice provider configured — add a Google Cloud or ElevenLabs key in Settings")
}

// activeTTS returns the configured TTS provider and the voice to use for it,
// based on Preferences.TTSProvider. If the chosen provider has no key, it falls
// back to the other configured provider so audio still works; if neither is
// configured it returns an error.
func (v *Voice) activeTTS() (TTS, string, error) {
	prefs, _ := v.store.GetPreferences()

	elevenVoice := prefs.VoiceID
	if elevenVoice == "" {
		elevenVoice = defaultVoiceID
	}
	googleVoice := prefs.GoogleVoiceID
	if googleVoice == "" {
		googleVoice = defaultGoogleVoiceID
	}

	eleven := v.providers.ElevenLabs()
	google := v.providers.Google()

	if prefs.TTSProvider == "elevenlabs" {
		if eleven != nil {
			return eleven, elevenVoice, nil
		}
		if google != nil {
			return google, googleVoice, nil // fall back so audio still works
		}
		return nil, "", fmt.Errorf("ElevenLabs API key not configured — add it in Settings")
	}

	// Default provider: Google.
	if google != nil {
		return google, googleVoice, nil
	}
	if eleven != nil {
		return eleven, elevenVoice, nil // fall back so audio still works
	}
	return nil, "", fmt.Errorf("Google API key not configured — add it in Settings")
}

// Transcribe converts recorded mic audio (base64 WAV, optionally a data URI)
// into text via the active STT provider (ElevenLabs Scribe if configured, else
// Google). mimeType labels the upload. The frontend feeds the result into the
// normal send loop, so voice and typed input share one path.
func (v *Voice) Transcribe(ctx context.Context, audioBase64, mimeType string) (string, error) {
	provider, err := v.activeSTT()
	if err != nil {
		return "", err
	}

	// Accept either a raw base64 string or a full data URI ("data:audio/...;base64,...").
	if i := strings.Index(audioBase64, ","); strings.HasPrefix(audioBase64, "data:") && i != -1 {
		audioBase64 = audioBase64[i+1:]
	}
	audio, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		return "", fmt.Errorf("decode audio: %w", err)
	}

	text, err := provider.Transcribe(ctx, audio, mimeType)
	if err != nil {
		return "", err
	}
	return stripNonSpeech(text), nil
}

// nonSpeechTag matches bracketed audio-event annotations like "[background
// noise]" or "(coughs)" that STT providers emit for non-speech sounds.
var nonSpeechTag = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)

// stripNonSpeech removes audio-event annotations from a transcript and returns
// the remaining speech (whitespace-collapsed). A clip with no speech — silence
// or background noise — transcribes to only such tags, so the result is empty
// and the frontend's "if (text)" gate drops the turn instead of sending the
// tags to the LLM as a candidate message.
func stripNonSpeech(text string) string {
	cleaned := nonSpeechTag.ReplaceAllString(text, " ")
	return strings.Join(strings.Fields(cleaned), " ")
}

// Synthesize converts interviewer text into spoken audio via the active TTS
// provider (Google or ElevenLabs), using the voice saved in preferences (or
// the default). The reply is cleaned of markdown first (ai.SanitizeForSpeech)
// so the voice doesn't read backticks/asterisks aloud. It returns the MP3 as
// base64 so it crosses the Wails boundary as a string, matching how
// screenshots are passed; the frontend plays it via the Web Audio API.
func (v *Voice) Synthesize(ctx context.Context, text string) (string, error) {
	provider, voiceID, err := v.activeTTS()
	if err != nil {
		return "", err
	}
	audio, err := provider.Synthesize(ctx, voiceID, ai.SanitizeForSpeech(text))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(audio), nil
}

// Voices returns the active provider's available voices for the Settings
// picker.
func (v *Voice) Voices(ctx context.Context) ([]models.Voice, error) {
	provider, _, err := v.activeTTS()
	if err != nil {
		return nil, err
	}
	return provider.ListVoices(ctx)
}

// Preview synthesizes a short fixed phrase with the given voice via the active
// provider and returns it as base64 MP3. It backs the picker's preview button
// for providers without hosted preview clips (Google). An empty voiceID falls
// back to the active provider's default.
func (v *Voice) Preview(ctx context.Context, voiceID string) (string, error) {
	provider, fallback, err := v.activeTTS()
	if err != nil {
		return "", err
	}
	if voiceID == "" {
		voiceID = fallback
	}
	audio, err := provider.Synthesize(ctx, voiceID, previewPhrase)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(audio), nil
}
