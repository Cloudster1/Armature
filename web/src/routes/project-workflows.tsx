import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { ProjectWorkflows } from "@/features/workflows/ProjectWorkflows";

export const projectWorkflowsRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/workflows",
  component: ProjectWorkflowsPage,
});

function ProjectWorkflowsPage() {
  const { projectKey } = projectWorkflowsRoute.useParams();

  return (
    <>
      <ProjectWorkflows projectKey={projectKey} />
    </>
  );
}
