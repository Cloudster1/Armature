import { useState, type FormEvent } from "react";
import { useBoards, useCreateBoard, useDeleteBoard, useUpdateBoard, type BoardSummary, type BoardType } from "@/api/boards";
import { useTeams } from "@/api/teams";
import { Button, ErrorBanner, Field, Segmented } from "@/components/ui";
import { useConfirm } from "@/features/shell/ConfirmProvider";

/** Chooses between a project's boards, which differ in whose work they draw;
 * most projects have one, and then there is nothing to choose. */
export function BoardPicker({
  projectKey,
  boardId,
  onPick,
  canManage = false,
}: {
  projectKey: string;
  boardId?: string;
  onPick: (board: BoardSummary | null) => void;
  /** Offers renaming, retyping and deleting the board on screen. */
  canManage?: boolean;
}) {
  const { data } = useBoards(projectKey);
  const { data: teamData } = useTeams(projectKey);
  const create = useCreateBoard(projectKey);

  const boards = data?.boards ?? [];
  const teams = teamData?.teams ?? [];
  // A team that has no board yet is the only thing worth offering to create.
  const unserved = teams.filter((team) => team.boardCount === 0);
  const shown = boards.find((board) => board.id === boardId) ?? boards[0];

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

      {canManage && shown && <ManageBoard key={shown.id} board={shown} isLast={boards.length <= 1} onDeleted={() => onPick(null)} />}

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

/** Puts right a board made by mistake: its name, its type, or the board itself. */
function ManageBoard({ board, isLast, onDeleted }: { board: BoardSummary; isLast: boolean; onDeleted: () => void }) {
  const update = useUpdateBoard();
  const remove = useDeleteBoard();
  const confirm = useConfirm();
  const [renaming, setRenaming] = useState(false);
  const [name, setName] = useState(board.name);
  const other: BoardType = board.type === "scrum" ? "kanban" : "scrum";
  const error = (update.error ?? remove.error) as Error | null;

  function rename(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    update.mutate({ id: board.id, name: name.trim() }, { onSuccess: () => setRenaming(false) });
  }

  async function deleteBoard() {
    const sure = await confirm({
      noun: `board ${board.name}`,
      body: `${board.name} and its swimlanes go. The issues it showed stay in the project.`,
    });
    // Not mutate's onSuccess: the refetched list can hide the picker first, and
    // an unmounted observer drops it, leaving the page on a board that is gone.
    if (sure) remove.mutateAsync(board.id).then(onDeleted, () => {});
  }

  return (
    <>
      {renaming ? (
        <form onSubmit={rename} className="flex items-end gap-2" noValidate>
          <Field label="Board name" autoFocus required value={name} onChange={(event) => setName(event.target.value)} />
          <Button type="submit" size="sm" loading={update.isPending} disabled={!name.trim()}>
            Save name
          </Button>
          <Button type="button" size="sm" variant="secondary" onClick={() => setRenaming(false)}>
            Cancel
          </Button>
        </form>
      ) : (
        <div className="flex items-center gap-1">
          <Button size="sm" variant="ghost" onClick={() => setRenaming(true)}>
            Rename
          </Button>
          <Button
            size="sm"
            variant="ghost"
            title={`Make ${board.name} a ${other} board`}
            disabled={update.isPending}
            onClick={() => update.mutate({ id: board.id, type: other })}
          >
            Change type
          </Button>
          <Button
            size="sm"
            variant="ghost"
            disabled={isLast}
            loading={remove.isPending}
            title={isLast ? "A project keeps at least one board" : `Delete ${board.name}`}
            onClick={deleteBoard}
          >
            Delete
          </Button>
        </div>
      )}
      {error && <ErrorBanner>{error.message}</ErrorBanner>}
    </>
  );
}
