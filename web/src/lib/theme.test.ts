import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { applyTheme, readTheme } from "./theme";

beforeEach(() => {
  localStorage.clear();
  document.documentElement.removeAttribute("data-theme");
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("theme", () => {
  it("defaults to following the system", () => {
    expect(readTheme()).toBe("system");
  });

  it("round trips an explicit choice", () => {
    applyTheme("dark");
    expect(readTheme()).toBe("dark");
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");

    applyTheme("light");
    expect(readTheme()).toBe("light");
    expect(document.documentElement.getAttribute("data-theme")).toBe("light");
  });

  // "system" must remove the attribute rather than stamp a value, or the page
  // would be frozen in whichever theme was last active instead of following
  // the operating system.
  it("removes the attribute when following the system", () => {
    applyTheme("dark");
    applyTheme("system");
    expect(document.documentElement.hasAttribute("data-theme")).toBe(false);
    expect(readTheme()).toBe("system");
  });

  it("ignores a stored value it does not recognise", () => {
    localStorage.setItem("armature.theme", "chartreuse");
    expect(readTheme()).toBe("system");
  });

  it("falls back to the system default when storage cannot be read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("access denied");
    });
    expect(readTheme()).toBe("system");
  });

  // A private window throws on write. The theme should still apply to the page
  // even though it will not survive a reload.
  it("still applies a theme when storage cannot be written", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("access denied");
    });
    expect(() => applyTheme("dark")).not.toThrow();
    expect(document.documentElement.getAttribute("data-theme")).toBe("dark");
  });
});
