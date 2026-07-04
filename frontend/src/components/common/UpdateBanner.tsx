import { OpenReleasePage, models } from "../../lib/wailsBridge";
import "./UpdateBanner.css";

interface UpdateBannerProps {
  update: models.UpdateInfo;
  onDismiss: () => void;
}

// UpdateBanner notifies the user that a newer app release is available. The app
// is unsigned and cannot self-replace, so Download opens the release (its .zip
// asset, or the release page as a fallback) in the browser for a manual install.
// Rendered only on non-overlay idle screens so it never intrudes on the floating
// interview overlay or an in-progress session.
export default function UpdateBanner({ update, onDismiss }: UpdateBannerProps) {
  function handleDownload() {
    // Prefer the direct .zip asset; fall back to the release page.
    OpenReleasePage(update.downloadUrl || update.releaseUrl).catch(() => {});
  }

  return (
    <div className="update-banner">
      <span className="update-banner-dot" />
      <span className="update-banner-text">
        A new version is available — <strong>{update.latestVersion}</strong>
      </span>
      <div className="update-banner-actions">
        <button className="btn btn-primary btn-icon" onClick={handleDownload}>
          <span className="material-symbols-outlined">download</span>
          Download
        </button>
        <button
          className="update-banner-dismiss"
          onClick={onDismiss}
          aria-label="Dismiss update notice"
          title="Dismiss"
        >
          &times;
        </button>
      </div>
    </div>
  );
}
