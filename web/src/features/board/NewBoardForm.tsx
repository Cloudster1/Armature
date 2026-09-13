import { useState, type FormEvent } from "react";
import { useCreateBoard, type BoardType } from "@/api/boards";
import { useTeams } from "@/api/teams";
import { Button, Card, ErrorBanner, Field, OptionCard, Select } from "@/components/ui";

const boardTypes: Array<{ value: BoardType; label: string; description: string }> = [
  {
    value: "scrum",
    label: "Scrum",
    description: "Shows the sprint that is running. The next one is planned from the backlog.",
  },
  {
    value: "kanban",
    label: "Kanban",
    description: "Shows everything in flight. Each swimlane can carry a limit.",
  },
];

/**
 * Adds a board to a project.
 *
 * The type is the decision that matters, so it is a pair of cards rather than a
 * select: which one is chosen changes what the board will show, and the words
 * on the card are what make that choice rather than the label.
 */
export function NewBoardForm({
  projectKey,
  defaultType,
  onDone,
}: {
  projectKey: string;
  /** The project's own board type, which a new board takes after until told otherwise. */
  defaultType: BoardType;
  onDone: () => void;
}) {
  const create = useCreateBoard(projectKey);
  const { data: teamData } = useTeams(projectKey);
  const [name, setName] = useState("");
  const [type, setType] = useState<BoardType>(defaultType);
  // Empty is a board over the whole project.
  const [teamId, setTeamId] = useState("");

  const teams = teamData?.teams ?? [];

  function onSubmit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) return;
    create.mutate(
      { name: name.trim(), type, teamId: teamId || null },
      { onSuccess: onDone },
    );
  }

  return (
    <Card className="mb-4 p-4">
      <form onSubmit={onSubmit} className="space-y-4" noValidate>
        {create.error && <ErrorBanner>{(create.error as Error).message}</ErrorBanner>}

        <Field
          label="Board name"
          autoFocus
          required
          value={name}
          onChange={(event) => setName(event.target.value)}
          placeholder="Release board"
        />

        <fieldset className="space-y-1.5">
          <legend className="block text-sm font-medium text-ink">Type</legend>
          <div role="radiogroup" aria-label="Board type" className="grid gap-2 sm:grid-cols-2">
            {boardTypes.map((option) => (
              <OptionCard
                key={option.value}
                checked={option.value === type}
                data-board-type={option.value}
                onSelect={() => setType(option.value)}
                title={option.label}
                description={option.description}
              />
            ))}
          </div>
        </fieldset>

        {teams.length > 0 && (
          <Select label="Draws from" id="board-team" value={teamId} onChange={(event) => setTeamId(event.target.value)}>
            <option value="">The whole project</option>
            {teams.map((team) => (
              <option key={team.id} value={team.id}>
                {team.name}
              </option>
            ))}
          </Select>
        )}

        <div className="flex gap-2">
          <Button type="submit" loading={create.isPending} disabled={!name.trim()}>
            Create board
          </Button>
          <Button type="button" variant="secondary" onClick={onDone}>
            Cancel
          </Button>
        </div>
      </form>
    </Card>
  );
}
