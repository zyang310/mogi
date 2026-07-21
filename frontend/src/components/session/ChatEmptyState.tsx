import "./ChatEmptyState.css";

interface StarterChip {
  icon: string;
  label: string;
  message: string;
  accent?: boolean; // true only for the waving_hand chip (secondary-tinted icon)
}

// Fixed quick-start prompts shown before the first message. Clicking one sends
// `message` immediately through the same onSend path as the textarea — it does
// not just prefill the draft. Deliberately named "starter chip", never
// "opener", to avoid confusion with Company Practice's AI opener
// (internal/ai/prompts.go's CompanyOpening/MockOpening, CompanySessionStart.opening).
const STARTER_CHIPS: StarterChip[] = [
  { icon: "waving_hand", label: "I'm ready to start", message: "I'm ready to start.", accent: true },
  { icon: "description", label: "Walk me through the problem", message: "Can you walk me through the problem?" },
  { icon: "lightbulb", label: "Give me a hint", message: "Can you give me a hint?" },
];

interface Props {
  onSend: (text: string) => void;
  disabled: boolean;
  showMicHint: boolean;
}

// macOS-only: native full-screen (green button) gives an app its own Space and
// evicts every other window, so the floating overlay can't sit over it. The tip
// below steers users to a filled/zoomed window instead. Detected from the user
// agent rather than a backend round-trip — it's a display-only hint, and
// Windows/Linux overlays aren't evicted by full-screen, so they don't need it.
const IS_MAC = typeof navigator !== "undefined" && /Mac/i.test(navigator.userAgent);

// ChatEmptyState is the "ready to begin" placeholder shown in Chat before the
// first message — a breathing icon avatar, headline, body copy, and three
// quick-start chips that send a canned first message via onSend.
export default function ChatEmptyState({ onSend, disabled, showMicHint }: Props) {
  return (
    <div className="chat-empty">
      <div className="chat-empty-avatar">
        <span className="material-symbols-outlined">forum</span>
      </div>

      <h2 className="chat-empty-headline">Ready when you are</h2>

      <p className="chat-empty-body">
        Send a message to open the conversation. Your interviewer can see the
        problem on your screen and will respond in real time — just like the
        real thing.
      </p>

      <div className="chat-starter-chips">
        {STARTER_CHIPS.map((chip) => (
          <button
            key={chip.label}
            className="chat-starter-chip"
            onClick={() => onSend(chip.message)}
            disabled={disabled}
          >
            <span
              className={`material-symbols-outlined${chip.accent ? " chat-starter-chip-icon--accent" : ""}`}
            >
              {chip.icon}
            </span>
            {chip.label}
          </button>
        ))}
      </div>

      {showMicHint && (
        <p className="chat-empty-mic-hint">
          <span className="material-symbols-outlined">mic</span>
          or press the mic to speak your answer
        </p>
      )}

      {IS_MAC && (
        <p className="chat-empty-fullscreen-tip">
          <span className="material-symbols-outlined">desktop_windows</span>
          <span>
            Keep your coding window <strong>filled to the screen</strong> — not
            in macOS full-screen (green button). Full-screen apps get their own
            Space and hide floating overlays like Mogi.
          </span>
        </p>
      )}
    </div>
  );
}
