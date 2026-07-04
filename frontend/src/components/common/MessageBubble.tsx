import { cleanForDisplay } from "../../lib/markdown";
import "./MessageBubble.css";

interface Props {
  role: "user" | "assistant";
  content: string;
}

export default function MessageBubble({ role, content }: Props) {
  // The interviewer is instructed to speak in plain text, but the model can still
  // emit stray markdown (backticks, list markers, emphasis). Strip it for display
  // so the bubble shows the same clean prose the user hears — never raw symbols.
  // User turns are shown verbatim; they may legitimately contain code or symbols.
  const text = role === "assistant" ? cleanForDisplay(content) : content;
  return (
    <div className={`bubble-row ${role}`}>
      <div className={`bubble ${role}`}>
        <span className="bubble-label">{role === "user" ? "You" : "Interviewer"}</span>
        <p className="bubble-text">{text}</p>
      </div>
    </div>
  );
}
