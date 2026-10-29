import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { canAdminister, useAccess } from "@/api/access";
import { RepositoryList } from "@/features/git/RepositoryList";

export const repositoriesRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/repositories",
  component: RepositoriesPage,
});

function RepositoriesPage() {
  const { projectKey } = repositoriesRoute.useParams();
  const { data: access } = useAccess();

  return (
    <>
      <RepositoryList projectKey={projectKey} canManage={canAdminister(access, projectKey)} />
    </>
  );
}
