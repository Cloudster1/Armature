import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { useProject } from "@/api/projects";
import { TeamList } from "@/features/teams/TeamList";

export const teamsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/teams",
  component: TeamsPage,
});

function TeamsPage() {
  const { projectKey } = teamsRoute.useParams();
  const { data } = useProject(projectKey);

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Teams"
      />

      <TeamList projectKey={projectKey} />
    </Page>
  );
}
