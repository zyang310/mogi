import { useState } from "react";
import { OpenURL, models } from "../../lib/wailsBridge";
import "./CompanyBanner.css";

interface Props {
  start: models.CompanySessionStart;
}

// CompanyBanner sits above the chat during an active Company Practice session. It
// shows the company and the assigned problem: a single problem chip, or — in mock
// mode — the Q1 chip plus a face-down Q2 card the candidate flips only once the
// interviewer brings it up. Purely local UI; revealing sends nothing (the reveal
// is a frontend affordance, since the frontend can't know the exact moment the AI
// transitions to Q2).
export default function CompanyBanner({ start }: Props) {
  const [revealed, setRevealed] = useState(false);

  const isMock = start.problems.length >= 2;
  const q1 = start.problems[0];
  const q2 = isMock ? start.problems[1] : undefined;

  function open(url: string) {
    OpenURL(url).catch(() => {});
  }

  function problemChip(label: string, p: models.Problem) {
    return (
      <div className="prob-chip">
        <span className="prob-chip-label">{label}</span>
        <span className="prob-chip-title">{p.title}</span>
        <span className={`diff-badge ${p.difficulty.toLowerCase()}`}>{p.difficulty}</span>
        <button className="prob-chip-link" title="Open on LeetCode" onClick={() => open(p.url)}>
          <span className="material-symbols-outlined">open_in_new</span>
        </button>
      </div>
    );
  }

  return (
    <div className="company-banner">
      <div className="company-banner-co">
        <span className="material-symbols-outlined">domain</span>
        <span className="company-banner-co-name">{start.company}</span>
        {isMock && <span className="company-banner-tag">Mock Interview</span>}
      </div>

      <div className="company-banner-probs">
        {q1 && problemChip(isMock ? "Question 1" : "Problem", q1)}

        {isMock && q2 && (revealed ? (
          problemChip("Question 2", q2)
        ) : (
          <button
            className="prob-chip prob-chip-hidden"
            onClick={() => setRevealed(true)}
            title="Reveal once your interviewer brings it up"
          >
            <span className="material-symbols-outlined">lock</span>
            <span className="prob-chip-hidden-text">
              Question 2 — revealed when your interviewer brings it up
            </span>
          </button>
        ))}
      </div>
    </div>
  );
}
