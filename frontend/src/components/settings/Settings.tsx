import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import {
  SetAPIKey,
  DeleteAPIKey,
  GetAuthStatus,
  GetAppVersion,
  GetHotkeyStatus,
  GetPreferences,
  CheckForUpdate,
  OpenInputMonitoringSettings,
  OpenReleasePage,
  UpdatePreferences,
  models,
  hotkey,
} from "../../lib/wailsBridge";
import { comboFromKeyboardEvent, bareModifierFromCode, prettyHotkey } from "../../lib/hotkey";
import type { ThemePref } from "../../lib/theme";
import ModelPicker from "./ModelPicker";
import VoicePicker from "./VoicePicker";
import "./Settings.css";

type KeyProvider = "openrouter" | "elevenlabs" | "google";

const PROVIDER_LABELS: Record<KeyProvider, string> = {
  openrouter: "OpenRouter",
  elevenlabs: "ElevenLabs",
  google: "Google Cloud",
};

// Per-provider metadata for the API-key cards. The three cards are structurally
// identical, so we drive them from this list (label, icon tile, input hint,
// placeholder) and render one <ApiKeyCard> per entry rather than hand-repeating.
interface KeyCard {
  id: KeyProvider;
  icon: string; // Material Symbols name for the icon tile
  placeholder: string;
  hint: ReactNode;
}

const KEY_CARDS: KeyCard[] = [
  {
    id: "openrouter",
    icon: "router",
    placeholder: "sk-or-...",
    hint: (
      <>
        Get a key at{" "}
        <a href="https://openrouter.ai/keys" target="_blank" rel="noopener noreferrer">
          openrouter.ai/keys
        </a>
      </>
    ),
  },
  {
    id: "elevenlabs",
    icon: "graphic_eq",
    placeholder: "sk-el-...",
    hint: (
      <>
        Optional — premium spoken interviews.{" "}
        <a
          href="https://elevenlabs.io/app/settings/api-keys"
          target="_blank"
          rel="noopener noreferrer"
        >
          elevenlabs.io
        </a>
      </>
    ),
  },
  {
    id: "google",
    icon: "cloud",
    placeholder: "AIza...",
    hint: (
      <>
        Low-cost spoken interviews. Enable the{" "}
        <a
          href="https://console.cloud.google.com/apis/library/texttospeech.googleapis.com"
          target="_blank"
          rel="noopener noreferrer"
        >
          Text-to-Speech API
        </a>{" "}
        and create a key in{" "}
        <a
          href="https://console.cloud.google.com/apis/credentials"
          target="_blank"
          rel="noopener noreferrer"
        >
          Credentials
        </a>
        .
      </>
    ),
  },
];

// Settings is a full page (not a modal). A left "Configuration" sidebar switches
// between sections; the right pane renders the active section. Navigation in/out
// is handled by the app's pill-nav, so there's no close button here.
type Section =
  | "general"
  | "models"
  | "api-keys"
  | "voice"
  | "push-to-talk"
  | "capture"
  | "privacy"
  | "about";

interface NavItem {
  id: Section;
  label: string;
  icon: string; // Material Symbols name
}

const NAV: NavItem[] = [
  { id: "general", label: "General", icon: "tune" },
  { id: "models", label: "Models", icon: "neurology" },
  { id: "api-keys", label: "API Keys", icon: "key" },
  { id: "voice", label: "Voice Calibration", icon: "record_voice_over" },
  { id: "push-to-talk", label: "Voice Hotkey", icon: "keyboard" },
  { id: "capture", label: "Capture Prefs", icon: "settings_input_component" },
  { id: "privacy", label: "Privacy", icon: "security" },
  { id: "about", label: "About", icon: "info" },
];

interface Props {
  authStatus: models.AuthStatus;
  onAuthChange: (status: models.AuthStatus) => void;
  // Bubble persisted preference changes up so the hub stays in sync.
  onPrefsChange?: (prefs: models.Preferences) => void;
  // Theme lives in App (mirrored to <html> + localStorage), so the control here
  // reads/writes through props to stay in sync with the pill-nav quick toggle.
  themePref: ThemePref;
  onThemeChange: (pref: ThemePref) => void;
}

export default function Settings({
  authStatus,
  onAuthChange,
  onPrefsChange,
  themePref,
  onThemeChange,
}: Props) {
  // Land on API Keys when unconfigured (the most useful next step), else Models.
  const [section, setSection] = useState<Section>(
    authStatus.openRouterConfigured ? "models" : "api-keys"
  );
  const [openRouterKey, setOpenRouterKey] = useState("");
  const [elevenLabsKey, setElevenLabsKey] = useState("");
  const [googleKey, setGoogleKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const [prefs, setPrefs] = useState<models.Preferences | null>(null);
  const [intervalSec, setIntervalSec] = useState("3");
  const [limitMinutes, setLimitMinutes] = useState("30");
  const [warningMinutes, setWarningMinutes] = useState("25");
  // Live slider value; persisted only on commit (pointer/key up) to avoid a DB
  // write per step. Drives the readout and the VoicePicker preview speed.
  const [voiceSpeed, setVoiceSpeed] = useState(1);
  // Active TTS provider; drives the voice picker and which voice field is saved.
  const [ttsProvider, setTtsProvider] = useState("google");
  // Push-to-talk: capturing = listening for the next keypress to bind; hkStatus
  // reports whether the global hook is live (drives the macOS permission hint).
  const [capturing, setCapturing] = useState(false);
  const [hkStatus, setHkStatus] = useState<hotkey.Status | null>(null);
  // About: app version + on-demand update check (mirrors the launch-time banner).
  const [appVersion, setAppVersion] = useState("");
  const [checking, setChecking] = useState(false);
  const [updateInfo, setUpdateInfo] = useState<models.UpdateInfo | null>(null);
  const [checkedOnce, setCheckedOnce] = useState(false);

  // Load preferences on mount.
  useEffect(() => {
    GetPreferences()
      .then((p) => {
        setPrefs(p);
        setIntervalSec(String(Math.max(1, Math.round(p.captureIntervalMs / 1000))));
        setLimitMinutes(String(p.sessionLimitMinutes ?? 30));
        setWarningMinutes(String(p.softWarningMinutes ?? 25));
        setVoiceSpeed(p.voiceSpeed || 1);
        setTtsProvider(p.ttsProvider || "google");
      })
      .catch(() => {});
    GetAppVersion().then(setAppVersion).catch(() => {});
    refreshHotkeyStatus();
  }, []);

  // Fetch the global push-to-talk hook status (best-effort; absent in browser
  // preview). hookEnabled only flips true once the OS confirms the hook started.
  async function refreshHotkeyStatus() {
    try {
      setHkStatus(await GetHotkeyStatus());
    } catch {
      // Wails runtime not present in browser preview.
    }
  }

  // While capturing, listen window-wide for the next key (or bare modifier) and
  // store it as the hotkey. Esc cancels. Capture-phase so it pre-empts inputs.
  useEffect(() => {
    if (!capturing) return;
    let sawMainKey = false;
    function onKeyDown(e: KeyboardEvent) {
      e.preventDefault();
      e.stopPropagation();
      if (e.key === "Escape") {
        setCapturing(false);
        return;
      }
      const combo = comboFromKeyboardEvent(e);
      if (combo) {
        sawMainKey = true;
        void commitHotkey(combo);
      }
    }
    function onKeyUp(e: KeyboardEvent) {
      e.preventDefault();
      e.stopPropagation();
      if (sawMainKey) return; // a combo already committed on key-down
      const bare = bareModifierFromCode(e.code);
      if (bare) void commitHotkey(bare);
    }
    window.addEventListener("keydown", onKeyDown, true);
    window.addEventListener("keyup", onKeyUp, true);
    return () => {
      window.removeEventListener("keydown", onKeyDown, true);
      window.removeEventListener("keyup", onKeyUp, true);
    };
  }, [capturing]);

  function goTo(s: Section) {
    setSection(s);
    setError("");
    setSuccess("");
  }

  // Shared preference-save path: merge a patch, persist, sync local + parent.
  async function savePrefs(patch: Partial<models.Preferences>, msg: string) {
    if (!prefs) return;
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const updated = new models.Preferences({ ...prefs, ...patch });
      await UpdatePreferences(updated);
      setPrefs(updated);
      onPrefsChange?.(updated);
      setSuccess(msg);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  function saveInterval() {
    const sec = Math.max(1, Math.round(Number(intervalSec) || 3));
    return savePrefs({ captureIntervalMs: sec * 1000 }, "Capture interval saved.");
  }

  function saveTimerSettings() {
    const limit = Math.max(0, Math.round(Number(limitMinutes) || 0));
    const warning = Math.max(0, Math.round(Number(warningMinutes) || 0));
    if (limit > 0 && warning >= limit) {
      setError("Warning time must be less than the session limit.");
      return;
    }
    return savePrefs(
      { sessionLimitMinutes: limit, softWarningMinutes: warning },
      "Session timer saved."
    );
  }

  function saveModel(modelId: string) {
    return savePrefs({ model: modelId }, "Model saved.");
  }

  // The provider actually in effect: the saved choice if its key exists, else
  // whichever provider is configured. Mirrors app.go's activeTTS fallback so the
  // picker and saved voice stay consistent with what gets spoken.
  function resolveProvider(pref: string): string {
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

  // Enable/disable push-to-talk, then re-read hook status (the backend starts or
  // stops the global hook in UpdatePreferences). The delayed re-read catches the
  // async hookEnabled confirmation (and, on macOS, a denied-permission outcome).
  async function savePTTEnabled(on: boolean) {
    await savePrefs(
      { pushToTalkEnabled: on },
      on ? "Push-to-talk enabled." : "Push-to-talk disabled."
    );
    refreshHotkeyStatus();
    setTimeout(refreshHotkeyStatus, 600);
  }

  // Persist a captured hotkey and stop listening.
  async function commitHotkey(spec: string) {
    setCapturing(false);
    await savePrefs({ pushToTalkKey: spec }, "Hotkey saved.");
    refreshHotkeyStatus();
    setTimeout(refreshHotkeyStatus, 600);
  }

  // Persist the slider's current value once the user finishes dragging.
  function saveVoiceSpeed() {
    return savePrefs({ voiceSpeed }, "Voice speed saved.");
  }

  const keyInputs: Record<KeyProvider, string> = {
    openrouter: openRouterKey,
    elevenlabs: elevenLabsKey,
    google: googleKey,
  };
  const keySetters: Record<KeyProvider, (v: string) => void> = {
    openrouter: setOpenRouterKey,
    elevenlabs: setElevenLabsKey,
    google: setGoogleKey,
  };

  async function saveKey(provider: KeyProvider) {
    const key = keyInputs[provider].trim();
    if (!key) return;
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      await SetAPIKey(provider, key);
      onAuthChange(await GetAuthStatus());
      keySetters[provider]("");
      setSuccess(`${PROVIDER_LABELS[provider]} API key saved.`);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  // Remove a stored key. The backend nils the matching client, so STT/TTS
  // provider resolution falls back to whatever remains configured.
  async function removeKey(provider: KeyProvider) {
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      await DeleteAPIKey(provider);
      onAuthChange(await GetAuthStatus());
      keySetters[provider]("");
      setSuccess(`${PROVIDER_LABELS[provider]} API key removed.`);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  // On-demand update check for the About section. Mirrors the launch-time banner
  // but driven by the button; errors surface inline via the shared error line.
  async function checkForUpdate() {
    setChecking(true);
    setError("");
    setSuccess("");
    try {
      setUpdateInfo(await CheckForUpdate());
      setCheckedOnce(true);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setChecking(false);
    }
  }

  const activeProvider = resolveProvider(ttsProvider);
  const anyVoiceConfigured = authStatus.googleConfigured || authStatus.elevenLabsConfigured;
  const configured: Record<KeyProvider, boolean> = {
    openrouter: authStatus.openRouterConfigured,
    elevenlabs: authStatus.elevenLabsConfigured,
    google: authStatus.googleConfigured,
  };

  return (
    <div className="settings-page">
      <div className="settings-layout">
        <aside className="settings-sidebar">
          <h2 className="settings-sidebar-title">Configuration</h2>
          <nav className="settings-nav">
            {NAV.map((item) => (
              <button
                key={item.id}
                className={`settings-nav-item${section === item.id ? " active" : ""}`}
                onClick={() => goTo(item.id)}
              >
                <span className="material-symbols-outlined">{item.icon}</span>
                <span>{item.label}</span>
              </button>
            ))}
          </nav>
        </aside>

        <div className="settings-content">
          {section === "general" && (
            <>
              <header className="settings-head">
                <h1>General</h1>
                <p>Session behavior and timing for your mock interviews.</p>
              </header>
              <div className="settings-card">
                <div className="settings-card-head">
                  <span className="material-symbols-outlined">contrast</span>
                  <h3 className="settings-card-title">Appearance</h3>
                </div>
                <p className="settings-hint">
                  Color theme for the app. “System” follows your OS light/dark setting.
                </p>
                <div className="settings-segmented">
                  {(["system", "light", "dark"] as ThemePref[]).map((opt) => (
                    <button
                      key={opt}
                      type="button"
                      className={`settings-segment${themePref === opt ? " active" : ""}`}
                      onClick={() => onThemeChange(opt)}
                    >
                      {opt === "system" ? "System" : opt === "light" ? "Light" : "Dark"}
                    </button>
                  ))}
                </div>
              </div>
              <div className="settings-card">
                <h3 className="settings-card-title">Session time limit</h3>
                <p className="settings-hint">
                  Set the limit to 0 for untimed practice. The warning fires N minutes
                  before the limit; 0 disables it.
                </p>
                <div className="settings-field-row">
                  <div className="settings-field">
                    <label className="settings-label">Limit (min)</label>
                    <input
                      type="number"
                      min={0}
                      className="settings-input"
                      value={limitMinutes}
                      onChange={(e) => setLimitMinutes(e.target.value)}
                      disabled={saving || !prefs}
                    />
                  </div>
                  <div className="settings-field">
                    <label className="settings-label">Warning (min)</label>
                    <input
                      type="number"
                      min={0}
                      className="settings-input"
                      value={warningMinutes}
                      onChange={(e) => setWarningMinutes(e.target.value)}
                      disabled={saving || !prefs}
                    />
                  </div>
                  <button
                    className="btn btn-primary settings-field-save"
                    onClick={saveTimerSettings}
                    disabled={saving || !prefs}
                  >
                    {saving ? "Saving…" : "Save"}
                  </button>
                </div>
              </div>
            </>
          )}

          {section === "models" && (
            <>
              <header className="settings-head">
                <h1>Models</h1>
                <p>Choose the OpenRouter model the interviewer uses to read your screen.</p>
              </header>
              <div className="settings-card">
                <div className="settings-card-head">
                  <span className="material-symbols-outlined">neurology</span>
                  <h3 className="settings-card-title">Model Architecture</h3>
                </div>
                <ModelPicker currentModelId={prefs?.model ?? ""} onSelect={saveModel} />
              </div>
            </>
          )}

          {section === "api-keys" && (
            <>
              <header className="settings-head">
                <h1>API Keys</h1>
                <p>Keys are stored locally and never leave this device except in API requests.</p>
              </header>
              {KEY_CARDS.map((card) => {
                const isSet = configured[card.id];
                return (
                  <div className="settings-card apikey-card" key={card.id}>
                    <div className="apikey-head">
                      <div className={`apikey-icon${isSet ? "" : " muted"}`}>
                        <span className="material-symbols-outlined">{card.icon}</span>
                      </div>
                      <div className="apikey-meta">
                        <div className="apikey-name">{PROVIDER_LABELS[card.id]}</div>
                        <div className="apikey-sub">{card.hint}</div>
                      </div>
                      <div className={`apikey-status${isSet ? " is-configured" : ""}`}>
                        <span className="apikey-status-dot" />
                        {isSet ? "Configured" : "Not set"}
                      </div>
                    </div>
                    <div className="apikey-field">
                      <div className="apikey-input-wrap">
                        <span className="material-symbols-outlined apikey-input-icon">key</span>
                        <input
                          type="password"
                          className="apikey-input"
                          value={keyInputs[card.id]}
                          onChange={(e) => keySetters[card.id](e.target.value)}
                          placeholder={card.placeholder}
                          disabled={saving}
                          onKeyDown={(e) => e.key === "Enter" && saveKey(card.id)}
                        />
                      </div>
                      <button
                        className="btn btn-primary settings-field-save"
                        onClick={() => saveKey(card.id)}
                        disabled={!keyInputs[card.id].trim() || saving}
                      >
                        {saving ? "Saving…" : "Save"}
                      </button>
                      {isSet && (
                        <button
                          type="button"
                          className="apikey-remove"
                          onClick={() => removeKey(card.id)}
                          disabled={saving}
                          title="Remove key"
                        >
                          <span className="material-symbols-outlined">delete</span>
                          Remove
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </>
          )}

          {section === "voice" && (
            <>
              <header className="settings-head">
                <h1>Voice Calibration</h1>
                <p>Pick the provider and voice your interviewer speaks with.</p>
              </header>
              {anyVoiceConfigured ? (
                <>
                  <div className="settings-card">
                    <div className="settings-card-head">
                      <span className="material-symbols-outlined">record_voice_over</span>
                      <h3 className="settings-card-title">Provider</h3>
                    </div>
                    <p className="settings-hint">
                      Sets the spoken voice only — Google is low-cost, ElevenLabs is premium, and
                      each remembers its own voice. Mic transcription uses ElevenLabs when its key
                      is present, otherwise Google.
                    </p>
                    <div className="settings-segmented">
                      <button
                        type="button"
                        className={`settings-segment${activeProvider === "google" ? " active" : ""}`}
                        onClick={() => saveTTSProvider("google")}
                        disabled={saving || !authStatus.googleConfigured}
                        title={authStatus.googleConfigured ? "" : "Add a Google Cloud key first"}
                      >
                        Google · low cost
                      </button>
                      <button
                        type="button"
                        className={`settings-segment${activeProvider === "elevenlabs" ? " active" : ""}`}
                        onClick={() => saveTTSProvider("elevenlabs")}
                        disabled={saving || !authStatus.elevenLabsConfigured}
                        title={authStatus.elevenLabsConfigured ? "" : "Add an ElevenLabs key first"}
                      >
                        ElevenLabs · premium
                      </button>
                    </div>
                  </div>

                  <div className="settings-card">
                    <div className="settings-card-head">
                      <span className="material-symbols-outlined">record_voice_over</span>
                      <h3 className="settings-card-title">Interviewer Voice</h3>
                    </div>
                    <p className="settings-hint">
                      Click a voice to select it, or the play button to hear a sample. During
                      a session, toggle voice mode to speak with the interviewer aloud.
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
                    />
                  </div>

                  <div className="settings-card">
                    <div className="settings-card-head">
                      <span className="material-symbols-outlined">speed</span>
                      <h3 className="settings-card-title">Speaking speed</h3>
                    </div>
                    <p className="settings-hint">
                      How fast the interviewer talks. Pitch stays natural at any speed — preview a
                      voice above to hear the change.
                    </p>
                    <div className="settings-slider-row">
                      <input
                        type="range"
                        className="settings-slider"
                        min={0.5}
                        max={2}
                        step={0.05}
                        value={voiceSpeed}
                        onChange={(e) => setVoiceSpeed(Number(e.target.value))}
                        onPointerUp={saveVoiceSpeed}
                        onKeyUp={saveVoiceSpeed}
                        disabled={saving || !prefs}
                      />
                      <span className="settings-slider-value">{voiceSpeed.toFixed(2)}×</span>
                      <button
                        type="button"
                        className="settings-link-btn"
                        onClick={() => {
                          setVoiceSpeed(1);
                          savePrefs({ voiceSpeed: 1 }, "Voice speed saved.");
                        }}
                        disabled={saving || !prefs || voiceSpeed === 1}
                      >
                        Reset
                      </button>
                    </div>
                  </div>
                </>
              ) : (
                <div className="settings-card settings-card-placeholder">
                  <span className="material-symbols-outlined">record_voice_over</span>
                  <h3 className="settings-card-title">Add a voice key first</h3>
                  <p className="settings-hint">
                    Spoken interviews need a Google Cloud or ElevenLabs API key. Add one under{" "}
                    <button className="settings-link-btn" onClick={() => goTo("api-keys")}>
                      API Keys
                    </button>{" "}
                    to choose a voice.
                  </p>
                </div>
              )}
            </>
          )}

          {section === "push-to-talk" && (
            <>
              <header className="settings-head">
                <h1>Voice Hotkey</h1>
                <p>Press a hotkey to talk to the interviewer — even while your IDE is focused.</p>
              </header>

              <div className="settings-card">
                <div className="settings-card-head">
                  <span className="material-symbols-outlined">keyboard</span>
                  <h3 className="settings-card-title">Enable voice hotkey</h3>
                </div>
                <p className="settings-hint">
                  When on, press your hotkey to start recording and press it again to stop and
                  send — same as the mic button. The key isn't captured exclusively, so it also
                  reaches your editor; pick one your IDE ignores (a right-hand modifier or
                  function key) if that's a problem.
                </p>
                <div className="settings-segmented">
                  <button
                    type="button"
                    className={`settings-segment${prefs?.pushToTalkEnabled ? " active" : ""}`}
                    onClick={() => savePTTEnabled(true)}
                    disabled={saving || !prefs}
                  >
                    On
                  </button>
                  <button
                    type="button"
                    className={`settings-segment${prefs && !prefs.pushToTalkEnabled ? " active" : ""}`}
                    onClick={() => savePTTEnabled(false)}
                    disabled={saving || !prefs}
                  >
                    Off
                  </button>
                </div>
              </div>

              <div className="settings-card">
                <div className="settings-card-head">
                  <span className="material-symbols-outlined">keyboard_command_key</span>
                  <h3 className="settings-card-title">Hotkey</h3>
                </div>
                <p className="settings-hint">
                  Click “Set hotkey”, then press the key you want — tap it once to start and
                  again to stop. A single right-hand modifier like{" "}
                  <strong>Right ⌥ Option</strong> (the default) works best — tapping it types
                  nothing and avoids the macOS key beep that a combo like Ctrl+Space causes (the
                  key still reaches your editor). Press Esc to cancel.
                </p>
                <div className="settings-field-row">
                  <button
                    type="button"
                    className={`btn ${capturing ? "btn-primary" : "btn-ghost"} settings-hotkey-btn`}
                    onClick={() => setCapturing((c) => !c)}
                    disabled={saving || !prefs}
                  >
                    {capturing ? "Press a key…" : prettyHotkey(prefs?.pushToTalkKey || "RightAlt")}
                  </button>
                  <button
                    type="button"
                    className="settings-link-btn"
                    onClick={() => commitHotkey("RightAlt")}
                    disabled={saving || !prefs}
                  >
                    Reset to default
                  </button>
                </div>
              </div>

              {hkStatus?.goos === "darwin" && prefs?.pushToTalkEnabled && (
                <div className="settings-card">
                  {hkStatus.hookEnabled ? (
                    <p className="settings-hint">
                      <span className="status-ok">●</span> Global hotkey is active.
                    </p>
                  ) : (
                    <>
                      <h3 className="settings-card-title">Enable Input Monitoring</h3>
                      <p className="settings-hint">
                        macOS needs permission for the global hotkey to fire while another app
                        is focused. Open Input Monitoring, enable “Mogi”, then relaunch
                        the app. (The mic button works without this.)
                      </p>
                      <button
                        type="button"
                        className="btn btn-primary"
                        onClick={() => OpenInputMonitoringSettings()}
                        disabled={saving}
                      >
                        Open Input Monitoring settings
                      </button>
                    </>
                  )}
                </div>
              )}
            </>
          )}

          {section === "capture" && (
            <>
              <header className="settings-head">
                <h1>Capture Prefs</h1>
                <p>How often the interviewer sees a fresh view of your screen.</p>
              </header>
              <div className="settings-card">
                <h3 className="settings-card-title">Capture interval</h3>
                <p className="settings-hint">
                  How often the app sends a fresh screenshot to the interviewer (seconds).
                </p>
                <div className="settings-field-row">
                  <input
                    type="number"
                    min={1}
                    className="settings-input settings-input-grow"
                    value={intervalSec}
                    onChange={(e) => setIntervalSec(e.target.value)}
                    disabled={saving || !prefs}
                    onKeyDown={(e) => e.key === "Enter" && saveInterval()}
                  />
                  <button
                    className="btn btn-primary settings-field-save"
                    onClick={saveInterval}
                    disabled={saving || !prefs}
                  >
                    {saving ? "Saving…" : "Save"}
                  </button>
                </div>
                <p className="settings-hint settings-hint-muted">
                  Choose the display and crop region from the Hub before starting a session.
                </p>
              </div>
            </>
          )}

          {section === "privacy" && (
            <>
              <header className="settings-head">
                <h1>Privacy</h1>
                <p>Where your data lives.</p>
              </header>
              <div className="settings-privacy-card">
                <span className="material-symbols-outlined">verified_user</span>
                <h3 className="settings-card-title">Stored locally</h3>
                <p className="settings-hint">
                  All settings and API keys are kept in a local SQLite database on this
                  device. Your API tokens never leave the client except during authenticated
                  requests to OpenRouter.
                </p>
              </div>
            </>
          )}

          {section === "about" && (
            <>
              <header className="settings-head">
                <h1>About</h1>
                <p>Version and software updates.</p>
              </header>
              <div className="settings-card">
                <div className="settings-card-head">
                  <span className="material-symbols-outlined">info</span>
                  <h3 className="settings-card-title">Mogi</h3>
                </div>
                <div className="settings-status">
                  Version: <span className="status-ok">{appVersion || "—"}</span>
                </div>
                <p className="settings-hint">
                  Updates are published on GitHub. The app is unsigned, so installing one
                  means downloading the new version and replacing the app — you may need to
                  right-click → Open (or run <code>xattr -cr</code>) the first time.
                </p>
                <div className="settings-field-row">
                  <button
                    className="btn btn-primary"
                    onClick={checkForUpdate}
                    disabled={checking}
                  >
                    {checking ? "Checking…" : "Check for updates"}
                  </button>
                  {updateInfo?.available && (
                    <button
                      className="btn btn-ghost btn-icon"
                      onClick={() =>
                        OpenReleasePage(updateInfo.downloadUrl || updateInfo.releaseUrl)
                      }
                    >
                      <span className="material-symbols-outlined">download</span>
                      Download {updateInfo.latestVersion}
                    </button>
                  )}
                </div>
                {checkedOnce && !checking && (
                  updateInfo?.available ? (
                    <p className="settings-hint">
                      <span className="status-ok">●</span> {updateInfo.latestVersion} is
                      available — you have {updateInfo.currentVersion}.
                    </p>
                  ) : updateInfo?.latestVersion ? (
                    <p className="settings-hint">
                      <span className="status-ok">●</span> You're on the latest version.
                    </p>
                  ) : (
                    <p className="settings-hint settings-hint-muted">
                      No published releases to compare against yet.
                    </p>
                  )
                )}
              </div>
            </>
          )}

          {error && <p className="settings-error">{error}</p>}
          {success && <p className="settings-success">{success}</p>}
        </div>
      </div>
    </div>
  );
}
