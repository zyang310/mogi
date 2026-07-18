import { models } from "../../lib/wailsBridge";
import VoicePicker from "./VoicePicker";
import "./VoiceSection.css";

// The two spoken-voice providers, rendered as selectable tiles. `tone` drives
// the pill color (low-cost = matcha, premium = gold); `keyLabel` names the API
// key a tile needs when it isn't configured.
const VOICE_PROVIDERS = [
  { id: "google", name: "Google", tag: "Low cost", tone: "low", keyLabel: "Google Cloud" },
  { id: "elevenlabs", name: "ElevenLabs", tag: "Premium", tone: "premium", keyLabel: "ElevenLabs" },
] as const;

interface Props {
  authStatus: models.AuthStatus;
  prefs: models.Preferences | null;
  saving: boolean;
  savePrefs: (patch: Partial<models.Preferences>, msg: string) => Promise<void>;
  // Provider choice + live slider value live in the Settings shell so "Clear
  // all data" can reset them centrally.
  ttsProvider: string;
  setTtsProvider: (v: string) => void;
  voiceSpeed: number;
  setVoiceSpeed: (v: number) => void;
  // Voice catalog bookkeeping stays in the shell: voiceNames accumulates across
  // provider switches and section visits so a non-active provider's stored
  // voice keeps its friendly name.
  voiceCount: number | null;
  voiceNames: Record<string, string>;
  onCatalog: (voices: models.Voice[]) => void;
  // Deep-link to the API Keys section from the "add a key first" placeholder.
  onOpenApiKeys: () => void;
}

// VoiceSection is the Settings → Voice Calibration pane: provider tiles, the
// searchable VoicePicker for the active provider, and the speaking-speed
// slider. Gated on a configured voice key.
export default function VoiceSection({
  authStatus,
  prefs,
  saving,
  savePrefs,
  ttsProvider,
  setTtsProvider,
  voiceSpeed,
  setVoiceSpeed,
  voiceCount,
  voiceNames,
  onCatalog,
  onOpenApiKeys,
}: Props) {
  // Managed test accounts speak with Google only: the shared ElevenLabs key is
  // STT-scoped, so its TTS would 4xx. Branch on authStatus, not prefs — the
  // backend's activeTTS guard is the source of truth; this is mirror-only.
  const managed = authStatus.keyMode === "managed";

  // The provider actually in effect: the saved choice if its key exists, else
  // whichever provider is configured. Mirrors app.go's activeTTS fallback so the
  // picker and saved voice stay consistent with what gets spoken.
  function resolveProvider(pref: string): string {
    if (managed) return "google";
    if (pref === "elevenlabs" && authStatus.elevenLabsConfigured) return "elevenlabs";
    if (pref === "google" && authStatus.googleConfigured) return "google";
    if (authStatus.googleConfigured) return "google";
    if (authStatus.elevenLabsConfigured) return "elevenlabs";
    return pref;
  }

  // Voices are provider-specific, so save to the field matching the active provider.
  function saveVoice(voiceId: string) {
    const patch =
      resolveProvider(ttsProvider) === "elevenlabs"
        ? { voiceId }
        : { googleVoiceId: voiceId };
    return savePrefs(patch, "Voice saved.");
  }

  function saveTTSProvider(provider: string) {
    setTtsProvider(provider);
    return savePrefs({ ttsProvider: provider }, "Voice provider saved.");
  }

  // Persist the slider's current value once the user finishes dragging.
  function saveVoiceSpeed() {
    return savePrefs({ voiceSpeed }, "Voice speed saved.");
  }

  const activeProvider = resolveProvider(ttsProvider);
  const anyVoiceConfigured = authStatus.googleConfigured || authStatus.elevenLabsConfigured;
  // Managed mode drops the premium tile rather than rendering it dead — the
  // managed ElevenLabs key can't speak, so it's not a choice at all.
  const providerTiles = managed
    ? VOICE_PROVIDERS.filter((p) => p.id === "google")
    : VOICE_PROVIDERS;

  return (
    <>
      <header className="settings-head">
        <h1>Voice Calibration</h1>
        <p>Pick the provider and voice your interviewer speaks with.</p>
      </header>
      {anyVoiceConfigured ? (
        // One merged panel: three numbered sections (Provider /
        // Interviewer Voice / Speaking Speed) split by gradient dividers.
        <div className="vc-panel">
          {/* 01 · PROVIDER — two selectable tiles. */}
          <div className="vc-section">
            <div className="vc-section-head">
              <span className="vc-section-label">01 · Provider</span>
              <span className="vc-section-note">Each provider remembers its own voice.</span>
            </div>
            <p className="vc-section-desc">
              Sets the spoken voice only. Mic transcription uses ElevenLabs if key is available, otherwise Google.
            </p>
            <div className="vc-provider-grid">
              {providerTiles.map((p) => {
                const selected = activeProvider === p.id;
                const isConfigured =
                  p.id === "google"
                    ? authStatus.googleConfigured
                    : authStatus.elevenLabsConfigured;
                const remembered =
                  p.id === "google" ? prefs?.googleVoiceId : prefs?.voiceId;
                const rememberedLabel =
                  (remembered && voiceNames[remembered]) || remembered || "Not set";
                return (
                  <button
                    key={p.id}
                    type="button"
                    className={`vc-provider${selected ? " is-selected" : ""}`}
                    onClick={() => saveTTSProvider(p.id)}
                    disabled={saving || !isConfigured}
                    title={isConfigured ? "" : `Add a ${p.keyLabel} key first`}
                  >
                    <div className="vc-provider-top">
                      <span className={`vc-provider-tag vc-provider-tag--${p.tone}`}>
                        <span className="vc-provider-tag-dot" />
                        {p.tag}
                      </span>
                      <span className="material-symbols-outlined vc-provider-check">
                        {selected ? "check_circle" : "radio_button_unchecked"}
                      </span>
                    </div>
                    <div className="vc-provider-name">{p.name}</div>
                    <div className="vc-provider-voice">
                      <span className="material-symbols-outlined">graphic_eq</span>
                      {rememberedLabel}
                    </div>
                  </button>
                );
              })}
            </div>
            {managed && (
              <p className="settings-hint settings-hint-muted">
                Test accounts speak with Google voices. ElevenLabs premium voices
                are available when you use your own keys.
              </p>
            )}
          </div>

          {/* 02 · INTERVIEWER VOICE — searchable list of the active provider's voices. */}
          <div className="vc-section">
            <div className="vc-section-head">
              <span className="vc-section-label">02 · Interviewer Voice</span>
              <span className="vc-section-note">
                {voiceCount != null ? `${voiceCount} voices available` : ""}
              </span>
            </div>
            <p className="vc-section-desc">
              Click a voice to select it, or the play button to hear a sample.
            </p>
            <VoicePicker
              provider={activeProvider}
              currentVoiceId={
                (activeProvider === "elevenlabs"
                  ? prefs?.voiceId
                  : prefs?.googleVoiceId) ?? ""
              }
              onSelect={saveVoice}
              speed={voiceSpeed}
              onCatalog={onCatalog}
            />
          </div>

          {/* 03 · SPEAKING SPEED — custom track over a transparent range input. */}
          <div className="vc-section">
            <div className="vc-section-head">
              <span className="vc-section-label">03 · Speaking Speed</span>
              <button
                type="button"
                className="vc-reset-btn"
                onClick={() => {
                  setVoiceSpeed(1);
                  savePrefs({ voiceSpeed: 1 }, "Voice speed saved.");
                }}
                disabled={saving || !prefs || voiceSpeed === 1}
              >
                Reset
              </button>
            </div>
            <div className="vc-speed-row">
              <div className="vc-speed-track">
                <div
                  className="vc-speed-fill"
                  style={{ width: `${((voiceSpeed - 0.5) / 1.5) * 100}%` }}
                />
                <div
                  className="vc-speed-thumb"
                  style={{ left: `${((voiceSpeed - 0.5) / 1.5) * 100}%` }}
                />
                <input
                  type="range"
                  className="vc-speed-input"
                  min={0.5}
                  max={2}
                  step={0.05}
                  value={voiceSpeed}
                  onChange={(e) => setVoiceSpeed(Number(e.target.value))}
                  onPointerUp={saveVoiceSpeed}
                  onKeyUp={saveVoiceSpeed}
                  disabled={saving || !prefs}
                />
              </div>
              <div className="vc-speed-value">{voiceSpeed.toFixed(2)}×</div>
            </div>
            <div className="vc-speed-scale">
              <span>0.5× slower</span>
              <span>1.0× natural</span>
              <span>2.0× faster</span>
            </div>
          </div>
        </div>
      ) : (
        <div className="settings-card settings-card-placeholder">
          <span className="material-symbols-outlined">record_voice_over</span>
          <h3 className="settings-card-title">Add a voice key first</h3>
          <p className="settings-hint">
            Spoken interviews need a Google Cloud or ElevenLabs API key. Add one under{" "}
            <button className="settings-link-btn" onClick={onOpenApiKeys}>
              API Keys
            </button>{" "}
            to choose a voice.
          </p>
        </div>
      )}
    </>
  );
}
