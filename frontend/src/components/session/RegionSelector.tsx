import { useEffect, useRef, useState } from "react";
import {
  ListDisplays,
  SnapshotDisplay,
  SetCaptureRegion,
  capture,
} from "../../lib/wailsBridge";
import "./RegionSelector.css";

interface Props {
  initialDisplay: number;
  onClose: () => void;
  onSaved: () => void; // called after the region is persisted
}

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

const MIN_FRACTION = 0.02; // ignore accidental tiny drags

export default function RegionSelector({ initialDisplay, onClose, onSaved }: Props) {
  const [displays, setDisplays] = useState<capture.DisplayInfo[]>([]);
  const [display, setDisplay] = useState(initialDisplay);
  const [snapshot, setSnapshot] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);

  const [sel, setSel] = useState<Rect | null>(null);
  const dragStart = useRef<{ x: number; y: number } | null>(null);
  const imageWrapRef = useRef<HTMLDivElement>(null);

  // Load the list of displays once.
  useEffect(() => {
    ListDisplays()
      .then((d) => {
        setDisplays(d);
        if (d.length > 0 && !d.some((x) => x.index === initialDisplay)) {
          setDisplay(d[0].index);
        }
      })
      .catch((e) => setError(e?.message || String(e)));
  }, [initialDisplay]);

  // (Re)load the snapshot whenever the chosen display changes.
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError("");
    setSel(null);
    SnapshotDisplay(display)
      .then((b64) => {
        if (!cancelled) setSnapshot(`data:image/png;base64,${b64}`);
      })
      .catch((e) => {
        if (!cancelled) setError(e?.message || String(e));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [display]);

  function fractionFromEvent(e: React.PointerEvent): { x: number; y: number } {
    const wrap = imageWrapRef.current;
    if (!wrap) return { x: 0, y: 0 };
    const r = wrap.getBoundingClientRect();
    const x = (e.clientX - r.left) / r.width;
    const y = (e.clientY - r.top) / r.height;
    return { x: clamp01(x), y: clamp01(y) };
  }

  function onPointerDown(e: React.PointerEvent) {
    if (loading || !snapshot) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const p = fractionFromEvent(e);
    dragStart.current = p;
    setSel({ x: p.x, y: p.y, w: 0, h: 0 });
  }

  function onPointerMove(e: React.PointerEvent) {
    if (!dragStart.current) return;
    const p = fractionFromEvent(e);
    const s = dragStart.current;
    setSel({
      x: Math.min(s.x, p.x),
      y: Math.min(s.y, p.y),
      w: Math.abs(p.x - s.x),
      h: Math.abs(p.y - s.y),
    });
  }

  function onPointerUp() {
    dragStart.current = null;
    setSel((cur) => {
      if (cur && (cur.w < MIN_FRACTION || cur.h < MIN_FRACTION)) return null;
      return cur;
    });
  }

  async function save(fullDisplay: boolean) {
    setSaving(true);
    setError("");
    try {
      if (fullDisplay) {
        await SetCaptureRegion(display, 0, 0, 0, 0); // w<=0 => full display
      } else if (sel) {
        await SetCaptureRegion(display, sel.x, sel.y, sel.w, sel.h);
      } else {
        setSaving(false);
        return;
      }
      onSaved();
      onClose();
    } catch (e: any) {
      setError(e?.message || String(e));
      setSaving(false);
    }
  }

  return (
    <div className="region-overlay" onClick={onClose}>
      <div className="region-panel" onClick={(e) => e.stopPropagation()}>
        <div className="region-header">
          <h2>Set capture region</h2>
          <button className="region-close" onClick={onClose}>
            &times;
          </button>
        </div>

        <div className="region-toolbar">
          <label className="region-label">
            Display
            <select
              className="region-select"
              value={display}
              onChange={(e) => setDisplay(Number(e.target.value))}
              disabled={saving}
            >
              {displays.map((d) => (
                <option key={d.index} value={d.index}>
                  {d.label}
                </option>
              ))}
            </select>
          </label>
          <p className="region-hint">
            Drag a box over the area to watch (e.g. your IDE or LeetCode).
          </p>
        </div>

        <div
          className="region-canvas"
          ref={imageWrapRef}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
        >
          {loading && <div className="region-status">Capturing screen…</div>}
          {!loading && snapshot && (
            <img className="region-image" src={snapshot} alt="display snapshot" draggable={false} />
          )}
          {sel && (sel.w > 0 || sel.h > 0) && (
            <div
              className="region-box"
              style={{
                left: `${sel.x * 100}%`,
                top: `${sel.y * 100}%`,
                width: `${sel.w * 100}%`,
                height: `${sel.h * 100}%`,
              }}
            />
          )}
        </div>

        {error && <p className="region-error">{error}</p>}

        <div className="region-actions">
          <button
            className="btn btn-ghost"
            onClick={() => save(true)}
            disabled={saving || loading}
          >
            Use full display
          </button>
          <button
            className="btn btn-primary"
            onClick={() => save(false)}
            disabled={saving || loading || !sel}
          >
            {saving ? "Saving…" : "Save region"}
          </button>
        </div>
      </div>
    </div>
  );
}

function clamp01(v: number): number {
  if (v < 0) return 0;
  if (v > 1) return 1;
  return v;
}
