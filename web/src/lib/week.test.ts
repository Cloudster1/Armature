import { describe, expect, it } from "vitest";
import { formatDay, hoursText, minutesFromHours, weekTotal, weekdayName } from "./week";

describe("a working week written in hours", () => {
  it("reads minutes as the hours a person would type", () => {
    expect(hoursText(480)).toBe("8");
    expect(hoursText(450)).toBe("7.5");
    expect(hoursText(225)).toBe("3.75");
    expect(hoursText(0)).toBe("0");
  });

  it("takes typed hours back as minutes, and refuses what a day cannot hold", () => {
    expect(minutesFromHours("8")).toBe(480);
    expect(minutesFromHours(" 7,5 ")).toBe(450);
    expect(minutesFromHours("")).toBe(0);
    expect(minutesFromHours("24")).toBe(1440);
    expect(minutesFromHours("25")).toBeNull();
    expect(minutesFromHours("-1")).toBeNull();
    expect(minutesFromHours("eight")).toBeNull();
  });

  it("adds a week up, a day left out being none", () => {
    expect(weekTotal({ mon: 480, tue: 480, wed: 240 })).toBe(1200);
  });

  it("names weekdays in the reader's language, Monday first", () => {
    expect(weekdayName("mon", "en-GB")).toBe("Monday");
    expect(weekdayName("sun", "en-GB", "short")).toBe("Sun");
    expect(weekdayName("fri", "de-DE")).toBe("Freitag");
  });

  it("writes a holiday as the date it is, whatever the zone", () => {
    expect(formatDay("2026-12-25", "en-GB")).toBe("25 Dec 2026");
    expect(formatDay("not a day", "en-GB")).toBe("not a day");
  });
});
