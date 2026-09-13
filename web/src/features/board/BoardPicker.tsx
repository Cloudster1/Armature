import { useBoards, useCreateBoard, type BoardSummary } from "@/api/boards";
import { useTeams } from "@/api/teams";
import { Button, ErrorBanner, Segmented } from "@/components/ui";

/**
 * Chooses between the boards a project has.
 *
 * Most projects have one and this is a single button nobody needs to press. A
 * project with teams has one board per team plus its own, and the difference
 * between them is which work they draw from.
 */
export function BoardPicker({
  projectKey,
  boardId,
  onPick,
}: {
  projectKey: string;
  boardId?: string;
  onPick: (board: BoardSummary | null) => void;
}) {
  const { data } = useBoards(projectKey);
  const { data: teamData } = useTeams(projectKey);
  const create = useCreateBoard(projectKey);

  const boards = data?.boards ?? [];
  const teams = teamData?.teams ?? [];
  // A team that has no board yet is the only thing worth offering to create.
  const unserved = teams.filter((team) => team.boardCount === 0);

  if (boards.length <= 1 && unserved.length === 0) return null;

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Segmented
        label="Board"
        // The first board is the one the project opens on, and is addressed by
        // the project rather than by id.
        value={boardId ?? boards[0]?.id ?? ""}
        onChange={(id) => {
          const index = boards.findIndex((board) => board.id === id);
          onPick(index <= 0 ? null : boards[index]!);
        }}
        options={boards.map((board) => ({
          value: board.id,
          attrs: { "data-board": board.name },
          label: (
            <>
              {board.name}
              <span aria-hidden="true" className="ml-1.5 text-2xs font-normal text-ink-subtle uppercase">
                {board.type === "scrum" ? "scrum" : "kanban"}
              </span>
            </>
          ),
        }))}
      />

      {unserved.map((team) => (
        <Button
          key={team.id}
          size="sm"
          variant="secondary"
          loading={create.isPending}
          onClick={() => create.mutate({ name: `${team.name} board`, teamId: team.id })}
        >
          Give {team.name} a board
        </Button>
      ))}

      {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}
    </div>
  );
}
