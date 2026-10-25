import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { RepositoryList } from "@/features/git/RepositoryList";
import { t } from "@/i18n";

export const repositoriesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/repositories",
  component: RepositoriesPage,
});

function RepositoriesPage() {
  const { projectKey } = repositoriesRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title={t.shell.projectPages.repositories}
      />
      <RepositoryList projectKey={projectKey} canManage={canAdminister(access, projectKey)} />
    </Page>
  );
}
