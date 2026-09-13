import { describe, expect, it } from "vitest";
import { cleanDomain, defaultCalendar, describeCalendar } from "./DeskExtras";

describe("business hours", () => {
  it("start as nine to five on weekdays", () => {
    const c = defaultCalendar("Europe/Berlin");
    expect(Object.keys(c.hours)).toEqual(["mon", "tue", "wed", "thu", "fri"]);
    expect(describeCalendar(c)).toBe("Mon to Fri 09:00 to 17:00 (Europe/Berlin)");
  });
  it("say when nothing is open, and when days differ", () => {
    expect(describeCalendar({ timezone: "UTC", hours: {}, holidays: [] })).toBe("never open");
    expect(describeCalendar({ timezone: "UTC", hours: { mon: [{ from: "08:00", to: "12:00" }], wed: [{ from: "09:00", to: "17:00" }] }, holidays: [] })).toBe("Mon, Wed varied (UTC)");
  });
});

describe("a trusted domain", () => {
  it("is written the way the server keeps it", () => {
    expect(cleanDomain(" @Acme.TEST ")).toBe("acme.test");
    expect(cleanDomain("")).toBe("");
  });
});
