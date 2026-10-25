import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { useProject } from "@/api/projects";
import { canAdminister, useAccess } from "@/api/access";
import { EmptyState, Page, PageHeader } from "@/components/ui";
import { ProjectArrangement } from "@/features/arrange/pages";
import { t } from "@/i18n";

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
        title={t.issueView.arrangement.title}
        meta={t.issueView.arrangement.meta}
      />
      {canAdminister(access, projectKey) ? (
        <ProjectArrangement projectKey={projectKey} />
      ) : (
        <EmptyState title={t.issueView.arrangement.onlyAdmins} description={t.issueView.arrangement.askOne} />
      )}
    </Page>
  );
}
