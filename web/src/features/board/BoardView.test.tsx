import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render } from "@testing-library/react";
import type { ReactNode } from "react";
import type { Board, Card, Swimlane } from "@/api/boards";
import type { Status } from "@/api/issues";

const moveCard = vi.fn();

vi.mock("@tanstack/react-router", () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a> }));
vi.mock("@/api/auth", () => ({ useMe: () => ({ data: { principal: { user: { id: "u1" } } } }) }));
vi.mock("@/features/issues/IssueDrawer", () => ({ useIssueDrawer: () => ({ current: null, open: vi.fn() }), useIssueList: () => {} }));
vi.mock("@/components/ui", async (importActual) => ({
  ...(await importActual<typeof import("@/components/ui")>()),
  useToast: () => ({ success: vi.fn(), error: vi.fn() }),
}));
vi.mock("./QuickFilters", () => ({ QuickFilters: () => null }));

const todo: Status = { id: "s1", name: "To Do", category: "todo", position: 0 };
const doing: Status = { id: "s2", name: "Doing", category: "in_progress", position: 1 };

function card(key: string, status: Status, rank: string): Card {
  return {
    id: key,
    key,
    summary: `${key} summary`,
    labels: [],
    type: { id: "t1", name: "Task", icon: "task", level: 0, isSubtask: false },
    status,
    priority: "medium",
    rank,
    updatedAt: "2026-10-01T00:00:00Z",
  };
}

function lane(id: string, name: string, status: Status, cards: Card[]): Swimlane {
  return { id, name, position: 0, wipLimit: 0, statuses: [status], cards };
}

const board: Board = {
  id: "b1",
  projectId: "p",
  projectKey: "CP",
  name: "Portal board",
  type: "kanban",
  groupBy: "none",
  doneForDays: 0,
  leftOut: 0,
  swimlanes: [
    lane("l1", "To Do", todo, [card("CP-1", todo, "a"), card("CP-2", todo, "b"), card("CP-3", todo, "c")]),
    lane("l2", "Doing", doing, [card("CP-4", doing, "a")]),
  ],
  unmapped: [],
  createdAt: "2026-10-01T00:00:00Z",
  updatedAt: "2026-10-01T00:00:00Z",
};

vi.mock("@/api/boards", async (importActual) => ({
  ...(await importActual<typeof import("@/api/boards")>()),
  useBoard: () => ({ data: { board }, isLoading: false, error: null }),
  useBoardById: () => ({ data: undefined, isLoading: false, error: null }),
  useSprintBoard: () => ({ data: undefined, isLoading: false, error: null }),
  useMoveCard: () => ({ mutate: moveCard, isPending: false, error: null, data: undefined }),
}));

const { BoardView } = await import("./BoardView");
const { EMPTY_QUICK_FILTER } = await import("./quickFilter");

/** A DataTransfer that carries what was set on it, which jsdom does not provide. */
function transfer() {
  const data = new Map<string, string>();
  return { setData: (type: string, value: string) => data.set(type, value), getData: (type: string) => data.get(type) ?? "", effectAllowed: "" };
}

function cardOf(container: HTMLElement, key: string): HTMLElement {
  return container.querySelector(`[data-card="${key}"]`)!;
}

/** Drags one card and lets it go over another, as a person would. */
function dragOnto(container: HTMLElement, dragged: string, onto: string) {
  const dataTransfer = transfer();
  fireEvent.dragStart(cardOf(container, dragged), { dataTransfer });
  fireEvent.dragOver(cardOf(container, onto), { dataTransfer });
  fireEvent.drop(cardOf(container, onto), { dataTransfer });
}

describe("dropping a card on the board", () => {
  beforeEach(() => moveCard.mockReset());

  // The card was its own lower neighbour, so the move went out with only the
  // card above and the server ranked it at the bottom of the lane.
  it("leaves the order alone when a card is dropped on itself", () => {
    const { container } = render(<BoardView projectKey="CP" filter={EMPTY_QUICK_FILTER} onFilter={() => {}} />);
    dragOnto(container, "CP-2", "CP-2");
    expect(moveCard).not.toHaveBeenCalled();
  });

  it("leaves the order alone when a card is dropped just above the card below it", () => {
    const { container } = render(<BoardView projectKey="CP" filter={EMPTY_QUICK_FILTER} onFilter={() => {}} />);
    dragOnto(container, "CP-2", "CP-3");
    expect(moveCard).not.toHaveBeenCalled();
  });

  it("sends both neighbours when a card is moved up its lane", () => {
    const { container } = render(<BoardView projectKey="CP" filter={EMPTY_QUICK_FILTER} onFilter={() => {}} />);
    dragOnto(container, "CP-3", "CP-2");
    expect(moveCard).toHaveBeenCalledWith(
      expect.objectContaining({ issueKey: "CP-3", swimlaneId: "l1", afterKey: "CP-1", beforeKey: "CP-2" }),
      expect.anything(),
    );
  });

  it("moves a card into another lane above the card it is dropped on", () => {
    const { container } = render(<BoardView projectKey="CP" filter={EMPTY_QUICK_FILTER} onFilter={() => {}} />);
    dragOnto(container, "CP-2", "CP-4");
    expect(moveCard).toHaveBeenCalledWith(
      expect.objectContaining({ issueKey: "CP-2", swimlaneId: "l2", afterKey: undefined, beforeKey: "CP-4" }),
      expect.anything(),
    );
  });
});
