import { createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { SprintPlanning } from "@/features/sprints/SprintPlanning";
import { t } from "@/i18n";

export const sprintsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/sprints",
  component: SprintsPage,
});

function SprintsPage() {
  const { projectKey } = sprintsRoute.useParams();

  return (
    <Page width="content">
      <PageHeader title={t.schedule.sprints.title} />

      <SprintPlanning projectKey={projectKey} />
    </Page>
  );
}
