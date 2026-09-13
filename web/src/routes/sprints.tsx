import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { useProject } from "@/api/projects";
import { SprintPlanning } from "@/features/sprints/SprintPlanning";

export const sprintsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/sprints",
  component: SprintsPage,
});

function SprintsPage() {
  const { projectKey } = sprintsRoute.useParams();
  const { data } = useProject(projectKey);

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Sprints"
      />

      <SprintPlanning projectKey={projectKey} />
    </Page>
  );
}
