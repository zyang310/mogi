import { models } from "../../lib/wailsBridge";
import RadarChart from "./RadarChart";
import "./Debrief.css";

interface Props {
  debrief?: models.Debrief;
  loading: boolean;
  error?: string;
}

// verdictTone maps a hire-scale verdict to a CSS modifier so the chip is tinted
// green (positive) / blue (borderline) / red (negative) / neutral (unknown).
function verdictTone(verdict: string): string {
  switch (verdict) {
    case "Strong Hire":
    case "Hire":
      return "positive";
    case "Lean Hire":
      return "lean";
    case "No Hire":
    case "Strong No Hire":
      return "negative";
    default:
      return "neutral";
  }
}

// RUBRIC pairs each rubric field with its full bar label and a short radar label.
const RUBRIC: { key: keyof models.DebriefRubric; label: string; short: string }[] = [
  { key: "problemSolving", label: "Problem solving", short: "Logic" },
  { key: "coding", label: "Code quality", short: "Quality" },
  { key: "communication", label: "Communication", short: "Comm." },
  { key: "complexity", label: "Complexity analysis", short: "Complexity" },
  { key: "pace", label: "Pace", short: "Pace" },
];

// MetricRow renders one 1-5 dimension as five glow segments (0 shows an em dash).
function MetricRow({ label, score }: { label: string; score: number }) {
  return (
    <div className="debrief-metric">
      <div className="debrief-metric-top">
        <span className="debrief-metric-label">{label}</span>
        <span className="debrief-metric-score">{score > 0 ? `${score}/5` : "—"}</span>
      </div>
      <span className="debrief-bar" aria-hidden="true">
        {[1, 2, 3, 4, 5].map((n) => (
          <span key={n} className={`debrief-seg${score >= n ? " filled" : ""}`} />
        ))}
      </span>
    </div>
  );
}

// Debrief renders the AI's post-interview scorecard: a verdict chip and summary
// with strengths/improvements on the left, and two visuals (a five-dimension
// metric-bar list and a radar chart) on the right. Purely presentational — the
// parent owns fetching, loading, and error state.
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

  const radarAxes = RUBRIC.map((r) => ({
    label: r.short,
    score: debrief.rubric?.[r.key] ?? 0,
  }));

  return (
    <div className="debrief">
      <div className="debrief-body">
        {/* Left: narrative */}
        <div className="debrief-narrative">
          <div className="debrief-head">
            {debrief.verdict && (
              <span className={`debrief-verdict ${verdictTone(debrief.verdict)}`}>
                {debrief.verdict}
              </span>
            )}
            {debrief.summary && <p className="debrief-summary">{debrief.summary}</p>}
          </div>

          {debrief.strengths?.length > 0 && (
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

          {debrief.improvements?.length > 0 && (
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

        {/* Right: visuals */}
        <div className="debrief-visuals">
          <div className="debrief-metrics">
            <h3 className="debrief-section-head">Performance metrics</h3>
            <div className="debrief-metrics-list">
              {RUBRIC.map((r) => (
                <MetricRow
                  key={r.key}
                  label={r.label}
                  score={debrief.rubric?.[r.key] ?? 0}
                />
              ))}
            </div>
          </div>
          <div className="debrief-radar-panel">
            <RadarChart axes={radarAxes} />
          </div>
        </div>
      </div>

      <p className="debrief-footnote">
        Based on the interview transcript and your captured final code — not a live
        re-run of your solution.
      </p>
    </div>
  );
}
