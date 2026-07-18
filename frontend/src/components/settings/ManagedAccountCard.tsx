import { models } from "../../lib/wailsBridge";
import "./ManagedAccountCard.css";

interface Props {
  authStatus: models.AuthStatus;
  saving: boolean;
  // Flip KeyMode to "byok" without signing out — the managed keys stay stored
  // so the user can switch back anytime. Runs through the shell's savePrefs.
  onSwitchToByok: () => void;
  // Device-local sign-out: managed keys + session removed; BYOK keys untouched.
  onSignOut: () => void;
}

// ManagedAccountCard is the Settings → API Keys pane while the app is in
// managed mode: it replaces the per-provider key cards with the signed-in test
// account — identity, the server-pinned model, and the two exits (switch to
// own keys / sign out). The managed keys themselves are never shown: like
// every key, they live only in the Go backend.
export default function ManagedAccountCard({
  authStatus,
  saving,
  onSwitchToByok,
  onSignOut,
}: Props) {
  return (
    <>
      <header className="settings-head">
        <h1>API Keys</h1>
        <p>
          You're on a managed test account — developer-funded keys are installed
          and refresh automatically on launch.
        </p>
      </header>

      <div className="settings-card managed-card">
        <div className="managed-card-head">
          <div className="managed-card-icon">
            <span className="material-symbols-outlined">verified</span>
          </div>
          <div className="managed-card-meta">
            <p className="managed-card-name">Test account</p>
            <p className="managed-card-email">{authStatus.managedEmail}</p>
          </div>
          <span className="managed-card-status">
            <span className="managed-card-dot" />
            Active
          </span>
        </div>

        {authStatus.pinnedModel && (
          <p className="managed-card-model">
            <span className="material-symbols-outlined">lock</span>
            Interview model pinned by the test program:{" "}
            <code>{authStatus.pinnedModel}</code>
          </p>
        )}

        <div className="managed-card-actions">
          <button className="btn btn-ghost" disabled={saving} onClick={onSwitchToByok}>
            Switch to my own keys
          </button>
          <button className="btn btn-danger" disabled={saving} onClick={onSignOut}>
            Sign out
          </button>
        </div>

        <p className="settings-hint managed-card-footnote">
          Switching to your own keys keeps you signed in — switch back anytime
          from this pane. Signing out removes the managed keys from this device;
          keys you pasted yourself are never touched.
        </p>
      </div>
    </>
  );
}
