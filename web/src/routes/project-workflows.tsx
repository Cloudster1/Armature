import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { useProject } from "@/api/projects";
import { ProjectWorkflows } from "@/features/workflows/ProjectWorkflows";

export const projectWorkflowsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/workflows",
  component: ProjectWorkflowsPage,
});

function ProjectWorkflowsPage() {
  const { projectKey } = projectWorkflowsRoute.useParams();
  const { data } = useProject(projectKey);

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title="Workflow scheme"
      />

      <ProjectWorkflows projectKey={projectKey} />
    </Page>
  );
}
