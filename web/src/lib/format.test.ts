import { describe, expect, it } from "vitest";
import { atInstantIn } from "@/test/zone";
import { applyLanguage } from "@/i18n";
import { endOfDayIn, makeFormat, relativeIn, todayIn } from "./format";

describe("format", () => {
  it("keeps the short relative forms and falls back to a date after a month", () => {
    const now = Date.parse("2026-09-05T12:00:00Z");
    expect(relativeIn("2026-09-05T11:59:30Z", "en-GB", "UTC", now)).toBe("just now");
    expect(relativeIn("2026-09-05T11:30:00Z", "en-GB", "UTC", now)).toBe("30m ago");
    expect(relativeIn("2026-09-05T09:00:00Z", "en-GB", "UTC", now)).toBe("3h ago");
    expect(relativeIn("2026-09-02T12:00:00Z", "en-GB", "UTC", now)).toBe("3d ago");
    expect(relativeIn("2026-06-01T12:00:00Z", "en-GB", "UTC", now)).toBe("1 Jun 2026");
  });

  it("says the relative forms in the language shown", () => {
    const now = Date.parse("2026-09-05T12:00:00Z");
    applyLanguage("de");
    try {
      expect(relativeIn("2026-09-05T11:59:30Z", "de-DE", "UTC", now)).toBe("gerade eben");
      expect(relativeIn("2026-09-05T09:00:00Z", "de-DE", "UTC", now)).toBe("vor 3 Std.");
      expect(relativeIn("2026-09-02T12:00:00Z", "de-DE", "UTC", now)).toBe("vor 3 T.");
    } finally {
      applyLanguage("en");
    }
    expect(relativeIn("2026-09-05T09:00:00Z", "en-GB", "UTC", now)).toBe("3h ago");
  });

  it("writes dates in the reader's zone and language", () => {
    const berlin = makeFormat("de-DE", "Europe/Berlin");
    expect(berlin.dateTime("2026-09-05T12:00:00Z")).toBe("05.09.2026, 14:00");
    const york = makeFormat("en-US", "America/New_York");
    expect(york.time("2026-09-05T12:00:00Z")).toBe("8:00 AM");
  });

  it("survives a zone the browser does not know", () => {
    expect(() => makeFormat("en-GB", "Mars/Olympus").date("2026-09-05T12:00:00Z")).not.toThrow();
  });
});

describe("today and the end of a day, where the profile says the reader is", () => {
  it("is the profile's day, not the browser's or UTC's", () => {
    // 01:00 in Berlin is still yesterday in UTC and in Honolulu.
    atInstantIn("Pacific/Honolulu", "2026-10-09T23:00:00Z");
    expect(todayIn("Europe/Berlin")).toBe("2026-10-10");
    // 18:00 in Los Angeles is already tomorrow in UTC and in Tokyo.
    atInstantIn("Asia/Tokyo", "2026-10-10T01:00:00Z");
    expect(todayIn("America/Los_Angeles")).toBe("2026-10-09");
  });

  it("ends a day at its last second in the profile's zone", () => {
    atInstantIn("Pacific/Honolulu", "2026-10-01T12:00:00Z");
    expect(endOfDayIn("2026-10-10", "Europe/Berlin")).toBe("2026-10-10T21:59:59Z");
    expect(endOfDayIn("2026-10-10", "America/Los_Angeles")).toBe("2026-10-11T06:59:59Z");
    expect(endOfDayIn("2026-10-10", "UTC")).toBe("2026-10-10T23:59:59Z");
  });

  it("ends a day the clocks change on at the offset that holds by its evening", () => {
    expect(endOfDayIn("2026-10-25", "Europe/Berlin")).toBe("2026-10-25T22:59:59Z");
    expect(endOfDayIn("2026-03-08", "America/Los_Angeles")).toBe("2026-03-09T06:59:59Z");
  });

  it("falls back to the browser's zone for one it does not know", () => {
    atInstantIn("Asia/Tokyo", "2026-10-10T01:00:00Z");
    expect(todayIn("Mars/Olympus")).toBe("2026-10-10");
    expect(endOfDayIn("2026-10-10", "Mars/Olympus")).toBe("2026-10-10T14:59:59Z");
  });
});
