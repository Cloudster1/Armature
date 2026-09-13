import { describe, expect, it } from "vitest";
import { safePath } from "./path";

describe("safePath", () => {
  // A mail carries where to go next. A browser reads a backslash as a slash,
  // so "/\evil.com" would leave the application without looking like it.
  it("keeps a path of this application and refuses anywhere else", () => {
    expect(safePath("/portal/requests/ABC-1")).toBe("/portal/requests/ABC-1");
    expect(safePath("/search?q=bug#top")).toBe("/search?q=bug#top");
    expect(safePath("/\\evil.com")).toBeUndefined();
    expect(safePath("//evil.com")).toBeUndefined();
    expect(safePath("https://evil.com")).toBeUndefined();
    expect(safePath("/ok\nBcc:")).toBeUndefined();
    expect(safePath("")).toBeUndefined();
    expect(safePath(undefined)).toBeUndefined();
    expect(safePath(42)).toBeUndefined();
  });
});
