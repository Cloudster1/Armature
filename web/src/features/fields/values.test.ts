import { describe, expect, it } from "vitest";
import { badInputMessage, inputFromValue, inputTypeFor, parseOptions, valueFromInput } from "./values";

describe("valueFromInput", () => {
  it("clears on an empty box rather than storing an empty string", () => {
    expect(valueFromInput("text", "   ")).toBeNull();
    expect(valueFromInput("number", "")).toBeNull();
  });

  it("sends numbers as numbers and leaves the rest to the API", () => {
    expect(valueFromInput("number", "12.5")).toBe(12.5);
    expect(valueFromInput("number", "twelve")).toBe("twelve");
  });

  it("trims text", () => {
    expect(valueFromInput("text", "  Acme ")).toBe("Acme");
  });

  it("turns a checkbox into a boolean", () => {
    expect(valueFromInput("checkbox", true)).toBe(true);
    expect(valueFromInput("checkbox", "")).toBe(false);
  });
});

describe("inputFromValue", () => {
  it("draws nothing for an unanswered field", () => {
    expect(inputFromValue("text", undefined)).toBe("");
    expect(inputFromValue("checkbox", undefined)).toBe("");
  });

  it("round trips a stored value", () => {
    expect(inputFromValue("number", 7)).toBe("7");
    expect(inputFromValue("date", "2026-03-01")).toBe("2026-03-01");
    expect(inputFromValue("checkbox", true)).toBe("true");
  });
});

describe("inputTypeFor", () => {
  it("picks the browser control that fits", () => {
    expect(inputTypeFor("date")).toBe("date");
    expect(inputTypeFor("url")).toBe("url");
    expect(inputTypeFor("number")).toBe("number");
    expect(inputTypeFor("text")).toBe("text");
  });
});

describe("parseOptions", () => {
  it("takes one option per line and drops blanks and repeats", () => {
    expect(parseOptions("Web\n\n Mobile \nWeb")).toEqual(["Web", "Mobile"]);
  });
});

describe("badInputMessage", () => {
  it("names the field and what it takes", () => {
    expect(badInputMessage("number", "Cost")).toBe("Cost takes a number, such as 12 or 2.5.");
    expect(badInputMessage("date", "Due")).toBe("Due takes a date such as 2026-01-31.");
  });
});
