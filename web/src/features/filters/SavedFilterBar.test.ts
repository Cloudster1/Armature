import { describe, expect, it } from "vitest";
import { orderFilters } from "./SavedFilterBar";
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
