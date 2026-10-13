import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { BULK_MAX_SELECTION } from "@/config";

vi.mock("@/api/filters", async () => ({
  ...(await vi.importActual<typeof import("@/api/filters")>("@/api/filters")),
  useBulkEdit: () => ({ mutate: vi.fn(), isPending: false, error: null }),
}));

vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useMembers: () => ({ data: { members: [] } }),
}));

const { BulkBar, changeIsEmpty } = await import("./BulkBar");
const { keysQuery } = await import("./ExportDialog");

/** The query a download link asks the export for. */
function exportedQuery(): string | null {
  const href = document.querySelector('[data-action="download-csv"]')?.getAttribute("href") ?? "";
  return new URL(href, "http://armature.test").searchParams.get("q");
}

describe("changeIsEmpty", () => {
  it("is empty until something is asked", () => {
    expect(changeIsEmpty({})).toBe(true);
    expect(changeIsEmpty({ transition: "  " })).toBe(true);
    expect(changeIsEmpty({ priority: "high" })).toBe(false);
    expect(changeIsEmpty({ assignee: null })).toBe(false);
    expect(changeIsEmpty({ addLabels: ["x"] })).toBe(false);
  });
});

describe("keysQuery", () => {
  it("names one issue with = and several with in", () => {
    expect(keysQuery(["CP-5"])).toBe("key = CP-5");
    expect(keysQuery(["CP-1", "OPS-2"])).toBe("key in (CP-1, OPS-2)");
  });
});

describe("BulkBar", () => {
  it("exports the ticked rows before any change is chosen", async () => {
    render(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    expect(screen.getByRole("button", { name: /Apply to 2/ })).toBeDisabled();
    const exporting = screen.getByRole("button", { name: "Export 2 selected" });
    expect(exporting).toBeEnabled();
    await userEvent.click(exporting);
    expect(exportedQuery()).toBe("key in (CP-1, CP-2)");
  });

  it("exports more rows than one change may touch", async () => {
    const keys = Array.from({ length: BULK_MAX_SELECTION + 1 }, (_, i) => `CP-${i + 1}`);
    render(<BulkBar keys={keys} onClear={() => {}} />);
    const exporting = screen.getByRole("button", { name: `Export ${keys.length} selected` });
    expect(exporting).toBeEnabled();
    await userEvent.click(exporting);
    expect(exportedQuery()).toBe(keysQuery(keys));
  });
});
