import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { canAdminister, useAccess } from "@/api/access";
import { ProjectArrangement } from "@/features/arrange/pages";
import { t } from "@/i18n";

/** Which fields an issue of each type shows here, where, and in what order. */
export const issueArrangementRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/issue-view",
  component: IssueArrangementPage,
});

function IssueArrangementPage() {
  const { projectKey } = issueArrangementRoute.useParams();
  const { data: access } = useAccess();
  return (
    <>
      <p className="mb-4 text-sm text-ink-muted">{t.issueView.arrangement.meta}</p>
      <ProjectArrangement projectKey={projectKey} readOnly={!canAdminister(access, projectKey)} />
    </>
  );
}
