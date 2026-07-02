import { useState } from "react";
import { models } from "../lib/wailsBridge";
import MessageBubble from "./MessageBubble";
import Debrief from "./Debrief";
import { formatSessionDate, formatDuration, prettyModel } from "../lib/format";
import "./SessionHistoryCard.css";

interface Props {
  summary: models.SessionSummary;
  expanded: boolean;
  transcript?: models.Message[];
  loadingTranscript: boolean;
  transcriptError?: string;
  debrief?: models.Debrief;
  loadingDebrief: boolean;
  debriefError?: string;
  onToggle: () => void;
  onDebrief: () => void;
  onDelete: () => void;
}

// SessionHistoryCard renders one past session as an expandable row: a collapsed
// summary (title, difficulty, date, duration, model) that opens to a tabbed body
// switching between the full Transcript and the AI Debrief. Delete uses an inline
// two-step confirm so it needs no modal.
export default function SessionHistoryCard({
  summary,
  expanded,
  transcript,
  loadingTranscript,
  transcriptError,
  debrief,
  loadingDebrief,
  debriefError,
  onToggle,
  onDebrief,
  onDelete,
}: Props) {
  const [confirming, setConfirming] = useState(false);
  const [tab, setTab] = useState<"transcript" | "debrief">("transcript");

  const title = summary.problemTitle?.trim() || "Interview session";

  return (
    <div className={`history-card${expanded ? " expanded" : ""}`}>
      <div className="history-card-head" onClick={onToggle}>
        <div className="history-card-main">
          <div className="history-card-title-row">
            <span className="history-card-title">{title}</span>
            {summary.difficulty && (
              <span className="history-badge">{summary.difficulty}</span>
            )}
            {summary.company && (
              <span className="history-company">
                <span className="material-symbols-outlined">domain</span>
                {summary.company}
                {summary.mode === "mock" && " · Mock"}
              </span>
            )}
          </div>
          <div className="history-card-meta">
            <span className="history-meta-item">
              <span className="material-symbols-outlined">calendar_today</span>
              {formatSessionDate(summary.startedAt)}
            </span>
            <span className="history-meta-item">
              <span className="material-symbols-outlined">timer</span>
              {formatDuration(summary.startedAt, summary.endedAt)}
            </span>
            <span className="history-meta-item history-meta-model">
              <span className="material-symbols-outlined">smart_toy</span>
              {prettyModel(summary.model)}
            </span>
          </div>
        </div>

        {/* Stop clicks on the controls from also toggling the card. */}
        <div className="history-card-actions" onClick={(e) => e.stopPropagation()}>
          {confirming ? (
            <div className="history-confirm">
              <span className="history-confirm-label">Delete?</span>
              <button
                className="history-icon-btn danger"
                title="Confirm delete"
                onClick={() => {
                  setConfirming(false);
                  onDelete();
                }}
              >
                <span className="material-symbols-outlined">check</span>
              </button>
              <button
                className="history-icon-btn"
                title="Cancel"
                onClick={() => setConfirming(false)}
              >
                <span className="material-symbols-outlined">close</span>
              </button>
            </div>
          ) : (
            <button
              className="history-icon-btn danger-hover"
              title="Delete session"
              onClick={() => setConfirming(true)}
            >
              <span className="material-symbols-outlined">delete</span>
            </button>
          )}
          <button
            className="history-icon-btn"
            title={expanded ? "Collapse" : "Expand"}
            onClick={onToggle}
          >
            <span className="material-symbols-outlined">
              {expanded ? "expand_less" : "expand_more"}
            </span>
          </button>
        </div>
      </div>

      {expanded && (
        <div className="history-card-body">
          {/* Transcript ⇄ Debrief tabs. Selecting Debrief lazily generates it
              (cached by the backend), so expanding never spends AI tokens. */}
          <div className="history-tabs">
            <button
              className={`history-tab${tab === "transcript" ? " active" : ""}`}
              onClick={() => setTab("transcript")}
            >
              Transcript
            </button>
            <button
              className={`history-tab${tab === "debrief" ? " active" : ""}`}
              onClick={() => {
                setTab("debrief");
                onDebrief();
              }}
            >
              Debrief
            </button>
          </div>

          {tab === "transcript" ? (
            <div className="history-transcript">
              {loadingTranscript ? (
                <p className="history-transcript-status">Loading transcript…</p>
              ) : transcriptError ? (
                <p className="history-transcript-status error">{transcriptError}</p>
              ) : transcript && transcript.length > 0 ? (
                transcript.map((m) => (
                  <MessageBubble
                    key={m.id}
                    role={m.role === "assistant" ? "assistant" : "user"}
                    content={m.content}
                  />
                ))
              ) : (
                <p className="history-transcript-status">No messages in this session.</p>
              )}
            </div>
          ) : (
            <Debrief debrief={debrief} loading={loadingDebrief} error={debriefError} />
          )}
        </div>
      )}
    </div>
  );
}
