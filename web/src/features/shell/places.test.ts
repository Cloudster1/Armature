import { describe, expect, it } from "vitest";
import { ADMIN_SECTIONS, adminFor, adminPlaces, inSettingsArea, personalPlaces } from "./places";

describe("the admin places", () => {
  it("file every place under a known section, and every section has one", () => {
    for (const place of adminPlaces) expect(ADMIN_SECTIONS, place.label).toContain(place.section);
    for (const section of ADMIN_SECTIONS)
      expect(
        adminPlaces.some((place) => place.section === section),
        section,
      ).toBe(true);
  });

  it("all live under /admin and are offered to administrators only", () => {
    expect(adminPlaces.every((place) => place.to.startsWith("/admin/"))).toBe(true);
    expect(adminFor(true)).toBe(adminPlaces);
    expect(adminFor(false)).toEqual([]);
  });
});

describe("the settings area", () => {
  it("holds the person's own pages and not the Admin page", () => {
    expect(inSettingsArea("/settings/profile")).toBe(true);
    expect(inSettingsArea("/admin/labels")).toBe(false);
    for (const place of personalPlaces) expect(place.to.startsWith("/admin")).toBe(false);
  });
});
