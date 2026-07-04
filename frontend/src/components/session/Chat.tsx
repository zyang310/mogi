import { useState, useRef, useEffect, KeyboardEvent } from "react";
import MessageBubble from "../common/MessageBubble";
import "./Chat.css";

interface Message {
  role: "user" | "assistant";
  content: string;
}

interface Props {
  messages: Message[];
  onSend: (text: string) => void;
  loading: boolean;
  disabled: boolean;
  // Voice controls (optional — typed-only callers omit these; the buttons render
  // only when their handlers are provided).
  recording?: boolean;
  transcribing?: boolean;
  voiceMode?: boolean;
  onMicToggle?: () => void;
  onToggleVoice?: () => void;
}

export default function Chat({
  messages,
  onSend,
  loading,
  disabled,
  recording = false,
  transcribing = false,
  voiceMode = false,
  onMicToggle,
  onToggleVoice,
}: Props) {
  const [draft, setDraft] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  // Auto-scroll to bottom when messages change.
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages, loading]);

  function handleKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      send();
    }
  }

  function send() {
    const text = draft.trim();
    if (!text || loading || disabled) return;
    setDraft("");
    onSend(text);
    // Reset textarea height.
    if (textareaRef.current) textareaRef.current.style.height = "auto";
  }

  function autoResize(e: React.ChangeEvent<HTMLTextAreaElement>) {
    const el = e.target;
    setDraft(el.value);
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 120) + "px";
  }

  const placeholder = disabled
    ? "Start an interview session first..."
    : recording
      ? "Listening… click the mic again to send"
      : transcribing
        ? "Transcribing…"
        : "Type your message... (Enter to send)";

  const micIcon = transcribing ? "hourglass_top" : recording ? "stop" : "mic";

  return (
    <div className="chat">
      <div className="chat-messages">
        {messages.length === 0 && !loading && (
          <p className="chat-empty">
            Start the interview and type a message to begin.
          </p>
        )}
        {messages.map((m, i) => (
          <MessageBubble key={i} role={m.role} content={m.content} />
        ))}
        {loading && (
          <div className="bubble-row assistant">
            <div className="bubble assistant thinking">
              <span className="dot-pulse" />
            </div>
          </div>
        )}
        <div ref={bottomRef} />
      </div>

      <div className="chat-input-area">
        {onToggleVoice && (
          <button
            className={`chat-icon-btn${voiceMode ? " is-active" : ""}`}
            onClick={onToggleVoice}
            disabled={disabled}
            title={voiceMode ? "Voice mode on — replies are spoken" : "Voice mode off"}
          >
            <span className="material-symbols-outlined">
              {voiceMode ? "volume_up" : "volume_off"}
            </span>
          </button>
        )}
        {onMicToggle && (
          <button
            className={`chat-icon-btn${recording ? " is-recording" : ""}`}
            onClick={onMicToggle}
            disabled={disabled || transcribing}
            title={recording ? "Stop and send" : "Speak your message"}
          >
            <span
              className={`material-symbols-outlined${transcribing ? " spin" : ""}`}
            >
              {micIcon}
            </span>
          </button>
        )}
        <textarea
          ref={textareaRef}
          className="chat-input"
          value={draft}
          onChange={autoResize}
          onKeyDown={handleKeyDown}
          placeholder={placeholder}
          disabled={disabled || loading}
          rows={1}
        />
        <button
          className="chat-send"
          onClick={send}
          disabled={!draft.trim() || loading || disabled}
        >
          Send
        </button>
      </div>
    </div>
  );
}
