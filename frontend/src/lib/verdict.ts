// Shared verdict/score semantics for the history timeline and debrief scorecard,
// so both color the same way from a single source of truth.
import { models } from "./wailsBridge";

export type VerdictTone = "positive" | "lean" | "negative" | "neutral";

// verdictTone maps a hire-scale verdict to a coarse tone: green (positive) / blue
// (borderline) / red (negative) / neutral (unknown or not yet reached).
export function verdictTone(verdict: string): VerdictTone {
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

// toneColorVar resolves a tone to the MD3 token that renders it, as a CSS value
// (itself a var() reference) so callers can assign it straight to an inline
// custom property, e.g. style={{ "--tone-color": toneColorVar(tone) }}.
export function toneColorVar(tone: VerdictTone): string {
  switch (tone) {
    case "positive":
      return "var(--secondary)";
    case "lean":
      return "var(--primary)";
    case "negative":
      return "var(--error)";
    default:
      return "var(--outline-variant)";
  }
}

// scoreOf averages a debrief's five 1-5 rubric dimensions into the single
// headline number shown on the timeline and the debrief's score ring, rounded to
// one decimal. Dimensions the model couldn't assess score 0 and still count
// toward the average (mirrors how the rubric bars render an unfilled "—").
export function scoreOf(rubric?: models.DebriefRubric): number {
  if (!rubric) return 0;
  const { problemSolving, coding, communication, complexity, pace } = rubric;
  const sum = problemSolving + coding + communication + complexity + pace;
  return Math.round((sum / 5) * 10) / 10;
}
