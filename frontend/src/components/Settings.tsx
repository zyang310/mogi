import { useEffect, useState } from "react";
import {
  SetAPIKey,
  GetAuthStatus,
  GetPreferences,
  UpdatePreferences,
  models,
} from "../lib/wailsBridge";
import ModelPicker from "./ModelPicker";
import "./Settings.css";

// Settings is a full page (not a modal). A left "Configuration" sidebar switches
// between sections; the right pane renders the active section. Navigation in/out
// is handled by the app's pill-nav, so there's no close button here.
type Section = "general" | "models" | "api-keys" | "voice" | "capture" | "privacy";

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
  { id: "capture", label: "Capture Prefs", icon: "settings_input_component" },
  { id: "privacy", label: "Privacy", icon: "security" },
];

interface Props {
  authStatus: models.AuthStatus;
  onAuthChange: (status: models.AuthStatus) => void;
  // Bubble persisted preference changes up so the hub stays in sync.
  onPrefsChange?: (prefs: models.Preferences) => void;
}

export default function Settings({ authStatus, onAuthChange, onPrefsChange }: Props) {
  // Land on API Keys when unconfigured (the most useful next step), else Models.
  const [section, setSection] = useState<Section>(
    authStatus.openRouterConfigured ? "models" : "api-keys"
  );
  const [openRouterKey, setOpenRouterKey] = useState("");
  const [elevenLabsKey, setElevenLabsKey] = useState("");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");

  const [prefs, setPrefs] = useState<models.Preferences | null>(null);
  const [intervalSec, setIntervalSec] = useState("3");
  const [limitMinutes, setLimitMinutes] = useState("30");
  const [warningMinutes, setWarningMinutes] = useState("25");

  // Load preferences on mount.
  useEffect(() => {
    GetPreferences()
      .then((p) => {
        setPrefs(p);
        setIntervalSec(String(Math.max(1, Math.round(p.captureIntervalMs / 1000))));
        setLimitMinutes(String(p.sessionLimitMinutes ?? 30));
        setWarningMinutes(String(p.softWarningMinutes ?? 25));
      })
      .catch(() => {});
  }, []);

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

  async function saveKey(provider: "openrouter" | "elevenlabs") {
    const key = (provider === "openrouter" ? openRouterKey : elevenLabsKey).trim();
    if (!key) return;
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      await SetAPIKey(provider, key);
      onAuthChange(await GetAuthStatus());
      if (provider === "openrouter") {
        setOpenRouterKey("");
        setSuccess("OpenRouter API key saved.");
      } else {
        setElevenLabsKey("");
        setSuccess("ElevenLabs API key saved.");
      }
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

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
              <div className="settings-card">
                <h3 className="settings-card-title">OpenRouter</h3>
                <p className="settings-hint">
                  Get a key at{" "}
                  <a href="https://openrouter.ai/keys" target="_blank" rel="noopener noreferrer">
                    openrouter.ai/keys
                  </a>
                </p>
                <div className="settings-status">
                  Status:{" "}
                  {authStatus.openRouterConfigured ? (
                    <span className="status-ok">Configured</span>
                  ) : (
                    <span className="status-missing">Not configured</span>
                  )}
                </div>
                <div className="settings-field-row">
                  <input
                    type="password"
                    className="settings-input settings-input-grow"
                    value={openRouterKey}
                    onChange={(e) => setOpenRouterKey(e.target.value)}
                    placeholder="sk-or-..."
                    disabled={saving}
                    onKeyDown={(e) => e.key === "Enter" && saveKey("openrouter")}
                  />
                  <button
                    className="btn btn-primary settings-field-save"
                    onClick={() => saveKey("openrouter")}
                    disabled={!openRouterKey.trim() || saving}
                  >
                    {saving ? "Saving…" : "Save"}
                  </button>
                </div>
              </div>

              <div className="settings-card">
                <h3 className="settings-card-title">ElevenLabs</h3>
                <p className="settings-hint">
                  Optional — used for spoken interviews (Phase 2). Get a key at{" "}
                  <a
                    href="https://elevenlabs.io/app/settings/api-keys"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    elevenlabs.io
                  </a>
                </p>
                <div className="settings-status">
                  Status:{" "}
                  {authStatus.elevenLabsConfigured ? (
                    <span className="status-ok">Configured</span>
                  ) : (
                    <span className="status-missing">Not configured</span>
                  )}
                </div>
                <div className="settings-field-row">
                  <input
                    type="password"
                    className="settings-input settings-input-grow"
                    value={elevenLabsKey}
                    onChange={(e) => setElevenLabsKey(e.target.value)}
                    placeholder="sk-el-..."
                    disabled={saving}
                    onKeyDown={(e) => e.key === "Enter" && saveKey("elevenlabs")}
                  />
                  <button
                    className="btn btn-primary settings-field-save"
                    onClick={() => saveKey("elevenlabs")}
                    disabled={!elevenLabsKey.trim() || saving}
                  >
                    {saving ? "Saving…" : "Save"}
                  </button>
                </div>
              </div>
            </>
          )}

          {section === "voice" && (
            <>
              <header className="settings-head">
                <h1>Voice Calibration</h1>
                <p>Spoken interviews powered by ElevenLabs.</p>
              </header>
              <div className="settings-card settings-card-placeholder">
                <span className="material-symbols-outlined">record_voice_over</span>
                <h3 className="settings-card-title">Coming in Phase 2</h3>
                <p className="settings-hint">
                  Voice input and a spoken interviewer aren't wired up yet. For now, type
                  your responses in the chat during a session.
                </p>
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

          {error && <p className="settings-error">{error}</p>}
          {success && <p className="settings-success">{success}</p>}
        </div>
      </div>
    </div>
  );
}
