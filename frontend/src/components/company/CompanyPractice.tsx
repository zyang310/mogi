import { useEffect, useMemo, useRef, useState } from "react";
import {
  ListCompanies,
  ListCompanyProblems,
  ListStarredCompanies,
  SetCompanyStarred,
  StartCompanySession,
  StartMockInterview,
  OpenURL,
  models,
} from "../../lib/wailsBridge";
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
  // Suggested time budget for a mock interview, shown in the confirm dialog so it
  // matches the session timer. 0 means untimed (the user disabled the limit).
  mockLimitMinutes: number;
}

// Rank used to sort by difficulty (Easy first) — matches the backend's tiers.
const DIFF_RANK: Record<string, number> = { Easy: 0, Medium: 1, Hard: 2 };

// normalizeDifficulty coerces a stored value to a valid filter, defaulting to All.
function normalizeDifficulty(d?: string): Difficulty {
  return (DIFFICULTIES as string[]).includes(d ?? "") ? (d as Difficulty) : "All";
}

// Number of decorative monogram tints (see .co-tint-* in the CSS). A company's
// tile colour is picked deterministically from its name so it stays stable.
const TINT_COUNT = 6;

// CompanyInfo augmented with the display fields the directory needs.
interface DerivedCompany extends models.CompanyInfo {
  mono: string; // single-letter monogram (first alphanumeric of the name)
  letter: string; // A–Z bucket, or "#" for names starting with a digit
  tint: number; // 0..TINT_COUNT-1 index into the tile-colour palette
  countLabel: string;
}

// deriveCompany computes a company's monogram, its A–Z group letter, a stable
// tint, and a pluralised count label — the fields the redesigned list renders.
function deriveCompany(c: models.CompanyInfo): DerivedCompany {
  const key = c.name.replace(/[^A-Za-z0-9]/g, "");
  const first = (key[0] || "#").toUpperCase();
  const letter = /[0-9]/.test(first) ? "#" : first;
  return {
    ...c,
    mono: first,
    letter,
    tint: (c.name.charCodeAt(0) + c.name.length) % TINT_COUNT,
    countLabel: `${c.problemCount} ${c.problemCount === 1 ? "problem" : "problems"}`,
  };
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
  mockLimitMinutes,
}: Props) {
  const [companies, setCompanies] = useState<models.CompanyInfo[]>([]);
  const [loadingCompanies, setLoadingCompanies] = useState(true);
  const [companiesError, setCompaniesError] = useState("");
  const [companySearch, setCompanySearch] = useState("");

  // Starred company slugs, loaded once on mount. Toggles are optimistic; a
  // failed write reverts its own change and surfaces starError.
  const [starred, setStarred] = useState<Set<string>>(new Set());
  const [starError, setStarError] = useState("");

  const [selected, setSelected] = useState<models.CompanyInfo | null>(null);
  const [problems, setProblems] = useState<models.Problem[]>([]);
  const [loadingProblems, setLoadingProblems] = useState(false);
  const [problemsError, setProblemsError] = useState("");

  const [difficulty, setDifficulty] = useState<Difficulty>("All");
  const [sortKey, setSortKey] = useState<SortKey>("Frequency");
  const [page, setPage] = useState(1);
  const [pageInput, setPageInput] = useState("1");
  // Top of the results area, scrolled into view on page change so each page starts
  // from the top rather than wherever the pager sat when clicked.
  const listTopRef = useRef<HTMLDivElement>(null);

  // The scrolling directory viewport + its per-letter group headers, so the A–Z
  // rail can bring a letter to the top of the viewport (only the viewport
  // scrolls, never the whole page).
  const dirScrollRef = useRef<HTMLDivElement>(null);
  const groupRefs = useRef<Record<string, HTMLDivElement | null>>({});

  function jumpToLetter(letter: string) {
    const scroller = dirScrollRef.current;
    const el = groupRefs.current[letter];
    if (!scroller || !el) return;
    const delta =
      el.getBoundingClientRect().top - scroller.getBoundingClientRect().top;
    scroller.scrollTo({ top: scroller.scrollTop + delta - 12, behavior: "smooth" });
  }

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
    // Starred slugs load in parallel; failure is non-fatal (rows just render
    // unstarred) and must never block or error the company list itself.
    (async () => {
      try {
        const slugs = await ListStarredCompanies();
        if (!cancelled) setStarred(new Set(slugs ?? []));
      } catch {
        // A later toggle surfaces its own error via starError.
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

  // toggleStar optimistically flips a company's star, persists it, and — if the
  // write fails — reverts this toggle's own change (an inverse update, not a
  // snapshot restore, so concurrent toggles on other rows aren't clobbered).
  async function toggleStar(slug: string) {
    const next = !starred.has(slug);
    setStarError("");
    setStarred((prev) => {
      const s = new Set(prev);
      if (next) s.add(slug);
      else s.delete(slug);
      return s;
    });
    try {
      await SetCompanyStarred(slug, next);
    } catch (e: any) {
      setStarred((prev) => {
        const s = new Set(prev);
        if (next) s.delete(slug);
        else s.add(slug);
        return s;
      });
      setStarError(e?.message || String(e));
    }
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

  // Every company augmented with its monogram/letter/tint (see deriveCompany).
  const derivedAll = useMemo(() => companies.map(deriveCompany), [companies]);

  // Search query, matched against name + slug. Empty query shows everything.
  const query = companySearch.trim().toLowerCase();
  const matchesQuery = (c: DerivedCompany) =>
    !query || c.name.toLowerCase().includes(query) || c.slug.toLowerCase().includes(query);
  const filtered = useMemo(
    () => derivedAll.filter(matchesQuery),
    // matchesQuery closes over `query`, so `query` is the real dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [derivedAll, query]
  );

  // The directory: filtered companies bucketed by first letter, buckets sorted
  // A→Z with "#" (numeric names) last, each bucket alphabetised.
  const groups = useMemo(() => {
    const byLetter: Record<string, DerivedCompany[]> = {};
    for (const c of filtered) (byLetter[c.letter] ??= []).push(c);
    return Object.keys(byLetter)
      .sort((a, b) => (a === "#" ? 1 : b === "#" ? -1 : a.localeCompare(b)))
      .map((letter) => ({
        letter,
        items: byLetter[letter].slice().sort((a, b) => a.name.localeCompare(b.name)),
      }));
  }, [filtered]);

  // The full A–Z rail (plus "#" when present): letters with no group render
  // muted and are not clickable.
  const railLetters = useMemo(() => {
    const present = new Set(groups.map((g) => g.letter));
    const rail = "ABCDEFGHIJKLMNOPQRSTUVWXYZ".split("").map((L) => ({
      L,
      present: present.has(L),
    }));
    if (present.has("#")) rail.push({ L: "#", present: true });
    return rail;
  }, [groups]);

  // Starred companies (in the loaded list's order) for the pinned band. Slugs
  // that vanish after a dataset refresh simply never render.
  const starredItems = useMemo(
    () => derivedAll.filter((c) => starred.has(c.slug)),
    [derivedAll, starred]
  );

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

  // Keep the editable page input in sync with the actual page (prev/next clicks,
  // filter resets, clamps all flow through currentPage).
  useEffect(() => {
    setPageInput(String(currentPage));
  }, [currentPage]);

  // Commit a typed page number: normalise the display and jump only on a change.
  function commitPageInput() {
    const n = parseInt(pageInput, 10);
    const target = Number.isNaN(n) ? currentPage : Math.min(pageCount, Math.max(1, n));
    setPageInput(String(target));
    if (target !== currentPage) goToPage(target);
  }

  // The pager, rendered both above (by the filters) and below the list. Null when
  // everything fits on one page. Rendering the same element in two spots is fine —
  // both page inputs bind to the same pageInput state.
  const pager =
    pageCount > 1 ? (
      <div className="company-pagination">
        <button
          className="company-icon-btn"
          disabled={currentPage <= 1}
          onClick={() => goToPage(1)}
          title="First page"
        >
          <span className="material-symbols-outlined">keyboard_double_arrow_left</span>
        </button>
        <button
          className="company-icon-btn"
          disabled={currentPage <= 1}
          onClick={() => goToPage(currentPage - 1)}
          title="Previous page"
        >
          <span className="material-symbols-outlined">chevron_left</span>
        </button>
        <span className="company-page-label">
          Page{" "}
          <input
            className="company-page-input"
            type="text"
            inputMode="numeric"
            value={pageInput}
            onChange={(e) => setPageInput(e.target.value.replace(/[^0-9]/g, ""))}
            onBlur={commitPageInput}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                commitPageInput();
                (e.target as HTMLInputElement).blur();
              }
            }}
            aria-label="Page number"
          />{" "}
          of {pageCount}
        </span>
        <button
          className="company-icon-btn"
          disabled={currentPage >= pageCount}
          onClick={() => goToPage(currentPage + 1)}
          title="Next page"
        >
          <span className="material-symbols-outlined">chevron_right</span>
        </button>
        <button
          className="company-icon-btn"
          disabled={currentPage >= pageCount}
          onClick={() => goToPage(pageCount)}
          title="Last page"
        >
          <span className="material-symbols-outlined">keyboard_double_arrow_right</span>
        </button>
      </div>
    ) : null;

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

// renderStarCard renders one pinned "Starred" band card. The whole card opens
  // the company; the corner badge is clickable to unstar it directly.
  function renderStarCard(c: DerivedCompany) {
    return (
      <div
        key={c.slug}
        className="co-star-card"
        onClick={() => openCompany(c)}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            openCompany(c);
          }
        }}
      >
        <span className="co-tile-wrap">
          <span className={`co-tile co-tint-${c.tint}`}>{c.mono}</span>
          <button
            className="co-star-badge"
            onClick={(e) => {
              e.stopPropagation();
              toggleStar(c.slug);
            }}
            aria-label={`Unstar ${c.name}`}
            title="Unstar"
          >
            <span className="material-symbols-outlined">star</span>
          </button>
        </span>
        <span className="co-star-card-text">
          <span className="co-star-card-name">{c.name}</span>
          <span className="co-card-count">{c.countLabel}</span>
        </span>
      </div>
    );
  }

  // renderDirCard renders one directory card: an open button (tile + name +
  // count) and a sibling star toggle. They are siblings, not nested — a
  // <button> inside a <button> is invalid HTML.
  function renderDirCard(c: DerivedCompany) {
    const isStarred = starred.has(c.slug);
    return (
      <div key={c.slug} className="co-card">
        <button className="co-card-open" onClick={() => openCompany(c)}>
          <span className={`co-tile co-tile-sm co-tint-${c.tint}`}>{c.mono}</span>
          <span className="co-card-text">
            <span className="co-card-name">{c.name}</span>
            <span className="co-card-count">{c.countLabel}</span>
          </span>
        </button>
        <button
          className={`co-card-star${isStarred ? " starred" : ""}`}
          aria-pressed={isStarred}
          aria-label={`${isStarred ? "Unstar" : "Star"} ${c.name}`}
          title={isStarred ? "Unstar" : "Star"}
          onClick={() => toggleStar(c.slug)}
        >
          <span className="material-symbols-outlined">star</span>
        </button>
      </div>
    );
  }

  return (
    <div className="company-page">
      <div className="company-inner">
        {!selected ? (
          <>
            <header className="company-head co-head">
              <div>
                <h1>Company Practice</h1>
                <p>
                  Practice for a specific company — pick a real interview-frequency
                  problem, or run a mock interview.
                </p>
              </div>
              {!loadingCompanies && !companiesError && (
                <span className="co-count">
                  {companies.length} {companies.length === 1 ? "company" : "companies"}
                </span>
              )}
            </header>

            <div className="co-search-wrap">
              <span className="material-symbols-outlined co-search-icon">search</span>
              <input
                type="text"
                className="co-search"
                placeholder="Search companies…"
                value={companySearch}
                onChange={(e) => setCompanySearch(e.target.value)}
                disabled={loadingCompanies || !!companiesError}
              />
            </div>

            {starError && <p className="company-status error">{starError}</p>}

            {loadingCompanies ? (
              <p className="company-status">Loading companies…</p>
            ) : companiesError ? (
              <p className="company-status error">{companiesError}</p>
            ) : (
              <>
                {/* Pinned Starred band — a persistent favourites shelf. It always
                    shows (search filters only the directory below), so searching
                    never hides a starred company. Starred companies also appear in
                    the directory; the shared Set keeps the two in sync. */}
                {starredItems.length > 0 && (
                  <div className="co-band">
                    <div className="co-label">
                      <span className="material-symbols-outlined co-label-star">star</span>
                      <span className="co-label-text">Starred</span>
                      {!query && <span className="co-label-hint">— jump back in</span>}
                      <span className="co-label-rule" />
                    </div>
                    <div className="co-band-grid">{starredItems.map(renderStarCard)}</div>
                  </div>
                )}

                <div className="co-directory">
                  <div className="co-label">
                    <span className="co-label-text">All companies</span>
                    <span className="co-label-rule" />
                  </div>

                  {filtered.length === 0 ? (
                    <p className="company-status">
                      {query
                        ? "No companies match your search."
                        : "No companies available."}
                    </p>
                  ) : (
                    <div className="co-scroll-wrap">
                      <div className="co-scroll" ref={dirScrollRef}>
                        {query ? (
                          // Search: a flat grid of matches (no rail / letter groups).
                          <div className="co-grid">{filtered.map(renderDirCard)}</div>
                        ) : (
                          <div className="co-scroll-inner">
                            <div className="co-rail">
                              {railLetters.map((r) => (
                                <span
                                  key={r.L}
                                  className={`co-rail-letter${r.present ? "" : " absent"}`}
                                  onClick={r.present ? () => jumpToLetter(r.L) : undefined}
                                >
                                  {r.L}
                                </span>
                              ))}
                            </div>
                            <div className="co-groups">
                              {groups.map((g) => (
                                <div
                                  key={g.letter}
                                  className="co-group"
                                  ref={(el) => {
                                    groupRefs.current[g.letter] = el;
                                  }}
                                >
                                  <div className="co-group-head">
                                    <span className="co-group-letter">{g.letter}</span>
                                    <span className="co-label-rule" />
                                  </div>
                                  <div className="co-grid">
                                    {g.items.map(renderDirCard)}
                                  </div>
                                </div>
                              ))}
                            </div>
                          </div>
                        )}
                      </div>
                      <div className="co-fade" />
                    </div>
                  )}
                </div>
              </>
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
              <div className="company-controls-pager">{pager}</div>
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

              {pager}
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
                <span className="material-symbols-outlined">
                  {mockLimitMinutes > 0 ? "schedule" : "timer_off"}
                </span>
                {mockLimitMinutes > 0
                  ? `~${mockLimitMinutes} minutes suggested`
                  : "Untimed — practice at your own pace"}
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
