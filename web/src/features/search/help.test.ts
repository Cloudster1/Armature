import { describe, expect, it } from "vitest";
import { QUERY_EXAMPLES, QUERY_FIELDS, QUERY_FUNCTIONS, caretLine } from "./help";

describe("query help", () => {
  it("shows every field with an example that uses it", () => {
    for (const field of QUERY_FIELDS) {
      expect(field.example.toLowerCase()).toContain(field.name.toLowerCase());
    }
    expect(new Set(QUERY_FIELDS.map((f) => f.name)).size).toBe(QUERY_FIELDS.length);
  });

  it("lists the functions as calls", () => {
    for (const fn of QUERY_FUNCTIONS) expect(fn.endsWith("()")).toBe(true);
    expect(QUERY_EXAMPLES.length).toBeGreaterThan(0);
  });

  it("puts the caret under the character named", () => {
    expect(caretLine(1)).toBe("^");
    expect(caretLine(5)).toBe("    ^");
    expect(caretLine(undefined)).toBe("");
    expect(caretLine(0)).toBe("");
  });
});
