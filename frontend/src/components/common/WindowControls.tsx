import { useState } from "react";
import { MinimiseWindow, ToggleMaximiseWindow, QuitApp } from "../../lib/wailsBridge";
import "./WindowControls.css";

// WindowControls draws macOS-style "traffic light" buttons (close / minimise /
// maximise, in that native left-to-right order) for the frameless window, which
// has no native titlebar controls. Each dot reveals its glyph on hover, matching
// the native look. Rendered in the main (non-overlay) chrome; the overlay bar
// provides its own controls. The Wails calls no-op in browser preview.
export default function WindowControls() {
  const [maximized, setMaximized] = useState(false);

  function toggleMaximise() {
    ToggleMaximiseWindow();
    setMaximized((m) => !m);
  }

  return (
    <div className="window-controls">
      <button
        className="traffic-light traffic-light-close"
        onClick={() => QuitApp()}
        title="Quit"
        aria-label="Quit"
      >
        <span className="material-symbols-outlined">close</span>
      </button>
      <button
        className="traffic-light traffic-light-min"
        onClick={() => MinimiseWindow()}
        title="Minimize"
        aria-label="Minimize"
      >
        <span className="material-symbols-outlined">remove</span>
      </button>
      <button
        className="traffic-light traffic-light-max"
        onClick={toggleMaximise}
        title={maximized ? "Restore" : "Maximize"}
        aria-label={maximized ? "Restore" : "Maximize"}
      >
        <span className="material-symbols-outlined">
          {maximized ? "close_fullscreen" : "open_in_full"}
        </span>
      </button>
    </div>
  );
}
