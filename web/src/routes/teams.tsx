import { createRoute } from "@tanstack/react-router";
import { projectSettingsRoute } from "./project-settings";
import { TeamList } from "@/features/teams/TeamList";

export const teamsRoute = createRoute({
  getParentRoute: () => projectSettingsRoute,
  path: "/teams",
  component: TeamsPage,
});

function TeamsPage() {
  const { projectKey } = teamsRoute.useParams();

  return (
    <>

      <TeamList projectKey={projectKey} />
    </>
  );
}
