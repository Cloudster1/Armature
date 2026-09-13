import { useState } from "react";
import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useBoards } from "@/api/boards";
import { useProject } from "@/api/projects";
import { useMe } from "@/api/auth";
import { BoardPicker } from "@/features/board/BoardPicker";
import { NewBoardForm } from "@/features/board/NewBoardForm";
import { BoardView } from "@/features/board/BoardView";
import { SwimlaneEditor } from "@/features/board/SwimlaneEditor";
import { Button, Page, PageHeader, Tag, cx } from "@/components/ui";

export const boardRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/board",
  component: BoardPage,
});

function BoardPage() {
  const { projectKey } = boardRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: me } = useMe();
  const [configuring, setConfiguring] = useState(false);
  const [adding, setAdding] = useState(false);
  // Null is the board the project opens on, which is addressed by the project
  // rather than by id.
  const [board, setBoard] = useState<{ id: string; name: string } | null>(null);
  const { data: boardData } = useBoards(projectKey);

  // The heading names whichever board is on screen. A project with three of
  // them and a heading that just says "Board" is a page you cannot read.
  const boards = boardData?.boards ?? [];
  const showing = board ? boards.find((each) => each.id === board.id) : boards[0];
  const shown = showing?.name ?? "Board";
  const scrum = showing?.type === "scrum";

  const canConfigure = me?.principal.role === "owner" || me?.principal.role === "admin";

  return (
    <Page width="wide">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title={
          <>
            {shown}
            {showing && <Tag data-board-type={showing.type}>{scrum ? "Scrum" : "Kanban"}</Tag>}
          </>
        }
        meta={scrum ? "Shows the sprint that is running." : "Shows everything in flight."}
        tabs={
          <BoardPicker
            projectKey={projectKey}
            boardId={board?.id}
            onPick={(picked) => setBoard(picked ? { id: picked.id, name: picked.name } : null)}
          />
        }
        actions={
          canConfigure && (
            <>
              {!adding && (
                <Button variant="secondary" onClick={() => setAdding(true)}>
                  New board
                </Button>
              )}
              <Button
                variant={configuring ? "primary" : "secondary"}
                onClick={() => setConfiguring(!configuring)}
              >
                {configuring ? "Done configuring" : "Configure swimlanes"}
              </Button>
            </>
          )
        }
      />

      {adding && (
        <NewBoardForm
          projectKey={projectKey}
          defaultType={boards[0]?.type ?? "kanban"}
          onDone={() => setAdding(false)}
        />
      )}

      <div className={cx(configuring && "grid gap-6 xl:grid-cols-[1fr_28rem]")}>
        {/* min-w-0 lets the grid track shrink below the board's natural width.
            Without it the swimlanes push the configuration panel off screen
            instead of scrolling within their own container. */}
        <div className="min-w-0">
          <BoardView projectKey={projectKey} boardId={board?.id} />
        </div>
        {configuring && (
          <aside className="min-w-0">
            <h2 className="mb-3 text-xs font-semibold tracking-wide text-ink-muted uppercase">
              Swimlanes
            </h2>
            <SwimlaneEditor projectKey={projectKey} />
          </aside>
        )}
      </div>
    </Page>
  );
}
