import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { BoardSummary } from "@/api/boards";
import type { QuickFilter } from "@/features/board/quickFilter";

// The address as a tiny store, so a navigation redraws the page as the router would.
const address = vi.hoisted(() => {
  let search: Record<string, unknown> = {};
  const listeners = new Set<() => void>();
  return {
    navigations: [] as Array<{ search: Record<string, unknown>; replace?: boolean }>,
    get: () => search,
    set(next: Record<string, unknown>) {
      search = next;
      listeners.forEach((listener) => listener());
    },
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
});

vi.mock("@tanstack/react-router", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
    createRoute: (options: object) => ({
      ...options,
      useParams: () => ({ projectKey: "CP" }),
      useSearch: () => useSyncExternalStore(address.subscribe, address.get),
    }),
    useNavigate: () => (options: { search: Record<string, unknown>; replace?: boolean }) => {
      address.navigations.push(options);
      address.set(options.search);
    },
  };
});
vi.mock("./project", () => ({ projectRoute: {} }));

const boards: BoardSummary[] = [
  { id: "b1", name: "Portal board", type: "scrum", swimlaneCount: 4 },
  { id: "b2", name: "Team flow", type: "kanban", teamId: "t1", swimlaneCount: 4 },
];

vi.mock("@/api/boards", () => ({ useBoards: () => ({ data: { boards } }) }));
vi.mock("@/api/projects", () => ({ useProject: () => ({ data: { project: { name: "Portal" } } }) }));
// What the signed-in person holds, per project, as /access/me answers it.
const held = vi.hoisted(() => ({
  access: {
    grants: [],
    projects: ["CP"],
    canAdministerOrg: false,
    canCreateProject: false,
    permissions: { org: [] as string[], projects: { CP: ["board.configure", "project.administer"] } as Record<string, string[]> },
  },
}));
vi.mock("@/api/access", async (importActual) => ({
  ...(await importActual<typeof import("@/api/access")>()),
  useAccess: () => ({ data: held.access }),
}));
vi.mock("@/features/board/BoardPicker", () => ({
  BoardPicker: ({ onPick, canManage }: { onPick: (board: BoardSummary | null) => void; canManage: boolean }) => (
    <div data-testid="picker" data-can-manage={String(canManage)}>
      <button type="button" onClick={() => onPick(null)}>
        Portal board
      </button>
      <button type="button" onClick={() => onPick(boards[1]!)}>
        Team flow
      </button>
    </div>
  ),
}));
vi.mock("@/features/board/BoardView", () => ({
  BoardView: ({ boardId, filter, onFilter }: { boardId?: string; filter: QuickFilter; onFilter: (filter: QuickFilter) => void }) => (
    <div data-testid="board" data-board-id={boardId ?? ""} data-mine={String(Boolean(filter.mine))}>
      <button type="button" onClick={() => onFilter({ ...filter, mine: true })}>
        Only mine
      </button>
    </div>
  ),
}));
vi.mock("@/features/board/NewBoardForm", () => ({ NewBoardForm: () => null }));
vi.mock("@/features/board/SwimlaneEditor", () => ({
  SwimlaneEditor: ({ boardId }: { boardId: string }) => <p data-testid="editing">{boardId}</p>,
}));

const { boardRoute } = await import("./board");
const BoardPage = (boardRoute as unknown as { component: () => ReactNode }).component;

describe("the board page", () => {
  beforeEach(() => {
    address.set({});
    address.navigations.length = 0;
    held.access.permissions = { org: [], projects: { CP: ["board.configure", "project.administer"] } };
  });

  // The controls followed the installation role, so a project role that grants
  // board.configure never saw them.
  it("offers the board's configuration to a plain member who may configure boards in the project", () => {
    held.access.permissions = { org: ["issue.read"], projects: { CP: ["board.configure"] } };
    render(<BoardPage />);
    expect(screen.getByRole("button", { name: "Configure swimlanes" })).toBeInTheDocument();
    expect(screen.getByTestId("picker")).toHaveAttribute("data-can-manage", "true");
    // Making a board is administering the project, which this person does not.
    expect(screen.queryByRole("button", { name: "New board" })).not.toBeInTheDocument();
  });

  it("hides the board's configuration from a member who may not configure boards in the project", () => {
    held.access.permissions = { org: ["issue.read"], projects: { CP: ["issue.write"], OTHER: ["board.configure", "project.administer"] } };
    render(<BoardPage />);
    expect(screen.queryByRole("button", { name: "Configure swimlanes" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New board" })).not.toBeInTheDocument();
    expect(screen.getByTestId("picker")).toHaveAttribute("data-can-manage", "false");
  });

  it("offers a new board to whoever administers the project", () => {
    render(<BoardPage />);
    expect(screen.getByRole("button", { name: "New board" })).toBeInTheDocument();
  });

  // The editor used to be handed only the project, and so always edited the
  // project's first board whichever one was on screen.
  it("configures the board on screen", async () => {
    render(<BoardPage />);
    await userEvent.click(screen.getByRole("button", { name: "Team flow" }));
    await userEvent.click(screen.getByRole("button", { name: "Configure swimlanes" }));
    expect(screen.getByTestId("editing")).toHaveTextContent("b2");
  });

  it("configures the board the project opens on until another is picked", async () => {
    render(<BoardPage />);
    await userEvent.click(screen.getByRole("button", { name: "Configure swimlanes" }));
    expect(screen.getByTestId("editing")).toHaveTextContent("b1");
  });

  it("keeps the board on screen and its filter in the address", async () => {
    render(<BoardPage />);
    await userEvent.click(screen.getByRole("button", { name: "Team flow" }));
    expect(address.navigations.at(-1)).toMatchObject({ search: { board: "b2" }, replace: false });

    await userEvent.click(screen.getByRole("button", { name: "Only mine" }));
    expect(address.navigations.at(-1)).toMatchObject({ search: { board: "b2", mine: true }, replace: true });
    expect(screen.getByTestId("board")).toHaveAttribute("data-mine", "true");
  });

  it("opens on the board and the filter a link names", () => {
    address.set({ board: "b2", mine: true });
    render(<BoardPage />);
    expect(screen.getByRole("heading", { name: /Team flow/ })).toBeInTheDocument();
    expect(screen.getByTestId("board")).toHaveAttribute("data-board-id", "b2");
    expect(screen.getByTestId("board")).toHaveAttribute("data-mine", "true");
  });

  it("keeps the filter when another board is picked", async () => {
    address.set({ mine: true });
    render(<BoardPage />);
    await userEvent.click(screen.getByRole("button", { name: "Team flow" }));
    expect(address.navigations.at(-1)).toMatchObject({ search: { board: "b2", mine: true } });
  });
});
