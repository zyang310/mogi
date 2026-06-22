import { useState, useEffect, useRef } from "react";
import Chat from "./components/Chat";
import CapturePanel from "./components/CapturePanel";
import HubReady from "./components/HubReady";
import Overlay from "./components/Overlay";
import RegionSelector from "./components/RegionSelector";
import Settings from "./components/Settings";
import SetupPage from "./components/SetupPage";
import {
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
  models,
} from "./lib/wailsBridge";
import { useVoiceRecorder } from "./lib/useVoiceRecorder";
import { useAudioPlayer } from "./lib/useAudioPlayer";
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

function App() {
  const [authStatus, setAuthStatus] = useState<models.AuthStatus>(
    new models.AuthStatus({
      openRouterConfigured: false,
      elevenLabsConfigured: false,
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
  const [view, setView] = useState<"hub" | "history" | "settings">("hub");
  const [overlayMode, setOverlayMode] = useState(false);
  const [error, setError] = useState("");

  // Voice: mic recorder + audio player, plus session-level "voice mode" (when on,
  // interviewer replies are spoken). voiceModeRef mirrors the state so async
  // handlers read the latest value without a stale closure.
  const recorder = useVoiceRecorder();
  const player = useAudioPlayer();
  const [voiceMode, setVoiceMode] = useState(false);
  const [transcribing, setTranscribing] = useState(false);
  const voiceModeRef = useRef(false);

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

  // Route to the settings page if no API key is configured.
  useEffect(() => {
    if (!authStatus.openRouterConfigured) {
      setView("settings");
    }
  }, [authStatus.openRouterConfigured]);

  async function handleStart() {
    setError("");
    try {
      const session = await StartSession("");
      setSessionId(session.id);
      setSessionStartedAt(new Date(session.startedAt));
      setMessages([]);
    } catch (e: any) {
      setError(e?.message || String(e));
    }
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
    }
  }

  // Speak an interviewer reply via ElevenLabs TTS — only when voice mode is on
  // and a key is configured. Fire-and-forget from handleSend; errors surface in
  // the banner but never drop the (already-shown) text reply.
  async function speak(text: string) {
    if (!voiceModeRef.current || !authStatus.elevenLabsConfigured) return;
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
    if (!voiceMode && !authStatus.elevenLabsConfigured) {
      setError("ElevenLabs API key not configured — add it in Settings.");
      return;
    }
    setVoice(!voiceMode);
  }

  // Click-to-toggle push-to-talk: first click records, second stops →
  // transcribes → feeds the text into the normal send loop. Speaking a question
  // turns voice mode on so the reply is spoken back.
  async function handleMicToggle() {
    if (recorder.recording) {
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
    } else {
      setError("");
      await recorder.start();
    }
  }

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
      <SetupPage
        authStatus={authStatus}
        onAuthChange={setAuthStatus}
        onContinue={() => setSetupDone(true)}
      />
    );
  }

  const isActive = sessionId !== null;
  const limitSec = (prefs?.sessionLimitMinutes ?? 30) * 60;
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
          lastAi?.content || "Listening… the interviewer will respond as you work."
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
          <div className="history-placeholder">
            <span className="material-symbols-outlined">history</span>
            <p className="history-placeholder-title">Session history</p>
            <p className="history-placeholder-sub">
              Your past interview sessions will appear here. This view is coming soon.
            </p>
          </div>
        ) : !isActive ? (
          <HubReady
            onStart={handleStart}
            onDefineRegion={() => setRegionOpen(true)}
            onFullScreen={handleFullScreen}
            canStart={authStatus.openRouterConfigured}
            targetLabel={targetLabel}
          />
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
