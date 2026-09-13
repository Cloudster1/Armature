import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, holdsRole, useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { ReleaseList } from "@/features/versions/ReleaseList";
import { ComponentList } from "@/features/versions/ComponentList";

/** What the project ships, in order. */
export const releasesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/releases",
  component: ReleasesPage,
});

/** The project's parts. */
export const componentsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/components",
  component: ComponentsPage,
});

function ReleasesPage() {
  const { projectKey } = releasesRoute.useParams();
  const { data } = useProject(projectKey);
  const { data: access } = useAccess();
  // Planning a release is the same job as planning a sprint.
  const canPlan = holdsRole(access, ["scrum_master", "project_administrator", "global_administrator"], projectKey);
  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Releases"
        meta="What ships, and how close each version is"
      />
      <ReleaseList projectKey={projectKey} canPlan={canPlan} />
    </Page>
  );
}

function ComponentsPage() {
  const { projectKey } = componentsRoute.useParams();
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
        title="Components"
        meta="The project's parts, and who looks after each"
      />
      <ComponentList projectKey={projectKey} canEdit={Boolean(canAdminister(access, projectKey))} />
    </Page>
  );
}
