import { useState } from "react";
import { ActivateTestAccount, RequestTestCode, models } from "../../lib/wailsBridge";
import "./InviteActivation.css";

// Invite redemption steps: enter email + invite code (request), type the
// emailed 6-digit code (verify), terminal success card (done).
type Phase = "request" | "verify" | "done";

interface Props {
  // Receives the fresh AuthStatus returned by ActivateTestAccount once the
  // managed keys are installed; the host decides what to show next.
  onActivated: (status: models.AuthStatus) => void;
  // Optional host navigation away from the flow (e.g. SetupPage's door
  // chooser); the request step renders a Back button only when provided.
  onBack?: () => void;
}

// Shown before any email is sent; the checkbox gates the request. Managed-mode
// interviews are billed to and processed via the developer's provider
// accounts, so consent is explicit (docs/managed-keys-plan.md, Privacy).
const PRIVACY_NOTICE =
  "Interviews run through the developer's API accounts (OpenRouter, Google, " +
  "ElevenLabs). Your screen captures and voice audio are processed by those " +
  "providers; nothing is stored on the test server.";

// InviteActivation is the managed test-account sign-in flow: it trades an
// invite code + email OTP for developer-funded API keys. The keys are
// installed backend-side and never reach this component — only an AuthStatus
// does. Host-agnostic: SetupPage's invite door and Settings' "Have an invite?"
// entry both mount it unchanged.
export default function InviteActivation({ onActivated, onBack }: Props) {
  const [phase, setPhase] = useState<Phase>("request");
  const [email, setEmail] = useState("");
  const [invite, setInvite] = useState("");
  const [otp, setOtp] = useState("");
  const [consented, setConsented] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const canRequest = email.includes("@") && invite.trim() !== "" && consented && !busy;
  const canVerify = otp.length === 6 && !busy;

  // Validate the invite and have the access service email a one-time code.
  // Nothing changes locally until the code is verified.
  async function handleRequest() {
    if (!canRequest) return;
    setBusy(true);
    setError("");
    try {
      await RequestTestCode(email.trim(), invite.trim());
      setOtp("");
      setPhase("verify");
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setBusy(false);
    }
  }

  // Verify the emailed code. On success the backend has already installed the
  // managed keys and switched to managed mode — hand the fresh status up.
  async function handleVerify() {
    if (!canVerify) return;
    setBusy(true);
    setError("");
    try {
      const status = await ActivateTestAccount(email.trim(), otp);
      setPhase("done");
      onActivated(status);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setBusy(false);
    }
  }

  // Back from the verify step: keep the typed email/invite for editing.
  function backToRequest() {
    setOtp("");
    setError("");
    setPhase("request");
  }

  return (
    <div className="invite-activation">
      {phase === "request" && (
        <form
          className="invite-form"
          onSubmit={(e) => {
            e.preventDefault();
            void handleRequest();
          }}
        >
          <div className="invite-field">
            <label className="invite-label" htmlFor="invite-email">
              Email
            </label>
            <input
              autoComplete="off"
              className="invite-input"
              id="invite-email"
              placeholder="you@example.com"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </div>

          <div className="invite-field">
            <label className="invite-label" htmlFor="invite-code">
              Invite code
            </label>
            <input
              autoComplete="off"
              className="invite-input"
              id="invite-code"
              placeholder="MOGI-XXXX"
              type="text"
              value={invite}
              onChange={(e) => setInvite(e.target.value)}
            />
          </div>

          <label className="invite-consent">
            <input
              checked={consented}
              type="checkbox"
              onChange={(e) => setConsented(e.target.checked)}
            />
            <span>{PRIVACY_NOTICE}</span>
          </label>

          {error && <p className="invite-error">{error}</p>}

          <div className="invite-actions">
            {onBack && (
              <button className="btn btn-ghost" onClick={onBack} type="button">
                Back
              </button>
            )}
            <button className="btn btn-primary" disabled={!canRequest} type="submit">
              {busy ? "Sending…" : "Email me a code"}
            </button>
          </div>
        </form>
      )}

      {phase === "verify" && (
        <form
          className="invite-form"
          onSubmit={(e) => {
            e.preventDefault();
            void handleVerify();
          }}
        >
          <p className="invite-hint">
            We emailed a 6-digit code to <strong>{email.trim()}</strong>. Enter it
            within 10 minutes.
          </p>

          <div className="invite-field">
            <label className="invite-label" htmlFor="invite-otp">
              Verification code
            </label>
            <input
              autoComplete="one-time-code"
              className="invite-input invite-input--otp"
              id="invite-otp"
              inputMode="numeric"
              placeholder="••••••"
              type="text"
              value={otp}
              onChange={(e) => setOtp(e.target.value.replace(/\D/g, "").slice(0, 6))}
            />
          </div>

          {error && <p className="invite-error">{error}</p>}

          <div className="invite-actions">
            <button className="btn btn-ghost" onClick={backToRequest} type="button">
              Back
            </button>
            <button className="btn btn-primary" disabled={!canVerify} type="submit">
              {busy ? "Activating…" : "Activate"}
            </button>
          </div>
        </form>
      )}

      {phase === "done" && (
        <div className="invite-done">
          <span className="material-symbols-outlined invite-done-icon">
            check_circle
          </span>
          <p className="invite-done-title">Test account activated</p>
          <p className="invite-done-sub">
            Signed in as {email.trim()} — the managed keys are installed and ready.
          </p>
        </div>
      )}
    </div>
  );
}
