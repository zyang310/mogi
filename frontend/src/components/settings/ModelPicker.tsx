import { useEffect, useMemo, useState } from "react";
import { ListAvailableModels, models } from "../../lib/wailsBridge";
import "./ModelPicker.css";

interface Props {
  currentModelId: string;
  onSelect: (modelId: string) => void; // parent persists via UpdatePreferences
}

// ModelPicker is a searchable list of OpenRouter models for Settings. It fetches
// the catalog on mount and filters client-side (search + vision/free toggles).
// Selecting a row reports the id upward — persistence is the parent's job. The
// app is screen-driven, so "Vision only" defaults on.
export default function ModelPicker({ currentModelId, onSelect }: Props) {
  const [allModels, setAllModels] = useState<models.Model[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [search, setSearch] = useState("");
  const [visionOnly, setVisionOnly] = useState(true);
  const [freeOnly, setFreeOnly] = useState(false);

  // Fetch the catalog once. Wails calls no-op in a plain browser, so guard with
  // try/catch and surface failures inline.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const list = await ListAvailableModels();
        if (!cancelled) setAllModels(list ?? []);
      } catch (e: any) {
        if (!cancelled) setError(e?.message || String(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Filter, then sort: pinned current selection first, then free models, then by
  // name. Recomputed only when inputs change.
  const visible = useMemo(() => {
    const q = search.trim().toLowerCase();
    const filtered = allModels.filter((m) => {
      if (visionOnly && !m.supportsVision) return false;
      if (freeOnly && !m.isFree) return false;
      if (q && !m.name.toLowerCase().includes(q) && !m.id.toLowerCase().includes(q)) {
        return false;
      }
      return true;
    });
    return filtered.sort((a, b) => {
      if (a.id === currentModelId) return -1;
      if (b.id === currentModelId) return 1;
      if (a.isFree !== b.isFree) return a.isFree ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
  }, [allModels, search, visionOnly, freeOnly, currentModelId]);

  return (
    <div className="model-picker">
      <input
        type="text"
        className="settings-input model-picker-search"
        placeholder="Search models…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        disabled={loading || !!error}
      />
      <div className="model-picker-filters">
        <label className="model-picker-toggle">
          <input
            type="checkbox"
            checked={visionOnly}
            onChange={(e) => setVisionOnly(e.target.checked)}
          />
          Vision only
        </label>
        <label className="model-picker-toggle">
          <input
            type="checkbox"
            checked={freeOnly}
            onChange={(e) => setFreeOnly(e.target.checked)}
          />
          Free only
        </label>
      </div>

      {loading && <p className="model-picker-status">Loading models…</p>}
      {error && <p className="settings-error">{error}</p>}
      {!loading && !error && visible.length === 0 && (
        <p className="model-picker-status">No models match your filters.</p>
      )}

      {!loading && !error && visible.length > 0 && (
        <ul className="model-picker-list">
          {visible.map((m) => {
            const active = m.id === currentModelId;
            const ctxLabel = m.contextLength > 0 ? `${formatContext(m.contextLength)} ctx` : "";
            const price = m.isFree
              ? ""
              : `$${fmtPrice(m.promptPrice)} / $${fmtPrice(m.completionPrice)} per 1M`;
            const sub = [ctxLabel, price].filter(Boolean).join("  ·  ");
            return (
              <li key={m.id}>
                <button
                  type="button"
                  className={`model-picker-row${active ? " is-active" : ""}`}
                  onClick={() => onSelect(m.id)}
                  title={m.description || m.id}
                >
                  <div className="model-picker-row-top">
                    <span className="model-picker-name">{m.name || m.id}</span>
                    <div className="model-picker-badges">
                      {m.isFree && <span className="model-badge model-badge-free">🟢 Free</span>}
                      {m.supportsVision && (
                        <span className="model-badge model-badge-vision">👁 Vision</span>
                      )}
                    </div>
                  </div>
                  <span className="model-picker-id">{m.id}</span>
                  {sub && <div className="model-picker-sub">{sub}</div>}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}

// formatContext renders a context length compactly, e.g. 128000 -> "128K".
function formatContext(n: number): string {
  if (n >= 1000) return `${Math.round(n / 1000)}K`;
  return String(n);
}

// fmtPrice formats a USD-per-1M-token price with enough precision for the wide
// range OpenRouter reports (sub-cent to tens of dollars).
function fmtPrice(n: number): string {
  if (n === 0) return "0";
  if (n < 0.01) return n.toFixed(4);
  if (n < 1) return n.toFixed(3);
  return n.toFixed(2);
}
