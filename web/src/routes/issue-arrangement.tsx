import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
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
  return (
    <>
      <p className="mb-4 text-sm text-ink-muted">{t.issueView.arrangement.meta}</p>
      <ProjectArrangement projectKey={projectKey} />
    </>
  );
}
