import { useEffect, useMemo, useState } from "react";
import { ListVoices, PreviewVoice, models } from "../../lib/wailsBridge";
import { useAudioPlayer } from "../../lib/useAudioPlayer";
import "./VoicePicker.css";

interface Props {
  currentVoiceId: string;
  onSelect: (voiceId: string) => void; // parent persists via UpdatePreferences
  speed?: number; // playback rate for previews, so they reflect the speed slider
  provider?: string; // active TTS provider; refetch the catalog when it changes
  onCountChange?: (count: number) => void; // report catalog size up for the header note
}

// VoicePicker is a searchable list of the active provider's voices for Settings.
// It fetches the catalog (refetching when the provider changes) and filters
// client-side by name. Selecting a row reports the id upward — persistence is the
// parent's job. Each row has a ▶ button: ElevenLabs voices play their hosted
// sample, Google voices synthesize one on the fly. Mirrors ModelPicker.
export default function VoicePicker({
  currentVoiceId,
  onSelect,
  speed = 1,
  provider,
  onCountChange,
}: Props) {
  const [allVoices, setAllVoices] = useState<models.Voice[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [previewingId, setPreviewingId] = useState<string | null>(null);
  const { speaking, play, stop } = useAudioPlayer();

  // Fetch the active provider's voices, refetching when the provider changes.
  // Wails calls no-op in a plain browser, so guard with try/catch and surface
  // failures inline.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    (async () => {
      try {
        const list = await ListVoices();
        if (!cancelled) {
          setAllVoices(list ?? []);
          onCountChange?.((list ?? []).length);
        }
      } catch (e: any) {
        if (!cancelled) setError(e?.message || String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [provider]);

  // Clear the "playing" marker whenever playback ends (or is stopped/fails).
  useEffect(() => {
    if (!speaking) setPreviewingId(null);
  }, [speaking]);

  // Filter by name, then sort: pinned current selection first, then by name.
  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = allVoices.filter(
      (v) => !q || v.name.toLowerCase().includes(q)
    );
    return filtered.sort((a, b) => {
      if (a.id === currentVoiceId) return -1;
      if (b.id === currentVoiceId) return 1;
      return a.name.localeCompare(b.name);
    });
  }, [allVoices, search, currentVoiceId]);

  // Preview a voice. ElevenLabs voices have a hosted previewUrl; Google voices
  // don't, so we synthesize a short sample on the fly via PreviewVoice.
  async function handlePreview(v: models.Voice) {
    if (previewingId === v.id) {
      stop(); // the speaking→false effect clears previewingId
      return;
    }
    setPreviewingId(v.id);
    try {
      const src = v.previewUrl || (await PreviewVoice(v.id));
      await play(src, speed);
    } catch {
      setPreviewingId(null);
    }
  }

  return (
    <div className="voice-picker">
      <div className="voice-search-wrap">
        <span className="material-symbols-outlined voice-search-icon">search</span>
        <input
          type="text"
          className="voice-search-input"
          placeholder="Search voices…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          disabled={loading || !!error}
        />
      </div>

      {loading && <p className="voice-picker-status">Loading voices…</p>}
      {error && <p className="settings-error">{error}</p>}
      {!loading && !error && visible.length === 0 && (
        <p className="voice-picker-status">No voices match your search.</p>
      )}

      {!loading && !error && visible.length > 0 && (
        <div className="voice-list vc-scroll">
          {visible.map((v) => {
            const active = v.id === currentVoiceId;
            const playing = previewingId === v.id;
            return (
              <div key={v.id} className={`voice-row${active ? " is-active" : ""}`}>
                <button
                  type="button"
                  className="voice-row-select"
                  onClick={() => onSelect(v.id)}
                  title={v.id}
                >
                  <span className="material-symbols-outlined voice-row-icon">
                    {active ? "graphic_eq" : "radio_button_unchecked"}
                  </span>
                  <span className="voice-row-name">{v.name || v.id}</span>
                </button>
                {v.category && <span className="voice-row-badge">{v.category}</span>}
                <button
                  type="button"
                  className={`voice-row-play${playing ? " is-playing" : ""}`}
                  onClick={() => handlePreview(v)}
                  title={playing ? "Stop preview" : "Preview voice"}
                >
                  {playing ? (
                    <span className="voice-bars">
                      <span />
                      <span />
                      <span />
                    </span>
                  ) : (
                    <span className="material-symbols-outlined">play_arrow</span>
                  )}
                </button>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
