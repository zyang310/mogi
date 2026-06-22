import { useState } from "react";
import "./Overlay.css";

interface Message {
  role: "user" | "assistant";
  content: string;
}

interface Props {
  messages: Message[];
  latestAiText: string;
  onEnd: () => void;
  onExpand: () => void;
  onHistoryToggle: (open: boolean) => void;
  // Voice state + handlers, owned by App.
  recording: boolean;
  transcribing: boolean;
  speaking: boolean;
  voiceMode: boolean;
  onMicToggle: () => void;
  onToggleVoice: () => void;
}

/**
 * Compact always-on-top overlay bar shown during an interview while the user
 * works in their own IDE. The mic drives click-to-toggle recording, the speaker
 * toggles voice mode, and the "Live" indicator + under-glow reflect the real
 * voice state (recording / transcribing / the interviewer speaking).
 */
export default function Overlay({
  messages,
  latestAiText,
  onEnd,
  onExpand,
  onHistoryToggle,
  recording,
  transcribing,
  speaking,
  voiceMode,
  onMicToggle,
  onToggleVoice,
}: Props) {
  const [historyOpen, setHistoryOpen] = useState(false);

  function toggleHistory() {
    const next = !historyOpen;
    setHistoryOpen(next);
    onHistoryToggle(next);
  }

  // Live indicator reflects the current voice activity.
  const liveLabel = recording
    ? "Rec"
    : transcribing
      ? "···"
      : speaking
        ? "Speaking"
        : "Live";
  const liveClass = recording ? "is-recording" : speaking ? "is-speaking" : "";

  const micIcon = transcribing ? "hourglass_top" : recording ? "stop" : "mic";

  return (
    <div className="overlay-root">
      <div className="overlay-bar">
        {/* Grab handle (drags the window) */}
        <div className="overlay-grab" title="Drag to move">
          <span className="material-symbols-outlined">drag_indicator</span>
        </div>

        {/* Live indicator */}
        <div className={`overlay-live ${liveClass}`}>
          <span className="overlay-live-dot" />
          <span className="overlay-live-label">{liveLabel}</span>
        </div>

        {/* Real-time transcript (latest interviewer line) */}
        <div className="overlay-transcript">
          <span className="overlay-transcript-speaker">AI:</span>
          <span className="overlay-transcript-text">{latestAiText}</span>
        </div>

        {/* Controls */}
        <div className="overlay-controls">
          <button
            className={`overlay-icon-btn${historyOpen ? " is-active" : ""}`}
            onClick={toggleHistory}
            title="Conversation history"
          >
            <span className="material-symbols-outlined">history</span>
          </button>
          <button
            className={`overlay-icon-btn${voiceMode ? " is-active" : ""}`}
            onClick={onToggleVoice}
            title={voiceMode ? "Voice mode on — replies are spoken" : "Voice mode off"}
          >
            <span className="material-symbols-outlined">
              {voiceMode ? "volume_up" : "volume_off"}
            </span>
          </button>
          <button
            className={`overlay-icon-btn${recording ? " is-recording" : ""}`}
            onClick={onMicToggle}
            disabled={transcribing}
            title={recording ? "Stop and send" : "Speak your message"}
          >
            <span className={`material-symbols-outlined${transcribing ? " spin" : ""}`}>
              {micIcon}
            </span>
          </button>
          <button
            className="overlay-icon-btn"
            onClick={onExpand}
            title="Expand to full window"
          >
            <span className="material-symbols-outlined">open_in_full</span>
          </button>
          <button className="overlay-end-btn" onClick={onEnd}>
            End Session
          </button>
        </div>
      </div>

      {/* Under-glow for AI presence */}
      <div className={`overlay-glow${speaking ? " is-speaking" : ""}`} />

      {/* Conversation history dropdown */}
      {historyOpen && (
        <div className="overlay-history">
          <div className="overlay-history-header">
            <span>Conversation History</span>
            <span className="overlay-live-dot" />
          </div>
          <div className="overlay-history-body">
            {messages.length === 0 ? (
              <p className="overlay-history-empty">No messages yet.</p>
            ) : (
              messages.map((m, i) => (
                <div key={i} className={`overlay-history-row ${m.role}`}>
                  <span className="overlay-history-role">
                    {m.role === "assistant" ? "AI" : "You"}
                  </span>
                  <p className="overlay-history-text">{m.content}</p>
                </div>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
