import { describe, expect, it } from "vitest";
import { formatDuration, parseDuration, timeProgress } from "./duration";

describe("parseDuration", () => {
  it("reads the units people write", () => {
    expect(parseDuration("2h 30m")).toBe(150);
    expect(parseDuration("1d")).toBe(480);
    expect(parseDuration("45m")).toBe(45);
    expect(parseDuration("1.5h")).toBe(90);
    expect(parseDuration("1d 2h 15m")).toBe(615);
    expect(parseDuration("2H")).toBe(120);
  });

  it("takes a bare number as hours", () => {
    expect(parseDuration("2")).toBe(120);
    expect(parseDuration("0.5")).toBe(30);
  });

  it("treats an empty box as nothing and nonsense as not time", () => {
    expect(parseDuration("")).toBe(0);
    expect(parseDuration("   ")).toBe(0);
    expect(parseDuration("soon")).toBeNull();
    expect(parseDuration("2h and a bit")).toBeNull();
    expect(parseDuration("3 weeks")).toBeNull();
  });
});

describe("formatDuration", () => {
  it("writes minutes the way the changelog does", () => {
    expect(formatDuration(150)).toBe("2h 30m");
    expect(formatDuration(480)).toBe("1d");
    expect(formatDuration(545)).toBe("1d 1h 5m");
    expect(formatDuration(0)).toBe("0m");
    expect(formatDuration(undefined)).toBe("");
  });

  it("round trips what it parses", () => {
    for (const text of ["2h 30m", "1d 1h", "45m", "3d"]) {
      expect(formatDuration(parseDuration(text)!)).toBe(text);
    }
  });
});

describe("timeProgress", () => {
  it("measures spent against what is left when that is known", () => {
    expect(timeProgress(90, 390, 480)).toBeCloseTo(90 / 480);
    expect(timeProgress(150, 30, 100)).toBeCloseTo(150 / 180);
  });

  it("falls back to the estimate, and shows full when over with nothing left", () => {
    expect(timeProgress(60, undefined, 120)).toBe(0.5);
    expect(timeProgress(60, 0, undefined)).toBe(1);
    expect(timeProgress(0, undefined, undefined)).toBe(0);
  });
});
