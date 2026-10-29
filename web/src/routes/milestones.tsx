import { createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { MilestoneList } from "@/features/milestones/MilestoneList";
import { t } from "@/i18n";

export const milestonesRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/milestones",
  component: MilestonesPage,
});

function MilestonesPage() {
  const { projectKey } = milestonesRoute.useParams();

  return (
    <Page width="content">
      <PageHeader title={t.schedule.milestones.title} />
      <MilestoneList projectKey={projectKey} />
    </Page>
  );
}
