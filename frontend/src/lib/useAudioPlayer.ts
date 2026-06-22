import { useCallback, useEffect, useRef, useState } from "react";

/**
 * useAudioPlayer plays a single audio clip at a time — either a base64-encoded
 * MP3 (as returned by SynthesizeSpeech) or a direct URL (a voice preview).
 * Starting a new clip stops the current one. `speaking` is true while audio is
 * playing, for UI indicators (the "Live" dot, AI-speaking state).
 */
export function useAudioPlayer() {
  const audioRef = useRef<HTMLAudioElement | null>(null);
  const [speaking, setSpeaking] = useState(false);

  // Stop playback and reset; safe to call when nothing is playing.
  const stop = useCallback(() => {
    const el = audioRef.current;
    if (el) {
      el.pause();
      el.currentTime = 0;
      audioRef.current = null;
    }
    setSpeaking(false);
  }, []);

  // Tear down on unmount so audio doesn't outlive the component.
  useEffect(() => stop, [stop]);

  // Play a base64 MP3 or a URL (http/https/data/blob). `rate` is the playback
  // speed (1 = normal); preservesPitch keeps the voice natural when sped up or
  // slowed down. Returns the play() promise (resolves once playback begins;
  // rejects if it can't start).
  const play = useCallback(
    (src: string, rate = 1) => {
      stop();
      const isUrl = /^(https?:|data:|blob:)/i.test(src);
      const el = new Audio(isUrl ? src : `data:audio/mpeg;base64,${src}`);
      el.playbackRate = rate;
      el.preservesPitch = true;
      // WebKit fallback for older WKWebView builds (frameless overlay runs in the OS webview).
      (el as any).webkitPreservesPitch = true;
      audioRef.current = el;
      el.addEventListener("ended", () => setSpeaking(false));
      setSpeaking(true);
      return el.play().catch((e) => {
        setSpeaking(false);
        throw e;
      });
    },
    [stop]
  );

  return { speaking, play, stop };
}
