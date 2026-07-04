import { useRef, useState } from "react";
import { SetAPIKey, GetAuthStatus, models } from "../../lib/wailsBridge";
import "./SetupPage.css";

interface Props {
  authStatus: models.AuthStatus;
  onAuthChange: (status: models.AuthStatus) => void;
  onContinue: () => void;
}

type CheckState = "idle" | "checking" | "success" | "error";

const CHECK_ICON: Record<CheckState, string> = {
  idle: "sync",
  checking: "sync",
  success: "check_circle",
  error: "error",
};

export default function SetupPage({ authStatus, onAuthChange, onContinue }: Props) {
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

      <main className={`setup-card${checkState === "success" ? " setup-card--success" : ""}`}>
        {/* Header */}
        <header className="setup-header">
          <div className="setup-icon-ring">
            <span
              className="material-symbols-outlined"
              style={{ fontVariationSettings: "'FILL' 1" }}
            >
              tune
            </span>
          </div>
          <h1 className="setup-title">Setup Environment</h1>
          <p className="setup-subtitle">
            Welcome to AI Interviewer. Let's get your environment ready.
          </p>
        </header>

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
            {authStatus.openRouterConfigured && !orKey.trim() && (
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
            {authStatus.elevenLabsConfigured && !elKey.trim() && (
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
            {authStatus.googleConfigured && !gKey.trim() && (
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
