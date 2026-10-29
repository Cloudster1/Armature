import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { en } from "@/i18n/en";
import { de } from "@/i18n/de";
import { CURSOR_KINDS, TOKEN_GROUPS, TOKEN_NAMES } from "./theme-tokens";

// The editor offers exactly the tokens the stylesheet defines: a token added
// to one and not the other is a colour nobody can theme, or one that does
// nothing when themed.
describe("the token catalogue", () => {
  it("names every --color-* token in the stylesheet, and nothing else", () => {
    const css = readFileSync(join(__dirname, "..", "styles", "index.css"), "utf8");
    const declared = new Set([...css.matchAll(/--color-([a-z0-9-]+):/g)].map((m) => m[1]!));
    expect([...declared].sort()).toEqual([...TOKEN_NAMES].sort());
  });

  it("lists each token once", () => {
    expect(new Set(TOKEN_NAMES).size).toBe(TOKEN_NAMES.length);
  });
});

// The editor names every colour, group and pointer from the catalogues, never from this list.
describe("the words for the tokens", () => {
  it.each([
    ["English", en],
    ["German", de],
  ])("are all in the %s catalogue", (_language, catalogue) => {
    const words = catalogue.looks.editor;
    expect(TOKEN_NAMES.filter((name) => !words.tokens[name])).toEqual([]);
    expect(TOKEN_GROUPS.map((group) => group.id).filter((id) => !words.groups[id])).toEqual([]);
    expect(CURSOR_KINDS.map((cursor) => cursor.kind).filter((kind) => !words.cursors[kind])).toEqual([]);
  });
});
