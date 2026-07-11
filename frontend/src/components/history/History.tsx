import { useEffect, useState } from "react";
import {
  ListSessions,
  GetSessionTranscript,
  GetDebrief,
  DeleteSession,
  models,
} from "../../lib/wailsBridge";
import { groupByRecency } from "../../lib/format";
import { scoreOf } from "../../lib/verdict";
import SessionHistoryCard from "./SessionHistoryCard";
import "./History.css";

// History is the Session History page: a timeline of past sessions grouped by
// recency, each expandable inline to its full transcript and deletable. It owns
// its own data fetch and per-card transcript cache (transcripts load lazily on
// expand).
export default function History() {
  const [sessions, setSessions] = useState<models.SessionSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [transcripts, setTranscripts] = useState<Record<string, models.Message[]>>({});
  const [transcriptLoading, setTranscriptLoading] = useState<string | null>(null);
  const [transcriptErrors, setTranscriptErrors] = useState<Record<string, string>>({});

  // Debrief is generated lazily the first time a card's Debrief tab is opened and
  // cached per-card, mirroring the transcript pattern above. The backend also
  // caches it, so a later app run still returns instantly — and ListSessions now
  // returns any already-cached debrief inline, so the initial load below seeds
  // this map directly and ensureDebrief's already-cached guard skips the round
  // trip entirely for sessions reviewed before.
  const [debriefs, setDebriefs] = useState<Record<string, models.Debrief>>({});
  const [debriefLoading, setDebriefLoading] = useState<string | null>(null);
  const [debriefErrors, setDebriefErrors] = useState<Record<string, string>>({});

  // Load the session list on mount.
  useEffect(() => {
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError("");
      try {
        const list = await ListSessions();
        if (!cancelled) {
          setSessions(list ?? []);
          const seeded: Record<string, models.Debrief> = {};
          for (const s of list ?? []) {
            if (s.debrief) seeded[s.id] = s.debrief;
          }
          setDebriefs(seeded);
        }
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

  // Toggle a card open/closed, lazy-loading its transcript on first open.
  async function toggle(id: string) {
    if (expandedId === id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(id);
    if (transcripts[id]) return; // already cached

    setTranscriptLoading(id);
    setTranscriptErrors((prev) => ({ ...prev, [id]: "" }));
    try {
      const msgs = await GetSessionTranscript(id);
      setTranscripts((prev) => ({ ...prev, [id]: msgs ?? [] }));
    } catch (e: any) {
      setTranscriptErrors((prev) => ({ ...prev, [id]: e?.message || String(e) }));
    } finally {
      setTranscriptLoading((cur) => (cur === id ? null : cur));
    }
  }

  // Ensure a card's debrief is loaded (idempotent): generate + cache it on first
  // request, and no-op if it's already loaded (including pre-seeded from the
  // session list) or in flight.
  async function ensureDebrief(id: string) {
    if (debriefs[id] || debriefLoading === id) return;

    setDebriefLoading(id);
    setDebriefErrors((prev) => ({ ...prev, [id]: "" }));
    try {
      const d = await GetDebrief(id);
      setDebriefs((prev) => ({ ...prev, [id]: d }));
    } catch (e: any) {
      setDebriefErrors((prev) => ({ ...prev, [id]: e?.message || String(e) }));
    } finally {
      setDebriefLoading((cur) => (cur === id ? null : cur));
    }
  }

  // Delete a session and drop it from the list (no full refetch).
  async function remove(id: string) {
    try {
      await DeleteSession(id);
      setSessions((prev) => prev.filter((s) => s.id !== id));
      if (expandedId === id) setExpandedId(null);
    } catch (e: any) {
      setError(e?.message || String(e));
    }
  }

  // Headline stats: total sessions, and the average of every reviewed session's
  // score (sessions never opened to their Debrief tab have none yet and are
  // excluded, not counted as 0).
  const scores = sessions
    .map((s) => s.debrief && scoreOf(s.debrief.rubric))
    .filter((n): n is number => !!n);
  const avgScore =
    scores.length > 0 ? Math.round((scores.reduce((a, b) => a + b, 0) / scores.length) * 10) / 10 : null;

  const groups = groupByRecency(sessions, (s) => s.startedAt);

  return (
    <div className="history-page">
      <div className="history-inner">
        <header className="history-head">
          <div className="history-head-text">
            <h1>Session History</h1>
            <p>Review past technical interviews and their transcripts.</p>
          </div>
          {!loading && !error && sessions.length > 0 && (
            <div className="history-stats">
              <div className="history-stat">
                <div className="history-stat-value">{sessions.length}</div>
                <div className="history-stat-label">sessions</div>
              </div>
              {avgScore !== null && (
                <div className="history-stat">
                  <div className="history-stat-value accent">{avgScore}</div>
                  <div className="history-stat-label">avg score</div>
                </div>
              )}
            </div>
          )}
        </header>

        {loading ? (
          <p className="history-status">Loading sessions…</p>
        ) : error ? (
          <div className="history-status error">{error}</div>
        ) : sessions.length === 0 ? (
          <div className="history-empty">
            <span className="material-symbols-outlined">history</span>
            <p className="history-empty-title">No sessions yet</p>
            <p className="history-empty-sub">
              Finished interviews will appear here with their full transcript.
            </p>
          </div>
        ) : (
          <div className="history-groups">
            {groups.map((group) => (
              <section key={group.label} className="history-group">
                <div className="history-group-label">{group.label}</div>
                <div className="history-rail">
                  {group.items.map((s) => (
                    <SessionHistoryCard
                      key={s.id}
                      summary={s}
                      expanded={expandedId === s.id}
                      transcript={transcripts[s.id]}
                      loadingTranscript={transcriptLoading === s.id}
                      transcriptError={transcriptErrors[s.id]}
                      debrief={debriefs[s.id]}
                      loadingDebrief={debriefLoading === s.id}
                      debriefError={debriefErrors[s.id]}
                      onToggle={() => toggle(s.id)}
                      onDebrief={() => ensureDebrief(s.id)}
                      onDelete={() => remove(s.id)}
                    />
                  ))}
                </div>
              </section>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
