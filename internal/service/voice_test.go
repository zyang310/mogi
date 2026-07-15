package service

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"mogi/internal/models"
)

// TestStripNonSpeech verifies that bracketed audio-event annotations are removed
// and tag-only transcripts (silence / noise) collapse to empty, while real
// speech is preserved.
func TestStripNonSpeech(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"tag only", "[background noise]", ""},
		{"phone ringing", "[phone ringing]", ""},
		{"parens cough", "(coughs)", ""},
		{"tags around speech", "[noise] hello [music]", "hello"},
		{"trailing tag", "let's start (laughs)", "let's start"},
		{"plain speech unchanged", "merge two sorted linked lists", "merge two sorted linked lists"},
		{"empty", "", ""},
		{"collapses whitespace", "  use   two   pointers ", "use two pointers"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripNonSpeech(c.in); got != c.want {
				t.Errorf("stripNonSpeech(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// voiceWith builds a Voice service over fake providers, populating the registry
// slots directly (in-package tests may reach the unexported fields, which keeps
// the registry free of test-only injection hooks).
func voiceWith(prefs models.Preferences, eleven, google Speech) *Voice {
	p := NewProviders()
	p.eleven = eleven
	p.google = google
	return NewVoice(&fakeStore{getPreferences: func() (models.Preferences, error) { return prefs, nil }}, p)
}

// TestActiveTTSMatrix exercises the full provider-resolution table: the saved
// TTSProvider preference crossed with which keys are configured, including the
// per-provider default voices and both cross-fallback rows.
func TestActiveTTSMatrix(t *testing.T) {
	eleven := &fakeSpeech{}
	google := &fakeSpeech{}

	cases := []struct {
		name       string
		prefs      models.Preferences
		eleven     Speech
		google     Speech
		wantTTS    TTS
		wantVoice  string
		wantErrSub string
	}{
		{"google default, both configured", models.Preferences{GoogleVoiceID: "g-v"}, eleven, google, google, "g-v", ""},
		{"google default, google only", models.Preferences{}, nil, google, google, defaultGoogleVoiceID, ""},
		{"google default falls back to eleven", models.Preferences{VoiceID: "el-v"}, eleven, nil, eleven, "el-v", ""},
		{"google default, none configured", models.Preferences{}, nil, nil, nil, "", "Google API key not configured"},
		{"eleven pref, both configured", models.Preferences{TTSProvider: "elevenlabs", VoiceID: "el-v"}, eleven, google, eleven, "el-v", ""},
		{"eleven pref, default voice", models.Preferences{TTSProvider: "elevenlabs"}, eleven, nil, eleven, defaultVoiceID, ""},
		{"eleven pref falls back to google", models.Preferences{TTSProvider: "elevenlabs"}, nil, google, google, defaultGoogleVoiceID, ""},
		{"eleven pref, none configured", models.Preferences{TTSProvider: "elevenlabs"}, nil, nil, nil, "", "ElevenLabs API key not configured"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := voiceWith(c.prefs, c.eleven, c.google)
			got, voiceID, err := v.activeTTS()
			if c.wantErrSub != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrSub) {
					t.Fatalf("activeTTS() err = %v, want containing %q", err, c.wantErrSub)
				}
				return
			}
			if err != nil {
				t.Fatalf("activeTTS() unexpected error: %v", err)
			}
			if got != c.wantTTS {
				t.Errorf("activeTTS() picked the wrong provider")
			}
			if voiceID != c.wantVoice {
				t.Errorf("activeTTS() voice = %q, want %q", voiceID, c.wantVoice)
			}
		})
	}
}

// TestManagedTTSForcesGoogle verifies managed mode forces Google TTS even when
// the saved provider is ElevenLabs (the managed EL key is STT-scoped), keeps an
// allowed saved voice, clamps a premium one to the Neural2 default, and errors
// (no silent EL fallback) when Google isn't configured.
func TestManagedTTSForcesGoogle(t *testing.T) {
	eleven := &fakeSpeech{}
	google := &fakeSpeech{}

	v := voiceWith(models.Preferences{KeyMode: "managed", TTSProvider: "elevenlabs", GoogleVoiceID: "en-US-Neural2-C"}, eleven, google)
	provider, voiceID, err := v.activeTTS()
	if err != nil {
		t.Fatalf("activeTTS() error: %v", err)
	}
	if provider != TTS(google) {
		t.Error("managed mode must force Google TTS despite TTSProvider=elevenlabs")
	}
	if voiceID != "en-US-Neural2-C" {
		t.Errorf("voice = %q, want the allowed saved voice kept", voiceID)
	}

	v = voiceWith(models.Preferences{KeyMode: "managed", GoogleVoiceID: "en-US-Chirp-HD-F"}, eleven, google)
	if _, voiceID, _ := v.activeTTS(); voiceID != defaultGoogleVoiceID {
		t.Errorf("premium saved voice = %q, want fallback to %q", voiceID, defaultGoogleVoiceID)
	}

	v = voiceWith(models.Preferences{KeyMode: "managed"}, eleven, nil)
	if _, _, err := v.activeTTS(); err == nil {
		t.Error("managed mode with no Google key should error, not fall back to ElevenLabs")
	}
}

// TestManagedVoicesFiltersCatalog verifies the picker catalog is trimmed to the
// cost-allowed tiers in managed mode, and left whole in BYOK mode.
func TestManagedVoicesFiltersCatalog(t *testing.T) {
	catalog := []models.Voice{
		{ID: "en-US-Neural2-F", Name: "Neural2 F"},
		{ID: "en-US-Wavenet-D", Name: "Wavenet D"},
		{ID: "en-US-Chirp-HD-O", Name: "Chirp HD O"},
		{ID: "en-US-Studio-M", Name: "Studio M"},
	}
	google := &fakeSpeech{listVoices: func() ([]models.Voice, error) { return catalog, nil }}

	managed := voiceWith(models.Preferences{KeyMode: "managed"}, nil, google)
	got, err := managed.Voices(context.Background())
	if err != nil {
		t.Fatalf("Voices() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("managed catalog = %d voices, want 2 (Neural2 + Wavenet only): %+v", len(got), got)
	}
	for _, voice := range got {
		if !managedGoogleVoiceAllowed(voice.ID) {
			t.Errorf("managed catalog leaked a premium voice: %q", voice.ID)
		}
	}

	byok := voiceWith(models.Preferences{KeyMode: "byok"}, nil, google)
	if got, _ := byok.Voices(context.Background()); len(got) != len(catalog) {
		t.Errorf("byok catalog = %d voices, want the full %d", len(got), len(catalog))
	}
}

// TestActiveSTTPrefersScribe verifies the STT order: ElevenLabs Scribe when
// configured, else Google, else a configuration error.
func TestActiveSTTPrefersScribe(t *testing.T) {
	eleven := &fakeSpeech{}
	google := &fakeSpeech{}

	if got, err := voiceWith(models.Preferences{}, eleven, google).activeSTT(); err != nil || got != STT(eleven) {
		t.Errorf("with both keys, activeSTT() = %v, %v; want the ElevenLabs client", got, err)
	}
	if got, err := voiceWith(models.Preferences{}, nil, google).activeSTT(); err != nil || got != STT(google) {
		t.Errorf("with google only, activeSTT() = %v, %v; want the Google client", got, err)
	}
	if _, err := voiceWith(models.Preferences{}, nil, nil).activeSTT(); err == nil {
		t.Error("with no keys, activeSTT() should error")
	}
}

// TestTranscribe covers the base64 handling around the provider call: data-URI
// prefixes are stripped, invalid base64 errors out, and the transcript is
// cleaned of non-speech tags before returning.
func TestTranscribe(t *testing.T) {
	var gotAudio []byte
	var gotMime string
	eleven := &fakeSpeech{transcribe: func(audio []byte, mimeType string) (string, error) {
		gotAudio, gotMime = audio, mimeType
		return "[noise] two pointers [music]", nil
	}}
	v := voiceWith(models.Preferences{}, eleven, nil)

	raw := base64.StdEncoding.EncodeToString([]byte("wav-bytes"))
	text, err := v.Transcribe(context.Background(), "data:audio/wav;base64,"+raw, "audio/wav")
	if err != nil {
		t.Fatalf("Transcribe() error: %v", err)
	}
	if string(gotAudio) != "wav-bytes" || gotMime != "audio/wav" {
		t.Errorf("provider received audio=%q mime=%q", gotAudio, gotMime)
	}
	if text != "two pointers" {
		t.Errorf("Transcribe() = %q, want non-speech tags stripped", text)
	}

	if _, err := v.Transcribe(context.Background(), "not-base64!!!", "audio/wav"); err == nil {
		t.Error("Transcribe() should reject invalid base64")
	}
}

// TestSynthesizeAndPreview verifies the TTS path returns base64 audio, speech
// is sanitized of markdown, and Preview substitutes the default voice + fixed
// phrase when no voice is given.
func TestSynthesizeAndPreview(t *testing.T) {
	var gotVoice, gotText string
	google := &fakeSpeech{synthesize: func(voiceID, text string) ([]byte, error) {
		gotVoice, gotText = voiceID, text
		return []byte("mp3"), nil
	}}
	v := voiceWith(models.Preferences{GoogleVoiceID: "g-v"}, nil, google)

	out, err := v.Synthesize(context.Background(), "try `two pointers`")
	if err != nil {
		t.Fatalf("Synthesize() error: %v", err)
	}
	if decoded, _ := base64.StdEncoding.DecodeString(out); string(decoded) != "mp3" {
		t.Errorf("Synthesize() should return the provider audio base64-encoded")
	}
	if gotVoice != "g-v" {
		t.Errorf("Synthesize() voice = %q, want saved preference", gotVoice)
	}
	if strings.Contains(gotText, "`") {
		t.Errorf("Synthesize() text %q should be sanitized for speech", gotText)
	}

	if _, err := v.Preview(context.Background(), ""); err != nil {
		t.Fatalf("Preview() error: %v", err)
	}
	if gotVoice != "g-v" || gotText != previewPhrase {
		t.Errorf("Preview(\"\") used voice=%q text=%q; want default voice + preview phrase", gotVoice, gotText)
	}
}
