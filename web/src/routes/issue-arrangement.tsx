import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { EmptyState, Page, PageHeader } from "@/components/ui";
import { ProjectArrangement } from "@/features/arrange/pages";

/** Which fields an issue of each type shows here, where, and in what order. */
export const issueArrangementRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/issue-view",
  component: IssueArrangementPage,
});

function IssueArrangementPage() {
  const { projectKey } = issueArrangementRoute.useParams();
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
        title="Issue view"
        meta="Which fields an issue shows, where, and in what order, for each issue type. What this project says nothing about follows the organization."
      />
      {canAdminister(access, projectKey) ? (
        <ProjectArrangement projectKey={projectKey} />
      ) : (
        <EmptyState title="Only the project's administrators arrange its issues" description="Ask one of them to change what an issue shows." />
      )}
    </Page>
  );
}
