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

function mutation(mutate: ReturnType<typeof vi.fn>) {
  return { mutate, isPending: false, error: null };
}

vi.mock("@/api/boards", () => ({
  useBoardById: (id: string) => useBoardById(id),
  useAddSwimlane: () => mutation(addSwimlane),
  useUpdateSwimlane: () => mutation(updateSwimlane),
  useDeleteSwimlane: () => mutation(deleteSwimlane),
  useReorderSwimlanes: () => mutation(reorderSwimlanes),
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
  swimlanes: [lane("l1", "Queued", 0, statuses[0]!), lane("l2", "Doing", 1, statuses[1]!)],
  unmapped: [],
  createdAt: "",
  updatedAt: "",
};

beforeEach(() => {
  for (const fn of [addSwimlane, updateSwimlane, deleteSwimlane, reorderSwimlanes, updateBoard, useBoardById]) fn.mockReset();
  useBoardById.mockReturnValue({ data: { board: teamBoard }, isLoading: false });
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
});
