import { describe, expect, it } from "vitest";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";

// The kit is the only place a control is typed by hand. Anything else reaches
// for a component, so a change to how a button looks is a change in one file.
function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) walk(path, out);
    else if (/\.tsx?$/.test(name) && !/\.test\.tsx?$/.test(name)) out.push(path);
  }
  return out;
}

const root = join(__dirname, "..");
const outsideKit = walk(root).filter((file) => !relative(root, file).startsWith("components/ui/"));

function hits(pattern: RegExp): string[] {
  return outsideKit.flatMap((file) => (readFileSync(file, "utf8").match(pattern) ?? []).map((m) => `${relative(root, file)}: ${m}`));
}

describe("the kit", () => {
  it("is where every size is decided", () => {
    expect(hits(/text-\[\d+px\]/g)).toEqual([]);
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
});
