import { useState, useEffect, useRef } from "react";
import CapturePanel from "./components/session/CapturePanel";
import Chat from "./components/session/Chat";
import Overlay from "./components/session/Overlay";
import RegionSelector from "./components/session/RegionSelector";
import CompanyBanner from "./components/company/CompanyBanner";
import CompanyPractice from "./components/company/CompanyPractice";
import History from "./components/history/History";
import HubReady from "./components/hub/HubReady";
import Settings from "./components/settings/Settings";
import SetupPage from "./components/setup/SetupPage";
import UpdateBanner from "./components/common/UpdateBanner";
import WindowControls from "./components/common/WindowControls";
import { cleanForDisplay } from "./lib/markdown";
import {
  EventsOn,
  GetAuthStatus,
  GetPreferences,
  StartSession,
  EndSession,
  EnterOverlayMode,
  ExitOverlayMode,
  SendMessage,
  SetCaptureRegion,
  SetOverlayExpanded,
  SynthesizeSpeech,
  TranscribeAudio,
  UpdatePreferences,
  models,
} from "./lib/wailsBridge";
import { useVoiceRecorder } from "./lib/useVoiceRecorder";
import { useAudioPlayer } from "./lib/useAudioPlayer";
import { useUpdateCheck } from "./lib/useUpdateCheck";
import "./App.css";

interface ChatMessage {
  role: "user" | "assistant";
  content: string;
}

function formatTime(sec: number): string {
  const m = Math.floor(sec / 60).toString().padStart(2, "0");
  const s = (sec % 60).toString().padStart(2, "0");
  return `${m}:${s}`;
}

// MOCK_LIMIT_MINUTES is the fixed time budget for a mock interview (two problems),
// sized to a real two-problem screen. Single-problem practice uses the user's
// configured session limit instead; untimed (limit 0) stays untimed for both.
const MOCK_LIMIT_MINUTES = 45;

function App() {
  const [authStatus, setAuthStatus] = useState<models.AuthStatus>(
    new models.AuthStatus({
      openRouterConfigured: false,
      elevenLabsConfigured: false,
      googleConfigured: false,
    })
  );
  const [authLoaded, setAuthLoaded] = useState(false);
  const [setupDone, setSetupDone] = useState(false);
  const [prefs, setPrefs] = useState<models.Preferences | null>(null);
  const [sessionId, setSessionId] = useState<string | null>(null);
  const [sessionStartedAt, setSessionStartedAt] = useState<Date | null>(null);
  const [elapsedSec, setElapsedSec] = useState(0);
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(false);
  const [regionOpen, setRegionOpen] = useState(false);
  const [view, setView] = useState<"hub" | "company" | "history" | "settings">("hub");
  const [overlayMode, setOverlayMode] = useState(false);
  // Set while a Company Practice / mock session is active; drives the session
  // banner and is cleared when the session ends. null for default Hub sessions.
  const [companySession, setCompanySession] = useState<models.CompanySessionStart | null>(null);
  const [error, setError] = useState("");

  // Voice: mic recorder + audio player, plus session-level "voice mode" (when on,
  // interviewer replies are spoken). voiceModeRef mirrors the state so async
  // handlers read the latest value without a stale closure.
  const recorder = useVoiceRecorder();
  const player = useAudioPlayer();
  // Launch-time "newer release available?" check; drives the hub banner below.
  const { update, dismiss: dismissUpdate } = useUpdateCheck();
  const [voiceMode, setVoiceMode] = useState(false);
  const [transcribing, setTranscribing] = useState(false);
  const voiceModeRef = useRef(false);

  // Voice hotkey: pttBusyRef guards the toggle against overlapping presses; the
  // *Ref mirrors let the once-subscribed key handler call the latest recording
  // closures without re-subscribing each render; the timer is a safety net that
  // auto-stops a recording the user forgets to end.
  const pttBusyRef = useRef(false);
  const pttTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const toggleRecordingRef = useRef<() => void>(() => {});
  const stopAndSendRef = useRef<() => void>(() => {});

  function setVoice(on: boolean) {
    voiceModeRef.current = on;
    setVoiceMode(on);
    if (!on) player.stop();
  }

  // Surface mic/permission errors from the recorder in the shared banner.
  useEffect(() => {
    if (recorder.error) setError(recorder.error);
  }, [recorder.error]);

  async function loadPrefs() {
    try {
      const p = await GetPreferences();
      setPrefs(p);
    } catch {
      // Wails runtime not present in browser preview
    }
  }

  // Persist the Company Practice tab's last company + difficulty so it resumes
  // where the user left off. Merges into the current prefs; best-effort.
  async function rememberCompany(slug: string, difficulty: string) {
    if (!prefs) return;
    const next = new models.Preferences({
      ...prefs,
      lastCompany: slug,
      lastDifficulty: difficulty,
    });
    setPrefs(next);
    try {
      await UpdatePreferences(next);
    } catch {
      // Non-fatal — resume is a convenience.
    }
  }

  // On mount: load auth status and preferences.
  useEffect(() => {
    (async () => {
      try {
        const s = await GetAuthStatus();
        setAuthStatus(s);
      } catch {
        // Wails runtime not present (browser preview) or key not set — show setup page.
      } finally {
        setAuthLoaded(true);
      }
    })();
    loadPrefs();
  }, []);

  // Tick the session timer every second while a session is active.
  useEffect(() => {
    if (!sessionStartedAt) {
      setElapsedSec(0);
      return;
    }
    const tick = () =>
      setElapsedSec(Math.floor((Date.now() - sessionStartedAt.getTime()) / 1000));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, [sessionStartedAt]);

  // Route to the settings page if no API key is configured — but only once the
  // user is past setup and auth has loaded. Without these guards it fires on the
  // initial default auth state (openRouterConfigured = false, before GetAuthStatus
  // resolves) and pins the view to settings, so "Continue to Hub" lands on Settings
  // even for configured users.
  useEffect(() => {
    if (authLoaded && setupDone && !authStatus.openRouterConfigured) {
      setView("settings");
    }
  }, [authLoaded, setupDone, authStatus.openRouterConfigured]);

  // Global voice hotkey: while enabled, each key press toggles recording — press
  // once to start, again to stop and send (same as the mic button). The backend
  // emits one "ptt:down" per press (releases aren't sent). The real guards live in
  // handleMicToggle / startRecording; pttBusyRef is reset on cleanup so toggling
  // the feature off can't leave it stuck.
  useEffect(() => {
    if (!prefs?.pushToTalkEnabled) return;
    const offDown = EventsOn("ptt:down", () => {
      void toggleRecordingRef.current();
    });
    return () => {
      offDown();
      pttBusyRef.current = false;
    };
  }, [prefs?.pushToTalkEnabled]);

  async function handleStart() {
    setError("");
    try {
      const session = await StartSession("");
      setCompanySession(null); // default Hub session — no company context
      setSessionId(session.id);
      setSessionStartedAt(new Date(session.startedAt));
      setMessages([]);
    } catch (e: any) {
      setError(e?.message || String(e));
    }
  }

  // Enter the active-session UI for a company/mock session started by the
  // CompanyPractice tab. The backend already created the session and persisted
  // the opener; we mirror the opener as the first interviewer turn and speak it
  // when voice mode is on (speak() itself no-ops otherwise).
  function handleCompanyStarted(start: models.CompanySessionStart) {
    setError("");
    setCompanySession(start);
    setSessionId(start.session.id);
    setSessionStartedAt(new Date(start.session.startedAt));
    setMessages(start.opening ? [{ role: "assistant", content: start.opening }] : []);
    if (start.opening) void speak(start.opening);
  }

  async function handleEnd() {
    if (!sessionId) return;
    setError("");
    player.stop();
    if (recorder.recording) void recorder.stop();
    try {
      await EndSession(sessionId);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setSessionId(null);
      setSessionStartedAt(null);
      setCompanySession(null);
    }
  }

  // Whether any TTS provider (Google or ElevenLabs) is available to speak replies.
  const ttsConfigured = authStatus.googleConfigured || authStatus.elevenLabsConfigured;
  // Whether any STT provider is available to transcribe the mic (Scribe if an
  // ElevenLabs key exists, else Google — resolved in the backend's activeSTT).
  const sttConfigured = authStatus.googleConfigured || authStatus.elevenLabsConfigured;

  // Speak an interviewer reply via the active TTS provider — only when voice mode
  // is on and a key is configured. Fire-and-forget from handleSend; errors surface
  // in the banner but never drop the (already-shown) text reply.
  async function speak(text: string) {
    if (!voiceModeRef.current || !ttsConfigured) return;
    try {
      const audioB64 = await SynthesizeSpeech(text);
      await player.play(audioB64, prefs?.voiceSpeed ?? 1);
    } catch (e: any) {
      setError(e?.message || String(e));
    }
  }

  // Toggle the speaker (voice mode). Turning it on without a key is a no-op with
  // a clear error; turning it off stops any in-flight playback (via setVoice).
  function toggleVoice() {
    if (!voiceMode && !ttsConfigured) {
      setError("No voice provider configured — add a Google Cloud or ElevenLabs key in Settings.");
      return;
    }
    setVoice(!voiceMode);
  }

  // Recording shared by the click-to-toggle mic button and global push-to-talk.
  // startRecording opens the mic (barging in over any TTS); stopAndSend ends it,
  // transcribes, and feeds the text into the normal send loop. Speaking a
  // question turns voice mode on so the reply is spoken back.
  async function startRecording() {
    if (sessionId === null) return; // only record during an active session
    if (!sttConfigured) {
      setError("No transcription provider configured — add a Google Cloud or ElevenLabs key in Settings.");
      return;
    }
    if (recorder.recording || transcribing) return;
    setError("");
    player.stop(); // barge in over the interviewer
    await recorder.start();
    // Safety net: stop a recording whose key-up was never received.
    if (pttTimerRef.current) clearTimeout(pttTimerRef.current);
    pttTimerRef.current = setTimeout(() => {
      pttBusyRef.current = false;
      void stopAndSendRef.current();
    }, 300000);
  }

  async function stopAndSend() {
    if (pttTimerRef.current) {
      clearTimeout(pttTimerRef.current);
      pttTimerRef.current = null;
    }
    if (!recorder.recording) return;
    const rec = await recorder.stop();
    if (!rec) return;
    setTranscribing(true);
    try {
      const text = (await TranscribeAudio(rec.base64, rec.mimeType)).trim();
      if (text) {
        setVoice(true);
        await handleSend(text);
      }
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setTranscribing(false);
    }
  }

  // Toggle recording — shared by the mic button and the voice hotkey. First
  // call records, second stops and sends. pttBusyRef serializes the async
  // start/stop so a fast double-press (or double-click) can't overlap.
  async function handleMicToggle() {
    if (pttBusyRef.current) return;
    pttBusyRef.current = true;
    try {
      if (recorder.recording) await stopAndSend();
      else await startRecording();
    } finally {
      pttBusyRef.current = false;
    }
  }

  // Keep latest-closure refs current so the once-subscribed hotkey handler and
  // the safety timer always call the freshest functions without re-subscribing.
  toggleRecordingRef.current = handleMicToggle;
  stopAndSendRef.current = stopAndSend;

  // "Full Screen" capture control — clear any cropped region (w=h=0) so the
  // backend captures the whole selected display.
  async function handleFullScreen() {
    if (!prefs) return;
    setError("");
    try {
      await SetCaptureRegion(prefs.captureDisplay, 0, 0, 0, 0);
      await loadPrefs();
    } catch (e: any) {
      setError(e?.message || String(e));
    }
  }

  // Collapse the app into the always-on-top floating overlay bar. The Go calls
  // resize/pin the window; they no-op in browser preview (no Wails runtime).
  function enterOverlay() {
    setOverlayMode(true);
    EnterOverlayMode().catch(() => {});
  }

  function exitOverlay() {
    setOverlayMode(false);
    ExitOverlayMode().catch(() => {});
  }

  async function handleEndFromOverlay() {
    exitOverlay();
    await handleEnd();
  }

  async function handleSend(text: string) {
    setError("");
    setMessages((prev) => [...prev, { role: "user", content: text }]);
    setLoading(true);
    try {
      const response = await SendMessage(text);
      setMessages((prev) => [...prev, { role: "assistant", content: response }]);
      void speak(response);
    } catch (e: any) {
      setError(e?.message || String(e));
      // Remove the user message on failure so they can re-send.
      setMessages((prev) => prev.slice(0, -1));
    } finally {
      setLoading(false);
    }
  }

  // Always show the welcome/setup page first; "Continue to Hub" dismisses it.
  if (!authLoaded) return null;
  if (!setupDone) {
    return (
      <>
        <WindowControls />
        <SetupPage
          authStatus={authStatus}
          onAuthChange={setAuthStatus}
          onContinue={() => setSetupDone(true)}
        />
      </>
    );
  }

  const isActive = sessionId !== null;
  // A mock is the only session with two assigned problems; it gets the fixed
  // MOCK_LIMIT_MINUTES budget. Single-problem practice (and Hub sessions) use the
  // configured limit. Untimed (0) is preserved for both — never force a mock timer.
  const baseLimitMinutes = prefs?.sessionLimitMinutes ?? 30;
  const isMockSession = !!companySession && companySession.problems.length > 1;
  const mockLimitMinutes = baseLimitMinutes === 0 ? 0 : MOCK_LIMIT_MINUTES;
  const limitSec = (isMockSession ? mockLimitMinutes : baseLimitMinutes) * 60;
  const warnSec = (prefs?.softWarningMinutes ?? 25) * 60;
  const timedOut = isActive && limitSec > 0 && elapsedSec >= limitSec;
  const nearLimit = isActive && limitSec > 0 && warnSec > 0 && elapsedSec >= warnSec && !timedOut;
  const displayNum = prefs ? prefs.captureDisplay + 1 : 1;
  const cropped = prefs ? prefs.regionW > 0 : false;
  const targetLabel = `Display ${displayNum} · ${cropped ? "cropped region" : "full display"}`;

  // Compact always-on-top overlay takes over the whole (resized) window.
  if (isActive && overlayMode) {
    const lastAi = [...messages].reverse().find((m) => m.role === "assistant");
    return (
      <Overlay
        messages={messages}
        latestAiText={
          lastAi?.content
            ? cleanForDisplay(lastAi.content)
            : "Listening… the interviewer will respond as you work."
        }
        onEnd={handleEndFromOverlay}
        onExpand={exitOverlay}
        onHistoryToggle={(open) => {
          SetOverlayExpanded(open).catch(() => {});
        }}
        recording={recorder.recording}
        transcribing={transcribing}
        speaking={player.speaking}
        voiceMode={voiceMode}
        onMicToggle={handleMicToggle}
        onToggleVoice={toggleVoice}
      />
    );
  }

  return (
    <div className="app">
      <WindowControls />
      {/* Floating pill navigation */}
      <nav className="pill-nav">
        <button
          className={`pill-tab${view === "hub" ? " active" : ""}`}
          onClick={() => setView("hub")}
        >
          <span className="material-symbols-outlined">grid_view</span>
          <span className="pill-tab-label">Hub</span>
        </button>
        <button
          className={`pill-tab${view === "company" ? " active" : ""}`}
          onClick={() => setView("company")}
        >
          <span className="material-symbols-outlined">domain</span>
          <span className="pill-tab-label">Companies</span>
        </button>
        <button
          className={`pill-tab${view === "history" ? " active" : ""}`}
          onClick={() => setView("history")}
        >
          <span className="material-symbols-outlined">history</span>
          <span className="pill-tab-label">History</span>
        </button>
        <button
          className={`pill-tab${view === "settings" ? " active" : ""}`}
          onClick={() => setView("settings")}
        >
          <span className="material-symbols-outlined">settings</span>
          <span className="pill-tab-label">Settings</span>
        </button>
      </nav>

      <div className="app-content">
        {/* Update-available banner — idle screens only, never over the overlay
            or mid-interview (preserves the transparent overlay invariant). */}
        {update && !isActive && (
          <UpdateBanner update={update} onDismiss={dismissUpdate} />
        )}

        {/* Warning banner (approaching time limit) */}
        {nearLimit && (
          <div className="app-warning">
            {Math.ceil((limitSec - elapsedSec) / 60)} minute(s) remaining in this session.
          </div>
        )}

        {/* Timeout banner */}
        {timedOut && (
          <div className="app-error">
            <span>Session time limit reached — review your work or end the interview.</span>
          </div>
        )}

        {/* Error banner */}
        {error && (
          <div className="app-error">
            <span>{error}</span>
            <button className="error-dismiss" onClick={() => setError("")}>
              &times;
            </button>
          </div>
        )}

        {view === "settings" ? (
          <Settings
            authStatus={authStatus}
            onAuthChange={setAuthStatus}
            onPrefsChange={setPrefs}
          />
        ) : view === "history" ? (
          <History />
        ) : !isActive ? (
          view === "company" ? (
            <CompanyPractice
              onStarted={handleCompanyStarted}
              initialCompany={prefs?.lastCompany ?? ""}
              initialDifficulty={prefs?.lastDifficulty ?? "All"}
              onRemember={rememberCompany}
              mockLimitMinutes={mockLimitMinutes}
            />
          ) : (
            <HubReady
              onStart={handleStart}
              onDefineRegion={() => setRegionOpen(true)}
              onFullScreen={handleFullScreen}
              canStart={authStatus.openRouterConfigured}
              targetLabel={targetLabel}
            />
          )
        ) : (
          <>
            {/* Active-session bar */}
            <div className="session-bar">
              <span className="session-bar-status">
                <span className="session-bar-dot" />
                Interview in progress
              </span>
              <div className="session-bar-right">
                {limitSec > 0 && (
                  <span
                    className={`session-timer${nearLimit ? " timer-warning" : ""}${timedOut ? " timer-expired" : ""}`}
                  >
                    {formatTime(elapsedSec)} / {formatTime(limitSec)}
                  </span>
                )}
                <button
                  className="btn btn-ghost btn-icon"
                  onClick={enterOverlay}
                  title="Collapse to floating overlay over your IDE"
                >
                  <span className="material-symbols-outlined">picture_in_picture</span>
                  Compact
                </button>
                <button className="btn btn-danger" onClick={handleEnd}>
                  End Interview
                </button>
              </div>
            </div>

            {/* Company/mock session banner (assigned problem + Q2 reveal card) */}
            {companySession && <CompanyBanner start={companySession} />}

            {/* Capture + chat */}
            <main className="app-body">
              <div className="panel-capture">
                <CapturePanel
                  isActive={isActive}
                  prefs={prefs}
                  onSetRegion={() => setRegionOpen(true)}
                />
              </div>
              <div className="panel-divider" />
              <div className="panel-chat">
                <Chat
                  messages={messages}
                  onSend={handleSend}
                  loading={loading}
                  disabled={!isActive || timedOut}
                  recording={recorder.recording}
                  transcribing={transcribing}
                  voiceMode={voiceMode}
                  onMicToggle={handleMicToggle}
                  onToggleVoice={toggleVoice}
                />
              </div>
            </main>
          </>
        )}
      </div>

      {/* Region selector modal */}
      {regionOpen && (
        <RegionSelector
          initialDisplay={prefs?.captureDisplay ?? 0}
          onClose={() => setRegionOpen(false)}
          onSaved={loadPrefs}
        />
      )}

    </div>
  );
}

export default App;
