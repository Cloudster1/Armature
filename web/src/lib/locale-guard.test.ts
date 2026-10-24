import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// Dates and numbers are written in the reader's profile language. Only
// lib/format.ts may fall back to the browser's, so nothing else chooses one.
const refused: { why: string; pattern: RegExp }[] = [
  { why: "the browser's language", pattern: /\.toLocale(?:Date|Time)?String\(\s*(?:\)|undefined\b)/g },
  { why: "the browser's language", pattern: /\bIntl\.\w+\(\s*undefined\b/g },
  { why: "the browser's language", pattern: /\bIntl\.\w+\(\s*\)(?!\.resolvedOptions\(\))/g },
  { why: "the browser's language", pattern: /\bnavigator\.languages?\b/g },
  { why: "a fixed language", pattern: /\.toLocale(?:Date|Time)?String\(\s*["'`]/g },
  { why: "a fixed language", pattern: /\bIntl\.\w+\(\s*["'`]/g },
];

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && !name.endsWith(".d.ts")) out.push(path);
  }
  return out;
}

function offences(source: string): string[] {
  return refused.flatMap(({ why, pattern }) => (source.match(pattern) ?? []).map((m) => `${m} (${why})`));
}

const root = join(__dirname, "..");
// The interface reads the browser's languages only to choose which catalogue it speaks.
const ALLOWED = new Set(["lib/format.ts", "i18n/index.ts"]);

describe("the reader's language", () => {
  it("is the only one a date or a number is written in outside lib/format.ts", () => {
    const found = walk(root)
      .filter((file) => !ALLOWED.has(relative(root, file)))
      .flatMap((file) => offences(readFileSync(file, "utf8")).map((o) => `${relative(root, file)}: ${o}`));
    expect(found.join("\n")).toBe("");
  });

  it("is guarded against every shape that picks another", () => {
    for (const line of [
      `new Date(iso).toLocaleDateString(undefined, { month: "long" })`,
      `new Date(iso).toLocaleDateString()`,
      `new Date(iso).toLocaleString()`,
      `new Date(iso).toLocaleTimeString(undefined, { hour: "2-digit" })`,
      `new Date(iso).toLocaleDateString("en-GB", { day: "numeric" })`,
      `new Intl.DateTimeFormat("de-DE").format(d)`,
      `new Intl.NumberFormat(undefined).format(n)`,
      `new Intl.DateTimeFormat().format(d)`,
      `relativeIn(iso, navigator.language, zone)`,
    ]) {
      expect(offences(line), line).not.toEqual([]);
    }
    for (const line of [
      `new Intl.DateTimeFormat(locale, { dateStyle: "medium" })`,
      `Intl.DateTimeFormat().resolvedOptions().timeZone`,
      `value.toLocaleUpperCase()`,
    ]) {
      expect(offences(line), line).toEqual([]);
    }
  });
});
