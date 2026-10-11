import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { BoardSummary } from "@/api/boards";
import type { Team } from "@/api/teams";

const createBoard = vi.fn();
const updateBoard = vi.fn();
const deleteBoard = vi.fn();
const confirm = vi.fn(async () => true);
const state = { boards: [] as BoardSummary[], teams: [] as Team[] };

vi.mock("@/api/boards", () => ({
  useBoards: () => ({ data: { boards: state.boards } }),
  useCreateBoard: () => ({ mutate: createBoard, isPending: false, error: null }),
  useUpdateBoard: () => ({ mutate: updateBoard, isPending: false, error: null }),
  useDeleteBoard: () => ({ mutate: deleteBoard, isPending: false, error: null }),
}));

vi.mock("@/features/shell/ConfirmProvider", () => ({ useConfirm: () => confirm }));

vi.mock("@/api/teams", () => ({
  useTeams: () => ({ data: { teams: state.teams } }),
}));

const { BoardPicker } = await import("./BoardPicker");

function board(over: Partial<BoardSummary> = {}): BoardSummary {
  return { id: "b1", name: "Customer Portal board", type: "kanban", swimlaneCount: 4, ...over };
}

function team(over: Partial<Team> = {}): Team {
  return {
    id: "t1",
    projectId: "p",
    projectKey: "CP",
    name: "Platform",
    position: 0,
    memberCount: 1,
    issueCount: 3,
    boardCount: 1,
    createdAt: "",
    updatedAt: "",
    ...over,
  };
}

describe("BoardPicker", () => {
  // Most projects have one board and nothing to choose between.
  it("says nothing when there is only one board and every team has one", () => {
    state.boards = [board()];
    state.teams = [];

    const { container } = render(
      <BoardPicker projectKey="CP" boardId={undefined} onPick={() => {}} />,
    );
    expect(container).toBeEmptyDOMElement();
  });

  it("offers each of a project's boards", () => {
    state.boards = [board(), board({ id: "b2", name: "Platform board", teamId: "t1" })];
    state.teams = [team()];

    render(<BoardPicker projectKey="CP" boardId={undefined} onPick={() => {}} />);
    expect(screen.getByRole("button", { name: "Customer Portal board" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Platform board" })).toBeInTheDocument();
  });

  // The first board is the one the project opens on, addressed by the project
  // rather than by id, so picking it hands back nothing.
  it("hands back nothing for the board the project opens on", async () => {
    state.boards = [board(), board({ id: "b2", name: "Platform board", teamId: "t1" })];
    state.teams = [team()];
    const onPick = vi.fn();

    render(<BoardPicker projectKey="CP" boardId="b2" onPick={onPick} />);
    await userEvent.click(screen.getByRole("button", { name: "Customer Portal board" }));

    expect(onPick).toHaveBeenCalledWith(null);
  });

  it("hands back the board that was picked", async () => {
    state.boards = [board(), board({ id: "b2", name: "Platform board", teamId: "t1" })];
    state.teams = [team()];
    const onPick = vi.fn();

    render(<BoardPicker projectKey="CP" boardId={undefined} onPick={onPick} />);
    await userEvent.click(screen.getByRole("button", { name: "Platform board" }));

    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "b2" }));
  });

  it("marks which board is being shown", () => {
    state.boards = [board(), board({ id: "b2", name: "Platform board", teamId: "t1" })];
    state.teams = [team()];

    render(<BoardPicker projectKey="CP" boardId="b2" onPick={() => {}} />);
    expect(screen.getByRole("button", { name: "Platform board" })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
  });

  // A team without a board has a backlog nobody can look at.
  it("offers to give a team without a board one", async () => {
    state.boards = [board()];
    state.teams = [team({ boardCount: 0 })];

    render(<BoardPicker projectKey="CP" boardId={undefined} onPick={() => {}} />);
    await userEvent.click(screen.getByRole("button", { name: "Give Platform a board" }));

    expect(createBoard).toHaveBeenCalledWith({ name: "Platform board", teamId: "t1" });
  });

  // A board made by mistake has to be put right from where it is seen.
  describe("managing the board on screen", () => {
    const two = () => [board(), board({ id: "b2", name: "Team flow", teamId: "t1" })];

    it("is not offered to somebody who cannot configure boards", () => {
      state.boards = two();
      state.teams = [team()];
      render(<BoardPicker projectKey="CP" boardId="b2" onPick={() => {}} />);
      expect(screen.queryByRole("button", { name: "Rename" })).not.toBeInTheDocument();
      expect(screen.queryByRole("button", { name: "Delete" })).not.toBeInTheDocument();
    });

    it("renames it", async () => {
      state.boards = two();
      state.teams = [team()];
      updateBoard.mockClear();
      render(<BoardPicker projectKey="CP" boardId="b2" onPick={() => {}} canManage />);

      await userEvent.click(screen.getByRole("button", { name: "Rename" }));
      await userEvent.clear(screen.getByLabelText("Board name"));
      await userEvent.type(screen.getByLabelText("Board name"), "Team lanes");
      await userEvent.click(screen.getByRole("button", { name: "Save name" }));

      expect(updateBoard).toHaveBeenCalledWith({ id: "b2", name: "Team lanes" }, expect.anything());
    });

    it("changes its type", async () => {
      state.boards = two();
      state.teams = [team()];
      updateBoard.mockClear();
      render(<BoardPicker projectKey="CP" boardId="b2" onPick={() => {}} canManage />);

      await userEvent.click(screen.getByRole("button", { name: "Change type" }));
      expect(updateBoard).toHaveBeenCalledWith({ id: "b2", type: "scrum" });
    });

    it("deletes it once asked by its name, and goes back to the opening board", async () => {
      state.boards = two();
      state.teams = [team()];
      deleteBoard.mockReset();
      deleteBoard.mockImplementation((_id: string, options?: { onSuccess?: () => void }) => options?.onSuccess?.());
      confirm.mockClear();
      const onPick = vi.fn();
      render(<BoardPicker projectKey="CP" boardId="b2" onPick={onPick} canManage />);

      await userEvent.click(screen.getByRole("button", { name: "Delete" }));

      expect(confirm).toHaveBeenCalledWith(expect.objectContaining({ noun: "board Team flow" }));
      expect(deleteBoard).toHaveBeenCalledWith("b2", expect.anything());
      expect(onPick).toHaveBeenCalledWith(null);
    });

    it("keeps nothing when the question is declined", async () => {
      state.boards = two();
      state.teams = [team()];
      deleteBoard.mockReset();
      confirm.mockImplementationOnce(async () => false);
      render(<BoardPicker projectKey="CP" boardId="b2" onPick={() => {}} canManage />);

      await userEvent.click(screen.getByRole("button", { name: "Delete" }));
      expect(deleteBoard).not.toHaveBeenCalled();
    });

    // A project keeps at least one board, which the server refuses too.
    it("does not offer deleting the last board", () => {
      state.boards = [board()];
      state.teams = [team({ boardCount: 0 })];
      render(<BoardPicker projectKey="CP" boardId={undefined} onPick={() => {}} canManage />);
      expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
    });
  });
});
