import { useCallback, useRef, useState } from "react";

/** A finished recording: base64-encoded audio (no data-URI prefix) + its MIME type. */
export interface Recording {
  base64: string;
  mimeType: string;
}

/**
 * useVoiceRecorder wraps MediaRecorder for click-to-toggle push-to-talk.
 * `start()` requests mic access and begins recording; `stop()` ends it and
 * resolves the captured audio (or null if nothing was recorded). The MIME type
 * is whatever the platform produces — on macOS WKWebView that's audio/mp4, on
 * Chromium audio/webm — and is passed through so the backend labels the upload
 * correctly.
 */
export function useVoiceRecorder() {
  const [recording, setRecording] = useState(false);
  const [error, setError] = useState("");
  const recorderRef = useRef<MediaRecorder | null>(null);
  const chunksRef = useRef<Blob[]>([]);
  const streamRef = useRef<MediaStream | null>(null);

  const supported =
    typeof navigator !== "undefined" &&
    !!navigator.mediaDevices &&
    typeof MediaRecorder !== "undefined";

  const start = useCallback(async () => {
    setError("");
    if (!supported) {
      setError("Audio recording isn't supported in this environment.");
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
      streamRef.current = stream;
      const mimeType = pickMimeType();
      const recorder = mimeType
        ? new MediaRecorder(stream, { mimeType })
        : new MediaRecorder(stream);
      chunksRef.current = [];
      recorder.ondataavailable = (e) => {
        if (e.data.size > 0) chunksRef.current.push(e.data);
      };
      recorderRef.current = recorder;
      recorder.start();
      setRecording(true);
    } catch (e: any) {
      // Most commonly a denied OS permission prompt.
      setError(e?.message || "Could not access the microphone.");
      setRecording(false);
    }
  }, [supported]);

  const stop = useCallback((): Promise<Recording | null> => {
    return new Promise((resolve) => {
      const recorder = recorderRef.current;
      if (!recorder || recorder.state === "inactive") {
        setRecording(false);
        resolve(null);
        return;
      }
      recorder.onstop = async () => {
        const blob = new Blob(chunksRef.current, { type: recorder.mimeType });
        streamRef.current?.getTracks().forEach((t) => t.stop());
        streamRef.current = null;
        recorderRef.current = null;
        setRecording(false);
        if (blob.size === 0) {
          resolve(null);
          return;
        }
        try {
          resolve({ base64: await blobToBase64(blob), mimeType: recorder.mimeType });
        } catch {
          resolve(null);
        }
      };
      recorder.stop();
    });
  }, []);

  return { recording, start, stop, error, supported };
}

// pickMimeType returns the first MediaRecorder type the platform supports.
// Chromium prefers webm/opus; WKWebView (macOS) falls through to audio/mp4.
function pickMimeType(): string {
  const candidates = ["audio/webm;codecs=opus", "audio/webm", "audio/mp4", "audio/mpeg"];
  for (const c of candidates) {
    if (typeof MediaRecorder !== "undefined" && MediaRecorder.isTypeSupported?.(c)) {
      return c;
    }
  }
  return ""; // let the browser pick its default
}

// blobToBase64 reads a Blob as base64, stripping the "data:...;base64," prefix.
function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onloadend = () => {
      const result = String(reader.result);
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}
