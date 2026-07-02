import { useEffect, useMemo, useRef, useState } from "react";
import {
  ListCompanies,
  ListCompanyProblems,
  StartCompanySession,
  StartMockInterview,
  OpenURL,
  models,
} from "../lib/wailsBridge";
import "./CompanyPractice.css";

type Difficulty = "All" | "Easy" | "Medium" | "Hard";
type SortKey = "Frequency" | "Title" | "Difficulty";

const DIFFICULTIES: Difficulty[] = ["All", "Easy", "Medium", "Hard"];

// Problems shown per page in the company view. Pools can be hundreds long (Google
// has 700+), so the filtered/sorted list is paginated.
const PAGE_SIZE = 25;

interface Props {
  // Called when a company/mock session has started; the parent (App) switches to
  // the active-session UI. The backend has already created the session.
  onStarted: (start: models.CompanySessionStart) => void;
  // The last company slug + difficulty filter the user left the tab on, restored
  // on mount so the tab resumes where they were.
  initialCompany?: string;
  initialDifficulty?: string;
  // Persist the current company slug + difficulty (empty slug clears it). The
  // parent (App) merges these into Preferences.
  onRemember?: (slug: string, difficulty: string) => void;
}

// Rank used to sort by difficulty (Easy first) — matches the backend's tiers.
const DIFF_RANK: Record<string, number> = { Easy: 0, Medium: 1, Hard: 2 };

// normalizeDifficulty coerces a stored value to a valid filter, defaulting to All.
function normalizeDifficulty(d?: string): Difficulty {
  return (DIFFICULTIES as string[]).includes(d ?? "") ? (d as Difficulty) : "All";
}

// CompanyPractice is the Company Practice tab. It has two views: a searchable
// company list, and — once a company is picked — that company's problem pool with
// difficulty/sort controls plus a Mock Interview button. Browse filtering is
// entirely client-side over ListCompanyProblems; only the mock draw is server-side
// (StartMockInterview), so no picker ever sees the mock questions before they're
// assigned.
export default function CompanyPractice({
  onStarted,
  initialCompany,
  initialDifficulty,
  onRemember,
}: Props) {
  const [companies, setCompanies] = useState<models.CompanyInfo[]>([]);
  const [loadingCompanies, setLoadingCompanies] = useState(true);
  const [companiesError, setCompaniesError] = useState("");
  const [companySearch, setCompanySearch] = useState("");

  const [selected, setSelected] = useState<models.CompanyInfo | null>(null);
  const [problems, setProblems] = useState<models.Problem[]>([]);
  const [loadingProblems, setLoadingProblems] = useState(false);
  const [problemsError, setProblemsError] = useState("");

  const [difficulty, setDifficulty] = useState<Difficulty>("All");
  const [sortKey, setSortKey] = useState<SortKey>("Frequency");
  const [page, setPage] = useState(1);
  // Top of the results area, scrolled into view on page change so each page starts
  // from the top rather than wherever the pager sat when clicked.
  const listTopRef = useRef<HTMLDivElement>(null);

  const [starting, setStarting] = useState(false);
  const [startError, setStartError] = useState("");
  const [mockConfirm, setMockConfirm] = useState(false);

  // Fetch the (static) company list once. Wails no-ops in a plain browser, so
  // guard with try/catch and surface failures inline.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const list = await ListCompanies();
        if (!cancelled) setCompanies(list ?? []);
      } catch (e: any) {
        if (!cancelled) setCompaniesError(e?.message || String(e));
      } finally {
        if (!cancelled) setLoadingCompanies(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // loadProblems fetches a company's pool and shows its detail view at the given
  // difficulty. The low-level move, shared by user clicks and the mount restore.
  async function loadProblems(c: models.CompanyInfo, diff: Difficulty) {
    setSelected(c);
    setDifficulty(diff);
    setSortKey("Frequency");
    setProblems([]);
    setProblemsError("");
    setStartError("");
    setLoadingProblems(true);
    try {
      const list = await ListCompanyProblems(c.slug);
      setProblems(list ?? []);
    } catch (e: any) {
      setProblemsError(e?.message || String(e));
    } finally {
      setLoadingProblems(false);
    }
  }

  function openCompany(c: models.CompanyInfo) {
    loadProblems(c, "All");
    onRemember?.(c.slug, "All");
  }

  function changeDifficulty(d: Difficulty) {
    setDifficulty(d);
    if (selected) onRemember?.(selected.slug, d);
  }

  function backToList() {
    setSelected(null);
    setProblems([]);
    setProblemsError("");
    setStartError("");
    setMockConfirm(false);
    onRemember?.("", difficulty); // leaving the company clears the resume target
  }

  // Restore the last company + difficulty once the company list is available.
  const restoredRef = useRef(false);
  useEffect(() => {
    if (restoredRef.current || loadingCompanies || companies.length === 0) return;
    restoredRef.current = true;
    if (initialCompany) {
      const c = companies.find((x) => x.slug === initialCompany);
      if (c) loadProblems(c, normalizeDifficulty(initialDifficulty));
    }
    // Only runs once, when the list first loads.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [loadingCompanies, companies]);

  const visibleCompanies = useMemo(() => {
    const q = companySearch.trim().toLowerCase();
    if (!q) return companies;
    return companies.filter(
      (c) => c.name.toLowerCase().includes(q) || c.slug.toLowerCase().includes(q)
    );
  }, [companies, companySearch]);

  const visibleProblems = useMemo(() => {
    let list = problems;
    if (difficulty !== "All") list = list.filter((p) => p.difficulty === difficulty);
    const sorted = [...list];
    if (sortKey === "Title") {
      sorted.sort((a, b) => a.title.localeCompare(b.title));
    } else if (sortKey === "Difficulty") {
      sorted.sort(
        (a, b) =>
          (DIFF_RANK[a.difficulty] ?? 1) - (DIFF_RANK[b.difficulty] ?? 1) ||
          b.frequency - a.frequency
      );
    } else {
      sorted.sort((a, b) => b.frequency - a.frequency);
    }
    return sorted;
  }, [problems, difficulty, sortKey]);

  // Reset to the first page whenever the company, filter, or sort changes so the
  // pager never points past the (possibly shorter) new list.
  useEffect(() => {
    setPage(1);
  }, [selected?.slug, difficulty, sortKey]);

  // Pagination over the filtered/sorted list.
  const pageCount = Math.max(1, Math.ceil(visibleProblems.length / PAGE_SIZE));
  const currentPage = Math.min(page, pageCount);
  const startIdx = (currentPage - 1) * PAGE_SIZE;
  const pagedProblems = visibleProblems.slice(startIdx, startIdx + PAGE_SIZE);

  function goToPage(p: number) {
    setPage(Math.min(pageCount, Math.max(1, p)));
    listTopRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
  }

  async function startSingle(p: models.Problem) {
    if (!selected || starting) return;
    setStarting(true);
    setStartError("");
    try {
      const start = await StartCompanySession(selected.slug, p);
      onStarted(start);
    } catch (e: any) {
      setStartError(e?.message || String(e));
    } finally {
      setStarting(false);
    }
  }

  async function startMock() {
    if (!selected || starting) return;
    setStarting(true);
    setStartError("");
    try {
      const start = await StartMockInterview(selected.slug);
      setMockConfirm(false);
      onStarted(start);
    } catch (e: any) {
      setStartError(e?.message || String(e));
      setMockConfirm(false);
    } finally {
      setStarting(false);
    }
  }

  async function openLeet(url: string) {
    try {
      await OpenURL(url);
    } catch (e: any) {
      setStartError(e?.message || String(e));
    }
  }

  return (
    <div className="company-page">
      <div className="company-inner">
        {!selected ? (
          <>
            <header className="company-head">
              <h1>Company Practice</h1>
              <p>
                Practice for a specific company — pick a real interview-frequency
                problem, or run a mock interview.
              </p>
            </header>

            <input
              type="text"
              className="settings-input company-search"
              placeholder="Search companies…"
              value={companySearch}
              onChange={(e) => setCompanySearch(e.target.value)}
              disabled={loadingCompanies || !!companiesError}
            />

            {loadingCompanies ? (
              <p className="company-status">Loading companies…</p>
            ) : companiesError ? (
              <p className="company-status error">{companiesError}</p>
            ) : visibleCompanies.length === 0 ? (
              <p className="company-status">No companies match your search.</p>
            ) : (
              <ul className="company-list">
                {visibleCompanies.map((c) => (
                  <li key={c.slug}>
                    <button className="company-row" onClick={() => openCompany(c)}>
                      <span className="company-row-name">{c.name}</span>
                      <span className="company-row-count">
                        {c.problemCount} {c.problemCount === 1 ? "problem" : "problems"}
                      </span>
                      <span className="material-symbols-outlined company-row-arrow">
                        chevron_right
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </>
        ) : (
          <>
            <button className="company-back" onClick={backToList}>
              <span className="material-symbols-outlined">arrow_back</span>
              All companies
            </button>

            <header className="company-head">
              <h1>{selected.name}</h1>
              <p>
                {selected.problemCount}{" "}
                {selected.problemCount === 1 ? "problem" : "problems"} in the pool.
                Pick one below, or run a mock interview.
              </p>
            </header>

            {/* Mock Interview CTA */}
            <div className="company-mock">
              <button
                className="btn btn-primary btn-icon company-mock-btn"
                disabled={!selected.mockEligible || starting}
                onClick={() => setMockConfirm(true)}
              >
                <span className="material-symbols-outlined">bolt</span>
                Start Mock Interview
              </button>
              <p className="company-mock-sub">
                {selected.mockEligible
                  ? `Two questions, drawn from what ${selected.name} actually asks.`
                  : "Not enough data for a mock interview — pick a problem from the list below."}
              </p>
            </div>

            {startError && <p className="company-status error">{startError}</p>}

            {/* Browse controls: difficulty chips + sort */}
            <div className="company-controls" ref={listTopRef}>
              <div className="company-chips">
                {DIFFICULTIES.map((d) => (
                  <button
                    key={d}
                    className={`company-chip${difficulty === d ? " active" : ""}`}
                    onClick={() => changeDifficulty(d)}
                  >
                    {d}
                  </button>
                ))}
              </div>
              <label className="company-sort">
                Sort
                <select
                  value={sortKey}
                  onChange={(e) => setSortKey(e.target.value as SortKey)}
                >
                  <option value="Frequency">Frequency</option>
                  <option value="Title">Title</option>
                  <option value="Difficulty">Difficulty</option>
                </select>
              </label>
            </div>

            {/* Problem list */}
            {loadingProblems ? (
              <p className="company-status">Loading problems…</p>
            ) : problemsError ? (
              <p className="company-status error">{problemsError}</p>
            ) : visibleProblems.length === 0 ? (
              <p className="company-status">No problems match this filter.</p>
            ) : (
              <>
              <ul className="problem-list">
                {pagedProblems.map((p) => (
                  <li key={p.url} className="problem-row">
                    <div className="problem-main">
                      <div className="problem-title-row">
                        <span className="problem-title">
                          {p.id}. {p.title}
                        </span>
                        <span className={`diff-badge ${p.difficulty.toLowerCase()}`}>
                          {p.difficulty}
                        </span>
                        {p.recent && <span className="recent-chip">Recent</span>}
                      </div>
                      <div className="problem-meta">
                        <span className="material-symbols-outlined">trending_up</span>
                        {Math.round(p.frequency)}% frequency
                      </div>
                    </div>
                    <div className="problem-actions">
                      <button
                        className="company-icon-btn"
                        title="Open on LeetCode"
                        onClick={() => openLeet(p.url)}
                      >
                        <span className="material-symbols-outlined">open_in_new</span>
                      </button>
                      <button
                        className="btn btn-primary problem-start"
                        disabled={starting}
                        onClick={() => startSingle(p)}
                      >
                        Start interview
                      </button>
                    </div>
                  </li>
                ))}
              </ul>

              {pageCount > 1 && (
                <div className="company-pagination">
                  <button
                    className="btn btn-ghost btn-icon"
                    disabled={currentPage <= 1}
                    onClick={() => goToPage(currentPage - 1)}
                  >
                    <span className="material-symbols-outlined">chevron_left</span>
                    Prev
                  </button>
                  <span className="company-page-label">
                    {startIdx + 1}–{startIdx + pagedProblems.length} of{" "}
                    {visibleProblems.length}
                  </span>
                  <button
                    className="btn btn-ghost btn-icon"
                    disabled={currentPage >= pageCount}
                    onClick={() => goToPage(currentPage + 1)}
                  >
                    Next
                    <span className="material-symbols-outlined">chevron_right</span>
                  </button>
                </div>
              )}
              </>
            )}
          </>
        )}
      </div>

      {/* Mock confirm modal — never shows the titles; the draw happens on start. */}
      {mockConfirm && selected && (
        <div
          className="company-modal-overlay"
          onClick={() => !starting && setMockConfirm(false)}
        >
          <div className="company-modal" onClick={(e) => e.stopPropagation()}>
            <h2>Mock Interview · {selected.name}</h2>
            <p className="company-modal-lead">
              Two questions — an easier one first, then a harder one.
            </p>
            <ul className="company-modal-points">
              <li>
                <span className="material-symbols-outlined">schedule</span>~45 minutes
                suggested
              </li>
              <li>
                <span className="material-symbols-outlined">visibility_off</span>
                Questions are revealed one at a time — you won't see them up front.
              </li>
              <li>
                <span className="material-symbols-outlined">forum</span>You can always
                ask to move on, just like a real interview.
              </li>
            </ul>
            <div className="company-modal-actions">
              <button
                className="btn btn-ghost"
                onClick={() => setMockConfirm(false)}
                disabled={starting}
              >
                Cancel
              </button>
              <button className="btn btn-primary" onClick={startMock} disabled={starting}>
                {starting ? "Starting…" : "Begin Mock Interview"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
