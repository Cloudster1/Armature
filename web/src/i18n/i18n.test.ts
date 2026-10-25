import { afterEach, describe, expect, it } from "vitest";
import { LANGUAGE_STORAGE_KEY } from "@/config";
import { de } from "./de";
import { en } from "./en";
import { applyLanguage, browserLanguage, language, languageOfLocale, resolveLanguage, t } from "./index";

afterEach(() => {
  applyLanguage("");
  localStorage.clear();
});

type Leaf = { path: string; kind: string; arity?: number };

function leaves(value: unknown, path = ""): Leaf[] {
  if (typeof value === "function") return [{ path, kind: "function", arity: value.length }];
  if (Array.isArray(value)) return [{ path, kind: `array of ${value.length}` }, ...value.flatMap((each, i) => leaves(each, `${path}[${i}]`))];
  if (value && typeof value === "object")
    return Object.keys(value).flatMap((key) => leaves((value as Record<string, unknown>)[key], path ? `${path}.${key}` : key));
  return [{ path, kind: typeof value }];
}

/** Each string a catalogue holds, by its path; a function is called with stand-ins so its sentence is compared too. */
function strings(value: unknown, path = "", args: unknown[] = ["X", "Y", "Z"]): Array<[string, string]> {
  if (typeof value === "string") return [[path, value]];
  if (typeof value === "function") {
    try {
      const out = (value as (...a: unknown[]) => unknown)(...args);
      return typeof out === "string" ? [[path, out]] : [];
    } catch {
      return [];
    }
  }
  if (value && typeof value === "object") return Object.entries(value).flatMap(([key, each]) => strings(each, path ? `${path}.${key}` : key, args));
  return [];
}

// Names, product words and units read the same in both languages; any other
// German string equal to its English one is an untranslated line.
const SAME_IN_BOTH = new Set([
  "Navigation",
  "Installation",
  "Workflows",
  "Workflow",
  "Webhooks",
  "Board",
  "Sprints",
  "Plan",
  "Dashboard",
  "Teams",
  "Import",
  "Name",
  "Backlog",
  "Zoom",
  "Team",
  // A pattern with only its arguments in it, such as "month year".
  "X Y",
  "X / Y",
]);

describe("the German catalogue", () => {
  it("has every key, of the same kind, that the English one has, and no other", () => {
    expect(leaves(de)).toEqual(leaves(en));
  });

  it("is German: a string equals its English one only when it is a listed name", () => {
    const english = new Map(strings(en));
    const copied = strings(de).filter(([path, text]) => english.get(path) === text && !SAME_IN_BOTH.has(text));
    expect(copied).toEqual([]);
  });

  it("keeps to the house typography in both languages", () => {
    for (const [path, each] of [...strings(en), ...strings(de), ...strings(de, "", [1, 1, 1]), ...strings(de, "", [2, 5, 9])]) {
      expect(each, path).not.toMatch(/[\u2013\u2014\u201c\u201d\u201e\u2018\u2019\u201a\u00ab\u00bb\u2026]/);
    }
  });

  it("says everyone, never organization, in both languages", () => {
    for (const [path, each] of [...strings(en), ...strings(de)]) expect(each, path).not.toMatch(/organi[sz]ation|organisation/i);
  });
});

describe("choosing the language", () => {
  it("reads the language a profile's locale asks for, and follows the browser for any other", () => {
    expect(languageOfLocale("de-DE")).toBe("de");
    expect(languageOfLocale("en-GB")).toBe("en");
    expect(languageOfLocale("fr-FR")).toBe("");
    expect(languageOfLocale("")).toBe("");
    expect(languageOfLocale(undefined)).toBe("");
  });

  it("takes the first of the browser's languages it speaks, else English", () => {
    expect(browserLanguage(["de-AT", "en-US"])).toBe("de");
    expect(browserLanguage(["fr-FR", "de-CH", "en"])).toBe("de");
    expect(browserLanguage(["fr-FR", "es"])).toBe("en");
    expect(browserLanguage([])).toBe("en");
  });

  it("puts the person's choice before the browser's", () => {
    expect(resolveLanguage("en", ["de-DE"])).toBe("en");
    expect(resolveLanguage("de", ["en-US"])).toBe("de");
    expect(resolveLanguage("", ["de-DE"])).toBe("de");
    expect(resolveLanguage("fr", ["en-GB"])).toBe("en");
  });

  it("switches the strings, the page's language and the remembered choice together", () => {
    expect(t).toBe(en);
    expect(applyLanguage("de")).toBe("de");
    expect(language()).toBe("de");
    expect(t).toBe(de);
    expect(t.common.close).toBe("Schließen");
    expect(document.documentElement.lang).toBe("de");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBe("de");

    applyLanguage("");
    expect(t).toBe(en);
    expect(document.documentElement.lang).toBe("en");
    expect(localStorage.getItem(LANGUAGE_STORAGE_KEY)).toBeNull();
  });
});
