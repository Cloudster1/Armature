import { createRoute } from "@tanstack/react-router";
import { projectRoute } from "./project";
import { Page, PageHeader } from "@/components/ui";
import { TeamList } from "@/features/teams/TeamList";
import { t } from "@/i18n";

export const teamsRoute = createRoute({
  getParentRoute: () => projectRoute,
  path: "/teams",
  component: TeamsPage,
});

function TeamsPage() {
  const { projectKey } = teamsRoute.useParams();

  return (
    <Page width="content">
      <PageHeader title={t.board.teams.title} />

      <TeamList projectKey={projectKey} />
    </Page>
  );
}
