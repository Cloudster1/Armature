import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// The kit and the stylesheet decide how things look, and config.ts names the values the rest reads; anything else
// reaches for a component or a token, so a size or a colour changes in one place. The same test as Exciter's and Stator's.
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name) && !name.endsWith(".d.ts")) out.push(path);
  }
  return out;
}

const root = join(__dirname, "..");
const decided = ["components/ui/", "styles/", "config.ts"];
const outsideKit = walk(root).filter((file) => !decided.some((prefix) => relative(root, file).startsWith(prefix)));

// What a comment says is no size or colour; the guard on // keeps the slashes of an address.
function code(file: string): string {
  return readFileSync(file, "utf8")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:"'`])\/\/.*$/gm, "$1");
}

/** Controls drawn by hand before this test, with why each is not the kit's; a count may only go down. */
const allowed: Record<string, { count: number; why: string }> = {
  "features/themes/ThemeEditor.tsx: \"#000000\"": { count: 1, why: "a colour field's placeholder shows the person the kind of value to type" },
  "features/themes/ThemeEditor.tsx: 1px": { count: 1, why: "the shadow field's placeholder shows the person a CSS shadow to type" },
  "features/themes/ThemeEditor.tsx: 2px": { count: 1, why: "the shadow field's placeholder shows the person a CSS shadow to type" },
  "features/themes/ThemeEditor.tsx: rgb(0": { count: 1, why: "the shadow field's placeholder shows the person a CSS shadow to type" },
  "lib/constellation.ts: \"#888\"": { count: 2, why: "a canvas cannot read a CSS variable, so a theme that leaves the token out is drawn grey" },
  "lib/constellation.ts: rgb(1": { count: 1, why: "a canvas cannot read a CSS variable, so a colour it cannot parse is drawn grey" },
};

function hits(pattern: RegExp): string[] {
  const found = outsideKit.flatMap((file) => (code(file).match(pattern) ?? []).map((m) => `${relative(root, file)}: ${m}`));
  return found.filter((hit, index) => {
    const allowance = allowed[hit];
    return !allowance || found.slice(0, index + 1).filter((each) => each === hit).length > allowance.count;
  });
}

describe("the kit", () => {
  it("is where every size is decided", () => {
    expect(hits(/\b[\w-]+-\[-?[\d.]+(?:px|rem)\]/g)).toEqual([]);
    expect(hits(/(?<![\w$.-])\d+(?:\.\d+)?px\b/g)).toEqual([]);
  });

  it("is where every colour is decided, the rest naming tokens", () => {
    expect(hits(/["'`]#[0-9a-fA-F]{3,8}["'`]/g)).toEqual([]);
    expect(hits(/-\[#[0-9a-fA-F]{3,8}\]/g)).toEqual([]);
    expect(hits(/\b(?:rgba?|hsla?|oklch|oklab)\(\s*\d/g)).toEqual([]);
  });

  it("is where every button is drawn", () => {
    expect(hits(/<button\b/g)).toEqual([]);
  });

  // A file input is hidden and pressed through a button, so it is not a drawn control.
  it("is where every input, select and textarea is drawn", () => {
    expect(hits(/<(select|textarea)\b|<input\b(?![^>]*type="file")/g)).toEqual([]);
  });

  it("owns the accent's name", () => {
    expect(hits(/\b(bg|text|border|ring|fill|stroke|border-[tlrb])-(on-)?brand\b/g)).toEqual([]);
  });

  it("keeps its exceptions to controls that are still there", () => {
    const counts = Object.keys(allowed).map((hit) => {
      const [file, pattern] = hit.split(": ");
      return code(join(root, file!)).split(pattern!).length - 1;
    });
    expect(counts).toEqual(Object.values(allowed).map((each) => each.count));
  });
});
