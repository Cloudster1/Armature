import { describe, expect, it } from "vitest";
import { describeRule } from "./describe";

const catalog = [
  { kind: "trigger" as const, type: "issue.transitioned", label: "An issue moves", description: "", options: [] },
  { kind: "trigger" as const, type: "issue.updated", label: "A field changes", description: "", options: [] },
];

describe("describeRule", () => {
  it("reads a rule as one sentence", () => {
    expect(
      describeRule(
        {
          trigger: { kind: "issue.transitioned" },
          conditions: [{ kind: "field_equals", field: "priority", value: "high" }],
          actions: [{ kind: "add_label", value: "hot" }, { kind: "send_webhook" }],
        },
        catalog,
      ),
    ).toBe("When an issue moves, if priority is high, then add the label hot, then send a webhook.");
  });
  it("names the field an update watches and a schedule's rhythm", () => {
    expect(describeRule({ trigger: { kind: "issue.updated", field: "assignee" }, conditions: [], actions: [{ kind: "add_comment" }] }, catalog)).toBe(
      "When assignee changes, then add a comment.",
    );
    expect(
      describeRule({ trigger: { kind: "scheduled", query: "due < now()", schedule: { unit: "hours", every: 2 } }, conditions: [], actions: [{ kind: "assign", value: "reporter" }] }, catalog),
    ).toBe("When every 2 hours, over due < now(), then assign to reporter.");
  });
});
