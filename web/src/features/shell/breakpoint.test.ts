import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import { SIDEBAR_DRAWER_BELOW_PX } from "@/config";

describe("the drawer's breakpoint", () => {
  it("is the same width in the stylesheet and in config.ts", () => {
    const css = readFileSync(resolve(process.cwd(), "src/styles/index.css"), "utf8");
    const rem = css.match(/--breakpoint-shell:\s*([\d.]+)rem;/)?.[1];
    // A rem in a media query is always the browser's 16px.
    expect(Number(rem) * 16).toBe(SIDEBAR_DRAWER_BELOW_PX);
  });
});
