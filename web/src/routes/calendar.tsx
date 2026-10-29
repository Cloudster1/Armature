import { createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { canWriteIssues, useAccess } from "@/api/access";
import { Page, PageHeader } from "@/components/ui";
import { MonthGrid } from "@/features/calendar/MonthGrid";
import { t } from "@/i18n";

/** A month of the project's dated work, sprints, milestones, versions and days off. */
export const calendarRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/calendar",
  component: CalendarPage,
});

function CalendarPage() {
  const { projectKey } = calendarRoute.useParams();
  const { data: access } = useAccess();
  return (
    <Page width="wide">
      <PageHeader title={t.schedule.calendar.title} meta={t.schedule.calendar.meta} />
      <MonthGrid projectKey={projectKey} canWrite={canWriteIssues(access, projectKey)} />
    </Page>
  );
}
