import { useState } from "react";
import { OpenURL, models } from "../../lib/wailsBridge";
import "./QuestionSetCard.css";

interface Props {
  set: models.QuestionSet;
  // True while any session start is in flight — disables every start button so
  // double-starts are impossible (the backend guard is the backstop).
  starting: boolean;
  expanded: boolean;
  onToggleExpand: () => void;
  // The parent opens the set-mock confirm modal (the draw happens on start).
  onStartMock: () => void;
  // The parent starts a single-question session (reuses StartCompanySession).
  onStartSingle: (p: models.Problem) => void;
  onEdit: () => void;
  onDelete: () => void;
}

// QuestionSetCard is one custom question set in the company detail view: a
// header row (name, count, mock/edit/delete actions) that expands into the
// set's questions, each startable as a single-question session. Delete uses
// the inline two-step confirm (same pattern as SessionHistoryCard) so it needs
// no modal.
export default function QuestionSetCard({
  set,
  starting,
  expanded,
  onToggleExpand,
  onStartMock,
  onStartSingle,
  onEdit,
  onDelete,
}: Props) {
  const [confirming, setConfirming] = useState(false);
  const count = set.questions.length;
  const mockable = count >= 2;

  // Opening the LeetCode page is best-effort; the row has no error surface and
  // OpenURL only no-ops in the stubbed browser preview.
  function openLink(url: string) {
    OpenURL(url).catch(() => {});
  }

  return (
    <div className={`qset-card${expanded ? " expanded" : ""}`}>
      <div className="qset-row" onClick={onToggleExpand}>
        <span className="material-symbols-outlined qset-chevron">
          {expanded ? "expand_more" : "chevron_right"}
        </span>
        <div className="qset-text">
          <span className="qset-name">{set.name}</span>
          <span className="qset-count">
            {count} {count === 1 ? "question" : "questions"}
          </span>
        </div>
        {/* Stop clicks on the actions from also toggling the row. */}
        <div className="qset-actions" onClick={(e) => e.stopPropagation()}>
          {confirming ? (
            <div className="qset-confirm">
              <span className="qset-confirm-label">Delete?</span>
              <button
                className="qset-icon-btn danger"
                title="Confirm delete"
                onClick={() => {
                  setConfirming(false);
                  onDelete();
                }}
              >
                <span className="material-symbols-outlined">check</span>
              </button>
              <button
                className="qset-icon-btn"
                title="Cancel"
                onClick={() => setConfirming(false)}
              >
                <span className="material-symbols-outlined">close</span>
              </button>
            </div>
          ) : (
            <>
              <button
                className="btn btn-primary btn-icon qset-mock-btn"
                disabled={!mockable || starting}
                title={mockable ? undefined : "Add at least 2 questions to run a mock"}
                onClick={onStartMock}
              >
                <span className="material-symbols-outlined">bolt</span>
                Mock
              </button>
              <button className="qset-icon-btn" title="Edit set" onClick={onEdit}>
                <span className="material-symbols-outlined">edit</span>
              </button>
              <button
                className="qset-icon-btn danger-hover"
                title="Delete set"
                onClick={() => setConfirming(true)}
              >
                <span className="material-symbols-outlined">delete</span>
              </button>
            </>
          )}
        </div>
      </div>

      {expanded && (
        <ul className="qset-questions">
          {set.questions.map((q) => (
            <li key={q.url} className="qset-question">
              <span className="qset-q-num">{q.id}</span>
              <span className="qset-q-title">{q.title}</span>
              <span className={`diff-badge ${q.difficulty.toLowerCase()}`}>
                {q.difficulty}
              </span>
              {q.recent && <span className="recent-chip">Recent</span>}
              <span className="qset-q-spacer" />
              <button
                className="qset-icon-btn"
                title="Open on LeetCode"
                onClick={() => openLink(q.url)}
              >
                <span className="material-symbols-outlined">open_in_new</span>
              </button>
              <button
                className="btn btn-primary qset-q-start"
                disabled={starting}
                onClick={() => onStartSingle(q)}
              >
                Start
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
