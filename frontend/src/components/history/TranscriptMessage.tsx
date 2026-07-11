import { cleanForDisplay } from "../../lib/markdown";
import "./TranscriptMessage.css";

interface Props {
  role: "user" | "assistant";
  content: string;
}

interface Segment {
  type: "text" | "code";
  text: string;
}

// splitCodeSegments breaks a message into alternating prose/code runs on ```
// fences, so a candidate's pasted solution renders as its own dark code panel
// instead of literal backticks inside a chat bubble.
function splitCodeSegments(content: string): Segment[] {
  const segments: Segment[] = [];
  const fence = /```[a-zA-Z0-9]*\n?([\s\S]*?)```/g;
  let last = 0;
  let m: RegExpExecArray | null;
  while ((m = fence.exec(content))) {
    if (m.index > last) {
      const text = content.slice(last, m.index).trim();
      if (text) segments.push({ type: "text", text });
    }
    const code = m[1].replace(/\n$/, "");
    if (code.trim()) segments.push({ type: "code", text: code });
    last = fence.lastIndex;
  }
  const rest = content.slice(last).trim();
  if (rest) segments.push({ type: "text", text: rest });
  return segments.length > 0 ? segments : [{ type: "text", text: content }];
}

// TranscriptMessage renders one past-session transcript turn for the History
// timeline's Transcript tab: an avatar plus one or more bubbles, splitting a
// candidate's fenced code out of the prose into its own monospace panel. The
// interviewer always speaks plain text (screen-driven invariant — see
// prompts.go), so only candidate turns are scanned for code.
export default function TranscriptMessage({ role, content }: Props) {
  const isUser = role === "user";
  const text = isUser ? content : cleanForDisplay(content);
  const segments = isUser ? splitCodeSegments(text) : [{ type: "text" as const, text }];

  return (
    <div className={`transcript-row ${role}`}>
      <span className={`transcript-avatar ${role}`}>
        <span className="material-symbols-outlined">{isUser ? "person" : "smart_toy"}</span>
      </span>
      <div className="transcript-bubbles">
        {segments.map((seg, i) =>
          seg.type === "code" ? (
            <pre key={i} className="transcript-code">
              <code>{seg.text}</code>
            </pre>
          ) : (
            <div key={i} className={`transcript-bubble ${role}`}>
              <span className="transcript-label">{isUser ? "You" : "Interviewer"}</span>
              <p className="transcript-text">{seg.text}</p>
            </div>
          )
        )}
      </div>
    </div>
  );
}
