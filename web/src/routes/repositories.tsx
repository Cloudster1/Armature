import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { Page, PageHeader } from "@/components/ui";
import { RepositoryList } from "@/features/git/RepositoryList";

export const repositoriesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/repositories",
  component: RepositoriesPage,
});

function RepositoriesPage() {
  const { projectKey } = repositoriesRoute.useParams();
  const { data } = useProject(projectKey);

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Repositories"
      />
      <RepositoryList projectKey={projectKey} />
    </Page>
  );
}
