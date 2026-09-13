import { describe, expect, it } from "vitest";
import { changeIsEmpty } from "./BulkBar";

describe("changeIsEmpty", () => {
  it("is empty until something is asked", () => {
    expect(changeIsEmpty({})).toBe(true);
    expect(changeIsEmpty({ transition: "  " })).toBe(true);
    expect(changeIsEmpty({ priority: "high" })).toBe(false);
    expect(changeIsEmpty({ assignee: null })).toBe(false);
    expect(changeIsEmpty({ addLabels: ["x"] })).toBe(false);
  });
});
