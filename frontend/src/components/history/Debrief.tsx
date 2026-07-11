import type { CSSProperties } from "react";
import { models } from "../../lib/wailsBridge";
import RadarChart from "./RadarChart";
import { verdictTone, toneColorVar, scoreOf } from "../../lib/verdict";
import "./Debrief.css";

interface Props {
  debrief?: models.Debrief;
  loading: boolean;
  error?: string;
}

// RUBRIC pairs each rubric field with its full bar label and a short radar label.
const RUBRIC: { key: keyof models.DebriefRubric; label: string; short: string }[] = [
  { key: "problemSolving", label: "Problem solving", short: "Logic" },
  { key: "coding", label: "Code quality", short: "Quality" },
  { key: "communication", label: "Communication", short: "Comm." },
  { key: "complexity", label: "Complexity analysis", short: "Complexity" },
  { key: "pace", label: "Pace", short: "Pace" },
];

// MetricRow renders one 1-5 dimension as a continuous fill bar (0 shows an em
// dash instead of an empty bar — no evidence, not a zero score).
function MetricRow({ label, score }: { label: string; score: number }) {
  const pct = (Math.max(0, Math.min(5, score)) / 5) * 100;
  return (
    <div className="debrief-metric">
      <div className="debrief-metric-top">
        <span className="debrief-metric-label">{label}</span>
        <span className="debrief-metric-score">{score > 0 ? `${score}/5` : "—"}</span>
      </div>
      <div className="debrief-bar-track">
        <span className="debrief-bar-fill" style={{ width: `${pct}%` }} />
      </div>
    </div>
  );
}

// Debrief renders the AI's post-interview scorecard: a hero (score ring +
// verdict + summary) up top, performance metrics and the radar chart side by
// side, then strengths/improvements. Purely presentational — the parent owns
// fetching, loading, and error state. Sets --tone-color from the verdict, which
// the ring, pill, metric bars, radar, and the parent's active tab underline all
// read (see SessionHistoryCard).
export default function Debrief({ debrief, loading, error }: Props) {
  if (loading) {
    return <p className="debrief-status">Generating debrief…</p>;
  }
  if (error) {
    return <p className="debrief-status error">{error}</p>;
  }
  if (!debrief) {
    return <p className="debrief-status">No debrief available.</p>;
  }

  const tone = verdictTone(debrief.verdict);
  const score = scoreOf(debrief.rubric);
  const pct = (Math.max(0, Math.min(5, score)) / 5) * 100;
  const hasStrengths = (debrief.strengths?.length ?? 0) > 0;
  const hasImprovements = (debrief.improvements?.length ?? 0) > 0;

  const radarAxes = RUBRIC.map((r) => ({
    label: r.short,
    score: debrief.rubric?.[r.key] ?? 0,
  }));

  return (
    <div className="debrief" style={{ "--tone-color": toneColorVar(tone) } as CSSProperties}>
      {/* Hero: score ring + verdict + summary */}
      <div className="debrief-hero">
        <div
          className="debrief-ring"
          style={{
            background: `conic-gradient(var(--tone-color) 0% ${pct}%, var(--outline-variant) ${pct}% 100%)`,
          }}
        >
          <div className="debrief-ring-inner">
            <span className="debrief-ring-score">{score.toFixed(1)}</span>
            <span className="debrief-ring-label">out of 5</span>
          </div>
        </div>
        <div className="debrief-hero-text">
          {debrief.verdict && <span className="debrief-verdict">{debrief.verdict}</span>}
          {debrief.summary && <p className="debrief-summary">{debrief.summary}</p>}
        </div>
      </div>

      <span className="debrief-divider" />

      {/* Metrics + radar */}
      <div className="debrief-split-row">
        <div className="debrief-metrics">
          <h3 className="debrief-section-head">Performance metrics</h3>
          <div className="debrief-metrics-list">
            {RUBRIC.map((r) => (
              <MetricRow key={r.key} label={r.label} score={debrief.rubric?.[r.key] ?? 0} />
            ))}
          </div>
        </div>
        <span className="debrief-vdivider" />
        <div className="debrief-radar-panel">
          <RadarChart axes={radarAxes} />
        </div>
      </div>

      {(hasStrengths || hasImprovements) && (
        <>
          <span className="debrief-divider" />
          <div className="debrief-split-row">
            {hasStrengths && (
              <div className="debrief-list">
                <h3 className="debrief-list-head positive">
                  <span className="material-symbols-outlined">check_circle</span>
                  <span>Strengths</span>
                </h3>
                <ul>
                  {debrief.strengths.map((s, i) => (
                    <li key={i} className="debrief-item positive">
                      <span className="material-symbols-outlined">check</span>
                      <span>{s}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
            {hasStrengths && hasImprovements && <span className="debrief-vdivider" />}
            {hasImprovements && (
              <div className="debrief-list">
                <h3 className="debrief-list-head improve">
                  <span className="material-symbols-outlined">trending_up</span>
                  <span>To improve</span>
                </h3>
                <ul>
                  {debrief.improvements.map((s, i) => (
                    <li key={i} className="debrief-item improve">
                      <span className="material-symbols-outlined">arrow_forward</span>
                      <span>{s}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </div>
        </>
      )}

      <p className="debrief-footnote">
        Based on the interview transcript and your captured final code — not a live
        re-run of your solution.
      </p>
    </div>
  );
}
