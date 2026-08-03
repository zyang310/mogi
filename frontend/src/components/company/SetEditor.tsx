import { useMemo, useState } from "react";
import { SaveQuestionSet, models } from "../../lib/wailsBridge";
import "./SetEditor.css";

// Cap on how many pool matches render in the picker at once — the pool is
// already in memory, so this is purely to keep the DOM small while typing.
const MAX_RESULTS = 50;
// Mirrors the backend's maxSetQuestions cap (the service layer enforces it).
const MAX_QUESTIONS = 200;
// Mirrors the backend's maxSetNameLen cap.
const MAX_NAME_LEN = 80;

interface Props {
  companySlug: string;
  companyName: string;
  // The company's full, already-loaded problem pool (CompanyPractice state) —
  // searching filters in memory and never refetches.
  pool: models.Problem[];
  // The set being edited, or null to create a new one.
  initial: models.QuestionSet | null;
  // Called with the stored set after a successful save; the parent refreshes
  // its list and closes the editor.
  onSaved: (saved: models.QuestionSet) => void;
  onClose: () => void;
}

// SetEditor is the create/edit modal for a custom question set: a name, the
// current selection (rendered from the set's own snapshots — so questions a
// dataset refresh has dropped from the pool stay visible and removable), and a
// searchable add-from-pool list. Validation lives on the backend; its errors
// surface inline.
export default function SetEditor({
  companySlug,
  companyName,
  pool,
  initial,
  onSaved,
  onClose,
}: Props) {
  const [name, setName] = useState(initial?.name ?? "");
  const [selected, setSelected] = useState<models.Problem[]>(
    initial?.questions ?? []
  );
  const [search, setSearch] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");

  // The URL is a question's identity key (matches the backend's dedupe rule).
  const selectedUrls = useMemo(
    () => new Set(selected.map((q) => q.url)),
    [selected]
  );

  // Pool matches for the picker: title substring, minus already-selected.
  const results = useMemo(() => {
    const q = search.trim().toLowerCase();
    const out: models.Problem[] = [];
    for (const p of pool) {
      if (selectedUrls.has(p.url)) continue;
      if (q && !p.title.toLowerCase().includes(q)) continue;
      out.push(p);
      if (out.length >= MAX_RESULTS) break;
    }
    return out;
  }, [pool, search, selectedUrls]);

  const full = selected.length >= MAX_QUESTIONS;
  const canSave = !saving && name.trim() !== "" && selected.length > 0;

  function add(p: models.Problem) {
    if (full || selectedUrls.has(p.url)) return;
    setSelected((prev) => [...prev, p]);
  }

  function remove(url: string) {
    setSelected((prev) => prev.filter((q) => q.url !== url));
  }

  async function save() {
    if (!canSave) return;
    setSaving(true);
    setSaveError("");
    try {
      const saved = await SaveQuestionSet(
        models.QuestionSet.createFrom({
          id: initial?.id ?? "",
          companySlug,
          name,
          questions: selected,
        })
      );
      onSaved(saved);
    } catch (e: any) {
      setSaveError(e?.message || String(e));
      setSaving(false);
    }
  }

  return (
    <div className="company-modal-overlay" onClick={() => !saving && onClose()}>
      <div className="set-editor" onClick={(e) => e.stopPropagation()}>
        <header className="set-editor-head">
          <h2>
            {initial ? "Edit set" : "New set"}
            <span className="set-editor-co"> · {companyName}</span>
          </h2>
          <button
            className="set-editor-close"
            title="Close"
            onClick={onClose}
            disabled={saving}
          >
            <span className="material-symbols-outlined">close</span>
          </button>
        </header>

        <div className="set-editor-body">
          <input
            className="set-editor-name"
            type="text"
            placeholder="Set name — e.g. Arrays drill"
            value={name}
            maxLength={MAX_NAME_LEN}
            onChange={(e) => setName(e.target.value)}
            autoFocus
          />

          <div className="set-editor-label">
            <span className="set-editor-label-text">
              In this set ({selected.length})
            </span>
            <span className="set-editor-label-rule" />
          </div>
          {selected.length === 0 ? (
            <p className="set-editor-empty">
              No questions yet — add some from the pool below.
            </p>
          ) : (
            <ul className="set-editor-list">
              {selected.map((q) => (
                <li key={q.url} className="set-editor-item">
                  <span className="set-editor-item-num">{q.id}</span>
                  <span className="set-editor-item-title">{q.title}</span>
                  <span className={`diff-badge ${q.difficulty.toLowerCase()}`}>
                    {q.difficulty}
                  </span>
                  <span className="set-editor-spacer" />
                  <button
                    className="set-editor-item-btn remove"
                    title="Remove from set"
                    onClick={() => remove(q.url)}
                  >
                    <span className="material-symbols-outlined">
                      do_not_disturb_on
                    </span>
                  </button>
                </li>
              ))}
            </ul>
          )}

          <div className="set-editor-label">
            <span className="set-editor-label-text">Add from the pool</span>
            <span className="set-editor-label-rule" />
          </div>
          <div className="set-editor-search">
            <span className="material-symbols-outlined set-editor-search-icon">
              search
            </span>
            <input
              className="set-editor-search-input"
              type="text"
              placeholder={`Search ${companyName}'s ${pool.length} questions…`}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
          </div>
          {results.length === 0 ? (
            <p className="set-editor-empty">
              {search
                ? "No matching questions — try a different search."
                : "Every pool question is already in the set."}
            </p>
          ) : (
            <ul className="set-editor-list set-editor-pool">
              {results.map((p) => (
                <li key={p.url} className="set-editor-item">
                  <span className="set-editor-item-num">{p.id}</span>
                  <span className="set-editor-item-title">{p.title}</span>
                  <span className={`diff-badge ${p.difficulty.toLowerCase()}`}>
                    {p.difficulty}
                  </span>
                  {p.recent && <span className="recent-chip">Recent</span>}
                  <span className="set-editor-spacer" />
                  <button
                    className="set-editor-item-btn add"
                    title={full ? `A set holds at most ${MAX_QUESTIONS} questions` : "Add to set"}
                    disabled={full}
                    onClick={() => add(p)}
                  >
                    <span className="material-symbols-outlined">add_circle</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        {saveError && <p className="set-editor-error">{saveError}</p>}

        <footer className="set-editor-actions">
          <span className="set-editor-count">
            {selected.length} of {MAX_QUESTIONS}
          </span>
          <button className="btn btn-ghost" onClick={onClose} disabled={saving}>
            Cancel
          </button>
          <button className="btn btn-primary" onClick={save} disabled={!canSave}>
            {saving ? "Saving…" : "Save set"}
          </button>
        </footer>
      </div>
    </div>
  );
}
