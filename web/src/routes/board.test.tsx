import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import type { BoardSummary } from "@/api/boards";

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  createRoute: (options: object) => ({ ...options, useParams: () => ({ projectKey: "CP" }) }),
}));
vi.mock("./project", () => ({ projectRoute: {} }));

const boards: BoardSummary[] = [
  { id: "b1", name: "Portal board", type: "scrum", swimlaneCount: 4 },
  { id: "b2", name: "Team flow", type: "kanban", teamId: "t1", swimlaneCount: 4 },
];

vi.mock("@/api/boards", () => ({ useBoards: () => ({ data: { boards } }) }));
vi.mock("@/api/projects", () => ({ useProject: () => ({ data: { project: { name: "Portal" } } }) }));
vi.mock("@/api/auth", () => ({ useMe: () => ({ data: { principal: { role: "owner" } } }) }));
vi.mock("@/features/board/BoardPicker", () => ({
  BoardPicker: ({ onPick }: { onPick: (board: BoardSummary | null) => void }) => (
    <div>
      <button onClick={() => onPick(null)}>Portal board</button>
      <button onClick={() => onPick(boards[1]!)}>Team flow</button>
    </div>
  ),
}));
vi.mock("@/features/board/BoardView", () => ({ BoardView: () => null }));
vi.mock("@/features/board/NewBoardForm", () => ({ NewBoardForm: () => null }));
vi.mock("@/features/board/SwimlaneEditor", () => ({
  SwimlaneEditor: ({ boardId }: { boardId: string }) => <p data-testid="editing">{boardId}</p>,
}));

const { boardRoute } = await import("./board");
const BoardPage = (boardRoute as unknown as { component: () => ReactNode }).component;

describe("the board page", () => {
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
});
