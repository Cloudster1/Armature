import { describe, expect, it } from "vitest";
import { makeFormat, relativeIn } from "./format";

describe("format", () => {
  it("keeps the short relative forms and falls back to a date after a month", () => {
    const now = Date.parse("2026-09-05T12:00:00Z");
    expect(relativeIn("2026-09-05T11:59:30Z", "en-GB", "UTC", now)).toBe("just now");
    expect(relativeIn("2026-09-05T11:30:00Z", "en-GB", "UTC", now)).toBe("30m ago");
    expect(relativeIn("2026-09-05T09:00:00Z", "en-GB", "UTC", now)).toBe("3h ago");
    expect(relativeIn("2026-09-02T12:00:00Z", "en-GB", "UTC", now)).toBe("3d ago");
    expect(relativeIn("2026-06-01T12:00:00Z", "en-GB", "UTC", now)).toBe("1 Jun 2026");
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
