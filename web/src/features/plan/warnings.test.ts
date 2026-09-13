import { describe, expect, it } from "vitest";
import type { PlanWarning } from "@/api/plan";
import { severityOf, wordsOf, worstOf } from "./warnings";

const warn = (kind: PlanWarning["kind"], message: string = kind): PlanWarning => ({ issueKey: "P-1", kind, message });

describe("plan warnings", () => {
  it("calls a contradiction an error and an omission a warning", () => {
    expect(severityOf("blocked-too-early")).toBe("error");
    expect(severityOf("outside-parent")).toBe("error");
    expect(severityOf("past-milestone")).toBe("error");
    expect(severityOf("half-scheduled")).toBe("warning");
    expect(severityOf("outside-sprint")).toBe("warning");
  });

  it("dresses a row in its worst warning", () => {
    expect(worstOf([])).toBeNull();
    expect(worstOf([warn("half-scheduled")])).toBe("warning");
    expect(worstOf([warn("half-scheduled"), warn("outside-parent")])).toBe("error");
  });

  it("puts the words one per line", () => {
    expect(wordsOf([warn("half-scheduled", "one end"), warn("outside-parent", "outside")])).toBe("one end\noutside");
  });
});
