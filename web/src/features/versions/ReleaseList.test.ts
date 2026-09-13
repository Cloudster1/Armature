import { describe, expect, it } from "vitest";
import { inView, notesAsText } from "./ReleaseList";

describe("the releases page", () => {
  it("sorts versions into the three views", () => {
    const planned = {};
    const shipped = { releasedAt: "2026-09-01T00:00:00Z" };
    const gone = { releasedAt: "2026-09-01T00:00:00Z", archivedAt: "2026-09-02T00:00:00Z" };
    expect([planned, shipped, gone].filter((v) => inView(v, "unreleased"))).toEqual([planned]);
    expect([planned, shipped, gone].filter((v) => inView(v, "released"))).toEqual([shipped]);
    expect([planned, shipped, gone].filter((v) => inView(v, "archived"))).toEqual([gone]);
  });
  it("writes notes somebody can paste", () => {
    expect(notesAsText([{ type: "Bug", issues: [{ key: "CP-1", summary: "Fix the door" }] }], "1.0")).toBe("Release 1.0\n\nBug\n- CP-1 Fix the door\n");
  });
});
