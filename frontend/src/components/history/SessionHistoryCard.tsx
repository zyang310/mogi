import { useState, type CSSProperties } from "react";
import { models } from "../../lib/wailsBridge";
import TranscriptMessage from "./TranscriptMessage";
import Debrief from "./Debrief";
import { formatSessionDate, formatDuration, prettyModel } from "../../lib/format";
import { verdictTone, toneColorVar, scoreOf } from "../../lib/verdict";
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

// Transcript turns beyond this count start collapsed behind a "N more
// messages" divider, so a long interview still reads as a quick scan.
const PREVIEW_MESSAGE_COUNT = 4;

// SessionHistoryCard renders one past session as a timeline entry: a dot on the
// shared rail, a collapsed summary row (title, difficulty, and score/verdict —
// "Ended early" for sessions too short to assess, "Not reviewed" when the debrief
// hasn't been generated yet), and an inline expanding panel tabbed between the
// full Transcript and the AI Debrief. Delete uses an inline two-step confirm so
// it needs no modal. The dot, chevron, and score all pick up one shared
// "--tone-color" custom property derived from the cached debrief's verdict, so
// the whole row reads as one color-coded unit.
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
  const [showAllMessages, setShowAllMessages] = useState(false);

  const title = summary.problemTitle?.trim() || "Interview session";
  const difficulty = summary.difficulty?.toLowerCase();
  // Mirrors the backend's own "too short to assess" threshold (History.Debrief),
  // so this reflects exactly when the Debrief tab would otherwise just error.
  const endedEarly = summary.messageCount < 2;
  const tone = debrief ? verdictTone(debrief.verdict) : "neutral";
  const rowStyle = { "--tone-color": toneColorVar(tone) } as CSSProperties;

  const meta = [
    formatSessionDate(summary.startedAt),
    formatDuration(summary.startedAt, summary.endedAt),
    prettyModel(summary.model),
  ]
    .filter(Boolean)
    .join(" · ");

  const visibleMessages =
    showAllMessages || !transcript ? transcript : transcript.slice(0, PREVIEW_MESSAGE_COUNT);
  const hiddenCount = transcript ? transcript.length - PREVIEW_MESSAGE_COUNT : 0;

  return (
    <div className={`history-entry${expanded ? " expanded" : ""}`} style={rowStyle}>
      <span className="history-dot" />

      <div className="history-entry-row" onClick={onToggle}>
        <div className="history-entry-left">
          <span className="material-symbols-outlined history-chevron">
            {expanded ? "expand_more" : "chevron_right"}
          </span>
          <div className="history-entry-text">
            <div className="history-entry-title-row">
              <span className="history-entry-title">{title}</span>
              {difficulty && <span className={`diff-badge ${difficulty}`}>{summary.difficulty}</span>}
              {summary.company && (
                <span className="history-company">
                  <span className="material-symbols-outlined">domain</span>
                  {summary.company}
                  {summary.mode === "mock" && " · Mock"}
                </span>
              )}
            </div>
            {meta && <div className="history-entry-meta">{meta}</div>}
          </div>
        </div>

        <div className="history-entry-right">
          {endedEarly ? (
            <span className="history-ended-early">Ended early</span>
          ) : debrief ? (
            <span className="history-score-group">
              <span className="history-score">{scoreOf(debrief.rubric).toFixed(1)}</span>
              {debrief.verdict && <span className="history-verdict-text">{debrief.verdict}</span>}
            </span>
          ) : (
            <span className="history-not-reviewed">Not reviewed</span>
          )}
          {/* Stop clicks on delete from also toggling the row. */}
          <div className="history-entry-delete" onClick={(e) => e.stopPropagation()}>
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
                <button className="history-icon-btn" title="Cancel" onClick={() => setConfirming(false)}>
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
          </div>
        </div>
      </div>

      {expanded && (
        <div className="history-entry-body">
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
              ) : visibleMessages && visibleMessages.length > 0 ? (
                <>
                  {visibleMessages.map((m) => (
                    <TranscriptMessage
                      key={m.id}
                      role={m.role === "assistant" ? "assistant" : "user"}
                      content={m.content}
                    />
                  ))}
                  {!showAllMessages && hiddenCount > 0 && (
                    <button
                      className="history-transcript-more"
                      onClick={() => setShowAllMessages(true)}
                    >
                      <span className="history-transcript-more-line" />
                      <span>{hiddenCount} more message{hiddenCount === 1 ? "" : "s"}</span>
                      <span className="history-transcript-more-line" />
                    </button>
                  )}
                </>
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
