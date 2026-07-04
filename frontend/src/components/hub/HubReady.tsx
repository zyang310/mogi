import "./HubReady.css";

interface Props {
  onStart: () => void;
  onDefineRegion: () => void;
  onFullScreen: () => void;
  canStart: boolean;
  targetLabel: string;
}

/**
 * Idle Hub — the pre-session "Ready to Begin?" screen.
 * Mirrors the capture preview + large Start Session action from the mockup.
 */
export default function HubReady({
  onStart,
  onDefineRegion,
  onFullScreen,
  canStart,
  targetLabel,
}: Props) {
  return (
    <div className="hub-ready">
      {/* Top section: status + headline + (idle) duration */}
      <div className="hub-top">
        <div className="hub-top-left">
          <p className="hub-status">
            <span className="hub-status-dot" />
            System Ready
          </p>
          <h2 className="hub-headline">Ready to Begin?</h2>
        </div>
        <div className="hub-top-right">
          <span className="hub-duration-label">Session Duration</span>
          <span className="hub-duration-value">00:00</span>
        </div>
      </div>

      {/* Stage: capture preview + action button */}
      <div className="hub-stage">
        <div className="hub-preview">
          {targetLabel && <span className="hub-preview-target">{targetLabel}</span>}

          <div className="hub-preview-empty">
            <span className="material-symbols-outlined">present_to_all</span>
            <span className="hub-preview-empty-text">Awaiting screen capture input</span>
          </div>

          {/* Floating overlay controls (revealed on hover) */}
          <div className="hub-preview-controls">
            <button className="hub-preview-ctrl" onClick={onDefineRegion}>
              <span className="material-symbols-outlined">crop_free</span>
              Define Region
            </button>
            <span className="hub-preview-ctrl-sep" />
            <button className="hub-preview-ctrl" onClick={onFullScreen}>
              <span className="material-symbols-outlined">desktop_windows</span>
              Full Screen
            </button>
          </div>
        </div>

        <button className="hub-start-btn" onClick={onStart} disabled={!canStart}>
          <span className="material-symbols-outlined">play_arrow</span>
          Start Session
          <span className="hub-start-shine" />
        </button>
      </div>

      {!canStart && (
        <p className="hub-start-hint">
          Add your OpenRouter API key in Settings to begin.
        </p>
      )}
    </div>
  );
}
