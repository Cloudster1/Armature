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
const useProjectStatuses = vi.fn();
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

// Everyone's statuses hold one only another project's workflow has, which the
// editor must never offer.
const elsewhere: Status = { id: "s8", name: "Elsewhere", category: "in_progress", position: 8 };
vi.mock("@/api/issues", () => ({
  useStatuses: () => ({ data: { statuses: [...statuses, elsewhere] } }),
  useProjectStatuses: (key: string) => useProjectStatuses(key),
}));
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
  for (const fn of [addSwimlane, updateSwimlane, deleteSwimlane, reorderSwimlanes, updateBoard, useBoardById, useProjectStatuses]) fn.mockReset();
  useBoardById.mockReturnValue({ data: { board: teamBoard }, isLoading: false });
  useProjectStatuses.mockReturnValue({ data: { statuses } });
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

  // Drawn before the project's states, every lane grows when they land and a
  // click aimed at one of its buttons hits whatever moved under it.
  it("waits for the project's states before it draws the lanes", () => {
    useProjectStatuses.mockReturnValue({ data: undefined, isError: false });
    render(<SwimlaneEditor boardId="b2" />);
    expect(screen.queryByLabelText("Swimlane 1 name")).toBeNull();
  });

  it("draws the lanes when the project's states cannot be had", () => {
    useProjectStatuses.mockReturnValue({ data: undefined, isError: true });
    render(<SwimlaneEditor boardId="b2" />);
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
    expect(addSwimlane).toHaveBeenCalledWith(expect.objectContaining({ boardId: "b2", name: "Blocked" }), expect.anything());
  });

  it("edits, moves and deletes that board's lanes", async () => {
    const { container } = render(<SwimlaneEditor boardId="b2" />);
    const queued = within(container.querySelector('[data-swimlane-editor="Queued"]') as HTMLElement);

    await userEvent.clear(queued.getByLabelText("Card limit"));
    await userEvent.type(queued.getByLabelText("Card limit"), "3");
    await userEvent.click(queued.getByRole("button", { name: "Save" }));
    expect(updateSwimlane).toHaveBeenCalledWith(expect.objectContaining({ boardId: "b2", swimlaneId: "l1", wipLimit: 3 }));

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

  // The editor offered every status in the installation, so the warning
  // counted states no card of the project can ever be in.
  describe("the states it offers", () => {
    const blocked: Status = { id: "s3", name: "Blocked", category: "in_progress", position: 2 };
    const retired: Status = { id: "s9", name: "Retired", category: "done", position: 9 };

    function laneOf(container: HTMLElement, name: string) {
      return within(container.querySelector(`[data-swimlane-editor="${name}"]`) as HTMLElement);
    }

    it("are the statuses of the board's project", () => {
      useProjectStatuses.mockReturnValue({ data: { statuses: [...statuses, blocked] } });
      const { container } = render(<SwimlaneEditor boardId="b2" />);
      expect(useProjectStatuses).toHaveBeenCalledWith("CP");
      const queued = laneOf(container, "Queued");
      expect(queued.getAllByRole("checkbox").map((box) => box.closest("label")?.textContent)).toEqual(["To Do", "Doing", "Blocked"]);
    });

    it("count only the project's statuses no lane holds", () => {
      useProjectStatuses.mockReturnValue({ data: { statuses: [...statuses, blocked] } });
      render(<SwimlaneEditor boardId="b2" />);
      expect(screen.getByText("1 state not on the board")).toBeInTheDocument();
      expect(screen.getByText(/\(Blocked\)\./)).toBeInTheDocument();
    });

    it("warn of nothing when every one of the project's statuses has a lane", () => {
      render(<SwimlaneEditor boardId="b2" />);
      expect(screen.queryByText(/not on the board/)).toBeNull();
    });

    it("are only the unclaimed ones for a new lane", async () => {
      useProjectStatuses.mockReturnValue({ data: { statuses: [...statuses, blocked] } });
      render(<SwimlaneEditor boardId="b2" />);
      await userEvent.click(screen.getByRole("button", { name: "Add swimlane" }));
      const offered = within(screen.getByText("States for it (only ones not already on the board)").closest("fieldset")!);
      expect(offered.getAllByRole("checkbox").map((box) => box.closest("label")?.textContent)).toEqual(["Blocked"]);
    });

    // A scheme change can leave a lane holding a state the project no longer
    // reaches; hiding it would leave no way to take it off.
    it("keep a state the lane holds that the project no longer reaches, so it can be let go", async () => {
      const held: Board = {
        ...teamBoard,
        swimlanes: [{ ...lane("l1", "Queued", 0, statuses[0]!), statuses: [statuses[0]!, retired] }, teamBoard.swimlanes[1]!],
      };
      useBoardById.mockReturnValue({ data: { board: held }, isLoading: false });
      const { container } = render(<SwimlaneEditor boardId="b2" />);
      const queued = laneOf(container, "Queued");
      const box = queued.getByRole("checkbox", { name: "Retired" });
      expect(box).toBeChecked();
      expect(laneOf(container, "Doing").queryByRole("checkbox", { name: "Retired" })).toBeNull();
      await userEvent.click(box);
      expect(updateSwimlane).toHaveBeenCalledWith({ boardId: "b2", swimlaneId: "l1", statusIds: ["s1"] });
    });
  });
});
