import { useRef, useState } from "react";
import { SetAPIKey, GetAuthStatus, models } from "../../lib/wailsBridge";
import InviteActivation from "./InviteActivation";
import MogiLogo from "../common/MogiLogo";
import "./SetupPage.css";

interface Props {
  authStatus: models.AuthStatus;
  onAuthChange: (status: models.AuthStatus) => void;
  onContinue: () => void;
}

type CheckState = "idle" | "checking" | "success" | "error";

// Setup doors: pick a path (choose), redeem a test invite (invite), or paste
// your own keys (byok — the original setup form, unchanged).
type Door = "choose" | "invite" | "byok";

const CHECK_ICON: Record<CheckState, string> = {
  idle: "sync",
  checking: "sync",
  success: "check_circle",
  error: "error",
};

// SetupPage is the launch gate: a managed signed-in device gets a pre-pass
// confirmation card, everyone else forks between the invite door (managed test
// account) and the BYOK door (own API keys — the original flow).
export default function SetupPage({ authStatus, onAuthChange, onContinue }: Props) {
  // Managed pre-pass: when the device is in managed mode (fresh activation or
  // relaunch), setup is a confirmation card, not a form. Computed per render
  // so a launch-refresh sign-out (managed:changed) drops back to the doors.
  const managed = authStatus.keyMode === "managed";

  // Where setup lands: BYOK users with keys already saved go straight to the
  // familiar form (no extra click every launch); fresh installs pick a door.
  const [door, setDoor] = useState<Door>(() =>
    authStatus.openRouterConfigured && authStatus.keyMode !== "managed"
      ? "byok"
      : "choose"
  );

  // Keys saved on a prior run (persisted in SQLite) count as pre-verified.
  const preConfigured = authStatus.openRouterConfigured;

  const [orKey, setOrKey] = useState("");
  const [elKey, setElKey] = useState("");
  const [gKey, setGKey] = useState("");
  const [showOr, setShowOr] = useState(false);
  const [showEl, setShowEl] = useState(false);
  const [showG, setShowG] = useState(false);
  const [checkState, setCheckState] = useState<CheckState>(
    preConfigured ? "success" : "idle"
  );
  const [errorMsg, setErrorMsg] = useState("");
  const [canContinue, setCanContinue] = useState(preConfigured);

  // Track the cursor for the grid "flashlight" reveal. We write CSS variables
  // straight to the DOM node rather than React state so the mask follows the
  // pointer without triggering re-renders.
  const rootRef = useRef<HTMLDivElement>(null);
  function handleGridMouseMove(e: React.MouseEvent<HTMLDivElement>) {
    const el = rootRef.current;
    if (!el) return;
    const { left, top } = el.getBoundingClientRect();
    el.style.setProperty("--setup-mx", `${e.clientX - left}px`);
    el.style.setProperty("--setup-my", `${e.clientY - top}px`);
  }

  const checkLabel: Record<CheckState, string> = {
    idle: "Check Connectivity",
    checking: "Validating…",
    success: preConfigured && !orKey.trim() ? "Keys Configured" : "Connection Verified",
    error: "Check Connectivity",
  };

  async function handleCheck() {
    // OpenRouter is required to use the app; ElevenLabs (voice) is optional.
    const haveOpenRouter = orKey.trim() !== "" || authStatus.openRouterConfigured;
    if (!haveOpenRouter) {
      setCheckState("error");
      setErrorMsg("OpenRouter API key is required.");
      setTimeout(() => {
        setCheckState(preConfigured ? "success" : "idle");
        setErrorMsg("");
      }, 2000);
      return;
    }

    setCheckState("checking");
    setErrorMsg("");

    try {
      if (orKey.trim()) await SetAPIKey("openrouter", orKey.trim());
      if (elKey.trim()) await SetAPIKey("elevenlabs", elKey.trim());
      if (gKey.trim()) await SetAPIKey("google", gKey.trim());
      const status = await GetAuthStatus();
      onAuthChange(status);

      if (status.openRouterConfigured) {
        setCheckState("success");
        setCanContinue(true);
      } else {
        throw new Error("Keys did not save correctly — please try again.");
      }
    } catch (e: any) {
      setCheckState("error");
      setErrorMsg(e?.message || String(e));
      setTimeout(() => {
        setCheckState("idle");
        setErrorMsg("");
      }, 3000);
    }
  }

  // Editing a key invalidates a prior verification — require a re-check.
  function resetCheck() {
    if (canContinue) {
      setCanContinue(false);
      setCheckState("idle");
      setErrorMsg("");
    }
  }

  const isChecking = checkState === "checking";
  const icon = CHECK_ICON[checkState];
  const label = checkState === "error" ? errorMsg || "Error" : checkLabel[checkState];

  // Per-view header copy; the byok door keeps the original text.
  const header = managed
    ? {
        title: "You're signed in",
        subtitle: "Your test account is active on this device.",
      }
    : door === "choose"
      ? { title: "Get Started", subtitle: "Choose how you'd like to set up Mogi." }
      : door === "invite"
        ? {
            title: "Redeem Invite",
            subtitle: "We'll email you a one-time code to activate your test account.",
          }
        : {
            title: "Setup Environment",
            subtitle: "Welcome to Mogi. Let's get your environment ready.",
          };

  return (
    <div className="setup-root" ref={rootRef} onMouseMove={handleGridMouseMove}>
      {/* Infinite scrolling grid — base layer */}
      <div className="setup-bg-grid" />
      {/* Highlighted grid revealed by a flashlight mask following the cursor */}
      <div className="setup-bg-grid setup-bg-grid--reveal" />

      {/* Decorative blur spheres */}
      <div className="setup-spheres">
        <div className="setup-sphere setup-sphere--1" />
        <div className="setup-sphere setup-sphere--2" />
      </div>

      <div className="setup-ambient-glow" />

      <main
        className={`setup-card${checkState === "success" || managed ? " setup-card--success" : ""}`}
      >
        {/* Header */}
        <header className="setup-header">
          <div className="setup-brand">
            <MogiLogo size={64} />
            <span className="setup-brand-name">Mogi</span>
          </div>
          <h1 className="setup-title">{header.title}</h1>
          <p className="setup-subtitle">{header.subtitle}</p>
        </header>

        {managed ? (
          /* Managed signed-in pre-pass card: nothing to configure — confirm
             the account and pass through. Account management lives in
             Settings → API Keys (2.4). */
          <div className="setup-managed">
            <span className="setup-managed-badge">
              <span className="material-symbols-outlined">verified</span>
              Test account
            </span>
            <p className="setup-managed-email">{authStatus.managedEmail}</p>
            <p className="setup-managed-note">
              Developer-funded keys are installed and refresh automatically on
              launch. You can manage or sign out of this account in Settings →
              API Keys.
            </p>
            <button className="setup-continue-btn" onClick={onContinue} type="button">
              <span>Continue to Hub</span>
              <span className="material-symbols-outlined">arrow_forward</span>
            </button>
          </div>
        ) : door === "choose" ? (
          /* Two-door fork: managed test account vs. bring-your-own-keys. */
          <div className="setup-doors">
            <button className="setup-door" onClick={() => setDoor("invite")} type="button">
              <span className="material-symbols-outlined setup-door-icon">
                confirmation_number
              </span>
              <span className="setup-door-text">
                <span className="setup-door-title">I have an invite code</span>
                <span className="setup-door-desc">
                  Activate a test account — the developer's API keys are
                  installed for you. No setup needed.
                </span>
              </span>
              <span className="material-symbols-outlined setup-door-chevron">
                chevron_right
              </span>
            </button>
            <button className="setup-door" onClick={() => setDoor("byok")} type="button">
              <span className="material-symbols-outlined setup-door-icon">key</span>
              <span className="setup-door-text">
                <span className="setup-door-title">Use my own API keys</span>
                <span className="setup-door-desc">
                  Bring OpenRouter, Google, or ElevenLabs keys. Calls go
                  straight from your machine — maximum privacy.
                </span>
              </span>
              <span className="material-symbols-outlined setup-door-chevron">
                chevron_right
              </span>
            </button>
          </div>
        ) : door === "invite" ? (
          <InviteActivation onActivated={onAuthChange} onBack={() => setDoor("choose")} />
        ) : (
          <>
        <button className="setup-back" onClick={() => setDoor("choose")} type="button">
          <span className="material-symbols-outlined">arrow_back</span>
          Other setup options
        </button>

        {/* Form */}
        <form className="setup-form" onSubmit={(e) => e.preventDefault()}>
          {/* OpenRouter key */}
          <div className="setup-field">
            <label className="setup-label" htmlFor="or-key">
              OpenRouter API Key
            </label>
            <div className="setup-input-wrap">
              <input
                autoComplete="off"
                className="setup-input"
                id="or-key"
                placeholder="sk-or-v1-..."
                type={showOr ? "text" : "password"}
                value={orKey}
                onChange={(e) => {
                  setOrKey(e.target.value);
                  resetCheck();
                }}
              />
              <button
                className="setup-vis-btn"
                onClick={() => setShowOr((v) => !v)}
                type="button"
              >
                <span className="material-symbols-outlined">
                  {showOr ? "visibility" : "visibility_off"}
                </span>
              </button>
            </div>
            {authStatus.openRouterConfigured && authStatus.keyMode !== "managed" && !orKey.trim() && (
              <p className="setup-configured-hint">
                <span className="material-symbols-outlined">check_circle</span>
                Already configured — leave blank to keep
              </p>
            )}
          </div>

          {/* ElevenLabs key */}
          <div className="setup-field">
            <label className="setup-label" htmlFor="el-key">
              ElevenLabs API Key
            </label>
            <div className="setup-input-wrap">
              <input
                autoComplete="off"
                className="setup-input"
                id="el-key"
                placeholder="sk-el-..."
                type={showEl ? "text" : "password"}
                value={elKey}
                onChange={(e) => {
                  setElKey(e.target.value);
                  resetCheck();
                }}
              />
              <button
                className="setup-vis-btn"
                onClick={() => setShowEl((v) => !v)}
                type="button"
              >
                <span className="material-symbols-outlined">
                  {showEl ? "visibility" : "visibility_off"}
                </span>
              </button>
            </div>
            {authStatus.elevenLabsConfigured && authStatus.keyMode !== "managed" && !elKey.trim() && (
              <p className="setup-configured-hint">
                <span className="material-symbols-outlined">check_circle</span>
                Already configured — leave blank to keep
              </p>
            )}
          </div>

          {/* Google Cloud TTS key (optional, low-cost default voice) */}
          <div className="setup-field">
            <label className="setup-label" htmlFor="g-key">
              Google Cloud API Key
            </label>
            <div className="setup-input-wrap">
              <input
                autoComplete="off"
                className="setup-input"
                id="g-key"
                placeholder="AIza..."
                type={showG ? "text" : "password"}
                value={gKey}
                onChange={(e) => {
                  setGKey(e.target.value);
                  resetCheck();
                }}
              />
              <button
                className="setup-vis-btn"
                onClick={() => setShowG((v) => !v)}
                type="button"
              >
                <span className="material-symbols-outlined">
                  {showG ? "visibility" : "visibility_off"}
                </span>
              </button>
            </div>
            {authStatus.googleConfigured && authStatus.keyMode !== "managed" && !gKey.trim() && (
              <p className="setup-configured-hint">
                <span className="material-symbols-outlined">check_circle</span>
                Already configured — leave blank to keep
              </p>
            )}
          </div>

          {/* Buttons */}
          <div className="setup-actions">
            <button
              className={`setup-check-btn${checkState === "success" ? " is-success" : ""}${checkState === "error" ? " is-error" : ""}`}
              disabled={isChecking}
              onClick={handleCheck}
              type="button"
            >
              <span className={`material-symbols-outlined${isChecking ? " spin" : ""}`}>
                {icon}
              </span>
              <span>{label}</span>
            </button>

            <button
              className="setup-continue-btn"
              disabled={!canContinue}
              onClick={onContinue}
              type="button"
            >
              <span>Continue to Hub</span>
              <span className="material-symbols-outlined">arrow_forward</span>
            </button>
          </div>
        </form>
          </>
        )}

        {/* Footer */}
        <div className="setup-footer">
          <span className="material-symbols-outlined">lock</span>
          <p className="setup-footer-text">
            Keys are stored locally in your SQLite database.
          </p>
        </div>
      </main>
    </div>
  );
}
