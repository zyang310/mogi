import { useEffect, useState } from "react";
import { GetLatestScreenshot, models } from "../../lib/wailsBridge";
import "./CapturePanel.css";

interface Props {
  isActive: boolean;
  prefs: models.Preferences | null;
  onSetRegion: () => void;
}

const PREVIEW_INTERVAL_MS = 2000;

export default function CapturePanel({ isActive, prefs, onSetRegion }: Props) {
  const [preview, setPreview] = useState("");

  // While a session is active, periodically refresh the "what the AI sees"
  // preview from the latest captured (cropped) screenshot.
  useEffect(() => {
    if (!isActive) {
      setPreview("");
      return;
    }
    let cancelled = false;
    const fetchLatest = () => {
      GetLatestScreenshot()
        .then((b64) => {
          if (!cancelled && b64) setPreview(`data:image/png;base64,${b64}`);
        })
        .catch(() => {}); // no screenshot yet — ignore
    };
    fetchLatest();
    const id = setInterval(fetchLatest, PREVIEW_INTERVAL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [isActive]);

  const displayNum = prefs ? prefs.captureDisplay + 1 : 1;
  const cropped = prefs ? prefs.regionW > 0 : false;

  return (
    <div className="capture-panel">
      <div className="capture-header">
        <h2 className="capture-title">Screen capture</h2>
        <button className="btn btn-ghost" onClick={onSetRegion}>
          Set region
        </button>
      </div>

      <div className="capture-summary">
        Watching: <span className="capture-summary-value">Display {displayNum}</span>
        <span className="capture-summary-sep">·</span>
        <span className="capture-summary-value">
          {cropped ? "cropped region" : "full display"}
        </span>
      </div>

      <div className="capture-preview">
        {isActive ? (
          preview ? (
            <img className="capture-preview-img" src={preview} alt="what the AI sees" />
          ) : (
            <p className="capture-preview-hint">Capturing…</p>
          )
        ) : (
          <p className="capture-preview-hint">
            Point this region at your IDE, LeetCode, or NeetCode — then press
            Start. The interviewer reads the problem and your code straight from
            the screen.
          </p>
        )}
      </div>

      {isActive && preview && (
        <p className="capture-caption">This is what the interviewer sees.</p>
      )}
    </div>
  );
}
