import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Board, Swimlane } from "@/api/boards";
import type { Status } from "@/api/issues";

const addSwimlane = vi.fn();
const updateSwimlane = vi.fn();
const deleteSwimlane = vi.fn();
const reorderSwimlanes = vi.fn();
const updateBoard = vi.fn();
const useBoardById = vi.fn();
let reorderPending = false;

function mutation(mutate: ReturnType<typeof vi.fn>) {
  return { mutate, isPending: false, error: null };
}

vi.mock("@/api/boards", () => ({
  useBoardById: (id: string) => useBoardById(id),
  useAddSwimlane: () => mutation(addSwimlane),
  useUpdateSwimlane: () => mutation(updateSwimlane),
  useDeleteSwimlane: () => mutation(deleteSwimlane),
  useReorderSwimlanes: () => ({ ...mutation(reorderSwimlanes), isPending: reorderPending }),
  useUpdateBoard: () => mutation(updateBoard),
}));

const statuses: Status[] = [
  { id: "s1", name: "To Do", category: "todo", position: 0 },
  { id: "s2", name: "Doing", category: "in_progress", position: 1 },
];

vi.mock("@/api/issues", () => ({ useStatuses: () => ({ data: { statuses } }) }));
vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => async () => true }));

const { SwimlaneEditor } = await import("./SwimlaneEditor");

function lane(id: string, name: string, position: number, status: Status): Swimlane {
  return { id, name, position, wipLimit: 0, statuses: [status], cards: [] };
}

// The team's board, which is not the project's first.
const teamBoard: Board = {
  id: "b2",
  projectId: "p",
  projectKey: "CP",
  name: "Team flow",
  type: "kanban",
  groupBy: "none",
  doneForDays: 14,
  leftOut: 0,
  swimlanes: [lane("l1", "Queued", 0, statuses[0]!), lane("l2", "Doing", 1, statuses[1]!)],
  unmapped: [],
  createdAt: "",
  updatedAt: "",
};

beforeEach(() => {
  for (const fn of [addSwimlane, updateSwimlane, deleteSwimlane, reorderSwimlanes, updateBoard, useBoardById]) fn.mockReset();
  useBoardById.mockReturnValue({ data: { board: teamBoard }, isLoading: false });
  reorderPending = false;
});

describe("SwimlaneEditor", () => {
  // The panel sits beside whichever board is on screen; reading the project's
  // first board instead edited lanes nobody was looking at.
  it("reads the board it is given", () => {
    render(<SwimlaneEditor boardId="b2" />);
    expect(useBoardById).toHaveBeenCalledWith("b2");
    expect(screen.getByLabelText("Swimlane 1 name")).toHaveValue("Queued");
  });

  it("groups only that board", async () => {
    render(<SwimlaneEditor boardId="b2" />);
    await userEvent.click(screen.getByRole("button", { name: "Assignee" }));
    expect(updateBoard).toHaveBeenCalledWith({ id: "b2", groupBy: "assignee" });
  });

  it("keeps a kanban board's finished work as long as it is told", async () => {
    render(<SwimlaneEditor boardId="b2" />);
    const card = within(document.querySelector<HTMLElement>("[data-done-for-days-editor]")!);
    const days = card.getByLabelText("Finished work stays for");
    expect(days).toHaveValue(14);
    expect(days).toHaveAccessibleDescription("0 keeps it on the board.");
    await userEvent.clear(days);
    await userEvent.type(days, "30");
    await userEvent.click(card.getByRole("button", { name: "Save" }));
    expect(updateBoard).toHaveBeenCalledWith({ id: "b2", doneForDays: 30 });

    await userEvent.clear(days);
    await userEvent.type(days, "0");
    await userEvent.click(card.getByRole("button", { name: "Save" }));
    expect(updateBoard).toHaveBeenLastCalledWith({ id: "b2", doneForDays: 0 });
  });

  // A scrum board draws its sprint, finished or not, so there is nothing to set.
  it("offers no such setting on a scrum board", () => {
    useBoardById.mockReturnValue({ data: { board: { ...teamBoard, type: "scrum" } }, isLoading: false });
    render(<SwimlaneEditor boardId="b2" />);
    expect(screen.queryByLabelText("Finished work stays for")).toBeNull();
  });

  it("adds a lane to that board", async () => {
    render(<SwimlaneEditor boardId="b2" />);
    await userEvent.click(screen.getByRole("button", { name: "Add swimlane" }));
    await userEvent.type(screen.getByLabelText("New swimlane name"), "Blocked");
    await userEvent.click(screen.getByRole("button", { name: "Add swimlane" }));
    expect(addSwimlane).toHaveBeenCalledWith(
      expect.objectContaining({ boardId: "b2", name: "Blocked" }),
      expect.anything(),
    );
  });

  it("edits, moves and deletes that board's lanes", async () => {
    const { container } = render(<SwimlaneEditor boardId="b2" />);
    const queued = within(container.querySelector('[data-swimlane-editor="Queued"]') as HTMLElement);

    await userEvent.clear(queued.getByLabelText("Card limit"));
    await userEvent.type(queued.getByLabelText("Card limit"), "3");
    await userEvent.click(queued.getByRole("button", { name: "Save" }));
    expect(updateSwimlane).toHaveBeenCalledWith(
      expect.objectContaining({ boardId: "b2", swimlaneId: "l1", wipLimit: 3 }),
    );

    await userEvent.click(queued.getByRole("button", { name: "Move Queued right" }));
    expect(reorderSwimlanes).toHaveBeenCalledWith({ boardId: "b2", order: ["l2", "l1"] });

    await userEvent.click(queued.getByRole("button", { name: "Delete" }));
    expect(deleteSwimlane).toHaveBeenCalledWith({ boardId: "b2", swimlaneId: "l1" });
  });

  // A move is built from the order on screen, so a second click before the
  // first landed sent that same stale order again and undid nothing it meant to.
  it("holds the arrows while a move is on its way", async () => {
    reorderPending = true;
    const { container } = render(<SwimlaneEditor boardId="b2" />);
    const queued = within(container.querySelector('[data-swimlane-editor="Queued"]') as HTMLElement);
    const doing = within(container.querySelector('[data-swimlane-editor="Doing"]') as HTMLElement);

    expect(queued.getByRole("button", { name: "Move Queued right" })).toBeDisabled();
    expect(doing.getByRole("button", { name: "Move Doing left" })).toBeDisabled();
    await userEvent.click(doing.getByRole("button", { name: "Move Doing left" }));
    expect(reorderSwimlanes).not.toHaveBeenCalled();
  });
});
