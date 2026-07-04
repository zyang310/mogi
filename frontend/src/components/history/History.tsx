import { useEffect, useState } from "react";
import {
  ListSessions,
  GetSessionTranscript,
  GetDebrief,
  DeleteSession,
  models,
} from "../../lib/wailsBridge";
import SessionHistoryCard from "./SessionHistoryCard";
import "./History.css";

// History is the Session History page: a reverse-chronological list of past
// sessions, each expandable to its full transcript and deletable. It owns its own
// data fetch and per-card transcript cache (transcripts load lazily on expand).
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
  // caches it, so a later app run still returns instantly.
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
        if (!cancelled) setSessions(list ?? []);
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
  // request, and no-op if it's already loaded or in flight.
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

  return (
    <div className="history-page">
      <div className="history-inner">
        <header className="history-head">
          <h1>Session History</h1>
          <p>Review past technical interviews and their transcripts.</p>
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
          <div className="history-list">
            {sessions.map((s) => (
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
        )}
      </div>
    </div>
  );
}
