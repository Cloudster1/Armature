import { Link, Outlet, createRoute, useLocation } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { EmptyState, Page } from "@/components/ui";
import { ApiError } from "@/api/client";
import { featureOfPath, featureWord, hasFeature } from "@/features/projects/features";

// Every page of a project hangs off this route, so the project is loaded once
// and a project that does not exist is answered once.
export const projectRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/projects/$projectKey",
  component: ProjectLayout,
});

function ProjectLayout() {
  const { projectKey } = projectRoute.useParams();
  const { data, error } = useProject(projectKey);
  const { data: access } = useAccess();
  const pathname = useLocation({ select: (location) => location.pathname });
  if (error instanceof ApiError && error.status === 404) {
    return (
      <Page width="content">
        <EmptyState
          title={`There is no project ${projectKey}`}
          description="It may have been archived, or the key in the address is not right."
          action={
            <Link to="/projects" className="text-sm font-medium text-accent hover:underline">
              All projects
            </Link>
          }
        />
      </Page>
    );
  }
  // A page the project does not have is answered here, once, rather than by
  // every route: an old link lands on a notice that points at the switch.
  const feature = featureOfPath(projectKey, pathname);
  if (feature && data?.project && !hasFeature(data.project, feature)) {
    return (
      <Page width="content">
        <div data-feature-off={feature}>
          <EmptyState
            title={`${data.project.name} does not use ${featureWord(feature)}`}
            description="Its template left the page out. An administrator can turn it on under the project's settings."
            action={
              canAdminister(access, projectKey) ? (
                <Link to="/projects/$projectKey/settings" params={{ projectKey }} className="text-sm font-medium text-accent hover:underline">
                  Turn it on in Settings
                </Link>
              ) : undefined
            }
          />
        </div>
      </Page>
    );
  }
  return <Outlet />;
}
