import { Link, Outlet, createRoute, useLocation } from "@tanstack/react-router";
import { appRoute } from "./app";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { EmptyState, Page } from "@/components/ui";
import { ApiError } from "@/api/client";
import { featureOfPath, featureWord, hasFeature } from "@/features/projects/features";
import { t } from "@/i18n";

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
          title={t.projectSetup.project.missing(projectKey)}
          description={t.projectSetup.project.missingDetail}
          action={
            <Link to="/projects" className="text-sm font-medium text-accent hover:underline">
              {t.projectSetup.project.allProjects}
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
            title={t.projectSetup.project.featureOff(data.project.name, featureWord(feature))}
            description={t.projectSetup.project.featureOffDetail}
            action={
              canAdminister(access, projectKey) ? (
                <Link to="/projects/$projectKey/settings" params={{ projectKey }} className="text-sm font-medium text-accent hover:underline">
                  {t.projectSetup.project.turnOn}
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
