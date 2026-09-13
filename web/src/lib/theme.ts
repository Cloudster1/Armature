export type Theme = "light" | "dark" | "system";

const STORAGE_KEY = "armature.theme";

/** Reads the stored preference, tolerating browsers that block site storage. */
export function readTheme(): Theme {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === "light" || value === "dark" || value === "system") return value;
  } catch {
    // Private windows and locked-down browsers throw on access; the system
    // default is a perfectly good answer.
  }
  return "system";
}

/**
 * Applies a theme by stamping the root element. "system" removes the attribute
 * entirely so the stylesheet's prefers-color-scheme rules take over, rather
 * than freezing whichever theme happened to be active.
 */
export function applyTheme(theme: Theme): void {
  const root = document.documentElement;
  if (theme === "system") {
    root.removeAttribute("data-theme");
  } else {
    root.setAttribute("data-theme", theme);
  }
  try {
    localStorage.setItem(STORAGE_KEY, theme);
  } catch {
    // A theme that does not persist is still a theme.
  }
}
