// theme.ts — light/dark theme preference, persisted in localStorage and mirrored
// onto <html data-theme> (which drives the :root / :root[data-theme="dark"] token
// blocks in style.css). Frontend-only, no Go call: a synchronous read on boot
// (see index.html) applies the theme before first paint, so this module owns only
// runtime changes. The "system" preference follows the OS via prefers-color-scheme.

export type ThemePref = "system" | "light" | "dark";
export type Theme = "light" | "dark";

const STORAGE_KEY = "mogi-theme";

// osTheme reads the current OS color scheme.
function osTheme(): Theme {
  return typeof window !== "undefined" &&
    window.matchMedia("(prefers-color-scheme: dark)").matches
    ? "dark"
    : "light";
}

// getThemePref returns the stored preference, or "system" when none is set
// (kept in sync with the boot script's `null === system` convention).
export function getThemePref(): ThemePref {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v === "light" || v === "dark") return v;
  } catch {
    // localStorage unavailable (private mode / preview) — treat as "system".
  }
  return "system";
}

// getEffectiveTheme resolves a preference to a concrete theme, following the OS
// when the preference is "system".
export function getEffectiveTheme(pref: ThemePref = getThemePref()): Theme {
  return pref === "system" ? osTheme() : pref;
}

// applyTheme writes the effective theme onto <html data-theme>.
export function applyTheme(pref: ThemePref = getThemePref()): void {
  document.documentElement.setAttribute("data-theme", getEffectiveTheme(pref));
}

// setThemePref persists a preference (clearing storage for "system" so future
// boots keep following the OS) and applies it to the document immediately.
export function setThemePref(pref: ThemePref): void {
  try {
    if (pref === "system") localStorage.removeItem(STORAGE_KEY);
    else localStorage.setItem(STORAGE_KEY, pref);
  } catch {
    // Non-fatal — the attribute is still applied for this session.
  }
  applyTheme(pref);
}

// subscribeSystemTheme re-applies the theme when the OS scheme changes, but only
// while the preference is "system". Returns an unsubscribe function.
export function subscribeSystemTheme(onChange: () => void): () => void {
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const handler = () => {
    if (getThemePref() === "system") {
      applyTheme("system");
      onChange();
    }
  };
  mq.addEventListener("change", handler);
  return () => mq.removeEventListener("change", handler);
}
