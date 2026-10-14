import { describe, expect, it } from "vitest";
import { chipFilters, orderFilters } from "./SavedFilterBar";
import type { SavedFilter } from "@/api/filters";

const f = (name: string, ownerId: string, starred: boolean): SavedFilter => ({
  id: name, ownerId, ownerName: "", name, query: "x", shared: true, columns: [], starred, createdAt: "", updatedAt: "",
});

describe("orderFilters", () => {
  it("puts starred first, then mine, then shared, by name", () => {
    const out = orderFilters([f("Zed", "other", false), f("Beta", "me", false), f("Alpha", "other", true), f("Gamma", "me", false)], "me");
    expect(out.map((x) => x.name)).toEqual(["Alpha", "Beta", "Gamma", "Zed"]);
  });
});

describe("chipFilters", () => {
  it("offers what I starred and what I made, and leaves out what others shared", () => {
    const out = chipFilters([f("Theirs", "other", false), f("Starred", "other", true), f("Mine", "me", false)], "me", null);
    expect(out.map((x) => x.name)).toEqual(["Starred", "Mine"]);
  });

  it("keeps the search that is running, even when somebody else shared it", () => {
    const theirs = f("Theirs", "other", false);
    const out = chipFilters([theirs, f("Others", "other", false)], "me", theirs.id);
    expect(out.map((x) => x.name)).toEqual(["Theirs"]);
  });
});
