import { useState } from "react";
import { SetAPIKey, DeleteAPIKey, GetAuthStatus, models } from "../../lib/wailsBridge";
import ApiKeyCard from "./ApiKeyCard";
import type { KeyCard, KeyProvider } from "./ApiKeyCard";
import InviteActivation from "../setup/InviteActivation";
import "./ApiKeysSection.css";

const PROVIDER_LABELS: Record<KeyProvider, string> = {
  openrouter: "OpenRouter",
  elevenlabs: "ElevenLabs",
  google: "Google Cloud",
};

// Per-provider metadata for the API-key cards. The three cards are structurally
// identical, so we drive them from this list (label, icon tile, input hint,
// placeholder) and render one <ApiKeyCard> per entry rather than hand-repeating.
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

interface Props {
  authStatus: models.AuthStatus;
  onAuthChange: (status: models.AuthStatus) => void;
  // Save/remove flows run through the shell's shared saving/error/success
  // status so inputs across Settings disable together.
  saving: boolean;
  setSaving: (v: boolean) => void;
  setError: (msg: string) => void;
  setSuccess: (msg: string) => void;
  // Flip KeyMode back to "managed" (the switch-back banner) — the managed
  // account stayed signed in when the user switched to their own keys.
  onSwitchBack: () => void;
  // A test account was activated from the invite footer; the shell pushes the
  // fresh status up and re-seeds its prefs mirror (KeyMode just changed).
  onActivated: (status: models.AuthStatus) => void;
}

// ApiKeysSection is the Settings → API Keys pane: one ApiKeyCard per provider,
// driving save/replace/remove against the backend key store (keys live only in
// the Go backend — the frontend never sees a stored key).
export default function ApiKeysSection({
  authStatus,
  onAuthChange,
  saving,
  setSaving,
  setError,
  setSuccess,
  onSwitchBack,
  onActivated,
}: Props) {
  // "Have an invite?" footer: collapsed to a single row until opened.
  const [inviteOpen, setInviteOpen] = useState(false);
  const [openRouterKey, setOpenRouterKey] = useState("");
  const [elevenLabsKey, setElevenLabsKey] = useState("");
  const [googleKey, setGoogleKey] = useState("");
  // A card rests in "view" (configured) or "bare" (not set); `keyUi` holds a
  // transient override — "edit" (replacing) or "confirmRemove". Only one
  // overflow menu is open at a time.
  const [keyUi, setKeyUi] = useState<Partial<Record<KeyProvider, "edit" | "confirmRemove">>>({});
  const [openKeyMenu, setOpenKeyMenu] = useState<KeyProvider | null>(null);
  // Per-provider reveal toggle for the key being entered (edit/bare only — the
  // stored key is never sent to the frontend, so there's nothing to reveal in view).
  const [keyReveal, setKeyReveal] = useState<Partial<Record<KeyProvider, boolean>>>({});

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

  const configured: Record<KeyProvider, boolean> = {
    openrouter: authStatus.openRouterConfigured,
    elevenlabs: authStatus.elevenLabsConfigured,
    google: authStatus.googleConfigured,
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

  return (
    <>
      <header className="settings-head">
        <h1>API Keys</h1>
        <p>Keys are stored locally and never leave this device except in API requests.</p>
      </header>

      {/* Still signed in to a test account, but running on own keys: offer the
          way back. (Signing out entirely lives on the managed card.) */}
      {authStatus.managedActive && (
        <div className="settings-card apikeys-managed-banner">
          <span className="material-symbols-outlined">verified</span>
          <div className="apikeys-managed-banner-text">
            <p className="apikeys-managed-banner-title">
              Signed in as {authStatus.managedEmail}
            </p>
            <p className="apikeys-managed-banner-sub">
              You're using your own keys — the test account stays signed in.
            </p>
          </div>
          <button className="btn btn-primary" disabled={saving} onClick={onSwitchBack}>
            Switch back
          </button>
        </div>
      )}

      {KEY_CARDS.map((card) => {
        const isSet = configured[card.id];
        return (
          <ApiKeyCard
            key={card.id}
            card={card}
            label={PROVIDER_LABELS[card.id]}
            isSet={isSet}
            mode={keyUi[card.id] ?? (isSet ? "view" : "bare")}
            draft={keyInputs[card.id]}
            onDraftChange={keySetters[card.id]}
            revealed={!!keyReveal[card.id]}
            onToggleReveal={() =>
              setKeyReveal((s) => ({ ...s, [card.id]: !s[card.id] }))
            }
            menuOpen={openKeyMenu === card.id}
            onToggleMenu={() =>
              setOpenKeyMenu(openKeyMenu === card.id ? null : card.id)
            }
            onCloseMenu={() => setOpenKeyMenu(null)}
            onSetMode={(mode) => setKeyMode(card.id, mode)}
            onSave={() => saveKey(card.id)}
            onRemove={() => removeKey(card.id)}
            saving={saving}
          />
        );
      })}

      {/* Invite entry point for BYOK users — hidden while a test account is
          signed in (the banner above is the affordance then). */}
      {!authStatus.managedActive && (
        <div className="settings-card apikeys-invite">
          {inviteOpen ? (
            <div className="apikeys-invite-body">
              <p className="settings-card-title">Redeem an invite</p>
              <p className="settings-hint">
                Activate a developer-funded test account — no keys needed. Keys
                you've saved here are kept.
              </p>
              <InviteActivation
                onActivated={onActivated}
                onBack={() => setInviteOpen(false)}
              />
            </div>
          ) : (
            <button className="apikeys-invite-toggle" onClick={() => setInviteOpen(true)}>
              <span className="material-symbols-outlined">confirmation_number</span>
              Have an invite code? Activate a test account
              <span className="material-symbols-outlined apikeys-invite-chevron">
                chevron_right
              </span>
            </button>
          )}
        </div>
      )}
    </>
  );
}
