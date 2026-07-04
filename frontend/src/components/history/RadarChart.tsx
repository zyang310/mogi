import "./RadarChart.css";

interface Axis {
  label: string;
  score: number;
}

interface Props {
  axes: Axis[];
  max?: number; // full-scale score (default 5)
}

const CX = 50;
const CY = 50;
const R = 37; // radius of the outer ring, in viewBox units
const LABEL_R = 47; // radius at which axis labels sit
const RINGS = [0.25, 0.5, 0.75, 1]; // concentric grid-ring fractions

// angleOf returns the radians for axis i, starting at the top and going clockwise.
function angleOf(i: number, n: number): number {
  return (-90 + (i * 360) / n) * (Math.PI / 180);
}

// point returns the [x, y] for axis i at a fraction (0..1) of the full radius.
function point(i: number, n: number, frac: number): [number, number] {
  const a = angleOf(i, n);
  return [CX + R * frac * Math.cos(a), CY + R * frac * Math.sin(a)];
}

// ringPoints builds the "x,y x,y …" string for a regular polygon at one fraction.
function ringPoints(n: number, frac: number): string {
  return Array.from({ length: n }, (_, i) => point(i, n, frac).join(",")).join(" ");
}

// RadarChart draws a small SVG radar/spider chart — one axis per entry, each
// plotted at score/max of the full radius (a 0 score collapses that axis to the
// centre). Purely presentational; colours come from MD3 tokens. Reusable for any
// set of labelled 0..max scores.
export default function RadarChart({ axes, max = 5 }: Props) {
  const n = axes.length;
  const frac = (score: number) => Math.max(0, Math.min(max, score)) / max;
  const dataPoints = axes
    .map((a, i) => point(i, n, frac(a.score)).join(","))
    .join(" ");

  return (
    <svg
      className="radar"
      viewBox="-32 -18 164 136"
      role="img"
      aria-label="Performance radar chart"
    >
      {RINGS.map((f) => (
        <polygon key={f} className="radar-ring" points={ringPoints(n, f)} />
      ))}
      {axes.map((_, i) => {
        const [x, y] = point(i, n, 1);
        return <line key={i} className="radar-spoke" x1={CX} y1={CY} x2={x} y2={y} />;
      })}

      <polygon className="radar-shape" points={dataPoints} />
      {axes.map((a, i) => {
        const [x, y] = point(i, n, frac(a.score));
        return <circle key={i} className="radar-dot" cx={x} cy={y} r={1.6} />;
      })}

      {axes.map((a, i) => {
        const aRad = angleOf(i, n);
        const lx = CX + LABEL_R * Math.cos(aRad);
        const ly = CY + LABEL_R * Math.sin(aRad);
        const anchor = lx < CX - 1 ? "end" : lx > CX + 1 ? "start" : "middle";
        return (
          <text
            key={i}
            className="radar-label"
            x={lx}
            y={ly}
            textAnchor={anchor}
            dominantBaseline="middle"
          >
            {a.label}
          </text>
        );
      })}
    </svg>
  );
}
