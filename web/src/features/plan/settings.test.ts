import { beforeEach, describe, expect, it } from "vitest";
import { PLAN_DEFAULT_CLOSED_FOR_DAYS } from "@/config";
import { readClosedForDays, writeClosedForDays } from "./settings";

describe("closed-for days", () => {
  beforeEach(() => localStorage.clear());

  it("starts at the default", () => {
    expect(readClosedForDays()).toBe(PLAN_DEFAULT_CLOSED_FOR_DAYS);
  });

  it("remembers a number, whole and never negative", () => {
    writeClosedForDays(30);
    expect(readClosedForDays()).toBe(30);
    writeClosedForDays(-3.7);
    expect(readClosedForDays()).toBe(0);
  });

  it("remembers that everything is wanted, which is not the default", () => {
    writeClosedForDays(null);
    expect(readClosedForDays()).toBeNull();
  });

  it("ignores something that is not a number", () => {
    localStorage.setItem("armature.plan.closedForDays", "soon");
    expect(readClosedForDays()).toBe(PLAN_DEFAULT_CLOSED_FOR_DAYS);
  });
});
