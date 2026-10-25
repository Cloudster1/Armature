import { useSyncExternalStore } from "react";
import { LANGUAGE_STORAGE_KEY } from "@/config";
import { de } from "./de";
import { en, type Messages } from "./en";

export type { Messages };

/** A language the interface speaks. */
export type Language = "en" | "de";

/** Every language, in the order a choice lists them. */
export const LANGUAGES: readonly Language[] = ["en", "de"];

/** What the person chose; an empty choice follows the browser. */
export type LanguageChoice = Language | "";

const catalogues: Record<Language, Messages> = { en, de };

/** Whether a value names a language the interface speaks. */
export function isLanguage(value: unknown): value is Language {
  return typeof value === "string" && (LANGUAGES as readonly string[]).includes(value);
}

/**
 * The interface language a profile's locale asks for. Armature keeps a locale
 * such as de-DE, for dates as well; one whose language it does not speak follows the browser.
 */
export function languageOfLocale(locale: string | null | undefined): LanguageChoice {
  const primary = (locale ?? "").toLowerCase().split("-")[0];
  return isLanguage(primary) ? primary : "";
}

/** The first of the browser's languages the interface speaks, else English. */
export function browserLanguage(preferred: readonly string[] = browserPreferences()): Language {
  for (const tag of preferred) {
    const primary = tag.toLowerCase().split("-")[0];
    if (isLanguage(primary)) return primary;
  }
  return "en";
}

/** The language to show: the person's choice, else the browser's. */
export function resolveLanguage(choice: string | null | undefined, preferred?: readonly string[]): Language {
  return isLanguage(choice) ? choice : browserLanguage(preferred);
}

function browserPreferences(): readonly string[] {
  if (typeof navigator === "undefined") return [];
  return navigator.languages?.length ? navigator.languages : [navigator.language];
}

// The profile's choice is remembered in the browser too, so a reload paints
// in the right language before the server has answered.
function readCachedChoice(): LanguageChoice {
  try {
    const value = localStorage.getItem(LANGUAGE_STORAGE_KEY);
    return isLanguage(value) ? value : "";
  } catch {
    return "";
  }
}

function writeCachedChoice(choice: LanguageChoice): void {
  try {
    if (choice) localStorage.setItem(LANGUAGE_STORAGE_KEY, choice);
    else localStorage.removeItem(LANGUAGE_STORAGE_KEY);
  } catch {
    // A choice the browser will not keep is still right for this page.
  }
}

let current: Language = resolveLanguage(readCachedChoice());

/**
 * The strings the interface shows, in the active language. A live binding:
 * read it while rendering, never keep a piece of it at module level.
 */
export let t: Messages = catalogues[current];

const listeners = new Set<() => void>();

function stampDocument(language: Language): void {
  if (typeof document !== "undefined") document.documentElement.lang = language;
}

stampDocument(current);

/** The active language. */
export function language(): Language {
  return current;
}

/** Switches the interface to a language, and remembers a person's choice for the next load. */
export function applyLanguage(choice: LanguageChoice, preferred?: readonly string[]): Language {
  writeCachedChoice(choice);
  const next = resolveLanguage(choice, preferred);
  if (next !== current) {
    current = next;
    t = catalogues[next];
    stampDocument(next);
    for (const listener of listeners) listener();
  }
  return next;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

/** The active language, re-rendering the caller when it changes. */
export function useLanguage(): Language {
  return useSyncExternalStore(subscribe, language, language);
}
