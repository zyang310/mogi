package models

// Voice is a selectable ElevenLabs voice — a subset of the /v1/voices response
// shaped for the Settings voice picker.
type Voice struct {
	ID         string `json:"id"`         // ElevenLabs voice_id
	Name       string `json:"name"`       // e.g. "Rachel"
	Category   string `json:"category"`   // e.g. "premade", "cloned"
	PreviewURL string `json:"previewUrl"` // mp3 sample, played by the picker's preview button
}
