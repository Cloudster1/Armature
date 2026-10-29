import { useMemo } from "react";
import { createRoute, useNavigate } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useSprints } from "@/api/sprints";
import { BoardView } from "@/features/board/BoardView";
import { Page, PageHeader, Tag } from "@/components/ui";
import { parseSearch, toSearch, type QuickFilterSearch } from "@/features/board/quickFilter";

/**
 * One sprint's board. A scrum board follows the sprint that is running; this
 * is the same board pointed at the sprint in the address, so a sprint that is
 * planned or over can be tracked as well.
 */
export const sprintBoardRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/sprints/$sprintId/board",
  validateSearch: (search: Record<string, unknown>): QuickFilterSearch => toSearch(parseSearch(search)),
  component: SprintBoardPage,
});

const stateLabel = { future: "Planned", active: "Running", closed: "Over" } as const;

function SprintBoardPage() {
  const { projectKey, sprintId } = sprintBoardRoute.useParams();
  const { data } = useSprints(projectKey, true);
  const sprint = data?.sprints.find((each) => each.id === sprintId);
  const search = sprintBoardRoute.useSearch();
  const navigate = useNavigate();
  const filter = useMemo(() => parseSearch(search), [search]);

  return (
    <Page width="wide">
      <PageHeader
        title={
          <>
            {sprint?.name ?? "Sprint board"}
            {sprint && <Tag data-sprint-state={sprint.state}>{stateLabel[sprint.state]}</Tag>}
          </>
        }
        meta={sprint?.goal || (sprint?.startsOn && sprint.endsOn ? `${sprint.startsOn.slice(0, 10)} to ${sprint.endsOn.slice(0, 10)}` : undefined)}
      />
      <BoardView
        projectKey={projectKey}
        sprintId={sprintId}
        filter={filter}
        onFilter={(next) =>
          navigate({ to: "/projects/$projectKey/sprints/$sprintId/board", params: { projectKey, sprintId }, search: toSearch(next), replace: true })
        }
      />
    </Page>
  );
}
