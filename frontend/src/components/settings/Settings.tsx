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
import { comboFromKeyboardEvent, bareModifierFromCode, hotkeyKeycaps } from "../../lib/hotkey";
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

// The two spoken-voice providers, rendered as selectable tiles in Voice
// Calibration. `tone` drives the pill color (low-cost = matcha, premium =
// gold); `keyLabel` names the API key a tile needs when it isn't configured.
const VOICE_PROVIDERS = [
  { id: "google", name: "Google", tag: "Low cost", tone: "low", keyLabel: "Google Cloud" },
  { id: "elevenlabs", name: "ElevenLabs", tag: "Premium", tone: "premium", keyLabel: "ElevenLabs" },
] as const;

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

// Sidebar sections are grouped by concern. A group with a `label` shows a small
// uppercase heading; the trailing info group (Privacy/About) has no label and
// instead sits below a divider line (`divider: true`).
interface NavGroup {
  label?: string;
  divider?: boolean;
  items: NavItem[];
}

const NAV_GROUPS: NavGroup[] = [
  {
    label: "Interview",
    items: [
      { id: "general", label: "General", icon: "tune" },
      { id: "models", label: "Models", icon: "neurology" },
      { id: "capture", label: "Capture Prefs", icon: "settings_input_component" },
    ],
  },
  {
    label: "Voice",
    items: [
      { id: "voice", label: "Voice Calibration", icon: "record_voice_over" },
      { id: "push-to-talk", label: "Voice Hotkey", icon: "keyboard" },
    ],
  },
  {
    label: "Keys",
    items: [{ id: "api-keys", label: "API Keys", icon: "key" }],
  },
  {
    divider: true,
    items: [
      { id: "privacy", label: "Privacy", icon: "security" },
      { id: "about", label: "About", icon: "info" },
    ],
  },
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
  // API-key card state. A card rests in "view" (configured) or "bare" (not set);
  // `keyUi` holds a transient override — "edit" (replacing) or "confirmRemove".
  // Only one overflow menu is open at a time.
  const [keyUi, setKeyUi] = useState<Partial<Record<KeyProvider, "edit" | "confirmRemove">>>({});
  const [openKeyMenu, setOpenKeyMenu] = useState<KeyProvider | null>(null);
  // Per-provider reveal toggle for the key being entered (edit/bare only — the
  // stored key is never sent to the frontend, so there's nothing to reveal in view).
  const [keyReveal, setKeyReveal] = useState<Partial<Record<KeyProvider, boolean>>>({});

  const [prefs, setPrefs] = useState<models.Preferences | null>(null);
  const [intervalSec, setIntervalSec] = useState("3");
  const [limitMinutes, setLimitMinutes] = useState("30");
  const [warningMinutes, setWarningMinutes] = useState("25");
  // Live slider value; persisted only on commit (pointer/key up) to avoid a DB
  // write per step. Drives the readout and the VoicePicker preview speed.
  const [voiceSpeed, setVoiceSpeed] = useState(1);
  // Active TTS provider; drives the voice picker and which voice field is saved.
  const [ttsProvider, setTtsProvider] = useState("google");
  // Count of the active provider's voices, reported up by VoicePicker so the
  // "N voices available" note can sit in the section header (outside the list).
  const [voiceCount, setVoiceCount] = useState<number | null>(null);
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

  // Set (or clear) a card's transient mode, closing any menu and discarding the
  // draft input so a cancelled edit never lingers.
  function setKeyMode(provider: KeyProvider, mode: "edit" | "confirmRemove" | null) {
    setKeyUi((s) => {
      const next = { ...s };
      if (mode) next[provider] = mode;
      else delete next[provider];
      return next;
    });
    setOpenKeyMenu(null);
    keySetters[provider]("");
    setKeyReveal((s) => ({ ...s, [provider]: false }));
    setError("");
    setSuccess("");
  }

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
      setKeyUi((s) => {
        const next = { ...s };
        delete next[provider]; // back to resting "view"
        return next;
      });
      setKeyReveal((s) => ({ ...s, [provider]: false }));
      setOpenKeyMenu(null);
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
      setKeyUi((s) => {
        const next = { ...s };
        delete next[provider]; // back to resting "bare"
        return next;
      });
      setOpenKeyMenu(null);
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

  // Voice-hotkey footer state. Off → muted; on but (macOS) still awaiting Input
  // Monitoring → warning with a shortcut to grant it; otherwise the hook is live.
  const pttEnabled = !!prefs?.pushToTalkEnabled;
  const needsInputMonitoring =
    hkStatus?.goos === "darwin" && pttEnabled && !hkStatus.hookEnabled;
  const hotkeyStatus: { tone: "off" | "active" | "warning"; text: string } = !pttEnabled
    ? { tone: "off", text: "Voice hotkey is off" }
    : needsInputMonitoring
      ? { tone: "warning", text: "Needs Input Monitoring — enable Mogi, then relaunch" }
      : { tone: "active", text: "Global hotkey is active" };

  return (
    <div className="settings-page">
      <div className="settings-layout">
        <aside className="settings-sidebar">
          <nav className="settings-nav">
            {NAV_GROUPS.map((group, i) => (
              <div
                key={group.label ?? `group-${i}`}
                className={`settings-nav-group${group.divider ? " has-divider" : ""}`}
              >
                {group.label && (
                  <span className="settings-nav-group-label">{group.label}</span>
                )}
                {group.items.map((item) => (
                  <button
                    key={item.id}
                    className={`settings-nav-item${section === item.id ? " active" : ""}`}
                    onClick={() => goTo(item.id)}
                  >
                    <span className="material-symbols-outlined">{item.icon}</span>
                    <span>{item.label}</span>
                  </button>
                ))}
              </div>
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
            // No card chrome here: "Model Architecture" is promoted to the section
            // header and the picker sits directly on the pane, so there's no inner
            // card boundary — the inset list well is the only container left.
            <>
              <header className="settings-head">
                <h1>Model Architecture</h1>
              </header>
              <ModelPicker currentModelId={prefs?.model ?? ""} onSelect={saveModel} />
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
                // Resting mode follows configured status; keyUi overrides it while
                // the user is replacing or confirming a remove.
                const mode = keyUi[card.id] ?? (isSet ? "view" : "bare");
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

                    {/* VIEW — a stored key, shown as a masked (non-revealable, the
                        frontend never holds it) field with an overflow menu. */}
                    {mode === "view" && (
                      <div className="apikey-row">
                        <div className="apikey-input-wrap apikey-input-wrap-static">
                          <span className="material-symbols-outlined apikey-input-icon">key</span>
                          <span className="apikey-masked">••••••••••••••••</span>
                        </div>
                        <div className="apikey-menu-wrap">
                          <button
                            type="button"
                            className="apikey-menu-btn"
                            title="More actions"
                            aria-label="More actions"
                            onClick={() =>
                              setOpenKeyMenu(openKeyMenu === card.id ? null : card.id)
                            }
                            disabled={saving}
                          >
                            <span className="material-symbols-outlined">more_vert</span>
                          </button>
                          {openKeyMenu === card.id && (
                            <>
                              <div
                                className="apikey-menu-overlay"
                                onClick={() => setOpenKeyMenu(null)}
                              />
                              <div className="apikey-menu" role="menu">
                                <button
                                  type="button"
                                  className="apikey-menu-item"
                                  onClick={() => setKeyMode(card.id, "edit")}
                                >
                                  <span className="material-symbols-outlined">sync</span>
                                  Replace key
                                </button>
                                <button
                                  type="button"
                                  className="apikey-menu-item apikey-menu-item-danger"
                                  onClick={() => setKeyMode(card.id, "confirmRemove")}
                                >
                                  <span className="material-symbols-outlined">delete</span>
                                  Remove key
                                </button>
                              </div>
                            </>
                          )}
                        </div>
                      </div>
                    )}

                    {/* EDIT (replacing) / BARE (first time) — editable input + Save. */}
                    {(mode === "edit" || mode === "bare") && (
                      <div className="apikey-row">
                        <div
                          className={`apikey-input-wrap${mode === "edit" ? " is-editing" : ""}`}
                        >
                          <span className="material-symbols-outlined apikey-input-icon">key</span>
                          <input
                            type={keyReveal[card.id] ? "text" : "password"}
                            className="apikey-input"
                            value={keyInputs[card.id]}
                            onChange={(e) => keySetters[card.id](e.target.value)}
                            placeholder={card.placeholder}
                            disabled={saving}
                            autoFocus={mode === "edit"}
                            onKeyDown={(e) => e.key === "Enter" && saveKey(card.id)}
                          />
                          <button
                            type="button"
                            className="apikey-input-eye"
                            onClick={() =>
                              setKeyReveal((s) => ({ ...s, [card.id]: !s[card.id] }))
                            }
                            title={keyReveal[card.id] ? "Hide key" : "Show key"}
                            aria-label={keyReveal[card.id] ? "Hide key" : "Show key"}
                            tabIndex={-1}
                          >
                            <span className="material-symbols-outlined">
                              {keyReveal[card.id] ? "visibility_off" : "visibility"}
                            </span>
                          </button>
                        </div>
                        <button
                          className="btn btn-primary settings-field-save"
                          onClick={() => saveKey(card.id)}
                          disabled={!keyInputs[card.id].trim() || saving}
                        >
                          {saving ? "Saving…" : "Save"}
                        </button>
                        {mode === "edit" && (
                          <button
                            type="button"
                            className="apikey-icon-btn"
                            title="Cancel"
                            aria-label="Cancel"
                            onClick={() => setKeyMode(card.id, null)}
                            disabled={saving}
                          >
                            <span className="material-symbols-outlined">close</span>
                          </button>
                        )}
                      </div>
                    )}

                    {/* CONFIRM REMOVE — a beat before the destructive action. */}
                    {mode === "confirmRemove" && (
                      <div className="apikey-confirm">
                        <span className="apikey-confirm-text">
                          Remove this key? You'll need to paste it again later.
                        </span>
                        <div className="apikey-confirm-actions">
                          <button
                            type="button"
                            className="apikey-confirm-keep"
                            onClick={() => setKeyMode(card.id, null)}
                            disabled={saving}
                          >
                            Keep it
                          </button>
                          <button
                            type="button"
                            className="apikey-confirm-remove"
                            onClick={() => removeKey(card.id)}
                            disabled={saving}
                          >
                            {saving ? "Removing…" : "Remove"}
                          </button>
                        </div>
                      </div>
                    )}
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
                      Sets the spoken voice only. Mic transcription uses ElevenLabs when its key is
                      present, otherwise Google.
                    </p>
                    <div className="vc-provider-grid">
                      {VOICE_PROVIDERS.map((p) => {
                        const selected = activeProvider === p.id;
                        const isConfigured =
                          p.id === "google"
                            ? authStatus.googleConfigured
                            : authStatus.elevenLabsConfigured;
                        const remembered =
                          p.id === "google" ? prefs?.googleVoiceId : prefs?.voiceId;
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
                              {remembered || "Not set"}
                            </div>
                          </button>
                        );
                      })}
                    </div>
                  </div>

                  <div className="vc-divider" />

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
                      onCountChange={setVoiceCount}
                    />
                  </div>

                  <div className="vc-divider" />

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

              {/* One consolidated card: enable toggle, key binding, footer status. */}
              <div className="settings-card hotkey-card">
                <div className="hotkey-head">
                  <div className="hotkey-head-left">
                    <span className="hotkey-icon">
                      <span className="material-symbols-outlined">keyboard</span>
                    </span>
                    <div>
                      <div className="hotkey-title">Enable voice hotkey</div>
                      <div className="hotkey-subtitle">Toggle push-to-talk on or off</div>
                    </div>
                  </div>
                  <div className="settings-segmented hotkey-toggle">
                    <button
                      type="button"
                      className={`settings-segment${pttEnabled ? " active" : ""}`}
                      onClick={() => savePTTEnabled(true)}
                      disabled={saving || !prefs}
                    >
                      On
                    </button>
                    <button
                      type="button"
                      className={`settings-segment${prefs && !pttEnabled ? " active" : ""}`}
                      onClick={() => savePTTEnabled(false)}
                      disabled={saving || !prefs}
                    >
                      Off
                    </button>
                  </div>
                </div>

                <p className="settings-hint hotkey-desc">
                  When on, press your hotkey to start recording and press it again to stop and
                  send — same as the mic button. The key isn't captured exclusively, so it also
                  reaches your editor; pick one your IDE ignores (a right-hand modifier or
                  function key) if that's a problem.
                </p>

                <div className="hotkey-divider" />

                <div className={`hotkey-bind${pttEnabled ? "" : " is-disabled"}`}>
                  <div className="hotkey-bind-head">
                    <span className="material-symbols-outlined">keyboard_command_key</span>
                    <span className="hotkey-bind-label">Assigned key</span>
                  </div>
                  <p className="settings-hint">
                    Click the key field, then press the key you want — tap once to start, again
                    to stop. A single right-hand modifier like <strong>Right ⌥ Option</strong>{" "}
                    works best: tapping it types nothing and avoids the macOS beep a combo like
                    Ctrl+Space causes. Press Esc to cancel.
                  </p>
                  <div className="hotkey-bind-row">
                    <button
                      type="button"
                      className={`hotkey-chip${capturing ? " is-capturing" : ""}`}
                      onClick={() => setCapturing((c) => !c)}
                      disabled={saving || !prefs}
                    >
                      {capturing ? (
                        <span className="hotkey-chip-prompt">Press a key…</span>
                      ) : (
                        <span className="hotkey-keycaps">
                          {hotkeyKeycaps(prefs?.pushToTalkKey || "RightAlt").map((cap, i) => (
                            <span className="hotkey-keycap" key={i}>
                              {cap}
                            </span>
                          ))}
                        </span>
                      )}
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

                <div className={`hotkey-status hotkey-status--${hotkeyStatus.tone}`}>
                  <span className="hotkey-status-dot" />
                  <span className="hotkey-status-text">{hotkeyStatus.text}</span>
                  {hotkeyStatus.tone === "warning" && (
                    <button
                      type="button"
                      className="hotkey-status-action"
                      onClick={() => OpenInputMonitoringSettings()}
                      disabled={saving}
                    >
                      Open settings
                    </button>
                  )}
                </div>
              </div>
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
