import { useState } from "react";
import { MinimiseWindow, ToggleMaximiseWindow, QuitApp } from "../../lib/wailsBridge";
import "./WindowControls.css";

// WindowControls draws minimise / maximise / quit buttons for the frameless
// window, which has no native titlebar controls. Rendered in the main (non-
// overlay) chrome; the overlay bar provides its own controls. The Wails calls
// no-op in browser preview.
export default function WindowControls() {
  const [maximized, setMaximized] = useState(false);

  function toggleMaximise() {
    ToggleMaximiseWindow();
    setMaximized((m) => !m);
  }

  return (
    <div className="window-controls">
      <button
        className="window-control"
        onClick={() => MinimiseWindow()}
        title="Minimize"
        aria-label="Minimize"
      >
        <span className="material-symbols-outlined">minimize</span>
      </button>
      <button
        className="window-control"
        onClick={toggleMaximise}
        title={maximized ? "Restore" : "Maximize"}
        aria-label={maximized ? "Restore" : "Maximize"}
      >
        <span className="material-symbols-outlined">
          {maximized ? "fullscreen_exit" : "fullscreen"}
        </span>
      </button>
      <button
        className="window-control window-control-close"
        onClick={() => QuitApp()}
        title="Quit"
        aria-label="Quit"
      >
        <span className="material-symbols-outlined">close</span>
      </button>
    </div>
  );
}
