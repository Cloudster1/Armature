import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Team } from "@/api/teams";

const createBoard = vi.fn();
const state = { teams: [] as Team[] };

vi.mock("@/api/boards", () => ({
  useCreateBoard: () => ({ mutate: createBoard, isPending: false, error: null }),
}));

vi.mock("@/api/teams", () => ({
  useTeams: () => ({ data: { teams: state.teams } }),
}));

const { NewBoardForm } = await import("./NewBoardForm");

const platform: Team = {
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
};

describe("NewBoardForm", () => {
  // A team's board in a scrum project should be a scrum board without anybody
  // having to say so; the project's own type is the starting point.
  it("starts on the project's own board type", () => {
    state.teams = [];
    render(<NewBoardForm projectKey="CP" defaultType="scrum" onDone={() => {}} />);
    expect(screen.getByRole("radio", { name: /Scrum/ })).toHaveAttribute("aria-checked", "true");
  });

  it("creates a board of the type that was picked, over the whole project", async () => {
    state.teams = [platform];
    createBoard.mockClear();
    render(<NewBoardForm projectKey="CP" defaultType="scrum" onDone={() => {}} />);

    await userEvent.type(screen.getByLabelText("Board name"), "Flow");
    await userEvent.click(screen.getByRole("radio", { name: /Kanban/ }));
    await userEvent.click(screen.getByRole("button", { name: "Create board" }));

    expect(createBoard).toHaveBeenCalledWith(
      { name: "Flow", type: "kanban", teamId: null },
      expect.anything(),
    );
  });

  it("can scope the board to a team", async () => {
    state.teams = [platform];
    createBoard.mockClear();
    render(<NewBoardForm projectKey="CP" defaultType="kanban" onDone={() => {}} />);

    await userEvent.type(screen.getByLabelText("Board name"), "Platform flow");
    await userEvent.selectOptions(screen.getByLabelText("Draws from"), "t1");
    await userEvent.click(screen.getByRole("button", { name: "Create board" }));

    expect(createBoard).toHaveBeenCalledWith(
      { name: "Platform flow", type: "kanban", teamId: "t1" },
      expect.anything(),
    );
  });

  // Without teams there is nothing to draw from but the project, so the
  // question is not asked.
  it("does not ask whose work to draw when there are no teams", () => {
    state.teams = [];
    render(<NewBoardForm projectKey="CP" defaultType="kanban" onDone={() => {}} />);
    expect(screen.queryByLabelText("Draws from")).not.toBeInTheDocument();
  });
});
