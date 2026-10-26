import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { BULK_MAX_SELECTION } from "@/config";
import { ConfirmProvider } from "@/features/shell/ConfirmProvider";
import type { Status } from "@/api/issues";

const state = { mutate: vi.fn(), shared: ["Start progress", "Close"] };

vi.mock("@/api/filters", async () => ({
  ...(await vi.importActual<typeof import("@/api/filters")>("@/api/filters")),
  useBulkEdit: () => ({ mutate: state.mutate, isPending: false, error: null }),
  useBulkTransitions: () => ({ data: { names: state.shared } }),
}));

vi.mock("@/api/labels", async () => ({
  ...(await vi.importActual<typeof import("@/api/labels")>("@/api/labels")),
  useLabelSearch: (q: string) => ({
    data: q ? { labels: [{ id: "l1", name: "backend", color: "blue", issueCount: 4 }].filter((l) => l.name.includes(q)) } : undefined,
  }),
}));

vi.mock("@/api/people", () => ({ useMemberSearch: () => ({ data: { members: [], total: 0, limit: 20, offset: 0 } }) }));
vi.mock("@/api/sprints", () => ({ useSprints: () => ({ data: { sprints: [{ id: "s1", name: "Sprint 7" }] } }) }));
vi.mock("@/api/milestones", () => ({ useMilestones: () => ({ data: { milestones: [{ id: "m1", name: "Launch" }] } }) }));
vi.mock("@/api/teams", () => ({ useTeams: () => ({ data: { teams: [{ id: "t1", name: "Crew" }] } }) }));
vi.mock("@/api/versions", () => ({
  useVersions: () => ({ data: { versions: [{ id: "v1", name: "1.0" }] } }),
  useComponents: () => ({ data: { components: [{ id: "c1", name: "Engine" }] } }),
}));
vi.mock("@/api/fields", () => ({ useProjectFields: () => ({ data: { fields: [{ id: "f1", name: "Cost", kind: "number", options: [], org: false }] } }) }));
vi.mock("@/api/projects", () => ({
  useProjectSearch: () => ({ data: { projects: [{ key: "OPS", name: "Operations" }] } }),
  useProject: () => ({ data: undefined }),
}));
vi.mock("@/api/issues", async () => ({
  ...(await vi.importActual<typeof import("@/api/issues")>("@/api/issues")),
  useProjectStatuses: (key: string) => ({
    data: key
      ? {
          statuses: [
            { id: "ops-todo", name: "To Do" },
            { id: "ops-doing", name: "Doing" },
          ],
        }
      : undefined,
  }),
}));

const { BulkBar, changeIsEmpty, projectOf } = await import("./BulkBar");
const { keysQuery } = await import("./ExportDialog");
const { defaultMapping } = await import("./BulkMoveDialog");

const renderBar = (ui: ReactElement) => render(<ConfirmProvider>{ui}</ConfirmProvider>);

/** The query a download link asks the export for. */
function exportedQuery(): string | null {
  const href = document.querySelector('[data-action="download-csv"]')?.getAttribute("href") ?? "";
  return new URL(href, "http://armature.test").searchParams.get("q");
}

const todo = { id: "todo", name: "To Do", category: "todo" as const, position: 0 };
const review = { id: "review", name: "In Review", category: "in_progress" as const, position: 1 };
const issue = (key: string, status: Status = todo) => ({ key, status, labels: [] });

describe("changeIsEmpty", () => {
  it("is empty until something is asked", () => {
    expect(changeIsEmpty({})).toBe(true);
    expect(changeIsEmpty({ transition: "  " })).toBe(true);
    expect(changeIsEmpty({ priority: "high" })).toBe(false);
    expect(changeIsEmpty({ assignee: null })).toBe(false);
    expect(changeIsEmpty({ addLabels: ["x"] })).toBe(false);
    expect(changeIsEmpty({ removeLabels: ["x"] })).toBe(false);
    expect(changeIsEmpty({ sprintId: null })).toBe(false);
    expect(changeIsEmpty({ fixVersions: { add: ["v1"] } })).toBe(false);
    expect(changeIsEmpty({ fields: {} })).toBe(true);
    expect(changeIsEmpty({ fields: { f1: null } })).toBe(false);
  });
});

describe("keysQuery", () => {
  it("names one issue with = and several with in", () => {
    expect(keysQuery(["CP-5"])).toBe("key = CP-5");
    expect(keysQuery(["CP-1", "OPS-2"])).toBe("key in (CP-1, OPS-2)");
  });
});

describe("projectOf", () => {
  it("is the key before the number", () => {
    expect(projectOf("CP-12")).toBe("CP");
    expect(projectOf("ops2-3")).toBe("OPS2");
  });
});

describe("defaultMapping", () => {
  it("keeps a status the target has, takes one of the same name, and leaves the rest to choose", () => {
    const there = [
      { ...todo, id: "elsewhere-todo" },
      { ...review, id: "review" },
    ];
    const doing = { ...todo, id: "doing", name: "Doing" };
    expect(defaultMapping([todo, review, doing], there)).toEqual({ todo: "elsewhere-todo", review: "review" });
  });
});

describe("BulkBar", () => {
  beforeEach(() => {
    state.mutate = vi.fn();
    state.shared = ["Start progress", "Close"];
  });

  it("exports the ticked rows before any change is chosen", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    expect(screen.getByRole("button", { name: /Apply to 2/ })).toBeDisabled();
    const exporting = screen.getByRole("button", { name: "Export 2 selected" });
    expect(exporting).toBeEnabled();
    await userEvent.click(exporting);
    expect(exportedQuery()).toBe("key in (CP-1, CP-2)");
  });

  it("exports more rows than one change may touch", async () => {
    const keys = Array.from({ length: BULK_MAX_SELECTION + 1 }, (_, i) => `CP-${i + 1}`);
    renderBar(<BulkBar keys={keys} onClear={() => {}} />);
    const exporting = screen.getByRole("button", { name: `Export ${keys.length} selected` });
    expect(exporting).toBeEnabled();
    await userEvent.click(exporting);
    expect(exportedQuery()).toBe(keysQuery(keys));
  });

  it("adds the labels picked in the box to every ticked row", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    await userEvent.type(screen.getByLabelText("Add labels"), "back");
    await userEvent.click(await screen.findByRole("option", { name: "backend" }));
    await userEvent.type(screen.getByLabelText("Add labels"), "fresh{Enter}");
    await userEvent.click(screen.getByRole("button", { name: /Apply to 2/ }));
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({ keys: ["CP-1", "CP-2"], change: { addLabels: ["backend", "fresh"] } });
  });

  // The other label fields add a typed word on blur; Apply must not wait for Enter.
  it("counts a label typed but not yet added, and applies it", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    const apply = screen.getByRole("button", { name: /Apply to 2/ });
    expect(apply).toBeDisabled();
    await userEvent.type(screen.getByLabelText("Add labels"), "pending");
    expect(apply).toBeEnabled();
    await userEvent.click(apply);
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({ keys: ["CP-1", "CP-2"], change: { addLabels: ["pending"] } });
  });

  it("takes a label off every ticked row", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    await userEvent.type(screen.getByLabelText("Remove labels"), "back");
    await userEvent.click(await screen.findByRole("option", { name: "backend" }));
    await userEvent.click(screen.getByRole("button", { name: /Apply to 2/ }));
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({ keys: ["CP-1", "CP-2"], change: { removeLabels: ["backend"] } });
  });

  it("offers only the transitions every ticked row shares", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    const transition = screen.getByLabelText<HTMLSelectElement>("Transition");
    expect([...transition.options].map((o) => o.textContent)).toEqual(["Leave", "Start progress", "Close"]);
    await userEvent.selectOptions(transition, "Close");
    await userEvent.click(screen.getByRole("button", { name: /Apply to 2/ }));
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({ keys: ["CP-1", "CP-2"], change: { transition: "Close" } });
  });

  it("says when the ticked rows share no transition", () => {
    state.shared = [];
    renderBar(<BulkBar keys={["CP-1", "OPS-2"]} onClear={() => {}} />);
    const transition = screen.getByLabelText<HTMLSelectElement>("Transition");
    expect(transition).toBeDisabled();
    expect(transition.options[0]?.textContent).toBe("None shared");
  });

  it("offers one project's sprints, milestones, teams, versions, components and fields", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} onClear={() => {}} />);
    await userEvent.selectOptions(screen.getByLabelText("Team"), "Crew");
    await userEvent.selectOptions(screen.getByLabelText("Sprint"), "No sprint");
    await userEvent.selectOptions(screen.getByLabelText("Milestone"), "Launch");
    await userEvent.click(screen.getByRole("button", { name: /add fix versions/i }));
    await userEvent.click(screen.getByLabelText("1.0"));
    await userEvent.selectOptions(screen.getByLabelText("Field"), "Cost");
    await userEvent.type(screen.getByLabelText("New Cost"), "12");
    await userEvent.click(screen.getByRole("button", { name: /Apply to 2/ }));
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({
      keys: ["CP-1", "CP-2"],
      change: { teamId: "t1", sprintId: null, milestoneId: "m1", fixVersions: { add: ["v1"] }, fields: { f1: 12 } },
    });
  });

  it("clears a field only when told to, never because its box is empty", async () => {
    renderBar(<BulkBar keys={["CP-1"]} onClear={() => {}} />);
    await userEvent.selectOptions(screen.getByLabelText("Field"), "Cost");
    expect(screen.getByRole("button", { name: /Apply to 1/ })).toBeDisabled();
    await userEvent.click(screen.getByLabelText("Clear it"));
    await userEvent.click(screen.getByRole("button", { name: /Apply to 1/ }));
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({ keys: ["CP-1"], change: { fields: { f1: null } } });
  });

  it("keeps what belongs to one project out of a selection across two", () => {
    renderBar(<BulkBar keys={["CP-1", "OPS-2"]} onClear={() => {}} />);
    expect(screen.queryByLabelText("Sprint")).toBeNull();
    expect(screen.queryByLabelText("Field")).toBeNull();
    expect(document.querySelector("[data-bulk-one-project]")?.textContent).toContain("Select issues of one project");
  });

  it("asks before deleting, with the count and the noun", async () => {
    const keys = Array.from({ length: 30 }, (_, i) => `CP-${i + 1}`);
    renderBar(<BulkBar keys={keys} onClear={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete 30 issues?" });
    expect(state.mutate).not.toHaveBeenCalled();
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete 30 issues" }));
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({ keys, change: { delete: true } });
  });

  it("moves the ticked rows with a status for each of theirs", async () => {
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} issues={[issue("CP-1"), issue("CP-2", review)]} onClear={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    const dialog = await screen.findByRole("dialog", { name: "Move 2 issues to another project" });
    await userEvent.selectOptions(within(dialog).getByLabelText("To project"), "OPS");
    const go = within(dialog).getByRole("button", { name: "Move" });
    // In Review has no namesake there, so it waits to be chosen.
    expect(go).toBeDisabled();
    await userEvent.selectOptions(within(dialog).getByLabelText("Issues in In Review go to"), "Doing");
    await userEvent.click(go);
    expect(state.mutate.mock.calls[0]?.[0]).toEqual({
      keys: ["CP-1", "CP-2"],
      change: { moveTo: { projectKey: "OPS", statuses: { todo: "ops-todo", review: "ops-doing" } } },
    });
  });

  it("lists what was refused and what a move renamed", async () => {
    state.mutate = vi.fn((_input, options: { onSuccess: (r: unknown) => void }) =>
      options.onSuccess({ applied: ["CP-1"], refused: [{ key: "CP-2", reason: "That issue was not found." }], moved: [{ from: "CP-1", to: "OPS-9" }] }),
    );
    renderBar(<BulkBar keys={["CP-1", "CP-2"]} issues={[issue("CP-1"), issue("CP-2")]} onClear={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    const dialog = await screen.findByRole("dialog", { name: "Move 2 issues to another project" });
    await userEvent.selectOptions(within(dialog).getByLabelText("To project"), "OPS");
    await userEvent.click(within(dialog).getByRole("button", { name: "Move" }));
    expect(document.querySelector('[data-bulk-refusal="CP-2"]')?.textContent).toContain("That issue was not found.");
    expect(document.querySelector('[data-bulk-moved="CP-1"]')?.textContent).toBe("CP-1 is now OPS-9");
  });
});
