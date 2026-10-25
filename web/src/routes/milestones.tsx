import { Link, createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { useProject } from "@/api/projects";
import { MilestoneList } from "@/features/milestones/MilestoneList";
import { t } from "@/i18n";

export const milestonesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/milestones",
  component: MilestonesPage,
});

function MilestonesPage() {
  const { projectKey } = milestonesRoute.useParams();
  const { data } = useProject(projectKey);

  return (
    <Page width="content">
      <PageHeader
        crumb={
          <Link to="/projects/$projectKey" params={{ projectKey }} className="hover:text-ink">
            {data?.project?.name ?? projectKey}
          </Link>
        }
        title={t.schedule.milestones.title}
      />
      <MilestoneList projectKey={projectKey} />
    </Page>
  );
}
