package voice

import "testing"

func TestMimeToExt(t *testing.T) {
	cases := map[string]string{
		"audio/mp4":                ".m4a",
		"audio/mp4;codecs=mp4a":    ".m4a", // codec param stripped
		"audio/webm;codecs=opus":   ".webm",
		"audio/webm":               ".webm",
		"audio/mpeg":               ".mp3",
		"audio/wav":                ".wav",
		"audio/ogg":                ".ogg",
		"":                         ".webm", // unknown/empty falls back
		"application/octet-stream": ".webm",
	}
	for input, want := range cases {
		if got := mimeToExt(input); got != want {
			t.Errorf("mimeToExt(%q) = %q, want %q", input, got, want)
		}
	}
}
