import { useEffect, useState } from "react";
import {
  GetAuthStatus,
  GetAppVersion,
  GetHotkeyStatus,
  GetPreferences,
  SignOutTestAccount,
  UpdatePreferences,
  models,
  hotkey,
} from "../../lib/wailsBridge";
import type { ThemePref } from "../../lib/theme";
// Shell CSS first so shared .settings-* classes load before section styles.
import "./Settings.css";
import AboutSection from "./AboutSection";
import ApiKeysSection from "./ApiKeysSection";
import CaptureSection from "./CaptureSection";
import GeneralSection from "./GeneralSection";
import ManagedAccountCard from "./ManagedAccountCard";
import ModelsSection from "./ModelsSection";
import PrivacySection from "./PrivacySection";
import PushToTalkSection from "./PushToTalkSection";
import VoiceSection from "./VoiceSection";

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
  // App's single auth-status update path: pass a fresh status when in hand
  // (activation/sign-out return one), or call with no args to refetch. Either
  // way the app shell reloads its prefs copy too (KeyMode lives there).
  onAuthChange: (status?: models.AuthStatus) => void;
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
  // Count of the active provider's voices, reported up by VoicePicker so the
  // "N voices available" note can sit in the section header (outside the list).
  const [voiceCount, setVoiceCount] = useState<number | null>(null);
  // id → friendly name, accumulated across providers as their catalogs load, so
  // the provider tiles can show a stored voice's name (ElevenLabs ids are opaque
  // hashes) rather than the raw id.
  const [voiceNames, setVoiceNames] = useState<Record<string, string>>({});
  // Global voice-hotkey hook status: PushToTalkSection drives it, About reads
  // its GOOS for the build line — so it lives here in the shell.
  const [hkStatus, setHkStatus] = useState<hotkey.Status | null>(null);
  // About: app version for the identity block (the update check lives in
  // AboutSection).
  const [appVersion, setAppVersion] = useState("");

  // Sync the shell-held input mirrors from a fresh Preferences read. Used on
  // mount and after "Clear all data" resets the store.
  function seedFromPrefs(p: models.Preferences) {
    setPrefs(p);
    setIntervalSec(String(Math.max(1, Math.round(p.captureIntervalMs / 1000))));
    setLimitMinutes(String(p.sessionLimitMinutes ?? 30));
    setWarningMinutes(String(p.softWarningMinutes ?? 25));
    setVoiceSpeed(p.voiceSpeed || 1);
    setTtsProvider(p.ttsProvider || "google");
  }

  // Load preferences on mount.
  useEffect(() => {
    GetPreferences().then(seedFromPrefs).catch(() => {});
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

  // Ingest a provider's loaded catalog: track its size for the header note and
  // merge id→name so the provider tiles can label stored voices by name.
  function handleCatalog(voices: models.Voice[]) {
    setVoiceCount(voices.length);
    setVoiceNames((prev) => {
      const next = { ...prev };
      for (const v of voices) if (v.name) next[v.id] = v.name;
      return next;
    });
  }

  // After PrivacySection wipes the store: reload auth + prefs from the now-empty
  // database so the whole UI reflects the reset without a restart.
  async function handleDataCleared() {
    onAuthChange(await GetAuthStatus());
    const p = await GetPreferences();
    seedFromPrefs(p);
    onPrefsChange?.(p);
  }

  // Flip between managed and BYOK key modes. The write goes through the normal
  // savePrefs → UpdatePreferences path — the backend's Settings.Update is the
  // single chokepoint that re-resolves the provider registry on a KeyMode
  // change — then the auth status is refetched, since which keys count as
  // "configured" just switched namespaces.
  async function switchKeyMode(mode: "managed" | "byok", msg: string) {
    await savePrefs({ keyMode: mode }, msg);
    onAuthChange();
  }

  // Sign out of the managed test account (device-local): the backend deletes
  // the managed keys + session and flips KeyMode to "byok", leaving BYOK keys
  // untouched. Re-seed the local prefs mirror — the store-side KeyMode changed
  // under it (same recovery as handleDataCleared).
  async function handleSignOut() {
    setSaving(true);
    setError("");
    setSuccess("");
    try {
      const status = await SignOutTestAccount();
      onAuthChange(status);
      seedFromPrefs(await GetPreferences());
      setSuccess("Signed out of the test account. Your own keys were not touched.");
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSaving(false);
    }
  }

  // A test account was activated from the invite footer: the backend installed
  // the managed keys and flipped KeyMode. Push the fresh status up and re-seed
  // the local prefs mirror.
  async function handleActivated(status: models.AuthStatus) {
    onAuthChange(status);
    try {
      seedFromPrefs(await GetPreferences());
    } catch {
      // Wails runtime not present in browser preview.
    }
    setSuccess("Test account activated.");
  }

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

        {/* Flush layout: the page title sits on the background and each section's
            content floats as its own card(s) below it. */}
        <div className="settings-content">
          {section === "general" && (
            <GeneralSection
              themePref={themePref}
              onThemeChange={onThemeChange}
              limitMinutes={limitMinutes}
              setLimitMinutes={setLimitMinutes}
              warningMinutes={warningMinutes}
              setWarningMinutes={setWarningMinutes}
              prefs={prefs}
              saving={saving}
              savePrefs={savePrefs}
              setError={setError}
            />
          )}

          {section === "models" && (
            <ModelsSection
              authStatus={authStatus}
              prefs={prefs}
              savePrefs={savePrefs}
              onOpenApiKeys={() => goTo("api-keys")}
            />
          )}

          {/* API Keys forks by mode: the managed account card replaces the
              per-provider key cards while KeyMode is "managed". */}
          {section === "api-keys" &&
            (authStatus.keyMode === "managed" ? (
              <ManagedAccountCard
                authStatus={authStatus}
                saving={saving}
                onSwitchToByok={() =>
                  switchKeyMode("byok", "Switched to your own keys — you're still signed in.")
                }
                onSignOut={handleSignOut}
              />
            ) : (
              <ApiKeysSection
                authStatus={authStatus}
                onAuthChange={onAuthChange}
                saving={saving}
                setSaving={setSaving}
                setError={setError}
                setSuccess={setSuccess}
                onSwitchBack={() =>
                  switchKeyMode("managed", "Switched back to your test account.")
                }
                onActivated={handleActivated}
              />
            ))}

          {section === "voice" && (
            <VoiceSection
              authStatus={authStatus}
              prefs={prefs}
              saving={saving}
              savePrefs={savePrefs}
              ttsProvider={ttsProvider}
              setTtsProvider={setTtsProvider}
              voiceSpeed={voiceSpeed}
              setVoiceSpeed={setVoiceSpeed}
              voiceCount={voiceCount}
              voiceNames={voiceNames}
              onCatalog={handleCatalog}
              onOpenApiKeys={() => goTo("api-keys")}
            />
          )}

          {section === "push-to-talk" && (
            <PushToTalkSection
              prefs={prefs}
              saving={saving}
              savePrefs={savePrefs}
              hkStatus={hkStatus}
              onRefreshHotkeyStatus={refreshHotkeyStatus}
            />
          )}

          {section === "capture" && (
            <CaptureSection
              intervalSec={intervalSec}
              setIntervalSec={setIntervalSec}
              prefs={prefs}
              saving={saving}
              savePrefs={savePrefs}
            />
          )}

          {section === "privacy" && (
            <PrivacySection
              setError={setError}
              setSuccess={setSuccess}
              onDataCleared={handleDataCleared}
            />
          )}

          {section === "about" && (
            <AboutSection
              appVersion={appVersion}
              goos={hkStatus?.goos}
              setError={setError}
              setSuccess={setSuccess}
            />
          )}

          {error && <p className="settings-error">{error}</p>}
          {success && <p className="settings-success">{success}</p>}
        </div>
      </div>
    </div>
  );
}
